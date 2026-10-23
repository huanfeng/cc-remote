import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import { execSync } from 'node:child_process';

// The version the page shows: the release's tag (CC_REMOTE_VERSION, set by the
// workflows), else what git says about this checkout.
function appVersion(): string {
  if (process.env.CC_REMOTE_VERSION) return process.env.CC_REMOTE_VERSION;
  try { return execSync('git describe --tags --always --dirty', { encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim() || 'dev'; } catch { return 'dev'; }
}

// The host sends its page a Content-Security-Policy header; a static site may not,
// so the standalone page carries one itself. It connects to any host the user adds
// (connect-src, wss: and ws: for the terminal view), but loads no script and no image from elsewhere: a remote image in
// Claude's Markdown cannot carry data out. frame-ancestors only works as a header,
// see docs/configuration.md.
const STANDALONE_CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' https: http: wss: ws:; worker-src 'self'; manifest-src 'self'; base-uri 'self'; form-action 'self'; object-src 'none'";
const standaloneCsp = (): Plugin => ({
  name: 'standalone-csp',
  transformIndexHtml: html => html.replace('<meta charset="UTF-8"/>', `<meta charset="UTF-8"/><meta http-equiv="Content-Security-Policy" content="${STANDALONE_CSP}"/>`),
  // The response headers a page cannot set itself, for static hosts that read a
  // _headers file (Cloudflare Pages, Netlify); web/vercel.json has the same for Vercel.
  generateBundle() {
    this.emitFile({ type: 'asset', fileName: '_headers', source: "/*\n  Content-Security-Policy: frame-ancestors 'none'\n  X-Content-Type-Options: nosniff\n  Referrer-Policy: no-referrer\n" });
  },
});

// `vite build --mode standalone` builds the standalone frontend: a static site
// that connects to hosts elsewhere, so it works from any path (base './').
export default defineConfig(({ mode }) => ({
  plugins: [react(), ...(mode === 'standalone' ? [standaloneCsp()] : [])],
  define: { __APP_VERSION__: JSON.stringify(appVersion()) },
  server: { proxy: { '/api': 'http://127.0.0.1:8787' } },
  ...(mode === 'standalone' ? { base: './', build: { outDir: 'dist-standalone' } } : {}),
}));
