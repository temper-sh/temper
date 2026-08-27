package fieldkitprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSupportsOnlyReviewedProtocolIdentity(t *testing.T) {
	if !Supports(QwenDynamicShortID, QwenDynamicShortRevision, QwenDynamicShortSchema) {
		t.Fatal("reviewed protocol identity is not supported")
	}
	for _, changed := range []struct {
		id       string
		revision int
		schema   string
	}{
		{id: "different", revision: QwenDynamicShortRevision, schema: QwenDynamicShortSchema},
		{id: QwenDynamicShortID, revision: 2, schema: QwenDynamicShortSchema},
		{id: QwenDynamicShortID, revision: QwenDynamicShortRevision, schema: "different/v1"},
	} {
		if Supports(changed.id, changed.revision, changed.schema) {
			t.Fatalf("unsupported identity accepted: %#v", changed)
		}
	}
}

func TestExactResultRetainsOnlyStructuredEvidence(t *testing.T) {
	content := expectedControl
	message, err := json.Marshal(assistantMessage{Role: "assistant", Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	result, err := exactResult(chatResponse{Choices: []chatChoice{{Message: message, FinishReason: "stop"}}}, expectedControl, "control")
	if err != nil {
		t.Fatal(err)
	}
	if result["content_sha256"] != digest([]byte(expectedControl)) || result["content_characters"] != len([]rune(expectedControl)) {
		t.Fatalf("unexpected exact evidence: %#v", result)
	}
	for _, value := range result {
		if text, ok := value.(string); ok && text == expectedControl {
			t.Fatal("generated content leaked into retained evidence")
		}
	}
}

func TestResourceSnapshotStopsOnSwapAndThermalWarnings(t *testing.T) {
	client, err := NewClient(&http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	for name, resources := range map[string]*resourceFake{
		"swap":    {swap: 600, thermal: "No thermal warning level has been recorded"},
		"thermal": {swap: 10, thermal: "Thermal warning level has been recorded"},
	} {
		t.Run(name, func(t *testing.T) {
			runner := Runner{resources: resources, client: client, now: time.Now}
			if _, err := runner.resourceSnapshot(context.Background(), 0); err == nil {
				t.Fatal("unsafe resource state was accepted")
			}
		})
	}
}

func TestRunnerRefusesUnknownProtocolBeforeEffects(t *testing.T) {
	probe := &probeFake{}
	resources := &resourceFake{}
	runner, err := NewRunnerWith(probe, resources, &http.Client{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	err = runner.Run(context.Background(), Options{ID: "unknown", Revision: 1, Schema: "unknown/v1"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error = %v", err)
	}
	if probe.calls != 0 || resources.calls != 0 {
		t.Fatalf("refusal caused effects: probe=%d resources=%d", probe.calls, resources.calls)
	}
}

type resourceFake struct {
	swap    float64
	thermal string
	calls   int
}

func (f *resourceFake) SwapMiB(context.Context) (float64, error) {
	f.calls++
	return f.swap, nil
}

func (f *resourceFake) Thermal(context.Context) (string, error) {
	f.calls++
	return f.thermal, nil
}

type probeFake struct{ calls int }

func (f *probeFake) DryRun(context.Context, ProbeOptions) error {
	f.calls++
	return errors.New("unexpected")
}

func (f *probeFake) Start(context.Context, ProbeOptions, io.Writer, io.Writer) (RunningProbe, error) {
	f.calls++
	return nil, errors.New("unexpected")
}
