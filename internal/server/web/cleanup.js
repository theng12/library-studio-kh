"use strict";

const CLEANUP_NUMBERS = {
  stagedDays: [1, 365], managedTrashDays: [0, 365], nativeTrashDays: [1, 3650],
  pressureFreeGB: [0, 10000000], seedRatio: [0, 1000000], seedMinutes: [0, 5256000],
};
let cleanupBusy = false;
let cleanupState = null;
let cleanupSerial = 0;

function cleanupPolicy(form) {
  const patch = { stagedCleanupOn: form.elements.stagedCleanupOn.checked, pressureBypassRetention: !!form.elements.pressureBypassRetention?.checked, managedTrashPath: form.elements.managedTrashPath.value.trim() };
  for (const [key, [min, max]] of Object.entries(CLEANUP_NUMBERS)) {
    const input = form.elements[key], n = Number(input.value);
    if (!input.value.trim() || !Number.isFinite(n) || n < min || n > max || (key !== "seedRatio" && !Number.isInteger(n))) {
      input.focus();
      throw new Error(input.parentElement.textContent.trim() + " must be " + (key === "seedRatio" ? "a number" : "a whole number") + " from " + min + " to " + max + ".");
    }
    patch[key] = n;
  }
  return patch;
}
function cleanupFill(settings) {
  const form = $("#cleanup-form");
  for (const key of Object.keys(CLEANUP_NUMBERS)) form.elements[key].value = settings[key] ?? "";
  form.elements.stagedCleanupOn.checked = !!settings.stagedCleanupOn;
  form.elements.pressureBypassRetention.checked = !!settings.pressureBypassRetention;
  form.elements.managedTrashPath.value = settings.managedTrashPath || "";
}
function cleanupPhase(phase) {
  return { staging: "Download folder", managed_trash: "Library’s _trash", native_trash: "macOS Trash", removed: "Removed", restored: "Restored" }[phase] || phase;
}
function cleanupCanRun(status) {
  return !!status?.enabled && !!status.engineAvailable && !(status.engineBlocked || []).length;
}
function cleanupButtons() {
  for (const id of ["#cleanup-save", "#cleanup-preview", "#cleanup-refresh"]) $(id).disabled = cleanupBusy;
  $("#cleanup-run").disabled = cleanupBusy || !cleanupCanRun(cleanupState);
  for (const button of document.querySelectorAll("[data-cleanup-restore]")) button.disabled = cleanupBusy;
}
function cleanupRender(status) {
  cleanupState = status;
  const body = $("#cleanup-status"); body.replaceChildren();
  const dl = mediaNode("dl", null, "facts");
  facts(dl, [
    ["Policy", status.enabled ? "On · only proven eligible imports can move" : "Off · no automatic cleanup"],
    ["Free space", Number.isFinite(status.freeBytes) ? bytes(status.freeBytes) : "Not reported"],
    ["Download folder", status.stagedDays + " days after verified import"],
    ["Library’s _trash", status.managedTrashDays + " extra days · " + status.totalDays + " days total"],
    ["macOS Trash", status.nativeTrashDays + " days, or earlier below the free-space threshold"],
    ["Free-space threshold", status.pressureFreeGB ? status.pressureFreeGB + " GB" : "Off"],
    ["Skip staging waits under pressure", status.pressureBypassRetention && status.pressureFreeGB ? "On · oldest completed, NAS-verified copies first" : "Off"],
    ["Managed folder", status.managedTrashPath || "Not reported"],
    ["Next due", status.nextDue ? stamp(status.nextDue) : "No due time reported"],
  ]);
  body.append(dl);
  const blockers = [...(status.engineBlocked || []), ...(status.blocked || [])];
  if (!status.engineAvailable && !blockers.length) blockers.push("Staged cleanup is unavailable on this computer.");
  if (blockers.length) body.append(mediaNode("p", blockers.join(" "), "notice media-padding"));
  const rows = (status.items || []).map(item => {
    const action = mediaNode("div");
    if (["managed_trash", "native_trash"].includes(item.phase)) {
      const button = mediaNode("button", "Restore local copy", "small");
      button.type = "button"; button.dataset.cleanupRestore = item.hash;
      button.setAttribute("aria-label", "Restore local copy of " + item.name);
      button.addEventListener("click", () => cleanupAction("restore", { hash: item.hash }));
      action.append(button);
    }
    const detail = mediaNode("div", item.proposed || "No action proposed");
    if ((item.blocked || []).length) detail.append(mediaNode("p", item.blocked.join(" "), "muted"));
    const proof = mediaNode("details"), evidence = mediaNode("dl", null, "facts");
    proof.append(mediaNode("summary", "Import proof and restore paths"));
    facts(evidence, [
      ["Managed by", item.app || "Not reported"],
      ["Verified import", item.verifiedImport ? stamp(item.verifiedImport) : "Not reported"],
      ["Imported library file", item.importPath || "Not reported"],
      ["Verified files", Number.isInteger(item.files) ? String(item.files) : "Not reported"],
      ["Original local path", item.sourcePath || "Not reported"],
      ["Managed copy", item.managedPath || "—"],
      ["Trash copy", item.nativePath || "—"],
      ["Restore to", item.restorePath || "Not reported"],
    ]);
    proof.append(evidence); detail.append(proof);
    return row([item.name, cleanupPhase(item.phase), bytes(item.bytes), item.nextDue ? stamp(item.nextDue) : "Not reported", detail, action]);
  });
  body.append(rows.length ? mediaTable(["Download", "Location", "Size", "Next due", "Proposed action / blockers", "Action"], rows) : mediaNode("p", "No tracked downloads. Preview checks for eligible verified imports.", "empty"));
  cleanupButtons();
}
async function loadCleanup() {
  if (cleanupBusy) return;
  const serial = ++cleanupSerial;
  cleanupBusy = true; cleanupButtons();
  $("#cleanup-msg").textContent = "Reading saved cleanup status…";
  try {
    const [state, status] = await Promise.all([api("/api/state"), api("/api/cleanup/status")]);
    if (serial !== cleanupSerial) return;
    cleanupFill(state.settings);
    cleanupRender(status);
    $("#cleanup-msg").textContent = "Saved status. Use Preview to verify current files and proposed actions.";
  } catch (e) {
    cleanupState = null;
    $("#cleanup-msg").textContent = e.message;
  } finally { cleanupBusy = false; cleanupButtons(); }
}
async function cleanupAction(action, request = {}) {
  if (cleanupBusy) return;
  cleanupBusy = true; cleanupButtons();
  $("#cleanup-msg").textContent = action === "preview" ? "Verifying imports and local copies. Nothing is changed by Preview…" : action === "restore" ? "Restoring the local copy…" : "Checking and applying eligible cleanup…";
  try {
    const result = await api("/api/cleanup/" + action, request);
    cleanupRender(result.status);
    const messages = [...(result.actions || []), ...(result.problems || [])];
    $("#cleanup-msg").textContent = messages.join(" ") || (action === "preview" ? "Preview complete. No changes made." : "Check complete. No changes were needed.");
  } catch (e) { $("#cleanup-msg").textContent = e.message; }
  finally { cleanupBusy = false; cleanupButtons(); }
}
$("#cleanup-refresh").addEventListener("click", loadCleanup);
$("#cleanup-preview").addEventListener("click", () => cleanupAction("preview"));
$("#cleanup-run").addEventListener("click", () => cleanupAction("run"));
$("#cleanup-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  if (cleanupBusy) return;
  const msg = $("#cleanup-save-msg");
  let patch;
  try { patch = cleanupPolicy(ev.target); } catch (e) { msg.textContent = e.message; return; }
  cleanupBusy = true; cleanupButtons(); msg.textContent = "Saving…";
  try {
    const result = await api("/api/settings", patch);
    msg.textContent = (result.problems || []).length ? "Policy saved. " + result.problems.join(" ") : "Cleanup policy saved.";
    await load();
  } catch (e) { msg.textContent = e.message; }
  finally { cleanupBusy = false; cleanupButtons(); }
  await loadCleanup();
});
