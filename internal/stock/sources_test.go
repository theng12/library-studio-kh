package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sourceSchemaJSON = `[{
	"name":"NZBgeek","definitionName":"nzbgeek","implementation":"Cardigann",
	"configContract":"CardigannSettings","protocol":"usenet","privacy":"private",
	"fields":[
		{"name":"baseUrl","label":"Base URL","type":"textbox","value":"http://private.example:8080"},
		{"name":"apiKey","label":"API Key","type":"textbox","value":"saved-secret","required":true},
		{"name":"username","label":"Username","type":"textbox","value":"owner"},
		{"name":"password","label":"Password","type":"password","value":"hunter2"},
		{"name":"categories","label":"Categories","type":"select","required":true,"selectOptions":[{"value":1,"label":"TV"},{"value":2,"label":"Movies"}]},
		{"name":"freeleech","label":"Freeleech","type":"checkbox","value":true},
		{"name":"note","label":"Read me","type":"info","value":"ignore"}
	]
}]`

const sourceNativeJSON = `{"id":7,"name":"Old name","enable":true,"priority":10,
	"implementation":"Cardigann","definitionName":"nzbgeek","configContract":"CardigannSettings",
	"protocol":"usenet","capabilities":{"limitsMax":100},"added":"2026-01-01T00:00:00Z",
	"fields":[
		{"name":"baseUrl","value":"http://old.example:8080"},
		{"name":"apiKey","value":"saved-secret"},
		{"name":"username","value":"olduser"},
		{"name":"password","value":"hunter2"},
		{"name":"categories","value":1},
		{"name":"freeleech","value":false},
		{"name":"nativeOnly","value":"keep"}
	]}`

func sourceMock(t *testing.T, posted *map[string]any, saved *map[string]any, tested *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/indexer/schema" && r.Method == "GET":
			w.Write([]byte(sourceSchemaJSON))
		case r.URL.Path == "/api/v1/indexer/7" && r.Method == "GET":
			w.Write([]byte(sourceNativeJSON))
		case r.URL.Path == "/api/v1/indexer" && r.Method == "POST":
			json.NewDecoder(r.Body).Decode(posted)
			w.Write([]byte(`{"id":42}`))
		case r.URL.Path == "/api/v1/indexer/7" && r.Method == "PUT":
			json.NewDecoder(r.Body).Decode(saved)
			w.WriteHeader(http.StatusAccepted)
		case r.URL.Path == "/api/v1/indexer/test" && r.Method == "POST":
			json.NewDecoder(r.Body).Decode(tested)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fieldValue(raw map[string]any, name string) any {
	fields, _ := raw["fields"].([]any)
	for _, x := range fields {
		m, _ := x.(map[string]any)
		if asStr(m["name"]) == name {
			return m["value"]
		}
	}
	return nil
}

func TestSourceCatalogAllowlistsShapeAndHidesValues(t *testing.T) {
	srv := sourceMock(t, nil, nil, nil)
	catalog, err := NewArr("Prowlarr", srv.URL, "{{KEY_1ce1a07b}}").IndexerCatalog()
	if err != nil || len(catalog) != 1 {
		t.Fatal(err, catalog)
	}
	entry := catalog[0]
	if entry.Key != "definition:nzbgeek" || entry.Name != "NZBgeek" || entry.Protocol != "usenet" {
		t.Fatalf("entry: %+v", entry)
	}
	body, _ := json.Marshal(catalog)
	for _, leak := range []string{"http://", "private.example", "saved-secret", "hunter2", "owner"} {
		if strings.Contains(string(body), leak) {
			t.Fatalf("catalog leaked %q: %s", leak, body)
		}
	}
	byName := map[string]SourceField{}
	for _, f := range entry.Fields {
		byName[f.Name] = f
	}
	if byName["apiKey"].Type != "password" || !byName["apiKey"].Supported {
		t.Fatalf("apiKey must be an editable secret: %+v", byName["apiKey"])
	}
	if byName["baseUrl"].Type != "text" || !byName["baseUrl"].Secret {
		t.Fatalf("URL value must be hidden but editable: %+v", byName["baseUrl"])
	}
	if byName["categories"].Type != "select" || len(byName["categories"].Options) != 2 {
		t.Fatalf("select options must be published: %+v", byName["categories"])
	}
	if byName["freeleech"].Type != "bool" || byName["note"].Supported {
		t.Fatalf("types wrong: %+v", byName)
	}
}

func TestCreateIndexerIsExplicitDisabledAndTyped(t *testing.T) {
	var posted map[string]any
	srv := sourceMock(t, &posted, nil, nil)
	id, err := NewArr("Prowlarr", srv.URL, "{{KEY_1ce1a07b}}").CreateIndexer(SourceInput{
		Key: "definition:nzbgeek", Name: "My source",
		Fields: map[string]any{"apiKey": "new-secret", "username": "me", "categories": float64(2), "freeleech": true},
	})
	if err != nil || id != 42 {
		t.Fatal(err, id)
	}
	if posted["name"] != "My source" || posted["enable"] != false || posted["priority"] != float64(25) {
		t.Fatalf("new source must stay disabled at priority 25: %+v", posted)
	}
	if posted["protocol"] != "usenet" || posted["definitionName"] != "nzbgeek" {
		t.Fatalf("schema identity lost: %+v", posted)
	}
	if fieldValue(posted, "apiKey") != "new-secret" || fieldValue(posted, "categories") != float64(2) || fieldValue(posted, "freeleech") != true {
		t.Fatalf("typed values not sent: %+v", posted["fields"])
	}
}

func TestCreateIndexerRefusesUnsupportedRequiredField(t *testing.T) {
	srv := sourceMock(t, nil, nil, nil)
	_, err := NewArr("Prowlarr", srv.URL, "{{KEY_1ce1a07b}}").CreateIndexer(SourceInput{
		Key: "definition:nzbgeek", Name: "My source",
		Fields: map[string]any{"apiKey": "secret", "categories": float64(1), "mystery": "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "isn't part") {
		t.Fatalf("unknown field must be refused: %v", err)
	}
}

func TestUpdateIndexerPreservesBlankSecretsAndNativeFields(t *testing.T) {
	var saved map[string]any
	srv := sourceMock(t, nil, &saved, nil)
	enable := false
	priority := 20
	err := NewArr("Prowlarr", srv.URL, "{{KEY_1ce1a07b}}").UpdateIndexer(SourceInput{
		ID: 7, Name: "New name", Enable: &enable, Priority: &priority,
		Fields: map[string]any{"apiKey": "", "password": "", "baseUrl": "", "username": "newuser", "freeleech": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved["name"] != "New name" || saved["enable"] != false || saved["priority"] != float64(20) {
		t.Fatalf("top-level merge wrong: %+v", saved)
	}
	if saved["capabilities"] == nil || saved["added"] == nil || fieldValue(saved, "nativeOnly") != "keep" {
		t.Fatalf("native fields were dropped: %+v", saved)
	}
	if fieldValue(saved, "apiKey") != "saved-secret" || fieldValue(saved, "password") != "hunter2" || fieldValue(saved, "baseUrl") != "http://old.example:8080" {
		t.Fatalf("blank secrets/URLs must keep their saved value: %+v", saved["fields"])
	}
	if fieldValue(saved, "username") != "newuser" || fieldValue(saved, "freeleech") != true {
		t.Fatalf("submitted fields not applied: %+v", saved["fields"])
	}
}

func TestIndexerForEditHidesSecretsAndNativeValues(t *testing.T) {
	srv := sourceMock(t, nil, nil, nil)
	edit, err := NewArr("Prowlarr", srv.URL, "{{KEY_1ce1a07b}}").IndexerForEdit(7)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(edit)
	for _, leak := range []string{"http://", "old.example", "saved-secret", "hunter2", "keep"} {
		if strings.Contains(string(body), leak) {
			t.Fatalf("edit form leaked %q: %s", leak, body)
		}
	}
	byName := map[string]SourceField{}
	for _, f := range edit.Fields {
		byName[f.Name] = f
	}
	if byName["apiKey"].Value != nil || !byName["apiKey"].HasValue || !byName["apiKey"].Secret {
		t.Fatalf("apiKey must report only that a value exists: %+v", byName["apiKey"])
	}
	if byName["baseUrl"].Value != nil || !byName["baseUrl"].HasValue {
		t.Fatalf("URL value must stay hidden: %+v", byName["baseUrl"])
	}
	if byName["username"].Value != "olduser" {
		t.Fatalf("safe value missing: %+v", byName["username"])
	}
}

func TestTestIndexerUsesProwlarrTestEndpointWithoutSaving(t *testing.T) {
	var tested map[string]any
	srv := sourceMock(t, nil, nil, &tested)
	err := NewArr("Prowlarr", srv.URL, "{{KEY_1ce1a07b}}").TestIndexer(SourceInput{
		ID: 7, Fields: map[string]any{"apiKey": "", "username": "tester"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tested["id"] != float64(7) || fieldValue(tested, "apiKey") != "saved-secret" || fieldValue(tested, "username") != "tester" {
		t.Fatalf("test must merge saved secrets: %+v", tested)
	}
}

func TestSourceActionErrorsRedactURLsAndKeys(t *testing.T) {
	const key = "{{KEY_1ce1a07b}}"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/indexer/schema" {
			w.Write([]byte(sourceSchemaJSON))
			return
		}
		http.Error(w, `[{"errorMessage":"failed http://10.0.0.5:9696/api?apikey=`+key+` password=hunter2"}]`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := NewArr("Prowlarr", srv.URL, key).CreateIndexer(SourceInput{
		Key: "definition:nzbgeek", Name: "Mine",
		Fields: map[string]any{"apiKey": "secret", "categories": float64(1)},
	})
	if err == nil {
		t.Fatal("failure expected")
	}
	msg := err.Error()
	for _, leak := range []string{"http://", "10.0.0.5", key, "hunter2", "password="} {
		if strings.Contains(msg, leak) {
			t.Fatalf("action error leaked %q: %s", leak, msg)
		}
	}
}

func TestSourceProtocolAcceptsNumericAndStringEnums(t *testing.T) {
	if got := sourceProtocol(float64(1)); got != "usenet" {
		t.Fatalf("1: %q", got)
	}
	if got := sourceProtocol(float64(2)); got != "torrent" {
		t.Fatalf("2: %q", got)
	}
	if got := sourceProtocol("Usenet"); got != "usenet" {
		t.Fatalf("Usenet: %q", got)
	}
	if got := sourceProtocol("2"); got != "torrent" {
		t.Fatalf("2 string: %q", got)
	}
}

func TestSourceReadsWhitelistAndSanitize(t *testing.T) {
	const apiKey = "s3cr3t-api-key-value"
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch r.URL.Path {
		case "/api/v1/indexer":
			w.Write([]byte(`[{"id":7,"name":"Nzbgeek","enable":true,"priority":25,"protocol":"usenet",
				"fields":[{"name":"apiKey","value":"` + apiKey + `"},{"name":"baseUrl","value":"http://indexer.example:8080"}],
				"baseUrl":"http://indexer.example:8080","password":"hunter2","downloadUrl":"http://indexer.example/get?t=abc"}]`))
		case "/api/v1/indexerstatus":
			w.Write([]byte(`[{"id":3,"indexerId":7,"disabledTill":"2026-11-06T10:00:00Z"}]`))
		case "/api/v1/applications":
			w.Write([]byte(`[{"id":1,"name":"Sonarr","syncLevel":"FullSync","fields":[{"name":"apiKey","value":"appkey"}]}]`))
		case "/api/v1/health":
			w.Write([]byte(`[{"type":"warning","message":"Indexer error: call http://10.0.0.5:8989/api?apikey=` + apiKey + ` passkey deadbeef magnet:?xt=urn:btih:aaa"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewArr("Prowlarr", srv.URL, apiKey)
	idx, err := p.Indexers()
	if err != nil || len(idx) != 1 {
		t.Fatal(err, idx)
	}
	raw := idx[0]
	if raw.ID != 7 || raw.Name != "Nzbgeek" || raw.Enabled == nil || !*raw.Enabled || raw.Priority == nil || *raw.Priority != 25 || raw.Protocol != "usenet" {
		t.Fatalf("indexer fields wrong: %+v", raw)
	}
	st, err := p.IndexerStatuses()
	if err != nil || !st[7].Equal(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)) {
		t.Fatal(err, st)
	}
	links, err := p.Applications()
	if err != nil || len(links) != 1 || links[0].Name != "Sonarr" || links[0].SyncLevel != "FullSync" {
		t.Fatal(err, links)
	}
	health, err := p.Health()
	if err != nil || len(health) != 1 {
		t.Fatal(err, health)
	}
	msg := health[0].Message
	for _, leak := range []string{"http", "10.0.0.5", "passkey", "deadbeef", "magnet", apiKey} {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(leak)) {
			t.Fatalf("health message leaked %q: %q", leak, msg)
		}
	}
	if health[0].Type != "warning" {
		t.Fatalf("type wrong: %+v", health[0])
	}
	for _, m := range methods {
		if m != http.MethodGet {
			t.Fatalf("mutation request sent: %s", m)
		}
	}
}

func TestSourceIndexerStatusFailureIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := NewArr("Prowlarr", srv.URL, "k").IndexerStatuses(); err == nil {
		t.Fatal("status failure must be an error, never read as healthy")
	}
}

func TestSanitizeStripsOwnBaseAndEnvKey(t *testing.T) {
	t.Setenv("PROWLARR__APIKEY", "env-secret-key")
	a := NewArr("Prowlarr", "http://127.0.0.1:9696", "local-key")
	got := a.sanitizeText("failed calling http://127.0.0.1:9696/api/v1/health key local-key env-secret-key")
	for _, leak := range []string{"http://127.0.0.1:9696", "local-key", "env-secret-key"} {
		if strings.Contains(got, leak) {
			t.Fatalf("leaked %q: %q", leak, got)
		}
	}
}
