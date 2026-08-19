// Command temper is the Temper CLI: it transforms artifacts (manifest +
// lock -> rendered configs) and controls the serving posture; it never
// sequences an install. cmd/ is the composition root — construction and
// wiring only; verbs live under internal/ as they land (docs/PLAN.md M1).
package main

import (
	"fmt"
	"os"
)

const version = "0.0.0-dev"

// Verbs are declared here as they are designed; each moves from this list
// to a real dispatch entry only after its contract in docs/PLAN.md §2 is
// reviewed. An unknown verb and a known-but-unbuilt verb both refuse
// loudly — never a silent stub.
var plannedVerbs = []string{"apply", "update", "check"}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "--version":
		fmt.Println("temper " + version)
	case "help", "--help", "-h":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "temper: %q is not implemented yet\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w *os.File) {
	fmt.Fprintf(w, "temper %s — pre-release scaffold; no verb is implemented yet\n", version)
	fmt.Fprintln(w, "\nplanned verbs (docs/PLAN.md):")
	for _, v := range plannedVerbs {
		fmt.Fprintf(w, "  %s\n", v)
	}
}
