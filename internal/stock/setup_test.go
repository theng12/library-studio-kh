package stock

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeStock is a minimal stand-in for the official test and create endpoints.
type fakeStock struct {
	clientSchema, appSchema    []map[string]any
	clients, apps, folders     []map[string]any
	categories                 map[string]string
	nextID                     int
	testErr, createErr         bool
	rootErr                    bool
	rootInaccessible           bool
	hideCreated                bool
	testedClients, madeClients []map[string]any
	testedApps, madeApps       []map[string]any
	madeCategories             []map[string]string
}

func (f *fakeStock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "POST" && r.URL.Path == "/api/v2/auth/login":
		w.Write([]byte("Ok."))
	case r.Method == "GET" && arrPath(r, "/downloadclient/schema"):
		writeFakeJSON(w, f.clientSchema)
	case r.Method == "GET" && arrPath(r, "/downloadclient"):
		writeFakeJSON(w, f.visibleClients())
	case r.Method == "POST" && arrPath(r, "/downloadclient/test"):
		body := decodeFakeBody(r)
		f.testedClients = append(f.testedClients, body)
		if f.testErr {
			fakeArrError(w, "test failed with "+fakeField(body, "password")+" at http://secret.example")
			return
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == "POST" && arrPath(r, "/downloadclient"):
		if f.createErr {
			fakeArrError(w, "create failed")
			return
		}
		body := decodeFakeBody(r)
		f.nextID++
		body["id"] = float64(f.nextID)
		f.clients = append(f.clients, body)
		f.madeClients = append(f.madeClients, body)
		writeFakeJSON(w, body)
	case r.Method == "GET" && arrPath(r, "/rootfolder"):
		if f.hideCreated && len(f.folders) > 0 {
			writeFakeJSON(w, []map[string]any{})
			return
		}
		writeFakeJSON(w, f.folders)
	case r.Method == "POST" && arrPath(r, "/rootfolder"):
		if f.rootErr {
			fakeArrError(w, "folder is not reachable")
			return
		}
		body := decodeFakeBody(r)
		folders := []map[string]any{}
		if len(f.folders) > 0 {
			folders = f.folders
		}
		f.folders = append(folders, map[string]any{
			"path": body["path"], "accessible": !f.rootInaccessible, "freeSpace": float64(1234),
		})
		writeFakeJSON(w, f.folders[len(f.folders)-1])
	case r.Method == "GET" && r.URL.Path == "/api/v1/applications/schema":
		writeFakeJSON(w, f.appSchema)
	case r.Method == "GET" && r.URL.Path == "/api/v1/applications":
		if f.hideCreated && len(f.madeApps) > 0 {
			writeFakeJSON(w, f.appsWithoutMade())
			return
		}
		writeFakeJSON(w, f.apps)
	case r.Method == "POST" && r.URL.Path == "/api/v1/applications/test":
		body := decodeFakeBody(r)
		f.testedApps = append(f.testedApps, body)
		if f.testErr {
			fakeArrError(w, "test failed with "+fakeField(body, "apiKey")+" at http://secret.example")
			return
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == "POST" && r.URL.Path == "/api/v1/applications":
		if f.createErr {
			fakeArrError(w, "create failed")
			return
		}
		body := decodeFakeBody(r)
		f.nextID++
		body["id"] = float64(f.nextID)
		f.apps = append(f.apps, body)
		f.madeApps = append(f.madeApps, body)
		writeFakeJSON(w, body)
	case r.Method == "GET" && r.URL.Path == "/api/v2/torrents/categories":
		writeFakeJSON(w, f.categoryObjects())
	case r.Method == "POST" && r.URL.Path == "/api/v2/torrents/createCategory":
		if f.createErr {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		name, path := r.FormValue("category"), r.FormValue("savePath")
		if f.categories == nil {
			f.categories = map[string]string{}
		}
		f.categories[name] = path
		f.madeCategories = append(f.madeCategories, map[string]string{"name": name, "path": path})
		writeFakeJSON(w, map[string]any{})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeStock) visibleClients() []map[string]any {
	if f.hideCreated && len(f.madeClients) > 0 {
		return []map[string]any{}
	}
	return f.clients
}

func (f *fakeStock) appsWithoutMade() []map[string]any {
	made := map[int]bool{}
	for _, a := range f.madeApps {
		made[asInt(a["id"])] = true
	}
	out := []map[string]any{}
	for _, a := range f.apps {
		if !made[asInt(a["id"])] {
			out = append(out, a)
		}
	}
	return out
}

func (f *fakeStock) categoryObjects() map[string]any {
	out := map[string]any{}
	for name, path := range f.categories {
		out[name] = map[string]any{"name": name, "savePath": path}
	}
	return out
}

func arrPath(r *http.Request, suffix string) bool {
	return r.URL.Path == "/api/v3"+suffix || r.URL.Path == "/api/v1"+suffix
}

func writeFakeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fakeArrError(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusBadRequest)
	writeFakeJSON(w, []map[string]any{{"errorMessage": message}})
}

func decodeFakeBody(r *http.Request) map[string]any {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	return body
}

func fakeField(raw map[string]any, name string) string {
	return asStr(fakeFieldValue(raw, name))
}

func fakeFieldValue(raw map[string]any, name string) any {
	_, byName := fieldMaps(raw)
	if f := byName[name]; f != nil {
		return f["value"]
	}
	return nil
}

func setupFakeServer(t *testing.T, f *fakeStock) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv
}

func qbitClientSchema() []map[string]any {
	return []map[string]any{{
		"name": "qBittorrent", "implementation": "QBittorrent", "configContract": "QBittorrentSettings",
		"fields": []any{
			map[string]any{"name": "host", "label": "Host", "type": "textbox", "value": "localhost", "required": true},
			map[string]any{"name": "port", "label": "Port", "type": "number", "value": float64(8080), "required": true},
			map[string]any{"name": "useSsl", "label": "Use SSL", "type": "checkbox", "value": false, "required": true},
			map[string]any{"name": "urlBase", "label": "URL Base", "type": "textbox", "value": "/"},
			map[string]any{"name": "username", "label": "Username", "type": "textbox", "value": "admin"},
			map[string]any{"name": "password", "label": "Password", "type": "password", "value": "schema-secret", "required": true},
			map[string]any{"name": "tvCategory", "label": "Category", "type": "select", "value": ""},
			map[string]any{"name": "recentTvPriority", "label": "Recent Priority", "type": "select", "value": float64(0)},
			map[string]any{"name": "futureField", "label": "Future", "type": "color", "value": "#fff"},
		},
	}}
}

func appSchema() []map[string]any {
	return []map[string]any{{
		"name": "Sonarr", "implementation": "Sonarr", "configContract": "SonarrSettings",
		"fields": []any{
			map[string]any{"name": "prowlarrUrl", "label": "Prowlarr Server", "type": "textbox", "value": "http://localhost:9696", "required": true},
			map[string]any{"name": "baseUrl", "label": "Sonarr Server", "type": "textbox", "value": "http://localhost:8989", "required": true},
			map[string]any{"name": "apiKey", "label": "API Key", "type": "textbox", "value": "", "required": true},
			map[string]any{"name": "syncCategories", "label": "Sync Categories", "type": "select", "value": []any{float64(5000), float64(5010)}, "required": true},
		},
	}}
}

func TestDownloadClientSchemasHidesSecretsAndKeepsDefaults(t *testing.T) {
	f := &fakeStock{clientSchema: qbitClientSchema()}
	srv := setupFakeServer(t, f)
	schemas, err := NewArr("Sonarr", srv.URL, "key").DownloadClientSchemas()
	if err != nil || len(schemas) != 1 {
		t.Fatal(err, schemas)
	}
	body, _ := json.Marshal(schemas)
	if strings.Contains(string(body), "schema-secret") {
		t.Fatalf("schema leaked a secret: %s", body)
	}
	var host, password, future *DownloadClientSchemaField
	for i := range schemas[0].Fields {
		switch schemas[0].Fields[i].Name {
		case "host":
			host = &schemas[0].Fields[i]
		case "password":
			password = &schemas[0].Fields[i]
		case "futureField":
			future = &schemas[0].Fields[i]
		}
	}
	if host == nil || password == nil || future == nil {
		t.Fatalf("missing fields: %+v", schemas[0].Fields)
	}
	if host.Value != "localhost" || !host.Supported {
		t.Fatalf("default not preserved: %+v", host)
	}
	if password.Value != nil || !password.Secret || !password.HasValue && !password.Required {
		t.Fatalf("secret not hidden: %+v", password)
	}
	if future.Supported {
		t.Fatalf("unknown type should fail closed: %+v", future)
	}
}

func TestEnsureQBitClientCreatesTestsAndReadsBack(t *testing.T) {
	f := &fakeStock{clientSchema: qbitClientSchema()}
	srv := setupFakeServer(t, f)
	a := NewArr("Sonarr", srv.URL, "arr-key")
	res, err := a.EnsureQBitClient(QBitClientInput{
		URL: "http://10.0.0.5:8080/qbt", Username: "owner", Password: "hunter2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != SetupCreated || res.Test != CheckPassed || res.Readback != CheckPassed || res.ClientID != 1 {
		t.Fatalf("result: %+v", res)
	}
	if len(f.madeClients) != 1 || len(f.testedClients) != 1 {
		t.Fatalf("writes: created=%d tested=%d", len(f.madeClients), len(f.testedClients))
	}
	created := f.madeClients[0]
	if asStr(created["name"]) != "qBittorrent" || created["enable"] != true {
		t.Fatalf("resource: %+v", created)
	}
	want := map[string]any{"host": "10.0.0.5", "port": float64(8080), "useSsl": false, "urlBase": "/qbt", "username": "owner", "password": "hunter2", "tvCategory": "tv"}
	for field, value := range want {
		if got := fmt.Sprint(fakeFieldValue(created, field)); got != fmt.Sprint(value) {
			t.Fatalf("%s = %#v, want %#v", field, got, value)
		}
	}
	if fakeFieldValue(created, "recentTvPriority") != float64(0) {
		t.Fatalf("schema default lost: %+v", created)
	}
	if fakeField(created, "futureField") != "#fff" {
		t.Fatalf("unknown native field lost: %+v", created)
	}
	body, _ := json.Marshal(res)
	if strings.Contains(string(body), "hunter2") {
		t.Fatalf("report leaked the password: %s", body)
	}

	res, err = a.EnsureQBitClient(QBitClientInput{
		URL: "http://10.0.0.5:8080/qbt", Username: "owner", Password: "hunter2",
	})
	if err != nil || res.Status != SetupReused || res.ClientID != 1 {
		t.Fatalf("second call: %+v %v", res, err)
	}
	if len(f.madeClients) != 1 {
		t.Fatalf("idempotent call created another client: %d", len(f.madeClients))
	}
}

func TestEnsureQBitClientConflictsWithoutOverwriting(t *testing.T) {
	f := &fakeStock{
		clientSchema: qbitClientSchema(),
		clients: []map[string]any{{
			"id": float64(7), "name": "Existing", "implementation": "QBittorrent", "enable": true,
			"fields": []any{
				map[string]any{"name": "host", "value": "10.0.0.5"},
				map[string]any{"name": "port", "value": float64(8080)},
				map[string]any{"name": "useSsl", "value": false},
				map[string]any{"name": "urlBase", "value": "/qbt"},
				map[string]any{"name": "username", "value": "owner"},
				map[string]any{"name": "tvCategory", "value": "movies"},
			},
		}},
	}
	srv := setupFakeServer(t, f)
	_, err := NewArr("Sonarr", srv.URL, "arr-key").EnsureQBitClient(QBitClientInput{
		URL: "http://10.0.0.5:8080/qbt", Username: "owner", Password: "hunter2",
	})
	if !errors.Is(err, ErrSetupConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if len(f.madeClients) != 0 || len(f.testedClients) != 0 {
		t.Fatal("a conflict must not test or write")
	}
	if got := setupFieldString(f.clients[0], "tvCategory"); got != "movies" {
		t.Fatalf("existing category changed: %s", got)
	}
}

func TestEnsureQBitClientRadarrAndLidarrDefaults(t *testing.T) {
	for _, tc := range []struct{ app, categoryField, want string }{
		{"Radarr", "movieCategory", "movies"},
		{"Lidarr", "musicCategory", "music"},
	} {
		t.Run(tc.app, func(t *testing.T) {
			schema := qbitClientSchema()
			for _, raw := range schema[0]["fields"].([]any) {
				field := raw.(map[string]any)
				if field["name"] == "tvCategory" {
					field["name"] = tc.categoryField
				}
			}
			f := &fakeStock{clientSchema: schema}
			srv := setupFakeServer(t, f)
			res, err := NewArr(tc.app, srv.URL, "arr-key").EnsureQBitClient(QBitClientInput{
				URL: "http://10.0.0.5:8080", Username: "owner", Password: "hunter2",
			})
			if err != nil || res.Status != SetupCreated {
				t.Fatalf("%+v %v", res, err)
			}
			if got := fakeField(f.madeClients[0], tc.categoryField); got != tc.want {
				t.Fatalf("%s = %q, want %q", tc.categoryField, got, tc.want)
			}
		})
	}
}

func TestEnsureQBitClientNameConflict(t *testing.T) {
	f := &fakeStock{
		clientSchema: qbitClientSchema(),
		clients: []map[string]any{{
			"id": float64(8), "name": "qBittorrent", "implementation": "QBittorrent", "enable": true,
			"fields": []any{
				map[string]any{"name": "host", "value": "other"},
				map[string]any{"name": "port", "value": float64(9999)},
				map[string]any{"name": "useSsl", "value": false},
				map[string]any{"name": "tvCategory", "value": "tv"},
			},
		}},
	}
	srv := setupFakeServer(t, f)
	_, err := NewArr("Sonarr", srv.URL, "arr-key").EnsureQBitClient(QBitClientInput{
		URL: "http://10.0.0.5:8080", Username: "owner", Password: "hunter2",
	})
	if !errors.Is(err, ErrSetupConflict) || len(f.madeClients) != 0 {
		t.Fatalf("want name conflict, got %v", err)
	}
}

func TestEnsureQBitClientDisabledIsBlocked(t *testing.T) {
	f := &fakeStock{
		clientSchema: qbitClientSchema(),
		clients: []map[string]any{{
			"id": float64(7), "name": "Existing", "implementation": "QBittorrent", "enable": false,
			"fields": []any{
				map[string]any{"name": "host", "value": "10.0.0.5"},
				map[string]any{"name": "port", "value": float64(8080)},
				map[string]any{"name": "useSsl", "value": false},
				map[string]any{"name": "urlBase", "value": "/"},
				map[string]any{"name": "username", "value": "owner"},
				map[string]any{"name": "tvCategory", "value": "tv"},
			},
		}},
	}
	srv := setupFakeServer(t, f)
	res, err := NewArr("Sonarr", srv.URL, "arr-key").EnsureQBitClient(QBitClientInput{
		URL: "http://10.0.0.5:8080", Username: "owner", Password: "hunter2",
	})
	if err == nil || res.Status != SetupBlocked || len(f.testedClients) != 0 {
		t.Fatalf("want blocked without changes: %+v %v", res, err)
	}
}

func TestEnsureQBitClientNativeTestFailureDoesNotCreate(t *testing.T) {
	f := &fakeStock{clientSchema: qbitClientSchema(), testErr: true}
	srv := setupFakeServer(t, f)
	res, err := NewArr("Sonarr", srv.URL, "arr-key").EnsureQBitClient(QBitClientInput{
		URL: "http://10.0.0.5:8080", Username: "owner", Password: "hunter2",
	})
	if err == nil || res.Status != SetupBlocked || res.Test != CheckFailed {
		t.Fatalf("result: %+v %v", res, err)
	}
	if len(f.madeClients) != 0 {
		t.Fatal("a failed native test must not create")
	}
	if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "secret.example") {
		t.Fatalf("error leaked a secret or URL: %v", err)
	}
}

func TestEnsureQBitClientMissingRequiredFailsClosed(t *testing.T) {
	schema := qbitClientSchema()
	schema[0]["fields"] = append(schema[0]["fields"].([]any),
		map[string]any{"name": "futureRequired", "label": "Future Required", "type": "color", "required": true})
	f := &fakeStock{clientSchema: schema}
	srv := setupFakeServer(t, f)
	res, err := NewArr("Sonarr", srv.URL, "arr-key").EnsureQBitClient(QBitClientInput{
		URL: "http://10.0.0.5:8080", Username: "owner", Password: "hunter2",
	})
	if err == nil || res.Status != SetupBlocked || len(f.madeClients) != 0 {
		t.Fatalf("want fail closed: %+v %v", res, err)
	}
}

func TestEnsureQBitClientReadbackFailureIsHonest(t *testing.T) {
	f := &fakeStock{clientSchema: qbitClientSchema(), hideCreated: true}
	srv := setupFakeServer(t, f)
	res, err := NewArr("Sonarr", srv.URL, "arr-key").EnsureQBitClient(QBitClientInput{
		URL: "http://10.0.0.5:8080", Username: "owner", Password: "hunter2",
	})
	if err == nil || res.Status != SetupCreated || res.Readback != CheckFailed {
		t.Fatalf("result: %+v %v", res, err)
	}
	if len(f.madeClients) != 1 {
		t.Fatal("the partial write should stay visible, not roll back")
	}
}

func TestEnsureRootFolderReusesAddsAndVerifies(t *testing.T) {
	f := &fakeStock{
		clientSchema: qbitClientSchema(),
		folders:      []map[string]any{{"path": "/nas/tv", "accessible": true, "freeSpace": float64(10)}},
	}
	srv := setupFakeServer(t, f)
	a := NewArr("Sonarr", srv.URL, "arr-key")
	res, err := a.EnsureRootFolder(RootFolderInput{Path: "/nas/tv/"})
	if err != nil || res.Status != SetupReused || res.Test != CheckPassed || res.Readback != CheckPassed {
		t.Fatalf("reuse: %+v %v", res, err)
	}
	if len(f.folders) != 1 {
		t.Fatal("an existing folder was written again")
	}
	res, err = a.EnsureRootFolder(RootFolderInput{Path: "/nas/movies"})
	if err != nil || res.Status != SetupCreated || res.Readback != CheckPassed {
		t.Fatalf("create: %+v %v", res, err)
	}
	if len(f.folders) != 2 {
		t.Fatalf("folders: %+v", f.folders)
	}
	res, err = a.EnsureRootFolder(RootFolderInput{Path: "/nas/movies/"})
	if err != nil || res.Status != SetupReused || len(f.folders) != 2 {
		t.Fatalf("second call: %+v %v", res, err)
	}
}

func TestEnsureRootFolderInaccessibleIsBlocked(t *testing.T) {
	f := &fakeStock{folders: []map[string]any{{"path": "/nas/tv", "accessible": false}}}
	srv := setupFakeServer(t, f)
	res, err := NewArr("Sonarr", srv.URL, "arr-key").EnsureRootFolder(RootFolderInput{Path: "/nas/tv"})
	if err == nil || res.Status != SetupBlocked || len(f.folders) != 1 {
		t.Fatalf("want blocked: %+v %v", res, err)
	}

	f = &fakeStock{rootInaccessible: true}
	srv = setupFakeServer(t, f)
	res, err = NewArr("Sonarr", srv.URL, "arr-key").EnsureRootFolder(RootFolderInput{Path: "/nas/new"})
	if err == nil || res.Status != SetupCreated || res.Readback != CheckFailed {
		t.Fatalf("created but inaccessible: %+v %v", res, err)
	}
}

func TestEnsureApplicationCreatesTestsAndReuses(t *testing.T) {
	f := &fakeStock{appSchema: appSchema()}
	srv := setupFakeServer(t, f)
	a := NewArr("Prowlarr", srv.URL, "prowlarr-key")
	in := ApplicationInput{Target: "sonarr", BaseURL: "http://10.0.0.9:8989/", APIKey: "key-123", ProwlarrURL: "http://10.0.0.2:9696"}
	res, err := a.EnsureProwlarrApplication(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != SetupCreated || res.Test != CheckPassed || res.Readback != CheckPassed || res.App != "Sonarr" {
		t.Fatalf("result: %+v", res)
	}
	if len(f.madeApps) != 1 || len(f.testedApps) != 1 {
		t.Fatalf("writes: %d %d", len(f.madeApps), len(f.testedApps))
	}
	created := f.madeApps[0]
	if asStr(created["name"]) != "Sonarr" || created["enable"] != true {
		t.Fatalf("resource: %+v", created)
	}
	if fakeField(created, "baseUrl") != "http://10.0.0.9:8989" || fakeField(created, "apiKey") != "key-123" || fakeField(created, "prowlarrUrl") != "http://10.0.0.2:9696" {
		t.Fatalf("fields: %+v", created)
	}
	if fakeFieldValue(created, "syncCategories") == nil {
		t.Fatalf("schema default lost: %+v", created)
	}
	body, _ := json.Marshal(res)
	if strings.Contains(string(body), "key-123") {
		t.Fatalf("report leaked the API key: %s", body)
	}

	res, err = a.EnsureProwlarrApplication(in)
	if err != nil || res.Status != SetupReused || res.ApplicationID != 1 || len(f.madeApps) != 1 {
		t.Fatalf("second call: %+v %v", res, err)
	}
}

func TestEnsureApplicationConflictAndDisabled(t *testing.T) {
	existing := map[string]any{
		"id": float64(4), "name": "Sonarr", "implementation": "Sonarr", "enable": true,
		"fields": []any{
			map[string]any{"name": "baseUrl", "value": "http://old:8989"},
			map[string]any{"name": "apiKey", "value": "saved"},
		},
	}
	f := &fakeStock{appSchema: appSchema(), apps: []map[string]any{existing}}
	srv := setupFakeServer(t, f)
	_, err := NewArr("Prowlarr", srv.URL, "prowlarr-key").EnsureProwlarrApplication(ApplicationInput{
		Target: "Sonarr", BaseURL: "http://new:8989", APIKey: "key-123", ProwlarrURL: "http://10.0.0.2:9696",
	})
	if !errors.Is(err, ErrSetupConflict) || len(f.madeApps) != 0 || len(f.testedApps) != 0 {
		t.Fatalf("want name conflict without writes: %v", err)
	}

	existing["enable"] = false
	existing["fields"] = []any{
		map[string]any{"name": "baseUrl", "value": "http://new:8989"},
		map[string]any{"name": "apiKey", "value": "saved"},
	}
	res, err := NewArr("Prowlarr", srv.URL, "prowlarr-key").EnsureProwlarrApplication(ApplicationInput{
		Target: "Sonarr", BaseURL: "http://new:8989", APIKey: "key-123", ProwlarrURL: "http://10.0.0.2:9696",
	})
	if err == nil || res.Status != SetupBlocked || len(f.madeApps) != 0 {
		t.Fatalf("want disabled blocked: %+v %v", res, err)
	}
}

func TestEnsureApplicationNativeTestFailureAndPartialReadback(t *testing.T) {
	f := &fakeStock{appSchema: appSchema(), testErr: true}
	srv := setupFakeServer(t, f)
	res, err := NewArr("Prowlarr", srv.URL, "prowlarr-key").EnsureProwlarrApplication(ApplicationInput{
		Target: "Sonarr", BaseURL: "http://10.0.0.9:8989", APIKey: "key-123", ProwlarrURL: "http://10.0.0.2:9696",
	})
	if err == nil || res.Status != SetupBlocked || res.Test != CheckFailed || len(f.madeApps) != 0 {
		t.Fatalf("test failure created a link: %+v %v", res, err)
	}
	if strings.Contains(err.Error(), "key-123") || strings.Contains(err.Error(), "secret.example") {
		t.Fatalf("error leaked a secret or URL: %v", err)
	}

	f = &fakeStock{appSchema: appSchema(), hideCreated: true}
	srv = setupFakeServer(t, f)
	res, err = NewArr("Prowlarr", srv.URL, "prowlarr-key").EnsureProwlarrApplication(ApplicationInput{
		Target: "Sonarr", BaseURL: "http://10.0.0.9:8989", APIKey: "key-123", ProwlarrURL: "http://10.0.0.2:9696",
	})
	if err == nil || res.Status != SetupCreated || res.Readback != CheckFailed || len(f.madeApps) != 1 {
		t.Fatalf("partial write: %+v %v", res, err)
	}
}

func TestEnsureCategoryCreatesReusesAndConflicts(t *testing.T) {
	f := &fakeStock{}
	srv := setupFakeServer(t, f)
	q := NewQBit(srv.URL, "owner", "hunter2")
	res, err := q.EnsureCategory(CategoryInput{Name: "tv", SavePath: "/downloads/tv"})
	if err != nil || res.Status != SetupCreated || res.Readback != CheckPassed || res.Test != CheckNotRun {
		t.Fatalf("create: %+v %v", res, err)
	}
	if len(f.madeCategories) != 1 || f.categories["tv"] != "/downloads/tv" {
		t.Fatalf("categories: %+v", f.categories)
	}
	res, err = q.EnsureCategory(CategoryInput{Name: "tv", SavePath: "/downloads/tv/"})
	if err != nil || res.Status != SetupReused || len(f.madeCategories) != 1 {
		t.Fatalf("reuse: %+v %v", res, err)
	}
	res, err = q.EnsureCategory(CategoryInput{Name: "tv", SavePath: "/elsewhere"})
	if !errors.Is(err, ErrSetupConflict) || res.Status != SetupConflict || len(f.madeCategories) != 1 {
		t.Fatalf("conflict: %+v %v", res, err)
	}
	if f.categories["tv"] != "/downloads/tv" {
		t.Fatalf("category was overwritten: %+v", f.categories)
	}
}

func TestEnsureCategoryReadbackFailureIsHonest(t *testing.T) {
	f := &fakeStock{hideCreated: true}
	srv := setupFakeServer(t, f)
	// hideCreated only affects Arr lists; make the category vanish after the write.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/torrents/createCategory" {
			f.categories = map[string]string{}
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/v2/auth/login" {
			w.Write([]byte("Ok."))
			return
		}
		if r.URL.Path == "/api/v2/torrents/categories" {
			writeFakeJSON(w, map[string]any{})
			return
		}
		http.NotFound(w, r)
	})
	q := NewQBit(srv.URL, "owner", "hunter2")
	res, err := q.EnsureCategory(CategoryInput{Name: "tv", SavePath: "/downloads/tv"})
	if err == nil || res.Status != SetupCreated || res.Readback != CheckFailed {
		t.Fatalf("partial write: %+v %v", res, err)
	}
}
