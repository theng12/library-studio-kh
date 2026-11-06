"use strict";

const DOWNLOAD_FILTERS = [
  ["all", "All"], ["downloading", "Downloading"], ["queued", "Queued"],
  ["paused", "Paused"], ["seeding", "Seeding"], ["completed", "Completed"], ["problems", "Problems"],
];

function downloadProgressText(progress) {
  return Number.isFinite(progress) && progress >= 0 && progress <= 1 ? Math.floor(progress * 100) + "%" : "Progress not reported";
}

function downloadMatches(item, filter) {
  const state = item.state || "";
  switch (filter) {
    case "downloading": return /^(downloading|forcedDL|stalledDL|metaDL|forcedMetaDL|checkingDL|allocating)$/.test(state);
    case "queued": return state === "queuedDL" || state === "queuedUP";
    case "paused": return /^(paused|stopped)/.test(state);
    case "seeding": return /^(uploading|forcedUP|stalledUP)$/.test(state);
    case "completed": return Number.isFinite(item.progress) && item.progress >= 1;
    case "problems": return state === "error" || state === "missingFiles";
    default: return true;
  }
}

function downloadRows(items, query, filter, sort, app = "") {
  const rows = items.filter(t => downloadMatches(t, filter) && (!app || (t.app || "Unknown") === app) && (t.name || "").toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const name = (a, b) => (a.name || "").localeCompare(b.name || "", undefined, { sensitivity: "base", numeric: true });
  const value = t => {
    const field = { queue: "priority", progress: "progress", added: "added_on", size: "size", speed: "dlspeed" }[sort];
    const n = t[field];
    return Number.isFinite(n) && (sort === "progress" || sort === "speed" ? n >= 0 : n > 0) ? n : null;
  };
  return rows.sort((a, b) => {
    if (sort === "name") return name(a, b);
    if (sort === "app") return (a.app || "Unknown").localeCompare(b.app || "Unknown") || name(a, b);
    const x = value(a), y = value(b);
    if (x === null || y === null) return x === y ? name(a, b) : x === null ? 1 : -1;
    return (sort === "queue" ? x - y : y - x) || name(a, b);
  });
}
