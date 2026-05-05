// Shared lazy shiki highlighter. We import shiki/core plus only the langs
// and themes we actually render, so vite does not code-split every grammar
// in the default shiki bundle into its own chunk.
//
// Engine: the JavaScript regex engine (oniguruma-to-js) replaces the
// WASM Oniguruma engine. The WASM blob is the single largest asset
// shiki ships (~230 KB gzipped, base64-inlined into a JS chunk); the
// JS engine drops it entirely. `forgiving: true` skips grammar patterns
// the converter cannot translate instead of throwing, so highlighting
// degrades gracefully on edge-case rules in the langs we load below.
import type { HighlighterCore } from 'shiki/core'

let highlighterPromise: Promise<HighlighterCore> | null = null

export function getHighlighter(): Promise<HighlighterCore> {
  if (!highlighterPromise) {
    highlighterPromise = (async () => {
      const [{ createHighlighterCore }, { createJavaScriptRegexEngine }] =
        await Promise.all([
          import('shiki/core'),
          import('shiki/engine/javascript'),
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
        engine: createJavaScriptRegexEngine({ forgiving: true }),
      })
    })()
  }
  return highlighterPromise
}

export const SHIKI_THEME = 'github-dark'
