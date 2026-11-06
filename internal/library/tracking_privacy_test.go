package library

import (
	"librarystudio/internal/config"
	"librarystudio/internal/stock"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrackingErrorsDoNotExposeSourceCredentials(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`[{"errorMessage":"Failed at https://source.invalid/download?apikey=private-value token=private-value"}]`))
	}))
	defer upstream.Close()
	c := config.Default()
	c.Radarr = config.App{URL: upstream.URL, APIKey: "private-value"}
	_, err := Releases(c, "Radarr", stock.ReleaseContext{ID: 1})
	if err == nil {
		t.Fatal("upstream failure missing")
	}
	if strings.Contains(err.Error(), "private-value") || strings.Contains(err.Error(), "https://") {
		t.Fatal("upstream credentials leaked")
	}
}
