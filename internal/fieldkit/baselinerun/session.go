// Package baselinerun owns the resumable local execution envelope for one
// explicitly consented immutable baseline.
package baselinerun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/fieldkit/baseline"
)

const (
	SessionSchemaV1 = "temper-field-kit-baseline-session/v1"
	MarkerSchemaV1  = "temper-field-kit-owned-root/v1"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Document struct {
	Schema     string           `json:"schema"`
	ID         string           `json:"id"`
	State      string           `json:"state"`
	Catalog    CatalogIdentity  `json:"catalog"`
	Machine    FileIdentity     `json:"machine"`
	Baseline   BaselineIdentity `json:"baseline"`
	Temper     FileIdentity     `json:"temper"`
	Consent    Consent          `json:"consent"`
	Workspace  Workspace        `json:"workspace"`
	Outcome    string           `json:"outcome"`
	Stages     []Stage          `json:"stages"`
	Generation string           `json:"generation"`
	Binding    *FileIdentity    `json:"binding"`
	Protocol   *FileIdentity    `json:"protocol_report"`
	Report     *FileIdentity    `json:"report"`
}

type CatalogIdentity struct {
	Revision int    `json:"revision"`
	SHA256   string `json:"sha256"`
}

type FileIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type BaselineIdentity struct {
	ID            string `json:"id"`
	Revision      int    `json:"revision"`
	PackageSHA256 string `json:"package_sha256"`
	SourceSHA256  string `json:"source_sha256"`
}

type Consent struct {
	Decision         string `json:"decision"`
	At               string `json:"at"`
	DisclosureSHA256 string `json:"disclosure_sha256"`
	BaselineID       string `json:"baseline_id"`
	Revision         int    `json:"revision"`
}

type Workspace struct {
	Root         string       `json:"root"`
	MarkerSHA256 string       `json:"marker_sha256"`
	PackageRoot  string       `json:"package_root"`
	SoftwareLock FileIdentity `json:"software_lock"`
}

type Stage struct {
	ID          string        `json:"id"`
	Operation   string        `json:"operation"`
	State       string        `json:"state"`
	CompletedAt string        `json:"completed_at"`
	Evidence    *FileIdentity `json:"evidence"`
}

type Marker struct {
	Schema           string `json:"schema"`
	SessionID        string `json:"session_id"`
	BaselineID       string `json:"baseline_id"`
	BaselineRevision int    `json:"baseline_revision"`
	Root             string `json:"root"`
}

func New(id string, snapshot baseline.Snapshot, entry baseline.Entry, machine, temper FileIdentity, root, outcome, disclosureSHA, softwareLockSHA, at string) (Document, Marker, error) {
	if outcome != "keep" && outcome != "restore" {
		return Document{}, Marker{}, errors.New("baseline outcome must be keep or restore")
	}
	marker := Marker{Schema: MarkerSchemaV1, SessionID: id, BaselineID: entry.Package.ID, BaselineRevision: entry.Package.Revision, Root: root}
	markerData, err := MarshalMarker(marker)
	if err != nil {
		return Document{}, Marker{}, err
	}
	document := Document{
		Schema: SessionSchemaV1, ID: id, State: "consented",
		Catalog:   CatalogIdentity{Revision: snapshot.Document.Revision, SHA256: snapshot.SHA256},
		Machine:   machine,
		Baseline:  BaselineIdentity{ID: entry.Package.ID, Revision: entry.Package.Revision, PackageSHA256: entry.Reference.PackageSHA256, SourceSHA256: entry.Package.Origin.SourceSHA256},
		Temper:    temper,
		Consent:   Consent{Decision: "yes", At: at, DisclosureSHA256: disclosureSHA, BaselineID: entry.Package.ID, Revision: entry.Package.Revision},
		Workspace: Workspace{Root: root, MarkerSHA256: Digest(markerData), PackageRoot: filepath.Join(root, "field-kit", "package"), SoftwareLock: FileIdentity{Path: filepath.Join(root, "field-kit", "software.lock.yaml"), SHA256: softwareLockSHA}},
		Outcome:   outcome, Stages: make([]Stage, len(entry.Package.Mechanics.Stages)),
	}
	for index, stage := range entry.Package.Mechanics.Stages {
		document.Stages[index] = Stage{ID: stage.ID, Operation: stage.Operation, State: "pending"}
	}
	if err := document.Validate(entry.Package); err != nil {
		return Document{}, Marker{}, err
	}
	return document, marker, nil
}

func (d Document) Next() (int, Stage, bool) {
	for index, stage := range d.Stages {
		if stage.State != "succeeded" {
			return index, stage, true
		}
	}
	return 0, Stage{}, false
}

func (d Document) CompleteStage(promoted baseline.Package, stageID, at string, evidence FileIdentity, generation string, binding, protocol *FileIdentity) (Document, error) {
	index, stage, ok := d.Next()
	if !ok {
		return Document{}, errors.New("all baseline stages are already complete")
	}
	if stage.ID != stageID {
		return Document{}, fmt.Errorf("next baseline stage is %q, not %q", stage.ID, stageID)
	}
	if !validTime(at) || !validFileIdentity(evidence) {
		return Document{}, errors.New("stage completion requires an RFC3339 time and evidence identity")
	}
	next := clone(d)
	next.State = "running"
	next.Stages[index].State = "succeeded"
	next.Stages[index].CompletedAt = at
	next.Stages[index].Evidence = &evidence
	switch stage.Operation {
	case "config-apply":
		if !sha256Pattern.MatchString(generation) {
			return Document{}, errors.New("config-apply completion requires an exact generation")
		}
		next.Generation = generation
	case "material-bind":
		if binding == nil || !validFileIdentity(*binding) || next.Generation == "" {
			return Document{}, errors.New("material-bind completion requires an exact binding after apply")
		}
		copy := *binding
		next.Binding = &copy
	case "live-protocol":
		if protocol == nil || !validFileIdentity(*protocol) || next.Binding == nil {
			return Document{}, errors.New("live-protocol completion requires a report after material binding")
		}
		copy := *protocol
		next.Protocol = &copy
	case "outcome":
		if next.Protocol == nil {
			return Document{}, errors.New("outcome cannot complete before the live protocol")
		}
	}
	if err := next.Validate(promoted); err != nil {
		return Document{}, err
	}
	return next, nil
}

func (d Document) Finish(promoted baseline.Package, report FileIdentity) (Document, error) {
	if _, _, pending := d.Next(); pending {
		return Document{}, errors.New("baseline session cannot finish with pending stages")
	}
	if d.Protocol == nil || !validFileIdentity(report) {
		return Document{}, errors.New("baseline finish requires protocol and final report identities")
	}
	next := clone(d)
	next.State = "complete"
	next.Report = &report
	if err := next.Validate(promoted); err != nil {
		return Document{}, err
	}
	return next, nil
}

func (d Document) Validate(promoted baseline.Package) error {
	if d.Schema != SessionSchemaV1 || !idPattern.MatchString(d.ID) {
		return errors.New("baseline session schema or id is invalid")
	}
	if d.State != "consented" && d.State != "running" && d.State != "complete" {
		return fmt.Errorf("baseline session state %q is unsupported", d.State)
	}
	if d.Catalog.Revision <= 0 || !sha256Pattern.MatchString(d.Catalog.SHA256) || !validFileIdentity(d.Machine) || !validFileIdentity(d.Temper) {
		return errors.New("baseline session catalog, machine, or Temper identity is invalid")
	}
	if d.Baseline.ID != promoted.ID || d.Baseline.Revision != promoted.Revision || !sha256Pattern.MatchString(d.Baseline.PackageSHA256) || d.Baseline.SourceSHA256 != promoted.Origin.SourceSHA256 {
		return errors.New("baseline session package identity differs from the immutable package")
	}
	if d.Consent.Decision != "yes" || !validTime(d.Consent.At) || !sha256Pattern.MatchString(d.Consent.DisclosureSHA256) || d.Consent.BaselineID != promoted.ID || d.Consent.Revision != promoted.Revision {
		return errors.New("baseline session lacks exact affirmative consent")
	}
	if d.Outcome != "keep" && d.Outcome != "restore" {
		return errors.New("baseline session outcome is invalid")
	}
	if !filepath.IsAbs(d.Workspace.Root) || filepath.Clean(d.Workspace.Root) != d.Workspace.Root || d.Workspace.Root == string(filepath.Separator) || !sha256Pattern.MatchString(d.Workspace.MarkerSHA256) || d.Workspace.PackageRoot != filepath.Join(d.Workspace.Root, "field-kit", "package") || !validFileIdentity(d.Workspace.SoftwareLock) || d.Workspace.SoftwareLock.Path != filepath.Join(d.Workspace.Root, "field-kit", "software.lock.yaml") {
		return errors.New("baseline workspace identity is invalid")
	}
	if len(d.Stages) != len(promoted.Mechanics.Stages) {
		return errors.New("baseline session stage count differs from package")
	}
	pendingSeen := false
	for index, stage := range d.Stages {
		declared := promoted.Mechanics.Stages[index]
		if stage.ID != declared.ID || stage.Operation != declared.Operation || (stage.State != "pending" && stage.State != "succeeded") {
			return fmt.Errorf("baseline session stage %d differs from package", index+1)
		}
		if stage.State == "pending" {
			pendingSeen = true
			if stage.CompletedAt != "" || stage.Evidence != nil {
				return errors.New("pending baseline stage carries completion evidence")
			}
		} else {
			if pendingSeen || !validTime(stage.CompletedAt) || stage.Evidence == nil || !validFileIdentity(*stage.Evidence) {
				return errors.New("baseline stages must complete in order with exact evidence")
			}
		}
	}
	if d.Generation != "" && !sha256Pattern.MatchString(d.Generation) {
		return errors.New("baseline generation is invalid")
	}
	if d.Binding != nil && !validFileIdentity(*d.Binding) {
		return errors.New("baseline binding identity is invalid")
	}
	if d.Protocol != nil && !validFileIdentity(*d.Protocol) {
		return errors.New("baseline protocol identity is invalid")
	}
	if d.State == "complete" {
		if _, _, pending := d.Next(); pending || d.Report == nil || !validFileIdentity(*d.Report) {
			return errors.New("complete baseline session has pending work or no report")
		}
	} else if d.Report != nil {
		return errors.New("incomplete baseline session cannot carry a final report")
	}
	return nil
}

func Marshal(document Document, promoted baseline.Package) ([]byte, error) {
	if err := document.Validate(promoted); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func Parse(data []byte, promoted baseline.Package) (Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Document{}, errors.New("baseline session expects exactly one JSON value")
	}
	canonical, err := Marshal(document, promoted)
	if err != nil {
		return Document{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Document{}, errors.New("baseline session bytes are not canonical")
	}
	return document, nil
}

// Identify returns only the immutable baseline selector carried by a session.
// Callers must still use Parse with the selected package before trusting any
// other session field or performing an effect.
func Identify(data []byte) (BaselineIdentity, error) {
	var envelope struct {
		Schema   string           `json:"schema"`
		Baseline BaselineIdentity `json:"baseline"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return BaselineIdentity{}, err
	}
	if envelope.Schema != SessionSchemaV1 || !idPattern.MatchString(envelope.Baseline.ID) || envelope.Baseline.Revision <= 0 {
		return BaselineIdentity{}, errors.New("baseline session identity is invalid")
	}
	return envelope.Baseline, nil
}

func MarshalMarker(marker Marker) ([]byte, error) {
	if marker.Schema != MarkerSchemaV1 || !idPattern.MatchString(marker.SessionID) || !idPattern.MatchString(marker.BaselineID) || marker.BaselineRevision <= 0 || !filepath.IsAbs(marker.Root) || filepath.Clean(marker.Root) != marker.Root || marker.Root == string(filepath.Separator) {
		return nil, errors.New("Field Kit root marker is invalid")
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func VerifyMarker(root string, expectedSHA string) error {
	data, err := readRegular(filepath.Join(root, ".temper-field-kit-owner.json"))
	if err != nil {
		return fmt.Errorf("read Field Kit root marker: %w", err)
	}
	if Digest(data) != expectedSHA {
		return errors.New("Field Kit root marker differs from the consented session")
	}
	var marker Marker
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return err
	}
	canonical, err := MarshalMarker(marker)
	if err != nil || !bytes.Equal(data, canonical) || marker.Root != root {
		return errors.New("Field Kit root marker is not canonical for this root")
	}
	return nil
}

type Store struct {
	Path   string
	Data   []byte
	Exists bool
}

func ReadStore(path string) (Store, error) {
	if path == "" {
		return Store{}, errors.New("baseline session path is required")
	}
	data, err := readRegular(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Store{Path: path}, nil
	}
	if err != nil {
		return Store{}, err
	}
	return Store{Path: path, Data: data, Exists: true}, nil
}

func (s Store) Commit(ctx context.Context, candidate []byte) error {
	if s.Path == "" {
		return errors.New("baseline session store has no path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(s.Path)
	stage, err := os.CreateTemp(directory, ".field-kit-baseline-session-*")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	defer os.Remove(stagePath)
	if err := stage.Chmod(0o600); err != nil {
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
	current, err := ReadStore(s.Path)
	if err != nil || current.Exists != s.Exists || !bytes.Equal(current.Data, s.Data) {
		return errors.New("baseline session changed concurrently; rerun command")
	}
	if err := os.Rename(stagePath, s.Path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err == nil {
		err = dir.Sync()
		dir.Close()
	}
	return err
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validFileIdentity(identity FileIdentity) bool {
	return identity.Path != "" && !strings.ContainsAny(identity.Path, "\r\n\x00") && sha256Pattern.MatchString(identity.SHA256)
}

func validTime(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC && parsed.Format(time.RFC3339Nano) == value
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("expected a regular file without symlink indirection")
	}
	return os.ReadFile(path)
}

func clone(document Document) Document {
	data, _ := json.Marshal(document)
	var result Document
	_ = json.Unmarshal(data, &result)
	return result
}
