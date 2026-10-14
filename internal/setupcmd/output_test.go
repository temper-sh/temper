package setupcmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/managed"
)

func TestRenderLayoutTellsWhereAndWhatIsRunning(t *testing.T) {
	job := &managed.Job{Layout: "default", Name: "Default", Generation: "g1", Listen: "127.0.0.1:8080", Presets: []string{"qwen", "gemma"}}
	running := managed.Status{
		State:    managed.State{Current: job, Desired: job},
		Observed: managed.Observation{Exists: true, Ready: true, Models: map[string]string{"qwen": "running; engine readiness not checked", "gemma": "available on demand"}},
	}
	cases := []struct {
		name   string
		report layoutReport
		want   []string
		absent []string
	}{
		{"status", layoutReport{Operation: "status", Before: job, Status: running, Default: "qwen"},
			[]string{"Layout Default is running at http://127.0.0.1:8080/v1", "qwen", "loaded; also answers as \"default\"", "gemma", "loads on first request", "temper layout stop"}, []string{"{"}},
		{"fresh activation", layoutReport{Operation: "activate", Status: running},
			[]string{"Started layout Default at http://127.0.0.1:8080/v1"}, nil},
		{"repeated activation", layoutReport{Operation: "activate", Before: job, Status: running},
			[]string{"already running"}, []string{"Started"}},
		{"dry activation", layoutReport{Operation: "activate", Dry: true, Status: managed.Status{State: managed.State{Desired: job}}},
			[]string{"Dry run: would start layout Default", "Nothing changed."}, []string{"Models"}},
		{"nothing to stop", layoutReport{Operation: "stop"}, []string{"No layout is running."}, nil},
		{"stop", layoutReport{Operation: "stop", Before: job}, []string{"Stopped layout Default."}, nil},
		{"no layout yet", layoutReport{Operation: "status", Saved: []string{"default"}},
			[]string{"No layout is running.", "temper layout activate default"}, nil},
		{"stopped job", layoutReport{Operation: "status", Status: managed.Status{State: managed.State{Current: job}}},
			[]string{"Layout Default is not running.", "temper layout activate default"}, []string{"Models"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			renderLayout(&out, tc.report)
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q in:\n%s", want, out.String())
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("unexpected %q in:\n%s", absent, out.String())
				}
			}
		})
	}
}

func TestLayoutNameShowsBothNameAndID(t *testing.T) {
	for _, tc := range []struct{ layout, name, want string }{
		{"work", "Work tools", "Work tools (work)"},
		{"default", "Default", "Default"},
		{"solo", "", "solo"},
	} {
		if got := layoutName(&managed.Job{Layout: tc.layout, Name: tc.name}); got != tc.want {
			t.Errorf("layoutName(%q, %q) = %q, want %q", tc.layout, tc.name, got, tc.want)
		}
	}
}
