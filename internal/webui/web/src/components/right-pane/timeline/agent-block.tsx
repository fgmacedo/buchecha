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

// formatToolUseDetail extracts a short, tool-aware preview of the
// arguments. The wire format is {tool: {name, args: {...}}}. The header
// badge already shows the tool name, so the detail must NOT repeat it.
function formatToolUseDetail(name: string, args: Record<string, unknown>): string {
  const str = (k: string): string => (typeof args[k] === 'string' ? (args[k] as string) : '')
  switch (name) {
    case 'Write':
    case 'Edit': {
      const path = str('file_path')
      const content = str('content') || str('new_string')
      const size = content ? ` (${content.length} B)` : ''
      return `${path}${size}`
    }
    case 'Read': {
      const path = str('file_path')
      const offset = typeof args.offset === 'number' ? `:${args.offset}` : ''
      return `${path}${offset}`
    }
    case 'Bash':
      return str('command').slice(0, 160)
    case 'Glob':
    case 'Grep':
      return str('pattern') || str('query')
    default: {
      const json = JSON.stringify(args)
      return json.length > 160 ? `${json.slice(0, 160)}...` : json
    }
  }
}

// extractText returns the most salient text string from an agent event.
function extractText(event: SeqEvent['event']): string {
  const kind = typeof event.kind === 'string' ? event.kind : ''
  if (kind === 'tool_use') {
    const tool = event.tool as { name?: string; args?: Record<string, unknown> } | undefined
    const name = tool?.name ?? ''
    const args = tool?.args ?? {}
    return formatToolUseDetail(name, args)
  }
  if (kind === 'tool_result') {
    const tool = event.tool as { summary?: string; is_error?: boolean } | undefined
    if (tool?.is_error) return `error: ${tool.summary ?? ''}`
    return tool?.summary ?? ''
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
    const tool = event.event.tool as { is_error?: boolean; summary?: string } | undefined
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
          {tool?.is_error ? 'error' : tool?.summary?.slice(0, 80) ?? ''}
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
  const headerLabel = toolName || kind

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
        {/* Stacked label + content */}
        <div className="flex-1 min-w-0 flex flex-col gap-0.5">
          <div className="flex items-center gap-2">
            <span
              title={headerLabel}
              className={`shrink min-w-0 truncate text-[10px] font-mono leading-tight ${kindBadgeClass(kind)}`}
            >
              {headerLabel}
            </span>
            {isPaired && isError && (
              <span
                className="shrink-0 text-[10px] font-mono leading-tight"
                style={{ color: 'var(--accent-warn)' }}
              >
                err
              </span>
            )}
          </div>
          <span
            className={`min-w-0 text-xs font-mono text-muted-foreground break-words ${
              expanded ? '' : 'line-clamp-3'
            }`}
          >
            {primaryText || '-'}
          </span>
        </div>

        {/* Chevron + seq column */}
        <div className="shrink-0 flex flex-col items-end gap-0.5 text-[10px] font-mono text-muted-foreground/50">
          <span>{expanded ? '▲' : '▼'}</span>
          <span>#{event.seq}</span>
        </div>
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
