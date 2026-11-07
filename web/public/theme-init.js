// Applies the stored theme before first paint so the page never flashes the
// wrong one. A file of its own: the host's Content-Security-Policy runs no
// inline script.
(function () {
  try {
    var pref = localStorage.getItem('ccn-theme') || 'system';
    var dark = pref === 'dark' || (pref === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
    var root = document.documentElement;
    root.classList.toggle('dark', dark);
    root.dataset.palette = localStorage.getItem('ccn-palette') || 'graphite';
  } catch (e) { document.documentElement.dataset.palette = 'graphite'; }
})();
