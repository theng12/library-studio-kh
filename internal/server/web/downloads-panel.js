"use strict";

// Downloads panel: the reconciliation table and detail pane for the Downloads page.
// Loaded as a plain script (no imports); every public name starts with "downloads".
//
// Contract: this panel shows only work that is NOT finished. The caller feeds it
// rows from GET /api/downloads after `downloadsPanelItems`, which drops anything
// already imported AND verified (status copied/moved with verified hash|size).
// Those rows belong in My Library with their proof. Activity owns the full
// timeline, so this panel never repeats it: history appears only in the detail
// pane, for the one row that was opened. A row whose source is gone shows
// "Files missing — no source" and a Clear action instead of a permanent 0%.
//
// CSS hooks for the lead: .downloads-panel, .downloads-table, .dlp-tools,
// .dlp-bulk, .dlp-poster, .dlp-progress, .dlp-missing, .dlp-empty, .dlp-detail;
// chips are the existing .badge plus .dlp-tone-{ok,info,muted,warn,bad}.

const DOWNLOADS_MISSING_LABEL = "Files missing — no source";

const DOWNLOADS_COLUMNS = [
  { key: "poster", label: "" },
  { key: "title", label: "Title" },
  { key: "status", label: "Status" },
  { key: "size", label: "Size" },
  { key: "progress", label: "Progress" },
  { key: "source", label: "Source" },
  { key: "target", label: "Target" },
];

const DOWNLOADS_FILTERS = [
  { key: "all", label: "All" },
  { key: "downloading", label: "Downloading" },
  { key: "queued", label: "Queued" },
  { key: "paused", label: "Paused" },
  { key: "seeding", label: "Seeding" },
  { key: "importing", label: "Importing" },
  { key: "completed", label: "Completed" },
  { key: "problems", label: "Problems" },
];

const DOWNLOADS_BULK_ACTIONS = [
  { key: "pause", label: "Pause" },
  { key: "resume", label: "Resume" },
  { key: "remove", label: "Remove" },
];

const DOWNLOADS_STATE_LABELS = {
  downloading: "Downloading", forcedDL: "Downloading", stalledDL: "Stalled",
  metaDL: "Fetching metadata", forcedMetaDL: "Fetching metadata",
  checkingDL: "Checking", allocating: "Allocating",
  queuedDL: "Queued", queuedUP: "Queued to seed",
  pausedDL: "Paused", stoppedDL: "Paused", pausedUP: "Paused, seeding", stoppedUP: "Paused, seeding",
  uploading: "Seeding", forcedUP: "Seeding", stalledUP: "Seeding, stalled",
  checkingUP: "Checking", moving: "Moving", error: "Error",
  missingFiles: DOWNLOADS_MISSING_LABEL,
};

const DOWNLOADS_VERIFICATION_LABELS = { hash: "SHA-256 hash", size: "Size and readback" };

// An item is only "done" when the import proof says so. Verified absence is not
// success: it stays in the panel flagged, never silently disappears.
function downloadsImportedVerified(item) {
  return /^(copied|moved)$/.test(String(item.status || "")) &&
    /^(hash|size)$/.test(String(item.verified || ""));
}

function downloadsPanelItems(items) {
  return (Array.isArray(items) ? items : []).filter((item) => !downloadsImportedVerified(item));
}

function downloadsStatus(item) {
  const state = String(item.state || "");
  const status = String(item.status || "");
  const make = (key, label, tone, extra) =>
    Object.assign({ key: key, label: label, tone: tone, missing: false, canPause: false, canResume: false }, extra);
  if (item.sourceGone === true || state === "missingFiles" || status === "missing") {
    return make("problems", DOWNLOADS_MISSING_LABEL, "bad", { missing: true });
  }
  if (status === "importing" || item.kind === "waiting") return make("importing", "Importing", "info");
  if (/^(copied|moved)$/.test(status)) return make("problems", "Imported — proof missing", "warn");
  if (state === "error") return make("problems", "Error", "bad", { canResume: true });
  if (/^(paused|stopped)/.test(state)) return make("paused", DOWNLOADS_STATE_LABELS[state] || "Paused", "muted", { canResume: true });
  if (state === "queuedDL" || state === "queuedUP") return make("queued", DOWNLOADS_STATE_LABELS[state], "muted", { canPause: true });
  if (/^(uploading|forcedUP|stalledUP)$/.test(state)) return make("seeding", DOWNLOADS_STATE_LABELS[state], "ok", { canPause: true });
  if (/^(downloading|forcedDL|stalledDL|metaDL|forcedMetaDL|checkingDL|allocating|moving)$/.test(state)) {
    return make("downloading", DOWNLOADS_STATE_LABELS[state], "info", { canPause: true });
  }
  if (status === "queued") return make("queued", "Queued", "muted", { canPause: true });
  if (status === "downloading" || status === "stalled") {
    return make("downloading", status === "stalled" ? "Stalled" : "Downloading", "info", { canPause: true });
  }
  if (item.progress === 1) return make("completed", "Completed", "ok");
  return make("other", state || status || "Status not reported", "muted");
}

function downloadsRowActions(status) {
  if (status.missing) return [{ key: "clear", label: "Clear" }];
  if (status.key === "importing") return [{ key: "reveal", label: "Reveal" }];
  if (status.key === "completed") return [{ key: "remove", label: "Remove" }, { key: "reveal", label: "Reveal" }];
  return [
    status.canPause ? { key: "pause", label: "Pause" } : status.canResume ? { key: "resume", label: "Resume" } : null,
    { key: "remove", label: "Remove" },
    { key: "reveal", label: "Reveal" },
  ].filter(Boolean);
}

function downloadsSourceText(item) {
  if (item.indexer) return String(item.indexer);
  const app = String(item.app || "");
  if (app === "Unmatched") return "qBittorrent · unmatched";
  if (!app || app === "Unknown") return "App not verified";
  return app;
}

function dlpBytes(n) {
  for (const [u, v] of [["TB", 1e12], ["GB", 1e9], ["MB", 1e6], ["KB", 1e3]]) {
    if (n >= v) return (n / v).toFixed(n >= 10 * v ? 0 : 1) + " " + u;
  }
  return n + " B";
}

function dlpRate(n) { return dlpBytes(n) + "/s"; }

function dlpDuration(s) {
  s = Math.round(s);
  if (s < 60) return s + " s";
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
  return d ? d + " d " + h + " h" : h ? h + " h " + m + " min" : m + " min";
}

function downloadsWhen(value) {
  if (!value) return "Not reported";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Not reported"
    : date.toLocaleString(undefined, { day: "numeric", month: "short", year: "numeric", hour: "numeric", minute: "2-digit" });
}

function downloadsSizeText(item) {
  const done = Number.isFinite(item.downloaded) && item.downloaded >= 0 ? dlpBytes(item.downloaded) : null;
  const total = Number.isFinite(item.size) && item.size > 0 ? dlpBytes(item.size) : null;
  if (done && total) return done + " of " + total;
  if (done) return done + " — total size not reported";
  if (total) return total + " total — downloaded not reported";
  return "Size not reported";
}

// A missing source is shown as missing, never as a 0% that looks like progress.
function downloadsProgress(item, status) {
  status = status || downloadsStatus(item);
  if (status.missing) return { known: false, value: null, label: "—", speedText: "—", etaText: "—" };
  const known = Number.isFinite(item.progress) && item.progress >= 0 && item.progress <= 1;
  const speed = item.speed != null ? item.speed : item.dlspeed;
  return {
    known: known,
    value: known ? item.progress : null,
    label: known ? Math.floor(item.progress * 100) + "%" : "Progress not reported",
    speedText: Number.isFinite(speed) && speed >= 0 ? dlpRate(speed) : "Speed not reported",
    etaText: Number.isFinite(item.eta) && item.eta >= 0 && item.eta < 8640000 ? dlpDuration(item.eta) + " left" : "ETA not reported",
  };
}

function downloadsRow(item) {
  const status = downloadsStatus(item);
  return {
    key: String(item.hash || item.name || item.title || ""),
    hash: String(item.hash || ""),
    item: item,
    title: String(item.name || item.title || "Untitled download"),
    poster: String(item.posterPath || ""),
    status: status,
    missing: status.missing,
    sizeText: downloadsSizeText(item),
    progress: downloadsProgress(item, status),
    source: downloadsSourceText(item),
    target: String(item.targetPath || "Target not known yet"),
    actions: downloadsRowActions(status),
  };
}

function downloadsRows(items, query, filter) {
  const wanted = String(query || "").trim().toLocaleLowerCase();
  const active = DOWNLOADS_FILTERS.some((f) => f.key === filter) ? filter : "all";
  return (Array.isArray(items) ? items : [])
    .filter((item) => active === "all" || downloadsStatus(item).key === active)
    .filter((item) => !wanted || String(item.name || item.title || "").toLocaleLowerCase().includes(wanted))
    .map(downloadsRow);
}

function downloadsFilters(items) {
  const list = Array.isArray(items) ? items : [];
  const counts = { all: list.length };
  for (const item of list) {
    const key = downloadsStatus(item).key;
    counts[key] = (counts[key] || 0) + 1;
  }
  return DOWNLOADS_FILTERS.map((filter) => ({ key: filter.key, label: filter.label, count: counts[filter.key] || 0 }));
}

function downloadsEmpty(all, rows, filter, query) {
  if (!all.length) return { message: "No downloads right now. New downloads appear here while they run.", action: null };
  if (!rows.length) {
    return {
      message: String(query || "").trim() ? "No downloads match this search." : "No downloads match this filter.",
      action: { key: "show-all", label: "Show all" },
    };
  }
  return null;
}

// Bulk actions act on the selection the caller holds; keys that vanished from the
// current rows do not count and never enable a button.
function downloadsBulk(rows, selected) {
  const keys = new Set((rows || []).map((row) => row.key));
  const count = [...(selected || [])].filter((key) => keys.has(key)).length;
  return {
    count: count,
    statusText: count === 0 ? "No downloads selected" : count === 1 ? "1 download selected" : count + " downloads selected",
    actions: DOWNLOADS_BULK_ACTIONS.map((action) => ({ key: action.key, label: action.label, disabled: count === 0 })),
  };
}

function downloadsModel(items, state) {
  state = state || {};
  const list = downloadsPanelItems(items);
  const filter = DOWNLOADS_FILTERS.some((f) => f.key === state.filter) ? state.filter : "all";
  const query = String(state.query || "");
  const rows = downloadsRows(list, query, filter);
  const visible = new Set(rows.map((row) => row.key));
  const selected = new Set([...(state.selected || [])].filter((key) => visible.has(key)));
  return {
    columns: DOWNLOADS_COLUMNS,
    filters: downloadsFilters(list),
    filter: filter,
    query: query,
    rows: rows,
    selected: selected,
    bulk: downloadsBulk(rows, selected),
    empty: downloadsEmpty(list, rows, filter, query),
    countText: list.length === 0 ? "No downloads" : rows.length + " of " + list.length + " downloads",
  };
}

function downloadsDetail(item, history) {
  const status = downloadsStatus(item);
  const method = DOWNLOADS_VERIFICATION_LABELS[item.verified] || null;
  const events = (Array.isArray(history) ? history : []).map((event) => ({
    when: downloadsWhen(event.at || event.when),
    text: String(event.text || event.message || "Recorded"),
  }));
  return {
    key: String(item.hash || item.name || item.title || ""),
    title: String(item.name || item.title || "Untitled download"),
    poster: String(item.posterPath || ""),
    status: status,
    facts: [
      ["Size", downloadsSizeText(item)],
      ["Source", downloadsSourceText(item)],
      ["Save folder", item.savePath || "Not reported"],
      ["Category", item.category || "None"],
    ],
    sections: [
      { key: "proof", title: "Proof", facts: [
        ["Status", status.label],
        ["Status recorded", downloadsWhen(item.statusAt)],
        ["Source file", item.sourcePath || "Not reported"],
      ] },
      { key: "target", title: "Target", facts: [
        ["Volume", item.targetVolume || "Not reported"],
        ["Path", item.targetPath || "Not reported"],
      ] },
      { key: "verification", title: "Verification", facts: [
        ["Method", method || "Not verified"],
        ["Verified", method ? downloadsWhen(item.verifiedAt) : "Not yet"],
      ] },
      { key: "history", title: "History", events: events, empty: "No history reported yet." },
    ],
  };
}

function dlpEl(tag, text, className) {
  const node = document.createElement(tag);
  if (text != null) node.textContent = text;
  if (className) node.className = className;
  return node;
}

function dlpButton(label, className) {
  const button = dlpEl("button", label, className || "small");
  button.type = "button";
  return button;
}

function downloadsPanelRender(root, model, handlers) {
  handlers = handlers || {};
  const on = (name, a, b) => { if (typeof handlers[name] === "function") handlers[name](a, b); };
  root.replaceChildren();
  root.classList.add("downloads-panel");

  const bar = dlpEl("div", null, "tools dlp-tools");

  const filterLabel = dlpEl("label", null, "dlp-filter");
  filterLabel.append(dlpEl("span", "Filter"));
  const select = document.createElement("select");
  select.setAttribute("aria-label", "Filter downloads by state");
  for (const filter of model.filters) {
    const option = document.createElement("option");
    option.value = filter.key;
    option.textContent = filter.label + " (" + filter.count + ")";
    select.append(option);
  }
  select.value = model.filter;
  select.addEventListener("change", () => on("filter", select.value));
  filterLabel.append(select);

  const search = document.createElement("input");
  search.type = "search";
  search.id = "dl-q";
  search.className = "search";
  search.placeholder = "Filter downloads…";
  search.setAttribute("aria-label", "Filter downloads by title");
  search.setAttribute("aria-keyshortcuts", "Meta+F Control+F");
  search.value = model.query;
  search.addEventListener("input", () => on("query", search.value));

  const total = dlpEl("span", model.countText, "dlp-total");

  const bulk = dlpEl("div", null, "dlp-bulk");
  bulk.setAttribute("role", "group");
  bulk.setAttribute("aria-label", "Selected downloads");
  const bulkStatus = dlpEl("span", model.bulk.statusText, "dlp-count");
  bulkStatus.setAttribute("aria-live", "polite");
  bulk.append(bulkStatus);
  for (const action of model.bulk.actions) {
    const button = dlpButton(action.label);
    button.dataset.action = action.key;
    button.disabled = action.disabled;
    button.addEventListener("click", () => on("bulk", action.key));
    bulk.append(button);
  }

  bar.append(filterLabel, search, total, bulk);

  const wrap = dlpEl("div", null, "table-scroll");
  const table = dlpEl("table", null, "list downloads-table");
  const head = document.createElement("thead");
  const headRow = document.createElement("tr");

  const selectHead = document.createElement("th");
  selectHead.scope = "col";
  const selectAll = document.createElement("input");
  selectAll.type = "checkbox";
  selectAll.setAttribute("data-select-all", "");
  selectAll.setAttribute("aria-label", "Select all downloads");
  selectAll.checked = model.rows.length > 0 && model.selected.size === model.rows.length;
  selectAll.disabled = model.rows.length === 0;
  selectAll.addEventListener("change", () => on("selectAll", selectAll.checked));
  selectHead.append(selectAll);
  headRow.append(selectHead);

  for (const column of model.columns) {
    const th = dlpEl("th", column.label);
    th.scope = "col";
    if (column.key) th.dataset.column = column.key;
    headRow.append(th);
  }
  const actionsHead = dlpEl("th", "Actions");
  actionsHead.scope = "col";
  headRow.append(actionsHead);
  head.append(headRow);

  const body = document.createElement("tbody");
  for (const row of model.rows) body.append(downloadsRowNode(row, model, on));
  table.append(head, body);
  wrap.append(table);

  const empty = dlpEl("div", model.empty ? model.empty.message : "", "dlp-empty");
  empty.setAttribute("role", "status");
  empty.hidden = !model.empty;
  if (model.empty && model.empty.action) {
    const showAll = dlpButton(model.empty.action.label);
    showAll.addEventListener("click", () => on("emptyAction", model.empty.action.key));
    empty.append(showAll);
  }

  root.append(bar, wrap, empty);
  return root;
}

function downloadsRowNode(row, model, on) {
  const tr = document.createElement("tr");
  tr.className = row.missing ? "dlp-row dlp-missing" : "dlp-row";
  tr.dataset.key = row.key;

  const selectCell = document.createElement("td");
  const box = document.createElement("input");
  box.type = "checkbox";
  box.checked = model.selected.has(row.key);
  box.setAttribute("aria-label", "Select " + row.title);
  box.addEventListener("change", () => on("select", row.key, box.checked));
  selectCell.append(box);
  tr.append(selectCell);

  const posterCell = dlpEl("td", null, "dlp-poster");
  if (row.poster) {
    const image = document.createElement("img");
    image.src = row.poster;
    image.alt = "";
    posterCell.append(image);
  } else {
    const placeholder = dlpEl("span", null, "dlp-poster-empty");
    placeholder.setAttribute("aria-hidden", "true");
    posterCell.append(placeholder);
  }
  tr.append(posterCell);

  const titleCell = dlpEl("td", null, "dlp-title");
  titleCell.dataset.label = "Title";
  const open = dlpButton(row.title, "dlp-open");
  open.addEventListener("click", () => on("open", row));
  titleCell.append(open);
  tr.append(titleCell);

  const statusCell = dlpEl("td", null, "dlp-status");
  statusCell.dataset.label = "Status";
  statusCell.append(dlpEl("span", row.status.label, "badge dlp-tone-" + row.status.tone));
  tr.append(statusCell);

  const sizeCell = dlpEl("td", row.sizeText, "dlp-size");
  sizeCell.dataset.label = "Size";
  tr.append(sizeCell);

  const progressCell = dlpEl("td", null, "dlp-progress");
  progressCell.dataset.label = "Progress";
  const bar = document.createElement("progress");
  bar.max = 1;
  if (row.progress.known) bar.value = row.progress.value;
  bar.setAttribute("aria-label", "Download progress for " + row.title);
  progressCell.append(
    bar,
    dlpEl("span", row.progress.label, "dlp-pct"),
    dlpEl("span", row.progress.speedText, "dlp-speed"),
    dlpEl("span", row.progress.etaText, "dlp-eta"));
  tr.append(progressCell);

  const sourceCell = dlpEl("td", row.source, "dlp-source");
  sourceCell.dataset.label = "Source";
  tr.append(sourceCell);

  const targetCell = dlpEl("td", row.target, "dlp-target");
  targetCell.dataset.label = "Target";
  targetCell.title = row.target;
  tr.append(targetCell);

  const actionsCell = dlpEl("td", null, "dlp-actions");
  actionsCell.dataset.label = "Actions";
  for (const action of row.actions) {
    const button = dlpButton(action.label);
    button.dataset.action = action.key;
    if (action.key === "remove" || action.key === "clear") button.title = "Removes it from qBittorrent. Its files are kept.";
    if (action.key === "reveal") button.title = "Shows its folder in Finder.";
    button.setAttribute("aria-label", action.label + " " + row.title);
    button.addEventListener("click", () => on("action", row, action.key));
    actionsCell.append(button);
  }
  tr.append(actionsCell);

  return tr;
}

function downloadsDetailRender(container, detail) {
  container.replaceChildren();
  container.classList.add("dlp-detail");

  const head = dlpEl("div", null, "dlp-detail-head");
  if (detail.poster) {
    const image = document.createElement("img");
    image.src = detail.poster;
    image.alt = "";
    head.append(image);
  }
  const heading = dlpEl("h2", detail.title);
  head.append(heading, dlpEl("span", detail.status.label, "badge dlp-tone-" + detail.status.tone));
  container.append(head);

  const facts = document.createElement("dl");
  facts.className = "facts";
  for (const [term, value] of detail.facts) facts.append(dlpEl("dt", term), dlpEl("dd", value));
  container.append(facts);

  for (const section of detail.sections) {
    const block = dlpEl("section", null, "dlp-detail-section");
    block.append(dlpEl("h3", section.title));
    if (section.facts) {
      const list = document.createElement("dl");
      list.className = "facts";
      for (const [term, value] of section.facts) list.append(dlpEl("dt", term), dlpEl("dd", value));
      block.append(list);
    }
    if (section.events) {
      if (!section.events.length) {
        block.append(dlpEl("p", section.empty, "muted"));
      } else {
        const history = document.createElement("ol");
        history.className = "dlp-history";
        for (const event of section.events) history.append(dlpEl("li", event.when + " — " + event.text));
        block.append(history);
      }
    }
    container.append(block);
  }
  return container;
}
