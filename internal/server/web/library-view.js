"use strict";

// Metadata the stock app did not report. Not a genre or year value.
const LIBRARY_NOT_REPORTED = "__not_reported";

function libraryTitleOrder(a, b) {
  return (a.title || "").localeCompare(b.title || "", undefined, { sensitivity: "base", numeric: true });
}

function libraryReportedGenres(item) {
  return (item.genres || []).map((g) => String(g || "").trim()).filter(Boolean);
}

function libraryGenres(items) {
  const seen = new Map();
  for (const item of items) {
    for (const genre of libraryReportedGenres(item)) {
      const key = genre.toLocaleLowerCase();
      if (!seen.has(key)) seen.set(key, genre);
    }
  }
  return [...seen.values()].sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base", numeric: true }));
}

function libraryYears(items) {
  const years = new Set();
  for (const item of items) {
    const year = Number(item.year);
    if (Number.isFinite(year) && year > 0) years.add(year);
  }
  return [...years].sort((a, b) => b - a);
}

function libraryGenreOptions(items) {
  return [
    { value: "", label: "All genres" },
    ...libraryGenres(items).map((genre) => ({ value: genre, label: genre })),
    { value: LIBRARY_NOT_REPORTED, label: "Not reported" },
  ];
}

function libraryYearOptions(items) {
  return [
    { value: "", label: "All years" },
    ...libraryYears(items).map((year) => ({ value: String(year), label: String(year) })),
    { value: LIBRARY_NOT_REPORTED, label: "Not reported" },
  ];
}

function libraryLanguageMatches(item, language) {
  const original = (item.originalLanguage || "").trim().toLowerCase();
  const unknown = !original || original === "unknown" || original === "any";
  if (!language) return true;
  return language === "english" ? original === "english" : language === "other" ? !unknown && original !== "english" : unknown;
}

function libraryGenreMatches(item, genre) {
  if (!genre) return true;
  const genres = libraryReportedGenres(item).map((g) => g.toLocaleLowerCase());
  if (genre === LIBRARY_NOT_REPORTED) return genres.length === 0;
  return genres.includes(genre.toLocaleLowerCase());
}

function libraryYearMatches(item, year) {
  if (!year) return true;
  const value = Number(item.year);
  const known = Number.isFinite(value) && value > 0;
  if (year === LIBRARY_NOT_REPORTED) return !known;
  return known && value === Number(year);
}

// Every control narrows the same reported library list; the source array is left untouched.
function libraryFilter(items, filters = {}) {
  const query = (filters.query || "").trim().toLocaleLowerCase();
  return items.filter((item) =>
    (!filters.type || item.app === filters.type) &&
    (item.title || "").toLocaleLowerCase().includes(query) &&
    libraryLanguageMatches(item, filters.language) &&
    libraryGenreMatches(item, filters.genre) &&
    libraryYearMatches(item, filters.year));
}

// Unknown years sort last in both directions, with title ordering keeping ties deterministic.
function librarySort(items, sort) {
  const rows = items.slice();
  const addedOrder = (a, b) => {
    const x = a.added || "", y = b.added || "";
    if (!x || !y) return x === y ? libraryTitleOrder(a, b) : x ? -1 : 1;
    return y.localeCompare(x) || libraryTitleOrder(a, b);
  };
  const yearOrder = (oldest) => (a, b) => {
    const x = Number(a.year) > 0 ? Number(a.year) : null;
    const y = Number(b.year) > 0 ? Number(b.year) : null;
    if (x === null || y === null) return x === y ? libraryTitleOrder(a, b) : x === null ? 1 : -1;
    return (oldest ? x - y : y - x) || libraryTitleOrder(a, b);
  };
  rows.sort(sort === "added" ? addedOrder
    : sort === "app" ? (a, b) => (a.app || "").localeCompare(b.app || "") || libraryTitleOrder(a, b)
    : sort === "year" ? yearOrder(false)
    : sort === "oldest" ? yearOrder(true) : libraryTitleOrder);
  return rows;
}
