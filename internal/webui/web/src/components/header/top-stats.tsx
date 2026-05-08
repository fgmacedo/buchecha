import { useEffect, useState, type ReactNode } from 'react'

// Metric renders a single label/value pair in the header stats cluster.
// Label is uppercased mono caption; value is mono numerical.
export function Metric({
  label,
  value,
  testId,
}: {
  label: string
  value: ReactNode
  testId?: string
}) {
  return (
    <div
      data-testid={testId}
      className="flex flex-col items-end leading-tight shrink-0"
      style={{ minWidth: 48 }}
    >
      <span
        style={{
          fontSize: 9.5,
          color: 'var(--color-faint, var(--color-muted-foreground))',
          textTransform: 'uppercase',
          letterSpacing: '.08em',
          fontFamily: 'var(--font-mono)',
        }}
      >
        {label}
      </span>
      <span
        style={{
          fontSize: 13,
          fontFamily: 'var(--font-mono)',
          color: 'var(--color-foreground)',
          marginTop: 2,
          whiteSpace: 'nowrap',
        }}
      >
        {value}
      </span>
    </div>
  )
}

// ProgressRing renders a small SVG ring with a centered percentage.
// Used in the header to summarize iteration progress (iter / max_iter).
export function ProgressRing({ pct, size = 36 }: { pct: number; size?: number }) {
  const r = (size - 8) / 2
  const c = 2 * Math.PI * r
  const off = c - (Math.max(0, Math.min(100, pct)) / 100) * c
  const cx = size / 2
  return (
    <div
      style={{ position: 'relative', width: size, height: size, flexShrink: 0 }}
      title={`${Math.round(pct)}% done`}
    >
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        <circle
          cx={cx}
          cy={cx}
          r={r}
          fill="none"
          stroke="var(--border-default)"
          strokeWidth="2.5"
        />
        <circle
          cx={cx}
          cy={cx}
          r={r}
          fill="none"
          stroke="var(--status-running)"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={off}
          transform={`rotate(-90 ${cx} ${cx})`}
          style={{ transition: 'stroke-dashoffset .4s' }}
        />
      </svg>
      <span
        style={{
          position: 'absolute',
          inset: 0,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 10,
          fontFamily: 'var(--font-mono)',
          color: 'var(--color-foreground)',
          fontWeight: 600,
        }}
      >
        {Math.round(pct)}
      </span>
    </div>
  )
}

// formatTokens compresses a token count into "k" units once it crosses 1k.
// Below that, it shows the raw integer so small-run sessions don't lose
// precision (e.g. "812" instead of "0k").
export function formatTokens(total: number): string {
  if (!Number.isFinite(total) || total <= 0) return '0'
  if (total < 1000) return String(total)
  return `${(total / 1000).toFixed(total >= 10_000 ? 0 : 1)}k`
}

// formatElapsed renders milliseconds as "MM:SS" or "HH:MM" once an hour has
// passed, keeping the field width predictable in the header.
export function formatElapsed(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) ms = 0
  const totalSeconds = Math.floor(ms / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) {
    return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}`
  }
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

// formatEta renders an ETA in milliseconds as "≈ Nm left" or "≈ Ns left".
// Returns "—" when the input is null, indicating no ETA can be computed.
export function formatEta(ms: number | null): string {
  if (ms === null || !Number.isFinite(ms) || ms <= 0) return '—'
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `≈ ${seconds}s left`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `≈ ${minutes}m left`
  const hours = Math.floor(minutes / 60)
  return `≈ ${hours}h left`
}

// useElapsed ticks once per second so the elapsed metric updates live.
// Returns elapsed milliseconds since startedAt; 0 when startedAt is missing.
export function useElapsed(startedAt: string | undefined, finishedAt?: string): number {
  const [now, setNow] = useState<number>(() => Date.now())
  const isFinal = !!finishedAt

  useEffect(() => {
    if (isFinal) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [isFinal])

  if (!startedAt) return 0
  const start = Date.parse(startedAt)
  if (!Number.isFinite(start)) return 0
  const end = finishedAt ? Date.parse(finishedAt) : now
  if (!Number.isFinite(end)) return 0
  return Math.max(0, end - start)
}

// computeEta projects the remaining session time from elapsed and progress.
// Returns null when progress is too low (no signal yet) or already complete.
export function computeEta(elapsedMs: number, iter: number, maxIter: number): number | null {
  if (iter <= 0 || maxIter <= 0 || iter >= maxIter) return null
  if (elapsedMs <= 0) return null
  const perIter = elapsedMs / iter
  return Math.max(0, perIter * (maxIter - iter))
}
