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
  // Chrome shows the chosen option while Alt is held; left alone otherwise.
  const nativeSelect = (el, collapse) => {
    el.focus();
    if (typeof el.select === "function") { el.select(); return; }
    const text = doc.getSelection?.();
    if (!text) return;
    const range = doc.createRange();
    range.selectNodeContents(el);
    if (collapse) range.collapse(true);
    text.removeAllRanges();
    text.addRange(range);
  };
  const currentView = () => (doc.querySelector(".view:not([hidden])")?.id || "").replace(/^view-/, "");
  const gateOpen = () => !doc.getElementById("gate")?.hidden;
  const shellOpen = () => !doc.getElementById("app-shell")?.hidden;

  // Help screen labels follow the platform the visitor is actually on.
  function platformLabels() {
    const mod = isMac ? "Command" : "Ctrl";
    const withMod = (key) => mod + " + " + key;
    for (const [id, label] of [
      ["kbd-mod", mod], ["kbd-find", withMod("F")], ["kbd-settings", withMod(",")],
      ["kbd-help", withMod("?")], ["kbd-refresh", withMod("R")], ["kbd-select-all", withMod("A")],
    ]) { const el = doc.getElementById(id); if (el) el.textContent = label; }
  }

  function selectVisibleDownloads() {
    const list = doc.getElementById("dl");
    if (!list || list.hidden) return false;
    const boxes = [...list.querySelectorAll("input[type=checkbox]")];
    if (!boxes.length) return false;
    const hadFocus = doc.activeElement?.closest?.("#dl");
    for (const box of boxes) { box.checked = true; box.dispatchEvent(new Event("change", { bubbles: true })); }
    // Rebuilding the cards can drop focus to the page; put it back in the list.
    if (hadFocus && !doc.activeElement?.closest?.("#dl")) (boxes[0] || hadFocus).focus?.();
    return true;
  }

  function handleKey(ev) {
    // A focused text control keeps every binding: Mac Ctrl+A/Ctrl+F movement,
    // Windows AltGr (Ctrl+Alt) typing and all Alt combinations stay untouched.
    const mod = ev.ctrlKey || ev.metaKey;
    // Shift only appears where the layout needs it: Cmd+Shift+/ types "?".
    const shift = ev.shiftKey && ev.key !== "?";
    if (!mod || ev.altKey || shift || ev.isComposing
      || ev.defaultPrevented || ev.repeat || ev.keyCode === 229) return;
    const editable = ev.target?.isContentEditable
      || (ev.target instanceof Element && ev.target.closest("[contenteditable=true]"))
      || (ev.target instanceof Element && ev.target.closest("input, textarea, select"));
    if (editable) return;
    const key = ev.key;
    const available = shellOpen() && !gateOpen() && !openDialog();

    if (key === "," && available) { ev.preventDefault(); show("settings"); return; }
    if (key === "?" && available) { ev.preventDefault(); show("help"); return; }
    if (key === "F5" || key === "r") {
      if (!available) return;
      const view = currentView();
      if (view === "downloads") { ev.preventDefault(); loadDownloads(true); }
      else if (view === "browse") { ev.preventDefault(); loadLibrary(); }
      else if (view === "activity") { ev.preventDefault(); load(); }
      return;
    }
    if (key === "f") {
      if (!available) return;
      const input = searchFor[currentView()] && doc.querySelector(searchFor[currentView()]);
      if (!input) return;
      ev.preventDefault(); nativeSelect(input, true); return;
    }
    if (key === "a" && ev.target?.closest?.("#dl") && selectVisibleDownloads()) {
      // Focus inside the list: tick the rendered (filtered) downloads above.
      // Anywhere else — or an empty list — the platform keeps Cmd/Ctrl+A.
      ev.preventDefault();
    }
  }

  doc.addEventListener("keydown", handleKey);
  platformLabels();
  return { handleKey, gateOpen, shellOpen, currentView, openDialog, selectVisibleDownloads, platformLabels };
};