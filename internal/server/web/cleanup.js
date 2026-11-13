"use strict";

const CLEANUP_NUMBERS = {
  stagedDays: [1, 365], managedTrashDays: [0, 365], nativeTrashDays: [1, 3650],
  pressureFreeGB: [0, 10000000], seedRatio: [0, 1000000], seedMinutes: [0, 5256000],
};
const CLEANUP_PHASES = { staging: "Download folder", managed_trash: "Library’s _trash", native_trash: "macOS Trash", removed: "Removed", restored: "Restored" };
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
  cleanupEffect();
}

// The numbers interact: stages are cumulative days from the verified import.
function cleanupEffect() {
  const form = $("#cleanup-form"), effect = $("#cleanup-effect");
  if (!form || !effect) return;
  const number = (name) => { const n = Number(form.elements[name]?.value); return Number.isFinite(n) && n >= 0 ? n : null; };
  const staged = number("stagedDays"), managed = number("managedTrashDays"), native = number("nativeTrashDays");
  const threshold = number("pressureFreeGB");
  if (staged == null || managed == null || native == null) { effect.textContent = "Fill in the three waits to see the effect."; return; }
  const total = staged + managed;
  const parts = [
    "A verified copy leaves the download folder after " + staged + (staged === 1 ? " day" : " days") + ".",
    "Library’s _trash after " + total + (total === 1 ? " day" : " days") + " from import.",
    "The system Trash " + native + (native === 1 ? " day" : " days") + " after that.",
    "Only then can it be removed for good.",
  ];
  if (threshold > 0) parts.push("Below " + threshold + " GB free it may skip these waits, oldest verified import first.");
  effect.textContent = parts.join(" ");
}

function cleanupCanRun(status) {
  return !!status?.enabled && !!status?.engineAvailable && !(status.engineBlocked || []).length;
}

function cleanupButtons() {
  for (const id of ["#cleanup-save", "#cleanup-preview", "#cleanup-refresh"]) $(id).disabled = cleanupBusy;
  $("#cleanup-run").disabled = cleanupBusy || !cleanupCanRun(cleanupState);
  for (const button of document.querySelectorAll("[data-cleanup-restore]")) button.disabled = cleanupBusy;
}

function cleanupDaysLeft(due) {
  if (!due) return "";
  const ms = new Date(due) - Date.now();
  if (Number.isNaN(ms)) return "";
  if (ms <= 0) return "Due now";
  const days = Math.ceil(ms / 86400000);
  return days === 1 ? "1 day left" : days + " days left";
}

function cleanupProof(item) {
  const dialog = $("#cleanup-proof");
  facts($("#cleanup-proof-facts"), [
    ["Download", item.name || "Not reported"],
    ["Managed by", item.app || "Not reported"],
    ["Stage", CLEANUP_PHASES[item.phase] || item.phase || "Not reported"],
    ["Verified import", item.verifiedImport ? stamp(item.verifiedImport) : "Not reported"],
    ["Library file", item.importPath || "Not reported"],
    ["Verified files", Number.isInteger(item.files) ? String(item.files) : "Not reported"],
    ["Original local path", item.sourcePath || "Not reported"],
    ["Managed copy", item.managedPath || "—"],
    ["System Trash copy", item.nativePath || "—"],
    ["Restore to", item.restorePath || "Not reported"],
    ["Next due", item.nextDue ? stamp(item.nextDue) + " · " + cleanupDaysLeft(item.nextDue) : "Not reported"],
    ["Blocker", (item.blocked || []).join(" ") || "None recorded"],
  ]);
  if (!dialog.open) dialog.showModal();
}

function cleanupRender(status) {
  cleanupState = status;
  const body = $("#cleanup-status");
  body.replaceChildren();
  const summary = mediaNode("dl", null, "facts");
  facts(summary, [
    ["Policy", status.enabled ? "On · only proven eligible imports can move" : "Off · no automatic cleanup"],
    ["Free space", Number.isFinite(status.freeBytes) ? bytes(status.freeBytes) : "Not reported"],
    ["Download folder", status.stagedDays + (status.stagedDays === 1 ? " day" : " days") + " after a verified import"],
    ["Library’s _trash", status.managedTrashDays + " extra days · " + status.totalDays + " days from import"],
    ["macOS Trash", status.nativeTrashDays + " days, or earlier below the free-space threshold"],
    ["Free-space threshold", status.pressureFreeGB ? status.pressureFreeGB + " GB" : "Off"],
    ["Skip staging waits under pressure", status.pressureBypassRetention && status.pressureFreeGB ? "On · oldest completed, NAS-verified copies first" : "Off"],
    ["Managed folder", status.managedTrashPath || "Not reported"],
    ["Next due", status.nextDue ? stamp(status.nextDue) + " · " + cleanupDaysLeft(status.nextDue) : "Not known: no tracked copy has a due time yet"],
  ]);
  body.append(summary);
  const blockers = [...(status.engineBlocked || []), ...(status.blocked || [])];
  if (!status.engineAvailable && !blockers.length) blockers.push("Staged cleanup is unavailable on this computer.");
  if (blockers.length) body.append(mediaNode("p", blockers.join(" "), "notice"));
  body.append(mediaNode("p", "Nothing is deleted for good from the download folder or Library’s _trash. Every permanent removal happens only from the system Trash, after its wait, with the library copy checked again first.", "muted"));

  const wrap = mediaNode("div", null, "table-wrap");
  const table = mediaNode("table", null, "page-table");
  const head = document.createElement("thead"), headRow = document.createElement("tr");
  for (const name of ["Download", "Stage", "Size", "Next due", "Proposed action", "Actions"]) {
    const th = mediaNode("th", name); th.scope = "col"; headRow.append(th);
  }
  head.append(headRow);
  const tbody = document.createElement("tbody");
  for (const item of status.items || []) {
    const tr = document.createElement("tr");
    const name = mediaNode("td", item.name || "Untitled");
    name.dataset.label = "Download";
    const phase = mediaNode("td");
    phase.dataset.label = "Stage";
    phase.append(mediaNode("span", CLEANUP_PHASES[item.phase] || item.phase || "Not reported", "badge"));
    const size = mediaNode("td", bytes(item.bytes || 0));
    size.dataset.label = "Size";
    const done = item.phase === "removed" ? "Done" : item.phase === "restored" ? "Restored" : "Not reported";
    const due = mediaNode("td", item.nextDue ? stamp(item.nextDue) + " · " + cleanupDaysLeft(item.nextDue) : done);
    due.dataset.label = "Next due";
    const proposed = mediaNode("td");
    proposed.dataset.label = "Proposed action";
    proposed.append(mediaNode("span", item.proposed || "No action proposed"));
    if ((item.blocked || []).length) proposed.append(mediaNode("p", item.blocked.join(" "), "muted"));
    const actions = mediaNode("td", null, "cleanup-actions");
    actions.dataset.label = "Actions";
    const proof = mediaNode("button", "Show proof", "small");
    proof.type = "button";
    proof.setAttribute("aria-label", "Show import proof for " + (item.name || "this download"));
    proof.addEventListener("click", () => cleanupProof(item));
    actions.append(proof);
    if (["managed_trash", "native_trash"].includes(item.phase)) {
      const restore = mediaNode("button", "Restore", "small");
      restore.type = "button";
      restore.dataset.cleanupRestore = item.hash;
      restore.setAttribute("aria-label", "Restore the local copy of " + (item.name || "this download"));
      restore.addEventListener("click", () => cleanupAction("restore", { hash: item.hash }));
      actions.append(restore);
    }
    tr.append(name, phase, size, due, proposed, actions);
    tbody.append(tr);
  }
  table.append(head, tbody);
  wrap.append(table);
  body.append((status.items || []).length ? wrap : mediaNode("p", "No tracked downloads. Preview checks for eligible verified imports.", "empty"));
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
    $("#cleanup-msg").textContent = "Saved status. Use Preview to verify current files and the next proposed action.";
  } catch (e) {
    cleanupState = null;
    $("#cleanup-msg").textContent = e.message;
  } finally { cleanupBusy = false; cleanupButtons(); }
}

async function cleanupAction(action, request = {}) {
  if (cleanupBusy) return;
  cleanupBusy = true; cleanupButtons();
  $("#cleanup-msg").textContent = action === "preview" ? "Verifying imports and local copies. Preview changes nothing…" : action === "restore" ? "Restoring the local copy…" : "Checking and applying eligible cleanup…";
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
$("#cleanup-form").addEventListener("input", cleanupEffect);
$("#cleanup-proof .close").addEventListener("click", () => $("#cleanup-proof").close());
$("#cleanup-proof").addEventListener("click", (ev) => { if (ev.target === ev.currentTarget) ev.currentTarget.close(); });
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
