"use strict";

const CLEANUP_NUMBERS = {
  stagedDays: [1, 365], managedTrashDays: [0, 365], nativeTrashDays: [1, 3650],
  pressureFreeGB: [0, 10000000], seedRatio: [0, 1000000], seedMinutes: [0, 5256000],
};
const CLEANUP_PHASE_TONES = { staging: "info", managed_trash: "info", native_trash: "info", removed: "muted", restored: "ok" };
const CLEANUP_PHASES = { staging: "Download folder", managed_trash: "Library’s _trash", native_trash: "macOS Trash", removed: "Removed", restored: "Restored" };
// What each number shows when nothing is saved: the owner sees a value, never a blank box.
// Mirrors config.Default() so a field is never blank and never shows a number the
// engine would not use. tests/cleanup-policy.test.mjs keeps the two in step.
const CLEANUP_DEFAULTS = { stagedDays: 3, managedTrashDays: 4, nativeTrashDays: 30, pressureFreeGB: 50, seedRatio: 2, seedMinutes: 2880 };
let cleanupBusy = false;
let cleanupState = null;
let cleanupSerial = 0;
let cleanupFocus = null; // where focus goes after a row action re-renders the table
let cleanupProofReturn = null; // the Show proof control that opened the dialog
let cleanupLastResult = "";
let cleanupSavedPath = ""; // the last saved folder; a typed path that fails its check keeps this one
const cleanupExpanded = {}; // region id -> expanded, once the owner chooses; a long block starts collapsed

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
  for (const [key, fallback] of Object.entries(CLEANUP_DEFAULTS)) {
    const value = settings[key];
    form.elements[key].value = value == null || value === "" ? fallback : value;
  }
  form.elements.stagedCleanupOn.checked = !!settings.stagedCleanupOn;
  form.elements.pressureBypassRetention.checked = !!settings.pressureBypassRetention;
  form.elements.managedTrashPath.value = settings.managedTrashPath || "";
  cleanupSavedPath = settings.managedTrashPath || "";
  cleanupLockNumbers();
  cleanupEffect();
}

// The numbers are the policy behind the switch: always shown, editable only while it is on.
function cleanupLockNumbers() {
  const form = $("#cleanup-form");
  const on = !!form.elements.stagedCleanupOn.checked;
  for (const key of Object.keys(CLEANUP_DEFAULTS)) form.elements[key].disabled = !on;
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
  // A row with a saved blocker keeps its Remove now disabled even when idle.
  for (const button of document.querySelectorAll("[data-cleanup-remove-now]")) {
    button.disabled = cleanupBusy || button.dataset.blocked === "1";
  }
}

function cleanupDaysLeft(due) {
  if (!due) return "";
  const ms = new Date(due) - Date.now();
  if (Number.isNaN(ms)) return "";
  if (ms <= 0) return "Due now";
  const days = Math.ceil(ms / 86400000);
  return days === 1 ? "1 day left" : days + " days left";
}

function cleanupProof(item, opener) {
  cleanupProofReturn = opener || document.activeElement || null;
  const dialog = $("#cleanup-proof");
  facts($("#cleanup-proof-facts"), [
    ["Download", item.name || "Not reported"],
    ["Managed by", item.app || "Not reported"],
    ["Stage", CLEANUP_PHASES[item.phase] || item.phase || "Not reported"],
    ["Verified import", item.verifiedImport ? stamp(item.verifiedImport) : "Not reported"],
    ["Verification", item.verified === "hash" ? "SHA-256 hash match" : item.verified === "size" ? "Size and readback match" : "Method not recorded"],
    ["Proof hash", (item.verifiedHashes || []).join(", ") || "Not recorded"],
    ["Library file", (item.importVolume ? libraryVolumeLabel(item.importVolume) + " · " : "") + (item.importPath || "Not reported")],
    ["Verified files", Number.isInteger(item.files) ? String(item.files) : "Not reported"],
    ["Original local path", item.sourcePath || "Not reported"],
    ["Managed copy", item.managedPath || "—"],
    ["System Trash copy", item.nativePath || "—"],
    ["Returns to", item.restorePath || "Not reported"],
    ["Next due", item.nextDue ? stamp(item.nextDue) + " · " + cleanupDaysLeft(item.nextDue) : "Not reported"],
    ["Blocker", (item.blocked || []).join(" ") || "None recorded"],
  ]);
  if (!dialog.open) dialog.showModal();
}

// The long blocks on this page collapse. The owner's choice lasts while the page
// is open; until then a block that runs long starts collapsed.
function cleanupRunsLong(region) {
  if ((region.scrollHeight || 0) > 360) return true; // measured once it is laid out
  return region.querySelectorAll("dt, tr, label, [data-companion-app]").length > 6; // counted while hidden
}

function cleanupCollapseApply(button, region) {
  if (!button || !region) return;
  const hasContent = (region.childElementCount || 0) > 0;
  button.hidden = !hasContent; // nothing to collapse → no control to press
  if (!hasContent) return;
  const open = cleanupExpanded[region.id] ?? !cleanupRunsLong(region);
  const active = typeof document !== "undefined" ? document.activeElement : null;
  const focusInside = !!(active && region.contains && region.contains(active));
  region.hidden = !open;
  button.setAttribute("aria-expanded", String(open));
  button.textContent = open ? "Show less" : "Show more";
  // A block never collapses out from under focus: it lands on the control that stays.
  if (!open && focusInside) button.focus();
}

function cleanupCollapseToggle(button, region) {
  cleanupExpanded[region.id] = region.hidden; // hidden → open it; showing → collapse it
  cleanupCollapseApply(button, region);
}

function cleanupRender(status) {
  cleanupState = status;
  const body = $("#cleanup-status");
  const blocked = $("#cleanup-blockers"); // a blocker is something to act on: it stays outside the collapse
  body.replaceChildren();
  blocked.replaceChildren();
  const summary = mediaNode("dl", null, "facts");
  facts(summary, [
    ["Policy", status.enabled ? "On · only proven eligible imports can move" : "Off · no automatic cleanup"],
    ["Free space where Library’s _trash lives", Number.isFinite(status.freeBytes) ? bytes(status.freeBytes) : "Not reported"],
    ["Download folder", status.stagedDays + (status.stagedDays === 1 ? " day" : " days") + " after a verified import"],
    ["Library’s _trash", status.managedTrashDays + " extra days · " + status.totalDays + " days from import"],
    ["macOS Trash", status.nativeTrashDays + " days, or earlier below the free-space threshold"],
    ["Free-space threshold", status.pressureFreeGB ? status.pressureFreeGB + " GB" : "Off"],
    ["Skip staging waits under pressure", status.pressureBypassRetention && status.pressureFreeGB ? "On · oldest completed, NAS-verified copies first" : "Off"],
    ["Managed folder", status.managedTrashPath || "Not reported"],
    ["Next due", status.nextDue ? stamp(status.nextDue) + " · " + cleanupDaysLeft(status.nextDue) : "Not known: no tracked copy has a due time yet"],
  ]);
  body.append(summary);
  const rawBlockers = [...(status.engineBlocked || []), ...(status.blocked || [])];
  const blockers = typeof groupMessages === "function" ? groupMessages(rawBlockers) : rawBlockers;
  if (!status.engineAvailable && !blockers.length) blockers.push("Staged cleanup is unavailable on this computer.");
  if (blockers.length) blocked.append(mediaNode("p", blockers.join(" "), "notice"));
  body.append(mediaNode("p", "Nothing is deleted for good from the download folder or Library’s _trash. Every permanent removal happens only from the system Trash, after its wait, with the library copy checked again first.", "muted"));

  const wrap = mediaNode("div", null, "table-wrap");
  // "list" gives the table the same stacked-card treatment the other mobile tables use.
  const table = mediaNode("table", null, "page-table list");
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
    const tone = CLEANUP_PHASE_TONES[item.phase];
    phase.append(mediaNode("span", CLEANUP_PHASES[item.phase] || item.phase || "Not reported", "badge" + (tone ? " dlp-tone-" + tone : "")));
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
    // Keep recovery visible; the early Remove now action stays in the overflow.
    const proof = mediaNode("button", "Show proof", "small");
    proof.type = "button";
    proof.setAttribute("aria-label", "Show import proof for " + (item.name || "this download"));
    proof.dataset.cleanupProof = item.hash;
    proof.addEventListener("click", () => cleanupProof(item, proof));
    actions.append(proof);
    const more = [];
    if (item.phase === "staging") {
      // Moves the copy into Library's _trash now, still recoverable there.
      const removeNow = mediaNode("button", null, null);
      removeNow.textContent = "Remove now";
      removeNow.type = "button";
      removeNow.dataset.cleanupRemoveNow = item.hash;
      // A saved blocker (e.g. seeding not finished) means this cannot run yet.
      if ((item.blocked || []).length > 0) {
        removeNow.dataset.blocked = "1";
        removeNow.disabled = true;
        removeNow.title = item.blocked.join(" ");
      }
      removeNow.setAttribute("aria-label", "Move the local copy of " + (item.name || "this download") + " to Library’s _trash now");
      removeNow.addEventListener("click", () => cleanupAction("remove-now", { hash: item.hash }));
      more.push(removeNow);
    }
    if (["managed_trash", "native_trash"].includes(item.phase)) {
      const restore = mediaNode("button", "Restore to download folder", "small");
      restore.type = "button";
      restore.dataset.cleanupRestore = item.hash;
      restore.setAttribute("aria-label", "Restore the local copy of " + (item.name || "this download") + " to the download folder");
      restore.addEventListener("click", () => cleanupAction("restore", { hash: item.hash }));
      actions.append(restore);
    }
    if (more.length) {
      const menu = mediaNode("details", null, "row-menu");
      const summary = document.createElement("summary");
      summary.setAttribute("aria-label", "More actions for " + (item.name || "this download"));
      summary.textContent = "⋯";
      const list = mediaNode("div", null, "row-menu-list");
      list.setAttribute("role", "menu");
      list.append(...more);
      menu.append(summary, list);
      actions.append(menu);
    }
    tr.append(name, phase, size, due, proposed, actions);
    tbody.append(tr);
  }
  table.append(head, tbody);
  wrap.append(table);
  body.append((status.items || []).length ? wrap : mediaNode("p", "No tracked downloads. Preview checks for eligible verified imports.", "empty"));
  cleanupCollapseApply($("#cleanup-status-toggle"), body);
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
    $("#cleanup-msg").textContent = cleanupLastResult || "Showing the saved policy. Preview runs a dry run.";
  } catch (e) {
    cleanupState = null;
    $("#cleanup-msg").textContent = typeof plainHostError === "function" ? plainHostError(e) : e.message;
  } finally { cleanupBusy = false; cleanupButtons(); }
}

async function cleanupAction(action, request = {}) {
  if (cleanupBusy) return;
  cleanupBusy = true; cleanupButtons();
  $("#cleanup-msg").textContent = action === "preview" ? "Verifying imports and local copies. Preview changes nothing…" : action === "restore" ? "Restoring the local copy…" : "Checking and applying eligible cleanup…";
  try {
    const result = await api("/api/cleanup/" + action, request);
    cleanupFocus = { hash: request.hash || "", action };
    cleanupRender(result.status);
    const messages = [...(result.actions || []), ...(result.problems || [])];
    $("#cleanup-msg").textContent = messages.join(" ") || (action === "preview" ? "Preview complete. No changes made." : "Check complete. No changes were needed.");
    if (action !== "preview") {
      cleanupLastResult = $("#cleanup-msg").textContent;
      await load(); // Activity and other pages move on too
    }
  } catch (e) {
    const message = typeof plainHostError === "function" ? plainHostError(e) : e.message;
    $("#cleanup-msg").textContent = message;
    // Re-read the saved state so the refusal's blocker is visible on the row.
    try { cleanupRender(await api("/api/cleanup/status")); } catch {}
    $("#cleanup-msg").textContent = message;
  }
  finally {
    cleanupBusy = false;
    cleanupButtons();
    if (cleanupFocus) {
      const f = cleanupFocus;
      cleanupFocus = null;
      const sel = f.action === "restore" ? '[data-cleanup-restore="' + CSS.escape(f.hash) + '"]'
        : '[data-cleanup-remove-now="' + CSS.escape(f.hash) + '"]';
      const target = document.querySelector(sel);
      (target && !target.disabled ? target : $("#cleanup-refresh"))?.focus();
    }
  }
}

// A typed folder is checked on its own: a bad path never blocks the rest of the save.
async function cleanupCheckPath(patch) {
  if (typeof window === "undefined" || !window.folderPicker?.checkPath) return "";
  const problem = await window.folderPicker.checkPath(patch.managedTrashPath || "");
  const line = typeof document !== "undefined" ? document.getElementById("managedTrashPath-picker-status") : null;
  if (problem) {
    patch.managedTrashPath = cleanupSavedPath; // the saved folder keeps working; the typed one is not stored
    if (line) {
      line.textContent = problem;
      line.dataset.invalid = "1";
      const owner = line.closest?.("details");
      if (owner) owner.open = true; // the reason must be visible where the field lives
    }
  } else if (line && line.dataset.invalid === "1") {
    line.textContent = "";
    delete line.dataset.invalid;
  }
  return problem;
}

$("#cleanup-refresh").addEventListener("click", loadCleanup);
$("#cleanup-preview").addEventListener("click", () => cleanupAction("preview"));

// The policy can start time-based deletion, so it gets the same in-app summary
// and confirmation as a Remove/Clear, never a bare button.
function cleanupRunSummary(status) {
  if (!status) return "The saved cleanup policy will be checked.";
  const stages = (status.phases || [])
    .map((phase) => CLEANUP_PHASES[phase.name] + " (" + phase.days + (phase.days === 1 ? " day" : " days") + ")")
    .join(", ");
  return "Library will check every tracked copy and apply any stage that is due: " + stages + ". "
    + (status.pressureFreeGB ? "Below " + status.pressureFreeGB + " GB free it may skip the waits, oldest verified import first. " : "")
    + "Nothing is deleted for good outside those waits.";
}

function confirmCleanupRun() {
  return new Promise((resolve) => {
    const dialog = document.createElement("dialog");
    dialog.className = "confirm";
    dialog.setAttribute("aria-labelledby", "cleanup-run-title");
    const heading = document.createElement("h2");
    heading.id = "cleanup-run-title";
    heading.textContent = "Run cleanup now?";
    const head = document.createElement("div");
    head.className = "d-head";
    const headClose = document.createElement("button");
    headClose.type = "button"; headClose.className = "close"; headClose.setAttribute("aria-label", "Close");
    headClose.textContent = "×";
    headClose.addEventListener("click", () => dialog.close());
    head.append(heading, headClose);
    const text = document.createElement("p");
    text.textContent = cleanupRunSummary(cleanupState);
    const actions = document.createElement("div");
    actions.className = "actions";
    const cancel = document.createElement("button");
    cancel.type = "button"; cancel.className = "small"; cancel.textContent = "Cancel";
    const go = document.createElement("button");
    go.type = "button"; go.className = "small primary"; go.textContent = "Run cleanup";
    actions.append(cancel, go);
    dialog.append(head, text, actions);
    document.body.append(dialog);
    const opener = document.activeElement;
    let answer = false;
    cancel.addEventListener("click", () => dialog.close());
    go.addEventListener("click", () => { answer = true; dialog.close(); });
    dialog.addEventListener("close", () => {
      opener?.focus?.();
      requestAnimationFrame(() => { dialog.remove(); opener?.focus?.(); setTimeout(() => { opener?.focus?.(); resolve(answer); }, 50); });
    });
    dialog.showModal();
  });
}

$("#cleanup-run").addEventListener("click", async () => {
  if (await confirmCleanupRun()) cleanupAction("run");
});
$("#cleanup-form").addEventListener("input", cleanupEffect);
$("#cleanup-form").addEventListener("change", (ev) => {
  if (ev.target?.name === "stagedCleanupOn") cleanupLockNumbers();
  // A folder's own reason clears the moment its value changes; Save re-checks it.
  if (ev.target?.name === "managedTrashPath") {
    const line = typeof document !== "undefined" ? document.getElementById("managedTrashPath-picker-status") : null;
    if (line && line.dataset.invalid === "1") { line.textContent = ""; delete line.dataset.invalid; }
  }
});
// One control per long block; the button lives in the card head, outside the region it hides.
for (const [toggleId, regionId] of [["cleanup-status-toggle", "cleanup-status"], ["cleanup-companions-toggle", "companion-settings"]]) {
  const button = $("#" + toggleId), region = $("#" + regionId);
  if (button?.addEventListener && region) button.addEventListener("click", () => cleanupCollapseToggle(button, region));
}
// The companions fill in after this script; their default waits for the real content.
if (typeof MutationObserver === "function") {
  const box = $("#companion-settings"), toggle = $("#cleanup-companions-toggle");
  if (box && toggle) new MutationObserver(() => cleanupCollapseApply(toggle, box)).observe(box, { childList: true });
}
// A backdrop click must reach the same focus restore as the close control.
$("#cleanup-proof").__libraryBackdropClose = () => $("#cleanup-proof").close();
$("#cleanup-proof .close").addEventListener("click", () => $("#cleanup-proof").close());
// Native restoration can land inside a dialog whose row was re-rendered; put
// focus back on the control that opened it, now and after the frame settles.
$("#cleanup-proof").addEventListener("close", () => {
  const opener = cleanupProofReturn;
  cleanupProofReturn = null;
  const restore = () => {
    if (opener && opener.isConnected) { opener.focus(); return; }
    const same = opener?.dataset?.cleanupProof
      ? document.querySelector('[data-cleanup-proof="' + CSS.escape(opener.dataset.cleanupProof) + '"]')
      : null;
    (same || document.querySelector("[data-cleanup-proof]"))?.focus();
  };
  restore();
  requestAnimationFrame(restore);
});
$("#cleanup-proof").addEventListener("click", (ev) => { if (ev.target === ev.currentTarget) ev.currentTarget.close(); });
$("#cleanup-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  if (cleanupBusy) return;
  const msg = $("#cleanup-save-msg");
  let patch;
  try { patch = cleanupPolicy(ev.target); } catch (e) { msg.textContent = e.message; return; }
  // The rest of the policy still saves when the folder typed here is wrong.
  const typedPath = patch.managedTrashPath;
  const pathProblem = await cleanupCheckPath(patch);
  cleanupBusy = true; cleanupButtons(); msg.textContent = "Saving…";
  try {
    const result = await api("/api/settings", patch);
    msg.textContent = ((result.problems || []).length ? "Policy saved. " + result.problems.join(" ") : "Cleanup policy saved.")
      + (pathProblem ? " The folder kept its saved value — the reason sits under it." : "");
    await load();
  } catch (e) { msg.textContent = e.message; }
  finally { cleanupBusy = false; cleanupButtons(); }
  await loadCleanup();
  // loadCleanup re-fills the saved folder; the typed text and its reason stay on screen.
  if (pathProblem) $("#cleanup-form").elements.managedTrashPath.value = typedPath;
});

// One shell with Settings: the same tab row and panel pattern on this page.
if (typeof window !== "undefined" && window.librarySettingsView?.mountTabbed) {
  window.librarySettingsView.mountTabbed($("#cleanup-tabs"), [
    { id: "policy", label: "Policy", node: $("#cleanup-form") },
    { id: "status", label: "Status & preview", node: $("#cleanup-status-card") },
    { id: "companions", label: "Companion apps", node: $("#cleanup-companions") },
  ], "Cleanup sections");
}
