package catalogcmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/catalog/distribution"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
)

func runDistribution(ctx context.Context, args []string, stdout, stderr io.Writer, trust publication.TrustRoot, source distribution.Source) int {
	verb := args[0]
	f := flag.NewFlagSet("temper catalog "+verb, flag.ContinueOnError)
	f.SetOutput(stderr)
	root := f.String("root", "", "explicit Temper data root")
	jsonOutput := f.Bool("json", false, "print the result as JSON")
	var dry bool
	var preset, sha string
	if verb != "inspect" {
		f.BoolVar(&dry, "dry-run", false, "validate and report without writes")
	}
	if verb == "inspect" {
		f.StringVar(&preset, "preset", "", "explicit preset ID")
	}
	if verb == "rollback" {
		f.StringVar(&sha, "snapshot", "", "exact retained catalog SHA-256")
	}
	if err := f.Parse(args[1:]); err != nil {
		return parseFailure(err)
	}
	if f.NArg() != 0 || *root == "" || verb == "rollback" && sha == "" {
		usage(stderr)
		return 2
	}
	if err := ctx.Err(); err != nil {
		return failed(stderr, err)
	}
	switch verb {
	case "update", "rollback":
		var result distribution.Result
		var err error
		if verb == "update" {
			bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			result, err = distribution.Update(bounded, *root, dry, trust, source)
		} else {
			result, err = distribution.Rollback(ctx, *root, sha, dry, trust)
		}
		if err != nil {
			return failed(stderr, err)
		}
		if *jsonOutput {
			return encode(stdout, stderr, result)
		}
		switch {
		case !result.Changed && verb == "update":
			fmt.Fprintf(stdout, "The catalog is up to date (sequence %d).\n", result.Active.Sequence)
		case !result.Changed:
			fmt.Fprintf(stdout, "Catalog sequence %d is already active.\n", result.Active.Sequence)
		case dry && verb == "update":
			fmt.Fprintf(stdout, "Dry run: would update the catalog to sequence %d. Nothing changed.\n", result.Active.Sequence)
		case dry:
			fmt.Fprintf(stdout, "Dry run: would roll the catalog back to sequence %d. Nothing changed.\n", result.Active.Sequence)
		case verb == "update":
			fmt.Fprintf(stdout, "Updated the catalog to sequence %d.\n", result.Active.Sequence)
		default:
			fmt.Fprintf(stdout, "Rolled the catalog back to sequence %d.\n", result.Active.Sequence)
		}
		return 0
	case "inspect":
		view, err := distribution.Inspect(*root, trust)
		if err != nil {
			return failed(stderr, err)
		}
		d := view.Active.Document
		if preset != "" {
			if _, ok := d.Presets[preset]; !ok {
				return failed(stderr, fmt.Errorf("unknown preset %q", preset))
			}
		}
		if *jsonOutput {
			result := map[string]any{"active": view.Active.Identity, "latest": view.Latest, "snapshots": view.Snapshots, "catalog": d}
			if preset != "" {
				result["preset"] = d.Presets[preset]
			}
			return encode(stdout, stderr, result)
		}
		fmt.Fprintf(stdout, "Catalog sequence %d of %s (signature verified)\n", view.Active.Sequence, d.Date)
		for _, id := range sortedKeys(d.Presets) {
			if preset != "" && preset != id {
				continue
			}
			p := d.Presets[id]
			engine := p.Engine
			if e, ok := d.Engines[p.Engine]; ok && e.Presentation.DisplayName != "" {
				engine = e.Presentation.DisplayName
			}
			name := p.Presentation.DisplayName
			if name == "" {
				name = id
			}
			fmt.Fprintf(stdout, "\n%s\n  %s · %s · context %d tokens · %s\n", id, name, engine, p.Window(), strings.ToUpper(string(p.Compute)))
			if _, section := d.Placement(p.Presentation); section != nil {
				fmt.Fprintf(stdout, "  %s: %s\n", section.DisplayName, section.Description)
			}
			if p.Presentation.Description != "" {
				fmt.Fprintln(stdout, "  "+p.Presentation.Description)
			}
			if p.Presentation.AssessmentURL != "" {
				fmt.Fprintln(stdout, "  Assessment: "+p.Presentation.AssessmentURL)
			}
		}
		if preset == "" && len(view.Snapshots) > 1 {
			fmt.Fprintln(stdout, "\nKept catalogs (roll back with: temper catalog rollback --snapshot SHA256):")
			for _, snapshot := range view.Snapshots {
				state := ""
				if snapshot.SHA256 == view.Active.SHA256 {
					state = " (active)"
				}
				fmt.Fprintf(stdout, "  sequence %d  %s%s\n", snapshot.Sequence, snapshot.SHA256, state)
			}
		}
		return 0
	default:
		usage(stderr)
		return 2
	}
}
