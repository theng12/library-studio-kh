---
name: Library Studio KH
description: A quiet control room over a personal media library.
colors:
  accent: "#3550c8"
  accent-soft: "#e3e8fb"
  accent-ink: "#ffffff"
  ground: "#f3f4f7"
  sidebar: "#e9ebf1"
  panel: "#ffffff"
  ink: "#1a1d26"
  ink-secondary: "#4a5061"
  muted: "#737a8c"
  line: "#dcdfe7"
  connected: "#187746"
  disconnected: "#c12d35"
  waiting: "#9a5b00"
  warning-bg: "#fbefd6"
  warning-ink: "#6b4200"
typography:
  page-title:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.02em"
  body:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.5
  lede:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "1.02rem"
    fontWeight: 400
    lineHeight: 1.5
  caption:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "0.85rem"
    fontWeight: 400
  group-label:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "0.72rem"
    fontWeight: 600
rounded:
  control: "8px"
  panel: "12px"
  icon-button: "7px"
  pill: "999px"
spacing:
  gap: "24px"
  panel-padding: "18px"
  control-y: "9px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-ink}"
    rounded: "{rounded.control}"
    padding: "10px 24px"
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.accent}"
    rounded: "{rounded.control}"
    padding: "5px 14px"
  input:
    backgroundColor: "{colors.ground}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "8px 10px"
  panel:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.ink}"
    rounded: "{rounded.panel}"
    padding: "18px"
  nav-item-selected:
    backgroundColor: "{colors.accent-soft}"
    textColor: "{colors.accent}"
    rounded: "{rounded.control}"
    padding: "9px 12px"
  pill:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    padding: "0 7px"
---

# Design System: Library Studio KH

## Overview

**Creative North Star: "The Quiet Control Room"**

Library is a room full of instruments that are already working: downloads, imports, a library on the NAS, companion apps on the network. The interface's job is to make that room legible without raising its voice. Pages are calm and scannable, state is stated in plain words, and the accent appears only where the owner can act or where something is selected. Nothing decorates; everything reports.

The system is deliberately small and browser-native: plain HTML and CSS served by the app itself, one system font, one accent, one panel shape. It shares the KH app family's shell — sidebar, footer, Support page, update line — but keeps its own tokens. Density is comfortable rather than airy: tables carry the work, panels group it, and the sidebar stays quiet.

The chosen control character is soft and tactile within that calm: gently rounded corners on controls, quiet borders, and hover/focus states that feel responsive rather than loud. This refines the incumbent system; it does not replace it.

**Key Characteristics:**
- One accent (indigo) for primary action, selection and state only.
- Light and dark are both first-class; the saved appearance choice wins over the OS.
- Plain-language state everywhere; a failure says what was not done and how to recover.
- Tables and panels, not cards inside cards.
- Native browser affordances kept: focus ring, selection, tabular numerals, scrollbars.

## Colors

A cool, low-saturation surface family with a single indigo accent; state colors are reserved for connection and warning facts.

### Primary
- **Calm Indigo** (`#3550c8`): primary buttons, the selected nav row's text, focused borders, inline links, the donate button. In dark mode it lifts to `#8ea2ff` rather than staying saturated.
- **Indigo Wash** (`#e3e8fb`, dark `#262d4d`): selected nav and pill backgrounds; the one tint used behind current location.

### Neutral
- **Paper Ground** (`#f3f4f7`, dark `#12141a`): page background.
- **Sidebar Haze** (`#e9ebf1`, dark `#171a22`): navigation column.
- **Panel White** (`#ffffff`, dark `#1c1f28`): cards, tables, forms, footer controls.
- **Ink** (`#1a1d26`, dark `#e6e8ef`): titles and primary text.
- **Secondary Ink** (`#4a5061`, dark `#b2b7c6`): body copy and table content.
- **Muted Ink** (`#737a8c`, dark `#8a90a2`): captions, hints, the update status line; never for body copy.
- **Hairline** (`#dcdfe7`, dark `#2c303c`): every border and divider.

### State
- **Connected Green** (`#187746`, dark `#69d99c`) and **Disconnected Red** (`#c12d35`, dark `#ff8585`): app chips and connection facts only.
- **Waiting Amber** (`#9a5b00` on `#fbefd6`, dark `#e2b25a` on `#3a2c10`): notices that need attention but are not failures.

**The One Accent Rule.** Accent marks action, selection and current location. It is never used to decorate a heading, an icon set, or a background.

## Typography

**Body Font:** system-ui stack (with -apple-system, Segoe UI, Roboto, Noto Sans fallbacks)
**Display Font:** the same family; hierarchy comes from size and weight.

**Character:** The platform's own voice. Numbers are tabular so tables and progress values line up while they change.

### Hierarchy
- **Page title** (700, 1.5rem, 1.2, -0.02em): one per screen; the screen's name.
- **Lede** (400, 1.02rem, 1.5, max 68ch): what this screen shows and what happens here.
- **Section title** (600, 1rem): panel headings and form groups.
- **Body** (400, 15px, 1.5): tables, controls, descriptions.
- **Caption / hint** (400, 0.85rem, muted): secondary facts, keyboard hints, status lines.
- **Group label** (600, 0.72rem, uppercase, 0.04em): sidebar group headers only.

**The State Is Type Rule.** A state change is written in words at body or caption size; it never relies on color alone, and it never becomes a badge unless it is a short, stable label.

## Layout

A fixed 248 px sidebar beside a content column that caps at 60rem and pads 2.25–2.5rem. Pages stack header, panels and notices with a 24 px gap; panels pad about 18 px. Tables are the default work surface; toolbar rows sit above them with search first. Below 760 px the shell becomes a single column: the sidebar collapses to a top bar with a Menu disclosure, the four primary destinations become a fixed bottom tab bar, tables turn into stacked labelled rows, and touch targets grow to 44 px.

**The One Column Rule.** One primary reading column; no split panes, no side-by-side settings columns except paired fields of the same kind.

## Elevation & Depth

Depth is tonal first: panels sit on the ground as white (or lifted-dark) surfaces with a hairline border. A single soft shadow (`0 1px 2px rgba(20,24,40,.06), 0 4px 14px rgba(20,24,40,.05)`; dark `0 1px 2px rgba(0,0,0,.3)`) separates panels from the page, never elements within a panel.

**The One Level Rule.** Surfaces are one level deep. A panel inside a panel is a grouping problem, not a styling problem.

## Shapes

Controls use gently rounded 8 px corners; panels use 12 px; icon buttons 7 px; the version pill is fully round (999 px). Borders are always one hairline, never doubled. The sidebar icon set is 18 px line art on a 1.4 stroke; footer glyphs use the shared 16 px stroke set.

**The Soft Corner Rule.** Rounded, never pill-shaped, except for the version pill, filter chips and status dots.

## Components

### Buttons
- **Shape:** 8 px radius.
- **Primary:** indigo fill, white text, 10×24 px padding, semibold; used once per panel for the committing action.
- **Secondary:** transparent with a 1 px indigo border and indigo text; hover fills with the indigo wash.
- **Disabled:** 55% opacity with a progress cursor only where the action is waiting on the app.
- **Hover / Focus:** brightness lift on primary, wash on secondary; the global 2 px accent focus ring is kept.

### Inputs / Fields
- **Style:** ground-colored fill, hairline border, 8 px radius, 8×10 px padding; labels above the field in body size.
- **Focus:** the same accent focus ring as everything else, offset 2 px.
- **Error:** the panel's status line names the problem and the recovery; fields keep their value.

### Panels / Tables
- **Corner Style:** 12 px.
- **Background:** panel surface; header rows use the same surface, separated by a hairline.
- **Shadow:** the single panel shadow.
- **Internal Padding:** 18 px; table rows 10–12 px vertical.
- **Mobile:** rows restack as label/value pairs using the column headers.

### Navigation
- 248 px column, 8 px row radius, 18 px line icons with 0.6rem gap, 13 px labels; group labels in muted uppercase. Selected row uses the indigo wash and accent text. Mobile: top bar plus four-tab bottom bar; group labels are hidden there.

### Version Pill and Update Line
- 26 px high, fully rounded, panel fill with hairline; app name at 11 px semibold and version at 10 px muted, inline. Icon-only 26 px square buttons for Check for Updates and the conditional update; accessible names and hover tooltips. One 11 px muted status line beneath the footer row.

## Do's and Don'ts

### Do:
- **Do** keep one accent for action, selection and state, and let borders and surfaces do the rest.
- **Do** write states in plain words at caption size, with the recovery step when something failed.
- **Do** use tabular numerals in tables, progress and counts.
- **Do** keep the saved appearance choice authoritative in both themes and check contrast in each.
- **Do** keep controls softly rounded (8 px) with quiet hairline borders and responsive hover/focus states.

### Don't:
- **Don't** nest panels or use cards inside cards.
- **Don't** use accent color for decoration, headings or icon sets.
- **Don't** use color alone for connection, warning or failure states.
- **Don't** add a component framework or design language; extend these tokens and components.
- **Don't** use eyebrow labels above headings, gradient text, glass/blur decoration, or emoji as icons.
- **Don't** rely on hover-only affordances where touch users need the same action.
