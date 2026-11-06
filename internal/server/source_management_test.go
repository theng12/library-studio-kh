package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

const managementSchema = `[{
	"name":"NZBgeek","definitionName":"nzbgeek","implementation":"Cardigann",
	"configContract":"CardigannSettings","protocol":"usenet","privacy":"private",
	"fields":[
		{"name":"baseUrl","label":"Base URL","type":"textbox","value":"http://private.example:8080"},
		{"name":"apiKey","label":"API Key","type":"textbox","value":"saved-secret","required":true},
		{"name":"username","label":"Username","type":"textbox","value":"owner"},
		{"name":"categories","label":"Categories","type":"select","required":true,"selectOptions":[{"value":1,"label":"TV"},{"value":2,"label":"Movies"}]},
		{"name":"freeleech","label":"Freeleech","type":"checkbox","value":true}
	]
}]`

const managementNative = `{"id":7,"name":"Old name","enable":true,"priority":10,
	"implementation":"Cardigann","definitionName":"nzbgeek","configContract":"CardigannSettings",
	"protocol":"usenet","capabilities":{"limitsMax":100},"added":"2026-01-01T00:00:00Z",
	"fields":[
		{"name":"baseUrl","value":"http://old.example:8080"},
		{"name":"apiKey","value":"saved-secret"},
		{"name":"username","value":"olduser"},
		{"name":"categories","value":1},
		{"name":"freeleech","value":false},
		{"name":"nativeOnly","value":"keep"}
	]}`

func managementMock(t *testing.T, key string, posted, saved, tested *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/indexer/schema" && r.Method == "GET":
			w.Write([]byte(managementSchema))
		case r.URL.Path == "/api/v1/indexer/7" && r.Method == "GET":
			w.Write([]byte(managementNative))
		case r.URL.Path == "/api/v1/indexer" && r.Method == "POST":
			json.NewDecoder(r.Body).Decode(posted)
			w.Write([]byte(`{"id":42}`))
		case r.URL.Path == "/api/v1/indexer/7" && r.Method == "PUT":
			json.NewDecoder(r.Body).Decode(saved)
			w.WriteHeader(http.StatusAccepted)
		case r.URL.Path == "/api/v1/indexer/test" && r.Method == "POST":
			json.NewDecoder(r.Body).Decode(tested)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func managementMux(t *testing.T, stockURL string) *http.ServeMux {
	t.Helper()
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.Prowlarr = config.App{URL: stockURL, APIKey: "{{KEY_1ce1a07b}}"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(store, home).registerSources(mux)
	return mux
}

func managementCall(t *testing.T, mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	return rw
}

func managementFieldValue(raw map[string]any, name string) any {
	fields, _ := raw["fields"].([]any)
	for _, x := range fields {
		m, _ := x.(map[string]any)
		if m["name"] == name {
			return m["value"]
		}
	}
	return nil
}

func TestSourceCatalogAndEditRoutesHideSecrets(t *testing.T) {
	srv := managementMock(t, "{{KEY_1ce1a07b}}", nil, nil, nil)
	mux := managementMux(t, srv.URL)

	catalog := managementCall(t, mux, "GET", "/api/sources/catalog", "")
	if catalog.Code != 200 {
		t.Fatalf("catalog: %d %s", catalog.Code, catalog.Body.String())
	}
	body := catalog.Body.String()
	for _, leak := range []string{"saved-secret", "http://", "private.example", "configContract", "capabilities"} {
		if strings.Contains(body, leak) {
			t.Fatalf("catalog leaked %q: %s", leak, body)
		}
	}
	for _, want := range []string{`"type":"select"`, `"type":"bool"`, `"label":"TV"`, `"supported":true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("catalog missing %q: %s", want, body)
		}
	}

	edit := managementCall(t, mux, "GET", "/api/sources/7", "")
	if edit.Code != 200 {
		t.Fatalf("edit: %d %s", edit.Code, edit.Body.String())
	}
	body = edit.Body.String()
	for _, leak := range []string{"saved-secret", "http://", "old.example", "keep"} {
		if strings.Contains(body, leak) {
			t.Fatalf("edit leaked %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, `"hasValue":true`) || !strings.Contains(body, `"value":null`) {
		t.Fatalf("edit must report saved values without returning them: %s", body)
	}
}

func TestSourceAddSaveTestRoutes(t *testing.T) {
	var posted, saved, tested map[string]any
	srv := managementMock(t, "{{KEY_1ce1a07b}}", &posted, &saved, &tested)
	mux := managementMux(t, srv.URL)

	add := managementCall(t, mux, "POST", "/api/sources",
		`{"key":"definition:nzbgeek","name":"Mine","fields":{"apiKey":"new-secret","categories":1}}`)
	if add.Code != 200 || !strings.Contains(add.Body.String(), `"id":42`) {
		t.Fatalf("add: %d %s", add.Code, add.Body.String())
	}
	if posted["enable"] != false || posted["name"] != "Mine" || posted["protocol"] != "usenet" {
		t.Fatalf("add request wrong: %+v", posted)
	}
	if strings.Contains(add.Body.String(), "new-secret") {
		t.Fatalf("add response leaked a secret: %s", add.Body.String())
	}

	save := managementCall(t, mux, "POST", "/api/sources/save",
		`{"id":7,"name":"New name","fields":{"apiKey":"","username":"newuser"}}`)
	if save.Code != 200 {
		t.Fatalf("save: %d %s", save.Code, save.Body.String())
	}
	if saved["name"] != "New name" || saved["capabilities"] == nil || managementFieldValue(saved, "apiKey") != "saved-secret" || managementFieldValue(saved, "nativeOnly") != "keep" {
		t.Fatalf("save did not preserve secrets/native fields: %+v", saved)
	}

	test := managementCall(t, mux, "POST", "/api/sources/test", `{"id":7,"fields":{"username":"tester"}}`)
	if test.Code != 200 {
		t.Fatalf("test: %d %s", test.Code, test.Body.String())
	}
	if tested == nil || managementFieldValue(tested, "apiKey") != "saved-secret" || managementFieldValue(tested, "username") != "tester" {
		t.Fatalf("test request wrong: %+v", tested)
	}
}

func TestSourceActionRouteRedactsUpstreamError(t *testing.T) {
	const key = "{{KEY_1ce1a07b}}"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/indexer/schema" {
			w.Write([]byte(managementSchema))
			return
		}
		http.Error(w, `[{"errorMessage":"failed http://10.0.0.5:9696?apikey=`+key+`"}]`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	mux := managementMux(t, srv.URL)
	rw := managementCall(t, mux, "POST", "/api/sources",
		`{"key":"definition:nzbgeek","name":"Mine","fields":{"apiKey":"a","categories":1}}`)
	if rw.Code != 400 {
		t.Fatalf("expected a readable refusal: %d %s", rw.Code, rw.Body.String())
	}
	for _, leak := range []string{"http://", "10.0.0.5", key, "apikey"} {
		if strings.Contains(rw.Body.String(), leak) {
			t.Fatalf("error leaked %q: %s", leak, rw.Body.String())
		}
	}
}

func TestSourceActionRoutesStayBehindTheGuard(t *testing.T) {
	srv := managementMock(t, "{{KEY_1ce1a07b}}", nil, nil, nil)
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.Prowlarr = config.App{URL: srv.URL, APIKey: "{{KEY_1ce1a07b}}"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	handler := New(store, home).Handler()
	req := httptest.NewRequest("POST", "http://127.0.0.1/api/sources", strings.NewReader(`{"name":"x"}`))
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusForbidden {
		t.Fatalf("unmarked write must be refused by the existing guard: %d", rw.Code)
	}
}
