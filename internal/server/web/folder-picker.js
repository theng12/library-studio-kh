"use strict";
// Every path field offers the chooser and accepts a typed path; a typed value is
// checked on save (checkPath) and the reason lands beside that field. Path
// fields use the in-app directory browser on both desktop and remote pages.
// Plain script, no imports, no dependencies.

function folderPickerIsAbsolute(path) {
  const p = typeof path === "string" ? path.trim() : "";
  return p.charAt(0) === "/" || /^[A-Za-z]:[\\/]/.test(p) || p.startsWith("\\\\");
}

function folderPickerBreadcrumb(path) {
  if (!folderPickerIsAbsolute(path)) return [];
  const p = path.trim();
  if (p.charAt(0) === "/") {
    const crumbs = [{ name: "/", path: "/" }];
    let sofar = "";
    for (const part of p.split("/")) {
      if (!part) continue;
      sofar += "/" + part;
      crumbs.push({ name: part, path: sofar });
    }
    return crumbs;
  }
  // Windows: a drive root (C:\) or a network share (\\server\share), then folders.
  const parts = p.replace(/\//g, "\\").split("\\").filter(Boolean);
  const unc = p.startsWith("\\\\");
  const rootParts = unc ? parts.slice(0, 2) : parts.slice(0, 1);
  if (!rootParts.length) return [];
  let sofar = unc ? "\\\\" + rootParts.join("\\") : rootParts[0] + "\\";
  const crumbs = [{ name: sofar, path: sofar }];
  for (const part of parts.slice(rootParts.length)) {
    if (!sofar.endsWith("\\")) sofar += "\\";
    sofar += part;
    crumbs.push({ name: part, path: sofar });
  }
  return crumbs;
}

function folderPickerValidSelection(state) {
  return !!(state && folderPickerIsAbsolute(state.path));
}

(function () {
  function launchToken() {
    try { return sessionStorage.getItem("token") || ""; } catch { return ""; }
  }

  function requestHeaders(json) {
    const headers = { "X-Library-Request": "1" };
    if (json) headers["Content-Type"] = "application/json";
    const token = launchToken();
    if (token) headers["X-Library-Token"] = token;
    return headers;
  }

  async function askNative(prompt) {
    const r = await fetch("/api/pick-folder", {
      method: "POST",
      headers: requestHeaders(true),
      credentials: "same-origin",
      body: JSON.stringify({ prompt }),
    });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data.error || "The folder panel could not open.");
    return data;
  }

  async function readDirs(path) {
    const url = "/api/browse-dirs" + (path ? "?path=" + encodeURIComponent(path) : "");
    const r = await fetch(url, { headers: requestHeaders(false), credentials: "same-origin" });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data.error || "That folder could not be read.");
    return data;
  }

  // Save-time check for a typed path: full path first, then a folder that opens.
  // Returns "" when the value is fine (an empty field just clears the setting).
  async function checkPath(path) {
    const p = typeof path === "string" ? path.trim() : "";
    if (!p) return "";
    if (!folderPickerIsAbsolute(p)) return "Type a full folder path, starting with /.";
    try {
      await readDirs(p);
      return "";
    } catch (e) {
      return plainHostError(e);
    }
  }

  function setPath(input, value) {
    input.value = value;
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  }

  function button(text, className) {
    const node = document.createElement("button");
    node.type = "button";
    node.className = className;
    node.textContent = text;
    return node;
  }

  function enhance(input) {
    if (!input || input.dataset.pathPickerReady === "1") return;
    input.dataset.pathPickerReady = "1";
    // No readonly here: a typed path is allowed and is checked on save. Fields
    // that stay read-only (setup, cleanup) carry the attribute in their markup.

    const choose = button("Choose folder", "folder-picker-choose");
    const clear = button("Clear", "folder-picker-clear");
    clear.setAttribute("aria-label", "Clear " + (input.name || "folder") + " path");
    const status = document.createElement("p");
    status.className = "folder-picker-status";
    status.setAttribute("role", "status");
    status.id = (input.id || input.name || "folder") + "-picker-status";
    const describedBy = (input.getAttribute("aria-describedby") || "").split(/\s+/).filter(Boolean);
    describedBy.push(status.id);
    input.setAttribute("aria-describedby", describedBy.join(" "));

    input.insertAdjacentElement("afterend", choose);
    choose.insertAdjacentElement("afterend", clear);
    const anchor = input.closest("label") || input;
    anchor.insertAdjacentElement("afterend", status);

    choose.addEventListener("click", () => {
      pickOpener = choose;
      choosePath(input, status);
    });
    clear.addEventListener("click", () => {
      setPath(input, "");
      status.textContent = "Cleared. Save to apply.";
    });
  }

  function choosePath(input, status) {
    status.textContent = "";
    openBrowser(input, status);
  }

  let dialog = null;
  let browser = null; // { input, status, listing }
  let pickOpener = null; // the control focus returns to when the panel closes

  function buildDialog() {
    dialog = document.createElement("dialog");
    dialog.id = "folder-picker-dialog";
    dialog.className = "folder-picker-dialog";
    dialog.innerHTML = [
      '<header class="d-head"><h2>Choose a folder</h2>',
      '<button type="button" class="close" aria-label="Close" data-picker-cancel>×</button></header>',
      '<nav class="folder-picker-crumbs" aria-label="Folder path"></nav>',
      '<ul class="folder-picker-list" aria-label="Folders"></ul>',
      '<p class="folder-picker-msg" role="status"></p>',
      '<div class="folder-picker-actions">',
      '<button type="button" class="primary" data-picker-use>Choose this folder</button>',
      '<button type="button" data-picker-cancel>Cancel</button>',
      "</div>",
    ].join("");
    document.body.append(dialog);
    for (const node of dialog.querySelectorAll("[data-picker-cancel]")) node.addEventListener("click", closeDialog);
    dialog.querySelector("[data-picker-use]").addEventListener("click", useCurrent);
    dialog.addEventListener("cancel", (ev) => { ev.preventDefault(); closeDialog(); });
    // A backdrop click closes the element directly; the promise still settles.
    dialog.addEventListener("close", () => {
      const resolve = browser?.resolve;
      if (browser) browser.resolve = null;
      if (resolve) resolve(null);
      restoreOpener();
    });
    dialog.addEventListener("keydown", (ev) => {
      if (ev.key === "Escape") { ev.preventDefault(); closeDialog(); }
    });
    dialog.querySelector(".folder-picker-list").addEventListener("keydown", (ev) => {
      if (ev.key !== "ArrowDown" && ev.key !== "ArrowUp") return;
      const items = [...ev.currentTarget.querySelectorAll("button")];
      const at = items.indexOf(document.activeElement);
      if (at < 0 || !items.length) return;
      ev.preventDefault();
      items[(at + (ev.key === "ArrowDown" ? 1 : items.length - 1)) % items.length].focus();
    });
  }

  function restoreOpener() {
    const target = pickOpener || browser?.input;
    pickOpener = null;
    browser = null; // one target per close; the path field is not a second one
    if (!target?.focus) return;
    target.focus();
    // The dialog's own focus restore runs after this call, so assert again next frame.
    requestAnimationFrame(() => { if (target.isConnected) target.focus(); });
  }

  function closeDialog() {
    dialog?.close();
    const resolve = browser?.resolve;
    if (browser) browser.resolve = null;
    if (resolve) resolve(null);
    restoreOpener();
  }

  function openBrowser(input, status, resolve) {
    if (!dialog) buildDialog();
    browser = { input, status, resolve: resolve || null, listing: null };
    dialog.showModal();
    const start = input && input.value && folderPickerValidSelection({ path: input.value }) ? input.value : "";
    load(start);
  }

  // One folder, chosen either in the native panel or the in-app browser. Used by
  // actions outside a path field (for example Set location in a detail panel).
  async function pick(promptText, opener) {
    // A synthetic click leaves focus on the body, so the caller's control wins.
    pickOpener = opener || (document.activeElement !== document.body ? document.activeElement : null);
    try {
      const data = await askNative(promptText || "Choose a folder");
      if (data.path) return data.path;
      if (!data.remote) return null; // the owner cancelled
    } catch {
      // A failed panel still leaves a working chooser.
    }
    return new Promise((resolve) => openBrowser(null, null, resolve));
  }

  function setMessage(text, bad) {
    const msg = dialog.querySelector(".folder-picker-msg");
    msg.textContent = text || "";
    msg.className = "folder-picker-msg" + (bad ? " bad" : "");
  }

  async function load(path) {
    setMessage("Reading…");
    try {
      const listing = await readDirs(path);
      if (!browser) return; // the panel closed while the folder was being read
      browser.listing = listing;
      render(listing);
      setMessage("");
    } catch (e) {
      if (!browser) return;
      browser.listing = null;
      renderCrumbs([]);
      renderList([]);
      dialog.querySelector("[data-picker-use]").disabled = true;
      setMessage(e.message, true);
    }
  }

  function renderCrumbs(crumbs) {
    const nav = dialog.querySelector(".folder-picker-crumbs");
    nav.replaceChildren();
    const up = button("↑ Up", "folder-picker-up");
    up.disabled = !browser.listing?.parent;
    up.addEventListener("click", () => load(browser.listing.parent));
    nav.append(up);
    for (const crumb of crumbs) {
      const node = button(crumb.name, "folder-picker-crumb");
      node.addEventListener("click", () => load(crumb.path));
      nav.append(node);
    }
  }

  function renderList(listing) {
    const list = dialog.querySelector(".folder-picker-list");
    list.replaceChildren();
    const add = (entry, kind) => {
      const item = document.createElement("li");
      const node = button(entry.name, "folder-picker-dir");
      if (kind === "volume") node.dataset.volume = "1";
      node.title = entry.path;
      node.addEventListener("click", () => load(entry.path));
      item.append(node);
      list.append(item);
    };
    if (listing) {
      for (const entry of listing.volumes || []) add(entry, "volume");
      for (const entry of listing.dirs || []) add(entry, "dir");
    }
    (list.querySelector("button") || dialog.querySelector("[data-picker-use]")).focus();
  }

  function render(listing) {
    if (!browser) return;
    renderCrumbs(folderPickerBreadcrumb(listing.path));
    renderList(listing);
    dialog.querySelector("[data-picker-use]").disabled = !folderPickerValidSelection(listing);
  }

  function useCurrent() {
    const path = browser?.listing?.path;
    if (!folderPickerValidSelection({ path })) return;
    if (browser.input) setPath(browser.input, path);
    if (browser.status) browser.status.textContent = "Chose " + path;
    const resolve = browser.resolve;
    browser.resolve = null;
    if (resolve) resolve(path);
    closeDialog();
  }

  function enhanceAll(root) {
    if (!root?.querySelectorAll) return;
    for (const input of root.querySelectorAll("input[data-path-picker]")) enhance(input);
  }

  function start() {
    enhanceAll(document);
    if (!document.body || typeof MutationObserver !== "function") return;
    // Settings cards are mounted after this script runs; pick up new path fields.
    new MutationObserver((records) => {
      for (const record of records) for (const node of record.addedNodes) {
        if (node.nodeType === 1) enhanceAll(node);
      }
    }).observe(document.body, { childList: true, subtree: true });
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();

  window.folderPicker = {
    enhance: enhanceAll,
    breadcrumb: folderPickerBreadcrumb,
    validSelection: folderPickerValidSelection,
    open: openBrowser,
    pick,
    checkPath,
  };
})();
