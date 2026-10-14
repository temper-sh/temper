// Package clihelp prints command help and suggestions in plain words.
package clihelp

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strings"
)

// Help is what a command says about itself before its flags.
type Help struct {
	Summary  string
	Usage    []string
	Examples []string
}

// Print writes the summary, usage forms, examples and flags. It replaces the
// flag package's raw dump; pass it as the flag set's Usage.
func Print(w io.Writer, h Help, f *flag.FlagSet) {
	if h.Summary != "" {
		fmt.Fprintln(w, h.Summary)
		fmt.Fprintln(w)
	}
	if len(h.Usage) > 0 {
		fmt.Fprintln(w, "Usage:")
		for _, u := range h.Usage {
			fmt.Fprintln(w, "  "+u)
		}
		fmt.Fprintln(w)
	}
	if len(h.Examples) > 0 {
		fmt.Fprintln(w, "Examples:")
		for _, e := range h.Examples {
			fmt.Fprintln(w, "  "+e)
		}
		fmt.Fprintln(w)
	}
	if f == nil {
		return
	}
	type row struct{ name, text string }
	var rows []row
	width := 0
	f.VisitAll(func(o *flag.Flag) {
		// A backquoted word that starts the usage names the value; it is shown
		// after the flag rather than repeated in the description.
		value, text := flag.UnquoteUsage(o)
		named := strings.HasPrefix(o.Usage, "`")
		if named {
			text = strings.TrimSpace(strings.TrimPrefix(text, value))
		}
		name := "--" + o.Name
		if !isBool(o) {
			if !named {
				value = valueName(value)
			}
			name += " " + value
		}
		if o.DefValue != "" && o.DefValue != "false" && o.DefValue != "0" && o.DefValue != "[]" {
			text += " (default " + o.DefValue + ")"
		}
		rows = append(rows, row{name, text})
		width = max(width, len(name))
	})
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "Options:")
	for _, r := range rows {
		fmt.Fprintf(w, "  %-*s  %s\n", width, r.name, r.text)
	}
}

// Usage returns a flag.Usage function that prints h and f's flags to w.
func Usage(w io.Writer, h Help, f *flag.FlagSet) func() {
	return func() { Print(w, h, f) }
}

func isBool(o *flag.Flag) bool {
	b, ok := o.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

func valueName(name string) string {
	switch name {
	case "", "value", "string":
		return "VALUE"
	}
	return "N"
}

// Closest returns the candidate most like id, or "" when none is close
// enough to be a likely typo or shortening.
func Closest(id string, candidates []string) string {
	best, bestScore := "", 0.0
	sorted := append([]string(nil), candidates...)
	sort.Strings(sorted)
	for _, c := range sorted {
		if score := similarity(strings.ToLower(id), strings.ToLower(c)); score > bestScore {
			best, bestScore = c, score
		}
	}
	if bestScore < 0.5 {
		return ""
	}
	return best
}

// similarity scores two IDs from 0 to 1: shared word parts count most, and
// edit distance separates near misses.
func similarity(a, b string) float64 {
	if a == b {
		return 1
	}
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == ' ' })
	}
	pa, pb := split(a), split(b)
	shared := 0
	for _, x := range pa {
		for _, y := range pb {
			if x == y || len(x) > 2 && strings.HasPrefix(y, x) {
				shared++
				break
			}
		}
	}
	parts := 0.0
	if len(pa) > 0 {
		parts = float64(shared) / float64(len(pa))
	}
	edit := 1 - float64(distance(a, b))/float64(max(len(a), len(b)))
	return max(parts, edit)
}

func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// UnknownID explains an ID that is not among ids and suggests the closest
// one and where to list them all.
func UnknownID(kind, id string, ids []string, list string) error {
	message := fmt.Sprintf("%s %q is not in the catalog", kind, id)
	if near := Closest(id, ids); near != "" {
		message += fmt.Sprintf("; did you mean %s?", near)
	}
	if list != "" {
		message += " List them with: " + list
	}
	return errors.New(message)
}

// Network adds what to check when err comes from the network.
func Network(what string, err error) error {
	var u *url.Error
	var op *net.OpError
	var dns *net.DNSError
	if errors.As(err, &u) || errors.As(err, &op) || errors.As(err, &dns) {
		return fmt.Errorf("could not %s: %w; check your network connection or proxy settings, then try again", what, err)
	}
	return err
}
