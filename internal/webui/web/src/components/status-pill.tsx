// StatusPill renders the lifecycle status as a colored, mildly tinted
// pill. Single source of styling so phase headers, task footers, and
// inspector chrome match.

export type LifecycleStatus =
  | 'pending'
  | 'in_progress'
  | 'done'
  | 'needs_fix'
  | 'error'

const STATUS_VAR: Record<LifecycleStatus, string> = {
  pending: '--status-pending',
  in_progress: '--status-running',
  done: '--status-done',
  needs_fix: '--status-needs-fix',
  error: '--status-error',
}

const STATUS_LABEL: Record<LifecycleStatus, string> = {
  pending: 'pending',
  in_progress: 'running',
  done: 'done',
  needs_fix: 'needs fix',
  error: 'error',
}

export function statusVar(status: LifecycleStatus): string {
  return `var(${STATUS_VAR[status]})`
}

export function statusLabel(status: LifecycleStatus): string {
  return STATUS_LABEL[status]
}

export type StatusPillSize = 'sm' | 'md'

export function StatusPill({
  status,
  size = 'md',
  pulseLive = false,
}: {
  status: LifecycleStatus
  size?: StatusPillSize
  pulseLive?: boolean
}) {
  const color = statusVar(status)
  const padX = size === 'sm' ? 7 : 9
  const padY = size === 'sm' ? 2 : 3
  const fs = size === 'sm' ? 10 : 10.5
  const dot = size === 'sm' ? 4 : 5
  const live = status === 'in_progress'

  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding: `${padY}px ${padX}px ${padY}px ${padX - 2}px`,
        borderRadius: 999,
        background: `color-mix(in srgb, ${color} 12%, transparent)`,
        border: `1px solid color-mix(in srgb, ${color} 50%, transparent)`,
        color,
        fontSize: fs,
        fontWeight: 500,
        letterSpacing: '0.02em',
        textTransform: 'uppercase',
        flexShrink: 0,
        lineHeight: 1.2,
      }}
    >
      <span
        style={{
          width: dot,
          height: dot,
          borderRadius: '50%',
          background: color,
          color,
          animation: live && pulseLive ? 'bcc-role-pulse 1.6s infinite' : undefined,
        }}
      />
      {STATUS_LABEL[status]}
    </span>
  )
}
