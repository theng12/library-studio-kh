"use strict";
// Sources management: add a source from Prowlarr's live catalog, then edit, test
// and save configured sources. Everything goes through the host API, which keeps
// auth, CSRF and secrets; this file never receives a saved secret value.

let sourceRoot = null;
let sourceRefresh = null;
let sourceCatalog = null;
let sourceEditing = null;
let sourceBusy = false;
let sourceUID = 0;

function smNode(tag, text, className) {
  const n = document.createElement(tag);
  if (text != null) n.textContent = text;
  if (className) n.className = className;
  return n;
}

function smButton(text, action, className) {
  const b = smNode("button", text, className || "small");
  b.type = "button";
  b.addEventListener("click", action);
  return b;
}

function smPanel(title, description, body) {
  const section = smNode("section", null, "panel");
  const head = smNode("div", null, "panel-head");
  head.append(smNode("h2", title));
  if (description) head.append(smNode("p", description, "muted"));
  section.append(head, body);
  return section;
}

function smStatus(text) {
  if (!sourceRoot) return;
  const box = sourceRoot.querySelector("[data-source-status]");
  box.hidden = !text;
  box.textContent = text || "";
}

async function smApi(path, body) {
  if (typeof window.api !== "function") throw new Error("The app is still loading. Try again in a moment.");
  return window.api(path, body, !!body);
}

function smFormatDisabled(until) {
  if (!until) return "";
  const when = new Date(until);
  return Number.isNaN(when.getTime()) ? "" : " until " + when.toLocaleString();
}

window.mountSourceManager = function (container, refreshCallback) {
  if (!container) return;
  sourceRoot = container;
  sourceRefresh = typeof refreshCallback === "function" ? refreshCallback : null;
  sourceEditing = null;
  sourceRoot.replaceChildren();

  const actions = smNode("div", null, "media-actions");
  const add = smButton("Add source", () => openSourceForm(null));
  actions.append(add);
  const status = smNode("p", "notice", "notice");
  status.dataset.sourceStatus = "1";
  status.setAttribute("role", "status");
  status.hidden = true;
  const body = smNode("div", null, "media-padding");
  sourceRoot.append(actions, status, body);
  loadConfiguredSources();
};

async function loadConfiguredSources() {
  if (!sourceRoot || sourceBusy) return;
  sourceBusy = true;
  const body = sourceRoot.querySelector(".media-padding");
  body.replaceChildren(smNode("p", "Reading configured sources…", "muted"));
  try {
    const data = await smApi("/api/sources");
    const rows = (data.indexers || []).map((x) => {
      const status = x.enabled == null ? "Not reported" : x.enabled ? "Enabled" : "Disabled" + smFormatDisabled(x.disabledUntil);
      const actions = smNode("div", null, "media-actions");
      actions.append(
        smButton("Edit", () => openSourceForm(x.id)),
        smButton("Test", () => testConfiguredSource(x.id, x.name))
      );
      const tr = document.createElement("tr");
      for (const [i, value] of [x.name, status, x.protocol || "Not reported", x.priority == null ? "Not reported" : String(x.priority), actions].entries()) {
        const td = document.createElement("td");
        if (i === 4) td.append(value); else td.textContent = value;
        const labels = ["Source", "Status", "Protocol", "Priority", "Actions"];
        td.dataset.label = labels[i];
        tr.append(td);
      }
      return tr;
    });
    const wrap = smNode("div", null, "table-scroll");
    const table = smNode("table", null, "list");
    const thead = document.createElement("thead");
    const head = document.createElement("tr");
    for (const name of ["Source", "Status", "Protocol", "Priority", "Actions"]) {
      const th = smNode("th", name);
      th.scope = "col";
      head.append(th);
    }
    thead.append(head);
    const tbody = document.createElement("tbody");
    tbody.append(...rows);
    table.append(thead, tbody);
    wrap.append(table);
    const panel = smPanel(
      "Configured sources",
      "Add, test or edit a source. Saving changes Prowlarr only; it never adds or deletes a source by itself.",
      rows.length ? wrap : smNode("p", "No sources are configured in Prowlarr yet.", "empty")
    );
    body.replaceChildren(panel);
    if (!data.configured) smStatus((data.problems || []).join(" "));
  } catch (err) {
    body.replaceChildren(smNode("p", "Configured sources could not be read.", "empty"));
    smStatus(err.message);
  } finally {
    sourceBusy = false;
  }
}

async function testConfiguredSource(id, name) {
  if (!sourceRoot || sourceBusy) return;
  sourceBusy = true;
  smStatus("Testing " + name + "…");
  try {
    await smApi("/api/sources/test", { id });
    smStatus("Prowlarr's test passed for " + name + ". Nothing was saved.");
  } catch (err) {
    smStatus("Prowlarr's test failed for " + name + ": " + err.message);
  } finally {
    sourceBusy = false;
  }
}

async function openSourceForm(id) {
  if (!sourceRoot || sourceBusy) return;
  sourceBusy = true;
  smStatus(id ? "Reading the source settings…" : "Reading Prowlarr's source catalog…");
  try {
    if (id) {
      const data = await smApi("/api/sources/" + encodeURIComponent(id));
      sourceEditing = data.item;
      renderSourceForm(null, data.item);
    } else {
      if (!sourceCatalog) {
        const data = await smApi("/api/sources/catalog");
        sourceCatalog = data.items || [];
      }
      sourceEditing = null;
      renderSourceForm(sourceCatalog, null);
    }
    smStatus("");
  } catch (err) {
    smStatus(err.message);
  } finally {
    sourceBusy = false;
  }
}

function renderSourceForm(catalog, edit) {
  const body = sourceRoot.querySelector(".media-padding");
  const form = document.createElement("form");
  form.className = "add-form panel";
  form.noValidate = true;
  const title = edit ? "Source settings" : "Add a source";
  form.append(smNode("h2", title));
  form.append(smNode("p", edit
    ? "Saved secrets stay hidden. Leave a secret blank to keep the saved value."
    : "The list comes from Prowlarr. New sources are added disabled unless you tick Enable after adding.", "muted"));

  let selectedKey = edit ? edit.key : (catalog && catalog.length ? catalog[0].key : "");
  let selected = edit || (catalog && catalog.length ? catalog[0] : null);

  if (!edit) {
    const providerLabel = smNode("label", "Provider definition");
    const provider = document.createElement("select");
    provider.required = true;
    for (const entry of [...(catalog || [])].sort((a, b) => a.name.localeCompare(b.name))) {
      const option = document.createElement("option");
      option.value = entry.key;
      option.textContent = entry.name + (entry.protocol ? " · " + entry.protocol : "") + (entry.privacy ? " · " + entry.privacy : "");
      provider.append(option);
    }
    provider.value = selectedKey;
    provider.addEventListener("change", () => {
      selectedKey = provider.value;
      selected = (catalog || []).find((x) => x.key === selectedKey) || null;
      const box = form.querySelector("[data-source-fields]");
      box.replaceChildren(...sourceFieldRows(selected, null));
      applySourceBlocked(form, selected, null);
    });
    providerLabel.append(provider);
    form.append(providerLabel);
    if (!catalog || !catalog.length) {
      form.append(smNode("p", "Prowlarr did not publish any source definitions.", "empty"));
    }
  } else {
    form.append(smNode("p", "Editing a configured source · " + (edit.protocol || "protocol not reported"), "muted"));
  }

  const nameLabel = smNode("label", "Source name");
  const nameInput = document.createElement("input");
  nameInput.type = "text";
  nameInput.maxLength = 200;
  nameInput.required = true;
  nameInput.value = edit ? edit.name || "" : "";
  nameLabel.append(nameInput);
  form.append(nameLabel);

  const priorityLabel = smNode("label", "Priority (1-50)");
  const priorityInput = document.createElement("input");
  priorityInput.type = "number";
  priorityInput.min = "1";
  priorityInput.max = "50";
  priorityInput.step = "1";
  priorityInput.value = String(edit ? edit.priority || 25 : 25);
  priorityLabel.append(priorityInput);
  form.append(priorityLabel);

  const enableLabel = smNode("label", null, "check media-monitor");
  const enableInput = document.createElement("input");
  enableInput.type = "checkbox";
  enableInput.checked = !!(edit && edit.enabled);
  enableLabel.append(enableInput, smNode("span", edit ? "Enabled" : "Enable after adding"));
  form.append(enableLabel);

  const fields = smNode("div", null, "media-padding");
  fields.dataset.sourceFields = "1";
  fields.append(...sourceFieldRows(selected, edit));
  form.append(fields);

  const blocked = smNode("div", null, "media-padding");
  blocked.dataset.sourceBlocked = "1";
  form.append(blocked);

  const actions = smNode("div", null, "media-actions");
  const primary = smButton(edit ? "Save source" : "Add source", (ev) => {
    ev.preventDefault();
    submitSourceForm(edit, selectedKey, nameInput, priorityInput, enableInput, fields, "save");
  });
  const test = smButton("Test source", (ev) => {
    ev.preventDefault();
    submitSourceForm(edit, selectedKey, nameInput, priorityInput, enableInput, fields, "test");
  });
  const cancel = smButton("Cancel", (ev) => {
    ev.preventDefault();
    sourceEditing = null;
    loadConfiguredSources();
  });
  actions.append(primary, test, cancel);
  form.append(actions);
  form.addEventListener("submit", (ev) => {
    ev.preventDefault();
    primary.click();
  });

  body.replaceChildren(form);
  applySourceBlocked(form, selected, edit);
  if (!edit && (!catalog || !catalog.length)) {
    primary.disabled = true;
    test.disabled = true;
  }
}

function applySourceBlocked(form, selected, edit) {
  const box = form.querySelector("[data-source-blocked]");
  if (!box) return;
  const blocked = (selected && selected.blocked) || (edit && edit.blocked) || [];
  if (!blocked.length) {
    box.replaceChildren();
    box.hidden = true;
    if (!edit) {
      for (const button of form.querySelectorAll("button")) {
        if (button.textContent === "Add source" || button.textContent === "Test source") button.disabled = false;
      }
    }
    return;
  }
  box.hidden = false;
  const list = smNode("ul", null, "source-notes");
  for (const reason of blocked) list.append(smNode("li", reason));
  box.replaceChildren(smNode("p", "This source can't be added or fully edited here:", "notice"), list);
  if (!edit) {
    for (const button of form.querySelectorAll("button")) {
      if (button.textContent === "Add source" || button.textContent === "Test source") button.disabled = true;
    }
  }
}

function sourceFieldRows(selected, edit) {
  if (!selected) return [];
  const normal = [];
  const advanced = [];
  for (const field of selected.fields || []) {
    if (!field.supported || field.type === "info") {
      normal.push(sourceUnsupportedRow(field));
      continue;
    }
    const row = sourceInputRow(field, edit);
    if (field.advanced) advanced.push(row); else normal.push(row);
  }
  if (advanced.length) {
    const details = document.createElement("details");
    details.append(smNode("summary", "Advanced settings"));
    details.append(...advanced);
    normal.push(details);
  }
  return normal;
}

function sourceUnsupportedRow(field) {
  const wrap = smNode("div", null, "media-padding");
  const label = smNode("label", field.label);
  const input = document.createElement("input");
  input.type = "text";
  input.disabled = true;
  input.value = field.hasValue ? "A saved value is kept unchanged" : "";
  input.placeholder = field.hasValue ? "Saved value kept unchanged" : "No value";
  label.append(input);
  wrap.append(label);
  if (field.blocked) wrap.append(smNode("p", field.blocked, "muted"));
  return wrap;
}

function sourceInputRow(field, edit) {
  const wrap = smNode("div", null, "media-padding");
  const id = "source-field-" + (++sourceUID);
  let label, input;
  if (field.type === "bool") {
    label = smNode("label", null, "check media-monitor");
    input = document.createElement("input");
    input.type = "checkbox";
    input.id = id;
    input.checked = field.value === true;
    label.append(input, smNode("span", field.label));
  } else {
    label = smNode("label", field.label);
    input = document.createElement("input");
    input.id = id;
    if (field.type === "select") {
      input = document.createElement("select");
      input.id = id;
      const blank = document.createElement("option");
      blank.value = "";
      blank.textContent = field.required ? "Choose…" : "Use Prowlarr's default";
      input.append(blank);
      for (const option of field.options || []) {
        const o = document.createElement("option");
        o.value = String(option.value);
        o.textContent = option.label;
        input.append(o);
      }
      input.value = field.value == null ? "" : String(field.value);
    } else {
      input.type = field.type === "password" ? "password" : field.type === "numeric" ? "number" : "text";
      if (field.type === "numeric") input.step = "any";
      else input.maxLength = 8192;
      if (field.secret) {
        input.value = "";
        input.placeholder = field.hasValue ? "Saved value kept — leave blank to keep" : "Enter a value";
      } else {
        input.value = field.value == null ? "" : String(field.value);
      }
    }
    label.append(input);
  }
  input.dataset.sourceField = field.name;
  input.dataset.sourceType = field.type;
  input.dataset.sourceSecret = field.secret ? "1" : "";
  input.required = !!field.required && !(field.secret && field.hasValue);
  wrap.append(label);
  if (field.help) wrap.append(smNode("p", field.help, "muted"));
  if (field.blocked) wrap.append(smNode("p", field.blocked, "muted"));
  return wrap;
}

function collectSourceFields(container, edit) {
  const values = {};
  for (const input of container.querySelectorAll("[data-source-field]")) {
    const name = input.dataset.sourceField;
    const type = input.dataset.sourceType;
    const secret = input.dataset.sourceSecret === "1";
    let value;
    if (type === "bool") value = input.checked;
    else if (type === "numeric") {
      if (input.value.trim() === "") continue;
      value = Number(input.value);
    } else {
      value = input.value;
    }
    if (secret) {
      if (String(value).trim() === "") continue; // blank keeps the saved secret
    } else if (!edit && String(value).trim() === "") {
      continue; // a new optional setting uses Prowlarr's default
    }
    values[name] = value;
  }
  return values;
}

async function submitSourceForm(edit, key, nameInput, priorityInput, enableInput, fields, action) {
  if (!sourceRoot || sourceBusy) return;
  const name = nameInput.value.trim();
  const priority = Number(priorityInput.value);
  if (!name) { smStatus("Give the source a name."); nameInput.focus(); return; }
  if (!Number.isInteger(priority) || priority < 1 || priority > 50) {
    smStatus("Priority must be a whole number from 1 to 50.");
    priorityInput.focus();
    return;
  }
  sourceBusy = true;
  smStatus(action === "test" ? "Asking Prowlarr to test these settings…" : "Sending the source settings…");
  try {
    const payload = {
      id: edit ? edit.id : 0,
      key: edit ? edit.key || "" : key,
      name,
      enable: enableInput.checked,
      priority,
      fields: collectSourceFields(fields, !!edit),
    };
    if (action === "test") {
      await smApi("/api/sources/test", payload);
      smStatus("Prowlarr's test passed. Nothing was saved.");
    } else if (edit) {
      await smApi("/api/sources/save", payload);
      smStatus("Source saved.");
    } else {
      await smApi("/api/sources", payload);
      smStatus("Source added. Refresh the list to see Prowlarr's report.");
    }
    if (action === "save") {
      sourceEditing = null;
      sourceBusy = false;
      await loadConfiguredSources();
      if (sourceRefresh) {
        try { await sourceRefresh(); } catch (_) { /* the read-only report shows its own error */ }
      }
    }
  } catch (err) {
    smStatus(err.message);
  } finally {
    sourceBusy = false;
  }
}
