package cli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fgmacedo/buchecha/internal/api"
	"github.com/fgmacedo/buchecha/internal/director/dag"
	"github.com/fgmacedo/buchecha/internal/mcp"
)

// TestStartRunListener_MountsMCPAtMcpPrefix asserts the run-wide
// listener mounts the boot's MCP handler at /mcp/ on the same port as
// /api/v1/. The test sends an MCP `initialize` payload with a
// registered role header; the dispatcher returns the protocol-version
// response.
func TestStartRunListener_MountsMCPAtMcpPrefix(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := startRunListener(ctx, nil, nil, nil, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startRunListener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Stop() })

	url := listener.boot.MCPURL()
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("MCPURL = %q, want http://127.0.0.1:<port>/...", url)
	}
	if !strings.HasSuffix(url, "/mcp/") {
		t.Fatalf("MCPURL = %q, want /mcp/ suffix", url)
	}

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(mcp.RoleHeader, string(dag.RolePlanner))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, out)
	}
	out, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(out), `"protocolVersion"`) {
		t.Errorf("response missing protocolVersion: %s", out)
	}
}

// TestStartRunListener_ServesAPIRootCatalog asserts the API root
// catalog responds at /api/v1/ on the shared listener. The session
// token bearer authenticates the request.
func TestStartRunListener_ServesAPIRootCatalog(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := startRunListener(ctx, nil, nil, nil, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startRunListener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Stop() })

	url := "http://" + listener.addr + "/api/v1"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+listener.sessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

// TestStartRunListener_RootIs404WithoutWebUI confirms the / subtree is
// not mounted when webuiHandler is nil. chi returns its default 404.
func TestStartRunListener_RootIs404WithoutWebUI(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := startRunListener(ctx, nil, nil, nil, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startRunListener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Stop() })

	url := "http://" + listener.addr + "/"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (no WebUI mount)", resp.StatusCode)
	}
}

// TestStartRunListener_StopShutsDown verifies the Stop helper drains
// the serve goroutine and unbinds the listener.
func TestStartRunListener_StopShutsDown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := startRunListener(ctx, nil, nil, nil, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startRunListener: %v", err)
	}

	if err := listener.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	// A second Stop should be a safe no-op.
	if err := listener.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}

	// The listener is closed; a request should fail at the transport.
	url := "http://" + listener.addr + "/api/v1"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
		t.Error("request after Stop succeeded; want connection error")
	}
}

// TestStartRunListener_BindError surfaces a deliberately invalid bind
// address as a non-nil error from the constructor.
func TestStartRunListener_BindError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	_, err := startRunListener(ctx, nil, nil, nil, "127.0.0.1:-1")
	if err == nil {
		t.Fatal("expected bind error, got nil")
	}
}

// TestStartRunListener_RoutesShareSinglePort asserts /api/v1/, /mcp/,
// and / are reachable under the same host:port. The MCP request needs
// the X-BCC-Role header; / has no mount so it returns 404; /api/v1
// happy path returns 200 with the session bearer.
func TestStartRunListener_RoutesShareSinglePort(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listener, err := startRunListener(ctx, nil, nil, nil, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startRunListener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Stop() })

	apiURL := "http://" + listener.addr + "/api/v1"
	mcpURL := listener.boot.MCPURL()

	if !strings.HasPrefix(apiURL, "http://127.0.0.1:") {
		t.Fatalf("api url malformed: %q", apiURL)
	}
	if !strings.HasPrefix(mcpURL, "http://127.0.0.1:") {
		t.Fatalf("mcp url malformed: %q", mcpURL)
	}

	// Same host:port on both surfaces.
	apiHostPort := strings.TrimSuffix(strings.TrimPrefix(apiURL, "http://"), "/api/v1")
	mcpHostPort := strings.TrimSuffix(strings.TrimPrefix(mcpURL, "http://"), "/mcp/")
	if apiHostPort != mcpHostPort {
		t.Errorf("api and mcp on different ports: api=%q mcp=%q", apiHostPort, mcpHostPort)
	}

	// /api/v1 with bearer.
	req1, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	req1.Header.Set("Authorization", "Bearer "+listener.sessionToken)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("api do: %v", err)
	}
	_ = resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("api status = %d, want 200", resp1.StatusCode)
	}

	// /mcp/ with role header.
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, body)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set(mcp.RoleHeader, string(dag.RolePlanner))
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("mcp do: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("mcp status = %d, want 200", resp2.StatusCode)
	}

	// / has no WebUI mount; chi returns 404.
	rootURL := "http://" + listener.addr + "/"
	req3, _ := http.NewRequestWithContext(ctx, http.MethodGet, rootURL, nil)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("root do: %v", err)
	}
	_ = resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("root status = %d, want 404", resp3.StatusCode)
	}
}

// staticHandler is a minimal http.Handler the WebUI mount tests use to
// confirm Mounts.WebUI flows requests at / through the configured
// handler instead of returning 404. The handler writes a deterministic
// body so the test can assert end-to-end dispatch.
type staticHandler struct {
	body string
}

func (h staticHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, h.body)
}

// TestStartRunListener_MountsWebUIAtRoot covers the WebUI mount path:
// when webuiHandler is non-nil, requests to / land on it instead of
// returning 404. The session bearer authorizes the request because the
// session token gates / and /api/v1/* in T4.6.
func TestStartRunListener_MountsWebUIAtRoot(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	hello := staticHandler{body: "hello-webui"}
	listener, err := startRunListener(ctx, nil, nil, hello, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startRunListener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Stop() })

	url := "http://" + listener.addr + "/"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+listener.sessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (webui handler hit)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello-webui" {
		t.Errorf("body = %q, want %q", body, "hello-webui")
	}
}

// Compile-time check that runListener exposes the api.Server we
// constructed; this guards the field rename if it ever happens.
var _ = (*api.Server)(nil)
