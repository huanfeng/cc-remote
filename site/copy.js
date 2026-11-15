document.querySelectorAll('[data-copy]').forEach(button => {
  const original = button.textContent;
  button.hidden = false;
  button.addEventListener('click', async () => {
    const code = document.getElementById(button.dataset.copy);
    const status = button.closest('.install-block').querySelector('[role="status"]');
    try {
      await navigator.clipboard.writeText(code.textContent);
      button.textContent = button.dataset.copied;
      status.textContent = button.dataset.success;
    } catch {
      const range = document.createRange();
      range.selectNodeContents(code);
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      status.textContent = button.dataset.manual;
    }
    window.setTimeout(() => { button.textContent = original; }, 2500);
  });
});
