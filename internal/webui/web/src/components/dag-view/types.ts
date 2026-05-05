// DAGState and related types represent the wire shape of the `dag` field
// in the snapshot response. The generated api-client types this as
// Record<string, never> because the OpenAPI schema uses additionalProperties;
// these local types capture the actual runtime shape from dag.MarshalJSON.

// TaskStatus mirrors director.TaskStatus values that the Go DAG layer emits.
export type TaskStatus = 'pending' | 'in_progress' | 'done' | 'needs_fix'

export interface DAGTask {
  id: string
  status: TaskStatus
  depends_on?: string[]
  retry_budget: number
}

export interface DAGPhase {
  id: string
  depends_on?: string[]
  tasks: DAGTask[]
}

// DAGData is the runtime shape of services.Snapshot.DAG after JSON
// deserialisation. The generated client types it as Record<string, never>
// because the schema uses additionalProperties; consumers cast to this type.
export interface DAGData {
  phases: DAGPhase[]
}
