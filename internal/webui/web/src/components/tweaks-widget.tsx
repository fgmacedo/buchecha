import { useEffect, useState } from 'react'
import { useTheme } from '../hooks/use-theme'

// NodeStyle controls phase/task density on the canvas. "card" is the rich
// default; "minimal" collapses tasks to thin label bars for a compact mode.
export type NodeStyle = 'card' | 'minimal'

const STYLE_STORAGE_KEY = 'bcc:node-style'
const STYLE_EVENT = 'bcc:node-style:changed'

// nodeStyle is module-level so multiple widgets and consumers stay in sync
// through the shared CustomEvent below without a context provider.
let nodeStyle: NodeStyle = (() => {
  if (typeof window === 'undefined') return 'card'
  try {
    const raw = window.localStorage.getItem(STYLE_STORAGE_KEY)
    return raw === 'minimal' ? 'minimal' : 'card'
  } catch {
    return 'card'
  }
})()

function setStoredNodeStyle(next: NodeStyle): void {
  nodeStyle = next
  try {
    window.localStorage.setItem(STYLE_STORAGE_KEY, next)
  } catch {
    // storage disabled is fine
  }
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent(STYLE_EVENT, { detail: next }))
  }
}

// useNodeStyle reads the persisted node-style and re-renders consumers when
// the TweaksWidget toggles it. Subscribes to a CustomEvent fired in
// setStoredNodeStyle so all consumers tick together without a context.
export function useNodeStyle(): NodeStyle {
  const [value, setValue] = useState<NodeStyle>(nodeStyle)
  useEffect(() => {
    function onChange(e: Event) {
      const detail = (e as CustomEvent<NodeStyle>).detail
      if (detail === 'card' || detail === 'minimal') setValue(detail)
    }
    window.addEventListener(STYLE_EVENT, onChange)
    return () => window.removeEventListener(STYLE_EVENT, onChange)
  }, [])
  return value
}

// TweaksWidget is the corner panel from the design handoff. It exposes two
// segmented controls — theme (dark/light) and node-style (card/minimal) —
// pinned to the bottom-right edge.
export function TweaksWidget() {
  const { theme, setTheme } = useTheme()
  const style = useNodeStyle()
  return (
    <div
      data-testid="tweaks-widget"
      style={{
        position: 'fixed',
        right: 18,
        bottom: 18,
        zIndex: 50,
        background: 'var(--surface-panel)',
        border: '1px solid var(--border-default)',
        borderRadius: 12,
        padding: 10,
        boxShadow: 'var(--shadow-pop)',
        display: 'flex',
        gap: 8,
        alignItems: 'center',
      }}
    >
      <span
        style={{
          fontSize: 10,
          color: 'var(--color-faint, var(--color-muted-foreground))',
          textTransform: 'uppercase',
          letterSpacing: '.08em',
          marginRight: 4,
          fontFamily: 'var(--font-mono)',
        }}
      >
        Tweaks
      </span>
      <Seg<typeof theme>
        value={theme}
        options={['dark', 'light']}
        onChange={(v) => setTheme(v)}
      />
      <Seg<NodeStyle>
        value={style}
        options={['card', 'minimal']}
        onChange={(v) => setStoredNodeStyle(v)}
      />
    </div>
  )
}

// Seg renders a small segmented control. The active option carries the
// elevated background; clicks fire onChange synchronously.
function Seg<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T
  options: readonly T[]
  onChange: (v: T) => void
}) {
  return (
    <div
      style={{
        display: 'flex',
        padding: 2,
        background: 'var(--surface-elevated)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 8,
      }}
    >
      {options.map((o) => (
        <button
          key={o}
          type="button"
          onClick={() => onChange(o)}
          style={{
            padding: '4px 10px',
            fontSize: 11,
            borderRadius: 6,
            border: 0,
            cursor: 'pointer',
            background: value === o ? 'var(--surface-card)' : 'transparent',
            color:
              value === o
                ? 'var(--color-foreground)'
                : 'var(--color-muted-foreground)',
            boxShadow:
              value === o ? '0 0 0 1px var(--border-default)' : 'none',
            textTransform: 'capitalize',
            fontFamily: 'inherit',
          }}
        >
          {o}
        </button>
      ))}
    </div>
  )
}
