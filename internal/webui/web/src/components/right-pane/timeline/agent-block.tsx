import { useState } from 'react'
import type { SeqEvent } from '../../../hooks/use-events'

// AgentEventKind is the discriminator inside an agent_event payload.
type AgentEventKind =
  | 'init'
  | 'thinking'
  | 'assistant_text'
  | 'tool_use'
  | 'tool_result'
  | 'rate_limit'
  | 'result_summary'
  | string

// kindBadgeClass returns a text-color class for each agent event sub-kind.
function kindBadgeClass(kind: AgentEventKind): string {
  switch (kind) {
    case 'tool_use':
      return 'text-purple-400'
    case 'tool_result':
      return 'text-purple-300'
    case 'thinking':
      return 'text-blue-400'
    case 'assistant_text':
      return 'text-foreground'
    case 'result_summary':
      return 'text-yellow-400'
    case 'rate_limit':
      return 'text-orange-400'
    case 'init':
      return 'text-green-400'
    default:
      return 'text-muted-foreground'
  }
}

// extractText returns the most salient text string from an agent event.
function extractText(event: SeqEvent['event']): string {
  const kind = typeof event.kind === 'string' ? event.kind : ''
  if (kind === 'tool_use') {
    const tool = event.tool as { name?: string; input?: unknown } | undefined
    const inputPreview = tool?.input
      ? JSON.stringify(tool.input).slice(0, 120)
      : ''
    return tool?.name ? `${tool.name} ${inputPreview}` : inputPreview
  }
  if (kind === 'tool_result') {
    const tool = event.tool as { content?: string; is_error?: boolean } | undefined
    if (tool?.is_error) return `error: ${tool.content ?? ''}`
    return tool?.content ?? ''
  }
  if (kind === 'thinking' || kind === 'assistant_text') {
    return typeof event.text === 'string' ? event.text : ''
  }
  if (kind === 'result_summary') {
    const usd =
      typeof event.total_cost_usd === 'number'
        ? `$${event.total_cost_usd.toFixed(4)}`
        : ''
    const inp = typeof event.input_tokens === 'number' ? `in:${event.input_tokens}` : ''
    const out = typeof event.output_tokens === 'number' ? `out:${event.output_tokens}` : ''
    return [usd, inp, out].filter(Boolean).join('  ')
  }
  return ''
}

export interface AgentBlockProps {
  event: SeqEvent
  // pairedResult is the tool_result SeqEvent whose tool_use_id matches this
  // event's tool_use_id. When present the two are rendered as one collapsible
  // block. Supplied by the parent timeline list.
  pairedResult?: SeqEvent
}

// AgentBlock renders a single agent_event entry, with internal
// discrimination on the event's `kind` field. For tool_use events that have
// a pairedResult the two are merged into one collapsible block keyed by the
// shared tool_use_id.
//
// Long text is collapsed to three lines; an expand chevron reveals the rest.
export function AgentBlock({ event, pairedResult }: AgentBlockProps) {
  const [expanded, setExpanded] = useState(false)

  const kind: AgentEventKind =
    typeof event.event.kind === 'string' ? event.event.kind : 'agent_event'

  // Skip rendering standalone tool_result entries that have already been
  // consumed by their paired tool_use block (the parent timeline list skips
  // them by not calling this component for already-paired results, but we
  // add a guard here too).
  if (kind === 'tool_result' && !pairedResult) {
    // Render a minimal line so the event is still visible if the parent did
    // not skip it (e.g. orphan tool_result with no matching tool_use).
    const tool = event.event.tool as { is_error?: boolean; content?: string } | undefined
    return (
      <div
        data-testid="agent-block"
        data-kind="tool_result"
        className="flex items-center gap-2 px-4 py-1.5 border-b border-border last:border-0"
      >
        <span className={`shrink-0 text-[10px] font-mono ${kindBadgeClass('tool_result')}`}>
          tool_result
        </span>
        <span className="flex-1 min-w-0 text-xs font-mono text-muted-foreground truncate">
          {tool?.is_error ? 'error' : tool?.content?.slice(0, 80) ?? ''}
        </span>
        <span className="shrink-0 text-[10px] font-mono text-muted-foreground/50">
          #{event.seq}
        </span>
      </div>
    )
  }

  const primaryText = extractText(event.event)
  const resultText = pairedResult ? extractText(pairedResult.event) : ''
  const toolName =
    kind === 'tool_use'
      ? ((event.event.tool as { name?: string } | undefined)?.name ?? '')
      : ''
  const isError =
    pairedResult
      ? !!((pairedResult.event.tool as { is_error?: boolean } | undefined)?.is_error)
      : false

  const isPaired = pairedResult !== undefined
  const headerLabel = isPaired
    ? toolName || kind
    : kind

  return (
    <div
      data-testid="agent-block"
      data-kind={kind}
      className="border-b border-border last:border-0"
      style={{ backgroundColor: isPaired ? 'var(--surface-card)' : undefined }}
    >
      <button
        type="button"
        className="w-full flex items-start gap-2 px-4 py-1.5 text-left hover:bg-border/20 transition-colors"
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
      >
        {/* Kind / tool-name badge */}
        <span className={`shrink-0 text-[10px] font-mono leading-tight mt-0.5 ${kindBadgeClass(kind)}`}>
          {headerLabel}
        </span>

        {/* Error indicator for paired tool results */}
        {isPaired && isError && (
          <span
            className="shrink-0 text-[10px] font-mono leading-tight mt-0.5"
            style={{ color: 'var(--accent-warn)' }}
          >
            err
          </span>
        )}

        {/* Primary text preview, collapsed to 3 lines */}
        <span
          className={`flex-1 min-w-0 text-xs font-mono text-muted-foreground break-words ${
            expanded ? '' : 'line-clamp-3'
          }`}
        >
          {primaryText || '—'}
        </span>

        {/* Chevron */}
        <span className="shrink-0 text-[10px] font-mono text-muted-foreground/50 mt-0.5">
          {expanded ? '▲' : '▼'}
        </span>

        {/* Seq */}
        <span className="shrink-0 text-[10px] font-mono text-muted-foreground/50 mt-0.5">
          #{event.seq}
        </span>
      </button>

      {expanded && (
        <div className="px-4 pb-3 pt-1 space-y-2">
          {primaryText && (
            <pre className="text-xs font-mono text-foreground whitespace-pre-wrap break-words">
              {primaryText}
            </pre>
          )}
          {isPaired && resultText && (
            <div>
              <span
                className={`text-[10px] font-mono ${isError ? 'text-red-400' : 'text-purple-300'}`}
              >
                result
              </span>
              <pre className="text-xs font-mono text-muted-foreground whitespace-pre-wrap break-words mt-1">
                {resultText}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
