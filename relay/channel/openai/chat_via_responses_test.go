package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func newClaudeResponsesTestContext(t *testing.T) (*httptest.ResponseRecorder, *gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{
		IsStream:          true,
		RelayFormat:       types.RelayFormatClaude,
		OriginModelName:   "gpt-5.5",
		ChannelMeta:       &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"},
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone},
		DisablePing:       true,
	}
	return w, c, info
}

func newResponsesStream(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestOaiResponsesToChatStreamHandlerEmitsClaudeStopForTextOnly(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"Short answer."}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Short answer.","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}}`,
	}, "\n\n") + "\n\n"

	usage, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler returned error: %v", err)
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 3 || usage.TotalTokens != 13 {
		t.Fatalf("unexpected usage: %#v", usage)
	}

	got := w.Body.String()
	if !strings.Contains(got, `"stop_reason":"end_turn"`) {
		t.Fatalf("expected Claude end_turn stop reason, got stream:\n%s", got)
	}
	if !strings.Contains(got, `event: message_stop`) {
		t.Fatalf("expected Claude message_stop, got stream:\n%s", got)
	}
}

func TestOaiResponsesToChatStreamHandlerDropsReasoningSummaryForClaude(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.reasoning_summary_text.delta","delta":"hidden reasoning"}`,
		`data: {"type":"response.reasoning_summary_text.done"}`,
		`data: {"type":"response.output_text.delta","delta":"Visible answer."}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Visible answer.","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}}`,
	}, "\n\n") + "\n\n"

	_, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler returned error: %v", err)
	}

	got := w.Body.String()
	if strings.Contains(got, "hidden reasoning") || strings.Contains(got, "thinking_delta") || strings.Contains(got, `"thinking"`) {
		t.Fatalf("Claude stream leaked OpenAI reasoning as Claude thinking: %s", got)
	}
	if !strings.Contains(got, "Visible answer.") {
		t.Fatalf("Claude stream missing visible answer: %s", got)
	}
}

func TestOaiResponsesToChatStreamHandlerPreservesToolCallAfterTextForClaude(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"I'll inspect the repo."}`,
		`data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"cmd\":\"rg websocket\"}"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"I'll inspect the repo.","annotations":[]}]},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"rg websocket\"}"}],"usage":{"input_tokens":10,"output_tokens":12,"total_tokens":22}}}`,
	}, "\n\n") + "\n\n"

	usage, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler returned error: %v", err)
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 12 || usage.TotalTokens != 22 {
		t.Fatalf("unexpected usage: %#v", usage)
	}

	got := w.Body.String()
	if !strings.Contains(got, `"type":"tool_use"`) {
		t.Fatalf("expected Claude tool_use block after text, got stream:\n%s", got)
	}
	if !strings.Contains(got, `"stop_reason":"tool_use"`) {
		t.Fatalf("expected Claude stop_reason tool_use, got stream:\n%s", got)
	}
	if strings.Contains(got, `"stop_reason":"end_turn"`) {
		t.Fatalf("tool call after text must not be reported as end_turn, got stream:\n%s", got)
	}
}

func TestOaiResponsesToChatStreamHandlerEmitsCompletedFallbackTextForClaude(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Only in completed.","annotations":[]}]}],"usage":{"input_tokens":8,"output_tokens":4,"total_tokens":12}}}`,
	}, "\n\n") + "\n\n"

	usage, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler returned error: %v", err)
	}
	if usage.PromptTokens != 8 || usage.CompletionTokens != 4 || usage.TotalTokens != 12 {
		t.Fatalf("unexpected usage: %#v", usage)
	}

	got := w.Body.String()
	if !strings.Contains(got, `"text":"Only in completed."`) {
		t.Fatalf("expected completed fallback text, got stream:\n%s", got)
	}
	if !strings.Contains(got, `event: message_stop`) {
		t.Fatalf("expected Claude message_stop, got stream:\n%s", got)
	}
}

func TestOaiResponsesToChatStreamHandlerEmitsCompletedFallbackToolCallForClaude(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}],"usage":{"input_tokens":8,"output_tokens":6,"total_tokens":14}}}`,
	}, "\n\n") + "\n\n"

	usage, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler returned error: %v", err)
	}
	if usage.PromptTokens != 8 || usage.CompletionTokens != 6 || usage.TotalTokens != 14 {
		t.Fatalf("unexpected usage: %#v", usage)
	}

	got := w.Body.String()
	if !strings.Contains(got, `"type":"tool_use"`) {
		t.Fatalf("expected completed fallback tool_use, got stream:\n%s", got)
	}
	if !strings.Contains(got, `"partial_json":"{\"cmd\":\"pwd\"}"`) {
		t.Fatalf("expected tool call arguments, got stream:\n%s", got)
	}
	if !strings.Contains(got, `"stop_reason":"tool_use"`) {
		t.Fatalf("expected Claude stop_reason tool_use, got stream:\n%s", got)
	}
}

func TestOaiResponsesToChatStreamHandlerEmitsCompletedFallbackTextAndToolCallForClaude(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"I'll inspect.","annotations":[]}]},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"pwd\"}"}],"usage":{"input_tokens":9,"output_tokens":8,"total_tokens":17}}}`,
	}, "\n\n") + "\n\n"

	usage, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler returned error: %v", err)
	}
	if usage.PromptTokens != 9 || usage.CompletionTokens != 8 || usage.TotalTokens != 17 {
		t.Fatalf("unexpected usage: %#v", usage)
	}

	got := w.Body.String()
	if !strings.Contains(got, `"text":"I'll inspect."`) {
		t.Fatalf("expected completed fallback text, got stream:\n%s", got)
	}
	if !strings.Contains(got, `"type":"tool_use"`) {
		t.Fatalf("expected completed fallback tool_use after text, got stream:\n%s", got)
	}
	if !strings.Contains(got, `"stop_reason":"tool_use"`) {
		t.Fatalf("expected Claude stop_reason tool_use, got stream:\n%s", got)
	}
}

func TestOaiResponsesToChatStreamHandlerTreatsIncompleteAsLengthStopForClaude(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"Partial answer"}`,
		`data: {"type":"response.incomplete","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Partial answer","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13},"incomplete_details":{"reason":"max_output_tokens"}}}`,
	}, "\n\n") + "\n\n"

	usage, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler returned error: %v", err)
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 3 || usage.TotalTokens != 13 {
		t.Fatalf("unexpected usage: %#v", usage)
	}

	got := w.Body.String()
	if !strings.Contains(got, `"stop_reason":"max_tokens"`) {
		t.Fatalf("expected Claude max_tokens stop reason, got stream:\n%s", got)
	}
	if !strings.Contains(got, `event: message_stop`) {
		t.Fatalf("expected Claude message_stop for incomplete terminal event, got stream:\n%s", got)
	}
}

func TestOaiResponsesToChatStreamHandlerErrorsWithoutCompleted(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"Partial only."}`,
	}, "\n\n") + "\n\n"

	_, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err == nil {
		t.Fatalf("expected error for stream without response.completed")
	}
	if err.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 for incomplete upstream stream, got %d", err.StatusCode)
	}

	got := w.Body.String()
	if strings.Contains(got, `event: message_stop`) {
		t.Fatalf("must not emit Claude message_stop for incomplete Responses stream, got:\n%s", got)
	}
}

func TestOaiResponsesToChatStreamHandlerDropsEmptyReadPagesForClaude(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"Read","arguments":"{\"file_path\":\"/tmp/demo.py\",\"limit\":2000,\"offset\":0,\"pages\":\"\"}"}],"usage":{"input_tokens":8,"output_tokens":6,"total_tokens":14}}}`,
	}, "\n\n") + "\n\n"

	_, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler returned error: %v", err)
	}

	got := w.Body.String()
	if strings.Contains(got, `"pages":""`) {
		t.Fatalf("Claude Read tool input must drop empty pages, got stream:\n%s", got)
	}
	if !strings.Contains(got, `\"file_path\":\"/tmp/demo.py\"`) {
		t.Fatalf("Claude Read tool input lost other arguments, got stream:\n%s", got)
	}
}

func TestOaiResponsesStreamToChatHandlerKeepsEmptyReadPagesForOpenAI(t *testing.T) {
	w, c, info := newOpenAIResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"Read","arguments":"{\"file_path\":\"/tmp/demo.py\",\"limit\":2000,\"offset\":0,\"pages\":\"\"}"}],"usage":{"input_tokens":5,"output_tokens":4,"total_tokens":9}}}`,
	}, "\n\n") + "\n\n"

	_, apiErr := OaiResponsesStreamToChatHandler(c, info, newResponsesStream(body))
	if apiErr != nil {
		t.Fatalf("unexpected error: %v", apiErr)
	}

	got := w.Body.String()
	if !strings.Contains(got, `\"pages\":\"\"`) {
		t.Fatalf("OpenAI Responses conversion must preserve empty pages, got:\n%s", got)
	}
}

func TestOaiResponsesToChatStreamHandlerKeepsEmptyPagesForOtherClaudeTools(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	defer func() {
		appconstant.StreamingTimeout = oldStreamingTimeout
	}()

	w, c, info := newClaudeResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"OtherTool","arguments":"{\"pages\":\"\"}"}],"usage":{"input_tokens":8,"output_tokens":6,"total_tokens":14}}}`,
	}, "\n\n") + "\n\n"

	_, err := OaiResponsesToChatStreamHandler(c, info, newResponsesStream(body))
	if err != nil {
		t.Fatalf("OaiResponsesToChatStreamHandler returned error: %v", err)
	}

	got := w.Body.String()
	if !strings.Contains(got, `\"pages\":\"\"`) {
		t.Fatalf("non-Read Claude tool input must preserve empty pages, got stream:\n%s", got)
	}
}

func TestOaiResponsesToChatHandlerDropsEmptyReadPagesForClaudeJSON(t *testing.T) {
	w, c, info := newClaudeResponsesTestContext(t)
	body := `{"id":"resp_1","created_at":123,"model":"gpt-5.5","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"Read","arguments":"{\"file_path\":\"/tmp/demo.py\",\"limit\":2000,\"offset\":0,\"pages\":\"\"}"}],"usage":{"input_tokens":8,"output_tokens":6,"total_tokens":14}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	_, err := OaiResponsesToChatHandler(c, info, resp)
	if err != nil {
		t.Fatalf("OaiResponsesToChatHandler returned error: %v", err)
	}

	got := w.Body.String()
	if strings.Contains(got, `"pages":""`) {
		t.Fatalf("Claude JSON Read tool input must drop empty pages, got:\n%s", got)
	}
	if !strings.Contains(got, `"file_path":"/tmp/demo.py"`) {
		t.Fatalf("Claude JSON Read tool input lost other arguments, got:\n%s", got)
	}
}

// ── OaiResponsesStreamToChatHandler tests ────────────────────────────────────

func newOpenAIResponsesTestContext(t *testing.T) (*httptest.ResponseRecorder, *gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		IsStream:        false,
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"},
	}
	return w, c, info
}

func TestOaiResponsesStreamToChatHandler_BasicText(t *testing.T) {
	w, c, info := newOpenAIResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"Hello "}`,
		`data: {"type":"response.output_text.delta","delta":"world."}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello world.","annotations":[]}]}],"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}}`,
	}, "\n\n") + "\n\n"

	usage, apiErr := OaiResponsesStreamToChatHandler(c, info, newResponsesStream(body))
	if apiErr != nil {
		t.Fatalf("unexpected error: %v", apiErr)
	}
	if usage.TotalTokens != 8 {
		t.Fatalf("expected total_tokens=8, got %d", usage.TotalTokens)
	}
	got := w.Body.String()
	if !strings.Contains(got, `"Hello world."`) {
		t.Fatalf("expected aggregated text in response, got:\n%s", got)
	}
	if !strings.Contains(got, `"finish_reason":"stop"`) {
		t.Fatalf("expected finish_reason stop, got:\n%s", got)
	}
}

func TestOaiResponsesToChatHandler_DetectsSSEBodyWithoutEventStreamHeader(t *testing.T) {
	w, c, info := newOpenAIResponsesTestContext(t)
	body := strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"Hello "}`,
		`data: {"type":"response.output_text.delta","delta":"world."}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello world.","annotations":[]}]}],"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}}`,
	}, "\n\n") + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesToChatHandler(c, info, resp)
	if apiErr != nil {
		t.Fatalf("unexpected error: %v", apiErr)
	}
	if usage.TotalTokens != 8 {
		t.Fatalf("expected total_tokens=8, got %d", usage.TotalTokens)
	}
	got := w.Body.String()
	if !strings.Contains(got, `"Hello world."`) {
		t.Fatalf("expected aggregated text in response, got:\n%s", got)
	}
}

func TestOaiResponsesStreamToChatHandler_LengthFinishReason(t *testing.T) {
	w, c, info := newOpenAIResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"Partial"}`,
		`data: {"type":"response.incomplete","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Partial","annotations":[]}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`,
	}, "\n\n") + "\n\n"

	usage, apiErr := OaiResponsesStreamToChatHandler(c, info, newResponsesStream(body))
	if apiErr != nil {
		t.Fatalf("unexpected error: %v", apiErr)
	}
	if usage.TotalTokens != 7 {
		t.Fatalf("expected total_tokens=7, got %d", usage.TotalTokens)
	}
	got := w.Body.String()
	if !strings.Contains(got, `"finish_reason":"length"`) {
		t.Fatalf("expected finish_reason length for incomplete stream, got:\n%s", got)
	}
}

func TestOaiResponsesStreamToChatHandler_NoTerminalEvent_Returns502(t *testing.T) {
	_, c, info := newOpenAIResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"Partial only."}`,
	}, "\n\n") + "\n\n"

	_, apiErr := OaiResponsesStreamToChatHandler(c, info, newResponsesStream(body))
	if apiErr == nil {
		t.Fatal("expected error for stream without terminal event")
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", apiErr.StatusCode)
	}
}

func TestOaiResponsesStreamToChatHandler_ToolCallDelta(t *testing.T) {
	w, c, info := newOpenAIResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"cmd\":"}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"\"ls\"}"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5","output":[],"usage":{"input_tokens":5,"output_tokens":4,"total_tokens":9}}}`,
	}, "\n\n") + "\n\n"

	usage, apiErr := OaiResponsesStreamToChatHandler(c, info, newResponsesStream(body))
	if apiErr != nil {
		t.Fatalf("unexpected error: %v", apiErr)
	}
	if usage.TotalTokens != 9 {
		t.Fatalf("expected total_tokens=9, got %d", usage.TotalTokens)
	}
	got := w.Body.String()
	if !strings.Contains(got, `"tool_calls"`) {
		t.Fatalf("expected tool_calls in response, got:\n%s", got)
	}
	if !strings.Contains(got, `"finish_reason":"tool_calls"`) {
		t.Fatalf("expected finish_reason tool_calls, got:\n%s", got)
	}
}

func TestOaiResponsesStreamToChatHandler_ToolCallDoneSupersedesDelta(t *testing.T) {
	w, c, info := newOpenAIResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"cmd\":\"wrong\"}"}`,
		`data: {"type":"response.function_call_arguments.done","item_id":"fc_1","delta":"{\"cmd\":\"correct\"}"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5","output":[],"usage":{"input_tokens":5,"output_tokens":4,"total_tokens":9}}}`,
	}, "\n\n") + "\n\n"

	_, apiErr := OaiResponsesStreamToChatHandler(c, info, newResponsesStream(body))
	if apiErr != nil {
		t.Fatalf("unexpected error: %v", apiErr)
	}
	got := w.Body.String()
	if strings.Contains(got, "wrong") {
		t.Fatalf("delta args must be superseded by done args, got:\n%s", got)
	}
	if !strings.Contains(got, "correct") {
		t.Fatalf("expected done args in response, got:\n%s", got)
	}
}

func TestOaiResponsesStreamToChatHandler_TopLevelErrorEvent(t *testing.T) {
	_, c, info := newOpenAIResponsesTestContext(t)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5"}}`,
		`data: {"type":"error","error":{"type":"server_error","code":"internal_error","message":"upstream failed","param":""}}`,
	}, "\n\n") + "\n\n"

	_, apiErr := OaiResponsesStreamToChatHandler(c, info, newResponsesStream(body))
	if apiErr == nil {
		t.Fatal("expected error for top-level error event")
	}
	if !strings.Contains(apiErr.Error(), "upstream failed") {
		t.Fatalf("expected error message preserved, got: %s", apiErr.Error())
	}
}

func TestOaiResponsesStreamToChatHandler_ContextCancelled_Returns504(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{
		IsStream:        false,
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-5.5",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"},
	}

	// cancel before the handler reads anything
	cancel()

	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5"}}`,
		`data: {"type":"response.output_text.delta","delta":"line1"}`,
		`data: {"type":"response.output_text.delta","delta":"line2"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","created_at":1000,"model":"gpt-5.5","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	}, "\n\n") + "\n\n"

	_, apiErr := OaiResponsesStreamToChatHandler(c, info, newResponsesStream(body))
	if apiErr == nil {
		t.Fatal("expected error when context is cancelled")
	}
	if apiErr.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", apiErr.StatusCode)
	}
}
