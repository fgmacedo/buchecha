package render

import (
	"bytes"
	"fmt"

	"github.com/fgmacedo/buchecha/internal/loop/agentcontract"
	"github.com/fgmacedo/buchecha/internal/supervision"
)

// Type aliases so callers and tests in this package can reference
// supervision types without qualification.
type (
	Briefing       = supervision.Briefing
	Phase          = supervision.Phase
	Task           = supervision.Task
	AcceptanceItem = supervision.AcceptanceItem
	EvidenceKind   = supervision.EvidenceKind
)

// Constants forwarded from supervision so tests in this package can use
// short names.
const (
	EvidenceTest = supervision.EvidenceTest
	EvidenceDiff = supervision.EvidenceDiff
	TaskPending  = supervision.TaskPending
)

// briefingView is the flat field set the briefing template consumes.
// Defining it explicitly (instead of feeding the template Briefing +
// Phase as is) keeps the template stable when either type grows new
// fields the template does not surface.
//
// Feedback maps a task id to the Reviewer's per-task note from the
// previous attempt's task_needs_fix call. The template renders the
// note inline under the task block when present. RetryNotice is the
// flag the template uses to print the "narrowed sub-DAG" banner above
// the task list when the iteration is a retry; it is set by callers
// alongside Feedback and Tasks.
type briefingView struct {
	IterationID   string
	PhaseID       string
	Title         string
	Intent        string
	ScopeIn       []string
	ScopeOut      []string
	Tasks         []Task
	SpecPath      string
	Instructions  string
	PriorFeedback string
	Hint          string
	Feedback      map[string]string
	RetryNotice   bool
}

// FeedbackFor returns the Reviewer's per-task note for the given task
// id, or empty when no note was recorded. Exposed as a method so the
// template can look up entries without using the map index helper.
func (v briefingView) FeedbackFor(taskID string) string {
	if v.Feedback == nil {
		return ""
	}
	return v.Feedback[taskID]
}

// RenderBriefingSystem returns the Executor's system prompt: the bcc
// contract (wire_protocol, absolute_restrictions, working_tree) framed
// as the durable rules every iteration must obey, plus the per-spawn
// agent_id the Executor must pass on every MCP call. The agent_id is
// the only per-spawn input; the rest of the prompt is stable across
// iterations. The contract sections are concatenated, never
// substituted; a render that omits one would relax the contract.
func RenderBriefingSystem(agentID string) (string, error) {
	if agentID == "" {
		return "", fmt.Errorf("director: RenderBriefingSystem: empty agent_id")
	}
	t := agentcontract.Partials()
	if _, err := t.New("briefing_system").Parse(briefingSystemMD); err != nil {
		return "", fmt.Errorf("director: parse briefing_system template: %w", err)
	}
	var buf bytes.Buffer
	view := struct {
		Role    string
		AgentID string
	}{Role: "executor", AgentID: agentID}
	if err := t.ExecuteTemplate(&buf, "briefing_system", view); err != nil {
		return "", fmt.Errorf("director: render briefing_system: %w", err)
	}
	return buf.String(), nil
}

// RenderBriefingUser composes the per-iteration user prompt for one
// iteration's sub-DAG slice: header, scope, per-task acceptance, spec
// path, briefer instructions, optional user-hint block (escalation
// resume), optional prior feedback. The contract sections live in the
// system prompt (see RenderBriefingSystem) so the contract stays stable
// across iterations while this body changes per iteration.
//
// hint is the free-form text the user attached to an EscalationResume
// reply. When non-empty, render prepends a "User hint (escalation)"
// block so the Executor sees the user's correction before the
// reviewer-derived prior feedback.
//
// This is the attempt-1 entry point: the rendered prompt covers the
// briefing's full SubDAGTaskIDs and carries no per-task feedback. The
// loop driver calls RenderBriefingUserView on retry to narrow the task
// list and inject the Reviewer's per-task notes.
func RenderBriefingUser(b *Briefing, p *Phase, hint string) (string, error) {
	return RenderBriefingUserView(b, p, hint, nil, nil)
}

// RenderBriefingUserView is the retry-aware variant of
// RenderBriefingUser. taskIDsOverride, when non-nil, replaces the
// briefing's SubDAGTaskIDs as the set of tasks rendered; pass it to
// narrow the prompt to the still-incomplete tasks on retry. feedback
// maps task id to the Reviewer's per-task note from the previous
// attempt; the template renders each note inline under its task. Both
// arguments may be nil to reproduce the attempt-1 behavior.
func RenderBriefingUserView(b *Briefing, p *Phase, hint string, taskIDsOverride []string, feedback map[string]string) (string, error) {
	if b == nil {
		return "", fmt.Errorf("director: RenderBriefingUser: nil briefing")
	}
	if p == nil {
		return "", fmt.Errorf("director: RenderBriefingUser: nil phase")
	}
	if b.PhaseID != p.ID {
		return "", fmt.Errorf("director: RenderBriefingUser: briefing phase %q != phase %q", b.PhaseID, p.ID)
	}

	t := agentcontract.Partials()
	if _, err := t.New("briefing").Parse(briefingPromptMD); err != nil {
		return "", fmt.Errorf("director: parse briefing template: %w", err)
	}

	taskIDs := taskIDsOverride
	if taskIDs == nil {
		taskIDs = b.SubDAGTaskIDs
	}
	tasks := selectTasks(p, taskIDs)
	priorFeedback := ""
	if b.PriorFeedback != nil {
		priorFeedback = *b.PriorFeedback
	}

	view := briefingView{
		IterationID:   b.IterationID,
		PhaseID:       b.PhaseID,
		Title:         p.Title,
		Intent:        p.Intent,
		ScopeIn:       p.ScopeIn,
		ScopeOut:      p.ScopeOut,
		Tasks:         tasks,
		SpecPath:      b.SpecPath,
		Instructions:  b.Instructions,
		PriorFeedback: priorFeedback,
		Hint:          hint,
		Feedback:      feedback,
		RetryNotice:   len(feedback) > 0,
	}

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "briefing", view); err != nil {
		return "", fmt.Errorf("director: render briefing: %w", err)
	}
	return buf.String(), nil
}

// selectTasks returns the tasks of phase p whose ids appear in ids.
// When ids is empty, every task in the phase is returned. The order
// follows the phase's task slice so the rendered prompt is stable.
func selectTasks(p *Phase, ids []string) []Task {
	if len(ids) == 0 {
		return p.Tasks
	}
	keep := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		keep[id] = struct{}{}
	}
	out := make([]Task, 0, len(ids))
	for _, t := range p.Tasks {
		if _, ok := keep[t.ID]; ok {
			out = append(out, t)
		}
	}
	return out
}
