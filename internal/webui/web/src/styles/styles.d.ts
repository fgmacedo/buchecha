// Side-effect import declarations for stylesheets. Vite handles the
// runtime; TypeScript needs the ambient module shape because the
// project sets noUncheckedSideEffectImports. The CSS modules export
// nothing JavaScript-visible: the import is purely a build-time hook
// that tells Vite to bundle the file into the page's hashed CSS.

declare module '*.css'
