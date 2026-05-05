// Shared lazy shiki highlighter. We import shiki/core plus only the langs
// and themes we actually render, so vite does not code-split every grammar
// in the default shiki bundle into its own chunk.
import type { HighlighterCore } from 'shiki/core'

let highlighterPromise: Promise<HighlighterCore> | null = null

export function getHighlighter(): Promise<HighlighterCore> {
  if (!highlighterPromise) {
    highlighterPromise = (async () => {
      const [{ createHighlighterCore }, { createOnigurumaEngine }, wasm] =
        await Promise.all([
          import('shiki/core'),
          import('shiki/engine/oniguruma'),
          import('shiki/wasm'),
        ])
      return createHighlighterCore({
        themes: [import('shiki/themes/github-dark.mjs')],
        langs: [
          import('shiki/langs/json.mjs'),
          import('shiki/langs/bash.mjs'),
          import('shiki/langs/go.mjs'),
          import('shiki/langs/typescript.mjs'),
          import('shiki/langs/markdown.mjs'),
        ],
        engine: createOnigurumaEngine(wasm),
      })
    })()
  }
  return highlighterPromise
}

export const SHIKI_THEME = 'github-dark'
