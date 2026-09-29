package managed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivityRequiresExplicitInflightSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, body    string
		busy, invalid bool
	}{
		{"idle", `data: {"type":"inflight","data":"{\"operation\":\"snapshot\",\"requests\":[]}"}`, false, false},
		// v260 omits the requests field in an empty snapshot (omitempty).
		{"idle omitted", `data:{"type":"inflight","data":"{\"operation\":\"snapshot\"}"}`, false, false},
		{"busy", `data: {"type":"inflight","data":"{\"operation\":\"snapshot\",\"requests\":[{}]}"}`, true, false},
		{"missing", `data: {"type":"status","data":"{}"}`, false, true},
		{"null", `data: {"type":"inflight","data":"{\"operation\":\"snapshot\",\"requests\":null}"}`, false, true},
		{"malformed", `data: {broken`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/events" {
					t.Error(r.URL.Path)
				}
				fmt.Fprintln(w, tc.body)
			}))
			defer server.Close()
			busy, err := readActivity(context.Background(), strings.TrimPrefix(server.URL, "http://"))
			if (err != nil) != tc.invalid || busy != tc.busy {
				t.Fatal(busy, err)
			}
		})
	}
}
