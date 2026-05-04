import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './app'

const rootEl = document.getElementById('root')
if (!rootEl) {
  throw new Error('root element #root not found in index.html')
}

createRoot(rootEl).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
