import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './style.css';
import { trackVisualViewport } from './viewport';
trackVisualViewport();
ReactDOM.createRoot(document.getElementById('root')!).render(<React.StrictMode><App /></React.StrictMode>);
if (import.meta.env.PROD && 'serviceWorker' in navigator) navigator.serviceWorker.register(`${import.meta.env.BASE_URL}sw.js`).catch(() => {});
