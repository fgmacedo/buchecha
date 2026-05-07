// ProviderChip renders a small badge with the LLM provider's brand glyph
// and name. Used next to model/effort on agent cards, in the inspector
// header, and in the phase plan strip.

interface ProviderMeta {
  label: string
  glyph: string
  color: string
}

const PROVIDER_META: Record<string, ProviderMeta> = {
  claude: { label: 'Claude', glyph: '✦', color: '#D97757' },
  codex: { label: 'Codex', glyph: '◐', color: '#10A37F' },
  openai: { label: 'OpenAI', glyph: '◐', color: '#10A37F' },
  gemini: { label: 'Gemini', glyph: '◇', color: '#4285F4' },
}

const FALLBACK: ProviderMeta = {
  label: '—',
  glyph: '•',
  color: 'var(--color-muted-foreground)',
}

export type ProviderChipSize = 'sm' | 'md'

export function ProviderChip({
  provider,
  size = 'sm',
}: {
  provider?: string | null
  size?: ProviderChipSize
}) {
  if (!provider) return null
  const meta = PROVIDER_META[provider.toLowerCase()] ?? {
    ...FALLBACK,
    label: provider,
  }
  const padY = size === 'sm' ? 1 : 2
  const padX = size === 'sm' ? 5 : 7
  const fs = size === 'sm' ? 9.5 : 10.5

  return (
    <span
      title={`provider: ${meta.label}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        padding: `${padY}px ${padX}px`,
        borderRadius: 4,
        background: `color-mix(in srgb, ${meta.color} 14%, transparent)`,
        border: `1px solid color-mix(in srgb, ${meta.color} 35%, transparent)`,
        color: meta.color,
        fontSize: fs,
        fontFamily: 'var(--font-mono)',
        lineHeight: 1,
        flexShrink: 0,
        letterSpacing: '0.02em',
      }}
    >
      <span aria-hidden="true" style={{ fontSize: fs + 1 }}>
        {meta.glyph}
      </span>
      <span>{meta.label.toLowerCase()}</span>
    </span>
  )
}
