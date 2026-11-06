"use strict";

let subtitleContext = "";
let subtitleRequest = 0;
let subtitleAudioFilter = "";
let subtitleLanguageFilter = "";

function subtitleAudioLanguages(item) {
  const reported = item.audioLanguages?.length ? item.audioLanguages : [item.audioLanguage];
  return [...new Set(reported.filter(value => typeof value === "string").map(value => value.trim()).filter(value => value && value.toLowerCase() !== "unknown"))];
}

function subtitleAudioMatches(item, filter) {
  const spoken = subtitleAudioLanguages(item).map(value => value.toLowerCase());
  const english = value => ["english", "en", "eng"].includes(value);
  return !filter || (filter === "english" ? spoken.some(english) : filter === "other" ? spoken.some(value => !english(value)) : spoken.length === 0);
}

async function loadMediaSubtitles(detail, serial = mediaSerial) {
  const request = ++subtitleRequest, isCurrent = () => serial === mediaSerial && request === subtitleRequest;
  const panel = $("#media-subtitles"), content = $("#subtitles-content"), message = $("#subtitles-message"), refresh = $("#subtitles-refresh");
  panel.hidden = detail.app !== "Sonarr" && detail.app !== "Radarr";
  if (panel.hidden) return;
  const context = { app: detail.app, id: detail.id }, key = context.app + ":" + context.id;
  if (key !== subtitleContext) { subtitleAudioFilter = ""; subtitleLanguageFilter = ""; subtitleContext = key; }
  refresh.disabled = true; $("#subtitle-overview").textContent = "Loading…";
  message.textContent = "Reading subtitle status from Bazarr…"; content.replaceChildren();
  try {
    const data = await api("/api/media/subtitles?" + new URLSearchParams(context));
    if (!isCurrent()) return;
    message.textContent = (data.problems || []).join(" ");
    $("#subtitle-overview").textContent = !data.enabled ? "Off" : !data.configured ? "Setup needed" : !data.version ? "Unavailable" : data.problems?.length ? "Check setup" : !data.items?.length ? "Waiting for files" : data.items.filter(item => item.existing.some(s => s.code === "en" && !s.forced && !s.hi)).length + " of " + data.items.length + " English available";
    if (!data.enabled || !data.configured) {
      const button = mediaNode("button", "Open subtitle settings", "small"); button.type = "button"; button.dataset.view = "settings";
      const actions = mediaNode("div", null, "media-actions"); actions.append(button); content.append(actions); return;
    }
    if (!data.version) return;
    const items = data.items || [], english = (data.profiles || []).filter(p => p.languages?.length && p.languages.every(v => v.toLowerCase() === "en"));
    const providerReady = (data.providers || []).length > 0;
    if (!items.length) {
      content.append(mediaNode("p", "Bazarr has not reported imported media files for this title yet. Subtitles become available after files are imported and synced.", "empty")); return;
    }
    const actions = mediaNode("div", null, "media-actions");
    if (english.length) {
      const profile = document.createElement("select"); profile.setAttribute("aria-label", "English subtitle profile");
      for (const p of english) profile.append(mediaOption(String(p.id), p.name));
      const assign = mediaNode("button", "Use English profile for this " + (detail.app === "Sonarr" ? "series" : "film"), "small"); assign.type = "button";
      assign.addEventListener("click", async () => {
        assign.disabled = true;
        try { await api("/api/media/subtitles/profile", { ...context, profileId: Number(profile.value) }); if (isCurrent()) await loadMediaSubtitles(detail, serial); }
        catch (e) { if (isCurrent()) message.textContent = e.message; }
        finally { assign.disabled = false; }
      });
      actions.append(profile, assign); content.append(actions);
    } else content.append(mediaNode("p", "Create an English-only language profile in Bazarr before assigning it to this title.", "empty"));
    const filters = mediaNode("div", null, "tools"), audioLabel = mediaNode("label", "Reported audio"), audio = document.createElement("select"), languageLabel = mediaNode("label", "English subtitles"), language = document.createElement("select");
    for (const [v, text] of [["", "All"], ["english", "English"], ["other", "Other reported languages"], ["unknown", "Not reported"]]) audio.append(mediaOption(v, text));
    for (const [v, text] of [["", "All"], ["available", "Available"], ["missing", "Reported missing"]]) language.append(mediaOption(v, text));
    audio.value = subtitleAudioFilter; language.value = subtitleLanguageFilter;
    audioLabel.append(audio); languageLabel.append(language); filters.append(audioLabel, languageLabel); content.append(filters);
    const list = mediaNode("div"); content.append(list);
    const isEnglish = value => value?.code?.toLowerCase() === "en" && !value.forced && !value.hi;
    function render() {
      subtitleAudioFilter = audio.value; subtitleLanguageFilter = language.value;
      const shown = items.filter(item => {
        return subtitleAudioMatches(item, audio.value) && (!language.value || (language.value === "available" ? item.existing.some(isEnglish) : item.missing.some(isEnglish)));
      });
      list.replaceChildren();
      if (!shown.length) { list.append(mediaNode("p", "No reported files match these language filters.", "empty")); return; }
      for (const item of shown) {
        const row = mediaNode("details", null, "subtitle-row"), summary = document.createElement("summary");
        const available = item.existing.some(isEnglish), missing = item.missing.some(isEnglish);
        const name = detail.app === "Sonarr" ? "S" + String(item.season).padStart(2, "0") + "E" + String(item.episode).padStart(2, "0") + " · " + item.title : item.title;
        summary.append(mediaNode("span", name, "download-title"), mediaNode("span", "Audio: " + (subtitleAudioLanguages(item).join(", ") || "Not reported") + " · " + (available ? "English available" : missing ? "English missing" : "English status not reported"), "download-status"));
        row.append(summary);
        row.append(mediaNode("p", "Available subtitles: " + (item.existing.map(s => s.name + (s.forced ? " (forced)" : "") + (s.hi ? " (hearing impaired)" : "")).join(", ") || "None reported")));
        if (!item.fileReported) row.append(mediaNode("p", "Waiting for a media file reported by Bazarr."));
        if (!item.profileId) row.append(mediaNode("p", "Assign an English profile before searching."));
        const search = mediaNode("button", available ? "English available" : "Find English subtitles", "small"); search.type = "button";
        search.disabled = available || !item.fileReported || !item.profileId || !providerReady;
        search.addEventListener("click", async () => {
          search.disabled = true;
          try {
            await api("/api/media/subtitles/english", { ...context, itemId: item.id });
            if (isCurrent()) { search.textContent = "Search requested"; message.textContent = "Bazarr accepted the English search request. Refresh status to check for a result; acceptance does not confirm a download."; }
          } catch (e) { if (isCurrent()) message.textContent = e.message; search.disabled = false; }
        });
        row.append(search); list.append(row);
      }
    }
    audio.addEventListener("input", render); language.addEventListener("input", render); render();
  } catch (e) { if (isCurrent()) { message.textContent = e.message; $("#subtitle-overview").textContent = "Unavailable"; } }
  finally { if (isCurrent()) refresh.disabled = false; }
}
$("#subtitles-refresh").addEventListener("click", () => { if (mediaData) loadMediaSubtitles(mediaData); });
