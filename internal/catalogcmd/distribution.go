package catalogcmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
		return 2
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
		fmt.Fprintf(stdout, "RESULT catalog-%s %s sequence=%d sha256=%s previous_sha256=%s latest_sequence=%d latest_sha256=%s\n",
			verb, status(result.Changed, dry), result.Active.Sequence, result.Active.SHA256, result.PreviousSHA256, result.Latest.Sequence, result.Latest.SHA256)
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
		fmt.Fprintf(stdout, "RESULT catalog-inspect verified sequence=%d sha256=%s\n", view.Active.Sequence, view.Active.SHA256)
		for _, id := range sortedKeys(d.Presets) {
			if preset != "" && preset != id {
				continue
			}
			p := d.Presets[id]
			fmt.Fprintf(stdout, "PRESET %s name=%q engine=%s context=%d recommended=%t\n", id, p.DisplayName, p.Engine, p.ContextWindowTokens, p.Recommended)
			if p.Description != "" {
				fmt.Fprintln(stdout, "  "+p.Description)
			}
			if p.AssessmentURL != "" {
				fmt.Fprintln(stdout, "  Assessment: "+p.AssessmentURL)
			}
			if preset != "" {
				data, err := json.MarshalIndent(p, "  ", "  ")
				if err != nil {
					return failed(stderr, err)
				}
				fmt.Fprintln(stdout, string(data))
			}
		}
		for _, snapshot := range view.Snapshots {
			fmt.Fprintf(stdout, "SNAPSHOT sequence=%d sha256=%s active=%t latest=%t\n", snapshot.Sequence, snapshot.SHA256, snapshot.SHA256 == view.Active.SHA256, snapshot.SHA256 == view.Latest.SHA256)
		}
		return 0
	default:
		usage(stderr)
		return 2
	}
}
