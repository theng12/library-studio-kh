// Runs only in the feature-built (--features webview-check) hidden, ephemeral
// native webview. Every distinguished outcome is reported separately: run nonce,
// fixture binding, clipboard feedback vs an actual readback, and exact folder
// equality. The Rust side re-checks all of it; a partial report cannot pass.
addEventListener("DOMContentLoaded", () => {
  const RUN_NONCE = "__TEST_NONCE__";
  const report = { nonce: RUN_NONCE, engine: navigator.userAgent, interaction: "DOM click in hidden WKWebView", pickerMode: "remote-in-app" };
  const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
  async function wait(check, description) {
    for (let n = 0; n < 150; n++) { const result = check(); if (result) return result; await sleep(100); }
    throw new Error("Timed out: " + description);
  }
  const finish = ok => { location.href = "/__library_webview_check?ok=" + Number(ok) + "&report=" + encodeURIComponent(JSON.stringify(report)); };
  (async () => {
    if (!sessionStorage.getItem("native-check-auth")) {
      const response = await fetch("/api/auth/login", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "X-Library-Request": "1" }, body: JSON.stringify({ username: "owner", password: __TEST_PASSWORD__, remember: false }) });
      if (!response.ok) throw new Error("Fixture sign-in failed: HTTP " + response.status);
      sessionStorage.setItem("native-check-auth", "1");
      location.reload();
      return;
    }
    // Fixture binding, asked of the server itself: its self-reported URL must be
    // this run's origin, and its served settings must belong to this run's
    // fixture work directory. Titles and file names are never proof.
    const stateResponse = await fetch("/api/state", { credentials: "same-origin", headers: { "X-Library-Request": "1" } });
    if (!stateResponse.ok) throw new Error("Fixture state failed: HTTP " + stateResponse.status);
    const state = await stateResponse.json();
    report.fixtureBinding = {
      localUrl: typeof state.localUrl === "string" ? state.localUrl : "",
      downloadPath: state.settings?.downloadPath || "",
      libraryPath: state.settings?.libraryPath || ""
    };
    await wait(() => document.querySelector("#settings-tab-general"), "app initialization");
    const expected = ["#settings-downloadpath", "#settings-librarypath", "#view-cleanup input[name=managedTrashPath]", ...["appDir", "downloadPath", "root.sonarr", "root.radarr", "root.lidarr", "root.whisparr"].map(name => '#view-help input[name="' + name + '"]')];
    report.pathControls = expected.map(selector => {
      const input = document.querySelector(selector);
      return { selector, readOnly: !!input?.readOnly, chooser: input?.nextElementSibling?.classList.contains("folder-picker-choose") || false };
    });
    if (report.pathControls.some(item => !item.readOnly || !item.chooser)) throw new Error("A required path control is editable or lacks a chooser");
    document.querySelector("button[data-view=browse]").click();
    const card = await wait(() => document.querySelector(".libgrid-card, .libgrid-row"), "library fixture");
    card.click();
    const field = await wait(() => document.querySelector(".libgrid-path-box input"), "selectable path field");
    document.querySelector(".libgrid-copy-path").click();
    const status = await wait(() => document.querySelector(".libgrid-path-msg")?.textContent, "clipboard result");
    report.controls = report.pathControls.length;
    report.chosen = report.pathControls.filter(item => item.readOnly && item.chooser).length;
    // Feedback message and a demonstrated clipboard round-trip are separate facts:
    // "Copied." only claims success, and an error message only proves the error path.
    let readbackOk = false;
    let readbackError = null;
    try {
      const text = await navigator.clipboard.readText();
      readbackOk = text === field.value;
      if (!readbackOk) readbackError = "clipboard readback did not equal the selected path";
    } catch (e) {
      readbackError = String(e?.message || e);
    }
    report.clipboard = {
      feedback: status,
      state: document.querySelector(".libgrid-path-msg")?.dataset.state || "",
      readbackOk,
      readbackError
    };
    if (!field.readOnly) throw new Error("The path field became editable");
    if (report.clipboard.state !== "ok" || !readbackOk) throw new Error("Clipboard write/readback was not demonstrated: " + (readbackError || status));
    document.querySelector("button[data-view=settings]").click();
    document.querySelector("#settings-tab-general").click();
    document.querySelector("#settings-downloadpath + .folder-picker-choose").click();
    await wait(() => document.querySelector("#folder-picker-dialog")?.open && document.querySelector(".folder-picker-dir"), "in-app folder browser");
    // Pick a fixture directory, not a mount volume: volumes always list first.
    const dir = document.querySelector(".folder-picker-dir:not([data-volume])");
    if (!dir) throw new Error("The fixture download folder has no subdirectory to choose");
    const pickExpected = dir.title;
    dir.click();
    await wait(() => document.querySelector(".folder-picker-msg")?.textContent === "", "directory navigation");
    await wait(() => !document.querySelector("[data-picker-use]").disabled, "selectable directory");
    document.querySelector("[data-picker-use]").click();
    await wait(() => !document.querySelector("#folder-picker-dialog").open, "picker completion");
    const picked = document.querySelector("#settings-downloadpath");
    const pickStatus = document.querySelector("#downloadPath-picker-status, #settings-downloadpath-picker-status")?.textContent || "";
    report.folder = { expected: pickExpected, chosen: picked.value, exact: picked.value === pickExpected, readOnly: picked.readOnly, status: pickStatus };
    report.picker = pickStatus;
    if (!picked.readOnly || !report.folder.exact) throw new Error("Picked folder was not the exact selected directory shown read-only");
    finish(true);
  })().catch(error => { report.error = error.message; finish(false); });
});
