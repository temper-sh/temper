package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var commandTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_./,:+=-]+$`)

type wordKind uint8

const (
	knownWordKind wordKind = iota
	dataWordKind
	portWordKind
)

type commandWord struct {
	kind  wordKind
	value string
}

// launchSpec preserves actual process structure while argumentGroups retain
// readable folding in llama-swap's shell command scalar.
type launchSpec struct {
	executable     commandWord
	argumentGroups [][]commandWord
}

func knownWord(value string) commandWord {
	return commandWord{kind: knownWordKind, value: value}
}

func dataWord(value string) commandWord {
	return commandWord{kind: dataWordKind, value: value}
}

func portWord() commandWord {
	return commandWord{kind: portWordKind}
}

func commandFromLaunch(spec launchSpec, runtime Runtime) (Command, error) {
	executable, err := renderCommandWord(spec.executable)
	if err != nil {
		return Command{}, fmt.Errorf("render executable: %w", err)
	}
	first := []string{executable}

	lines := make([]string, 0, len(spec.argumentGroups))
	for index, group := range spec.argumentGroups {
		words := make([]string, 0, len(group))
		for _, word := range group {
			rendered, err := renderCommandWord(word)
			if err != nil {
				return Command{}, fmt.Errorf("render argument group %d: %w", index, err)
			}
			words = append(words, rendered)
		}
		if index == 0 {
			words = append(first, words...)
		}
		if len(words) == 0 {
			return Command{}, fmt.Errorf("argument group %d is empty", index)
		}
		lines = append(lines, strings.Join(words, " "))
	}
	if len(spec.argumentGroups) == 0 {
		lines = append(lines, strings.Join(first, " "))
	}
	if err := validateRuntime(runtime); err != nil {
		return Command{}, err
	}
	return Command{lines: lines, runtime: runtime}, nil
}

var environmentNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func validateRuntime(runtime Runtime) error {
	if runtime.Requirement.Package == "" || runtime.Requirement.RelativeExecutable == "" {
		return errors.New("runtime package and relative executable are required")
	}
	if strings.Contains(runtime.Requirement.RelativeExecutable, `\`) ||
		strings.HasPrefix(runtime.Requirement.RelativeExecutable, "/") ||
		strings.ContainsAny(runtime.Requirement.RelativeExecutable, "\r\n\x00") {
		return errors.New("runtime relative executable is unsafe")
	}
	parts := strings.Split(runtime.Requirement.RelativeExecutable, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errors.New("runtime relative executable is unsafe")
		}
	}
	if runtime.CheckEndpoint == "" || !strings.HasPrefix(runtime.CheckEndpoint, "/") || strings.ContainsAny(runtime.CheckEndpoint, "\r\n\x00") {
		return errors.New("runtime check endpoint is invalid")
	}
	seen := map[string]bool{}
	for _, assignment := range runtime.Environment {
		if !environmentNamePattern.MatchString(assignment.Name) || strings.ContainsAny(assignment.Value, "\r\n\x00") {
			return errors.New("runtime environment assignment is invalid")
		}
		if seen[assignment.Name] {
			return fmt.Errorf("runtime environment repeats %q", assignment.Name)
		}
		seen[assignment.Name] = true
	}
	return nil
}

func renderCommandWord(word commandWord) (string, error) {
	switch word.kind {
	case knownWordKind:
		if !commandTokenPattern.MatchString(word.value) {
			return "", fmt.Errorf("known command token %q is unsafe", word.value)
		}
		return word.value, nil
	case dataWordKind:
		if err := validateCommandData(word.value); err != nil {
			return "", err
		}
		return shellQuote(word.value), nil
	case portWordKind:
		if word.value != "" {
			return "", errors.New("port placeholder cannot carry data")
		}
		return "${PORT}", nil
	default:
		return "", errors.New("command word has an unknown kind")
	}
}

func validateCommandData(value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return errors.New("command data contains an unsupported control character")
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
