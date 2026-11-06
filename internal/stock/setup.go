package stock

// Fresh-machine setup for the stock apps. These helpers only run when main calls
// them: they never create anything on their own, never edit an existing resource,
// and never enable a disabled one. Every write is followed by a native readback.

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// SetupStatus says what happened to the resource this call ensured.
type SetupStatus string

const (
	SetupCreated  SetupStatus = "created"
	SetupReused   SetupStatus = "reused"
	SetupBlocked  SetupStatus = "blocked"
	SetupConflict SetupStatus = "conflict"
)

// SetupCheck is the outcome of a native test or of the readback after a write.
// not_run is honest: the app has no such check, or the write never happened.
type SetupCheck string

const (
	CheckPassed SetupCheck = "passed"
	CheckFailed SetupCheck = "failed"
	CheckNotRun SetupCheck = "not_run"
)

// ErrSetupConflict marks a plan the caller must show before anything changes.
var ErrSetupConflict = errors.New("native setup conflicts with existing values")

// DownloadClientSchema is one live /downloadclient/schema entry. Secret values
// are never included; non-secret defaults are, so the caller can show them.
type DownloadClientSchema struct {
	Name           string                      `json:"name"`
	Implementation string                      `json:"implementation"`
	ConfigContract string                      `json:"configContract"`
	Fields         []DownloadClientSchemaField `json:"fields"`
}

type DownloadClientSchemaField struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Type      string `json:"type"`
	Required  bool   `json:"required"`
	Advanced  bool   `json:"advanced"`
	Secret    bool   `json:"secret"`
	Supported bool   `json:"supported"`
	Blocked   string `json:"blocked,omitempty"`
	Value     any    `json:"value"`
	HasValue  bool   `json:"hasValue"`
}

// DownloadClientSchemas lists the download client types the app offers right now.
func (a *Arr) DownloadClientSchemas() ([]DownloadClientSchema, error) {
	raw, err := a.setupSchemas("/downloadclient/schema")
	if err != nil {
		return nil, a.SafeError(err)
	}
	out := make([]DownloadClientSchema, 0, len(raw))
	for _, s := range raw {
		item := DownloadClientSchema{
			Name:           a.sanitizeText(asStr(s["name"])),
			Implementation: a.sanitizeText(asStr(s["implementation"])),
			ConfigContract: a.sanitizeText(asStr(s["configContract"])),
			Fields:         []DownloadClientSchemaField{},
		}
		for _, def := range fieldDefs(s) {
			_, supported, why := publicFieldType(def)
			secret := secretField(def)
			f := DownloadClientSchemaField{
				Name: a.sanitizeText(asStr(def["name"])), Label: a.fieldLabel(def), Type: a.sanitizeText(asStr(def["type"])),
				Required: asBool(def["required"]), Advanced: asBool(def["advanced"]),
				Secret: secret, Supported: supported, Blocked: a.sanitizeText(why),
			}
			v := def["value"]
			f.HasValue = !valueBlank(v)
			if supported && !secret {
				f.Value = v
			}
			item.Fields = append(item.Fields, f)
		}
		out = append(out, item)
	}
	return out, nil
}

// QBitClientInput is an explicit qBittorrent client request. URL is the real
// address including its port; Name and Category are caller-editable names.
type QBitClientInput struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"-"`
	Name     string `json:"name,omitempty"`     // empty = "qBittorrent"
	Category string `json:"category,omitempty"` // empty = tv / movies / music by app
	Enable   *bool  `json:"enable,omitempty"`   // new clients only; default on
}

// ClientSetup reports the plan and its native checks. Secrets never appear here.
type ClientSetup struct {
	App              string      `json:"app"`
	Status           SetupStatus `json:"status"`
	ClientID         int         `json:"clientId,omitempty"`
	Name             string      `json:"name,omitempty"`
	Enabled          *bool       `json:"enabled,omitempty"`
	Category         string      `json:"category,omitempty"`
	ExistingName     string      `json:"existingName,omitempty"`
	ExistingCategory string      `json:"existingCategory,omitempty"`
	Test             SetupCheck  `json:"test"`
	TestError        string      `json:"testError,omitempty"`
	SavedSecretCheck SetupCheck  `json:"savedSecretCheck"`
	Readback         SetupCheck  `json:"readback"`
	ReadbackError    string      `json:"readbackError,omitempty"`
	Conflict         []string    `json:"conflict,omitempty"`
	Blocked          string      `json:"blocked,omitempty"`
	Notes            []string    `json:"notes,omitempty"`
}

type qbitEndpoint struct {
	host    string
	port    int
	useSSL  bool
	urlBase string
}

// EnsureQBitClient reuses an identical qBittorrent client or creates one from
// the app's live schema, tests it natively, then reads it back. A different
// existing value is returned as a conflict, never overwritten.
func (a *Arr) EnsureQBitClient(in QBitClientInput) (ClientSetup, error) {
	res := ClientSetup{App: a.Name, Test: CheckNotRun, SavedSecretCheck: CheckNotRun, Readback: CheckNotRun}
	ep, err := parseQBitEndpoint(in.URL)
	if err != nil {
		return res, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "qBittorrent"
	}
	if err := validateSetupName(name); err != nil {
		return res, err
	}
	category := strings.TrimSpace(in.Category)
	if category == "" {
		category = defaultCategory(a.Name)
	}
	if category != "" {
		if err := validateSetupText("category", category, 120); err != nil {
			return res, err
		}
	}
	res.Category = category

	schemas, err := a.setupSchemas("/downloadclient/schema")
	if err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err)
		return res, a.SafeError(err)
	}
	schema := findSetupImplementation(schemas, "QBittorrent")
	if schema == nil {
		res.Status = SetupBlocked
		res.Blocked = "this app's download client schema has no qBittorrent entry"
		return res, errors.New(res.Blocked)
	}
	desired, err := a.buildQBitClient(schema, name, ep, in.Username, in.Password, category, in.Enable)
	if err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err)
		return res, errors.New(res.Blocked)
	}

	clients, err := a.setupDownloadClients()
	if err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err)
		return res, a.SafeError(err)
	}
	match, conflict, existingName, existingCategory := matchQBitClient(clients, schema, ep, in.Username, name, category)
	if len(conflict) > 0 {
		res.Status, res.Conflict = SetupConflict, conflict
		res.ExistingName, res.ExistingCategory = existingName, existingCategory
		return res, fmt.Errorf("%w: %s already has a %s client that differs in %s",
			ErrSetupConflict, a.Name, "qBittorrent", strings.Join(conflict, ", "))
	}
	if match != nil {
		res.Status = SetupReused
		res.ClientID, res.Name = asInt(match["id"]), a.sanitizeText(asStr(match["name"]))
		res.ExistingName, res.ExistingCategory = res.Name, existingCategory
		if enabled, known := setupEnabled(match); known {
			res.Enabled = &enabled
			if !enabled {
				res.Status = SetupBlocked
				res.Blocked = "the matching qBittorrent client is disabled in " + a.Name + "; it was not enabled silently"
				return res, errors.New(res.Blocked)
			}
		}
		if saved, exposed := setupFieldStringValue(match, "password"); exposed && saved != "" && !secretLooksHidden(saved) {
			if err := a.do("POST", "/downloadclient/test", cloneResource(match), nil); err != nil {
				res.Test, res.TestError = CheckFailed, a.setupErrorText(err, saved)
				res.SavedSecretCheck = CheckFailed
				return res, fmt.Errorf("%s could not reach the existing qBittorrent client with its saved password: %s", a.Name, res.TestError)
			}
			res.Test, res.SavedSecretCheck, res.Readback = CheckPassed, CheckPassed, CheckPassed
			res.Notes = append(res.Notes, "The app returned the saved password; the native test used the existing client unchanged.")
			res.Notes = append(res.Notes, "The existing client was reused; its name and settings were left unchanged.")
			return res, nil
		}
		res.Test, res.SavedSecretCheck = CheckNotRun, CheckNotRun
		res.Readback = CheckPassed
		res.Notes = append(res.Notes, "The existing client was reused; its name and settings were left unchanged.")
		res.Notes = append(res.Notes, "The saved password is not returned by the app, so it was not tested. Re-enter it in "+a.Name+" if the connection is not working.")
		return res, nil
	}

	if err := a.do("POST", "/downloadclient/test", desired, nil); err != nil {
		res.Status = SetupBlocked
		res.Blocked = "the native qBittorrent client test failed; nothing was created"
		res.Test, res.TestError = CheckFailed, a.setupErrorText(err, in.Password)
		return res, fmt.Errorf("%s: %s", a.Name, res.TestError)
	}
	res.Test = CheckPassed

	var created map[string]any
	if err := a.do("POST", "/downloadclient", desired, &created); err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err, in.Password)
		return res, errors.New(res.Blocked)
	}
	res.Status, res.ClientID, res.Name = SetupCreated, asInt(created["id"]), name
	if v, ok := desired["enable"].(bool); ok {
		res.Enabled = &v
	}

	secretCheck, err := a.verifyQBitClientReadback(res.ClientID, desired, schema, ep, in.Username, category, in.Password)
	if err != nil {
		res.Readback, res.ReadbackError = CheckFailed, a.setupErrorText(err, in.Password)
		res.SavedSecretCheck = secretCheck
		return res, fmt.Errorf("%s created the qBittorrent client but could not verify it: %s", a.Name, res.ReadbackError)
	}
	res.Readback, res.SavedSecretCheck = CheckPassed, secretCheck
	if secretCheck == CheckPassed {
		res.Notes = append(res.Notes, "The app returned the saved password and it matches the supplied value.")
	} else {
		res.Notes = append(res.Notes, "The app did not return the saved password; the pre-create native test passed with the supplied value, but the stored value was not read back.")
	}
	return res, nil
}

// RootFolderInput is an explicit folder the caller wants the app to index.
type RootFolderInput struct {
	Path              string `json:"path"`
	QualityProfileID  int    `json:"qualityProfileId,omitempty"`
	MetadataProfileID int    `json:"metadataProfileId,omitempty"`
}

type RootFolderSetup struct {
	App           string      `json:"app"`
	Path          string      `json:"path"`
	Status        SetupStatus `json:"status"`
	Accessible    *bool       `json:"accessible,omitempty"`
	FreeSpace     *int64      `json:"freeSpace,omitempty"`
	Test          SetupCheck  `json:"test"`
	Readback      SetupCheck  `json:"readback"`
	ReadbackError string      `json:"readbackError,omitempty"`
	Blocked       string      `json:"blocked,omitempty"`
	Notes         []string    `json:"notes,omitempty"`
}

// EnsureRootFolder reuses the exact folder or adds it, then reads it back and
// reports the app's own accessibility answer. It never edits an existing folder.
func (a *Arr) EnsureRootFolder(in RootFolderInput) (RootFolderSetup, error) {
	res := RootFolderSetup{App: a.Name, Path: strings.TrimSpace(in.Path), Test: CheckNotRun, Readback: CheckNotRun}
	if err := validateSetupText("root folder path", res.Path, 4096); err != nil {
		return res, err
	}
	folders, err := a.setupRootFolders()
	if err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err)
		return res, a.SafeError(err)
	}
	if found := findRootFolder(folders, res.Path); found != nil {
		res.Status = SetupReused
		res.Accessible, res.FreeSpace = setupAccessible(found)
		if res.Accessible != nil && !*res.Accessible {
			res.Status = SetupBlocked
			res.Test = CheckFailed
			res.Blocked = "the folder exists in " + a.Name + " but " + a.Name + " says it is not accessible; nothing was changed"
			return res, errors.New(res.Blocked)
		}
		res.Test, res.Readback = setupAccessCheck(res.Accessible), CheckPassed
		return res, nil
	}

	payload := map[string]any{"path": res.Path}
	if a.Name == "Lidarr" {
		options, err := a.Options()
		if err != nil {
			return res, a.SafeError(err)
		}
		if !hasID(options.QualityProfiles, in.QualityProfileID) || !hasID(options.MetadataProfiles, in.MetadataProfileID) {
			return res, errors.New("choose the quality and metadata profiles Lidarr currently lists before creating its root folder")
		}
		payload["name"] = filepath.Base(filepath.Clean(res.Path))
		payload["defaultQualityProfileId"] = in.QualityProfileID
		payload["defaultMetadataProfileId"] = in.MetadataProfileID
	}
	if err := a.do("POST", "/rootfolder", payload, nil); err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err)
		return res, a.SafeError(err)
	}
	res.Status = SetupCreated

	folders, err = a.setupRootFolders()
	if err != nil {
		res.Readback, res.ReadbackError = CheckFailed, a.setupErrorText(err)
		return res, fmt.Errorf("%s added the root folder but could not read it back: %s", a.Name, res.ReadbackError)
	}
	found := findRootFolder(folders, res.Path)
	if found == nil {
		res.Readback, res.ReadbackError = CheckFailed, "the new folder is missing from "+a.Name+"'s root folder list"
		return res, errors.New(res.ReadbackError)
	}
	if a.Name == "Lidarr" && (asInt(found["defaultQualityProfileId"]) != in.QualityProfileID || asInt(found["defaultMetadataProfileId"]) != in.MetadataProfileID) {
		res.Readback, res.ReadbackError = CheckFailed, "Lidarr did not read back the selected root-folder profiles"
		return res, errors.New(res.ReadbackError)
	}
	res.Accessible, res.FreeSpace = setupAccessible(found)
	res.Test = setupAccessCheck(res.Accessible)
	if res.Accessible != nil && !*res.Accessible {
		res.Readback, res.ReadbackError = CheckFailed, a.Name+" reports the new folder is not accessible"
		return res, errors.New(res.ReadbackError)
	}
	res.Readback = CheckPassed
	return res, nil
}

// ApplicationInput links Prowlarr to one library app. Every address and key is explicit.
type ApplicationInput struct {
	Target      string `json:"target"`         // Sonarr, Radarr or Lidarr
	Name        string `json:"name,omitempty"` // empty = Target
	BaseURL     string `json:"baseUrl"`
	APIKey      string `json:"-"`
	ProwlarrURL string `json:"prowlarrUrl"`
	Enable      *bool  `json:"enable,omitempty"` // new links only; default on
}

type ApplicationSetup struct {
	App                 string      `json:"app"`
	Status              SetupStatus `json:"status"`
	ApplicationID       int         `json:"applicationId,omitempty"`
	Name                string      `json:"name,omitempty"`
	Enabled             *bool       `json:"enabled,omitempty"`
	BaseURL             string      `json:"baseUrl,omitempty"`
	ProwlarrURL         string      `json:"prowlarrUrl,omitempty"`
	ExistingName        string      `json:"existingName,omitempty"`
	ExistingBaseURL     string      `json:"existingBaseUrl,omitempty"`
	ExistingProwlarrURL string      `json:"existingProwlarrUrl,omitempty"`
	Test                SetupCheck  `json:"test"`
	TestError           string      `json:"testError,omitempty"`
	SavedSecretCheck    SetupCheck  `json:"savedSecretCheck"`
	Readback            SetupCheck  `json:"readback"`
	ReadbackError       string      `json:"readbackError,omitempty"`
	Conflict            []string    `json:"conflict,omitempty"`
	Blocked             string      `json:"blocked,omitempty"`
	Notes               []string    `json:"notes,omitempty"`
}

// EnsureProwlarrApplication reuses the identical link or creates it from the
// live /applications/schema entry, tests it natively, then reads it back.
func (a *Arr) EnsureProwlarrApplication(in ApplicationInput) (ApplicationSetup, error) {
	res := ApplicationSetup{App: canonicalArrName(in.Target), BaseURL: normalizeURLBase(in.BaseURL), Test: CheckNotRun, SavedSecretCheck: CheckNotRun, Readback: CheckNotRun}
	if res.App == "" {
		return res, errors.New("choose Sonarr, Radarr or Lidarr")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = res.App
	}
	if err := validateSetupName(name); err != nil {
		return res, err
	}
	base, err := validateSetupURL("the library app address", in.BaseURL)
	if err != nil {
		return res, err
	}
	prowlarrURL, err := validateSetupURL("Prowlarr's address", in.ProwlarrURL)
	if err != nil {
		return res, err
	}
	res.ProwlarrURL = prowlarrURL
	if strings.TrimSpace(in.APIKey) == "" {
		return res, errors.New("an API key is required for the library app")
	}

	schemas, err := a.setupSchemas("/applications/schema")
	if err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err)
		return res, a.SafeError(err)
	}
	schema := findSetupImplementation(schemas, res.App)
	if schema == nil {
		res.Status = SetupBlocked
		res.Blocked = "Prowlarr's application schema has no " + res.App + " entry"
		return res, errors.New(res.Blocked)
	}
	desired, err := a.buildApplication(schema, name, base, in.APIKey, prowlarrURL, in.Enable)
	if err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err, in.APIKey)
		return res, errors.New(res.Blocked)
	}

	apps, err := a.setupApplications()
	if err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err)
		return res, a.SafeError(err)
	}
	match, conflict, existingName, existingBase, existingProwlarr := matchApplication(apps, res.App, base, prowlarrURL, name)
	if len(conflict) > 0 {
		res.Status, res.Conflict = SetupConflict, conflict
		res.ExistingName, res.ExistingBaseURL, res.ExistingProwlarrURL = existingName, existingBase, existingProwlarr
		return res, fmt.Errorf("%w: Prowlarr already links %s differently in %s", ErrSetupConflict, res.App, strings.Join(conflict, ", "))
	}
	if match != nil {
		res.Status = SetupReused
		res.ApplicationID, res.Name = asInt(match["id"]), a.sanitizeText(asStr(match["name"]))
		res.ExistingName, res.ExistingBaseURL = res.Name, existingBase
		res.ExistingProwlarrURL = existingProwlarr
		if enabled, known := setupEnabled(match); known {
			res.Enabled = &enabled
			if !enabled {
				res.Status = SetupBlocked
				res.Blocked = "the matching Prowlarr link to " + res.App + " is disabled; it was not enabled silently"
				return res, errors.New(res.Blocked)
			}
		}
		if saved, exposed := setupFieldStringValue(match, "apiKey"); exposed && saved != "" && !secretLooksHidden(saved) {
			if err := a.do("POST", "/applications/test", cloneResource(match), nil); err != nil {
				res.Test, res.TestError = CheckFailed, a.setupErrorText(err, saved)
				res.SavedSecretCheck = CheckFailed
				return res, fmt.Errorf("Prowlarr could not reach the existing %s link with its saved API key: %s", res.App, res.TestError)
			}
			res.Test, res.SavedSecretCheck, res.Readback = CheckPassed, CheckPassed, CheckPassed
			res.Notes = append(res.Notes, "Prowlarr returned the saved API key; the native test used the existing link unchanged.")
			res.Notes = append(res.Notes, "The existing link was reused; its name and settings were left unchanged.")
			return res, nil
		}
		res.Test, res.SavedSecretCheck = CheckNotRun, CheckNotRun
		res.Readback = CheckPassed
		res.Notes = append(res.Notes, "The existing link was reused; its name and settings were left unchanged.")
		res.Notes = append(res.Notes, "The saved API key is not returned by Prowlarr, so it was not tested. Re-enter it in Prowlarr if the connection is not working.")
		return res, nil
	}

	if err := a.do("POST", "/applications/test", desired, nil); err != nil {
		res.Status = SetupBlocked
		res.Blocked = "Prowlarr's native application test failed; nothing was created"
		res.Test, res.TestError = CheckFailed, a.setupErrorText(err, in.APIKey)
		return res, fmt.Errorf("Prowlarr: %s", res.TestError)
	}
	res.Test = CheckPassed

	var created map[string]any
	if err := a.do("POST", "/applications", desired, &created); err != nil {
		res.Status, res.Blocked = SetupBlocked, a.setupErrorText(err, in.APIKey)
		return res, errors.New(res.Blocked)
	}
	res.Status, res.ApplicationID, res.Name = SetupCreated, asInt(created["id"]), name
	if v, ok := desired["enable"].(bool); ok {
		res.Enabled = &v
	}

	secretCheck, err := a.verifyApplicationReadback(res.ApplicationID, desired, res.App, base, prowlarrURL, in.APIKey)
	if err != nil {
		res.Readback, res.ReadbackError = CheckFailed, a.setupErrorText(err, in.APIKey)
		res.SavedSecretCheck = secretCheck
		return res, fmt.Errorf("Prowlarr created the %s link but could not verify it: %s", res.App, res.ReadbackError)
	}
	res.Readback, res.SavedSecretCheck = CheckPassed, secretCheck
	if secretCheck == CheckPassed {
		res.Notes = append(res.Notes, "Prowlarr returned the saved API key and it matches the supplied value.")
	} else {
		res.Notes = append(res.Notes, "Prowlarr did not return the saved API key; the pre-create native test passed with the supplied value, but the stored value was not read back.")
	}
	return res, nil
}

// CategoryInput is one qBittorrent category and its explicit local save path.
type CategoryInput struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath"`
}

type CategorySetup struct {
	Name             string      `json:"name"`
	SavePath         string      `json:"savePath"`
	Status           SetupStatus `json:"status"`
	ExistingSavePath string      `json:"existingSavePath,omitempty"`
	Test             SetupCheck  `json:"test"`
	Readback         SetupCheck  `json:"readback"`
	ReadbackError    string      `json:"readbackError,omitempty"`
	Conflict         []string    `json:"conflict,omitempty"`
	Blocked          string      `json:"blocked,omitempty"`
	Notes            []string    `json:"notes,omitempty"`
}

// EnsureCategory reuses the category when its save path matches, reports a
// conflict when it differs, and otherwise creates it and reads it back.
func (q *QBit) EnsureCategory(in CategoryInput) (CategorySetup, error) {
	res := CategorySetup{Name: strings.TrimSpace(in.Name), SavePath: strings.TrimSpace(in.SavePath), Test: CheckNotRun, Readback: CheckNotRun}
	if err := validateSetupText("category name", res.Name, 200); err != nil {
		return res, err
	}
	if err := validateSetupText("save path", res.SavePath, 4096); err != nil {
		return res, err
	}
	categories, err := q.categories()
	if err != nil {
		res.Status, res.Blocked = SetupBlocked, qbitErrorText(err, res.Name)
		return res, err
	}
	if existing, ok := categories[res.Name]; ok {
		res.ExistingSavePath = existing
		if normalizedSetupPath(existing) != normalizedSetupPath(res.SavePath) {
			res.Status, res.Conflict = SetupConflict, []string{"savePath"}
			return res, fmt.Errorf("%w: qBittorrent category %q already saves to a different folder", ErrSetupConflict, res.Name)
		}
		res.Status, res.Readback = SetupReused, CheckPassed
		res.Notes = append(res.Notes, "qBittorrent has no category test endpoint; the category list is the readback.")
		return res, nil
	}

	err = q.call("/api/v2/torrents/createCategory", url.Values{"category": {res.Name}, "savePath": {res.SavePath}}, nil)
	if err != nil {
		res.Status, res.Blocked = SetupBlocked, qbitErrorText(err)
		return res, err
	}
	res.Status = SetupCreated

	categories, err = q.categories()
	if err != nil {
		res.Readback, res.ReadbackError = CheckFailed, qbitErrorText(err)
		return res, fmt.Errorf("qBittorrent created the category but could not read it back: %s", res.ReadbackError)
	}
	actual, ok := categories[res.Name]
	if !ok {
		res.Readback, res.ReadbackError = CheckFailed, "the new category is missing from qBittorrent's list"
		return res, errors.New(res.ReadbackError)
	}
	res.ExistingSavePath = actual
	if normalizedSetupPath(actual) != normalizedSetupPath(res.SavePath) {
		res.Readback, res.ReadbackError = CheckFailed, "qBittorrent reported a different save path for the new category"
		return res, errors.New(res.ReadbackError)
	}
	res.Readback = CheckPassed
	res.Notes = append(res.Notes, "qBittorrent has no category test endpoint; the category list is the readback.")
	return res, nil
}

// ---- shared helpers ----

func (a *Arr) setupSchemas(path string) ([]map[string]any, error) {
	var raw []map[string]any
	if err := a.do("GET", path, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (a *Arr) setupDownloadClients() ([]map[string]any, error) {
	var raw []map[string]any
	if err := a.do("GET", "/downloadclient", nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (a *Arr) setupApplications() ([]map[string]any, error) {
	var raw []map[string]any
	if err := a.do("GET", "/applications", nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (a *Arr) setupRootFolders() ([]map[string]any, error) {
	var raw []map[string]any
	if err := a.do("GET", "/rootfolder", nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func findSetupImplementation(schemas []map[string]any, implementation string) map[string]any {
	var first, preferred map[string]any
	for _, s := range schemas {
		if !strings.EqualFold(asStr(s["implementation"]), implementation) {
			continue
		}
		if first == nil {
			first = s
		}
		if strings.Contains(strings.ToLower(asStr(s["configContract"])), "qbittorrent") {
			preferred = s
			break
		}
	}
	if preferred != nil {
		return preferred
	}
	return first
}

func cloneResource(raw map[string]any) map[string]any {
	out := cloneMap(raw)
	out["fields"] = cloneFields(raw["fields"])
	return out
}

func cloneFields(v any) []any {
	raw, _ := v.([]any)
	out := make([]any, 0, len(raw))
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, cloneMap(m))
		} else {
			out = append(out, x)
		}
	}
	return out
}

func setupSetField(raw map[string]any, name string, value any) bool {
	_, byName := fieldMaps(raw)
	f := byName[name]
	if f == nil {
		return false
	}
	f["value"] = value
	return true
}

func setupFieldValue(raw map[string]any, name string) any {
	_, byName := fieldMaps(raw)
	if f := byName[name]; f != nil {
		return f["value"]
	}
	return nil
}

func requiredFieldProblem(raw map[string]any) string {
	for _, def := range fieldDefs(raw) {
		kind, supported, _ := publicFieldType(def)
		if !asBool(def["required"]) || kind == "info" {
			continue
		}
		label := asStr(def["label"])
		if label == "" {
			label = asStr(def["name"])
		}
		if !supported {
			return "the " + label + " setting is required but this helper cannot set it"
		}
		if kind != "select" && valueBlank(def["value"]) {
			return "the " + label + " setting is required and has no value"
		}
	}
	return ""
}

func (a *Arr) buildQBitClient(schema map[string]any, name string, ep qbitEndpoint, username, password, category string, enable *bool) (map[string]any, error) {
	raw := cloneResource(schema)
	delete(raw, "id")
	raw["name"], raw["enable"] = name, true
	if enable != nil {
		raw["enable"] = *enable
	}
	set := func(field string, value any) error {
		if !setupSetField(raw, field, value) {
			return fmt.Errorf("the qBittorrent schema has no %s setting", field)
		}
		return nil
	}
	if err := set("host", ep.host); err != nil {
		return nil, err
	}
	if err := set("port", ep.port); err != nil {
		return nil, err
	}
	if err := set("useSsl", ep.useSSL); err != nil {
		return nil, err
	}
	if err := set("urlBase", ep.urlBase); err != nil {
		return nil, err
	}
	if err := set("username", username); err != nil {
		return nil, err
	}
	if err := set("password", password); err != nil {
		return nil, err
	}
	cat, ok := qbitCategoryField(raw)
	if !ok {
		return nil, errors.New("the qBittorrent schema has no category setting")
	}
	if !setupSetField(raw, cat, category) {
		return nil, errors.New("the qBittorrent category setting could not be set")
	}
	if problem := requiredFieldProblem(raw); problem != "" {
		return nil, errors.New(problem)
	}
	return raw, nil
}

func (a *Arr) verifyQBitClientReadback(id int, desired map[string]any, schema map[string]any, ep qbitEndpoint, username, category, suppliedSecret string) (SetupCheck, error) {
	clients, err := a.setupDownloadClients()
	if err != nil {
		return CheckNotRun, err
	}
	var found map[string]any
	if id > 0 {
		for _, c := range clients {
			if asInt(c["id"]) == id {
				found = c
				break
			}
		}
	}
	if found == nil {
		// Fall back only when exactly one qBittorrent client carries the name; an
		// unrelated implementation with the same name must never satisfy readback.
		wantName := asStr(desired["name"])
		candidates := []map[string]any{}
		for _, c := range clients {
			if strings.EqualFold(asStr(c["name"]), wantName) && strings.EqualFold(asStr(c["implementation"]), "QBittorrent") {
				candidates = append(candidates, c)
			}
		}
		if len(candidates) != 1 {
			return CheckNotRun, errors.New("the new client is missing from the app's download client list")
		}
		found = candidates[0]
	}
	if !strings.EqualFold(asStr(found["implementation"]), "QBittorrent") {
		return CheckNotRun, errors.New("the readback client is not qBittorrent")
	}
	if want := asStr(desired["name"]); want != "" && !strings.EqualFold(asStr(found["name"]), want) {
		return CheckNotRun, errors.New("the readback name differs from the request")
	}
	if problem := compareQBitClient(found, schema, ep, username, category); problem != "" {
		return CheckNotRun, errors.New(problem)
	}
	if desiredEnable, ok := desired["enable"].(bool); ok {
		if actual, known := setupEnabled(found); known && actual != desiredEnable {
			return CheckNotRun, errors.New("the new client's enabled state differs from the request")
		}
	}
	secretCheck := setupSecretStatus(found, "password", suppliedSecret)
	if secretCheck == CheckFailed {
		return CheckFailed, errors.New("the saved password does not match the supplied value")
	}
	return secretCheck, nil
}

func compareQBitClient(raw map[string]any, schema map[string]any, ep qbitEndpoint, username, category string) string {
	if !setupFieldMatches(raw, "host", ep.host, true) {
		return "the readback host differs from the request"
	}
	if !setupFieldMatches(raw, "port", ep.port, false) {
		return "the readback port differs from the request"
	}
	if !setupFieldMatches(raw, "useSsl", ep.useSSL, false) {
		return "the readback SSL setting differs from the request"
	}
	if normalizeQBitBase(setupFieldString(raw, "urlBase")) != normalizeQBitBase(ep.urlBase) {
		return "the readback URL base differs from the request"
	}
	if username != "" && !setupFieldMatches(raw, "username", username, false) {
		return "the readback username differs from the request"
	}
	if cat, ok := qbitCategoryField(schema); ok && category != "" && !setupFieldMatches(raw, cat, category, false) {
		return "the readback category differs from the request"
	}
	return ""
}

func matchQBitClient(clients []map[string]any, schema map[string]any, ep qbitEndpoint, username, name, category string) (match map[string]any, conflict []string, existingName, existingCategory string) {
	catField, _ := qbitCategoryField(schema)
	matches := []map[string]any{}
	for _, c := range clients {
		if !strings.EqualFold(asStr(c["implementation"]), "QBittorrent") {
			continue
		}
		if !setupFieldMatches(c, "host", ep.host, true) || !setupFieldMatches(c, "port", ep.port, false) ||
			!setupFieldMatches(c, "useSsl", ep.useSSL, false) {
			continue
		}
		if normalizeQBitBase(setupFieldString(c, "urlBase")) != normalizeQBitBase(ep.urlBase) {
			continue
		}
		if username != "" && !setupFieldMatches(c, "username", username, false) {
			continue
		}
		matches = append(matches, c)
	}
	if len(matches) > 1 {
		return nil, []string{"address"}, "", ""
	}
	if len(matches) == 1 {
		found := matches[0]
		existingName = setupText(found["name"])
		if catField != "" {
			existingCategory = setupFieldString(found, catField)
		}
		if category != "" && catField == "" {
			return nil, []string{"category"}, existingName, existingCategory
		}
		if category != "" && setupFieldString(found, catField) != category {
			return nil, []string{"category"}, existingName, existingCategory
		}
		return found, nil, existingName, existingCategory
	}
	for _, c := range clients {
		if strings.EqualFold(setupText(c["name"]), name) {
			return nil, []string{"name"}, setupText(c["name"]), ""
		}
	}
	return nil, nil, "", ""
}

func (a *Arr) buildApplication(schema map[string]any, name, baseURL, apiKey, prowlarrURL string, enable *bool) (map[string]any, error) {
	raw := cloneResource(schema)
	delete(raw, "id")
	raw["name"], raw["enable"] = name, true
	if enable != nil {
		raw["enable"] = *enable
	}
	set := func(field string, value any) error {
		if !setupSetField(raw, field, value) {
			return fmt.Errorf("the %s schema has no %s setting", a.Name, field)
		}
		return nil
	}
	if err := set("baseUrl", baseURL); err != nil {
		return nil, err
	}
	if err := set("apiKey", apiKey); err != nil {
		return nil, err
	}
	if err := set("prowlarrUrl", prowlarrURL); err != nil {
		return nil, err
	}
	if problem := requiredFieldProblem(raw); problem != "" {
		return nil, errors.New(problem)
	}
	return raw, nil
}

func (a *Arr) verifyApplicationReadback(id int, desired map[string]any, target, baseURL, prowlarrURL, suppliedKey string) (SetupCheck, error) {
	apps, err := a.setupApplications()
	if err != nil {
		return CheckNotRun, err
	}
	var found map[string]any
	if id > 0 {
		for _, app := range apps {
			if asInt(app["id"]) == id {
				found = app
				break
			}
		}
	}
	if found == nil {
		wantName := asStr(desired["name"])
		candidates := []map[string]any{}
		for _, app := range apps {
			if strings.EqualFold(asStr(app["name"]), wantName) && strings.EqualFold(asStr(app["implementation"]), target) {
				candidates = append(candidates, app)
			}
		}
		if len(candidates) != 1 {
			return CheckNotRun, errors.New("the new link is missing from Prowlarr's application list")
		}
		found = candidates[0]
	}
	if want := asStr(desired["name"]); want != "" && !strings.EqualFold(asStr(found["name"]), want) {
		return CheckNotRun, errors.New("the readback name differs from the request")
	}
	if !strings.EqualFold(asStr(found["implementation"]), target) {
		return CheckNotRun, errors.New("the readback implementation differs from the request")
	}
	if normalizeURLBase(setupFieldString(found, "baseUrl")) != normalizeURLBase(baseURL) {
		return CheckNotRun, errors.New("the readback base URL differs from the request")
	}
	if normalizeURLBase(setupFieldString(found, "prowlarrUrl")) != normalizeURLBase(prowlarrURL) {
		return CheckNotRun, errors.New("the readback Prowlarr URL differs from the request")
	}
	if desiredEnable, ok := desired["enable"].(bool); ok {
		if actual, known := setupEnabled(found); known && actual != desiredEnable {
			return CheckNotRun, errors.New("the new link's enabled state differs from the request")
		}
	}
	secretCheck := setupSecretStatus(found, "apiKey", suppliedKey)
	if secretCheck == CheckFailed {
		return CheckFailed, errors.New("the saved API key does not match the supplied value")
	}
	return secretCheck, nil
}

func matchApplication(apps []map[string]any, target, baseURL, prowlarrURL, name string) (match map[string]any, conflict []string, existingName, existingBase, existingProwlarr string) {
	matches := []map[string]any{}
	for _, app := range apps {
		if !strings.EqualFold(asStr(app["implementation"]), target) {
			continue
		}
		if normalizeURLBase(setupFieldString(app, "baseUrl")) == normalizeURLBase(baseURL) {
			matches = append(matches, app)
		}
	}
	if len(matches) > 1 {
		return nil, []string{"baseUrl"}, "", "", ""
	}
	if len(matches) == 1 {
		found := matches[0]
		existingName = setupText(found["name"])
		existingBase = setupFieldString(found, "baseUrl")
		existingProwlarr = setupFieldString(found, "prowlarrUrl")
		if normalizeURLBase(existingProwlarr) != normalizeURLBase(prowlarrURL) {
			return nil, []string{"prowlarrUrl"}, existingName, existingBase, existingProwlarr
		}
		return found, nil, existingName, existingBase, existingProwlarr
	}
	for _, app := range apps {
		if strings.EqualFold(setupText(app["name"]), name) {
			return nil, []string{"name"}, setupText(app["name"]), setupFieldString(app, "baseUrl"), setupFieldString(app, "prowlarrUrl")
		}
	}
	return nil, nil, "", "", ""
}

func (q *QBit) categories() (map[string]string, error) {
	var raw map[string]struct {
		Name     string `json:"name"`
		SavePath string `json:"savePath"`
	}
	if err := q.call("/api/v2/torrents/categories", nil, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for name, c := range raw {
		out[name] = c.SavePath
	}
	return out, nil
}

// ---- small validators and comparisons ----

func validateSetupName(s string) error {
	if s == "" {
		return errors.New("a name is required")
	}
	if len(s) > 200 || strings.ContainsFunc(s, unicode.IsControl) {
		return errors.New("a name must be up to 200 ordinary characters")
	}
	return nil
}

func validateSetupText(what, s string, max int) error {
	if strings.TrimSpace(s) == "" {
		return errors.New(what + " is required")
	}
	if len(s) > max || strings.ContainsFunc(s, unicode.IsControl) {
		return fmt.Errorf("%s is invalid", what)
	}
	return nil
}

func validateSetupURL(what, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("%s must look like http://127.0.0.1:8989", what)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%s must be a plain address without credentials or query", what)
	}
	return strings.TrimRight(raw, "/"), nil
}

func parseQBitEndpoint(raw string) (qbitEndpoint, error) {
	base, err := validateSetupURL("the qBittorrent address", raw)
	if err != nil {
		return qbitEndpoint{}, err
	}
	u, _ := url.Parse(base)
	port := u.Port()
	if port == "" {
		return qbitEndpoint{}, errors.New("include qBittorrent's port in the address, e.g. http://127.0.0.1:8080")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return qbitEndpoint{}, errors.New("qBittorrent's port must be a whole number from 1 to 65535")
	}
	return qbitEndpoint{host: u.Hostname(), port: n, useSSL: u.Scheme == "https", urlBase: normalizeQBitBase(u.EscapedPath())}, nil
}

func normalizeQBitBase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "/" {
		return "/"
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return strings.TrimRight(s, "/")
}

func normalizeURLBase(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

func normalizedSetupPath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) <= 1 || (len(p) == 3 && p[1] == ':') {
		return p
	}
	return strings.TrimRight(p, "/\\")
}

func defaultCategory(app string) string {
	switch app {
	case "Sonarr":
		return "tv"
	case "Radarr":
		return "movies"
	case "Lidarr":
		return "music"
	}
	return ""
}

func canonicalArrName(s string) string {
	for _, name := range []string{"Sonarr", "Radarr", "Lidarr"} {
		if strings.EqualFold(strings.TrimSpace(s), name) {
			return name
		}
	}
	return ""
}

func qbitCategoryField(raw map[string]any) (string, bool) {
	for _, name := range []string{"tvCategory", "movieCategory", "musicCategory", "category"} {
		if _, byName := fieldMaps(raw); byName[name] != nil {
			return name, true
		}
	}
	_, byName := fieldMaps(raw)
	for name := range byName {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "category") && !strings.Contains(lower, "priority") {
			return name, true
		}
	}
	return "", false
}

func setupEnabled(raw map[string]any) (bool, bool) {
	v, ok := raw["enable"].(bool)
	return v, ok
}

func setupAccessible(raw map[string]any) (*bool, *int64) {
	var accessible *bool
	if v, ok := raw["accessible"].(bool); ok {
		accessible = &v
	}
	var free *int64
	if n, ok := numberValue(raw["freeSpace"]); ok {
		v := int64(n)
		free = &v
	}
	return accessible, free
}

func setupAccessCheck(accessible *bool) SetupCheck {
	if accessible == nil {
		return CheckNotRun
	}
	if *accessible {
		return CheckPassed
	}
	return CheckFailed
}

func findRootFolder(folders []map[string]any, path string) map[string]any {
	want := normalizedSetupPath(path)
	for _, f := range folders {
		if normalizedSetupPath(asStr(f["path"])) == want {
			return f
		}
	}
	return nil
}

func setupFieldMatches(raw map[string]any, name string, want any, fold bool) bool {
	got := setupFieldValue(raw, name)
	if got == nil {
		return false
	}
	switch w := want.(type) {
	case string:
		g := asStr(got)
		if fold {
			return strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(w))
		}
		return strings.TrimSpace(g) == strings.TrimSpace(w)
	case int:
		n, ok := numberValue(got)
		return ok && int(n) == w
	case bool:
		b, ok := got.(bool)
		return ok && b == w
	}
	return false
}

func setupFieldString(raw map[string]any, name string) string {
	return strings.TrimSpace(asStr(setupFieldValue(raw, name)))
}

func setupFieldStringValue(raw map[string]any, name string) (string, bool) {
	_, byName := fieldMaps(raw)
	f := byName[name]
	if f == nil {
		return "", false
	}
	s, _ := f["value"].(string)
	return s, true
}

// secretLooksHidden recognises masked values that apps use in place of the real secret.
func secretLooksHidden(s string) bool {
	return strings.Trim(s, "*•. ") == ""
}

// setupSecretStatus is passed only when the API returns the saved secret and it
// equals the supplied value. A missing, empty or masked value is honestly not_run.
func setupSecretStatus(raw map[string]any, field, supplied string) SetupCheck {
	saved, present := setupFieldStringValue(raw, field)
	if !present || saved == "" || secretLooksHidden(saved) {
		return CheckNotRun
	}
	if saved == supplied {
		return CheckPassed
	}
	return CheckFailed
}

func setupText(v any) string { return strings.TrimSpace(asStr(v)) }

func (a *Arr) setupErrorText(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	s := a.SafeError(err).Error()
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "REDACTED")
		}
	}
	return s
}

func qbitErrorText(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "REDACTED")
		}
	}
	return s
}
