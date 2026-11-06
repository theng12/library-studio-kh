"use strict";

/* Native companion settings. Mounted by the lead: mountCompanionSettings(container).
   Uses only the shared panel, form and badge classes; no shared UI edits. */

async function companionAPI(path, body) {
  const token = sessionStorage.getItem("token") || "";
  const headers = token ? { "X-Library-Token": token } : {};
  if (body) { headers["Content-Type"] = "application/json"; headers["X-Library-Request"] = "1"; }
  const r = await fetch(path, {
    method: body ? "POST" : "GET",
    headers,
    credentials: "same-origin",
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || "Something went wrong (" + r.status + ").");
  return data;
}

// Only changed fields are sent, so applying one setting never rewrites another.
function companionPatchFor(fields, baseline) {
  const patch = {};
  for (const f of fields) {
    if (!f.valid) throw new Error(f.error);
    if (f.value !== baseline[f.key]) patch[f.key] = f.value;
  }
  return patch;
}

function companionNode(tag, text, className) {
  const el = document.createElement(tag);
  if (text != null) el.textContent = text;
  if (className) el.className = className;
  return el;
}

function companionBadge(app, setting) {
  const badge = companionNode("span", setting.managedBy === "library" ? "Library policy" : "Saved in " + app.app, "badge");
  if (setting.managedBy === "library") badge.classList.add("planned");
  return badge;
}

function companionField(app, setting, baseline) {
  const wrap = companionNode("div");
  const values = [];
  if (setting.control === "toggle") {
    const label = companionNode("label", null, "check");
    const box = document.createElement("input");
    box.type = "checkbox";
    box.checked = setting.value === true;
    box.disabled = !setting.editable;
    label.append(box, document.createTextNode(" " + setting.label), document.createTextNode(" "), companionBadge(app, setting));
    wrap.append(label);
    baseline[setting.key] = setting.value === true;
    if (setting.editable) values.push({ key: setting.key, get value() { return box.checked; }, get valid() { return true; } });
  } else if (setting.control === "number") {
    const label = companionNode("label", setting.label + (setting.unit ? " " : ""));
    if (setting.unit) label.append(companionNode("span", "· " + setting.unit, "hint"));
    label.append(document.createTextNode(" "), companionBadge(app, setting));
    const input = document.createElement("input");
    input.type = "number";
    input.inputMode = "decimal";
    if (setting.range) {
      input.min = setting.range.min;
      input.max = setting.range.max;
      input.step = setting.range.step;
    }
    input.value = setting.value == null ? "" : String(setting.value);
    input.disabled = !setting.editable;
    label.append(input);
    wrap.append(label);
    baseline[setting.key] = setting.value == null ? null : Number(setting.value);
    if (setting.editable && setting.value != null) values.push({
      key: setting.key,
      get value() { return Number(input.value); },
      get valid() {
        const n = Number(input.value);
        if (input.value.trim() === "" || !Number.isFinite(n)) return false;
        if (setting.range && (n < setting.range.min || n > setting.range.max)) return false;
        return true;
      },
      get error() { return setting.label + " must be a number from " + setting.range.min + " to " + setting.range.max + "."; },
    });
  } else if (setting.control === "stop") {
    const label = companionNode("label", setting.label + " ");
    label.append(companionBadge(app, setting));
    const select = document.createElement("select");
    if (setting.value != null && setting.value !== 0) {
      const current = companionNode("option", "Removal action (" + setting.value + ") · current");
      current.value = String(setting.value);
      current.disabled = true;
      select.append(current);
    }
    const stop = companionNode("option", "Stop torrent");
    stop.value = "0";
    select.append(stop);
    select.value = String(setting.value == null ? 0 : setting.value);
    select.disabled = !setting.editable;
    label.append(select);
    wrap.append(label);
    baseline[setting.key] = setting.value == null ? null : Number(setting.value);
    if (setting.editable && setting.value != null) values.push({ key: setting.key, get value() { return Number(select.value); }, get valid() { return true; } });
  }
  if (setting.note) wrap.append(companionNode("p", setting.note, "muted"));
  return { wrap, values };
}

function companionClientRow(app, client) {
  const wrap = companionNode("div");
  const head = companionNode("p", null, "muted");
  head.style.margin = "0";
  head.append(companionNode("strong", client.name || "Download client"));
  head.append(document.createTextNode(" · " + (client.implementation || "unknown client") + " · "));
  if (client.removeCompletedDownloads) {
    head.append(companionNode("span", "Remove after import is ON", "off"));
  } else {
    head.append(companionNode("span", "Remove after import is off", "muted"));
  }
  wrap.append(head);
  if (client.removeCompletedDownloads && client.reason) {
    wrap.append(companionNode("p", client.reason, "muted"));
    const button = companionNode("button", "Turn off in " + app.app, "small");
    button.type = "button";
    button.addEventListener("click", async () => {
      button.disabled = true;
      button.textContent = "Turning off…";
      const root = button.closest("[data-companion-root]");
      try {
        const r = await companionAPI("/api/companion/settings", { app: app.app, removeCompletedDownloads: false });
        companionReplaceApp(root, r.app);
        companionStatus(root, "Saved. Remove after import is off in " + app.app + ".");
      } catch (e) {
        button.disabled = false;
        button.textContent = "Try again";
        wrap.append(companionNode("p", e.message, "msg"));
      }
    });
    wrap.append(button);
  }
  return wrap;
}

function companionAppPanel(app) {
  const panel = companionNode("section", null, "panel");
  const head = companionNode("div", null, "panel-head");
  head.append(companionNode("h2", app.app));
  head.append(companionNode("p", app.connected
    ? "These are " + app.app + "'s own settings. Apply writes only what you change, then reads it back."
    : app.configured ? "Could not read " + app.app + " just now." : "Not connected yet.", "muted"));
  panel.append(head);
  if (app.error) {
    const prose = companionNode("div", null, "prose");
    prose.append(companionNode("p", app.error, "msg"));
    panel.append(prose);
  }
  if (!app.connected) return panel;

  const form = companionNode("form", null, "prose");
  form.style.maxWidth = "none";
  form.style.display = "grid";
  form.style.gap = "0.35rem";
  const baseline = {}, fields = [];
  for (const setting of app.settings || []) {
    const f = companionField(app, setting, baseline);
    form.append(f.wrap);
    fields.push(...f.values);
  }
  for (const client of app.clients || []) form.append(companionClientRow(app, client));

  const actions = companionNode("div", null, "actions");
  const apply = companionNode("button", "Apply changes", "primary");
  apply.type = "submit";
  const msg = companionNode("p", "", "msg");
  msg.hidden = true;
  msg.setAttribute("role", "status");
  actions.append(apply, msg);
  form.append(actions);

  form.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    let patch;
    try {
      patch = companionPatchFor(fields, baseline);
    } catch (e) {
      msg.hidden = false; msg.textContent = e.message; return;
    }
    if (!Object.keys(patch).length) {
      msg.hidden = false; msg.textContent = "Nothing changed."; return;
    }
    patch.app = app.app;
    apply.disabled = true; apply.textContent = "Applying…"; msg.hidden = false; msg.textContent = "Writing to " + app.app + "…";
    try {
      const r = await companionAPI("/api/companion/settings", patch);
      const next = r.app;
      if (next && next.connected) {
        const root = form.closest("[data-companion-root]");
        companionReplaceApp(root, next);
        companionStatus(root, "Saved. Read back from " + app.app + ".");
      } else {
        msg.textContent = next && next.error ? next.error : "Saved, but the new value couldn't be read back.";
        apply.disabled = false; apply.textContent = "Apply changes";
      }
    } catch (e) {
      msg.textContent = e.message;
      apply.disabled = false; apply.textContent = "Apply changes";
    }
  });
  panel.append(form);
  return panel;
}

function companionReplaceApp(root, next) {
  if (!root || !next) return;
  const old = root.querySelector('[data-companion-app="' + next.app + '"]');
  if (!old) return;
  const panel = companionAppPanel(next);
  panel.dataset.companionApp = next.app;
  old.replaceWith(panel);
}

function companionStatus(root, text) {
  const status = root && root.querySelector("[data-companion-status]");
  if (!status) return;
  status.hidden = !text;
  status.textContent = text || "";
}

function companionRender(root, state) {
  if (!root || !state) return;
  const wrap = companionNode("div");
  wrap.style.display = "grid";
  wrap.style.gap = "var(--gap)";
  const intro = companionNode("div", null, "panel prose");
  const title = companionNode("h2", "Settings inside your apps");
  const refresh = companionNode("button", "Refresh", "small");
  refresh.type = "button";
  refresh.style.marginLeft = "0.75rem";
  title.append(refresh);
  intro.append(title);
  intro.append(companionNode("p", state.policy || "Library manages local copies. Remove-after-import stays off in each library app.", "muted"));
  const status = companionNode("p", "", "msg");
  status.hidden = true;
  status.setAttribute("role", "status");
  status.dataset.companionStatus = "1";
  intro.append(status);
  wrap.append(intro);
  for (const app of state.apps || []) {
    const panel = companionAppPanel(app);
    panel.dataset.companionApp = app.app;
    wrap.append(panel);
  }
  if (state.qbittorrent) {
    const panel = companionAppPanel(state.qbittorrent);
    panel.dataset.companionApp = state.qbittorrent.app;
    wrap.append(panel);
  }
  refresh.addEventListener("click", () => mountCompanionSettings(root));
  root.replaceChildren(wrap);
}

async function mountCompanionSettings(container) {
  if (!container) return;
  container.dataset.companionRoot = "1";
  container.replaceChildren(companionNode("p", "Reading the companions' settings…", "muted"));
  try {
    const state = await companionAPI("/api/companion/settings");
    companionRender(container, state);
  } catch (e) {
    const box = companionNode("div", null, "panel prose");
    box.append(companionNode("h2", "Settings inside your apps"));
    box.append(companionNode("p", "Couldn't read the companion settings: " + e.message, "msg"));
    const retry = companionNode("button", "Try again", "small");
    retry.type = "button";
    retry.addEventListener("click", () => mountCompanionSettings(container));
    box.append(retry);
    container.replaceChildren(box);
  }
}
