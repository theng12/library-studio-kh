"use strict";

function fillWebAddresses(state) {
  const remote = state.remote || {};
  for (const [id, value] of [["local-web-address", state.localUrl], ["tail-web-address", remote.on ? remote.url : ""]]) {
    const input = document.getElementById(id);
    input.value = value || "";
    input.placeholder = "Not available";
    document.querySelector('[data-copy-address="' + id + '"]').disabled = !value;
  }
  document.getElementById("web-address-help").textContent = remote.error || (remote.on ? "Both devices must be connected to the same Tailscale network." : "Tailscale access is off. Enable it in Settings to get a link for other devices.");
}

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

for (const button of document.querySelectorAll("[data-copy-address]")) {
  button.addEventListener("click", () => copyWebAddress(document.getElementById(button.dataset.copyAddress), document.getElementById("web-address-msg")));
}
