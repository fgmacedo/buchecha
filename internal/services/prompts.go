package services

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/fgmacedo/buchecha/internal/director"
)

// Prompt is the rendered system prompt for one role within a session.
// The Markdown body is whatever the run boot wrote under
// <sessionDir>/prompts/<role>.md (for role-based prompts) or
// <sessionDir>/spawns/<spawn_id>.md (for per-spawn prompts).
// Role is left empty when the prompt is fetched by spawn ID;
// the SPA already knows the role from the originating event.
type Prompt struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role,omitempty"`
	SpawnID   string `json:"spawn_id,omitempty"`
	Markdown  string `json:"markdown"`
}

// validRoles is the closed set the PromptService accepts. Other
// values map to ErrInvalidRequest before any filesystem call.
var validRoles = map[string]bool{
	"planner":  true,
	"briefer":  true,
	"executor": true,
	"reviewer": true,
}

// PromptService reads rendered system prompts from
// .bcc/sessions/<id>/prompts/<role>.md.
type PromptService struct {
	deps Deps
}

func newPromptService(deps Deps) *PromptService {
	return &PromptService{deps: deps}
}

// Get returns the rendered prompt for (sessionID, role). Unknown
// roles return ErrInvalidRequest, unknown sessions return
// ErrSessionNotFound, and missing prompt files return
// ErrRoleNotFound (the session exists but the run boot did not
// materialize a prompt for the role, e.g. trivial phases that opted
// out of the Reviewer agent).
func (s *PromptService) Get(ctx context.Context, sessionID, role string) (Prompt, error) {
	if err := ctx.Err(); err != nil {
		return Prompt{}, err
	}
	if sessionID == "" {
		return Prompt{}, ErrInvalidRequest.WithMessage("prompt service: empty session_id")
	}
	if !validRoles[role] {
		return Prompt{}, ErrInvalidRequest.WithDetails(map[string]any{
			"role":            role,
			"accepted_values": []string{"planner", "briefer", "executor", "reviewer"},
		})
	}
	sessionDir, err := s.sessionDir(sessionID)
	if err != nil {
		return Prompt{}, err
	}
	path := filepath.Join(sessionDir, "prompts", role+".md")
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Prompt{}, ErrRoleNotFound.WithDetails(map[string]any{
				"session_id": sessionID,
				"role":       role,
			})
		}
		return Prompt{}, fmt.Errorf("services: read prompt %s: %w", path, err)
	}
	return Prompt{
		SessionID: sessionID,
		Role:      role,
		Markdown:  string(body),
	}, nil
}

// GetSpawn returns the resolved prompt for a specific spawn by its ID.
// The spawn ID is validated against ULID shape (^[0-9a-z]{16,32}$) before
// any filesystem call. The Role field is left empty since the SPA already
// knows the role from the originating spawn event. Unknown spawns return
// ErrRoleNotFound (the closest existing canonical error); malformed spawn
// IDs return ErrInvalidRequest.
func (s *PromptService) GetSpawn(ctx context.Context, sessionID, spawnID string) (Prompt, error) {
	if err := ctx.Err(); err != nil {
		return Prompt{}, err
	}
	if sessionID == "" {
		return Prompt{}, ErrInvalidRequest.WithMessage("prompt service: empty session_id")
	}
	if spawnID == "" {
		return Prompt{}, ErrInvalidRequest.WithMessage("prompt service: empty spawn_id")
	}
	// Validate spawnID format (ULID-shaped: 16-32 lowercase alphanumeric)
	// before any filesystem access to prevent path traversal.
	validSpawnID := regexp.MustCompile(`^[0-9a-z]{16,32}$`)
	if !validSpawnID.MatchString(spawnID) {
		return Prompt{}, ErrInvalidRequest.WithDetails(map[string]any{
			"spawn_id": spawnID,
			"pattern":  "^[0-9a-z]{16,32}$",
		})
	}
	sessionDir, err := s.sessionDir(sessionID)
	if err != nil {
		return Prompt{}, err
	}
	path := filepath.Join(sessionDir, "spawns", spawnID+".md")
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Prompt{}, ErrRoleNotFound.WithDetails(map[string]any{
				"session_id": sessionID,
				"spawn_id":   spawnID,
			})
		}
		return Prompt{}, fmt.Errorf("services: read spawn prompt %s: %w", path, err)
	}
	return Prompt{
		SessionID: sessionID,
		SpawnID:   spawnID,
		Markdown:  string(body),
	}, nil
}

// sessionDir matches BriefingService.sessionDir: prefer the live
// SessionStore when ids match, fall back to OpenSession otherwise.
func (s *PromptService) sessionDir(sessionID string) (string, error) {
	if s.deps.SessionStore != nil {
		if live := s.deps.SessionStore.Session(); live != nil && live.ID == sessionID {
			return s.deps.SessionStore.SessionDir(), nil
		}
	}
	if s.deps.SessionsBaseDir == "" {
		return "", ErrSessionNotFound.WithDetails(map[string]any{"id": sessionID})
	}
	store, err := director.OpenSession(s.deps.SessionsBaseDir, sessionID)
	if err != nil {
		if errors.Is(err, director.ErrSessionNotFound) || errors.Is(err, fs.ErrNotExist) {
			return "", ErrSessionNotFound.WithDetails(map[string]any{"id": sessionID})
		}
		return "", fmt.Errorf("services: open session %q: %w", sessionID, err)
	}
	return store.SessionDir(), nil
}
