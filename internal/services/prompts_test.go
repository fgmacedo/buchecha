package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fgmacedo/buchecha/internal/director"
)

// seedPrompt writes <sessionDir>/prompts/<role>.md with the supplied
// body so PromptService.Get has the same on-disk shape the run boot
// produces.
func seedPrompt(t *testing.T, sessionDir, role, body string) {
	t.Helper()
	dir := filepath.Join(sessionDir, "prompts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, role+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
}

func TestPromptService_Get(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := director.Session{
		ID:        "abcdef111111",
		SpecPath:  "/spec/p.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	sessionDir := filepath.Join(baseDir, "sessions", sess.ID)
	seedPrompt(t, sessionDir, "planner", "# planner prompt\n")
	seedPrompt(t, sessionDir, "briefer", "# briefer prompt\n")
	seedPrompt(t, sessionDir, "executor", "# executor prompt\n")
	seedPrompt(t, sessionDir, "reviewer", "# reviewer prompt\n")

	svc := newPromptService(Deps{SessionsBaseDir: baseDir})

	cases := []struct {
		name     string
		role     string
		wantBody string
		wantErr  error
	}{
		{name: "planner", role: "planner", wantBody: "# planner prompt\n"},
		{name: "briefer", role: "briefer", wantBody: "# briefer prompt\n"},
		{name: "executor", role: "executor", wantBody: "# executor prompt\n"},
		{name: "reviewer", role: "reviewer", wantBody: "# reviewer prompt\n"},
		{name: "invalid role", role: "operator", wantErr: ErrInvalidRequest},
		{name: "empty role", role: "", wantErr: ErrInvalidRequest},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.Get(context.Background(), sess.ID, tc.role)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Markdown != tc.wantBody {
				t.Fatalf("Markdown = %q, want %q", got.Markdown, tc.wantBody)
			}
			if got.Role != tc.role {
				t.Fatalf("Role = %q", got.Role)
			}
			if got.SessionID != sess.ID {
				t.Fatalf("SessionID = %q", got.SessionID)
			}
		})
	}
}

func TestPromptService_Get_UnknownSession(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	svc := newPromptService(Deps{SessionsBaseDir: filepath.Join(tmp, ".bcc")})
	_, err := svc.Get(context.Background(), "000000000000", "planner")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestPromptService_Get_RoleFileMissing(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := director.Session{
		ID:        "999999999999",
		SpecPath:  "/spec/x.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	// No prompts written.
	svc := newPromptService(Deps{SessionsBaseDir: baseDir})
	_, err := svc.Get(context.Background(), sess.ID, "executor")
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestPromptService_Get_EmptySession(t *testing.T) {
	t.Parallel()
	svc := newPromptService(Deps{})
	_, err := svc.Get(context.Background(), "", "planner")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}
