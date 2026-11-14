"use strict";
// Small conventional keyboard map for the current screen. Native text editing
// (cut, copy, paste, undo, redo, select-all in a field, Tab, Enter, Escape) is
// left to the focused control and the platform menu.
window.libraryKeyboard = (doc) => {
  const searchFor = {
    activity: "#act-q", downloads: "#dl-q", browse: "#lib-q",
    media: "#media-child-q", find: "#find-q",
  };
  const openDialog = () => [...doc.querySelectorAll("dialog")].find((d) => d.open);
  const isMac = /mac|iphone|ipad/i.test(navigator.platform || navigator.userAgent || "");
  // One primary modifier per platform, and never both at once.
  const primary = (ev) => (isMac ? ev.metaKey && !ev.ctrlKey : ev.ctrlKey && !ev.metaKey);
  const TEXT_INPUT = ["text", "search", "url", "tel", "password", "email", "number",
    "date", "time", "datetime-local", "month", "week", "color", "file"];
  // A text-entry control keeps every binding. Checkboxes and radio buttons hold
  // no text, so a focused download tick box still allows the list shortcut.
  function editable(t) {
    if (t?.isContentEditable) return true;
    if (!(t instanceof Element)) return false;
    if (t.closest("[contenteditable]:not([contenteditable=false])")) return true;
    const field = t.closest("input, textarea, select");
    if (!field) return false;
    if (field.tagName !== "INPUT") return true;
    return TEXT_INPUT.includes((field.type || "text").toLowerCase());
  }
  const currentView = () => (doc.querySelector(".view:not([hidden])")?.id || "").replace(/^view-/, "");
  const gateOpen = () => !doc.getElementById("gate")?.hidden;
  const shellOpen = () => !doc.getElementById("app-shell")?.hidden;
  const available = () => shellOpen() && !gateOpen() && !openDialog();

  // Help screen labels follow the platform the visitor is actually on.
  function platformLabels() {
    const mod = isMac ? "Command" : "Ctrl";
    const withMod = (key) => mod + " + " + key;
    for (const [id, label] of [
      ["kbd-mod", mod], ["kbd-find", withMod("F")], ["kbd-settings", withMod(",")],
      ["kbd-help", withMod("?")], ["kbd-refresh", withMod("R")], ["kbd-select-all", withMod("A")],
    ]) { const el = doc.getElementById(id); if (el) el.textContent = label; }
  }

  // Keyboard navigation lands on the destination screen's own heading, so focus
  // never stays on the control that screen just hid.
  function focusHeading(view) {
    const h = doc.querySelector("#view-" + view + " h1");
    if (!h) return;
    if (h.getAttribute("tabindex") == null) h.setAttribute("tabindex", "-1");
    h.focus();
  }

  function selectVisibleDownloads() {
    const list = doc.getElementById("dl");
    if (!list || list.hidden) return false;
    const all = list.querySelector("[data-select-all]");
    if (!all || all.disabled) return false;
    // One header checkbox drives the table's own select-all; ticking every row
    // box separately would rebuild the table between boxes.
    if (!all.checked) { all.checked = true; all.dispatchEvent(new Event("change", { bubbles: true })); }
    return true;
  }

  function handleKey(ev) {
    // A focused text control keeps every binding: Mac Ctrl+A/Ctrl+F movement,
    // Windows AltGr (Ctrl+Alt) typing and all Alt combinations stay untouched.
    if (!primary(ev) || ev.altKey || (ev.shiftKey && ev.key !== "?")
      || ev.isComposing || ev.defaultPrevented || ev.repeat || ev.keyCode === 229) return;
    if (editable(ev.target) || !available()) return;
    // Caps Lock reports the letter capitalised without shiftKey: compare folded.
    const key = ev.key.toLowerCase();

    if (key === ",") { ev.preventDefault(); show("settings"); focusHeading("settings"); return; }
    if (key === "?") { ev.preventDefault(); show("help"); focusHeading("help"); return; }
    if (key === "r") {
      const view = currentView();
      if (view === "downloads") { ev.preventDefault(); loadDownloads(true); }
      else if (view === "browse") { ev.preventDefault(); loadLibrary(); }
      else if (view === "activity") { ev.preventDefault(); load(); }
      return;
    }
    if (key === "f") {
      const input = searchFor[currentView()] && doc.querySelector(searchFor[currentView()]);
      // The media search box exists even where its filter bar is hidden (film
      // details), so an invisible or disabled field must never be hijacked.
      if (!input || input.disabled || input.closest("[hidden]")) return;
      ev.preventDefault(); input.focus(); input.select?.(); return;
    }
    // Downloads and My library only, and only with focus inside the list, so a
    // hidden ancestor view, the gate or an open dialog never selects behind the
    // scenes.
    if (key === "a" && currentView() === "downloads" && ev.target?.closest?.("#dl")
      && selectVisibleDownloads()) ev.preventDefault();
    if (key === "a" && currentView() === "browse" && ev.target?.closest?.("#library-host")
      && window.librarySelectAllShown?.()) ev.preventDefault();
  }

  doc.addEventListener("keydown", handleKey);
  platformLabels();
  return { handleKey, editable, primary, focusHeading, gateOpen, shellOpen, currentView, openDialog, selectVisibleDownloads, platformLabels };
};