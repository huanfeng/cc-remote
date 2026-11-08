import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

// The version the page shows: the release's tag (CC_REMOTE_VERSION, set by the
// workflows), else what git says about this checkout.
function appVersion(): string {
  if (process.env.CC_REMOTE_VERSION) return process.env.CC_REMOTE_VERSION;
  try { return execSync('git describe --tags --always --dirty', { encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim() || 'dev'; } catch { return 'dev'; }
}

// The host sends its page a Content-Security-Policy header; a static site may not,
// so the standalone page carries one itself. It connects to any host the user adds
// (connect-src, wss: and ws: for the terminal view), but loads no script and no image
// from elsewhere: a remote image in Claude's Markdown cannot carry data out.
// frame-ancestors only works as a header (main.tsx also refuses to run in a frame).
const STANDALONE_CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' https: http: wss: ws:; worker-src 'self'; manifest-src 'self'; base-uri 'self'; form-action 'self'; object-src 'none'";
const standaloneCsp = (): Plugin => ({
  name: 'standalone-csp',
  transformIndexHtml: html => html.replace(/<meta charset="UTF-8"\s*\/?>/, m => `${m}\n    <meta http-equiv="Content-Security-Policy" content="${STANDALONE_CSP}" />`),
  // The response headers a page cannot set itself, for static hosts that read a
  // _headers file (Cloudflare Pages, Netlify).
  generateBundle() {
    this.emitFile({ type: 'asset', fileName: '_headers', source: "/*\n  Content-Security-Policy: frame-ancestors 'none'\n  X-Content-Type-Options: nosniff\n  Referrer-Policy: no-referrer\n" });
  },
});

// The standalone page may be served from a sub-path (base './'): public files it
// names in index.html are made relative too. Vite rewrites module scripts and
// links itself, but leaves a classic <script src> alone.
const relativePublic = (): Plugin => ({
  name: 'relative-public',
  apply: 'build',
  transformIndexHtml: html => html.replace('<script src="/theme-init.js">', '<script src="./theme-init.js">'),
});

// `vite build --mode standalone` builds the standalone frontend: a static site
// that connects to hosts elsewhere, so it works from any path (base './').
// The dev server proxies /api to a development host (CC_DEV_HOST, default the
// acceptance host on 8790), never the one a user runs on 8787 by default.
export default defineConfig(({ mode }) => {
  const standalone = mode === 'standalone';
  // `--mode desktop`: the desktop app's frontend (desktop/, Tauri). Tauri sends the CSP
  // (desktop/src-tauri/tauri.conf.json) and serves the files from the root.
  const desktop = mode === 'desktop';
  const devHost = process.env.CC_DEV_HOST || 'http://127.0.0.1:8790';
  return {
    plugins: [react(), tailwindcss(), ...(standalone ? [standaloneCsp(), relativePublic()] : [])],
    resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
    define: { __APP_VERSION__: JSON.stringify(appVersion()) },
    server: desktop
      ? { port: 5175, strictPort: true, host: 'localhost' }
      : { port: 5174, proxy: { '/api': { target: devHost, ws: true } } },
    clearScreen: !desktop,
    ...(standalone ? { base: './' } : {}),
    build: {
      outDir: standalone ? 'dist-standalone' : desktop ? 'dist-desktop' : 'dist',
      chunkSizeWarningLimit: 1500,
      // Fonts stay files: the host CSP (default-src 'self', no font-src) blocks
      // data: fonts, and Vite would otherwise inline the small woff2 slices.
      assetsInlineLimit: (file: string) => (/\.(woff2?|ttf|otf)$/.test(file) ? false : undefined),
    },
  };
});
