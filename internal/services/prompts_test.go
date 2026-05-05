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

// seedSpawnPrompt writes <sessionDir>/spawns/<spawn_id>.md with the
// supplied body so PromptService.GetSpawn has the same on-disk shape
// the executor/director writes before subprocess launch.
func seedSpawnPrompt(t *testing.T, sessionDir, spawnID, body string) {
	t.Helper()
	dir := filepath.Join(sessionDir, "spawns")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir spawns: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, spawnID+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write spawn prompt: %v", err)
	}
}

func TestPromptService_GetSpawn(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := director.Session{
		ID:        "aabbccdd1111",
		SpecPath:  "/spec/spawn.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	sessionDir := filepath.Join(baseDir, "sessions", sess.ID)

	// Seed two valid spawns with different content.
	spawn1ID := "0123456789abcdef"
	spawn2ID := "fedcba9876543210"
	seedSpawnPrompt(t, sessionDir, spawn1ID, "# executor spawn 1\n")
	seedSpawnPrompt(t, sessionDir, spawn2ID, "# briefer spawn 2\n")

	svc := newPromptService(Deps{SessionsBaseDir: baseDir})

	cases := []struct {
		name     string
		spawnID  string
		wantBody string
		wantErr  error
	}{
		{name: "happy path spawn 1", spawnID: spawn1ID, wantBody: "# executor spawn 1\n"},
		{name: "happy path spawn 2", spawnID: spawn2ID, wantBody: "# briefer spawn 2\n"},
		{name: "malformed spawn id (too short)", spawnID: "short", wantErr: ErrInvalidRequest},
		{name: "malformed spawn id (uppercase)", spawnID: "0123456789ABCDEF", wantErr: ErrInvalidRequest},
		{name: "malformed spawn id (special chars)", spawnID: "0123456789ab@def!", wantErr: ErrInvalidRequest},
		{name: "unknown spawn id", spawnID: "99999999999999aa", wantErr: ErrRoleNotFound},
		{name: "empty spawn id", spawnID: "", wantErr: ErrInvalidRequest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.GetSpawn(context.Background(), sess.ID, tc.spawnID)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetSpawn: %v", err)
			}
			if got.Markdown != tc.wantBody {
				t.Fatalf("Markdown = %q, want %q", got.Markdown, tc.wantBody)
			}
			if got.SpawnID != tc.spawnID {
				t.Fatalf("SpawnID = %q, want %q", got.SpawnID, tc.spawnID)
			}
			if got.SessionID != sess.ID {
				t.Fatalf("SessionID = %q, want %q", got.SessionID, sess.ID)
			}
			if got.Role != "" {
				t.Fatalf("Role should be empty for spawn prompts, got %q", got.Role)
			}
		})
	}
}

func TestPromptService_GetSpawn_UnknownSession(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	svc := newPromptService(Deps{SessionsBaseDir: filepath.Join(tmp, ".bcc")})
	_, err := svc.GetSpawn(context.Background(), "000000000000", "0123456789abcdef")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}
