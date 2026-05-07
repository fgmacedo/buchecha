import {
  createContext,
  useContext,
  useReducer,
  useEffect,
  createElement,
  type ReactNode,
  type ReactElement,
} from 'react'

// Selection is the discriminated union of selectable node types in the SPA.
// Shared across DAGView, ActivityView, and the Inspector via the
// SelectionProvider context.
export type Selection =
  | { kind: 'phase'; phaseId: string }
  | { kind: 'task'; phaseId: string; taskId: string }
  | { kind: 'iteration'; iterationId: string }
  | { kind: 'spawn'; spawnId: string; role: string; phaseId?: string }
  // Agent: a live or recently finished agent on the canvas. The optional
  // subAgentToolUseId narrows the inspector to a single Task tool call
  // child of that agent.
  | { kind: 'agent'; spawnId: string; subAgentToolUseId?: string }

// SelectionContextValue is the shape returned by useSelection.
export interface SelectionContextValue {
  selection: Selection | null
  select: (s: Selection | null) => void
}

const SelectionContext = createContext<SelectionContextValue | null>(null)

// State held in the reducer: the current selection plus the session id so
// we can reset when the session changes.
interface State {
  selection: Selection | null
  sessionId: string
}

type Action =
  | { type: 'select'; payload: Selection | null }
  | { type: 'session_changed'; sessionId: string }

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'select':
      return { ...state, selection: action.payload }
    case 'session_changed':
      // No-op when the session id has not actually changed.
      if (state.sessionId === action.sessionId) return state
      return { selection: null, sessionId: action.sessionId }
    default:
      return state
  }
}

// SelectionProviderProps exposes sessionId so the provider can reset its
// selection when the active session changes (e.g. the user navigates between
// archived sessions).
export interface SelectionProviderProps {
  sessionId: string
  children?: ReactNode
}

// SelectionProvider is the context root for the selection state. Mount it
// once in AppShell around the entire layout so all consumers share the same
// selection.
export function SelectionProvider({ sessionId, children }: SelectionProviderProps): ReactElement {
  const [state, dispatch] = useReducer(reducer, { selection: null, sessionId })

  // Reset selection whenever the session id changes.
  useEffect(() => {
    dispatch({ type: 'session_changed', sessionId })
  }, [sessionId])

  function select(s: Selection | null): void {
    dispatch({ type: 'select', payload: s })
  }

  return createElement(
    SelectionContext.Provider,
    { value: { selection: state.selection, select } },
    children,
  )
}

// useSelection returns the current selection and a setter. Must be called
// inside a SelectionProvider.
export function useSelection(): SelectionContextValue {
  const ctx = useContext(SelectionContext)
  if (!ctx) {
    throw new Error('useSelection must be used within a SelectionProvider')
  }
  return ctx
}
