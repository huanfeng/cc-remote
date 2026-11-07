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

// After the host is updated, a page opened earlier still references the old
// build's lazy chunks, which are gone (404). Reload once to pick up the new
// build; the flag stops a reload loop if the chunk is missing for another reason.
window.addEventListener('vite:preloadError', event => {
  try {
    const last = Number(sessionStorage.getItem('ccn-chunk-reload') || 0);
    if (Date.now() - last < 30_000) return;
    sessionStorage.setItem('ccn-chunk-reload', String(Date.now()));
  } catch { /* storage unavailable: reload anyway, at worst once per failure */ }
  event.preventDefault();
  location.reload();
});

// The service worker caches the app's own assets and shows notifications on
// Android (no Notification constructor there). Relative to the page: the
// standalone build may live under a sub-path.
if (!framed && import.meta.env.PROD && 'serviceWorker' in navigator) {
  navigator.serviceWorker.register(`${import.meta.env.BASE_URL}sw.js`).catch(() => { /* unsupported or blocked */ });
}
