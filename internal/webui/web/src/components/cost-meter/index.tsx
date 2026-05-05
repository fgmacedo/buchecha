import { useState, useEffect, useRef } from 'react'
import type { CostAgg } from '../../hooks/use-cost-aggregator'

export interface CostMeterProps {
  agg: CostAgg
}

/**
 * SparklineChart renders a 24px sparkline showing USD values across iterations.
 */
function SparklineChart({ perIteration }: { perIteration: CostAgg['perIteration'] }) {
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
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="inline-block">
      <path d={pathD} stroke="currentColor" strokeWidth="1" fill="none" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
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
 */
export function CostMeter({ agg }: CostMeterProps) {
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
