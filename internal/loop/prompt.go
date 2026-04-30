package loop

import (
	"bytes"
	"fmt"
	"text/template"
)

// PromptInput holds the data passed into prompt templates.
//
// Both heading values must include the leading "## " markers; they are
// inserted verbatim into the prompt text.
type PromptInput struct {
	SpecPath  string
	GuidePath string

	// Extra is an optional block appended at the end of the prompt with a
	// header that frames it as "additional instructions" complementing
	// (not overriding) the guide and spec. Empty omits the block.
	Extra string

	PlanHeading    string
	JournalHeading string
	ResultKeyword  string

	ResultOK      string
	ResultPartial string
	ResultDone    string
	ResultBlocked string
	ResultReview  string
}

const promptLoopTmpl = `You are running in autonomous loop-by-phase mode, controlled by bcc. This invocation implements ONE pending phase and exits.

Spec: {{.SpecPath}}
Guide: {{.GuidePath}} (read it first; pay attention to "Discovered work" and "Stop criteria").

Context from bcc (env vars set in your subprocess):
- BCC_RUNNING=1 confirms you are running under bcc.
- BCC_ITERATION is the 1-based index of this iteration.
- BCC_MAX_ITERATIONS is the cap configured for this run.
- BCC_SPEC_PATH is the absolute path of the spec you are implementing.
- BCC_BRANCH is the current git branch.

Procedure:
1. Read the autonomous-execution guide.
2. Read the spec; for large specs, read selectively: front matter, the "{{.PlanHeading}}" section's headings, the next pending phase in detail, and the most recent entries of the "{{.JournalHeading}}".
3. Identify the next pending phase (the first phase with any [ ] item).
4. Examine the items of that phase BEFORE deciding to implement. If any item is a placeholder waiting on the observer (text like "(placeholder; observer fills...)" or items inside a phase explicitly marked "observer-driven"), set "{{.ResultKeyword}}: {{.ResultReview}}", record what you need from the observer in **Decisions**, and exit. Do NOT invent content for placeholder items.
5. Otherwise, implement the phase end to end: code, tests, lint, small commits, mark [x] in the same commit that delivers each item.
6. Working tree invariants: it is clean when you start; it must be clean when you exit. The journal entry itself goes in the final commit of the iteration.
7. Append a NEW entry at the TOP of the spec's "{{.JournalHeading}}" section following the contract (the **{{.ResultKeyword}}** field on its own line, exact value, no quotes).
8. Exit.

Non-negotiable rules on scope and checkboxes (violations cause rework):
- Do not mark [x] on a partially delivered sub-item. A checked box is a contract that the spec is satisfied at that point.
- If during implementation you discover work the spec covers (in sections like "Components", "URL contract", "API contract", etc.) that does not fit in the current sub-item, you have TWO options, not three: (a) implement now, (b) add a NEW [ ] sub-item to the plan (in the current phase, in an existing future phase, or in a new phase created for it) BEFORE EXITING. There is no "leave for another iteration" option recorded only as prose in **Summary** or **Decisions**: the journal does not transfer scope, only the plan does.
- When adding a new [ ] sub-item, mention it explicitly in **Decisions** or **Problems**: "added sub-item <description> to P<n>".

**{{.ResultKeyword}}** values (strict):
- {{.ResultOK}}: every sub-item of the current phase is [x] AND any discovered work was implemented or became a new [ ] sub-item in a future phase.
- {{.ResultPartial}}: a [ ] sub-item from the current phase remains for the next iteration, or new [ ] sub-items appeared in the current phase.
- {{.ResultDone}}: ZERO [ ] sub-items in the entire plan. The outer loop verifies and aborts (exit 5) if any [ ] remains.
- {{.ResultReview}}: recoverable observer checkpoint. The spec or its protocol asks the human to look and edit before proceeding (e.g., placeholder phase, observer-driven gate). Loop stops with exit 6; observer fills the gap and re-triggers.
- {{.ResultBlocked}}: unrecoverable without a tech fix: technical failure (3 attempts after revert), absolute-restriction temptation, or undocumented ambiguity that needs developer attention. Loop stops with exit 1.

The "**Commits**" field of your journal entry: list every commit by short hash and message. The journal commit itself is necessarily the last one and its hash is unknowable at write time; refer to it as "this commit <message>" or "<HEAD> <message>" — the loop tolerates both.

Implement exactly one phase. Do not advance to the next within this invocation. Do not ask for confirmation.{{if .Extra}}

Additional instructions from the invoker (complement the guide and spec; do not override absolute restrictions):
{{.Extra}}{{end}}
`

const promptSingleShotTmpl = `You are running in autonomous single-shot mode, controlled by bcc. Implement all phases possible in this single session.

Spec: {{.SpecPath}}
Guide: {{.GuidePath}} (read it first).

Context from bcc (env vars in your subprocess): BCC_RUNNING, BCC_ITERATION, BCC_MAX_ITERATIONS, BCC_SPEC_PATH, BCC_BRANCH.

Update the "{{.JournalHeading}}" at every milestone (new entry on TOP, strict format from the guide). Mark [x] in the same commit that delivers each item. Working tree must be clean when you exit. Do not ask for confirmation. Stop when a stop criterion is met.{{if .Extra}}

Additional instructions from the invoker (complement the guide and spec; do not override absolute restrictions):
{{.Extra}}{{end}}
`

var (
	promptLoopT       = template.Must(template.New("loop").Parse(promptLoopTmpl))
	promptSingleShotT = template.Must(template.New("singleshot").Parse(promptSingleShotTmpl))
)

// BuildPromptLoop renders the per-iteration prompt for loop mode.
func BuildPromptLoop(in PromptInput) (string, error) {
	var buf bytes.Buffer
	if err := promptLoopT.Execute(&buf, in); err != nil {
		return "", fmt.Errorf("render loop prompt: %w", err)
	}
	return buf.String(), nil
}

// BuildPromptSingleShot renders the prompt for single-shot mode.
func BuildPromptSingleShot(in PromptInput) (string, error) {
	var buf bytes.Buffer
	if err := promptSingleShotT.Execute(&buf, in); err != nil {
		return "", fmt.Errorf("render single-shot prompt: %w", err)
	}
	return buf.String(), nil
}
