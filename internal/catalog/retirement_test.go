package catalog_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software"
)

func retiredDocument() catalog.Document {
	d := document()
	d.RetiredPresets = map[string]catalog.Retirement{"old-qwen": {RetiredOn: d.Date, Reason: "Lower-memory successor", Successor: "qwen-32k"}, "older-qwen": {RetiredOn: d.Date, Reason: "Replaced", Successor: "old-qwen"}, "withdrawn": {RetiredOn: d.Date, Reason: "No supported successor"}}
	return d
}

func TestRetirementRoundTripChainAndExecutionIndependence(t *testing.T) {
	d := retiredDocument()
	raw, _ := json.Marshal(d)
	parsed, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"old-qwen", "older-qwen"} {
		if got, err := parsed.Successor(id); err != nil || got != "qwen-32k" {
			t.Fatal(got, err)
		}
	}
	if _, err := parsed.Successor("withdrawn"); err == nil {
		t.Fatal("invented a successor")
	}
	lock, err := catalog.CompilePreset(parsed, "qwen-32k", 32, 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Records.RetiredPresets) != 0 {
		t.Fatal("retirement metadata leaked into exact execution")
	}
	view, err := catalog.Read(raw, "99.0.0")
	if err != nil || len(view.Document.RetiredPresets) != 3 {
		t.Fatal(err, view.Unavailable)
	}
}

func TestRetirementPublisherRejectsInvalidRecordsAndGraphs(t *testing.T) {
	for name, edit := range map[string]func(*catalog.Document){
		"missing date": func(d *catalog.Document) {
			r := d.RetiredPresets["old-qwen"]
			r.RetiredOn = ""
			d.RetiredPresets["old-qwen"] = r
		},
		"invalid date": func(d *catalog.Document) {
			r := d.RetiredPresets["old-qwen"]
			r.RetiredOn = "2026-02-30"
			d.RetiredPresets["old-qwen"] = r
		},
		"future date": func(d *catalog.Document) {
			r := d.RetiredPresets["old-qwen"]
			r.RetiredOn = "2999-01-01"
			d.RetiredPresets["old-qwen"] = r
		},
		"empty reason": func(d *catalog.Document) {
			r := d.RetiredPresets["old-qwen"]
			r.Reason = " "
			d.RetiredPresets["old-qwen"] = r
		},
		"terminal injection": func(d *catalog.Document) {
			r := d.RetiredPresets["old-qwen"]
			r.Reason = "hello\x1b[2J"
			d.RetiredPresets["old-qwen"] = r
		},
		"missing target": func(d *catalog.Document) {
			r := d.RetiredPresets["old-qwen"]
			r.Successor = "missing"
			d.RetiredPresets["old-qwen"] = r
		},
		"self cycle": func(d *catalog.Document) {
			r := d.RetiredPresets["old-qwen"]
			r.Successor = "old-qwen"
			d.RetiredPresets["old-qwen"] = r
		},
		"chain cycle": func(d *catalog.Document) {
			r := d.RetiredPresets["old-qwen"]
			r.Successor = "older-qwen"
			d.RetiredPresets["old-qwen"] = r
		},
		"active collision": func(d *catalog.Document) {
			d.RetiredPresets["qwen-32k"] = catalog.Retirement{RetiredOn: d.Date, Reason: "Collision"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := retiredDocument()
			edit(&d)
			raw, _ := json.Marshal(d)
			if _, err := catalog.Parse(raw); err == nil {
				t.Fatal("invalid retirement accepted")
			}
		})
	}
}

func TestRetirementReaderKeepsUnavailableSuccessorDistinctFromRetirement(t *testing.T) {
	value := tree(t)
	value["retired_presets"] = map[string]any{"old-qwen": map[string]any{"retired_on": value["date"], "reason": "Replaced", "successor": "qwen-32k"}}
	record(t, value, "presets", "qwen-32k")["future_feature"] = true
	view := read(t, value, "99.0.0")
	if len(view.Document.RetiredPresets) != 1 || len(view.Document.Presets) != 1 {
		t.Fatal("unavailable successor corrupted retirement or other choices", view.Unavailable)
	}
	if _, err := view.Document.Successor("old-qwen"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatal(err)
	}
	delete(value, "retired_presets")
	view = read(t, value, "99.0.0")
	if len(view.Document.RetiredPresets) != 0 {
		t.Fatal("absence was treated as retirement")
	}
}

func TestRetirementUnknownRecordDoesNotBreakUnrelatedChoices(t *testing.T) {
	value := tree(t)
	value["retired_presets"] = map[string]any{"future-old": map[string]any{"retired_on": value["date"], "reason": "Future", "new_policy": true}}
	view := read(t, value, "99.0.0")
	if len(view.Document.Presets) != 2 || len(view.Document.RetiredPresets) != 0 || len(view.Unavailable) != 1 || view.Unavailable[0].Kind != "retired-preset" {
		t.Fatal(view.Unavailable)
	}
	raw, _ := json.Marshal(value)
	if _, err := catalog.Parse(raw); err == nil {
		t.Fatal("publisher ignored an unknown retirement field")
	}
}

func TestRetirementDatesSurvivePublicationAndNewDatesMatchPublication(t *testing.T) {
	previous := retiredDocument()
	next := retiredDocument()
	next.Date = "2026-10-01"
	if err := next.CheckRetirementHistory(previous); err != nil {
		t.Fatal(err)
	}
	r := next.RetiredPresets["old-qwen"]
	r.Reason = "Reworded"
	r.Successor = "qwen-32k"
	next.RetiredPresets["old-qwen"] = r
	if err := next.CheckRetirementHistory(previous); err != nil {
		t.Fatal(err)
	}
	r.RetiredOn = next.Date
	next.RetiredPresets["old-qwen"] = r
	if err := next.CheckRetirementHistory(previous); err == nil {
		t.Fatal("retirement date was rewritten")
	}
	next = retiredDocument()
	next.Date = "2026-10-01"
	next.RetiredPresets = map[string]catalog.Retirement{"new-old": {RetiredOn: next.Date, Reason: "Latest retirement", Successor: "qwen-32k"}}
	if err := next.CheckRetirementHistory(previous); err != nil {
		t.Fatal("could not remove earlier notices", err)
	}
	next.RetiredPresets["qwen-32k"] = catalog.Retirement{RetiredOn: "2026-09-12", Reason: "New withdrawal"}
	if err := next.CheckRetirementHistory(previous); err == nil {
		t.Fatal("backdated retirement of a currently offered ID")
	}
}

func TestRetirementNoticesAccumulateAcrossPublicationsUntilRemoved(t *testing.T) {
	previous := retiredDocument()
	next := retiredDocument()
	next.Date = "2026-10-01"
	next.RetiredPresets["later-withdrawal"] = catalog.Retirement{RetiredOn: next.Date, Reason: "Later", Successor: "qwen-32k"}
	if err := next.Validate(); err != nil {
		t.Fatal("rejected notices from several dates", err)
	}
	if err := next.CheckRetirementHistory(previous); err != nil {
		t.Fatal("a later retirement dropped earlier notices", err)
	}
	// Cleanup and reversing a mistaken retirement are reviewed source edits.
	delete(next.RetiredPresets, "withdrawn")
	if err := next.CheckRetirementHistory(previous); err != nil {
		t.Fatal("could not remove an old notice", err)
	}
	next.Presets["withdrawn"] = next.Presets["qwen-32k"]
	if err := next.CheckRetirementHistory(previous); err != nil {
		t.Fatal("could not offer a retired ID again", err)
	}
}

func TestASameDayRetirementKeepsTheEarlierNotices(t *testing.T) {
	previous := retiredDocument()
	next := retiredDocument()
	next.RetiredPresets["another-withdrawal"] = catalog.Retirement{RetiredOn: next.Date, Reason: "Same day", Successor: "qwen-32k"}
	if err := next.CheckRetirementHistory(previous); err != nil {
		t.Fatal("a same-day addition replaced earlier notices", err)
	}
}

func TestReaderOffersEveryValidNoticeWhateverItsDate(t *testing.T) {
	value := tree(t)
	value["retired_presets"] = map[string]any{"old-notice": map[string]any{"retired_on": "2026-09-12", "reason": "Older", "successor": "qwen-32k"}, "current-notice": map[string]any{"retired_on": value["date"], "reason": "Latest", "successor": "qwen-32k"}}
	view := read(t, value, "99.0.0")
	if len(view.Document.RetiredPresets) != 2 {
		t.Fatal("reader hid an older notice", view.Document.RetiredPresets)
	}
}
