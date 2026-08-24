package qualification

import (
	"fmt"
	"math/big"
	"regexp"
)

var canonicalDecimalPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.[0-9]*[1-9])?$`)

const (
	PerformanceValueInteger         = "integer"
	PerformanceValueDecimal         = "decimal"
	PerformanceValueDurationMillis  = "duration-millis"
	PerformanceValueSuccessFraction = "success-fraction"
)

// RuntimePerformance keeps all performance axes explicit. An omitted
// measurement is data, not a zero value or an implied passing result.
type RuntimePerformance struct {
	TaskSuccess        PerformanceAxis `yaml:"task_success"`
	Regressions        PerformanceAxis `yaml:"regressions"`
	TaskTimeAndToolUse PerformanceAxis `yaml:"task_time_and_tool_use"`
	Throughput         PerformanceAxis `yaml:"throughput"`
	Context            PerformanceAxis `yaml:"context"`
	Memory             PerformanceAxis `yaml:"memory"`
	CacheAndReplay     PerformanceAxis `yaml:"cache_and_replay"`
}

type PerformanceAxis struct {
	State        string                   `yaml:"state"`
	Reason       string                   `yaml:"reason,omitempty"`
	Observations []PerformanceObservation `yaml:"observations,omitempty"`
}

type PerformanceObservation struct {
	Metric     string           `yaml:"metric"`
	Value      PerformanceValue `yaml:"value"`
	Definition string           `yaml:"definition"`
	Witness    string           `yaml:"witness"`
}

type PerformanceValue struct {
	Kind            string                      `yaml:"kind"`
	Integer         *uint64                     `yaml:"integer,omitempty"`
	Decimal         string                      `yaml:"decimal,omitempty"`
	DurationMillis  *uint64                     `yaml:"duration_millis,omitempty"`
	SuccessFraction *PerformanceSuccessFraction `yaml:"success_fraction,omitempty"`
}

type PerformanceSuccessFraction struct {
	Successes uint64 `yaml:"successes"`
	Attempts  uint64 `yaml:"attempts"`
}

type performanceAxisDefinition struct {
	name    string
	axis    PerformanceAxis
	metrics map[string]string
}

func validateRuntimePerformance(performance RuntimePerformance, evidence []ProfileEvidence, problem func(string, ...any)) {
	evidenceIDs := map[string]bool{}
	for _, item := range evidence {
		evidenceIDs[item.ID] = true
	}

	axes := []performanceAxisDefinition{
		{name: "cache_and_replay", axis: performance.CacheAndReplay, metrics: map[string]string{
			"cache-hit-fraction":     PerformanceValueSuccessFraction,
			"history-tokens":         PerformanceValueInteger,
			"replayed-prompt-tokens": PerformanceValueInteger,
		}},
		{name: "context", axis: performance.Context, metrics: map[string]string{
			"qualified-task-context-tokens": PerformanceValueInteger,
			"raw-window-tokens":             PerformanceValueInteger,
		}},
		{name: "memory", axis: performance.Memory, metrics: map[string]string{
			"full-slot-mib": PerformanceValueInteger,
			"peak-mib":      PerformanceValueInteger,
			"resident-mib":  PerformanceValueInteger,
		}},
		{name: "regressions", axis: performance.Regressions, metrics: map[string]string{
			"known-bad-tasks":     PerformanceValueInteger,
			"new-regressions":     PerformanceValueInteger,
			"retained-good-tasks": PerformanceValueInteger,
		}},
		{name: "task_success", axis: performance.TaskSuccess, metrics: map[string]string{
			"first-attempt-task-success": PerformanceValueSuccessFraction,
			"overall-task-success":       PerformanceValueSuccessFraction,
		}},
		{name: "task_time_and_tool_use", axis: performance.TaskTimeAndToolUse, metrics: map[string]string{
			"completed-task-wall-time": PerformanceValueDurationMillis,
			"recovery-count":           PerformanceValueInteger,
			"successful-tool-calls":    PerformanceValueInteger,
			"unnecessary-tool-calls":   PerformanceValueInteger,
		}},
		{name: "throughput", axis: performance.Throughput, metrics: map[string]string{
			"decode-tokens-per-second":  PerformanceValueDecimal,
			"prefill-tokens-per-second": PerformanceValueDecimal,
		}},
	}

	for _, definition := range axes {
		validatePerformanceAxis(definition, evidenceIDs, problem)
	}
}

func validatePerformanceAxis(definition performanceAxisDefinition, evidenceIDs map[string]bool, problem func(string, ...any)) {
	location := "spec.performance." + definition.name
	axis := definition.axis
	switch axis.State {
	case "measured":
		if axis.Reason != "" {
			problem("%s.reason must be absent when state is measured", location)
		}
		if len(axis.Observations) == 0 {
			problem("%s.observations must not be empty when state is measured", location)
		}
	case "not-applicable", "unmeasured":
		validateLine(location+".reason", axis.Reason, problem)
		if len(axis.Observations) != 0 {
			problem("%s.observations must be absent when state is %s", location, axis.State)
		}
		return
	default:
		problem("%s.state %q must be measured, not-applicable, or unmeasured", location, axis.State)
		return
	}

	previous := ""
	for index, observation := range axis.Observations {
		observationLocation := fmt.Sprintf("%s.observations[%d]", location, index)
		wantKind, ok := definition.metrics[observation.Metric]
		if !ok {
			problem("%s.metric %q is not supported for %s", observationLocation, observation.Metric, definition.name)
		}
		validateLine(observationLocation+".definition", observation.Definition, problem)
		if !stableIDPattern.MatchString(observation.Witness) {
			problem("%s.witness %q is not a lowercase stable id", observationLocation, observation.Witness)
		} else if !evidenceIDs[observation.Witness] {
			problem("%s.witness references unknown evidence id %q", observationLocation, observation.Witness)
		}
		exactIdentity := observation.Metric + "\x00" + observation.Witness
		if index > 0 && exactIdentity <= previous {
			problem("%s.observations must be unique and sorted by metric and witness", location)
		}
		previous = exactIdentity
		validatePerformanceValue(observationLocation+".value", observation.Value, wantKind, problem)
	}
}

func validatePerformanceValue(location string, value PerformanceValue, wantKind string, problem func(string, ...any)) {
	if value.Kind != wantKind {
		problem("%s.kind is %q, want %q for this metric", location, value.Kind, wantKind)
	}
	present := 0
	if value.Integer != nil {
		present++
	}
	if value.Decimal != "" {
		present++
	}
	if value.DurationMillis != nil {
		present++
	}
	if value.SuccessFraction != nil {
		present++
	}
	if present != 1 {
		problem("%s must contain exactly one typed value", location)
	}

	switch value.Kind {
	case PerformanceValueInteger:
		if value.Integer == nil {
			problem("%s.integer is required for integer values", location)
		}
	case PerformanceValueDecimal:
		validateCanonicalNonnegativeDecimal(location+".decimal", value.Decimal, problem)
	case PerformanceValueDurationMillis:
		if value.DurationMillis == nil {
			problem("%s.duration_millis is required for duration-millis values", location)
		}
	case PerformanceValueSuccessFraction:
		if value.SuccessFraction == nil {
			problem("%s.success_fraction is required for success-fraction values", location)
		} else {
			if value.SuccessFraction.Attempts == 0 {
				problem("%s.success_fraction.attempts must be greater than zero", location)
			}
			if value.SuccessFraction.Successes > value.SuccessFraction.Attempts {
				problem("%s.success_fraction.successes must not exceed attempts", location)
			}
		}
	default:
		problem("%s.kind %q is not supported", location, value.Kind)
	}
}

func validateCanonicalDecimal(location, value, minimum, maximum string, problem func(string, ...any)) {
	parsed := parseCanonicalDecimal(location, value, problem)
	if parsed == nil {
		return
	}
	min, _ := new(big.Rat).SetString(minimum)
	max, _ := new(big.Rat).SetString(maximum)
	if parsed.Cmp(min) < 0 || parsed.Cmp(max) > 0 {
		problem("%s must be between %s and %s", location, minimum, maximum)
	}
}

func validateCanonicalNonnegativeDecimal(location, value string, problem func(string, ...any)) {
	parseCanonicalDecimal(location, value, problem)
}

func parseCanonicalDecimal(location, value string, problem func(string, ...any)) *big.Rat {
	if !canonicalDecimalPattern.MatchString(value) {
		problem("%s %q must be a canonical nonnegative decimal string", location, value)
		return nil
	}
	parsed, ok := new(big.Rat).SetString(value)
	if !ok {
		problem("%s %q must be a decimal number", location, value)
		return nil
	}
	return parsed
}
