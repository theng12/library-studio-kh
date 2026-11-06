"use strict";

/*
 * Film artwork and collection extras for the Radarr detail page.
 *
 * Entry point: loadMovieExtras(detail, container)
 *   detail    = { app: "Radarr", id, title } from the existing detail view
 *   container = an element the host creates inside the film detail
 *
 * The module only writes inside `container`; the overview and facts that the
 * existing detail view renders stay untouched when this request fails.
 *
 * Find & add hook (optional, never called automatically):
 *   globalThis.movieExtrasFindAndAdd = (member, collection) => { ... }
 * A member with tmdbId, title, year and the collection name/overview is passed
 * in. The hook must open the existing Find & add confirmation for that TMDb
 * title; it must not add, monitor or search anything until the owner confirms.
 */

let movieExtrasRequest = 0;
let movieExtrasObjectURL = "";

function movieExtrasNode(tag, text, className) {
  const n = document.createElement(tag);
  if (text != null) n.textContent = text;
  if (className) n.className = className;
  return n;
}

// The desktop bootstrap/session token is a header, never a query value. Use the
// host's live token binding when present and fall back to sessionStorage.
function movieExtrasToken() {
  try {
    return typeof token === "string" ? token : sessionStorage.getItem("token") || "";
  } catch {
    return "";
  }
}

function movieExtrasArtworkRequest(extras) {
  const url = "/api/media/artwork?" + new URLSearchParams({ app: "Radarr", id: String(extras.id), name: "poster.jpg" });
  const headers = { "X-Library-Request": "1" };
  const token = movieExtrasToken();
  if (token) headers["X-Library-Token"] = token;
  return { url, init: { headers, credentials: "same-origin" } };
}

function movieExtrasClearPoster() {
  if (!movieExtrasObjectURL) return;
  URL.revokeObjectURL(movieExtrasObjectURL);
  movieExtrasObjectURL = "";
}

function movieExtrasDate(value) {
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

function movieExtrasRelease(member) {
  const status = String(member.status || "").toLowerCase();
  const date = member.releaseDate ? movieExtrasDate(member.releaseDate) : "", year = member.year ? String(member.year) : "";
  if (status === "released") return date ? "Released " + date : year ? "Released " + year : "Released";
  if (status === "incinemas") return date ? "In cinemas " + date : year ? "In cinemas " + year : "In cinemas";
  if (status === "announced" || status === "tba") return year ? "Announced " + year : "Announced";
  if (status === "deleted") return "Removed from Radarr";
  return date || year || "Not reported";
}

function movieExtrasLibraryState(member) {
  if (member.haveFile === true) return "Owned";
  if (member.haveFile === false) return member.inLibrary ? "In library · no file" : "Missing";
  return "Status unknown";
}

function movieExtrasSummary(collection, catalogKnown) {
  if (!catalogKnown) return "Owned, in-library and missing states are unknown because Radarr's movie list could not be read.";
  return collection.ownedCount + " owned · " + collection.noFileCount + " in library without a file · " + collection.missingCount + " missing";
}

function movieExtrasPanel() {
  const section = movieExtrasNode("section", null, "panel movie-extras");
  const head = movieExtrasNode("div", null, "panel-head");
  head.append(
    movieExtrasNode("h2", "Artwork and collection"),
    movieExtrasNode("p", "Reported by Radarr. The film details above stay available if any of this cannot load.", "muted"),
  );
  section.append(head);
  return section;
}

async function movieExtrasPoster(extras, body, request) {
  if (!extras.poster) {
    body.classList.add("no-poster");
    return;
  }
  const figure = movieExtrasNode("figure", null, "movie-poster");
  const img = document.createElement("img");
  img.alt = "Poster for " + (extras.title || "this film");
  img.decoding = "async";
  figure.append(img, movieExtrasNode("figcaption", "Poster from Radarr"));
  body.append(figure);

  const artwork = movieExtrasArtworkRequest(extras);
  try {
    const response = await fetch(artwork.url, artwork.init);
    if (request !== movieExtrasRequest || body.isConnected === false) return;
    if (!response.ok) throw new Error("Radarr artwork is unavailable (" + response.status + ").");
    const blob = await response.blob();
    if (request !== movieExtrasRequest || body.isConnected === false) return;
    if (!blob.type.startsWith("image/")) throw new Error("Radarr sent something that isn't an image.");
    movieExtrasClearPoster();
    const objectURL = URL.createObjectURL(blob);
    movieExtrasObjectURL = objectURL;
    img.addEventListener("load", () => {
      if (movieExtrasObjectURL === objectURL) {
        URL.revokeObjectURL(objectURL);
        movieExtrasObjectURL = "";
      }
    }, { once: true });
    img.addEventListener("error", () => {
      movieExtrasClearPoster();
      figure.remove();
      body.classList.add("no-poster");
    }, { once: true });
    img.src = objectURL;
  } catch {
    figure.remove();
    body.classList.add("no-poster");
  }
}

function movieExtrasFacts(extras) {
  const wrap = movieExtrasNode("div", null, "movie-extras-facts");
  const dl = document.createElement("dl"); dl.className = "facts";
  const pairs = [
    ["Studio", extras.studio || ""],
    ["Certification", extras.certification || ""],
    ["Runtime", extras.runtimeMinutes > 0 ? extras.runtimeMinutes + " min" : ""],
    ["Ratings", (extras.ratings || []).map(r => r.source + " " + Number(r.value).toFixed(1)).join(" · ")],
    ["TMDb", extras.tmdbId ? String(extras.tmdbId) : ""],
    ["IMDb", extras.imdbId || ""],
  ];
  facts(dl, pairs); // existing app helper; skips empty values
  wrap.append(dl);
  if (!dl.childNodes.length) wrap.append(movieExtrasNode("p", "Radarr didn't report extra facts for this film.", "empty"));
  return wrap;
}

function movieExtrasCollection(extras) {
  const c = extras.collection;
  if (!c) return movieExtrasNode("p", "Radarr doesn't report a collection for this film.", "movie-collection-empty muted");

  const hook = typeof globalThis.movieExtrasFindAndAdd === "function" ? globalThis.movieExtrasFindAndAdd : null;
  const showAction = !!hook && extras.catalogKnown;
  const box = movieExtrasNode("div", null, "movie-collection");
  box.append(
    movieExtrasNode("h3", c.title || "Collection"),
    movieExtrasNode("p", movieExtrasSummary(c, extras.catalogKnown), "movie-collection-summary"),
  );

  const headers = ["Film", "Year", "Release", "Library"];
  if (showAction) headers.push("Action");
  const table = movieExtrasNode("table", null, "list"), head = document.createElement("thead"), headRow = document.createElement("tr");
  for (const name of headers) { const th = movieExtrasNode("th", name); th.scope = "col"; headRow.append(th); }
  head.append(headRow);
  const tbody = document.createElement("tbody");
  const status = movieExtrasNode("p", null, "movie-collection-msg"); status.setAttribute("role", "status");

  for (const member of [...(c.members || [])].sort((a, b) => (a.year || 9999) - (b.year || 9999))) {
    const tr = document.createElement("tr");
    const cells = [
      (member.title || "Untitled") + (member.isCurrent ? " · this film" : ""),
      member.year || "Not reported",
      movieExtrasRelease(member),
      movieExtrasLibraryState(member) + (member.isExcluded ? " · excluded" : ""),
    ];
    if (showAction) {
      let action = "";
      if (!member.inLibrary && member.tmdbId) {
        const button = movieExtrasNode("button", "Find & add", "small");
        button.type = "button";
        button.setAttribute("aria-label", "Find and add " + (member.title || "this film") + " in Radarr");
        button.addEventListener("click", async () => {
          button.disabled = true;
          status.textContent = "";
          try {
            await hook(member, { tmdbId: c.tmdbId, title: c.title, overview: c.overview || "" });
            status.textContent = "Continue in the Find & add confirmation. Nothing is added until you confirm there.";
          } catch (e) {
            status.textContent = e.message || "Find & add could not be opened.";
          } finally {
            // A cancelled confirmation must leave the button usable.
            button.disabled = false;
          }
        });
        action = button;
      }
      cells.push(action);
    }
    cells.forEach((cell, i) => {
      const td = document.createElement("td");
      td.dataset.label = headers[i]; // the shared mobile table styles read this
      if (cell instanceof Node) td.append(cell); else td.textContent = cell;
      tr.append(td);
    });
    tbody.append(tr);
  }
  table.append(head, tbody);
  const scroll = movieExtrasNode("div", null, "table-scroll"); scroll.append(table);
  box.append(scroll, status);
  return box;
}

async function loadMovieExtras(detail, container) {
  if (!container || !detail || detail.app !== "Radarr") return;
  const request = ++movieExtrasRequest;
  movieExtrasClearPoster();
  container.replaceChildren(movieExtrasNode("p", "Reading artwork and collection…", "empty"));
  try {
    const extras = await api("/api/media/movie-extras?" + new URLSearchParams({ app: "Radarr", id: String(detail.id) }));
    if (request !== movieExtrasRequest || container.isConnected === false) return;
    const panel = movieExtrasPanel();
    const body = movieExtrasNode("div", null, "movie-extras-body");
    movieExtrasPoster(extras, body, request);
    body.append(movieExtrasFacts(extras));
    panel.append(body, movieExtrasCollection(extras));
    if ((extras.problems || []).length) panel.append(movieExtrasNode("p", extras.problems.join(" "), "movie-extras-problems muted"));
    container.replaceChildren(panel);
  } catch (e) {
    if (request !== movieExtrasRequest || container.isConnected === false) return;
    container.replaceChildren(movieExtrasNode("p", "Artwork and collection unavailable. " + e.message + " The film details above are still shown.", "notice"));
  }
}

globalThis.loadMovieExtras = loadMovieExtras;
