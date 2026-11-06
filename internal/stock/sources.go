package stock

import (
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Source shapes only carry allowlisted fields; upstream config, credentials and
// base URLs are never decoded into them.

type SourceIndexer struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	Enabled       *bool      `json:"enabled"`
	Protocol      string     `json:"protocol,omitempty"`
	Priority      *int       `json:"priority,omitempty"`
	DisabledUntil *time.Time `json:"disabledUntil,omitempty"`
}

type SourceLink struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	SyncLevel string `json:"syncLevel,omitempty"`
}

type SourceHealth struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Indexers lists Prowlarr's configured indexers with their observed enable flag.
func (a *Arr) Indexers() ([]SourceIndexer, error) {
	var raw []map[string]any
	if err := a.do("GET", "/indexer", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SourceIndexer, 0, len(raw))
	for _, r := range raw {
		s := SourceIndexer{ID: asInt(r["id"]), Name: a.sanitizeText(asStr(r["name"])), Protocol: sourceProtocol(r["protocol"])}
		if v, ok := r["enable"].(bool); ok {
			s.Enabled = &v
		}
		if v, ok := r["priority"].(float64); ok {
			n := int(v)
			s.Priority = &n
		}
		out = append(out, s)
	}
	return out, nil
}

// IndexerStatuses maps indexer id to the until-when Prowlarr has disabled it
// (upstream field: disabledTill). Any read failure is an error so the caller
// reports the unknown state instead of letting it read as healthy.
func (a *Arr) IndexerStatuses() (map[int]time.Time, error) {
	var raw []map[string]any
	if err := a.do("GET", "/indexerstatus", nil, &raw); err != nil {
		return nil, err
	}
	out := map[int]time.Time{}
	for _, r := range raw {
		if t, ok := r["disabledTill"].(string); ok && t != "" {
			if when, err := time.Parse(time.RFC3339, t); err == nil && !when.IsZero() {
				out[asInt(r["indexerId"])] = when
			}
		}
	}
	return out, nil
}

// Applications lists Prowlarr's links to the library apps (name and syncLevel only).
func (a *Arr) Applications() ([]SourceLink, error) {
	var raw []map[string]any
	if err := a.do("GET", "/applications", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SourceLink, 0, len(raw))
	for _, r := range raw {
		out = append(out, SourceLink{ID: asInt(r["id"]), Name: a.sanitizeText(asStr(r["name"])), SyncLevel: a.sanitizeText(asStr(r["syncLevel"]))})
	}
	return out, nil
}

// Health reads Prowlarr's reported health messages, sanitized before they leave.
func (a *Arr) Health() ([]SourceHealth, error) {
	var raw []map[string]any
	if err := a.do("GET", "/health", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SourceHealth, 0, len(raw))
	for _, r := range raw {
		msg := a.sanitizeText(asStr(r["message"]))
		if msg == "" {
			continue
		}
		typ := asStr(r["type"])
		if !reSafe.MatchString(typ) {
			typ = "Message"
		}
		out = append(out, SourceHealth{Type: typ, Message: msg})
	}
	return out, nil
}

// LibraryIndexers lists one library app's configured indexers from its /indexer API.
func (a *Arr) LibraryIndexers() ([]SourceIndexer, error) {
	var raw []map[string]any
	if err := a.do("GET", "/indexer", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]SourceIndexer, 0, len(raw))
	for _, r := range raw {
		s := SourceIndexer{ID: asInt(r["id"]), Name: a.sanitizeText(asStr(r["name"])), Protocol: sourceProtocol(r["protocol"])}
		if v, ok := r["enable"].(bool); ok {
			s.Enabled = &v
		} else {
			known := 0
			for _, field := range []string{"enableRss", "enableAutomaticSearch", "enableInteractiveSearch"} {
				if enabled, ok := r[field].(bool); ok {
					known++
					if enabled {
						value := true
						s.Enabled = &value
						break
					}
				}
			}
			if s.Enabled == nil && known == 3 {
				value := false
				s.Enabled = &value
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// ---- Source catalog and explicit source actions ----

// SourceOption is a select choice Prowlarr publishes. Values never carry credentials.
type SourceOption struct {
	Label string `json:"label"`
	Value any    `json:"value"`
}

// SourceField is the allowlisted form shape of one Prowlarr indexer setting.
// Password and URL-shaped values are never returned; HasValue says whether one is saved.
type SourceField struct {
	Name      string         `json:"name"`
	Label     string         `json:"label"`
	Type      string         `json:"type"`
	Required  bool           `json:"required"`
	Help      string         `json:"help,omitempty"`
	Advanced  bool           `json:"advanced,omitempty"`
	Options   []SourceOption `json:"options,omitempty"`
	Supported bool           `json:"supported"`
	Blocked   string         `json:"blocked,omitempty"`
	Secret    bool           `json:"secret,omitempty"`
	Value     any            `json:"value"`
	HasValue  bool           `json:"hasValue,omitempty"`
}

// SourceCatalogEntry is one provider definition from Prowlarr's live schema.
type SourceCatalogEntry struct {
	Key      string        `json:"key"`
	Name     string        `json:"name"`
	Protocol string        `json:"protocol"`
	Privacy  string        `json:"privacy,omitempty"`
	Fields   []SourceField `json:"fields"`
	Blocked  []string      `json:"blocked,omitempty"`
}

// SourceEdit is one configured source, safe to show in an edit form.
type SourceEdit struct {
	ID       int           `json:"id"`
	Key      string        `json:"key"`
	Name     string        `json:"name"`
	Enabled  bool          `json:"enabled"`
	Priority int           `json:"priority"`
	Protocol string        `json:"protocol"`
	Fields   []SourceField `json:"fields"`
	Blocked  []string      `json:"blocked,omitempty"`
}

// SourceInput is an explicit add, save or test request. ID selects an existing source.
type SourceInput struct {
	ID       int            `json:"id"`
	Key      string         `json:"key"`
	Name     string         `json:"name"`
	Enable   *bool          `json:"enable"`
	Priority *int           `json:"priority"`
	Fields   map[string]any `json:"fields"`
}

// IndexerCatalog lists Prowlarr's live provider definitions. No schema value is
// returned: Prowlarr schemas can carry defaults that should not cross the API.
func (a *Arr) IndexerCatalog() ([]SourceCatalogEntry, error) {
	schemas, err := a.indexerSchema()
	if err != nil {
		return nil, a.SafeError(err)
	}
	out := make([]SourceCatalogEntry, 0, len(schemas))
	for _, s := range schemas {
		fields, blocked := a.publicFields(fieldDefs(s), nil)
		if len(fields) == 0 {
			continue
		}
		out = append(out, SourceCatalogEntry{
			Key: schemaKey(s), Name: a.sanitizeText(asStr(s["name"])),
			Protocol: sourceProtocol(s["protocol"]), Privacy: a.sanitizeText(asStr(s["privacy"])),
			Fields: fields, Blocked: blocked,
		})
	}
	return out, nil
}

// IndexerForEdit reads one configured source for its edit form. The raw object is
// never returned; only allowlisted fields and non-secret current values are.
func (a *Arr) IndexerForEdit(id int) (SourceEdit, error) {
	if id <= 0 {
		return SourceEdit{}, errors.New("Choose a configured source.")
	}
	raw, err := a.indexerRaw(id)
	if err != nil {
		return SourceEdit{}, a.SafeError(err)
	}
	schemas, err := a.indexerSchema()
	if err != nil {
		return SourceEdit{}, a.SafeError(err)
	}
	defs, key := matchSchema(schemas, raw)
	if defs == nil {
		defs = inferDefs(raw)
	}
	fields, blocked := a.publicFields(defs, raw)
	out := SourceEdit{
		ID: asInt(raw["id"]), Key: key, Name: a.sanitizeText(asStr(raw["name"])),
		Protocol: sourceProtocol(raw["protocol"]), Priority: asInt(raw["priority"]), Fields: fields, Blocked: blocked,
	}
	if v, ok := raw["enable"].(bool); ok {
		out.Enabled = v
	}
	return out, nil
}

// CreateIndexer adds one source only after the owner explicitly asks. New sources
// stay disabled unless the request enables them.
func (a *Arr) CreateIndexer(in SourceInput) (int, error) {
	raw, err := a.buildIndexer(in)
	if err != nil {
		return 0, err
	}
	var created map[string]any
	if err := a.do("POST", "/indexer", raw, &created); err != nil {
		return 0, a.SafeError(err)
	}
	return asInt(created["id"]), nil
}

// UpdateIndexer merges the submitted settings into Prowlarr's current object, so
// every field this screen does not edit (including secrets) is preserved.
func (a *Arr) UpdateIndexer(in SourceInput) error {
	if in.ID <= 0 {
		return errors.New("Choose a configured source.")
	}
	raw, err := a.buildIndexer(in)
	if err != nil {
		return err
	}
	if err := a.do("PUT", fmt.Sprintf("/indexer/%d", in.ID), raw, nil); err != nil {
		return a.SafeError(err)
	}
	return nil
}

// TestIndexer runs Prowlarr's own source test without saving anything.
func (a *Arr) TestIndexer(in SourceInput) error {
	raw, err := a.buildIndexer(in)
	if err != nil {
		return err
	}
	if err := a.do("POST", "/indexer/test", raw, nil); err != nil {
		return a.SafeError(err)
	}
	return nil
}

func (a *Arr) indexerSchema() ([]map[string]any, error) {
	var raw []map[string]any
	return raw, a.do("GET", "/indexer/schema", nil, &raw)
}

func (a *Arr) indexerRaw(id int) (map[string]any, error) {
	var raw map[string]any
	return raw, a.do("GET", fmt.Sprintf("/indexer/%d", id), nil, &raw)
}

// buildIndexer creates or merges the object Prowlarr expects. Fields it does not
// understand stay exactly as Prowlarr returned them.
func (a *Arr) buildIndexer(in SourceInput) (map[string]any, error) {
	schemas, err := a.indexerSchema()
	if err != nil {
		return nil, a.SafeError(err)
	}
	var raw map[string]any
	var defs []map[string]any
	isNew := in.ID <= 0
	if isNew {
		raw, _ = findSchema(schemas, in.Key)
		if raw == nil {
			return nil, errors.New("That source isn't in Prowlarr's catalog now. Refresh the list and choose it again.")
		}
		raw = cloneMap(raw)
		defs = fieldDefs(raw)
		delete(raw, "id")
		raw["enable"] = false
		raw["priority"] = 25
	} else {
		raw, err = a.indexerRaw(in.ID)
		if err != nil {
			return nil, a.SafeError(err)
		}
		defs, _ = matchSchema(schemas, raw)
		if defs == nil {
			defs = inferDefs(raw)
		}
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		if len(name) > 200 || strings.ContainsFunc(name, unicode.IsControl) {
			return nil, errors.New("Give the source a name of up to 200 ordinary characters.")
		}
		raw["name"] = name
	}
	if isNew && strings.TrimSpace(asStr(raw["name"])) == "" {
		return nil, errors.New("Give the source a name.")
	}
	if in.Enable != nil {
		raw["enable"] = *in.Enable
	}
	if in.Priority != nil {
		if *in.Priority < 1 || *in.Priority > 50 {
			return nil, errors.New("Priority must be a whole number from 1 to 50.")
		}
		raw["priority"] = *in.Priority
	}
	if err := a.applySourceFields(defs, raw, in.Fields, isNew); err != nil {
		return nil, err
	}
	return raw, nil
}

func (a *Arr) applySourceFields(defs []map[string]any, target map[string]any, submitted map[string]any, isNew bool) error {
	if submitted == nil {
		submitted = map[string]any{}
	}
	if len(submitted) > 200 {
		return errors.New("Too many settings were sent.")
	}
	fields, byName := fieldMaps(target)
	known := map[string]bool{}
	for _, def := range defs {
		name := asStr(def["name"])
		if name == "" {
			continue
		}
		known[name] = true
		label := a.fieldLabel(def)
		kind, supported, blocked := publicFieldType(def)
		required := asBool(def["required"])
		secret := secretField(def)
		trueSecret := isTrueSecret(def)
		value, submittedValue := submitted[name]
		f := byName[name]
		if f == nil {
			if !isNew && !submittedValue {
				continue // an existing source never gains schema-only defaults
			}
			f = cloneMap(def)
			delete(f, "value")
			fields = append(fields, f)
			byName[name] = f
		}
		if submittedValue {
			if !supported || kind == "info" {
				if blocked == "" {
					blocked = "This setting can't be edited here yet."
				}
				return fmt.Errorf("%s: %s", label, blocked)
			}
			if !isNew && kind == "select" && valueBlank(value) {
				continue // unchanged or no-longer-offered choice keeps the native value
			}
			if secret && valueBlank(value) {
				if isNew {
					if required {
						return fmt.Errorf("%s is required.", label)
					}
					f["value"] = ""
				}
				continue // an edited source keeps its saved secret
			}
			coerced, err := coerceField(def, kind, value)
			if err != nil {
				return fmt.Errorf("%s: %w", label, err)
			}
			if required && !trueSecret && valueBlank(coerced) {
				return fmt.Errorf("%s is required.", label)
			}
			f["value"] = coerced
			continue
		}
		if isNew && trueSecret {
			f["value"] = "" // never adopt a schema default as a new secret
			if required {
				return fmt.Errorf("%s is required.", label)
			}
		}
		if isNew && required && !supported && kind != "info" {
			return fmt.Errorf("This source needs %s, which this screen can't set yet. Use Prowlarr to add it.", label)
		}
		if required && supported && !trueSecret && valueBlank(f["value"]) {
			return fmt.Errorf("%s is required.", label)
		}
	}
	for name := range submitted {
		if !known[name] {
			return fmt.Errorf("The setting %q isn't part of this source.", truncateText(name, 60))
		}
	}
	target["fields"] = fields
	return nil
}

func (a *Arr) publicFields(defs []map[string]any, current map[string]any) ([]SourceField, []string) {
	currentValues := map[string]any{}
	hasCurrent := map[string]bool{}
	if current != nil {
		_, byName := fieldMaps(current)
		for name, f := range byName {
			currentValues[name] = f["value"]
			hasCurrent[name] = true
		}
	}
	out := make([]SourceField, 0, len(defs))
	blocked := []string{}
	for _, def := range defs {
		name := asStr(def["name"])
		if name == "" {
			continue
		}
		kind, supported, why := publicFieldType(def)
		f := SourceField{
			Name: name, Label: a.fieldLabel(def), Type: kind, Required: asBool(def["required"]),
			Help: a.sanitizeText(asStr(def["helpText"])), Advanced: asBool(def["advanced"]),
			Supported: supported, Blocked: why, Secret: secretField(def),
			Options: publicOptions(a, def),
		}
		if hasCurrent[name] {
			v := currentValues[name]
			if sensitiveValue(v) {
				f.Secret = true
			}
			if f.Secret || !f.Supported {
				f.HasValue = !valueBlank(v)
			} else {
				f.Value, f.HasValue = v, true
			}
		}
		if f.Required && !f.Supported && f.Type != "info" {
			blocked = append(blocked, "This source needs “"+f.Label+"”, which this screen can't set yet. Add or edit it in Prowlarr.")
		}
		out = append(out, f)
	}
	return out, blocked
}

func publicOptions(a *Arr, def map[string]any) []SourceOption {
	raw, _ := def["selectOptions"].([]any)
	out := make([]SourceOption, 0, len(raw))
	for _, x := range raw {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		label := a.sanitizeText(asStr(m["label"]))
		if label == "" {
			label = a.sanitizeText(fmt.Sprint(m["value"]))
		}
		out = append(out, SourceOption{Label: label, Value: m["value"]})
	}
	return out
}

func publicFieldType(def map[string]any) (string, bool, string) {
	raw := strings.ToLower(asStr(def["type"]))
	name, label := asStr(def["name"]), asStr(def["label"])
	if raw == "info" {
		return "info", false, "This is a read-only note from Prowlarr; it has no setting to change."
	}
	if isSecretName(name, label) || raw == "password" {
		return "password", true, ""
	}
	switch raw {
	case "textbox", "url", "path", "":
		return "text", true, ""
	case "checkbox":
		return "bool", true, ""
	case "number":
		return "numeric", true, ""
	case "select":
		return "select", true, ""
	default:
		return raw, false, "This setting type (" + raw + ") can't be edited here yet. Change it in Prowlarr."
	}
}

func secretField(def map[string]any) bool {
	name, label := asStr(def["name"]), asStr(def["label"])
	return isTrueSecret(def) || isSensitiveURL(name, label)
}

// isTrueSecret excludes URLs: a URL is hidden from read responses, but a new
// source may still use the provider's own default URL.
func isTrueSecret(def map[string]any) bool {
	name, label := asStr(def["name"]), asStr(def["label"])
	return isSecretName(name, label) || strings.EqualFold(asStr(def["type"]), "password")
}

func isSecretName(name, label string) bool {
	s := strings.ToLower(name + " " + label)
	for _, word := range []string{"apikey", "api key", "api_key", "passkey", "password", "passwd", "cookie", "token", "secret", "credential"} {
		if strings.Contains(s, word) {
			return true
		}
	}
	return false
}

func isSensitiveURL(name, label string) bool {
	s := strings.ToLower(name + " " + label)
	return strings.Contains(s, "url") || strings.Contains(s, "link") || strings.Contains(s, "uri")
}

func coerceField(def map[string]any, kind string, value any) (any, error) {
	switch kind {
	case "text":
		s, ok := value.(string)
		if !ok {
			return nil, errors.New("enter text")
		}
		if len(s) > 8192 {
			return nil, errors.New("the value is too long")
		}
		return strings.TrimSpace(s), nil
	case "password":
		s, ok := value.(string)
		if !ok {
			return nil, errors.New("enter text")
		}
		if len(s) > 8192 {
			return nil, errors.New("the value is too long")
		}
		return s, nil
	case "bool":
		b, ok := value.(bool)
		if !ok {
			return nil, errors.New("choose on or off")
		}
		return b, nil
	case "numeric":
		n, ok := numberValue(value)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, errors.New("enter a number")
		}
		return n, nil
	case "select":
		options, _ := def["selectOptions"].([]any)
		for _, x := range options {
			m, ok := x.(map[string]any)
			if ok && (fmt.Sprint(m["value"]) == fmt.Sprint(value) || asStr(m["label"]) == fmt.Sprint(value)) {
				return m["value"], nil
			}
		}
		return nil, errors.New("choose one of the available options")
	}
	return nil, errors.New("this setting can't be edited here yet")
}

func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

func fieldDefs(raw map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, f := range asMaps(raw["fields"]) {
		out = append(out, f)
	}
	return out
}

func fieldMaps(raw map[string]any) ([]any, map[string]map[string]any) {
	fields, _ := raw["fields"].([]any)
	byName := map[string]map[string]any{}
	for _, x := range fields {
		if m, ok := x.(map[string]any); ok {
			if name := asStr(m["name"]); name != "" {
				byName[name] = m
			}
		}
	}
	return fields, byName
}

func asMaps(v any) []map[string]any {
	raw, _ := v.([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (a *Arr) fieldLabel(def map[string]any) string {
	label := strings.TrimSpace(asStr(def["label"]))
	if label == "" {
		label = asStr(def["name"])
	}
	if label == "" {
		label = "Setting"
	}
	return truncateText(a.sanitizeText(label), 120)
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func schemaKey(s map[string]any) string {
	if d := strings.TrimSpace(asStr(s["definitionName"])); d != "" {
		return "definition:" + d
	}
	return "implementation:" + asStr(s["implementation"]) + ":" + asStr(s["name"])
}

func findSchema(schemas []map[string]any, key string) (map[string]any, bool) {
	if key == "" {
		return nil, false
	}
	for _, s := range schemas {
		if schemaKey(s) == key {
			return s, true
		}
	}
	return nil, false
}

func matchSchema(schemas []map[string]any, raw map[string]any) ([]map[string]any, string) {
	defName := strings.TrimSpace(asStr(raw["definitionName"]))
	impl := asStr(raw["implementation"])
	contract := asStr(raw["configContract"])
	name := asStr(raw["name"])
	var fallback map[string]any
	for _, s := range schemas {
		if defName != "" && asStr(s["definitionName"]) == defName {
			return fieldDefs(s), schemaKey(s)
		}
		if asStr(s["implementation"]) != impl {
			continue
		}
		if name != "" && asStr(s["name"]) == name {
			return fieldDefs(s), schemaKey(s)
		}
		if asStr(s["configContract"]) == contract {
			if fallback == nil {
				fallback = s
			} else {
				fallback = nil // ambiguous; don't guess
				break
			}
		}
	}
	if fallback != nil {
		return fieldDefs(fallback), schemaKey(fallback)
	}
	return nil, ""
}

// inferDefs is used only when Prowlarr no longer publishes a definition for an
// existing source. It edits simple scalar fields and leaves every other field untouched.
func inferDefs(raw map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, f := range asMaps(raw["fields"]) {
		m := cloneMap(f)
		switch f["value"].(type) {
		case bool:
			m["type"] = "checkbox"
		case float64:
			m["type"] = "number"
		default:
			m["type"] = "textbox"
		}
		out = append(out, m)
	}
	return out
}

func sourceProtocol(v any) string {
	switch x := v.(type) {
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		switch s {
		case "1", "usenet":
			return "usenet"
		case "2", "torrent":
			return "torrent"
		}
		return s
	case float64:
		switch int(x) {
		case 1:
			return "usenet"
		case 2:
			return "torrent"
		}
	}
	return ""
}

func valueBlank(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

// sensitiveValue hides a URL-like value even when its field name looks ordinary.
func sensitiveValue(v any) bool {
	s, ok := v.(string)
	return ok && reURL.MatchString(strings.TrimSpace(s))
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var (
	reURL  = regexp.MustCompile(`(?i)\b(?:https?|magnet):\S+|www\.[^\s]+`)
	reKey  = regexp.MustCompile(`(?i)\b(?:api[ _-]?key|apikey|passkey|password|passwd|token|secret|url)\b\s*[:=]?\s*\S+`)
	reSafe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// sanitizeText strips URLs, key-shaped values and this app's own secrets so no
// config, password, API key, base URL, passkey or download link can leave.
func (a *Arr) sanitizeText(s string) string {
	pairs := []string{}
	for _, v := range []string{a.key, os.Getenv("SONARR__APIKEY"), os.Getenv("RADARR__APIKEY"),
		os.Getenv("LIDARR__APIKEY"), os.Getenv("PROWLARR__APIKEY"), a.base} {
		if v != "" {
			pairs = append(pairs, v, "REDACTED")
		}
	}
	if len(pairs) > 0 {
		s = strings.NewReplacer(pairs...).Replace(s)
	}
	s = reURL.ReplaceAllString(s, "REDACTED")
	s = reKey.ReplaceAllString(s, "REDACTED")
	return strings.TrimSpace(s)
}

// SafeError keeps useful upstream reasons without returning source URLs or credentials.
func (a *Arr) SafeError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(a.sanitizeText(err.Error()))
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func asInt(v any) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return 0
}
