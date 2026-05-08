// Minimal stroke icons for the four director roles plus a small tool icon
// library. Stroke-based, no fill, keep them readable at 9-14px. Sourcing
// these from a shared module so the agent card, history badges, and
// inspector header can all draw the same glyph by role name.

import type { AgentRole } from '../hooks/use-agents'

interface IconProps {
  size?: number
  strokeWidth?: number
}

function StrokeIcon({
  paths,
  size = 14,
  strokeWidth = 1.6,
}: IconProps & { paths: string[] }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths.map((d, i) => (
        <path key={i} d={d} />
      ))}
    </svg>
  )
}

// Compass — Planner
export function PlannerIcon(p: IconProps) {
  return (
    <StrokeIcon
      {...p}
      paths={[
        'M12 2v2',
        'M12 20v2',
        'M2 12h2',
        'M20 12h2',
        'M4.93 4.93l1.41 1.41',
        'M17.66 17.66l1.41 1.41',
        'M4.93 19.07l1.41-1.41',
        'M17.66 6.34l1.41-1.41',
        'M14.5 9.5l-2 5-5 2 2-5 5-2z',
      ]}
    />
  )
}

// Clipboard — Briefer
export function BrieferIcon(p: IconProps) {
  return (
    <StrokeIcon
      {...p}
      paths={[
        'M9 4h6a1 1 0 0 1 1 1v1H8V5a1 1 0 0 1 1-1z',
        'M8 6H6a1 1 0 0 0-1 1v13a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V7a1 1 0 0 0-1-1h-2',
        'M8 12h8',
        'M8 16h5',
      ]}
    />
  )
}

// Hammer — Executor
export function ExecutorIcon(p: IconProps) {
  return (
    <StrokeIcon
      {...p}
      paths={[
        'M14 4l6 6-3 3-6-6 3-3z',
        'M11 7l-7 7 4 4 7-7',
        'M5 21l2-2',
      ]}
    />
  )
}

// Magnifier — Reviewer
export function ReviewerIcon(p: IconProps) {
  return (
    <StrokeIcon
      {...p}
      paths={['M11 4a7 7 0 1 1 0 14 7 7 0 0 1 0-14z', 'M16 16l5 5', 'M9 11h4']}
    />
  )
}

const ROLE_ICONS: Record<
  AgentRole,
  (p: IconProps) => ReturnType<typeof StrokeIcon>
> = {
  planner: PlannerIcon,
  briefer: BrieferIcon,
  executor: ExecutorIcon,
  reviewer: ReviewerIcon,
}

const ROLE_LABEL: Record<AgentRole, string> = {
  planner: 'Planner',
  briefer: 'Briefer',
  executor: 'Executor',
  reviewer: 'Reviewer',
}

export function RoleIcon({ role, size }: { role: AgentRole; size?: number }) {
  const Icon = ROLE_ICONS[role]
  return <Icon size={size} />
}

export function roleLabel(role: AgentRole): string {
  return ROLE_LABEL[role]
}

// roleColor returns the CSS variable reference for the role's hue, and
// roleColorDim returns the translucent variant used as an icon-tile fill.
export function roleColor(role: AgentRole): string {
  return `var(--role-${role})`
}
export function roleColorDim(role: AgentRole): string {
  return `var(--role-${role}-dim)`
}

// ---------- Tool icons -------------------------------------------------
// Small, uniform, monochrome. Used by tool chips on the agent card and
// inside the Overview tab in the inspector.
const TOOL_PATHS: Record<string, string[]> = {
  Read: [
    'M4 5h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5z',
    'M8 9h8M8 13h8M8 17h5',
  ],
  Edit: ['M4 20h4l10-10-4-4L4 16v4z', 'M14 6l4 4'],
  Write: ['M5 19l4-1 11-11-3-3L6 15l-1 4z', 'M14 6l3 3'],
  Bash: [
    'M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z',
    'M7 10l3 2-3 2',
    'M13 14h4',
  ],
  Glob: [
    'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z',
    'M3 12h18',
    'M12 3a14 14 0 0 1 0 18',
    'M12 3a14 14 0 0 0 0 18',
  ],
  Task: ['M4 6h16M4 12h16M4 18h10'],
}

export function ToolIcon({ name, size = 11 }: { name: string; size?: number }) {
  const paths = TOOL_PATHS[name] ?? TOOL_PATHS.Bash
  return <StrokeIcon paths={paths} size={size} strokeWidth={1.5} />
}
