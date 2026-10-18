import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';

// The host sends its page a Content-Security-Policy header; a static site may not,
// so the standalone page carries one itself. It connects to any host the user adds
// (connect-src), but loads no script and no image from elsewhere: a remote image in
// Claude's Markdown cannot carry data out. frame-ancestors only works as a header,
// see docs/configuration.md.
const STANDALONE_CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' https: http:; worker-src 'self'; manifest-src 'self'; base-uri 'self'; form-action 'self'; object-src 'none'";
const standaloneCsp = (): Plugin => ({
  name: 'standalone-csp',
  transformIndexHtml: html => html.replace('<meta charset="UTF-8"/>', `<meta charset="UTF-8"/><meta http-equiv="Content-Security-Policy" content="${STANDALONE_CSP}"/>`),
});

// `vite build --mode standalone` builds the standalone frontend: a static site
// that connects to hosts elsewhere, so it works from any path (base './').
export default defineConfig(({ mode }) => ({
  plugins: [react(), ...(mode === 'standalone' ? [standaloneCsp()] : [])],
  server: { proxy: { '/api': 'http://127.0.0.1:8787' } },
  ...(mode === 'standalone' ? { base: './', build: { outDir: 'dist-standalone' } } : {}),
}));
