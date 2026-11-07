import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './styles/index.css';
import { App } from './App';

// Another site must not frame the page to have its buttons (approvals) clicked.
// The host's CSP says so too, but a static site serving the standalone build may
// send no headers.
const framed = (() => { try { return window.top !== window.self; } catch { return true; } })();

if (framed) {
  document.body.textContent = 'CC Remote cannot run inside a frame.';
} else {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}

// The service worker caches the app's own assets and shows notifications on
// Android (no Notification constructor there). Relative to the page: the
// standalone build may live under a sub-path.
if (!framed && import.meta.env.PROD && 'serviceWorker' in navigator) {
  navigator.serviceWorker.register(`${import.meta.env.BASE_URL}sw.js`).catch(() => { /* unsupported or blocked */ });
}
