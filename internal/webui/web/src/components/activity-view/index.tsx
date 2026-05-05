import { useMemo, useState, useRef, useEffect } from 'react'
import { scaleTime, scaleBand, type ScaleTime } from 'd3-scale'
import { axisBottom } from 'd3-axis'
import { select, type Selection } from 'd3-selection'
import type { SeqEvent } from '../../hooks/use-events'
import type { Snapshot } from '../../hooks/use-snapshot'
import { computeGanttData } from './compute-bars'
import type { Bar, GanttData } from './types'

// Margin around the SVG plot area.
const MARGIN = { top: 16, right: 24, bottom: 36, left: 140 }
const ROW_PAD = 0.2
const BAR_RADIUS = 3

// BAR_STATUS_COLORS maps bar status to CSS variable names from tokens.css.
const BAR_STATUS_COLORS: Record<string, string> = {
  completed: 'var(--status-done)',
  approved: 'var(--status-done)',
  needs_fix: 'var(--status-needs-fix)',
  running: 'var(--status-running)',
}

function barColor(status: string): string {
  return BAR_STATUS_COLORS[status] ?? 'var(--status-pending)'
}

// formatMs converts a millisecond duration to a human-readable string.
function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  const s = Math.round(ms / 100) / 10
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  const rem = Math.round(s % 60)
  return `${m}m ${rem}s`
}

// formatTime formats an epoch-ms value as a short clock string.
function formatTime(ms: number): string {
  return new Date(ms).toLocaleTimeString('en', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

interface TooltipState {
  x: number
  y: number
  bar: Bar
}

interface XAxisProps {
  scale: ScaleTime<number, number>
  height: number
}

// XAxis renders the time axis using d3-axis for tick generation and
// label formatting, then appends the resulting SVG into a <g> ref.
function XAxis({ scale, height }: XAxisProps) {
  const ref = useRef<SVGGElement>(null)

  useEffect(() => {
    if (!ref.current) return
    const axis = axisBottom(scale)
      .ticks(6)
      .tickFormat((d) => {
        const ms = typeof d === 'number' ? d : (d as Date).getTime()
        return formatTime(ms)
      })
    select(ref.current).call(axis as (selection: Selection<SVGGElement, unknown, null, undefined>) => void)

    // Apply design-token colours to the generated axis marks.
    select(ref.current)
      .selectAll('text')
      .attr('fill', 'var(--color-muted-foreground)')
      .attr('font-size', '10')
      .attr('font-family', 'var(--font-mono)')

    select(ref.current)
      .selectAll('line, path')
      .attr('stroke', 'var(--color-border)')
  }, [scale])

  return <g ref={ref} transform={`translate(0, ${height})`} />
}

interface GanttPlotProps {
  data: GanttData
  width: number
  height: number
  snapshot: Snapshot | null
  onHover: (state: TooltipState | null) => void
}

// GanttPlot renders the SVG plot area: phase lanes, bars, iteration
// boundary rules, retry markers, and the time axis.
function GanttPlot({ data, width, height, snapshot, onHover }: GanttPlotProps) {
  const plotW = width - MARGIN.left - MARGIN.right
  const plotH = height - MARGIN.top - MARGIN.bottom

  // Collect unique phase ids in document order when available.
  const phaseIds = useMemo(() => {
    // Use snapshot phase order if available; fall back to event order.
    const dag = snapshot?.dag as unknown as { phases?: Array<{ id: string }> } | null
    if (dag?.phases?.length) {
      return dag.phases.map((p) => p.id)
    }
    const seen = new Set<string>()
    for (const bar of data.bars) {
      seen.add(bar.phaseId)
    }
    return Array.from(seen)
  }, [data.bars, snapshot])

  const xScale = useMemo(
    () =>
      scaleTime()
        .domain([new Date(data.minMs), new Date(data.maxMs)])
        .range([0, plotW]),
    [data.minMs, data.maxMs, plotW],
  )

  const yScale = useMemo(
    () =>
      scaleBand<string>()
        .domain(phaseIds)
        .range([0, plotH])
        .padding(ROW_PAD),
    [phaseIds, plotH],
  )

  const bandwidth = yScale.bandwidth()

  return (
    <svg
      width={width}
      height={height}
      style={{ display: 'block', overflow: 'visible' }}
      aria-label="Activity Gantt chart"
    >
      <g transform={`translate(${MARGIN.left}, ${MARGIN.top})`}>
        {/* Phase lane labels and background bands */}
        {phaseIds.map((phaseId, i) => {
          const y = yScale(phaseId) ?? 0
          return (
            <g key={phaseId}>
              <rect
                x={0}
                y={y}
                width={plotW}
                height={bandwidth}
                fill={i % 2 === 0 ? 'rgba(255,255,255,0.02)' : 'transparent'}
              />
              <text
                x={-10}
                y={y + bandwidth / 2}
                textAnchor="end"
                dominantBaseline="middle"
                fontSize={10}
                fontFamily="var(--font-mono)"
                fill="var(--color-muted-foreground)"
              >
                {phaseId}
              </text>
            </g>
          )
        })}

        {/* Iteration boundary rules */}
        {data.boundaries.map((b, i) => (
          <line
            key={i}
            x1={xScale(new Date(b.ms))}
            x2={xScale(new Date(b.ms))}
            y1={0}
            y2={plotH}
            stroke={
              b.kind === 'start'
                ? 'var(--color-accent)'
                : 'var(--color-border)'
            }
            strokeOpacity={0.3}
            strokeWidth={1}
            strokeDasharray={b.kind === 'end' ? '3,3' : undefined}
          />
        ))}

        {/* Task bars */}
        {data.bars.map((bar, i) => {
          const x = xScale(new Date(bar.startMs))
          const endMs = bar.endMs ?? data.maxMs
          const barW = Math.max(xScale(new Date(endMs)) - x, 2)
          const y = (yScale(bar.phaseId) ?? 0) + bandwidth * 0.1
          const bh = bandwidth * 0.8
          const color = barColor(bar.status)

          return (
            <rect
              key={i}
              x={x}
              y={y}
              width={barW}
              height={bh}
              rx={BAR_RADIUS}
              ry={BAR_RADIUS}
              fill={color}
              fillOpacity={bar.status === 'running' ? 0.5 : 0.7}
              stroke={color}
              strokeWidth={1}
              strokeOpacity={0.9}
              style={{ cursor: 'pointer' }}
              onMouseEnter={(e) => {
                const svgEl = (e.target as SVGElement).ownerSVGElement
                if (!svgEl) return
                const rect = svgEl.getBoundingClientRect()
                onHover({
                  x: e.clientX - rect.left,
                  y: e.clientY - rect.top - 10,
                  bar,
                })
              }}
              onMouseLeave={() => onHover(null)}
            />
          )
        })}

        {/* Retry markers: vertical ticks at task_needs_fix timestamps that
            preceded a retry (another task_started for the same task). */}
        {data.retryMarkers.map((rm, i) => {
          const x = xScale(new Date(rm.ms))
          const y = yScale(rm.phaseId) ?? 0
          return (
            <line
              key={i}
              x1={x}
              x2={x}
              y1={y}
              y2={y + bandwidth}
              stroke="var(--status-needs-fix)"
              strokeWidth={1.5}
              strokeOpacity={0.7}
            />
          )
        })}

        {/* Time axis rendered via d3-axis */}
        <XAxis scale={xScale} height={plotH} />
      </g>
    </svg>
  )
}

export interface ActivityViewProps {
  snapshot: Snapshot | null
  events: SeqEvent[]
}

// ActivityView renders a horizontal Gantt chart derived from the session
// event stream. X axis is wall-clock time; Y axis is phases as lanes.
// One bar per (task, attempt). Iteration boundaries appear as vertical
// rules; retry markers appear as vertical ticks at task_needs_fix
// timestamps that preceded a retry.
//
// Bar geometry is sourced from IterationStarted, IterationFinished,
// TaskStarted, TaskCompleted, TaskApproved, TaskNeedsFix, and
// PhaseBriefed events. No new event types are required.
export function ActivityView({ snapshot, events }: ActivityViewProps) {
  const [tooltip, setTooltip] = useState<TooltipState | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const [containerSize, setContainerSize] = useState({ width: 800, height: 400 })

  // Track container dimensions so the SVG fills the available area.
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const ro = new ResizeObserver((entries) => {
      const entry = entries[0]
      if (entry) {
        setContainerSize({
          width: entry.contentRect.width,
          height: Math.max(entry.contentRect.height, 200),
        })
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const ganttData = useMemo(() => computeGanttData(events), [events])

  const hasData = ganttData.bars.length > 0 || ganttData.boundaries.length > 0

  return (
    <div
      ref={containerRef}
      style={{
        width: '100%',
        height: '100%',
        position: 'relative',
        overflow: 'auto',
      }}
    >
      {!hasData ? (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            height: '100%',
            color: 'var(--color-muted-foreground)',
            fontSize: 13,
            fontFamily: 'var(--font-sans)',
          }}
        >
          Waiting for events...
        </div>
      ) : (
        <GanttPlot
          data={ganttData}
          width={containerSize.width}
          height={containerSize.height}
          snapshot={snapshot}
          onHover={setTooltip}
        />
      )}

      {/* Hover tooltip rendered outside the SVG for correct layering. */}
      {tooltip && (
        <div
          style={{
            position: 'absolute',
            left: tooltip.x + 12,
            top: tooltip.y - 8,
            zIndex: 50,
            backgroundColor: 'var(--color-background)',
            border: '1px solid var(--color-border)',
            borderRadius: 8,
            padding: '10px 14px',
            boxShadow: '0 8px 32px rgba(0,0,0,0.5)',
            pointerEvents: 'none',
            fontSize: 11,
            fontFamily: 'var(--font-mono)',
            minWidth: 200,
          }}
        >
          <TooltipContent bar={tooltip.bar} snapshot={snapshot} />
        </div>
      )}
    </div>
  )
}

interface TooltipContentProps {
  bar: Bar
  snapshot: Snapshot | null
}

function TooltipContent({ bar, snapshot }: TooltipContentProps) {
  const duration = bar.endMs !== null ? formatDuration(bar.endMs - bar.startMs) : 'running'
  const color = barColor(bar.status)

  // Look up role/model/effort from the snapshot's dag phase-level data.
  // The plan keeps executor_assignment per phase; task-level assignments
  // are not separately tracked in the DAG state payload.
  const phaseData = useMemo(() => {
    const dag = snapshot?.dag as unknown as {
      phases?: Array<{
        id: string
        executor_assignment?: { model?: string; effort?: string }
      }>
    } | null
    return dag?.phases?.find((p) => p.id === bar.phaseId) ?? null
  }, [snapshot, bar.phaseId])

  const model = phaseData?.executor_assignment?.model ?? 'default'
  const effort = phaseData?.executor_assignment?.effort ?? 'default'

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
        <span style={{ color, fontWeight: 700 }}>{bar.taskId}</span>
        <span
          style={{
            fontSize: 9,
            color,
            border: `1px solid ${color}`,
            borderRadius: 3,
            padding: '1px 4px',
            textTransform: 'uppercase',
            letterSpacing: '0.06em',
          }}
        >
          {bar.status.replace('_', ' ')}
        </span>
      </div>
      <TooltipRow label="Phase" value={bar.phaseId} />
      <TooltipRow label="Attempt" value={String(bar.attempt)} />
      <TooltipRow label="Model" value={model} />
      <TooltipRow label="Effort" value={effort} />
      <TooltipRow label="Duration" value={duration} />
      <TooltipRow label="Started" value={formatTime(bar.startMs)} />
    </div>
  )
}

function TooltipRow({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: 'flex', gap: 8 }}>
      <span style={{ color: 'var(--color-muted-foreground)', minWidth: 60, flexShrink: 0 }}>
        {label}
      </span>
      <span style={{ color: 'var(--color-foreground)' }}>{value}</span>
    </div>
  )
}
