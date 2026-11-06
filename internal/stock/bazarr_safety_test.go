package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBazarrDoesNotForwardKeyOnRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	if _, err := NewBazarr(redirect.URL, "fixture-only-key").Status(); err == nil || reached {
		t.Fatal("Bazarr redirect must not forward a credential")
	}
}
func TestBazarrReadinessDecodesOnlyProviderNames(t *testing.T) {
	response := `{"general":{"enabled_providers":["podnapisi"]},"auth":{"apikey":"fixture-only-sensitive-value"},"provider":{"password":"fixture-only-sensitive-value"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(response)) }))
	defer srv.Close()
	providers, err := NewBazarr(srv.URL, "fixture-only-key").ProviderNames()
	if err != nil || len(providers) != 1 || providers[0] != "podnapisi" {
		t.Fatal("provider readiness missing")
	}
	b, _ := json.Marshal(providers)
	if strings.Contains(string(b), "sensitive") {
		t.Fatal("settings escaped allowlist")
	}
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"general":{}}`)) }))
	defer missing.Close()
	if _, err := NewBazarr(missing.URL, "fixture-only-key").ProviderNames(); err == nil {
		t.Fatal("missing provider state read as empty/success")
	}
}
