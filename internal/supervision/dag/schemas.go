package dag

import (
	"bytes"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/fgmacedo/buchecha/internal/supervision"
)

// methodSchemaFile maps each MCP method name to the embedded JSON
// Schema file (relative to internal/supervision/schemas/mcp/) that
// validates its input. The Plan body inside bcc_plan_emit is checked
// separately against the existing Plan schema in internal/supervision.
var methodSchemaFile = map[string]string{
	MethodPlanEmit:          "schemas/mcp/bcc_plan_emit.schema.json",
	MethodPlanSkip:          "schemas/mcp/bcc_plan_skip.schema.json",
	MethodBriefingEmit:      "schemas/mcp/bcc_briefing_emit.schema.json",
	MethodGetDAGSnapshot:    "schemas/mcp/bcc_get_dag_snapshot.schema.json",
	MethodGetBriefing:       "schemas/mcp/bcc_get_briefing.schema.json",
	MethodGetPendingTasks:   "schemas/mcp/bcc_get_pending_tasks.schema.json",
	MethodTaskStarted:       "schemas/mcp/bcc_task_started.schema.json",
	MethodTaskCompleted:     "schemas/mcp/bcc_task_completed.schema.json",
	MethodTaskApproved:      "schemas/mcp/bcc_task_approved.schema.json",
	MethodTaskNeedsFix:      "schemas/mcp/bcc_task_needs_fix.schema.json",
	MethodIterationFinished: "schemas/mcp/bcc_iteration_finished.schema.json",
	MethodReviewFinished:    "schemas/mcp/bcc_review_finished.schema.json",
	MethodGetBaseline:       "schemas/mcp/bcc_get_baseline.schema.json",
	MethodGetJournalDelta:   "schemas/mcp/bcc_get_journal_delta.schema.json",
}

// compileMethodSchemas reads every per-method schema embedded in the
// director package, compiles them, and returns a method-name lookup the
// handler consults at dispatch time. The plan-emit case also compiles
// the Plan schema as a separate entry under planSchemaKey so the
// handler can validate the nested plan body.
func compileMethodSchemas() (map[string]*jsonschema.Schema, error) {
	fs := supervision.MCPSchemaFS()
	c := jsonschema.NewCompiler()
	out := make(map[string]*jsonschema.Schema, len(methodSchemaFile)+1)
	for method, path := range methodSchemaFile {
		body, err := fs.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("dag: read schema %s: %w", path, err)
		}
		raw, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("dag: parse schema %s: %w", path, err)
		}
		uri := "bcc:///mcp/" + method
		if err := c.AddResource(uri, raw); err != nil {
			return nil, fmt.Errorf("dag: register schema %s: %w", path, err)
		}
		sch, err := c.Compile(uri)
		if err != nil {
			return nil, fmt.Errorf("dag: compile schema %s: %w", path, err)
		}
		out[method] = sch
	}

	planRaw, err := jsonschema.UnmarshalJSON(bytes.NewReader(supervision.PlanSchema()))
	if err != nil {
		return nil, fmt.Errorf("dag: parse plan schema: %w", err)
	}
	const planURI = "bcc:///plan"
	if err := c.AddResource(planURI, planRaw); err != nil {
		return nil, fmt.Errorf("dag: register plan schema: %w", err)
	}
	planSchema, err := c.Compile(planURI)
	if err != nil {
		return nil, fmt.Errorf("dag: compile plan schema: %w", err)
	}
	out[planSchemaKey] = planSchema

	return out, nil
}

// planSchemaKey is the lookup key the handler uses to find the compiled
// Plan schema in the methodSchemas map. It is not a method name; using
// a dedicated key keeps the namespace clean.
const planSchemaKey = "_plan_body"
