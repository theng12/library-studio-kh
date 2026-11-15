"use strict";

async function copyWebAddress(input, message) {
  if (!input.value) return;
  input.focus(); input.select(); input.setSelectionRange(0, input.value.length);
  let copied = false;
  try {
    if (navigator.clipboard?.writeText) { await navigator.clipboard.writeText(input.value); copied = true; }
  } catch (_) { /* HTTP pages may need the selection-based fallback. */ }
  if (!copied) {
    try { copied = document.execCommand("copy"); } catch (_) { /* Keep the selected link available for manual copying. */ }
  }
  message.textContent = copied ? "Link copied." : "Automatic copy is unavailable. The link is selected; use your device’s Copy command.";
}

// Delegated: the Copy button lives in Settings, which renders after this script loads.
document.addEventListener("click", event => {
  const button = event.target.closest?.("[data-copy-address]");
  if (!button) return;
  event.preventDefault();
  copyWebAddress(document.getElementById(button.dataset.copyAddress), document.getElementById(button.dataset.copyMsg));
});
