/**
 * Keeps the app inside the part of the screen the soft keyboard leaves visible.
 * Mobile browsers shrink only the visual viewport for the keyboard (100dvh does
 * not change), so a full-height app leaves its composer under the keyboard and
 * the page scrolls. The app is sized and placed from visualViewport instead:
 * --app-height and --app-top on the root element.
 */
export function trackVisualViewport() {
  const vv = window.visualViewport;
  if (!vv) return;
  const root = document.documentElement;
  let frame = 0;
  const update = () => {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(() => {
      // Pinch zoom also shrinks the visual viewport: keep the layout as it is then.
      if (Math.abs(vv.scale - 1) > 0.01) return;
      root.style.setProperty('--app-height', `${Math.round(vv.height)}px`);
      root.style.setProperty('--app-top', `${Math.round(vv.offsetTop)}px`);
    });
  };
  vv.addEventListener('resize', update);
  vv.addEventListener('scroll', update);
  update();
}
