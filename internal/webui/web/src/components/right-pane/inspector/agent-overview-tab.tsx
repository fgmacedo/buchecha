import type { SeqEvent } from '../../../hooks/use-events'
import type { AgentCard } from '../../../hooks/use-agents'
import { useAgents } from '../../../hooks/use-agents'

export interface AgentOverviewTabProps {
  agentId: string
  subAgentToolUseId?: string
  events: SeqEvent[]
}

const ROLE_COLOR: Record<string, string> = {
  planner: 'var(--role-planner)',
  briefer: 'var(--role-briefer)',
  executor: 'var(--role-executor)',
  reviewer: 'var(--role-reviewer)',
}

// AgentOverviewTab renders the static metadata for the selected agent: role,
// model, effort, anchor, lifetime, cost, exit code. When subAgentToolUseId is
// set, the panel narrows to a single sub-agent (Task tool call) child of the
// parent and shows its prompt + summary.
export function AgentOverviewTab({ agentId, subAgentToolUseId, events }: AgentOverviewTabProps) {
  const agents = useAgents(events)
  const card = agents.byId[agentId]

  if (!card) {
    return <NotFound label={`agent ${agentId}`} />
  }

  if (subAgentToolUseId) {
    const sub = card.subAgents[subAgentToolUseId]
    if (!sub) return <NotFound label={`sub-agent ${subAgentToolUseId}`} />
    return (
      <div style={{ height: '100%', overflowY: 'auto', padding: 12, fontFamily: 'var(--font-sans)', fontSize: 12, lineHeight: 1.5 }}>
        <Section title="Sub-agent (Task)">
          <Row label="Type" value={sub.subagentType ?? '(unspecified)'} mono />
          <Row label="Status" value={sub.status} />
          <Row label="Started" value={sub.startedAt} mono />
          {sub.finishedAt && <Row label="Finished" value={sub.finishedAt} mono />}
          {sub.isError && <Row label="Errored" value="yes" mono />}
        </Section>
        {sub.prompt && (
          <Section title="Prompt">
            <Pre>{sub.prompt}</Pre>
          </Section>
        )}
        {sub.summary && (
          <Section title="Summary">
            <Pre>{sub.summary}</Pre>
          </Section>
        )}
      </div>
    )
  }

  return <AgentDetail card={card} />
}

function AgentDetail({ card }: { card: AgentCard }) {
  const color = ROLE_COLOR[card.role]
  const anchorStr =
    card.anchor.kind === 'plan'
      ? 'plan'
      : card.anchor.kind === 'phase'
        ? `phase ${card.anchor.phaseId}`
        : `task ${card.anchor.phaseId} / ${card.anchor.taskId}`
  return (
    <div style={{ padding: 12, fontFamily: 'var(--font-sans)', fontSize: 12, lineHeight: 1.5 }}>
      <Section title="Identity">
        <Row label="Role" value={<RoleChip role={card.role} color={color} />} />
        <Row label="Agent id" value={card.agentId} mono />
        {card.spawnId && <Row label="Spawn id" value={card.spawnId} mono />}
        <Row label="Status" value={card.status} mono />
        <Row label="Anchor" value={anchorStr} mono />
        {card.iterationId && <Row label="Iteration" value={card.iterationId} mono />}
        {typeof card.attempt === 'number' && <Row label="Attempt" value={String(card.attempt)} mono />}
      </Section>

      <Section title="Configuration">
        {card.model && <Row label="Model" value={card.model} mono />}
        {card.effort && <Row label="Effort" value={card.effort} mono />}
      </Section>

      <Section title="Lifetime">
        <Row label="Started" value={card.startedAt} mono />
        {card.finishedAt && <Row label="Finished" value={card.finishedAt} mono />}
        {typeof card.durationMs === 'number' && (
          <Row label="Duration" value={`${(card.durationMs / 1000).toFixed(2)}s`} mono />
        )}
        {typeof card.exitCode === 'number' && (
          <Row label="Exit code" value={String(card.exitCode)} mono />
        )}
        {typeof card.costUSD === 'number' && (
          <Row label="Cost" value={`$${card.costUSD.toFixed(4)}`} mono />
        )}
      </Section>

      {Object.keys(card.subAgents).length > 0 && (
        <Section title="Sub-agents (Task tool)">
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
            {Object.values(card.subAgents).map((sub) => (
              <li
                key={sub.toolUseId}
                style={{
                  border: '1px solid var(--border-default)',
                  borderRadius: 4,
                  padding: '6px 8px',
                  fontSize: 11,
                  fontFamily: 'var(--font-mono)',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 2,
                }}
              >
                <span>
                  <strong>{sub.subagentType ?? 'Task'}</strong>{' '}
                  <span style={{ color: 'var(--color-muted-foreground)' }}>{sub.status}</span>
                </span>
                {sub.prompt && (
                  <span style={{ color: 'var(--color-muted-foreground)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    {sub.prompt.split('\n')[0]}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </Section>
      )}
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 14 }}>
      <div
        style={{
          fontSize: 10,
          textTransform: 'uppercase',
          letterSpacing: '0.06em',
          color: 'var(--color-muted-foreground)',
          marginBottom: 6,
          fontFamily: 'var(--font-mono)',
        }}
      >
        {title}
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>{children}</div>
    </div>
  )
}

function Row({ label, value, mono }: { label: string; value: React.ReactNode; mono?: boolean }) {
  return (
    <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
      <span
        style={{
          minWidth: 92,
          color: 'var(--color-muted-foreground)',
          fontSize: 10,
          flexShrink: 0,
        }}
      >
        {label}
      </span>
      <span
        style={{
          fontFamily: mono ? 'var(--font-mono)' : 'var(--font-sans)',
          color: 'var(--color-foreground)',
          fontSize: 11,
          wordBreak: 'break-word',
        }}
      >
        {value}
      </span>
    </div>
  )
}

function RoleChip({ role, color }: { role: string; color: string }) {
  return (
    <span
      style={{
        backgroundColor: color,
        color: 'var(--surface-canvas)',
        borderRadius: 3,
        padding: '0 6px',
        fontSize: 10,
        fontFamily: 'var(--font-mono)',
        textTransform: 'uppercase',
        letterSpacing: '0.04em',
      }}
    >
      {role}
    </span>
  )
}

function Pre({ children }: { children: string }) {
  return (
    <pre
      style={{
        fontFamily: 'var(--font-mono)',
        fontSize: 11,
        lineHeight: 1.45,
        backgroundColor: 'var(--surface-panel)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 4,
        padding: 8,
        overflow: 'auto',
        maxHeight: 220,
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-word',
      }}
    >
      {children}
    </pre>
  )
}

function NotFound({ label }: { label: string }) {
  return (
    <div
      style={{
        height: '100%',
        overflowY: 'auto',
        padding: 16,
        color: 'var(--color-muted-foreground)',
        fontSize: 12,
        fontFamily: 'var(--font-sans)',
        fontStyle: 'italic',
      }}
    >
      {label} not found in current event stream.
    </div>
  )
}
