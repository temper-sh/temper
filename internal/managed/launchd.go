package managed

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/probecmd"
)

// Launchd owns one explicitly bootstrapped job in the user's GUI domain. It
// writes no login agent and never alters a pre-existing job or listener.
type Launchd struct {
	Root   string
	Label  string
	Listen string
}

func (l Launchd) target() string     { return fmt.Sprintf("gui/%d/%s", os.Getuid(), l.Label) }
func (l Launchd) plist(j Job) string { return filepath.Join(filepath.Dir(j.Config), "job.plist") }
func launchctl(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/launchctl", args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}
	return cmd.CombinedOutput()
}

func (l Launchd) print(ctx context.Context) (string, bool, error) {
	if runtime.GOOS != "darwin" {
		return "", false, errors.New("managed launchd availability requires macOS")
	}
	raw, err := launchctl(ctx, "print", l.target())
	if err != nil {
		if strings.Contains(string(raw), "Could not find service") || strings.Contains(string(raw), "Could not find specified service") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("inspect managed job: %w: %s", err, raw)
	}
	return string(raw), true, nil
}

var jobPID = regexp.MustCompile(`(?m)^\s*pid = ([0-9]+)\s*$`)

func (l Launchd) Observe(ctx context.Context, jobs []Job) (Observation, error) {
	printed, exists, err := l.print(ctx)
	if err != nil {
		return Observation{}, err
	}
	if !exists {
		for _, j := range jobs {
			loaded, err := j.loaded()
			if err != nil {
				return Observation{}, err
			}
			identities, err := readShutdown(j)
			if err != nil {
				return Observation{}, err
			}
			remaining, err := probecmd.ReapManaged(append(identities, loaded...), false)
			if err != nil {
				return Observation{}, err
			}
			if remaining {
				return Observation{Exists: true, Generation: j.Generation}, nil
			}
		}
		if !probecmd.ManagedListenerClosed(l.Listen) {
			return Observation{}, errors.New("listener is occupied without the owned managed job")
		}
		return Observation{}, nil
	}
	var job *Job
	for _, j := range jobs {
		if strings.Contains(printed, "path = "+l.plist(j)+"\n") {
			j := j
			job = &j
			break
		}
	}
	if job == nil {
		return Observation{}, errors.New("launchd label belongs to an unrecognized job; refusing takeover")
	}
	if err := verifyJobConfig(*job); err != nil {
		return Observation{}, err
	}
	raw, err := os.ReadFile(l.plist(*job))
	if err != nil {
		return Observation{}, err
	}
	if !bytes.Equal(raw, plistBytes(*job)) {
		return Observation{}, errors.New("owned launchd plist was edited")
	}
	o := Observation{Exists: true, Generation: job.Generation, Models: map[string]string{}}
	match := jobPID.FindStringSubmatch(printed)
	if len(match) != 2 {
		if _, err := job.loaded(); err != nil {
			return o, err
		}
		return o, nil
	}
	o.PID, _ = strconv.Atoi(match[1])
	var commands []probecmd.ManagedCommand
	for _, p := range job.Processes {
		commands = append(commands, probecmd.ManagedCommand{ID: p.Role, Path: p.Path, Arguments: p.Arguments})
	}
	observed, err := probecmd.ObserveManaged(o.PID, job.Router, job.Arguments(), job.Listen, commands)
	if err != nil {
		return o, err
	}
	o.Ready = observed.Listening
	if observed.Suspended {
		o.Ready = false
		return o, nil
	}
	for _, id := range job.Presets {
		o.Models[id] = "available on demand"
	}
	for _, p := range observed.Processes {
		if p.ID != "router" && p.ID != "inspection" {
			o.Models[strings.Split(p.ID, "/")[0]] = "running; engine readiness not checked"
		}
	}
	if o.Ready {
		busy, err := readActivity(ctx, job.Listen)
		if err != nil {
			return o, err
		}
		o.Busy = busy
	}
	return o, nil
}

func xmlString(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return "<string>" + b.String() + "</string>"
}
func plistBytes(j Job) []byte {
	args := append([]string{j.Router}, j.Arguments()...)
	var encoded strings.Builder
	for _, a := range args {
		encoded.WriteString(xmlString(a))
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key>` + xmlString(j.Label) + `<key>ProgramArguments</key><array>` + encoded.String() + `</array><key>RunAtLoad</key><true/><key>KeepAlive</key><false/><key>ExitTimeOut</key><integer>30</integer><key>EnvironmentVariables</key><dict><key>PATH</key><string>/usr/bin:/bin:/usr/sbin:/sbin</string></dict><key>StandardOutPath</key>` + xmlString(filepath.Join(filepath.Dir(j.Config), "router.log")) + `<key>StandardErrorPath</key>` + xmlString(filepath.Join(filepath.Dir(j.Config), "router.log")) + `</dict></plist>`)
}

func (l Launchd) Start(ctx context.Context, j Job) error {
	if err := j.Validate(l.Root); err != nil {
		return err
	}
	if err := verifyJobConfig(j); err != nil {
		return err
	}
	digest, err := executableDigest(j.Launcher)
	if err != nil {
		return err
	}
	if digest != j.LauncherSHA256 {
		return errors.New("Temper executable changed; render activation again")
	}
	o, err := l.Observe(ctx, []Job{j})
	if err != nil {
		return err
	}
	if o.Ready {
		return nil
	}
	loaded, err := j.loaded()
	if err != nil {
		return err
	}
	if len(loaded) > 0 {
		return errors.New("recorded engines remain after router exit; stop the layout before restarting")
	}
	if o.Exists {
		raw, err := launchctl(ctx, "kickstart", l.target())
		if err != nil {
			return fmt.Errorf("restart owned job: %w: %s", err, raw)
		}
	} else {
		if err := writeAtomic(ctx, l.plist(j), plistBytes(j)); err != nil {
			return err
		}
		raw, err := launchctl(ctx, "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), l.plist(j))
		if err != nil {
			return fmt.Errorf("bootstrap managed job: %w: %s", err, raw)
		}
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("router readiness is pending; inspect layout status and retry activation")
		case <-ticker.C:
			o, err := l.Observe(ctx, []Job{j})
			if err == nil && o.Ready {
				return nil
			}
		}
	}
}

func (l Launchd) Stop(ctx context.Context, j Job) error {
	o, err := l.Observe(ctx, []Job{j})
	if err != nil {
		return err
	}
	if !o.Exists {
		return nil
	}
	if o.Busy {
		return errors.New("active requests prevent stopping")
	}
	if o.PID != 0 {
		var beforeCommands []probecmd.ManagedCommand
		for _, p := range j.Processes {
			beforeCommands = append(beforeCommands, probecmd.ManagedCommand{ID: p.Role, Path: p.Path, Arguments: p.Arguments})
		}
		before, err := probecmd.ObserveManaged(o.PID, j.Router, j.Arguments(), j.Listen, beforeCommands)
		if err != nil {
			return err
		}
		pausePath := filepath.Join(filepath.Dir(j.Config), "suspend.json")
		var routerIdentity probecmd.ManagedIdentity
		for _, identity := range before.Identities {
			if identity.ID == "router" {
				routerIdentity = identity
				raw, err := json.Marshal(identity)
				if err != nil {
					return err
				}
				if err = writeAtomic(ctx, pausePath, raw); err != nil {
					return err
				}
			}
		}
		defer func() { _ = l.Recover(context.WithoutCancel(ctx), []Job{j}) }()
		release, err := probecmd.QuiesceManaged(routerIdentity)
		if err != nil {
			return err
		}
		var commands []probecmd.ManagedCommand
		for _, p := range j.Processes {
			commands = append(commands, probecmd.ManagedCommand{ID: p.Role, Path: p.Path, Arguments: p.Arguments})
		}
		bound, err := probecmd.ObserveManaged(o.PID, j.Router, j.Arguments(), j.Listen, commands)
		if err != nil {
			_ = release()
			return err
		}
		rawIdentity, err := json.Marshal(bound.Identities)
		if err != nil {
			_ = release()
			return err
		}
		if err = writeAtomic(ctx, filepath.Join(filepath.Dir(j.Config), "shutdown.json"), rawIdentity); err != nil {
			_ = release()
			return err
		}
		// KeepAlive is false: after a verified idle stop, launchd cannot restart
		// the router while its job is being removed. Retry activation is explicit.
		raw, signalErr := launchctl(ctx, "kill", "SIGTERM", l.target())
		resumeErr := release()
		if signalErr != nil {
			return fmt.Errorf("stop router: %w: %s", signalErr, raw)
		}
		if resumeErr != nil {
			return resumeErr
		}
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for o.PID != 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return errors.New("owned router shutdown remains pending; no force was sent")
			case <-ticker.C:
				printed, exists, err := l.print(ctx)
				if err != nil {
					return err
				}
				if !exists || len(jobPID.FindStringSubmatch(printed)) == 0 {
					o.PID = 0
				}
			}
		}
	}
	identities, err := readShutdown(j)
	if err != nil {
		return err
	}
	loaded, err := j.loaded()
	if err != nil {
		return err
	}
	identities = append(identities, loaded...)
	remaining, err := probecmd.ReapManaged(identities, true)
	if err != nil {
		return err
	}
	if remaining {
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for remaining {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return errors.New("verified descendants remain; stop is pending and may be retried")
			case <-ticker.C:
				remaining, err = probecmd.ReapManaged(identities, false)
				if err != nil {
					return err
				}
			}
		}
	}
	if !probecmd.ManagedListenerClosed(j.Listen) {
		return errors.New("listener remains after router exit; refusing cleanup")
	}
	_, exists, err := l.print(ctx)
	if err != nil {
		return err
	}
	if exists {
		raw, err := launchctl(ctx, "bootout", l.target())
		if err != nil {
			return fmt.Errorf("remove owned launchd job: %w: %s", err, raw)
		}
	}
	if err := os.Remove(filepath.Join(filepath.Dir(j.Config), "shutdown.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Recovery only resumes a router whose exact identity was recorded before our
// interrupted stop hold. Status remains read-only; the next mutation owns this.
func (l Launchd) Recover(ctx context.Context, jobs []Job) error {
	for _, j := range jobs {
		path := filepath.Join(filepath.Dir(j.Config), "suspend.json")
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return errors.New("unsafe suspend recovery record")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var identity probecmd.ManagedIdentity
		if err = json.Unmarshal(raw, &identity); err != nil {
			return err
		}
		if identity.ID != "router" || !probecmd.ManagedIdentityAllowed(identity, j.commands()) {
			return errors.New("suspend recovery does not name the selected router")
		}
		if err = probecmd.ResumeManaged(identity); err != nil {
			return err
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func readShutdown(j Job) ([]probecmd.ManagedIdentity, error) {
	path := filepath.Join(filepath.Dir(j.Config), "shutdown.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("unsafe managed shutdown record")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var identities []probecmd.ManagedIdentity
	if err = json.Unmarshal(raw, &identities); err != nil {
		return nil, err
	}
	for _, identity := range identities {
		if !probecmd.ManagedIdentityAllowed(identity, j.commands()) {
			return nil, errors.New("shutdown record contains an unselected process")
		}
	}
	return identities, nil
}

// Read the initial in-flight snapshot from the pinned router's SSE API. Missing
// or unfamiliar evidence is an error, never evidence that the router is idle.
func readActivity(ctx context.Context, listen string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listen+"/api/events", nil)
	if err != nil {
		return false, err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("router activity redirect refused") }}
	res, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return false, fmt.Errorf("router activity status %d", res.StatusCode)
	}
	scanner := bufio.NewScanner(io.LimitReader(res.Body, 1<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var envelope struct{ Type, Data string }
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &envelope); err != nil {
			return false, err
		}
		if envelope.Type != "inflight" {
			continue
		}
		// v260 uses requests,omitempty: an explicit snapshot with no requests
		// field is idle. Null and malformed arrays remain unrecognized evidence.
		// https://github.com/mostlygeek/llama-swap/blob/v260/internal/swaputil/events.go
		snapshot := struct {
			Operation string            `json:"operation"`
			Requests  []json.RawMessage `json:"requests"`
		}{Requests: []json.RawMessage{}}
		if err := json.Unmarshal([]byte(envelope.Data), &snapshot); err != nil {
			return false, err
		}
		if snapshot.Operation != "snapshot" || snapshot.Requests == nil {
			return false, errors.New("router returned no in-flight snapshot")
		}
		return len(snapshot.Requests) > 0, nil
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, errors.New("router did not disclose in-flight requests")
}
