package setupcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/setup"
)

// Commands print plain words by default. --json prints one JSON document for
// scripts and Field Kit instead, and nothing else on stdout.

func writeJSON(out io.Writer, value any) error {
	e := json.NewEncoder(out)
	e.SetIndent("", "  ")
	return e.Encode(value)
}

// layoutReport is what a layout command shows after its work.
type layoutReport struct {
	Operation string
	Dry       bool
	// Before is the job known before the command ran; stop clears it.
	Before *managed.Job
	Status managed.Status
	// Default is the preset that answers the "default" model ID.
	Default string
	// Saved lists the configured layout IDs, for the next-step hint.
	Saved []string
}

func renderLayout(out io.Writer, r layoutReport) {
	job := r.Status.State.Current
	if job == nil {
		job = r.Status.State.Desired
	}
	switch r.Operation {
	case "stop":
		switch {
		case r.Before == nil:
			fmt.Fprintln(out, "No layout is running.")
		case r.Dry:
			fmt.Fprintf(out, "Dry run: would stop layout %s. Nothing changed.\n", layoutName(r.Before))
		default:
			fmt.Fprintf(out, "Stopped layout %s.\n", layoutName(r.Before))
		}
		if r.Status.Error != "" {
			fmt.Fprintln(out, "Problem: "+r.Status.Error)
		}
		return
	case "activate":
		if r.Dry && job != nil {
			fmt.Fprintf(out, "Dry run: would start layout %s at %s. Nothing changed.\n", layoutName(job), baseURL(job))
			return
		}
	}
	if job == nil {
		fmt.Fprintln(out, "No layout is running.")
		if len(r.Saved) > 0 {
			fmt.Fprintf(out, "Start one with: temper layout activate %s\n", strings.Join(r.Saved, " | "))
		}
		return
	}
	o := r.Status.Observed
	switch {
	case o.Exists && o.Ready && (r.Operation == "activate" || r.Operation == "restart") && r.Before != nil && r.Before.Generation == job.Generation:
		fmt.Fprintf(out, "Layout %s is already running at %s\n", layoutName(job), baseURL(job))
	case o.Exists && o.Ready && r.Operation == "restart":
		fmt.Fprintf(out, "Restarted layout %s at %s\n", layoutName(job), baseURL(job))
	case o.Exists && o.Ready && r.Operation == "activate":
		fmt.Fprintf(out, "Started layout %s at %s\n", layoutName(job), baseURL(job))
	case o.Exists && o.Ready:
		fmt.Fprintf(out, "Layout %s is running at %s\n", layoutName(job), baseURL(job))
	case o.Exists:
		fmt.Fprintf(out, "Layout %s is starting at %s; the router is not answering yet.\n", layoutName(job), baseURL(job))
	default:
		fmt.Fprintf(out, "Layout %s is not running.\n", layoutName(job))
	}
	if r.Status.Error != "" {
		fmt.Fprintln(out, "Problem: "+r.Status.Error)
	}
	if !o.Exists {
		fmt.Fprintf(out, "Start it with: temper layout activate %s\n", job.Layout)
		return
	}
	fmt.Fprintln(out, "Models (use these IDs in requests):")
	ids := slices.Clone(job.Presets)
	slices.Sort(ids)
	width := 0
	for _, id := range ids {
		width = max(width, len(id))
	}
	for _, id := range ids {
		state := "loads on first request"
		if strings.HasPrefix(o.Models[id], "running") {
			state = "loaded"
		}
		if id == r.Default {
			state += `; also answers as "default"`
		}
		fmt.Fprintf(out, "  %-*s  %s\n", width, id, state)
	}
	if o.Busy {
		fmt.Fprintln(out, "It is answering a request right now.")
	}
	fmt.Fprintln(out, "Stop it with: temper layout stop")
}

func layoutName(job *managed.Job) string {
	switch {
	case job.Name == "":
		return job.Layout
	case strings.EqualFold(job.Name, job.Layout):
		return job.Name
	}
	return fmt.Sprintf("%s (%s)", job.Name, job.Layout)
}

func baseURL(job *managed.Job) string {
	return "http://" + job.Listen + "/v1"
}

// savedLayouts reads the saved layout IDs and the default preset of one
// layout for next-step hints; a missing configuration has neither.
func savedLayouts(root, layout string) (ids []string, defaultPreset string) {
	c, _, err := setup.ReadConfiguration(root)
	if err != nil {
		return nil, ""
	}
	for id := range c.Layouts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, c.Layouts[layout].Default
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// renderConfiguration is configure --show in plain words.
func renderConfiguration(out io.Writer, c setup.Configuration, revision string) {
	if revision == "" {
		fmt.Fprintln(out, "No saved configuration. Choose presets with: temper init")
		return
	}
	ids := slices.Sorted(maps.Keys(c.Presets))
	fmt.Fprintln(out, "Presets:")
	for _, id := range ids {
		p := c.Presets[id]
		fmt.Fprintf(out, "  %s  %s\n", id, p.Name)
	}
	fmt.Fprintln(out, "Layouts:")
	for _, id := range slices.Sorted(maps.Keys(c.Layouts)) {
		l := c.Layouts[id]
		line := fmt.Sprintf("  %s  %s: %s", id, l.Name, strings.Join(l.Presets, ", "))
		if l.Default != "" {
			line += "; default " + l.Default
		}
		fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "Software: %s\n", c.Software)
	fmt.Fprintf(out, "Revision: %s (scripted edits pass it with --revision)\n", revision)
}
