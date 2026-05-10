package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fgmacedo/buchecha/internal/loop"
	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/services/events"
	"github.com/fgmacedo/buchecha/internal/supervision/session"
)

// CostAggregate is the per-session cost+tokens summary materialized to
// .bcc/sessions/<id>/cost.json on every SpawnFinished and on session
// finalization. It is also the response shape of GET /sessions/{id}/cost.
//
// TotalTokens carries the same five vendor-neutral buckets as the wire
// TokenUsage. ByRole groups SpawnFinished costs by emitter (planner /
// briefer / executor / reviewer); the breakdown ignores live in-flight
// assistant_text usage because that is not authoritative.
type CostAggregate struct {
	SessionID   string                   `json:"session_id"`
	UpdatedAt   time.Time                `json:"updated_at"`
	Spawns      int                      `json:"spawns"`
	TotalUSD    float64                  `json:"total_usd"`
	TotalTokens agentcontract.TokenUsage `json:"total_tokens"`
	ByRole      []RoleCost               `json:"by_role"`
}

// CostSummary is the smaller projection embedded in SessionMeta so the
// sessions list and sidebar can render a per-session chip in one
// round-trip. The full breakdown lives behind GET /sessions/{id}/cost.
type CostSummary struct {
	TotalUSD    float64                  `json:"total_usd"`
	TotalTokens agentcontract.TokenUsage `json:"total_tokens"`
}

// summary returns the projection embedded into SessionMeta.
func (a CostAggregate) summary() *CostSummary {
	if a.Spawns == 0 && a.TotalUSD == 0 && a.TotalTokens.IsZero() {
		return nil
	}
	return &CostSummary{TotalUSD: a.TotalUSD, TotalTokens: a.TotalTokens}
}

// RoleCost is one row of the by_role breakdown.
type RoleCost struct {
	Role   string                   `json:"role"`
	Spawns int                      `json:"spawns"`
	USD    float64                  `json:"usd"`
	Tokens agentcontract.TokenUsage `json:"tokens"`
}

// costAccumulator is the in-memory aggregator updated by the EventService
// fan-out callback. It is also the source the SessionService.Cost reader
// consults for the live session; archived sessions read the materialized
// cost.json or fall back to walking events.ndjson.
type costAccumulator struct {
	mu          sync.Mutex
	updatedAt   time.Time
	spawns      int
	totalUSD    float64
	totalTokens agentcontract.TokenUsage
	byRole      map[string]*RoleCost
}

func newCostAccumulator() *costAccumulator {
	return &costAccumulator{byRole: make(map[string]*RoleCost)}
}

// observe folds one SpawnFinished into the running totals. Any other
// event kind is a no-op: SpawnFinished is the authoritative carrier of
// per-spawn cost.
func (c *costAccumulator) observe(ev loop.Event) {
	sf, ok := ev.(loop.SpawnFinished)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updatedAt = time.Now().UTC()
	c.spawns++
	c.totalUSD += sf.Cost.USD
	c.totalTokens = c.totalTokens.Add(sf.Cost.Tokens)
	if c.totalTokens.Provider == "" {
		c.totalTokens.Provider = sf.Cost.Tokens.Provider
	}
	role := sf.Role
	if role == "" {
		role = "unknown"
	}
	r, ok := c.byRole[role]
	if !ok {
		r = &RoleCost{Role: role}
		c.byRole[role] = r
	}
	r.Spawns++
	r.USD += sf.Cost.USD
	r.Tokens = r.Tokens.Add(sf.Cost.Tokens)
	if r.Tokens.Provider == "" {
		r.Tokens.Provider = sf.Cost.Tokens.Provider
	}
}

// snapshot returns a deep copy suitable for JSON serialization. Roles
// are sorted alphabetically so the on-disk cost.json is stable across
// equivalent runs.
func (c *costAccumulator) snapshot(sessionID string) CostAggregate {
	c.mu.Lock()
	defer c.mu.Unlock()
	roles := make([]RoleCost, 0, len(c.byRole))
	for _, r := range c.byRole {
		roles = append(roles, *r)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Role < roles[j].Role })
	return CostAggregate{
		SessionID:   sessionID,
		UpdatedAt:   c.updatedAt,
		Spawns:      c.spawns,
		TotalUSD:    c.totalUSD,
		TotalTokens: c.totalTokens,
		ByRole:      roles,
	}
}

// CostMaterializer is the bridge between the live EventService and the
// on-disk cost.json. Construction is hidden behind newCostMaterializer;
// the public surface is the OnSeqEvent callback registered on the
// EventService and the SessionService.Cost reader.
type costMaterializer struct {
	acc       *costAccumulator
	store     *session.Store
	costPath  string
	sessionID string
}

func newCostMaterializer(deps Deps) *costMaterializer {
	m := &costMaterializer{acc: newCostAccumulator(), store: deps.SessionStore}
	if deps.SessionStore != nil {
		if sess := deps.SessionStore.Session(); sess != nil {
			m.sessionID = sess.ID
			m.costPath = filepath.Join(deps.SessionStore.SessionDir(), "cost.json")
		}
	}
	return m
}

// onSeqEvent is registered as the EventService observer. It folds every
// SpawnFinished into the in-memory accumulator and writes cost.json
// atomically; LoopFinished also forces a final write so an archived
// session's cost.json reflects the terminal state even if the last
// SpawnFinished arrived microseconds before LoopFinished.
func (m *costMaterializer) onSeqEvent(se events.SeqEvent) {
	if m == nil {
		return
	}
	switch se.Event.(type) {
	case loop.SpawnFinished, loop.LoopFinished:
	default:
		return
	}
	m.acc.observe(se.Event)
	m.flush()
}

func (m *costMaterializer) flush() {
	if m.costPath == "" {
		return
	}
	agg := m.acc.snapshot(m.sessionID)
	if err := writeCostJSON(m.costPath, agg); err != nil {
		// Materialization is best-effort; the in-memory accumulator stays
		// authoritative for the live session and the archived path falls
		// back to events.ndjson when cost.json is missing.
		// Logging happens at the read site.
		_ = err
	}
}

func writeCostJSON(path string, agg CostAggregate) error {
	body, err := json.MarshalIndent(agg, "", "  ")
	if err != nil {
		return fmt.Errorf("services cost: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "cost-*.json")
	if err != nil {
		return fmt.Errorf("services cost: create tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("services cost: write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("services cost: close tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("services cost: rename: %w", err)
	}
	return nil
}

// Cost returns the per-session cost aggregate. The live session reads
// from the in-memory accumulator (always up to date). Archived sessions
// read .bcc/sessions/<id>/cost.json; if the file is missing (e.g. the
// session crashed before LoopFinished), the reader walks events.ndjson
// to rebuild the aggregate so the API contract still holds.
func (s *SessionService) Cost(ctx context.Context, id string) (CostAggregate, error) {
	if err := ctx.Err(); err != nil {
		return CostAggregate{}, err
	}
	if id == "" {
		return CostAggregate{}, ErrInvalidRequest.WithMessage("session service: empty id")
	}
	if id == LiveSessionAlias {
		if live := s.liveSession(); live != nil {
			id = live.ID
		} else if s.deps.LiveAliasArchivedID != "" {
			id = s.deps.LiveAliasArchivedID
		} else {
			return CostAggregate{}, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
		}
	}
	if live := s.liveSession(); live != nil && live.ID == id && s.cost != nil {
		return s.cost.acc.snapshot(id), nil
	}
	if s.deps.SessionsBaseDir == "" {
		return CostAggregate{}, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
	}
	store, err := session.OpenSession(s.deps.SessionsBaseDir, id)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) || errors.Is(err, fs.ErrNotExist) {
			return CostAggregate{}, ErrSessionNotFound.WithDetails(map[string]any{"id": id})
		}
		return CostAggregate{}, fmt.Errorf("services: cost session %q: %w", id, err)
	}
	costPath := filepath.Join(store.SessionDir(), "cost.json")
	if agg, err := readCostJSON(costPath); err == nil {
		agg.SessionID = id
		return agg, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return CostAggregate{}, fmt.Errorf("services: read cost.json %q: %w", id, err)
	}
	return rebuildCostFromEvents(store.SessionDir(), id)
}

func readCostJSON(path string) (CostAggregate, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return CostAggregate{}, err
	}
	var agg CostAggregate
	if err := json.Unmarshal(body, &agg); err != nil {
		return CostAggregate{}, fmt.Errorf("decode cost.json: %w", err)
	}
	return agg, nil
}

// rebuildCostFromEvents walks the session's events.ndjson and folds
// every SpawnFinished into a fresh accumulator. Used as a fallback
// when cost.json is missing for an archived session.
func rebuildCostFromEvents(sessionDir, id string) (CostAggregate, error) {
	path := filepath.Join(sessionDir, "events.ndjson")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return CostAggregate{SessionID: id}, nil
		}
		return CostAggregate{}, fmt.Errorf("open events.ndjson: %w", err)
	}
	defer func() { _ = f.Close() }()

	acc := newCostAccumulator()
	dec := json.NewDecoder(f)
	for dec.More() {
		var raw struct {
			Event json.RawMessage `json:"event"`
		}
		if err := dec.Decode(&raw); err != nil {
			return CostAggregate{}, fmt.Errorf("decode events.ndjson: %w", err)
		}
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw.Event, &head); err != nil || head.Type != "spawn_finished" {
			continue
		}
		var sf wireSpawnFinished
		if err := json.Unmarshal(raw.Event, &sf); err != nil {
			continue
		}
		acc.observe(sf.toLoopEvent())
	}
	return acc.snapshot(id), nil
}

// wireSpawnFinished mirrors the on-disk shape of a spawn_finished event
// emitted by internal/loop/eventjson.go. Only the fields the cost reader
// needs are typed; everything else is dropped.
type wireSpawnFinished struct {
	Role string `json:"role"`
	Cost struct {
		USD    float64 `json:"usd"`
		Tokens struct {
			InputFresh  int64  `json:"input_fresh"`
			InputCached int64  `json:"input_cached"`
			CacheWrite  int64  `json:"cache_write"`
			Output      int64  `json:"output"`
			Reasoning   int64  `json:"reasoning"`
			Provider    string `json:"provider"`
		} `json:"tokens"`
	} `json:"cost"`
}

func (w wireSpawnFinished) toLoopEvent() loop.SpawnFinished {
	return loop.SpawnFinished{
		Role: w.Role,
		Cost: loop.SpawnCost{
			USD: w.Cost.USD,
			Tokens: agentcontract.TokenUsage{
				InputFresh:  w.Cost.Tokens.InputFresh,
				InputCached: w.Cost.Tokens.InputCached,
				CacheWrite:  w.Cost.Tokens.CacheWrite,
				Output:      w.Cost.Tokens.Output,
				Reasoning:   w.Cost.Tokens.Reasoning,
				Provider:    agentcontract.Provider(w.Cost.Tokens.Provider),
			},
		},
	}
}
