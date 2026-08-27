// Package session owns the append-only local record of one explicitly
// consented promoted experiment. Mutations are pure until Store.Commit stages
// and atomically replaces the one canonical document.
package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/fieldkit/catalog"
)

const SchemaV1 = "field-kit-session/v1"

var sessionIDPattern = regexp.MustCompile("^[a-z0-9]+(?:[.-][a-z0-9]+)*$")

type Document struct {
	Schema      string             `json:"schema"`
	ID          string             `json:"id"`
	State       string             `json:"state"`
	Catalog     CatalogIdentity    `json:"catalog"`
	Machine     FileIdentity       `json:"machine"`
	Experiment  ExperimentIdentity `json:"experiment"`
	Consent     Consent            `json:"consent"`
	Attempts    []Attempt          `json:"attempts"`
	Deviations  []Note             `json:"deviations"`
	Conclusions []Note             `json:"conclusions"`
	Report      *FileIdentity      `json:"report"`
}

type CatalogIdentity struct {
	Revision int    `json:"revision"`
	SHA256   string `json:"sha256"`
}

type FileIdentity struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type ExperimentIdentity struct {
	ID                string `json:"id"`
	Revision          int    `json:"revision"`
	PackageSHA256     string `json:"package_sha256"`
	PromptSHA256      string `json:"prompt_sha256"`
	PromotionID       string `json:"promotion_id"`
	PromotionRevision int    `json:"promotion_revision"`
	PromotionSHA256   string `json:"promotion_sha256"`
}

type Consent struct {
	Decision         string `json:"decision"`
	At               string `json:"at"`
	DisclosureSHA256 string `json:"disclosure_sha256"`
	ExperimentID     string `json:"experiment_id"`
	Revision         int    `json:"revision"`
}

type Attempt struct {
	Number        int              `json:"number"`
	StartedAt     string           `json:"started_at"`
	BindingSHA256 string           `json:"temper_binding_sha256"`
	Parameters    []ParameterValue `json:"parameters"`
	Observations  []Observation    `json:"observations"`
	Decisions     []Decision       `json:"decisions"`
}

type ParameterValue struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type Observation struct {
	At    string `json:"at"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Decision struct {
	At     string `json:"at"`
	Action string `json:"action"`
	Reason string `json:"reason"`
}

type Note struct {
	At    string `json:"at"`
	Value string `json:"value"`
}

func New(id string, snapshot catalog.Snapshot, entry catalog.Entry, machineName, machineSHA, disclosureSHA, at string) (Document, error) {
	document := Document{
		Schema: SchemaV1, ID: id, State: "consented",
		Catalog: CatalogIdentity{Revision: snapshot.Document.Revision, SHA256: snapshot.SHA256},
		Machine: FileIdentity{Name: machineName, SHA256: machineSHA},
		Experiment: ExperimentIdentity{
			ID: entry.Package.ID, Revision: entry.Package.Revision, PackageSHA256: entry.Reference.PackageSHA256,
			PromptSHA256: entry.Package.Prompt.SHA256,
			PromotionID:  entry.Package.Origin.PromotionID, PromotionRevision: entry.Package.Origin.PromotionRevision,
			PromotionSHA256: entry.Package.Origin.PromotionSHA256,
		},
		Consent: Consent{
			Decision: "yes", At: at, DisclosureSHA256: disclosureSHA,
			ExperimentID: entry.Package.ID, Revision: entry.Package.Revision,
		},
		Attempts: []Attempt{}, Deviations: []Note{}, Conclusions: []Note{},
	}
	if err := document.Validate(entry.Package); err != nil {
		return Document{}, err
	}
	return document, nil
}

func (d Document) StartAttempt(promoted catalog.Package, at, bindingSHA string, values map[string]string) (Document, error) {
	if d.State == "complete" || d.State == "stopped" {
		return Document{}, fmt.Errorf("session is %s", d.State)
	}
	if int64(len(d.Attempts)) >= promoted.Bounds.MaximumAttempts {
		return Document{}, errors.New("experiment maximum attempt count reached; renewed package or consent is required")
	}
	parameters, err := validateParameters(promoted.Parameters, values)
	if err != nil {
		return Document{}, err
	}
	if !sha256(bindingSHA) {
		return Document{}, errors.New("attempt Temper binding hash is invalid")
	}
	next := clone(d)
	next.State = "running"
	next.Attempts = append(next.Attempts, Attempt{
		Number: len(next.Attempts) + 1, StartedAt: at, BindingSHA256: bindingSHA,
		Parameters: parameters, Observations: []Observation{}, Decisions: []Decision{},
	})
	if err := next.Validate(promoted); err != nil {
		return Document{}, err
	}
	return next, nil
}

func (d Document) Observe(promoted catalog.Package, attempt int, observation Observation) (Document, error) {
	if !sessionIDPattern.MatchString(observation.Kind) || strings.TrimSpace(observation.Value) == "" || !validTime(observation.At) {
		return Document{}, errors.New("observation requires a stable kind, value, and RFC3339 time")
	}
	next := clone(d)
	target, err := attemptAt(&next, attempt)
	if err != nil {
		return Document{}, err
	}
	target.Observations = append(target.Observations, observation)
	if err := next.Validate(promoted); err != nil {
		return Document{}, err
	}
	return next, nil
}

func (d Document) Decide(promoted catalog.Package, attempt int, decision Decision) (Document, error) {
	if decision.Action != "continue" && decision.Action != "adapt" && decision.Action != "stop" {
		return Document{}, fmt.Errorf("decision action %q is unsupported", decision.Action)
	}
	if promoted.Kind == "fixed" && decision.Action == "adapt" {
		return Document{}, errors.New("fixed experiment cannot adapt")
	}
	if decision.Action == "adapt" && int64(len(d.Attempts)) >= promoted.Bounds.MaximumAttempts {
		return Document{}, errors.New("adaptation would exceed the promoted attempt bound; renewed consent is required")
	}
	if !validTime(decision.At) || strings.TrimSpace(decision.Reason) == "" {
		return Document{}, errors.New("decision requires an RFC3339 time and reason")
	}
	next := clone(d)
	target, err := attemptAt(&next, attempt)
	if err != nil {
		return Document{}, err
	}
	target.Decisions = append(target.Decisions, decision)
	if decision.Action == "stop" {
		next.State = "stopped"
	}
	if err := next.Validate(promoted); err != nil {
		return Document{}, err
	}
	return next, nil
}

func (d Document) AddNote(promoted catalog.Package, class, at, value string) (Document, error) {
	if !validTime(at) || strings.TrimSpace(value) == "" {
		return Document{}, errors.New("note requires an RFC3339 time and value")
	}
	next := clone(d)
	note := Note{At: at, Value: value}
	switch class {
	case "deviation":
		next.Deviations = append(next.Deviations, note)
	case "conclusion":
		next.Conclusions = append(next.Conclusions, note)
	default:
		return Document{}, fmt.Errorf("note class %q is unsupported", class)
	}
	if err := next.Validate(promoted); err != nil {
		return Document{}, err
	}
	return next, nil
}

func (d Document) Finish(promoted catalog.Package, reportName, reportSHA string) (Document, error) {
	if len(d.Attempts) == 0 {
		return Document{}, errors.New("session cannot finish without an attempt")
	}
	if !safeName(reportName) || !sha256(reportSHA) {
		return Document{}, errors.New("report identity is invalid")
	}
	next := clone(d)
	next.State = "complete"
	next.Report = &FileIdentity{Name: reportName, SHA256: reportSHA}
	if err := next.Validate(promoted); err != nil {
		return Document{}, err
	}
	return next, nil
}

func (d Document) Validate(promoted catalog.Package) error {
	if d.Schema != SchemaV1 || !sessionIDPattern.MatchString(d.ID) {
		return errors.New("session schema or id is invalid")
	}
	switch d.State {
	case "consented", "running", "stopped", "complete":
	default:
		return fmt.Errorf("session state %q is unsupported", d.State)
	}
	if d.Catalog.Revision <= 0 || !sha256(d.Catalog.SHA256) || !safeName(d.Machine.Name) || !sha256(d.Machine.SHA256) {
		return errors.New("session catalog or machine identity is invalid")
	}
	if d.Experiment.ID != promoted.ID || d.Experiment.Revision != promoted.Revision ||
		d.Experiment.PromptSHA256 != promoted.Prompt.SHA256 ||
		d.Experiment.PromotionID != promoted.Origin.PromotionID ||
		d.Experiment.PromotionRevision != promoted.Origin.PromotionRevision ||
		d.Experiment.PromotionSHA256 != promoted.Origin.PromotionSHA256 ||
		!sha256(d.Experiment.PackageSHA256) {
		return errors.New("session experiment identity differs from promoted package")
	}
	if d.Consent.Decision != "yes" || !validTime(d.Consent.At) || !sha256(d.Consent.DisclosureSHA256) ||
		d.Consent.ExperimentID != promoted.ID || d.Consent.Revision != promoted.Revision {
		return errors.New("session lacks exact affirmative experiment consent")
	}
	if int64(len(d.Attempts)) > promoted.Bounds.MaximumAttempts {
		return errors.New("session exceeds promoted attempt bound")
	}
	for index, attempt := range d.Attempts {
		if attempt.Number != index+1 || !validTime(attempt.StartedAt) || !sha256(attempt.BindingSHA256) {
			return fmt.Errorf("attempt %d identity is invalid", index+1)
		}
		values := map[string]string{}
		previous := ""
		for _, value := range attempt.Parameters {
			if value.ID <= previous {
				return fmt.Errorf("attempt %d parameters are not unique and sorted", attempt.Number)
			}
			previous = value.ID
			values[value.ID] = value.Value
		}
		if _, err := validateParameters(promoted.Parameters, values); err != nil {
			return fmt.Errorf("attempt %d: %w", attempt.Number, err)
		}
		for _, observation := range attempt.Observations {
			if !validTime(observation.At) || !sessionIDPattern.MatchString(observation.Kind) || strings.TrimSpace(observation.Value) == "" {
				return fmt.Errorf("attempt %d has invalid observation", attempt.Number)
			}
		}
		for _, decision := range attempt.Decisions {
			if !validTime(decision.At) || strings.TrimSpace(decision.Reason) == "" ||
				(decision.Action != "continue" && decision.Action != "adapt" && decision.Action != "stop") ||
				(promoted.Kind == "fixed" && decision.Action == "adapt") {
				return fmt.Errorf("attempt %d has invalid decision", attempt.Number)
			}
		}
	}
	for _, notes := range [][]Note{d.Deviations, d.Conclusions} {
		for _, note := range notes {
			if !validTime(note.At) || strings.TrimSpace(note.Value) == "" {
				return errors.New("session has invalid note")
			}
		}
	}
	if d.State == "complete" {
		if d.Report == nil || !safeName(d.Report.Name) || !sha256(d.Report.SHA256) {
			return errors.New("complete session lacks report identity")
		}
	} else if d.Report != nil {
		return errors.New("incomplete session cannot carry final report identity")
	}
	return nil
}

func Marshal(document Document, promoted catalog.Package) ([]byte, error) {
	if err := document.Validate(promoted); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func Parse(data []byte, promoted catalog.Package) (Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode session: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Document{}, errors.New("decode session: expected exactly one JSON value")
	}
	canonical, err := Marshal(document, promoted)
	if err != nil {
		return Document{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Document{}, errors.New("session bytes are not canonical")
	}
	return document, nil
}

type Store struct {
	Path   string
	Data   []byte
	Exists bool
}

func ReadStore(path string) (Store, error) {
	if path == "" {
		return Store{}, errors.New("session path is required")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Store{Path: path}, nil
	}
	if err != nil {
		return Store{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		return Store{}, errors.New("session must be a regular file without symlink indirection")
	}
	data, err := os.ReadFile(path)
	return Store{Path: path, Data: data, Exists: true}, err
}

func (s Store) Commit(ctx context.Context, candidate []byte) error {
	if s.Path == "" {
		return errors.New("session store has no path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(s.Path)
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect session directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return errors.New("session directory must already exist and cannot be a symlink")
	}
	current, err := ReadStore(s.Path)
	if err != nil {
		return fmt.Errorf("verify session before commit: %w", err)
	}
	if current.Exists != s.Exists || !bytes.Equal(current.Data, s.Data) {
		return errors.New("session changed concurrently; rerun command")
	}
	stage, err := os.CreateTemp(directory, ".field-kit-session-*")
	if err != nil {
		return fmt.Errorf("stage session: %w", err)
	}
	stagePath := stage.Name()
	defer os.Remove(stagePath)
	if err := stage.Chmod(0o644); err != nil {
		stage.Close()
		return err
	}
	if _, err := stage.Write(candidate); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err = ReadStore(s.Path)
	if err != nil {
		return err
	}
	if current.Exists != s.Exists || !bytes.Equal(current.Data, s.Data) {
		return errors.New("session changed concurrently; rerun command")
	}
	if err := os.Rename(stagePath, s.Path); err != nil {
		return fmt.Errorf("commit session: %w", err)
	}
	return syncDirectory(directory)
}

func validateParameters(definitions []catalog.Parameter, values map[string]string) ([]ParameterValue, error) {
	known := map[string]catalog.Parameter{}
	for _, definition := range definitions {
		known[definition.ID] = definition
	}
	for id := range values {
		if _, ok := known[id]; !ok {
			return nil, fmt.Errorf("unknown parameter %q", id)
		}
	}
	result := make([]ParameterValue, 0, len(definitions))
	for _, definition := range definitions {
		value, present := values[definition.ID]
		if !present {
			if definition.Required {
				return nil, fmt.Errorf("required parameter %q is absent", definition.ID)
			}
			continue
		}
		switch definition.Kind {
		case "fixed":
			if value != definition.Fixed {
				return nil, fmt.Errorf("parameter %q must equal promoted value %q", definition.ID, definition.Fixed)
			}
		case "integer":
			integer, err := strconv.ParseInt(value, 10, 64)
			if err != nil || integer < definition.Minimum || integer > definition.Maximum {
				return nil, fmt.Errorf("parameter %q is outside promoted integer bounds", definition.ID)
			}
		case "enum":
			index := sort.SearchStrings(definition.Values, value)
			if index == len(definition.Values) || definition.Values[index] != value {
				return nil, fmt.Errorf("parameter %q is outside promoted enum values", definition.ID)
			}
		default:
			return nil, fmt.Errorf("parameter %q kind is unsupported", definition.ID)
		}
		result = append(result, ParameterValue{ID: definition.ID, Value: value})
	}
	return result, nil
}

func attemptAt(document *Document, number int) (*Attempt, error) {
	if number <= 0 || number > len(document.Attempts) {
		return nil, fmt.Errorf("attempt %d does not exist", number)
	}
	return &document.Attempts[number-1], nil
}

func clone(document Document) Document {
	data, _ := json.Marshal(document)
	var result Document
	_ = json.Unmarshal(data, &result)
	return result
}

func validTime(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.Format(time.RFC3339) == value
}

func sha256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func safeName(value string) bool {
	return value != "" && filepath.Base(value) == value && value != "."
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
