package softwarecmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	checkverb "github.com/temper-sh/temper/internal/software/check"
	installverb "github.com/temper-sh/temper/internal/software/install"
	"github.com/temper-sh/temper/internal/software/installplan"
	removeverb "github.com/temper-sh/temper/internal/software/remove"
)

// The commands print a short summary by default; --json prints the full
// report as one document. In-process callers receive the summary.

type counts struct {
	Packages     int `json:"packages"`
	Units        int `json:"units"`
	Effects      int `json:"effects,omitempty"`
	Claims       int `json:"claims,omitempty"`
	Requirements int `json:"requirements,omitempty"`
	Problems     int `json:"problems,omitempty"`
}

type unitEffect struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Ownership string `json:"ownership"`
	Claim     string `json:"claim"`
}

type effectGroup struct {
	ID     string       `json:"id"`
	Effect string       `json:"effect"`
	Units  []unitEffect `json:"units"`
}

type packageRow struct {
	ID       string `json:"id"`
	Method   string `json:"method"`
	Adapter  string `json:"adapter"`
	RootUnit string `json:"root_unit"`
}

type installReport struct {
	Schema       string        `json:"schema"`
	Status       string        `json:"status"`
	Installation string        `json:"installation"`
	Counts       counts        `json:"counts"`
	Packages     []packageRow  `json:"packages"`
	Effects      []effectGroup `json:"effects"`
}

func newInstallReport(result installverb.Result) installReport {
	r := installReport{Schema: "temper-software-install/v1", Status: changeStatus(result.Changed, result.DryRun), Installation: result.Installation,
		Counts: counts{Packages: result.Packages, Units: result.Units, Effects: result.Effects, Claims: result.Claims}}
	for _, row := range result.PackageRows {
		r.Packages = append(r.Packages, packageRow{ID: row.ID, Method: string(row.Method), Adapter: row.Adapter, RootUnit: row.RootUnit})
	}
	for _, group := range result.Groups {
		effect := "unchanged"
		if group.ChangesProvider() {
			effect = "install"
			if group.EffectModel == installplan.EffectIsolated {
				effect = "publish-isolated"
			}
		}
		g := effectGroup{ID: group.ID, Effect: effect}
		for _, unit := range group.Units {
			claim := string(unit.ClaimAction)
			if claim == "" {
				claim = "none"
			}
			g.Units = append(g.Units, unitEffect{ID: unit.ID, Action: string(unit.Action), Ownership: string(unit.Ownership), Claim: claim})
		}
		r.Effects = append(r.Effects, g)
	}
	return r
}

func renderInstall(out io.Writer, result installverb.Result) {
	r := newInstallReport(result)
	var ids []string
	for _, p := range r.Packages {
		ids = append(ids, p.ID)
	}
	what := fmt.Sprintf("%s (%s)", count(r.Counts.Packages, "package", "packages"), strings.Join(ids, ", "))
	switch r.Status {
	case "unchanged":
		fmt.Fprintf(out, "Installation %s already has %s.\n", r.Installation, what)
	case "would-change":
		fmt.Fprintf(out, "Dry run: would install %s into installation %s. Nothing changed.\n", what, r.Installation)
	default:
		fmt.Fprintf(out, "Installed %s into installation %s.\n", what, r.Installation)
	}
}

type checkUnit struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Adapter   string `json:"adapter"`
	Scope     string `json:"scope"`
	Location  string `json:"location,omitempty"`
	Ownership string `json:"ownership"`
	Claim     string `json:"claim,omitempty"`
}

type checkRequirement struct {
	LockDigest    string `json:"lock_digest"`
	Status        string `json:"status"`
	Installation  string `json:"installation,omitempty"`
	ReceiptSHA256 string `json:"receipt_sha256,omitempty"`
}

type checkProblem struct {
	Code        string `json:"code"`
	Unit        string `json:"unit,omitempty"`
	Requirement string `json:"requirement,omitempty"`
	Detail      string `json:"detail"`
}

type checkReport struct {
	Schema        string             `json:"schema"`
	Status        string             `json:"status"`
	Installation  string             `json:"installation"`
	ReceiptSHA256 string             `json:"receipt_sha256,omitempty"`
	Counts        counts             `json:"counts"`
	Requirements  []checkRequirement `json:"requirements"`
	Units         []checkUnit        `json:"units"`
	Problems      []checkProblem     `json:"problems"`
}

func newCheckReport(result checkverb.Result) checkReport {
	status := "findings"
	if result.Exact() {
		status = "exact"
	}
	r := checkReport{Schema: "temper-software-check/v1", Status: status, Installation: result.Installation, ReceiptSHA256: result.ReceiptSHA256,
		Counts:       counts{Packages: result.Packages, Units: len(result.Units), Requirements: len(result.Requirements), Problems: result.ProblemCount()},
		Requirements: []checkRequirement{}, Units: []checkUnit{}, Problems: []checkProblem{}}
	for _, q := range result.Requirements {
		r.Requirements = append(r.Requirements, checkRequirement{LockDigest: q.SoftwareLockDigest, Status: string(q.Status), Installation: q.Installation, ReceiptSHA256: q.ReceiptSHA256})
	}
	for _, u := range result.Units {
		r.Units = append(r.Units, checkUnit{ID: u.ID, Status: string(u.Status), Adapter: u.Adapter, Scope: u.Scope, Location: u.Location, Ownership: string(u.Ownership), Claim: u.Claim})
	}
	for _, f := range result.Findings {
		r.Problems = append(r.Problems, checkProblem{Code: string(f.Code), Unit: f.Unit, Requirement: f.Requirement, Detail: f.Detail})
	}
	return r
}

func renderCheck(out io.Writer, result checkverb.Result) {
	r := newCheckReport(result)
	if r.Status == "exact" {
		fmt.Fprintf(out, "Installation %s matches its lock (%s).\n", r.Installation, count(r.Counts.Packages, "package", "packages"))
		return
	}
	fmt.Fprintf(out, "Installation %s differs from its lock (%s):\n", r.Installation, count(r.Counts.Problems, "problem", "problems"))
	for _, p := range r.Problems {
		where := ""
		if p.Unit != "" {
			where = ", " + p.Unit
		}
		fmt.Fprintf(out, "  - %s (%s%s)\n", p.Detail, p.Code, where)
	}
}

type removeReport struct {
	Schema       string        `json:"schema"`
	Status       string        `json:"status"`
	Installation string        `json:"installation"`
	Counts       counts        `json:"counts"`
	Effects      []effectGroup `json:"effects"`
}

func newRemoveReport(result removeverb.Result) removeReport {
	r := removeReport{Schema: "temper-software-remove/v1", Status: changeStatus(result.Changed, result.DryRun), Installation: result.Installation,
		Counts: counts{Packages: result.Packages, Units: result.Units, Effects: result.Effects, Claims: result.Claims}}
	for _, group := range result.Groups {
		effect := "preserve"
		if group.ChangesProvider() {
			effect = "remove"
		}
		g := effectGroup{ID: group.ID, Effect: effect}
		for _, unit := range group.Units {
			claim := "none"
			if unit.SharedClaim != "" {
				claim = "release"
			}
			g.Units = append(g.Units, unitEffect{ID: unit.ID, Action: string(unit.Action), Ownership: string(unit.Ownership), Claim: claim})
		}
		r.Effects = append(r.Effects, g)
	}
	return r
}

func renderRemove(out io.Writer, result removeverb.Result) {
	r := newRemoveReport(result)
	what := count(r.Counts.Packages, "package", "packages")
	switch r.Status {
	case "unchanged":
		fmt.Fprintf(out, "Nothing to remove from installation %s.\n", r.Installation)
	case "would-change":
		fmt.Fprintf(out, "Dry run: would remove %s from installation %s. Nothing changed.\n", what, r.Installation)
	default:
		fmt.Fprintf(out, "Removed %s from installation %s.\n", what, r.Installation)
	}
}

func writeJSON(out io.Writer, value any) error {
	e := json.NewEncoder(out)
	e.SetIndent("", "  ")
	return e.Encode(value)
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
