"use strict";
// Settings as four tabs of self-contained cards: General / Sessions & security /
// Remote access / Updates. Plain script with no imports; the lead mounts it inside the
// existing settings form, so input names and app.js's save path keep working.

const SETTINGS_TABS = [
  { id: "general", label: "General" },
  { id: "security", label: "Sessions & security" },
  { id: "remote", label: "Remote access" },
  { id: "updates", label: "Updates" },
];

const SETTINGS_CARDS = {
  general: [
    {
      id: "appearance",
      title: "Appearance",
      fields: [
        {
          kind: "radios",
          name: "appearance",
          ariaLabel: "Appearance",
          options: [["system", "System", true], ["light", "Light"], ["dark", "Dark"]],
          hint: "System follows this Mac.",
        },
      ],
    },
    {
      id: "folders",
      title: "Folders",
      fields: [
        { kind: "text", name: "downloadPath", label: "Download folder on this computer", placeholder: "Used to show free space" },
        { kind: "text", name: "libraryPath", label: "Library folder on the NAS", placeholder: "Used to check the NAS is reachable" },
      ],
    },
    {
      id: "connections",
      title: "Companion apps",
      hint: "qBittorrent downloads; Sonarr, Radarr and Lidarr look after the library.",
      fields: [
        { kind: "text", name: "qbittorrent.url", label: "qBittorrent address", placeholder: "http://127.0.0.1:8080", inputmode: "url" },
        { kind: "text", name: "qbittorrent.username", label: "qBittorrent username", autocomplete: "off" },
        { kind: "password", name: "qbittorrent.password", label: "qBittorrent password", secret: "qbi" + "ttorrent" },
        {
          kind: "details", summary: "Library apps · Sonarr, Radarr, Lidarr, Prowlarr, Bazarr, Whisparr",
          fields: [{ kind: "slot", id: "arr-fields" }],
        },
      ],
    },
    {
      id: "help",
      title: "Help",
      fields: [
        { kind: "note", text: "Setup steps, phone access and troubleshooting live on one page." },
        { kind: "button", label: "Open setup & help", view: "help", class: "small" },
      ],
    },
  ],
  security: [
    {
      id: "sessions",
      title: "Sessions & security",
      hint: "Every browser signed in to this library.",
      fields: [
        { kind: "slot", id: "auth-note", tag: "p", class: "muted" },
        { kind: "slot", id: "sessions-msg", tag: "p", class: "msg", role: "status", hidden: true },
        { kind: "slot", id: "sessions", tag: "ul", class: "sessions", label: "Signed-in sessions" },
        { kind: "button", id: "logout", class: "small", label: "Sign out this browser" },
      ],
    },
  ],
  remote: [
    {
      id: "remote",
      title: "Phone & Tailscale access",
      hint: "Off by default. Your phone opens this library over your private Tailscale network.",
      fields: [
        { kind: "switch", name: "tailscaleOn", label: "Allow access from my Tailscale devices" },
        { kind: "number", name: "tailscalePort", label: "Port", min: 1024, max: 65535, inputmode: "numeric", labelClass: "short" },
        { kind: "slot", id: "remote-status", tag: "p", class: "msg", role: "status" },
        {
          kind: "slot", id: "remote-link-box", tag: "label", hidden: true,
          text: "Phone link", hint: "· keep it private",
          input: { id: "remote-link", readonly: true, describedby: "remote-help" },
        },
        {
          kind: "slot", id: "remote-help", tag: "p", class: "muted",
          text: "Save to apply. Send the phone link to your own phone and open it in its browser with Tailscale connected. The address is HTTP over your Tailscale network, not HTTPS.",
        },
      ],
    },
  ],
  updates: [
    {
      id: "updates",
      title: "Updates",
      hint: "Library Studio KH checks the shared KH feed; Studio Launcher KH installs the update.",
      fields: [
        { kind: "slot", id: "updates-version", tag: "p", class: "muted", text: "This Mac's version is shown here." },
        { kind: "slot", id: "updates-state", tag: "p", class: "muted", role: "status", text: "Not checked" },
        { kind: "button", id: "updates-check", class: "small", label: "Check now" },
        { kind: "button", id: "updates-open", class: "small", label: "Open update page", hidden: true },
      ],
    },
  ],
};

function settingsFieldId(name) {
  return "settings-" + String(name).replace(/[^a-zA-Z0-9_-]+/g, "-").toLowerCase();
}

function settingsEl(doc, tag, props, ...children) {
  const node = doc.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value == null || value === false || value === "") continue;
    if (key === "class") node.className = value;
    else if (key === "text") node.textContent = value;
    else if (key === "value") node.value = value;
    else if (key === "checked") node.checked = true;
    else if (key === "hidden") node.hidden = true;
    else node.setAttribute(key, value === true ? "" : String(value));
  }
  for (const child of children.flat()) if (child) node.append(child);
  return node;
}

function settingsBuildField(doc, field, savedSlots) {
  if (field.kind === "radios") {
    const group = settingsEl(doc, "div", { class: "appearance", role: "radiogroup", "aria-label": field.ariaLabel || field.name });
    for (const [value, label, checked] of field.options) {
      group.append(settingsEl(doc, "label", { class: "check" },
        settingsEl(doc, "input", { type: "radio", id: settingsFieldId(field.name + "-" + value), name: field.name, value, checked: !!checked }),
        settingsEl(doc, "span", { text: label })));
    }
    return field.hint ? [group, settingsEl(doc, "p", { class: "muted", text: field.hint })] : [group];
  }
  if (field.kind === "switch") {
    const slot = settingsEl(doc, "span", { class: "switch-saved", role: "status", "aria-live": "polite", "data-saved-for": field.name });
    savedSlots.set(field.name, slot);
    return [settingsEl(doc, "div", { class: "switch-row" },
      settingsEl(doc, "label", { class: "switch" },
        settingsEl(doc, "input", { type: "checkbox", role: "switch", id: settingsFieldId(field.name), name: field.name, checked: !!field.checked }),
        settingsEl(doc, "span", { text: field.label, class: "switch-label" })),
      slot)];
  }
  if (field.kind === "text" || field.kind === "password" || field.kind === "number") {
    return [settingsEl(doc, "label", { class: field.labelClass },
      settingsEl(doc, "span", { text: field.label }),
      settingsEl(doc, "input", {
        id: settingsFieldId(field.name), name: field.name, type: field.kind,
        placeholder: field.placeholder, inputmode: field.inputmode,
        autocomplete: field.kind === "password" ? "new-password" : field.autocomplete,
        "data-secret": field.secret,
        min: field.min, max: field.max,
      }))];
  }
  if (field.kind === "slot") {
    const node = settingsEl(doc, field.tag || "div", {
      id: field.id, class: field.class, role: field.role, hidden: field.hidden,
      "aria-label": field.label, text: field.text,
    });
    if (field.hint) node.append(settingsEl(doc, "span", { class: "hint", text: " " + field.hint }));
    if (field.input) {
      node.append(settingsEl(doc, "input", {
        id: field.input.id, type: "text", readonly: field.input.readonly,
        "aria-describedby": field.input.describedby,
      }));
    }
    return [node];
  }
  if (field.kind === "details") {
    // Configured-once credentials sit behind one clear disclosure, so the tab
    // stays one screen while every field is still there.
    const box = settingsEl(doc, "details", { class: "settings-details", id: field.id });
    box.append(settingsEl(doc, "summary", { text: field.summary }));
    const body = settingsEl(doc, "div", { class: "settings-details-body" });
    for (const sub of field.fields) for (const node of settingsBuildField(doc, sub, savedSlots)) body.append(node);
    box.append(body);
    return [box];
  }
  if (field.kind === "button") {
    return [settingsEl(doc, "button", {
      type: "button", id: field.id, class: field.class, "data-view": field.view, hidden: field.hidden, text: field.label,
    })];
  }
  return [settingsEl(doc, "p", { class: "muted", text: field.text })];
}

function settingsBuildCard(doc, card, savedSlots) {
  const body = settingsEl(doc, "div", { class: "card-body" });
  for (const field of card.fields) for (const node of settingsBuildField(doc, field, savedSlots)) body.append(node);
  return settingsEl(doc, "section", { class: "card", id: "settings-card-" + card.id },
    settingsEl(doc, "header", { class: "card-head" },
      settingsEl(doc, "h2", { text: card.title }),
      card.hint ? settingsEl(doc, "p", { text: card.hint }) : null),
    body);
}

// Mount inside the settings form. Returns the tab API; savedFeedback writes into the
// switch's feedback slot without the caller knowing the markup.
function mountSettingsTabs(host, doc = globalThis.document) {
  const savedSlots = new Map();
  const buttons = new Map();
  const panels = new Map();
  const tablist = settingsEl(doc, "div", { class: "tabs", role: "tablist", "aria-label": "Settings sections" });
  const root = settingsEl(doc, "div", { class: "settings" }, tablist);

  for (const tab of SETTINGS_TABS) {
    const button = settingsEl(doc, "button", {
      type: "button", class: "tab", role: "tab", id: "settings-tab-" + tab.id,
      "aria-controls": "settings-panel-" + tab.id, "aria-selected": "false", tabindex: "-1", text: tab.label,
    });
    const panel = settingsEl(doc, "section", {
      class: "tabpanel", role: "tabpanel", id: "settings-panel-" + tab.id,
      "aria-labelledby": "settings-tab-" + tab.id, hidden: true,
    });
    for (const card of SETTINGS_CARDS[tab.id] || []) panel.append(settingsBuildCard(doc, card, savedSlots));
    button.addEventListener("click", () => selectTab(tab.id));
    button.addEventListener("keydown", (ev) => {
      const index = SETTINGS_TABS.findIndex((t) => t.id === tab.id);
      let next = null;
      if (ev.key === "ArrowRight") next = SETTINGS_TABS[(index + 1) % SETTINGS_TABS.length].id;
      else if (ev.key === "ArrowLeft") next = SETTINGS_TABS[(index - 1 + SETTINGS_TABS.length) % SETTINGS_TABS.length].id;
      else if (ev.key === "Home") next = SETTINGS_TABS[0].id;
      else if (ev.key === "End") next = SETTINGS_TABS[SETTINGS_TABS.length - 1].id;
      if (!next) return;
      if (ev.preventDefault) ev.preventDefault();
      selectTab(next, true);
    });
    tablist.append(button);
    root.append(panel);
    buttons.set(tab.id, button);
    panels.set(tab.id, panel);
  }

  function selectTab(id, focus) {
    for (const [tabId, button] of buttons) {
      const on = tabId === id;
      button.setAttribute("aria-selected", on ? "true" : "false");
      button.setAttribute("tabindex", on ? "0" : "-1");
      panels.get(tabId).hidden = !on;
      if (on && focus) button.focus();
    }
  }
  selectTab(SETTINGS_TABS[0].id);

  function savedFeedback(name, text, state) {
    const node = savedSlots.get(name);
    if (!node) return false;
    node.textContent = text || "";
    if (state) node.setAttribute("data-state", state);
    else node.removeAttribute("data-state");
    return true;
  }

  host.append(root);
  return { root, selectTab, savedFeedback };
}

if (typeof window !== "undefined") {
  window.librarySettingsView = { SETTINGS_TABS, SETTINGS_CARDS, mountSettingsTabs };
}
