package oairesponses

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rawToolArguments(t *testing.T, arguments string) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	return encoded
}

func collectStreamToolArguments(chunks []dto.ChatCompletionsStreamResponse) (string, string) {
	var name string
	var arguments string
	for _, chunk := range chunks {
		for _, choice := range chunk.Choices {
			for _, tool := range choice.Delta.ToolCalls {
				if tool.Function.Name != "" {
					name = tool.Function.Name
				}
				arguments += tool.Function.Arguments
			}
		}
	}
	return name, arguments
}

func TestSanitizeClaudeReadToolArgumentsScope(t *testing.T) {
	assert.JSONEq(t,
		`{"file_path":"/tmp/demo.py","limit":2000}`,
		SanitizeClaudeReadToolArguments("Read", `{"file_path":"/tmp/demo.py","limit":2000,"pages":""}`),
	)
	assert.JSONEq(t,
		`{"pages":"1-2"}`,
		SanitizeClaudeReadToolArguments("Read", `{"pages":"1-2"}`),
	)
	assert.Equal(t,
		`{"pages":""}`,
		SanitizeClaudeReadToolArguments("OtherTool", `{"pages":""}`),
	)
	assert.Equal(t, `{"pages":""`, SanitizeClaudeReadToolArguments("Read", `{"pages":""`))
}

func TestSanitizeClaudeReadToolArgumentsInResponsesOutputScope(t *testing.T) {
	outputs := []dto.ResponsesOutput{
		{
			Type:      responsesOutputTypeFunctionCall,
			Name:      "Read",
			Arguments: rawToolArguments(t, `{"file_path":"/tmp/demo.py","pages":""}`),
		},
		{
			Type:      responsesOutputTypeCustomToolCall,
			Name:      "Read",
			Arguments: rawToolArguments(t, `{"pages":""}`),
		},
	}

	SanitizeClaudeReadToolArgumentsInResponsesOutput(outputs)
	assert.JSONEq(t, `{"file_path":"/tmp/demo.py"}`, outputs[0].ArgumentsString())
	assert.JSONEq(t, `{"pages":""}`, outputs[1].ArgumentsString())
}

func TestResponsesResponseToClaudeMessagesResponseSanitizesReadArguments(t *testing.T) {
	response := &dto.OpenAIResponsesResponse{
		ID: "resp_1",
		Output: []dto.ResponsesOutput{{
			Type:      responsesOutputTypeFunctionCall,
			ID:        "fc_1",
			CallId:    "call_1",
			Name:      "Read",
			Arguments: rawToolArguments(t, `{"file_path":"/tmp/demo.py","pages":""}`),
		}},
	}

	converted, _, err := ResponsesResponseToClaudeMessagesResponse(response)
	require.NoError(t, err)
	require.Len(t, converted.Content, 1)
	assert.Equal(t, map[string]any{"file_path": "/tmp/demo.py"}, converted.Content[0].Input)
}

func TestResponsesToChatStreamBuffersSplitClaudeReadArguments(t *testing.T) {
	state := NewResponsesToChatStreamState("gpt-test", false)
	state.EnableClaudeReadToolCompatibility()
	var chunks []dto.ChatCompletionsStreamResponse

	added, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
		Type: responsesEventOutputItemAdded,
		Item: &dto.ResponsesOutput{
			ID:        "fc_1",
			CallId:    "call_1",
			Type:      responsesOutputTypeFunctionCall,
			Name:      "Read",
			Arguments: rawToolArguments(t, ""),
		},
	}, state)
	require.NoError(t, err)
	chunks = append(chunks, added...)

	for _, delta := range []string{
		`{"file_path":"/tmp/demo.py","limit":2000`,
		`,"offset":0,"pa`,
		`ges":""}`,
	} {
		converted, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
			Type:   responsesEventFunctionArgsDelta,
			ItemID: "fc_1",
			Delta:  delta,
		}, state)
		require.NoError(t, err)
		chunks = append(chunks, converted...)
	}

	done, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
		Type:   responsesEventFunctionArgsDone,
		ItemID: "fc_1",
	}, state)
	require.NoError(t, err)
	chunks = append(chunks, done...)

	name, arguments := collectStreamToolArguments(chunks)
	assert.Equal(t, "Read", name)
	assert.JSONEq(t, `{"file_path":"/tmp/demo.py","limit":2000,"offset":0}`, arguments)
}

func TestResponsesToChatStreamBuffersClaudeReadArgumentsBeforeName(t *testing.T) {
	state := NewResponsesToChatStreamState("gpt-test", false)
	state.EnableClaudeReadToolCompatibility()
	var chunks []dto.ChatCompletionsStreamResponse

	pending, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
		Type:   responsesEventFunctionArgsDelta,
		ItemID: "fc_1",
		Delta:  `{"file_path":"/tmp/demo.py","pages":""}`,
	}, state)
	require.NoError(t, err)
	require.Empty(t, pending)

	added, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
		Type: responsesEventOutputItemAdded,
		Item: &dto.ResponsesOutput{
			ID:     "fc_1",
			CallId: "call_1",
			Type:   responsesOutputTypeFunctionCall,
			Name:   "Read",
		},
	}, state)
	require.NoError(t, err)
	chunks = append(chunks, added...)
	chunks = append(chunks, FinalizeResponsesToChatStream(state)...)

	name, arguments := collectStreamToolArguments(chunks)
	assert.Equal(t, "Read", name)
	assert.JSONEq(t, `{"file_path":"/tmp/demo.py"}`, arguments)
}

func TestResponsesToChatStreamClaudeReadDoneArgumentsOverrideAndFallback(t *testing.T) {
	tests := []struct {
		name           string
		deltaArguments string
		finalArguments string
		wantArguments  string
	}{
		{
			name:           "valid final overrides deltas",
			deltaArguments: `{"file_path":"/tmp/old.py","pages":""}`,
			finalArguments: `{"file_path":"/tmp/final.py","limit":2000,"pages":""}`,
			wantArguments:  `{"file_path":"/tmp/final.py","limit":2000}`,
		},
		{
			name:           "malformed final falls back to deltas",
			deltaArguments: `{"file_path":"/tmp/good.py","pages":""}`,
			finalArguments: `{"file_path":"/tmp/bad.py","pages":""`,
			wantArguments:  `{"file_path":"/tmp/good.py"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewResponsesToChatStreamState("gpt-test", false)
			state.EnableClaudeReadToolCompatibility()
			var chunks []dto.ChatCompletionsStreamResponse

			added, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
				Type: responsesEventOutputItemAdded,
				Item: &dto.ResponsesOutput{
					ID:     "fc_1",
					CallId: "call_1",
					Type:   responsesOutputTypeFunctionCall,
					Name:   "Read",
				},
			}, state)
			require.NoError(t, err)
			chunks = append(chunks, added...)

			delta, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
				Type:   responsesEventFunctionArgsDelta,
				ItemID: "fc_1",
				Delta:  tt.deltaArguments,
			}, state)
			require.NoError(t, err)
			chunks = append(chunks, delta...)

			done, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
				Type:      responsesEventFunctionArgsDone,
				ItemID:    "fc_1",
				Arguments: kitutil.GetPointer(tt.finalArguments),
			}, state)
			require.NoError(t, err)
			chunks = append(chunks, done...)

			_, arguments := collectStreamToolArguments(chunks)
			assert.JSONEq(t, tt.wantArguments, arguments)
		})
	}
}

func TestResponsesToChatStreamPreservesOtherToolArguments(t *testing.T) {
	state := NewResponsesToChatStreamState("gpt-test", false)
	state.EnableClaudeReadToolCompatibility()
	var chunks []dto.ChatCompletionsStreamResponse

	added, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
		Type: responsesEventOutputItemAdded,
		Item: &dto.ResponsesOutput{
			ID:     "fc_1",
			CallId: "call_1",
			Type:   responsesOutputTypeFunctionCall,
			Name:   "OtherTool",
		},
	}, state)
	require.NoError(t, err)
	chunks = append(chunks, added...)

	delta, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{
		Type:   responsesEventFunctionArgsDelta,
		ItemID: "fc_1",
		Delta:  `{"pages":""}`,
	}, state)
	require.NoError(t, err)
	chunks = append(chunks, delta...)

	name, arguments := collectStreamToolArguments(chunks)
	assert.Equal(t, "OtherTool", name)
	assert.Equal(t, `{"pages":""}`, arguments)
}
