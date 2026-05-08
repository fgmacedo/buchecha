import { useState, useEffect, useRef } from 'react'
import type { CostAgg } from '../../hooks/use-cost-aggregator'

export interface CostMeterProps {
  agg: CostAgg
  // compact collapses the meter to the USD pill only, hiding the sparkline
  // and token count. Used below 1024px in the header.
  compact?: boolean
}

/**
 * SparklineChart renders a 24px sparkline showing USD values across iterations.
 * On hover, displays a tooltip with the iteration index and USD amount.
 */
function SparklineChart({ perIteration }: { perIteration: CostAgg['perIteration'] }) {
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)
  const width = 24
  const height = 24
  const padding = 2

  if (perIteration.length === 0) {
    return (
      <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="inline-block">
        <line x1={padding} y1={height - padding} x2={width - padding} y2={height - padding} stroke="currentColor" strokeWidth="1" opacity="0.3" />
      </svg>
    )
  }

  const maxUsd = Math.max(...perIteration.map(i => i.usd), 0.01)
  const points: Array<{ x: number; y: number }> = perIteration.map((iter, idx) => ({
    x: padding + (idx / (perIteration.length - 1 || 1)) * (width - 2 * padding),
    y: height - padding - (iter.usd / maxUsd) * (height - 2 * padding),
  }))

  const pathD = points.length > 1
    ? `M ${points.map(p => `${p.x},${p.y}`).join(' L ')}`
    : `M ${points[0].x},${points[0].y} L ${points[0].x},${points[0].y}`

  return (
    <div className="relative inline-block">
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className="inline-block"
        onMouseLeave={() => setHoveredIdx(null)}
      >
        <path d={pathD} stroke="currentColor" strokeWidth="1" fill="none" strokeLinecap="round" strokeLinejoin="round" />
        {/* Invisible circles for hover detection */}
        {points.map((p, idx) => (
          <g key={idx}>
            <circle
              cx={p.x}
              cy={p.y}
              r="2"
              fill="transparent"
              style={{ cursor: 'pointer' }}
              onMouseEnter={() => setHoveredIdx(idx)}
            />
            <title>{`Iter ${perIteration[idx].iterationIndex}: $${perIteration[idx].usd.toFixed(2)}`}</title>
          </g>
        ))}
      </svg>
      {/* Tooltip shown on hover */}
      {hoveredIdx !== null && (
        <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 bg-surface-elevated text-foreground text-xs px-2 py-1 rounded whitespace-nowrap border border-border-default shadow-md pointer-events-none">
          Iter {perIteration[hoveredIdx].iterationIndex}: ${perIteration[hoveredIdx].usd.toFixed(2)}
        </div>
      )}
    </div>
  )
}

/**
 * Popover is a simple custom popover component using useState and click-outside detection.
 */
function Popover({ isOpen, onClose, anchor, children }: {
  isOpen: boolean
  onClose: () => void
  anchor: React.ReactNode
  children: React.ReactNode
}) {
  const popoverRef = useRef<HTMLDivElement>(null)
  const anchorRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isOpen) return

    function handleClickOutside(event: MouseEvent) {
      if (
        popoverRef.current &&
        !popoverRef.current.contains(event.target as Node) &&
        anchorRef.current &&
        !anchorRef.current.contains(event.target as Node)
      ) {
        onClose()
      }
    }

    function handleEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onClose()
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    document.addEventListener('keydown', handleEscape)

    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [isOpen, onClose])

  return (
    <div className="relative inline-block">
      <div ref={anchorRef}>
        {anchor}
      </div>
      {isOpen && (
        <div
          ref={popoverRef}
          className="absolute top-full right-0 z-50 mt-2 min-w-[280px] rounded border border-border-default bg-surface-panel p-3 shadow-lg"
        >
          {children}
        </div>
      )}
    </div>
  )
}

/**
 * CostMeter displays the total USD and tokens with a sparkline and detailed breakdown.
 * When compact=true, only the USD pill is shown (no sparkline or token count).
 */
export function CostMeter({ agg, compact = false }: CostMeterProps) {
  const [isOpen, setIsOpen] = useState(false)

  const handleToggle = () => {
    setIsOpen(!isOpen)
  }

  const totalTokens = agg.totalTokens.input + agg.totalTokens.output

  return (
    <Popover
      isOpen={isOpen}
      onClose={() => setIsOpen(false)}
      anchor={
        compact ? (
          <button
            onClick={handleToggle}
            className="flex flex-col items-end leading-tight shrink-0 cursor-pointer"
            style={{ minWidth: 48, background: 'transparent', border: 0, padding: 0 }}
            title="Cost breakdown"
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
              cost
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
              <span
                style={{ color: 'var(--color-faint, var(--color-muted-foreground))' }}
              >
                $
              </span>
              {agg.totalUSD.toFixed(2)}
            </span>
          </button>
        ) : (
          <button
            onClick={handleToggle}
            className="flex items-center gap-2 px-3 py-1.5 rounded-full border border-border-default hover:border-border-strong bg-surface-card hover:bg-surface-elevated transition-colors text-xs text-foreground"
            title="Cost breakdown"
          >
            <span className="font-display italic text-sm">${agg.totalUSD.toFixed(2)}</span>
            <span className="font-numeric text-xs text-muted-foreground">{totalTokens}</span>
            <div className="text-muted-foreground">
              <SparklineChart perIteration={agg.perIteration} />
            </div>
          </button>
        )
      }
    >
      <div className="space-y-3">
        {/* Per-role section */}
        <div>
          <h4 className="text-xs font-semibold text-muted-foreground mb-2">By Role</h4>
          <div className="space-y-1.5">
            {Object.entries(agg.perRole).map(([role, data]) => {
              const percentage = agg.totalUSD > 0 ? (data.usd / agg.totalUSD * 100).toFixed(0) : '0'
              return (
                <div key={role} className="flex items-center justify-between text-xs">
                  <div className="flex items-center gap-2 flex-1">
                    <span className="inline-block w-2 h-2 rounded-full" style={{
                      backgroundColor: getRoleColor(role),
                    }} />
                    <span className="text-foreground capitalize flex-1">{role}</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="font-numeric text-muted-foreground">${data.usd.toFixed(3)}</span>
                    <span className="text-muted-foreground w-6 text-right">{percentage}%</span>
                  </div>
                </div>
              )
            })}
          </div>
        </div>

        {/* Divider */}
        {agg.perIteration.length > 0 && (
          <div className="border-t border-border-subtle" />
        )}

        {/* Per-iteration section */}
        {agg.perIteration.length > 0 && (
          <div>
            <h4 className="text-xs font-semibold text-muted-foreground mb-2">By Iteration</h4>
            <div className="space-y-1.5">
              {agg.perIteration.map((iter) => (
                <div key={iter.iterationIndex} className="flex items-center justify-between text-xs">
                  <span className="text-foreground">Iter {iter.iterationIndex}</span>
                  <div className="flex items-center gap-2">
                    <span className="font-numeric text-muted-foreground">${iter.usd.toFixed(3)}</span>
                    <span className="text-muted-foreground font-numeric">{iter.tokens}</span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </Popover>
  )
}

/**
 * getRoleColor returns a color string for the given role.
 */
function getRoleColor(role: string): string {
  const colors: Record<string, string> = {
    planner: '#6b7280',      // gray
    briefer: '#6ea8ff',      // blue
    executor: '#4ade80',     // green
    reviewer: '#f59e0b',     // amber
  }
  return colors[role] ?? '#9ba1a8' // muted-foreground
}
