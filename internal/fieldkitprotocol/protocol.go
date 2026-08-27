// Package fieldkitprotocol owns the portable live protocols that Temper may
// execute for an embedded Field Kit package. Protocols retain hashes and
// structural outcomes, never generated response text.
package fieldkitprotocol

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	QwenDynamicShortID       = "qwen38-dynamic-short"
	QwenDynamicShortRevision = 1
	QwenDynamicShortSchema   = "field-kit-qwen38-dynamic-protocol/v1"
	expectedControl          = "TEMPER-QWEN-OK"
)

type Client struct {
	http *http.Client
}

func NewClient(httpClient *http.Client) (Client, error) {
	if httpClient == nil {
		return Client{}, errors.New("Field Kit protocol requires an HTTP client")
	}
	return Client{http: httpClient}, nil
}

func Supports(id string, revision int, schema string) bool {
	return id == QwenDynamicShortID && revision == QwenDynamicShortRevision && schema == QwenDynamicShortSchema
}

func (c Client) Exercise(ctx context.Context, base, model string, snapshot func(context.Context) (ResourceSnapshot, error)) (map[string]any, []ResourceSnapshot, error) {
	if base == "" || model == "" || snapshot == nil {
		return nil, nil, errors.New("Field Kit protocol requires base, model, and resource snapshot inputs")
	}
	checks := map[string]any{}
	resources := []ResourceSnapshot{}

	modelsData, err := c.request(ctx, base, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, nil, err
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(modelsData, &models); err != nil {
		return nil, nil, fmt.Errorf("model discovery response: %w", err)
	}
	found := false
	for _, item := range models.Data {
		found = found || item.ID == model
	}
	if !found {
		return nil, nil, errors.New("router model discovery did not contain the exact baseline layout")
	}
	checks["models"] = map[string]any{"count": len(models.Data), "response_sha256": digestCanonicalJSON(modelsData)}

	control, err := c.post(ctx, base, chatPayload(model, []any{message("user", "Reply with exactly "+expectedControl+" and nothing else.")}, 64))
	if err != nil {
		return nil, nil, err
	}
	controlResult, err := exactResult(control, expectedControl, "control/nonstream")
	if err != nil {
		return nil, nil, err
	}
	checks["control"] = controlResult
	stream, err := c.streamExact(ctx, base, model)
	if err != nil {
		return nil, nil, err
	}
	checks["stream"] = stream
	item, err := snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	resources = append(resources, item)

	tools, err := c.runTools(ctx, base, model)
	if err != nil {
		return nil, nil, err
	}
	checks["tools"] = tools
	item, err = snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	resources = append(resources, item)

	history, err := c.runHistory(ctx, base, model)
	if err != nil {
		return nil, nil, err
	}
	checks["history"] = history
	item, err = snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	resources = append(resources, item)

	cancel, err := c.cancelAndRecover(ctx, base, model)
	if err != nil {
		return nil, nil, err
	}
	checks["cancel"] = cancel
	item, err = snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	resources = append(resources, item)

	soak := make([]map[string]any, 0, 3)
	for index := 1; index <= 3; index++ {
		started := time.Now()
		response, err := c.post(ctx, base, chatPayload(model, []any{message("user", "Reply with exactly "+expectedControl+" and nothing else.")}, 64))
		if err != nil {
			return nil, nil, err
		}
		result, err := exactResult(response, expectedControl, fmt.Sprintf("soak/%d", index))
		if err != nil {
			return nil, nil, err
		}
		result["wall_seconds"] = seconds(time.Since(started))
		soak = append(soak, result)
	}
	checks["soak"] = soak
	item, err = snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	resources = append(resources, item)
	return checks, resources, nil
}

func (c Client) streamExact(ctx context.Context, base, model string) (map[string]any, error) {
	payload := chatPayload(model, []any{message("user", "Reply with exactly "+expectedControl+" and nothing else.")}, 64)
	payload["stream"] = true
	payload["stream_options"] = map[string]any{"include_usage": true}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, base, http.MethodPost, "/v1/chat/completions", body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var content strings.Builder
	done := false
	chunks := 0
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		value := strings.TrimPrefix(line, "data: ")
		if value == "[DONE]" {
			done = true
			continue
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(value), &chunk); err != nil {
			return nil, fmt.Errorf("control/stream: decode chunk: %w", err)
		}
		chunks++
		if len(chunk.Choices) == 0 {
			continue
		}
		if chunk.Choices[0].Delta.ReasoningContent != nil && *chunk.Choices[0].Delta.ReasoningContent != "" {
			return nil, errors.New("control/stream: unexpected reasoning content")
		}
		if chunk.Choices[0].Delta.Content != nil {
			content.WriteString(*chunk.Choices[0].Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("control/stream: %w", err)
	}
	value := content.String()
	if strings.TrimSpace(value) != expectedControl || !done {
		return nil, errors.New("control/stream: output was not exact or stream did not finish")
	}
	return map[string]any{"content_sha256": digest([]byte(value)), "content_characters": len([]rune(value)), "chunks": chunks, "done": true}, nil
}

func (c Client) runTools(ctx context.Context, base, model string) (map[string]any, error) {
	definitions := []any{
		map[string]any{"type": "function", "function": map[string]any{"name": "weather.lookup", "description": "Look up weather.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}, "units": map[string]any{"type": "string", "enum": []string{"celsius", "fahrenheit"}}}, "required": []string{"city", "units"}, "additionalProperties": false}}},
		map[string]any{"type": "function", "function": map[string]any{"name": "clock.lookup", "description": "Look up local time.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"}, "additionalProperties": false}}},
	}
	system := message("system", "Follow the request exactly. Emit only requested tool calls.")
	singleMessages := []any{system, message("user", "Call weather.lookup exactly once for Stockholm using celsius.")}
	singlePayload := chatPayload(model, singleMessages, 512)
	singlePayload["tools"] = definitions
	singleResponse, err := c.post(ctx, base, singlePayload)
	if err != nil {
		return nil, err
	}
	single, messageRaw, err := parseToolCalls(singleResponse, "tools/single")
	if err != nil {
		return nil, err
	}
	if len(single) != 1 || single[0].Name != "weather.lookup" || canonicalArguments(single[0].Arguments) != `{"city":"Stockholm","units":"celsius"}` {
		return nil, errors.New("tools/single: call differed from expected")
	}
	continuation := append(append([]any{}, singleMessages...), json.RawMessage(messageRaw))
	continuation = append(continuation,
		map[string]any{"role": "tool", "tool_call_id": single[0].ID, "content": "CITY_TEMP_MARKER_17C"},
		message("user", "Reply with exactly CITY_TEMP_MARKER_17C and nothing else."),
	)
	continued, err := c.post(ctx, base, chatPayload(model, continuation, 128))
	if err != nil {
		return nil, err
	}
	continuedResult, err := exactResult(continued, "CITY_TEMP_MARKER_17C", "tools/continuation")
	if err != nil {
		return nil, err
	}
	parallelPayload := chatPayload(model, []any{system, message("user", "Make exactly two parallel calls: weather.lookup for Stockholm in celsius and clock.lookup for Tokyo.")}, 768)
	parallelPayload["tools"] = definitions
	parallelPayload["parallel_tool_calls"] = true
	parallelResponse, err := c.post(ctx, base, parallelPayload)
	if err != nil {
		return nil, err
	}
	parallel, _, err := parseToolCalls(parallelResponse, "tools/parallel")
	if err != nil {
		return nil, err
	}
	observed := make([]string, 0, len(parallel))
	for _, call := range parallel {
		observed = append(observed, call.Name+":"+canonicalArguments(call.Arguments))
	}
	sort.Strings(observed)
	wanted := []string{`clock.lookup:{"city":"Tokyo"}`, `weather.lookup:{"city":"Stockholm","units":"celsius"}`}
	if len(observed) != len(wanted) || strings.Join(observed, "\n") != strings.Join(wanted, "\n") {
		return nil, errors.New("tools/parallel: calls differed from expected")
	}
	return map[string]any{
		"single":       map[string]any{"count": len(single), "calls_sha256": digestToolCalls(single), "exact": true},
		"continuation": continuedResult,
		"parallel":     map[string]any{"count": len(parallel), "calls_sha256": digestToolCalls(parallel), "exact": true},
	}, nil
}

func (c Client) runHistory(ctx context.Context, base, model string) (map[string]any, error) {
	marker := "QWEN-HISTORY-9F3A"
	messages := []any{}
	turns := make([]map[string]any, 0, 4)
	for turn := 1; turn <= 4; turn++ {
		expected := fmt.Sprintf("QWEN-TURN-%d-OK", turn)
		prompt := fmt.Sprintf("Reply with exactly %s. Remember marker %s for later turns.", expected, marker)
		if turn > 1 {
			prompt = fmt.Sprintf("This is turn %d. Reply with exactly %s; the marker remains %s.", turn, expected, marker)
		}
		messages = append(messages, message("user", prompt))
		response, err := c.post(ctx, base, chatPayload(model, messages, 96))
		if err != nil {
			return nil, err
		}
		result, err := exactResult(response, expected, fmt.Sprintf("history/%d", turn))
		if err != nil {
			return nil, err
		}
		turns = append(turns, result)
		messages = append(messages, json.RawMessage(response.Choices[0].Message))
	}
	return map[string]any{"turns": turns, "marker_sha256": digest([]byte(marker))}, nil
}

func (c Client) cancelAndRecover(ctx context.Context, base, model string) (map[string]any, error) {
	cancelCtx, cancel := context.WithCancel(ctx)
	payload := chatPayload(model, []any{message("user", "Write the positive integers in order, one per line, for as long as possible.")}, 4096)
	payload["stream"] = true
	body, err := json.Marshal(payload)
	if err != nil {
		cancel()
		return nil, err
	}
	response, err := c.do(cancelCtx, base, http.MethodPost, "/v1/chat/completions", body)
	if err != nil {
		cancel()
		return nil, err
	}
	observed := make([]byte, 256)
	count, readErr := io.ReadAtLeast(response.Body, observed, 1)
	cancel()
	response.Body.Close()
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("cancel: read streamed bytes: %w", readErr)
	}
	if count == 0 {
		return nil, errors.New("cancel: server produced no streamed bytes")
	}
	observed = observed[:count]
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
	}
	recovered, err := c.post(ctx, base, chatPayload(model, []any{message("user", "Reply with exactly "+expectedControl+" and nothing else.")}, 64))
	if err != nil {
		return nil, err
	}
	recovery, err := exactResult(recovered, expectedControl, "cancel/recovery")
	if err != nil {
		return nil, err
	}
	return map[string]any{"cancelled_after_bytes": count, "cancelled_prefix_sha256": digest(observed), "recovery": recovery}, nil
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   any          `json:"usage,omitempty"`
	Timings any          `json:"timings,omitempty"`
}

type chatChoice struct {
	Message      json.RawMessage `json:"message"`
	FinishReason any             `json:"finish_reason"`
}

type assistantMessage struct {
	Role             string     `json:"role"`
	Content          *string    `json:"content"`
	ReasoningContent *string    `json:"reasoning_content"`
	ToolCalls        []toolCall `json:"tool_calls"`
}

type toolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type observedToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          *string `json:"content"`
			ReasoningContent *string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
}

func (c Client) post(ctx context.Context, base string, payload map[string]any) (chatResponse, error) {
	data, err := c.request(ctx, base, http.MethodPost, "/v1/chat/completions", payload)
	if err != nil {
		return chatResponse{}, err
	}
	var response chatResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return chatResponse{}, fmt.Errorf("chat response: %w", err)
	}
	return response, nil
}

func (c Client) request(ctx context.Context, base, method, requestPath string, payload any) ([]byte, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	response, err := c.do(ctx, base, method, requestPath, body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", requestPath, err)
	}
	return data, nil
}

func (c Client) do(ctx context.Context, base, method, requestPath string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+requestPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", requestPath, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 500))
		response.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s: response_prefix_bytes=%d response_prefix_sha256=%s", response.StatusCode, requestPath, len(detail), digest(detail))
	}
	return response, nil
}

func chatPayload(model string, messages []any, maximum int) map[string]any {
	return map[string]any{
		"model": model, "messages": messages, "max_tokens": maximum,
		"temperature": 0.0, "top_k": 1, "top_p": 1.0, "seed": 4242,
	}
}

func message(role, content string) map[string]any {
	return map[string]any{"role": role, "content": content}
}

func exactResult(response chatResponse, expected, label string) (map[string]any, error) {
	if len(response.Choices) != 1 {
		return nil, fmt.Errorf("%s: expected one choice", label)
	}
	var assistant assistantMessage
	if err := json.Unmarshal(response.Choices[0].Message, &assistant); err != nil || assistant.Role != "assistant" {
		return nil, fmt.Errorf("%s: missing assistant message", label)
	}
	content := ""
	if assistant.Content != nil {
		content = *assistant.Content
	}
	reasoning := ""
	if assistant.ReasoningContent != nil {
		reasoning = *assistant.ReasoningContent
	}
	if strings.TrimSpace(content) != expected || reasoning != "" || strings.Contains(content, "<think>") {
		return nil, fmt.Errorf("%s: output was not exact non-reasoning content", label)
	}
	return map[string]any{
		"content_sha256": digest([]byte(content)), "content_characters": len([]rune(content)),
		"finish_reason": response.Choices[0].FinishReason, "usage": response.Usage, "timings": response.Timings,
	}, nil
}

func parseToolCalls(response chatResponse, label string) ([]observedToolCall, []byte, error) {
	if len(response.Choices) != 1 {
		return nil, nil, fmt.Errorf("%s: expected one choice", label)
	}
	var assistant assistantMessage
	if err := json.Unmarshal(response.Choices[0].Message, &assistant); err != nil || assistant.Role != "assistant" {
		return nil, nil, fmt.Errorf("%s: missing assistant message", label)
	}
	if len(assistant.ToolCalls) == 0 {
		return nil, nil, fmt.Errorf("%s: no tool calls", label)
	}
	result := make([]observedToolCall, 0, len(assistant.ToolCalls))
	for _, call := range assistant.ToolCalls {
		arguments := call.Function.Arguments
		if len(arguments) > 0 && arguments[0] == '"' {
			var encoded string
			if err := json.Unmarshal(arguments, &encoded); err != nil {
				return nil, nil, fmt.Errorf("%s: invalid string tool arguments", label)
			}
			arguments = json.RawMessage(encoded)
		}
		var value any
		if err := json.Unmarshal(arguments, &value); err != nil {
			return nil, nil, fmt.Errorf("%s: invalid tool arguments", label)
		}
		canonical, _ := json.Marshal(value)
		result = append(result, observedToolCall{ID: call.ID, Name: call.Function.Name, Arguments: canonical})
	}
	return result, append([]byte(nil), response.Choices[0].Message...), nil
}

func canonicalArguments(arguments json.RawMessage) string { return string(arguments) }

func digestToolCalls(calls []observedToolCall) string {
	values := make([]string, 0, len(calls))
	for _, call := range calls {
		values = append(values, call.Name+":"+canonicalArguments(call.Arguments))
	}
	sort.Strings(values)
	return digest([]byte(strings.Join(values, "\n")))
}

func digestCanonicalJSON(data []byte) string {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return digest(data)
	}
	canonical, _ := json.Marshal(value)
	return digest(canonical)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func seconds(duration time.Duration) float64 {
	return float64(duration.Round(time.Microsecond)) / float64(time.Second)
}

type ResourceSnapshot struct {
	SwapMiB         float64        `json:"swap_mib"`
	SwapGrowthMiB   float64        `json:"swap_growth_mib"`
	ThermalSHA256   string         `json:"thermal_sha256"`
	ChildPeakMemory map[string]any `json:"child_peak_memory"`
}
