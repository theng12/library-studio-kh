// Runs only in --webview-check's hidden, ephemeral native webview.
addEventListener("DOMContentLoaded", () => {
  const report = { engine: navigator.userAgent, interaction: "DOM click in hidden WKWebView", pickerMode: "remote-in-app" };
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
    await wait(() => document.title === "Library Studio KH" && document.querySelector("#settings-tab-general"), "app initialization");
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
    report.clipboard = { status, readOnly: field.readOnly, selected: field.selectionEnd - field.selectionStart > 0 };
    if (!field.readOnly || !(/Copied\./.test(status) || /^Could not copy — .+/.test(status))) throw new Error("Clipboard action did not report an explicit result");
    document.querySelector("button[data-view=settings]").click();
    document.querySelector("#settings-tab-general").click();
    document.querySelector("#settings-downloadpath + .folder-picker-choose").click();
    await wait(() => document.querySelector("#folder-picker-dialog")?.open && document.querySelector(".folder-picker-dir"), "in-app folder browser");
    document.querySelector(".folder-picker-dir").click();
    await wait(() => !document.querySelector("[data-picker-use]").disabled, "selectable directory");
    document.querySelector("[data-picker-use]").click();
    await wait(() => !document.querySelector("#folder-picker-dialog").open, "picker completion");
    const picked = document.querySelector("#settings-downloadpath");
    report.folder = { valuePresent: !!picked.value, readOnly: picked.readOnly, status: document.querySelector("#downloadPath-picker-status, #settings-downloadpath-picker-status")?.textContent };
    if (!report.folder.valuePresent || !report.folder.readOnly) throw new Error("Picked path was not shown read-only");
    finish(true);
  })().catch(error => { report.error = error.message; finish(false); });
});
