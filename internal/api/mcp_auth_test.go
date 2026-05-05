package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/fgmacedo/buchecha/internal/services"
)

const mcpTestToken = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

// TestMCPAuth_AcceptsValidBearerAndRole walks the happy path: an
// authorized agent presents the run-wide bearer plus a registered
// role. The middleware passes the request through to the wrapped
// handler.
func TestMCPAuth_AcceptsValidBearerAndRole(t *testing.T) {
	t.Parallel()

	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := MCPAuth(mcpTestToken, []string{"bcc-planner", "bcc-executor"})(probe)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
	req.Header.Set("Authorization", "Bearer "+mcpTestToken)
	req.Header.Set(mcpRoleHeader, "bcc-planner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", rec.Code)
	}
}

// TestMCPAuth_RejectsMissingBearer covers the no-credential path. The
// canonical unauthorized envelope must be written; downstream handlers
// must not run.
func TestMCPAuth_RejectsMissingBearer(t *testing.T) {
	t.Parallel()

	hit := false
	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusNoContent)
	})
	h := MCPAuth(mcpTestToken, []string{"bcc-planner"})(probe)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
	req.Header.Set(mcpRoleHeader, "bcc-planner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	if hit {
		t.Error("downstream handler reached despite missing bearer")
	}
	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != services.CodeUnauthorized {
		t.Errorf("envelope code = %q, want %q", body.Code, services.CodeUnauthorized)
	}
}

// TestMCPAuth_RejectsBadBearer asserts a wrong bearer is rejected even
// when the role is valid.
func TestMCPAuth_RejectsBadBearer(t *testing.T) {
	t.Parallel()

	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := MCPAuth(mcpTestToken, []string{"bcc-planner"})(probe)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
	req.Header.Set("Authorization", "Bearer not-the-token")
	req.Header.Set(mcpRoleHeader, "bcc-planner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", rec.Code)
	}
}

// TestMCPAuth_RejectsUnknownRole asserts an unregistered role with a
// valid bearer is still rejected.
func TestMCPAuth_RejectsUnknownRole(t *testing.T) {
	t.Parallel()

	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := MCPAuth(mcpTestToken, []string{"bcc-planner"})(probe)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
	req.Header.Set("Authorization", "Bearer "+mcpTestToken)
	req.Header.Set(mcpRoleHeader, "bcc-mystery")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", rec.Code)
	}
}

// TestMCPAuth_RejectsMissingRole asserts an empty X-BCC-Role header is
// not silently accepted even when the bearer matches.
func TestMCPAuth_RejectsMissingRole(t *testing.T) {
	t.Parallel()

	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := MCPAuth(mcpTestToken, []string{"bcc-planner"})(probe)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
	req.Header.Set("Authorization", "Bearer "+mcpTestToken)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401 (missing role)", rec.Code)
	}
}

// TestMCPAuth_RejectsEmptyToken pins the precondition: a server that
// somehow boots with an empty token must reject every request rather
// than silently accept anything.
func TestMCPAuth_RejectsEmptyToken(t *testing.T) {
	t.Parallel()

	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := MCPAuth("", []string{"bcc-planner"})(probe)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
	req.Header.Set("Authorization", "Bearer anything")
	req.Header.Set(mcpRoleHeader, "bcc-planner")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401 (empty server token rejects all)", rec.Code)
	}
}

// TestPathScopedAuth_CrossSurfaceRejection ties the two auth schemes
// together by mounting both on the same Server. A session-token bearer
// must reach /api/v1/* but be rejected at /mcp/; an MCP bearer + role
// must reach /mcp/ but be rejected at /api/v1/*.
func TestPathScopedAuth_CrossSurfaceRejection(t *testing.T) {
	t.Parallel()

	const sessionToken = "cafef00dcafef00dcafef00dcafef00dcafef00dcafef00dcafef00dcafef00d"

	mcpHit := 0
	mcpProbe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mcpHit++
		w.WriteHeader(http.StatusNoContent)
	})

	s := New(nil).
		WithAuth(sessionToken).
		WithMounts(Mounts{
			MCP:     mcpProbe,
			MCPAuth: MCPAuth(mcpTestToken, []string{"bcc-planner"}),
		})
	// Materialize the router and register a huma probe under /api/v1/
	// so the session-bearer path has a downstream operation to hit.
	handler := s.Routes()
	huma.Register(s.HumaAPI(), huma.Operation{
		Method:        http.MethodGet,
		Path:          "/probe",
		DefaultStatus: http.StatusNoContent,
	}, func(_ context.Context, _ *struct{}) (*struct{}, error) {
		return nil, nil
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	t.Run("session bearer reaches api but not mcp", func(t *testing.T) {
		// api happy path.
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/probe", nil)
		req.Header.Set("Authorization", "Bearer "+sessionToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("api do: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("api status: got %d, want 204", resp.StatusCode)
		}

		// mcp must reject the session token.
		req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp/", nil)
		req2.Header.Set("Authorization", "Bearer "+sessionToken)
		req2.Header.Set(mcpRoleHeader, "bcc-planner")
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			t.Fatalf("mcp do: %v", err)
		}
		_ = resp2.Body.Close()
		if resp2.StatusCode != http.StatusUnauthorized {
			t.Errorf("mcp status with session token: got %d, want 401", resp2.StatusCode)
		}
	})

	t.Run("mcp bearer reaches mcp but not api", func(t *testing.T) {
		// mcp happy path.
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp/", nil)
		req.Header.Set("Authorization", "Bearer "+mcpTestToken)
		req.Header.Set(mcpRoleHeader, "bcc-planner")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("mcp do: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("mcp status: got %d, want 204", resp.StatusCode)
		}

		// api must reject the mcp token.
		req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/probe", nil)
		req2.Header.Set("Authorization", "Bearer "+mcpTestToken)
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			t.Fatalf("api do: %v", err)
		}
		_ = resp2.Body.Close()
		if resp2.StatusCode != http.StatusUnauthorized {
			t.Errorf("api status with mcp token: got %d, want 401", resp2.StatusCode)
		}
	})

	t.Run("missing credential rejected on both", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/probe", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("api do: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("api status: got %d, want 401", resp.StatusCode)
		}

		req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp/", nil)
		req2.Header.Set(mcpRoleHeader, "bcc-planner")
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			t.Fatalf("mcp do: %v", err)
		}
		_ = resp2.Body.Close()
		if resp2.StatusCode != http.StatusUnauthorized {
			t.Errorf("mcp status: got %d, want 401", resp2.StatusCode)
		}
	})

	if mcpHit < 1 {
		t.Errorf("mcp probe never hit on happy path")
	}
}
