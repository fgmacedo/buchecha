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

// seedBriefing writes both <sessionDir>/briefings/<iter>.json and
// <sessionDir>/runs/<iter>/briefing.md so the BriefingService has the
// same on-disk shape the loop produces. The mtime of the json file is
// shifted to mtimeOffset so tests can control attempt order
// deterministically.
func seedBriefing(t *testing.T, sessionDir, iterationID, phaseID, markdown string, mtimeOffset time.Duration) {
	t.Helper()
	briefingsDir := filepath.Join(sessionDir, "briefings")
	if err := os.MkdirAll(briefingsDir, 0o755); err != nil {
		t.Fatalf("mkdir briefings: %v", err)
	}
	body := []byte(`{"iteration_id":"` + iterationID + `","phase_id":"` + phaseID + `"}`)
	jsonPath := filepath.Join(briefingsDir, iterationID+".json")
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		t.Fatalf("write briefing json: %v", err)
	}
	at := time.Now().Add(mtimeOffset)
	if err := os.Chtimes(jsonPath, at, at); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	runDir := filepath.Join(sessionDir, "runs", iterationID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "briefing.md"), []byte(markdown), 0o644); err != nil {
		t.Fatalf("write briefing.md: %v", err)
	}
}

func TestBriefingService_Get(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := director.Session{
		ID:        "abcdef000001",
		SpecPath:  "/spec/p.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	sessionDir := filepath.Join(baseDir, "sessions", sess.ID)

	seedBriefing(t, sessionDir, "P1-100", "P1", "# attempt 1\n", -2*time.Second)
	seedBriefing(t, sessionDir, "P1-200", "P1", "# attempt 2\n", -1*time.Second)
	seedBriefing(t, sessionDir, "P2-300", "P2", "# attempt 1 phase2\n", -1*time.Second)

	svc := newBriefingService(Deps{SessionsBaseDir: baseDir})

	cases := []struct {
		name        string
		phaseID     string
		attempt     int
		wantErr     error
		wantMarkdwn string
		wantIter    string
	}{
		{
			name:        "happy path attempt 1",
			phaseID:     "P1",
			attempt:     1,
			wantMarkdwn: "# attempt 1\n",
			wantIter:    "P1-100",
		},
		{
			name:        "happy path attempt 2",
			phaseID:     "P1",
			attempt:     2,
			wantMarkdwn: "# attempt 2\n",
			wantIter:    "P1-200",
		},
		{
			name:    "phase miss",
			phaseID: "P-unknown",
			attempt: 1,
			wantErr: ErrPhaseNotFound,
		},
		{
			name:    "attempt beyond recorded",
			phaseID: "P1",
			attempt: 5,
			wantErr: ErrAttemptNotFound,
		},
		{
			name:    "attempt zero rejected",
			phaseID: "P1",
			attempt: 0,
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "empty phase rejected",
			phaseID: "",
			attempt: 1,
			wantErr: ErrInvalidRequest,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.Get(context.Background(), sess.ID, tc.phaseID, tc.attempt)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Markdown != tc.wantMarkdwn {
				t.Fatalf("Markdown = %q, want %q", got.Markdown, tc.wantMarkdwn)
			}
			if got.IterationID != tc.wantIter {
				t.Fatalf("IterationID = %q, want %q", got.IterationID, tc.wantIter)
			}
			if got.PhaseID != tc.phaseID {
				t.Fatalf("PhaseID = %q", got.PhaseID)
			}
			if got.Attempt != tc.attempt {
				t.Fatalf("Attempt = %d", got.Attempt)
			}
			if got.SessionID != sess.ID {
				t.Fatalf("SessionID = %q", got.SessionID)
			}
		})
	}
}

func TestBriefingService_Get_UnknownSession(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	svc := newBriefingService(Deps{SessionsBaseDir: filepath.Join(tmp, ".bcc")})
	_, err := svc.Get(context.Background(), "000000000000", "P1", 1)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestBriefingService_Get_EmptySession(t *testing.T) {
	t.Parallel()
	svc := newBriefingService(Deps{})
	_, err := svc.Get(context.Background(), "", "P1", 1)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestBriefingService_Get_LiveStorePreferred(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, ".bcc")
	now := time.Now().UTC().Truncate(time.Second)
	sess := director.Session{
		ID:        "010203040506",
		SpecPath:  "/spec/live.md",
		SpecHash:  "h",
		CreatedAt: now,
		UpdatedAt: now,
		Status:    director.SessionRunning,
	}
	writeManifest(t, baseDir, sess)
	store, err := director.OpenSession(baseDir, sess.ID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	seedBriefing(t, store.SessionDir(), "P1-1", "P1", "live\n", 0)

	svc := newBriefingService(Deps{SessionStore: store, SessionsBaseDir: baseDir})
	got, err := svc.Get(context.Background(), sess.ID, "P1", 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Markdown != "live\n" {
		t.Fatalf("Markdown = %q", got.Markdown)
	}
}
