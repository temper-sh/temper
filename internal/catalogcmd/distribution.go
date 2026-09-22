package catalogcmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/temper-sh/temper/internal/catalog"
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
	var profile, out, sha string
	if verb != "inspect" {
		f.BoolVar(&dry, "dry-run", false, "validate and report without writes")
	}
	if verb == "inspect" || verb == "select" {
		f.StringVar(&profile, "profile", "", "explicit profile ID")
	}
	if verb == "select" {
		f.StringVar(&out, "out", "", "new user-owned selection path")
	}
	if verb == "rollback" {
		f.StringVar(&sha, "snapshot", "", "exact retained catalog SHA-256")
	}
	if err := f.Parse(args[1:]); err != nil {
		return 2
	}
	if f.NArg() != 0 || *root == "" || verb == "select" && (profile == "" || out == "") || verb == "rollback" && sha == "" {
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
		if profile != "" {
			if _, ok := d.Profiles[profile]; !ok {
				return failed(stderr, fmt.Errorf("unknown profile %q", profile))
			}
		}
		if *jsonOutput {
			result := map[string]any{"active": view.Active.Identity, "latest": view.Latest, "snapshots": view.Snapshots, "catalog": d}
			if profile != "" {
				result["profile"] = profile
				result["selection"] = d.Profiles[profile]
			}
			return encode(stdout, stderr, result)
		}
		fmt.Fprintf(stdout, "RESULT catalog-inspect verified sequence=%d sha256=%s\n", view.Active.Sequence, view.Active.SHA256)
		for _, id := range sortedKeys(d.Profiles) {
			if profile != "" && profile != id {
				continue
			}
			p := d.Profiles[id]
			fmt.Fprintf(stdout, "PROFILE %s gpu_memory_utilization=%g\n", id, p.GPUMemoryUtilization)
			for _, b := range p.Bindings {
				l := d.Layouts[b.Layout]
				fmt.Fprintf(stdout, "  LAYOUT %s name=%q engine=%s context=%d route=%s residency=%s\n", b.Layout, l.DisplayName, l.Engine, l.ContextWindowTokens, b.Route, b.Residency)
				if profile != "" {
					data, err := json.MarshalIndent(l, "  ", "  ")
					if err != nil {
						return failed(stderr, err)
					}
					fmt.Fprintln(stdout, string(data))
				}
			}
		}
		for _, snapshot := range view.Snapshots {
			fmt.Fprintf(stdout, "SNAPSHOT sequence=%d sha256=%s active=%t latest=%t\n", snapshot.Sequence, snapshot.SHA256, snapshot.SHA256 == view.Active.SHA256, snapshot.SHA256 == view.Latest.SHA256)
		}
		return 0
	case "select":
		snapshot, err := distribution.Read(*root, trust)
		if err != nil {
			return failed(stderr, err)
		}
		if _, ok := snapshot.Document.Profiles[profile]; !ok {
			return failed(stderr, fmt.Errorf("unknown profile %q", profile))
		}
		selection := catalog.Selection{Schema: catalog.SelectionSchema, Profile: profile}
		if err := selection.Validate(); err != nil {
			return failed(stderr, err)
		}
		data, err := json.MarshalIndent(selection, "", "  ")
		if err != nil {
			return failed(stderr, err)
		}
		changed, err := publishFile(ctx, out, append(data, '\n'), dry)
		if err != nil {
			return failed(stderr, err)
		}
		if *jsonOutput {
			return encode(stdout, stderr, map[string]any{"profile": profile, "path": out, "catalog_sha256": snapshot.SHA256, "changed": changed, "dry_run": dry})
		}
		fmt.Fprintf(stdout, "RESULT catalog-select %s profile=%s path=%q catalog_sha256=%s\n", status(changed, dry), profile, out, snapshot.SHA256)
		return 0
	default:
		usage(stderr)
		return 2
	}
}
