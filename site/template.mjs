import { repo, windowsCommand, unixCommand } from './content.mjs';

const escape = text => String(text).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);

export function renderPage(c) {
  const doc = file => `${repo}/blob/main/docs/${file}`;
  const command = (id, title, detail, text) => `<article class="install-block">
    <div class="command-heading"><div><h3>${title}</h3><p>${detail}</p></div><button hidden type="button" data-copy="${id}" data-copied="${c.copied}" data-success="${c.success}" data-manual="${c.manual}">${c.copy}</button></div>
    <pre><code id="${id}">${escape(text)}</code></pre><p class="copy-status" role="status" aria-live="polite"></p>
  </article>`;
  return `<!doctype html>
<html lang="${c.lang}">
<head>
  <meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="description" content="${escape(c.description)}">
  <meta name="theme-color" content="#263d31"><meta name="referrer" content="no-referrer">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src 'self'; style-src 'self'; script-src 'self'; base-uri 'none'; form-action 'none'; connect-src 'none'">
  <meta property="og:type" content="website"><meta property="og:title" content="${escape(c.title)}"><meta property="og:description" content="${escape(c.description)}">
  <meta property="og:image" content="https://huanfeng.github.io/cc-remote/about/assets/desktop.png">
  <title>${escape(c.title)}</title><link rel="icon" href="${c.assets}/icon.svg"><link rel="stylesheet" href="${c.assets}/style.css">
  <script src="${c.assets}/copy.js" defer></script>
</head>
<body>
  <a class="skip" href="#main">${c.skip}</a>
  <header class="container header">
    <a class="brand" href="#main"><img src="${c.assets}/icon.svg" alt="" width="34" height="34">CC Remote</a>
    <nav aria-label="${c.docs}"><a href="#features">${c.features}</a><a href="${doc(c.guide)}">${c.docs}</a><a href="${c.switchHref}" lang="${c.lang === 'en' ? 'zh-CN' : 'en'}">${c.language}</a><a class="nav-source" href="${repo}">GitHub ↗</a></nav>
  </header>
  <main id="main">
    <section class="container hero" aria-labelledby="headline">
      <div class="hero-copy"><p class="eyebrow">${c.eyebrow}</p><h1 id="headline">${c.headline}</h1><p class="lead">${c.lead}</p>
        <div class="actions"><a class="button primary" href="#install">${c.primary}<span aria-hidden="true">↗</span></a><a class="button secondary" href="${doc(c.guide)}">${c.secondary}</a></div><p class="platforms">${c.supported}</p>
      </div>
      <aside class="handoff" aria-label="Claude Code">
        <div class="terminal"><div class="terminal-top"><span class="terminal-dots" aria-hidden="true">● ● ●</span><span>${c.terminalLabel}</span></div><p class="comment">${c.terminalComment}</p><pre><code><span class="prompt">$</span> cd your-project\n<span class="prompt">$</span> ccr</code></pre><p class="detach">${c.detach}</p></div>
        <div class="handoff-line" aria-hidden="true">↓</div><div class="phone-step"><span class="status-dot" aria-hidden="true"></span><div><strong>${c.phone}</strong><p>${c.phoneText}</p></div></div><p class="return">↳ ${c.return}</p>
      </aside>
    </section>
    <figure class="container product"><div class="product-frame"><img class="desktop-shot" src="${c.assets}/desktop.png" alt="${c.desktopAlt}" width="1440" height="900" fetchpriority="high"><img class="phone-shot" src="${c.assets}/mobile.png" alt="${c.mobileAlt}" loading="lazy"></div><figcaption>${c.screenshot}</figcaption></figure>
    <section class="container section" id="features" aria-labelledby="features-title"><div class="section-intro"><h2 id="features-title">${c.featureHeading}</h2><p>${c.featureIntro}</p></div><div class="features">${c.featureItems.map(([title, description], i) => `<article><span class="feature-number">0${i + 1}</span><div><h3>${title}</h3><p>${description}</p></div></article>`).join('')}</div></section>
    <section class="install-section" id="install" aria-labelledby="install-title"><div class="container"><div class="section-intro"><h2 id="install-title">${c.installHeading}</h2><p>${c.prerequisite}</p></div>
      ${command('windows-command', c.winLabel, c.winDetail, windowsCommand)}${command('unix-command', c.unixLabel, c.unixDetail, unixCommand)}<p class="update-note">${c.update}</p>
      <ol class="first-steps">${c.firstSteps.map(([title, description]) => `<li><h3>${title}</h3><p>${description}</p></li>`).join('')}</ol><a class="text-link" href="${doc(c.guide)}">${c.secondary} →</a>
    </div></section>
    <section class="container section resources" aria-labelledby="resources-title"><h2 id="resources-title">${c.guideHeading}</h2><div class="guide-list">${c.guides.map(([title, description, file]) => `<a href="${doc(file)}"><div><h3>${title}</h3><p>${description}</p></div><span aria-hidden="true">↗</span></a>`).join('')}</div><div class="client-link"><a class="button secondary" href="${c.appHref}">${c.app} →</a><p>${c.appNote}</p></div><p class="security">${c.security} <a href="${doc('security.md')}">${c.securityLink} ↗</a></p></section>
  </main>
  <footer class="container footer"><div><a class="brand" href="#main">CC Remote</a><p>${c.footer}</p></div><div><a href="${repo}">${c.source}</a><a href="${repo}/blob/main/LICENSE">${c.license}</a></div></footer>
</body></html>\n`;
}
