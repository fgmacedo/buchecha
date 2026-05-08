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
//
// `selection` is the head of the floating-inspector stack: the most recently
// opened card, used by canvas nodes to render their selected outline.
// `cards` is the full stack, surfaced so the AppShell can render one
// floating Inspector per entry. `closeCard(index)` removes a single card
// without disturbing the others. `select(null)` clears the entire stack
// (Escape key behavior).
export interface SelectionContextValue {
  selection: Selection | null
  cards: Selection[]
  select: (s: Selection | null) => void
  closeCard: (index: number) => void
}

const SelectionContext = createContext<SelectionContextValue | null>(null)

// State held in the reducer: the floating-card stack plus the session id so
// we can reset when the session changes. `cards` is the canonical store;
// derived `selection` is its top element.
interface State {
  cards: Selection[]
  sessionId: string
}

type Action =
  | { type: 'select'; payload: Selection | null }
  | { type: 'close'; index: number }
  | { type: 'session_changed'; sessionId: string }

// sameSelection compares two selections structurally so opening an
// already-open card focuses it (moves to top) instead of duplicating.
function sameSelection(a: Selection, b: Selection): boolean {
  if (a.kind !== b.kind) return false
  switch (a.kind) {
    case 'phase':
      return b.kind === 'phase' && a.phaseId === b.phaseId
    case 'task':
      return b.kind === 'task' && a.phaseId === b.phaseId && a.taskId === b.taskId
    case 'iteration':
      return b.kind === 'iteration' && a.iterationId === b.iterationId
    case 'spawn':
      return b.kind === 'spawn' && a.spawnId === b.spawnId
    case 'agent':
      return (
        b.kind === 'agent' &&
        a.spawnId === b.spawnId &&
        a.subAgentToolUseId === b.subAgentToolUseId
      )
  }
}

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'select': {
      if (action.payload === null) return { ...state, cards: [] }
      const idx = state.cards.findIndex((c) => sameSelection(c, action.payload!))
      if (idx >= 0) {
        // Card already open: move to top so it becomes the head selection.
        const next = state.cards.slice()
        const [picked] = next.splice(idx, 1)
        next.push(picked)
        return { ...state, cards: next }
      }
      return { ...state, cards: [...state.cards, action.payload] }
    }
    case 'close': {
      if (action.index < 0 || action.index >= state.cards.length) return state
      const next = state.cards.slice()
      next.splice(action.index, 1)
      return { ...state, cards: next }
    }
    case 'session_changed':
      if (state.sessionId === action.sessionId) return state
      return { cards: [], sessionId: action.sessionId }
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
  const [state, dispatch] = useReducer(reducer, { cards: [], sessionId })

  // Reset selection whenever the session id changes.
  useEffect(() => {
    dispatch({ type: 'session_changed', sessionId })
  }, [sessionId])

  function select(s: Selection | null): void {
    dispatch({ type: 'select', payload: s })
  }

  function closeCard(index: number): void {
    dispatch({ type: 'close', index })
  }

  const selection = state.cards.length > 0 ? state.cards[state.cards.length - 1] : null

  return createElement(
    SelectionContext.Provider,
    { value: { selection, cards: state.cards, select, closeCard } },
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
