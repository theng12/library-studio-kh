"use strict";
// Guided setup and first-run host/client choice. The host API owns installs,
// approvals, secrets and verified checks; this file only renders its job.
// Owner-only content is marked data-owner-only and removed from the public Help copy.

/* Pure helpers (unit tested; no DOM) */
const SETUP_APP_IDS = ["qbittorrent", "sonarr", "radarr", "lidarr", "prowlarr"];
// Optional apps only join the plan when selected; Whisparr is a native app.
const SETUP_OPTIONAL_IDS = ["recyclarr", "unpackerr", "whisparr"];
const SETUP_NATIVE_IDS = SETUP_APP_IDS.concat("whisparr");
const SETUP_ROOT_IDS = ["sonarr", "radarr", "lidarr", "whisparr"];
const SETUP_ACTIVE_STATES = ["planning", "installing", "checking", "configuring"];
const SETUP_OPEN_STATES = ["needs_open", "opened", "blocked"];
const SETUP_APP_NAMES = { qbittorrent: "qBittorrent", sonarr: "Sonarr", radarr: "Radarr", lidarr: "Lidarr", prowlarr: "Prowlarr", recyclarr: "Recyclarr", unpackerr: "Unpackerr", whisparr: "Whisparr" };

function setupClientTarget(value) {
  const raw = String(value == null ? "" : value).trim();
  if (!raw) throw new Error("Enter the other computer's web address.");
  let url;
  try { url = new URL(raw); } catch { throw new Error("Enter a full web address, like http://203.0.113.10:8080."); }
  if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("Use an http:// or https:// address.");
  if (url.username || url.password) throw new Error("Remove the username or password from the address.");
  if ((url.pathname && url.pathname !== "/") || url.search || url.hash) throw new Error("Use only the host's web address, with no page, query or key.");
  return url.protocol + "//" + url.host + "/";
}

async function setupClientGo(value, deps) {
  const url = setupClientTarget(value);
  const saved = await deps.api("/api/setup/client", { url });
  const target = setupClientTarget(saved && saved.url);
  deps.clearToken();
  deps.navigate(target);
  return target;
}

function setupString(value, fallback) {
  const text = value == null ? "" : String(value).trim();
  if (text) return text;
  return fallback == null ? "" : String(fallback);
}

function setupPort(value, fallback) {
  const raw = value == null || value === "" ? fallback : value;
  const port = Number(raw);
  if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error("Ports must be whole numbers from 1024 to 65535.");
  return port;
}

function setupProfileId(value) {
  if (value == null || value === "") return null;
  const id = Number(value);
  return Number.isInteger(id) && id > 0 ? id : null;
}

function setupProfileChoice(list, value, label) {
  const id = Number(value);
  if (!Number.isInteger(id) || id <= 0) throw new Error("Choose a music " + label + ".");
  if (!Array.isArray(list) || !list.some((profile) => Number(profile.id) === id)) {
    throw new Error("That music " + label + " is no longer offered. Refresh status and choose again.");
  }
  return id;
}

function setupPreferencesRequest(job, qualityProfileId, metadataProfileId) {
  if (!job || !job.id) throw new Error("There is no saved setup plan yet.");
  const music = job.musicProfiles || {};
  return {
    id: job.id,
    qualityProfileId: setupProfileChoice(music.qualityProfiles, qualityProfileId, "quality profile"),
    metadataProfileId: setupProfileChoice(music.metadataProfiles, metadataProfileId, "metadata profile"),
  };
}

function setupRequest(values, defaults) {
  const given = values || {}, server = defaults || {};
  const apps = [];
  for (const id of SETUP_APP_IDS.concat(SETUP_OPTIONAL_IDS)) {
    const chosen = given.apps && Object.prototype.hasOwnProperty.call(given.apps, id)
      ? !!given.apps[id]
      : Array.isArray(server.apps) ? server.apps.includes(id) : id === "qbittorrent";
    if (chosen) apps.push(id);
  }
  if (!apps.length) throw new Error("Choose at least one app to set up.");
  const roots = {}, categories = {}, ports = {};
  for (const id of SETUP_ROOT_IDS.filter((id) => id !== "whisparr" || apps.includes(id))) {
    roots[id] = setupString(given.roots && given.roots[id], server.roots && server.roots[id]);
    categories[id] = setupString(given.categories && given.categories[id], server.categories && server.categories[id]);
  }
  for (const id of SETUP_NATIVE_IDS.filter((id) => id !== "whisparr" || apps.includes(id))) ports[id] = setupPort(given.ports && given.ports[id], server.ports && server.ports[id]);
  const usedPorts = new Map();
  for (const id of apps) {
    if (!SETUP_NATIVE_IDS.includes(id)) continue; // helpers have no port
    const other = usedPorts.get(ports[id]);
    if (other) throw new Error((SETUP_APP_NAMES[other] || other) + " and " + (SETUP_APP_NAMES[id] || id) + " both use port " + ports[id] + ". Choose a different port for one of them.");
    usedPorts.set(ports[id], id);
  }
  const renameTV = given.renameTV == null ? server.renameTV !== false : !!given.renameTV;
  const startAtLogin = given.startAtLogin == null ? server.startAtLogin === true : !!given.startAtLogin;
  return {
    apps,
    appDir: setupString(given.appDir, server.appDir),
    downloadPath: setupString(given.downloadPath, server.downloadPath),
    roots, ports, categories, renameTV, startAtLogin,
    musicQualityProfileId: setupProfileId(given.musicQualityProfileId == null ? server.musicQualityProfileId : given.musicQualityProfileId),
    musicMetadataProfileId: setupProfileId(given.musicMetadataProfileId == null ? server.musicMetadataProfileId : given.musicMetadataProfileId),
  };
}

function setupSize(bytes) {
  const value = Number(bytes);
  if (!Number.isFinite(value) || value <= 0) return "Size not published";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value, unit = 0;
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return (unit === 0 || size >= 10 ? Math.round(size) : size.toFixed(1)) + " " + units[unit];
}

function setupJobView(job) {
  if (!job || !job.id) {
    return { hasJob: false, state: "not_started", active: false, plan: true, run: false, retry: false, check: false, open: [], sources: false, settings: false, restart: false, ready: false, edit: false, preferences: false };
  }
  const state = String(job.state || "not_started");
  const apps = Array.isArray(job.apps) ? job.apps : [];
  const appState = (s) => apps.some((a) => a && a.state === s);
  const active = SETUP_ACTIVE_STATES.includes(state);
  return {
    hasJob: true, state, active,
    plan: false,
    run: state === "planned",
    retry: ["failed", "interrupted"].includes(state),
    check: !active && ["needs_approval", "needs_sources", "blocked", "needs_restart", "ready"].includes(state),
    open: active ? [] : apps.filter((a) => a && a.managed === true && SETUP_OPEN_STATES.includes(a.state)).map((a) => a.id),
    sources: state === "needs_sources" || appState("needs_sources"),
    settings: appState("needs_connection"),
    restart: state === "needs_restart" || appState("needs_restart"),
    ready: state === "ready",
    edit: !active,
    preferences: state === "needs_preferences",
  };
}

function setupExistingApp(app) {
  if (!app || app.managed === true) return false;
  return app.state === "adopt" || app.state === "needs_connection" || app.state === "blocked" || app.connectionDetected === true;
}

function setupCheckState(state) {
  if (state === "passed") return "ok";
  if (state === "failed") return "bad";
  return "";
}

function setupCheckLabel(state) {
  return { passed: "Passed", failed: "Failed", not_run: "Not run", not_checked: "Not checked" }[state] || state || "Not checked";
}

function setupJobRequest(job, action, app) {
  if (!job || !job.id) throw new Error("There is no saved setup plan yet.");
  if (action === "run") return { path: "/api/setup/run", body: { id: job.id } };
  if (action === "check") return { path: "/api/setup/check", body: { id: job.id } };
  if (action === "open") {
    if (!app) throw new Error("Choose an app to open.");
    return { path: "/api/setup/open", body: { id: job.id, app } };
  }
  if (action === "preview" || action === "apply") {
    if (!app) throw new Error("Choose an optional helper first.");
    // Preview is the default; apply is only ever sent after the owner confirms.
    return { path: "/api/setup/optional", body: { id: job.id, app, action, confirm: action === "apply" } };
  }
  throw new Error("Unknown setup action.");
}

/* DOM wiring */
const SETUP_JOB_LABELS = {
  not_started: "Not started", planning: "Planning", planned: "Plan ready", installing: "Installing",
  needs_approval: "Waiting for macOS approval", checking: "Checking connections", configuring: "Configuring",
  needs_preferences: "Choose music profiles", needs_sources: "Needs sources", blocked: "Blocked", interrupted: "Interrupted", failed: "Failed",
  ready: "Ready", needs_restart: "Restart needed",
};
const SETUP_APP_LABELS = {
  not_checked: "Not checked", adopt: "Existing app — kept", download: "Ready to download",
  needs_open: "Needs you to open it", needs_connection: "Needs sign-in in Settings",
  opened: "Opened — approve in macOS if asked", connected: "Connected", blocked: "Blocked",
  needs_config: "Installed — configuration not checked",
  configured: "Configured — runtime not checked",
  needs_sources: "Needs sources", needs_restart: "Restart needed",
};

const SETUP_JOB_TONES = { ready: "ok", needs_restart: "warn", blocked: "bad", interrupted: "warn", failed: "bad", not_started: "muted" };
const SETUP_APP_TONES = { ready: "ok", connected: "ok", adopt: "ok", needs_restart: "warn", blocked: "bad", failed: "bad", not_checked: "muted", needs_connection: "warn", needs_open: "warn", needs_config: "warn", configured: "warn", needs_sources: "warn" };

let setupJob = null;
let setupDefaults = null;
let setupTimer = 0;
let setupVisible = false;
let setupBusy = false;
let setupEditing = false;
let setupGeneration = 0;
let setupMusicProfileIds = { quality: null, metadata: null };

const setupForm = document.querySelector("#setup-plan-form");
const setupJobBox = document.querySelector("#setup-job");
const setupStatusLine = document.querySelector("#setup-status");
const setupClientForm = document.querySelector("#setup-client-form");

function setupApi(path, body, write) {
  if (typeof api === "function") return api(path, body, write);
  if (typeof window.api === "function") return window.api(path, body, write);
  throw new Error("The app is still loading. Try again in a moment.");
}

function setupNode(tag, text, className) {
  const node = document.createElement(tag);
  if (text != null) node.textContent = text;
  if (className) node.className = className;
  return node;
}

function setupButton(text, className, action) {
  const button = setupNode("button", text, className || "small");
  button.type = "button";
  button.addEventListener("click", action);
  return button;
}

function setupSay(text) {
  if (!setupStatusLine) return;
  setupStatusLine.hidden = !text;
  setupStatusLine.textContent = text || "";
}

function setupField(name) {
  return setupForm && setupForm.elements.namedItem(name);
}

// A typed folder is checked before a plan runs: the reason sits under that field
// and nothing plans from a folder that does not open.
const SETUP_PATH_FIELDS = ["appDir", "downloadPath", ...SETUP_ROOT_IDS.map((id) => "root." + id)];

async function setupCheckPaths() {
  const bad = {};
  if (!window.folderPicker?.checkPath) return bad;
  for (const name of SETUP_PATH_FIELDS) {
    const input = setupField(name);
    if (!input) continue;
    const reason = await window.folderPicker.checkPath(input.value);
    let line = input.nextElementSibling;
    if (!line || !line.classList?.contains("setup-path-reason")) {
      line = setupNode("p", "", "hint setup-path-reason");
      input.insertAdjacentElement("afterend", line);
    }
    line.textContent = reason;
    line.hidden = !reason;
    if (reason) bad[name] = reason;
  }
  return bad;
}

function setupSetField(name, value) {
  const field = setupField(name);
  if (!field) return;
  if (field.type === "checkbox") field.checked = !!value;
  else if (value != null) field.value = String(value);
}

function fillSetupForm(defaults, force) {
  if (!setupForm || (!force && setupForm.dataset.filled === "1")) return;
  const d = defaults || {};
  const chosen = Array.isArray(d.apps) ? new Set(d.apps) : null;
  for (const id of SETUP_APP_IDS.concat(SETUP_OPTIONAL_IDS)) setupSetField("app." + id, chosen ? chosen.has(id) : id === "qbittorrent");
  setupSetField("appDir", d.appDir);
  setupSetField("downloadPath", d.downloadPath);
  for (const id of SETUP_ROOT_IDS) {
    setupSetField("root." + id, d.roots && d.roots[id]);
    setupSetField("category." + id, d.categories && d.categories[id]);
  }
  for (const id of SETUP_NATIVE_IDS) setupSetField("port." + id, d.ports && d.ports[id]);
  setupSetField("renameTV", d.renameTV !== false);
  setupSetField("startAtLogin", d.startAtLogin === true);
  setupForm.dataset.filled = "1";
}

function setupRememberMusicProfiles(request) {
  setupMusicProfileIds = {
    quality: setupProfileId(request && request.musicQualityProfileId),
    metadata: setupProfileId(request && request.musicMetadataProfileId),
  };
}

function readSetupForm() {
  const apps = {}, roots = {}, categories = {}, ports = {};
  for (const id of SETUP_NATIVE_IDS) {
    const field = setupField("app." + id);
    apps[id] = !!(field && field.checked);
    ports[id] = setupField("port." + id) ? setupField("port." + id).value : "";
  }
  for (const id of SETUP_OPTIONAL_IDS) {
    const field = setupField("app." + id);
    apps[id] = !!(field && field.checked);
  }
  for (const id of SETUP_ROOT_IDS) {
    roots[id] = setupField("root." + id) ? setupField("root." + id).value : "";
    categories[id] = setupField("category." + id) ? setupField("category." + id).value : "";
  }
  return {
    apps,
    appDir: setupField("appDir") ? setupField("appDir").value : "",
    downloadPath: setupField("downloadPath") ? setupField("downloadPath").value : "",
    roots, ports, categories,
    renameTV: !!(setupField("renameTV") && setupField("renameTV").checked),
    startAtLogin: !!(setupField("startAtLogin") && setupField("startAtLogin").checked),
    musicQualityProfileId: setupMusicProfileIds.quality,
    musicMetadataProfileId: setupMusicProfileIds.metadata,
  };
}

function setupReview(request) {
  const box = setupNode("div", null, "setup-review");
  box.append(setupNode("h3", "Plan review"));
  const facts = setupNode("dl", null, "help-facts");
  const add = (term, value) => {
    if (!value) return;
    facts.append(setupNode("dt", term), setupNode("dd", value));
  };
  add("Apps", (request.apps || []).map((id) => SETUP_APP_NAMES[id] || id).join(", "));
  add("App folder", request.appDir);
  add("Download folder", request.downloadPath);
  add("Root folders", SETUP_ROOT_IDS.filter((id) => id !== "whisparr" || request.apps.includes(id)).map((id) => SETUP_APP_NAMES[id] + ": " + ((request.roots && request.roots[id]) || "not set")).join(" · "));
  add("Ports", SETUP_NATIVE_IDS.filter((id) => id !== "whisparr" || request.apps.includes(id)).map((id) => SETUP_APP_NAMES[id] + ": " + ((request.ports && request.ports[id]) || "not reported")).join(" · "));
  add("Categories", SETUP_ROOT_IDS.filter((id) => id !== "whisparr" || request.apps.includes(id)).map((id) => SETUP_APP_NAMES[id] + ": " + ((request.categories && request.categories[id]) || "not set")).join(" · "));
  add("Rename TV", request.renameTV ? "On" : "Off");
  add("Start at login", request.startAtLogin ? "On" : "Off");
  box.append(facts);
  return box;
}

function setupAppActions(app, view) {
  const actions = setupNode("div", null, "actions setup-app-actions");
  const name = app.name || SETUP_APP_NAMES[app.id] || app.id;
  if (view.open.includes(app.id)) {
    const open = setupButton(app.state === "opened" ? "Open again" : "Open", "small", () => setupRun("open", app.id));
    open.setAttribute("aria-label", "Open " + name);
    actions.append(open);
  }
  if (SETUP_NATIVE_IDS.includes(app.id) && (setupExistingApp(app) || app.state === "needs_connection")) {
    const settings = setupButton("Open Settings", "small", () => {});
    settings.dataset.view = "settings";
    settings.setAttribute("aria-label", "Open Settings to connect " + name);
    actions.append(settings);
  }
  if (!view.active && app.id === "recyclarr" && app.state === "configured") {
    const preview = setupButton("Preview sync", "small", () => setupRun("preview", app.id));
    preview.setAttribute("aria-label", "Preview a Recyclarr sync for " + name);
    const apply = setupButton("Apply sync", "small", () => setupRun("apply", app.id));
    apply.setAttribute("aria-label", "Apply a Recyclarr sync for " + name);
    actions.append(preview, apply);
  }
  if (app.state === "needs_sources") {
    const sources = setupButton("Open Sources", "small", () => {});
    sources.dataset.view = "sources";
    sources.setAttribute("aria-label", "Open Sources for " + name);
    actions.append(sources);
  }
  return actions.childNodes.length ? actions : null;
}

function setupPreferenceOptions(select, list, label, saved) {
  const placeholder = setupNode("option", "Choose a " + label);
  placeholder.value = "";
  select.append(placeholder);
  if (Array.isArray(list)) {
    for (const profile of list) {
      const id = Number(profile && profile.id);
      if (!Number.isInteger(id) || id <= 0) continue;
      const option = setupNode("option", profile.name || String(id));
      option.value = String(id);
      select.append(option);
    }
  }
  const wanted = setupProfileId(saved);
  if (wanted && Array.from(select.options).some((option) => option.value === String(wanted))) select.value = String(wanted);
  select.required = true;
  select.style.width = "100%";
}

function setupPreferencesForm(job, view) {
  if (!view.preferences) return null;
  const music = job.musicProfiles || {};
  const qualityProfiles = Array.isArray(music.qualityProfiles) ? music.qualityProfiles : [];
  const metadataProfiles = Array.isArray(music.metadataProfiles) ? music.metadataProfiles : [];
  const form = setupNode("form", null, "setup-preferences");
  form.append(setupNode("h3", "Music profiles"));
  form.append(setupNode("p", "Lidarr needs a live quality profile and metadata profile. Choose both to continue.", "muted"));

  const qualityLabel = setupNode("label", "Music quality profile");
  const qualitySelect = setupNode("select");
  qualitySelect.name = "qualityProfileId";
  setupPreferenceOptions(qualitySelect, qualityProfiles, "quality profile", job.request && job.request.musicQualityProfileId);
  qualityLabel.append(qualitySelect);
  const metadataLabel = setupNode("label", "Music metadata profile");
  const metadataSelect = setupNode("select");
  metadataSelect.name = "metadataProfileId";
  setupPreferenceOptions(metadataSelect, metadataProfiles, "metadata profile", job.request && job.request.musicMetadataProfileId);
  metadataLabel.append(metadataSelect);
  form.append(qualityLabel, metadataLabel);

  const message = setupNode("p", null, "msg");
  message.hidden = true;
  message.setAttribute("role", "alert");
  form.append(message);
  const actions = setupNode("div", null, "actions");
  const submit = setupButton("Continue setup", "primary", () => {});
  submit.type = "submit";
  actions.append(submit);
  form.append(actions);
  const missing = [];
  if (!qualityProfiles.length) missing.push("quality profiles");
  if (!metadataProfiles.length) missing.push("metadata profiles");
  if (missing.length) {
    message.hidden = false;
    message.className = "notice";
    message.textContent = "The server did not report any music " + missing.join(" or ") + ". Refresh status and try again.";
    submit.disabled = true;
  }

  form.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    if (setupBusy || missing.length) return;
    message.hidden = true;
    message.className = "msg";
    let request;
    try { request = setupPreferencesRequest(job, qualitySelect.value, metadataSelect.value); }
    catch (e) { message.hidden = false; message.textContent = e.message; return; }
    const generation = setupGeneration;
    setupBusy = true;
    submit.disabled = true;
    submit.textContent = "Saving…";
    let failure = "";
    try {
      const next = await setupApi("/api/setup/preferences", request, true);
      if (generation !== setupGeneration) return;
      if (next && next.id) setupJob = next;
    } catch (e) {
      if (generation !== setupGeneration) return;
      failure = e.message;
    }
    setupBusy = false;
    if (failure) {
      message.hidden = false;
      message.textContent = failure;
      submit.disabled = false;
      submit.textContent = "Continue setup";
      return;
    }
    renderSetupJob();
    scheduleSetupPoll();
  });
  return form;
}

function renderSetupJob() {
  if (setupJob && setupJob.request) setupRememberMusicProfiles(setupJob.request);
  if (!setupJobBox) return;
  setupJobBox.replaceChildren();
  const view = setupJobView(setupJob);
  if (setupForm) setupForm.hidden = !((!view.hasJob || setupEditing) && setupForm.dataset.unsupported !== "1");
  const cancelEdit = document.querySelector("#setup-cancel-edit");
  if (cancelEdit) cancelEdit.hidden = !setupEditing;
  if (!view.hasJob) return;

  const job = setupJob;
  const state = String(job.state || "not_started");
  const box = setupNode("div", null, "setup-job");
  const head = setupNode("div", null, "setup-job-head");
  head.append(setupNode("h3", "Setup plan"));
  head.append(setupNode("span", SETUP_JOB_LABELS[state] || state, "badge" + (SETUP_JOB_TONES[state] ? " dlp-tone-" + SETUP_JOB_TONES[state] : "")));
  box.append(head);
  if (setupEditing) box.append(setupNode("p", "Editing choices. The saved plan is kept until you review a new plan.", "notice"));
  if (job.detail) box.append(setupNode("p", job.detail, "muted"));
  if (job.request) box.append(setupReview(job.request));
  box.append(setupNode("p", "Nothing is downloaded until you choose Install. Setup does not start a media download.", "muted"));

  if (Array.isArray(job.apps) && job.apps.length) {
    const list = setupNode("ol", null, "setup-apps");
    list.setAttribute("aria-label", "Apps in this setup, in order");
    for (const app of job.apps) {
      const item = setupNode("li", null, "setup-app");
      const appHead = setupNode("div", null, "setup-app-head");
      const name = app.name || SETUP_APP_NAMES[app.id] || app.id;
      appHead.append(setupNode("strong", name));
      appHead.append(setupNode("span", SETUP_APP_LABELS[app.state] || app.state || "Not checked", "badge" + (SETUP_APP_TONES[app.state] ? " dlp-tone-" + SETUP_APP_TONES[app.state] : "")));
      item.append(appHead);
      if (app.detail) item.append(setupNode("p", app.detail, "muted setup-detail"));
      const facts = [];
      if (app.version) facts.push("Version " + app.version);
      if (app.expectedBytes != null && (app.expectedBytes > 0 || app.state === "download")) facts.push("Download size: " + setupSize(app.expectedBytes));
      if (app.expectedBytes != null && (app.expectedBytes > 0 || app.state === "download")) facts.push("Download limit: " + (app.sizeCap > 0 ? setupSize(app.sizeCap) : "not reported"));
      if (app.source) facts.push("Source: " + app.source);
      if (app.path) facts.push("Installed at " + app.path);
      if (setupExistingApp(app)) facts.push("Existing app — kept as it is");
      if (facts.length) item.append(setupNode("p", facts.join(" · "), "setup-app-facts"));
      const actions = setupAppActions(app, view);
      if (actions) item.append(actions);
      list.append(item);
    }
    box.append(list);
  }

  if (Array.isArray(job.checks) && job.checks.length) {
    const checks = setupNode("div", null, "setup-checks-list");
    checks.append(setupNode("h3", "Connection checks"));
    const list = setupNode("ul", null, "setup-check-list");
    for (const check of job.checks) {
      const item = setupNode("li", null, setupCheckState(check.state));
      item.textContent = (check.name || check.app || "Check") + ": " + setupCheckLabel(check.state);
      if (check.detail) item.textContent += " — " + check.detail;
      list.append(item);
    }
    checks.append(list);
    box.append(checks);
  }

  if (!setupEditing) {
    const preferences = setupPreferencesForm(job, view);
    if (preferences) box.append(preferences);
  }

  if (view.open.length) box.append(setupNode("p", "Choose Open, then complete any macOS approval prompts. Check connections afterward.", "muted"));
  if (view.restart) box.append(setupNode("p", "Quit Library Studio KH completely, then reopen it. Start at login takes effect after that. Setup is not ready until the app has reopened.", "notice"));
  if (view.ready) box.append(setupNode("p", (setupJob.apps || []).some((app) => app.state === "configured")
    ? "Required app checks passed. Optional helper configurations are saved; their runtime has not been checked."
    : "Library's own connection checks passed. This setup is ready.", "muted"));

  if (!setupEditing) {
    const actions = setupNode("div", null, "actions setup-actions");
    if (view.edit && setupForm && setupForm.dataset.unsupported !== "1") actions.append(setupButton("Edit choices", "small", startSetupEdit));
    if (view.run) actions.append(setupButton("Install selected apps", "primary", () => setupRun("run")));
    if (view.retry) actions.append(setupButton("Retry this plan", "primary", () => setupRun("run")));
    if (view.check) actions.append(setupButton("Check connections", "small", () => setupRun("check")));
    actions.append(setupButton("Refresh", "small", () => loadSetupStatus()));
    box.append(actions);
  }
  setupJobBox.append(box);
}

// Packaged webviews silently ignore the browser confirm API, so confirmations are ours.
function setupConfirm(title, detail) {
  return new Promise((resolve) => {
    const dialog = document.createElement("dialog");
    const head = setupNode("div", null, "d-head");
    const headClose = setupButton("×", "close", () => dialog.close());
    headClose.setAttribute("aria-label", "Close");
    head.append(setupNode("h2", title), headClose);
    const body = setupNode("div", null, "prose");
    body.append(setupNode("p", detail));
    const actions = setupNode("div", null, "actions");
    let answer = false;
    const keep = setupButton("Keep settings", "small", () => { answer = false; dialog.close(); });
    const go = setupButton("Apply sync", "primary", () => { answer = true; dialog.close(); });
    actions.append(keep, go);
    body.append(actions);
    dialog.append(head, body);
    dialog.addEventListener("close", () => { dialog.remove(); resolve(answer); }, { once: true });
    document.body.append(dialog);
    dialog.showModal();
    go.focus();
  });
}

async function setupRun(action, app) {
  if (setupBusy || !setupJob) return;
  // Apply changes the native app through Recyclarr, so it always asks first.
  if (action === "apply" && !(await setupConfirm("Apply this Recyclarr sync now?", "It updates profiles in the connected Sonarr or Radarr app. Saving nothing else changes."))) return;
  let request;
  try { request = setupJobRequest(setupJob, action, app); }
  catch (e) { setupSay(e.message); return; }
  const generation = setupGeneration;
  setupBusy = true;
  setupSay(action === "open" ? "Opening…" : action === "check" ? "Checking connections…" : action === "preview" ? "Running a preview…" : action === "apply" ? "Applying the sync…" : "Starting…");
  let failure = "";
  try {
    const job = await setupApi(request.path, request.body, true);
    if (generation !== setupGeneration) return;
    if (job && job.id) setupJob = job;
    setupRememberMusicProfiles(setupJob && setupJob.request);
  } catch (e) {
    if (generation !== setupGeneration) return;
    failure = e.message;
  }
  setupBusy = false;
  renderSetupJob();
  setupSay(failure);
  if (!failure) scheduleSetupPoll();
}

function startSetupEdit() {
  if (!setupJob || setupJobView(setupJob).active) return;
  setupEditing = true;
  const msg = document.querySelector("#setup-form-msg");
  if (msg) { msg.hidden = true; msg.textContent = ""; }
  fillSetupForm(setupJob.request || setupDefaults || {}, true);
  renderSetupJob();
  const first = setupForm && setupForm.querySelector("input, button");
  if (first) first.focus();
}

function cancelSetupEdit() {
  setupEditing = false;
  const msg = document.querySelector("#setup-form-msg");
  if (msg) { msg.hidden = true; msg.textContent = ""; }
  renderSetupJob();
}

function resetPlanButton() {
  const button = setupForm && setupForm.querySelector("button.primary");
  if (button) { button.disabled = false; button.textContent = "Review setup plan"; }
}

async function loadSetupStatus() {
  if (!setupForm) return;
  if (setupBusy) return;
  const generation = setupGeneration;
  try {
    const status = await setupApi("/api/setup/status");
    if (generation !== setupGeneration) return;
    setupDefaults = status && status.defaults ? status.defaults : null;
    const supported = !!(status && status.supported === true);
    setupForm.dataset.unsupported = supported ? "0" : "1";
    const note = document.querySelector("#setup-unsupported");
    if (note) note.hidden = supported;
    fillSetupForm(setupDefaults || {});
    const nextJob = status && status.job && status.job.id ? status.job : null;
    if (!(setupEditing && !nextJob)) setupJob = nextJob;
    setupRememberMusicProfiles(setupJob && setupJob.request);
    renderSetupJob();
    if (setupJob) setupSay("");
    else setupSay(supported ? "Choose what to set up, then review the plan." : "");
    scheduleSetupPoll();
  } catch (e) {
    if (generation !== setupGeneration) return;
    stopSetupPoll();
    setupSay("Could not read setup status: " + e.message);
  }
}

function setupActiveVisible() {
  return setupVisible && !document.hidden && !document.querySelector("#view-help").hidden;
}

function stopSetupPoll() {
  if (setupTimer) clearTimeout(setupTimer);
  setupTimer = 0;
}

function scheduleSetupPoll() {
  stopSetupPoll();
  if (!setupJobView(setupJob).active || !setupActiveVisible()) return;
  setupTimer = setTimeout(async () => {
    setupTimer = 0;
    if (!setupActiveVisible() || setupBusy) return;
    const generation = setupGeneration;
    try {
      const status = await setupApi("/api/setup/status");
      if (generation !== setupGeneration) return;
      setupDefaults = status && status.defaults ? status.defaults : setupDefaults;
      const nextJob = status && status.job && status.job.id ? status.job : null;
      if (!(setupEditing && !nextJob)) setupJob = nextJob;
      setupRememberMusicProfiles(setupJob && setupJob.request);
      renderSetupJob();
      scheduleSetupPoll();
    } catch (e) {
      if (generation !== setupGeneration) return;
      setupSay("Could not refresh setup: " + e.message);
    }
  }, 2500);
}

function resetSetup() {
  setupGeneration++;
  stopSetupPoll();
  setupJob = null;
  setupDefaults = null;
  setupVisible = false;
  setupBusy = false;
  setupEditing = false;
  setupMusicProfileIds = { quality: null, metadata: null };
  resetPlanButton();
  const cancelEdit = document.querySelector("#setup-cancel-edit");
  if (cancelEdit) cancelEdit.hidden = true;
  if (setupForm) {
    setupForm.reset();
    delete setupForm.dataset.filled;
    delete setupForm.dataset.unsupported;
    setupForm.hidden = false;
  }
  if (setupJobBox) setupJobBox.replaceChildren();
  const formMsg = document.querySelector("#setup-form-msg");
  if (formMsg) { formMsg.hidden = true; formMsg.textContent = ""; }
  setupSay("");
  const unsupported = document.querySelector("#setup-unsupported");
  if (unsupported) unsupported.hidden = true;
  if (setupClientForm) {
    setupClientForm.reset();
    const clientMsg = document.querySelector("#setup-client-msg");
    if (clientMsg) { clientMsg.hidden = true; clientMsg.textContent = ""; }
  }
}

function setupClearLocalToken() {
  if (typeof window.libraryClearToken === "function") window.libraryClearToken();
  else sessionStorage.removeItem("token");
}

function showSetupHost() {
  const host = document.querySelector("#setup-host-block");
  const client = document.querySelector("#setup-client-block");
  if (host) host.hidden = false;
  if (client) client.hidden = true;
  const username = document.querySelector("#setup-form input[name=username]");
  if (username) username.focus();
}

function showSetupClient() {
  const host = document.querySelector("#setup-host-block");
  const client = document.querySelector("#setup-client-block");
  if (host) host.hidden = true;
  if (client) client.hidden = false;
  const url = setupClientForm && setupClientForm.elements.namedItem("url");
  if (url) url.focus();
}

if (setupForm) {
  setupForm.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const msg = document.querySelector("#setup-form-msg");
    const button = setupForm.querySelector("button.primary");
    if (msg) msg.hidden = true;
    // A folder that does not open stops the plan and names itself, rather than
    // planning work against a path that cannot be written.
    const badPaths = await setupCheckPaths();
    if (Object.keys(badPaths).length) {
      if (msg) { msg.hidden = false; msg.textContent = "Check the folder marked below, then review the plan again."; }
      return;
    }
    let request;
    try { request = setupRequest(readSetupForm(), setupDefaults || {}); }
    catch (e) { if (msg) { msg.hidden = false; msg.textContent = e.message; } return; }
    setupBusy = true;
    const generation = setupGeneration;
    if (button) { button.disabled = true; button.textContent = "Planning…"; }
    let failure = "";
    try {
      const job = await setupApi("/api/setup/plan", request, true);
      if (generation !== setupGeneration) return;
      if (!job || !job.id) throw new Error("The host did not return a setup plan.");
      setupJob = job;
      setupRememberMusicProfiles(setupJob.request);
      setupEditing = false;
    } catch (e) {
      if (generation !== setupGeneration) return;
      failure = e.message;
    }
    setupBusy = false;
    if (button) { button.disabled = false; button.textContent = "Review setup plan"; }
    if (failure) { if (msg) { msg.hidden = false; msg.textContent = failure; } return; }
    renderSetupJob();
    setupSay("");
    scheduleSetupPoll();
  });
  const refresh = document.querySelector("#setup-refresh");
  if (refresh) refresh.addEventListener("click", () => loadSetupStatus());
  const cancelEdit = document.querySelector("#setup-cancel-edit");
  if (cancelEdit) cancelEdit.addEventListener("click", cancelSetupEdit);
}

if (setupClientForm) {
  setupClientForm.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const msg = document.querySelector("#setup-client-msg");
    const button = setupClientForm.querySelector("button.primary");
    if (msg) msg.hidden = true;
    if (button) button.disabled = true;
    try {
      await setupClientGo(setupClientForm.elements.namedItem("url").value, {
        api: (path, body) => setupApi(path, body, true),
        clearToken: setupClearLocalToken,
        navigate: (url) => window.location.assign(url),
      });
    } catch (e) {
      if (msg) { msg.hidden = false; msg.textContent = e.message; }
      if (button) button.disabled = false;
    }
  });
}

const showClientButton = document.querySelector("#show-client");
const showOwnerButton = document.querySelector("#show-owner");
if (showClientButton) showClientButton.addEventListener("click", showSetupClient);
if (showOwnerButton) showOwnerButton.addEventListener("click", showSetupHost);

async function loadGateSupport() {
  const note = document.querySelector("#setup-support-note");
  if (!note) return;
  const generation = setupGeneration;
  showSetupHost();
  if (!document.querySelector("#gate-setup").hidden) {
    note.hidden = false;
    note.textContent = "Checking whether guided app setup is available on this Mac…";
  }
  try {
    const status = await setupApi("/api/setup/status");
    if (generation !== setupGeneration || document.querySelector("#gate-setup").hidden) return;
    const supported = !!(status && status.supported === true);
    note.hidden = false;
    note.textContent = supported
      ? "Guided app setup is available on this Mac. Creating the account here does not install anything yet."
      : "Guided app setup runs only on Apple Silicon Macs. You can still create the owner account and connect apps in Settings, or use an existing library on another computer.";
  } catch (e) {
    if (generation !== setupGeneration || document.querySelector("#gate-setup").hidden) return;
    note.hidden = false;
    note.textContent = "Guided setup status could not be read. You can still create the owner account, or use an existing library on another computer.";
  }
}

document.addEventListener("library:gate", (ev) => {
  setupGeneration++;
  setupVisible = false;
  setupBusy = false;
  resetPlanButton();
  stopSetupPoll();
  if (ev.detail && ev.detail.mode === "setup") loadGateSupport();
});
document.addEventListener("library:view", (ev) => {
  setupVisible = !!(ev.detail && ev.detail.view === "help");
  if (setupVisible) loadSetupStatus();
  else stopSetupPoll();
});
document.addEventListener("library:signedout", resetSetup);
document.addEventListener("visibilitychange", () => {
  if (document.hidden) stopSetupPoll();
  else if (setupVisible) loadSetupStatus();
});
window.addEventListener("pagehide", stopSetupPoll);
