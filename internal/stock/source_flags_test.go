package stock

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLibrarySourceModesUseActualArrFlags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":1,"name":"Interactive","enableInteractiveSearch":true},{"id":2,"name":"Disabled","enableRss":false,"enableAutomaticSearch":false,"enableInteractiveSearch":false},{"id":3,"name":"Unknown"}]`))
	}))
	defer srv.Close()
	items, err := NewArr("Sonarr", srv.URL, "test-value").LibraryIndexers()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Enabled == nil || !*items[0].Enabled || items[1].Enabled == nil || *items[1].Enabled || items[2].Enabled != nil {
		t.Fatal("actual source flags or unknown state misreported")
	}
}
