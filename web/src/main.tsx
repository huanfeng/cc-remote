import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './style.css';
import { applyTheme, currentTheme } from './theme';
import { trackVisualViewport } from './viewport';
applyTheme(currentTheme());
// Another site must not frame the page to have its buttons (approvals) clicked.
// The host's CSP says so too, but a static host serving the standalone build may
// send no headers.
const framed = (() => { try { return window.top !== window.self; } catch { return true; } })();
if (framed) document.body.textContent = 'CC Remote cannot run inside a frame.';
else {
  trackVisualViewport();
  ReactDOM.createRoot(document.getElementById('root')!).render(<React.StrictMode><App /></React.StrictMode>);
}
if (import.meta.env.PROD && 'serviceWorker' in navigator) navigator.serviceWorker.register(`${import.meta.env.BASE_URL}sw.js`).catch(() => {});
