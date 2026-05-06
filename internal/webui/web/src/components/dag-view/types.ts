// DAGState and related types represent the wire shape of the `dag` field
// in the snapshot response. The generated api-client types this as
// Record<string, never> because the OpenAPI schema uses additionalProperties;
// these local types capture the actual runtime shape from dag.MarshalJSON.

// TaskStatus mirrors director.TaskStatus values that the Go DAG layer emits.
export type TaskStatus = 'pending' | 'in_progress' | 'done' | 'needs_fix'

export interface AcceptanceItem {
  id?: string
  text?: string
}

export interface DAGTask {
  id: string
  title?: string
  intent?: string
  status: TaskStatus
  depends_on?: string[]
  priority?: number
  acceptance?: AcceptanceItem[]
  retry_budget: number
}

export interface RoleAssignment {
  provider?: string
  model?: string
  effort?: string
}

export interface DAGPhase {
  id: string
  title?: string
  intent?: string
  depends_on?: string[]
  parallelizable?: boolean
  priority?: number
  scope_in?: string[]
  scope_out?: string[]
  tasks: DAGTask[]
  executor_assignment?: RoleAssignment | null
}

// DAGData is the runtime shape of services.Snapshot.DAG after JSON
// deserialisation. The generated client types it as Record<string, never>
// because the schema uses additionalProperties; consumers cast to this type.
export interface DAGData {
  phases: DAGPhase[]
}
