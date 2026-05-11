package openai

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func responsesStreamIndexKey(itemID string, idx *int) string {
	if itemID == "" {
		return ""
	}
	if idx == nil {
		return itemID
	}
	return fmt.Sprintf("%s:%d", itemID, *idx)
}

func stringDeltaFromPrefix(prev string, next string) string {
	if next == "" {
		return ""
	}
	if prev != "" && strings.HasPrefix(next, prev) {
		return next[len(prev):]
	}
	return next
}

func applyResponsesUsage(dst *dto.Usage, src *dto.Usage) {
	if dst == nil || src == nil {
		return
	}
	if src.InputTokens != 0 {
		dst.PromptTokens = src.InputTokens
		dst.InputTokens = src.InputTokens
	} else if src.PromptTokens != 0 {
		dst.PromptTokens = src.PromptTokens
		dst.InputTokens = src.PromptTokens
	}
	if src.OutputTokens != 0 {
		dst.CompletionTokens = src.OutputTokens
		dst.OutputTokens = src.OutputTokens
	} else if src.CompletionTokens != 0 {
		dst.CompletionTokens = src.CompletionTokens
		dst.OutputTokens = src.CompletionTokens
	}
	if src.TotalTokens != 0 {
		dst.TotalTokens = src.TotalTokens
	} else {
		dst.TotalTokens = dst.PromptTokens + dst.CompletionTokens
	}
	if src.InputTokensDetails != nil {
		dst.PromptTokensDetails.CachedTokens = src.InputTokensDetails.CachedTokens
		dst.PromptTokensDetails.ImageTokens = src.InputTokensDetails.ImageTokens
		dst.PromptTokensDetails.AudioTokens = src.InputTokensDetails.AudioTokens
	}
	if src.PromptTokensDetails.CachedTokens != 0 {
		dst.PromptTokensDetails.CachedTokens = src.PromptTokensDetails.CachedTokens
	}
	if src.PromptTokensDetails.CachedCreationTokens != 0 {
		dst.PromptTokensDetails.CachedCreationTokens = src.PromptTokensDetails.CachedCreationTokens
	}
	if src.CompletionTokenDetails.ReasoningTokens != 0 {
		dst.CompletionTokenDetails.ReasoningTokens = src.CompletionTokenDetails.ReasoningTokens
	}
}

// OaiResponsesStreamToChatHandler aggregates an upstream SSE /v1/responses stream into a single
// non-streaming /v1/chat/completions JSON response. Used when the client requested non-streaming
// but the upstream channel only supports (or was forced to use) streaming.
func OaiResponsesStreamToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	chatID := helper.GetResponseID(c)
	responsesResp := &dto.OpenAIResponsesResponse{}
	outputTextByIndex := make(map[int]string) // content_index → accumulated text
	toolCallArgsByID := make(map[string]string)
	toolCallNameByID := make(map[string]string)
	toolCallCanonicalIDByItemID := make(map[string]string)
	var usageText strings.Builder
	var sawTerminal bool  // P1: track response.completed / response.incomplete
	finishReason := "stop"

	handleEvent := func(data string) *types.NewAPIError {
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			return nil
		}
		if !strings.HasPrefix(data, "{") {
			return nil
		}
		var ev dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &ev); err != nil {
			logger.LogWarn(c, "skip invalid responses stream event: "+err.Error())
			return nil
		}
		switch ev.Type {
		case "response.created":
			if ev.Response != nil {
				if ev.Response.ID != "" {
					responsesResp.ID = ev.Response.ID
				}
				if ev.Response.Object != "" {
					responsesResp.Object = ev.Response.Object
				}
				if ev.Response.Model != "" {
					responsesResp.Model = ev.Response.Model
				}
				if ev.Response.CreatedAt != 0 {
					responsesResp.CreatedAt = ev.Response.CreatedAt
				}
			}

		case "response.output_text.delta":
			idx := 0
			if ev.ContentIndex != nil && *ev.ContentIndex >= 0 {
				idx = *ev.ContentIndex
			}
			outputTextByIndex[idx] += ev.Delta
			usageText.WriteString(ev.Delta)

		case "response.output_item.added", "response.output_item.done":
			if ev.Item == nil {
				break
			}
			if ev.Item.Type == "function_call" {
				itemID := strings.TrimSpace(ev.Item.ID)
				callID := strings.TrimSpace(ev.Item.CallId)
				if callID == "" {
					callID = itemID
				}
				if itemID != "" && callID != "" {
					toolCallCanonicalIDByItemID[itemID] = callID
				}
				if name := strings.TrimSpace(ev.Item.Name); name != "" {
					toolCallNameByID[callID] = name
					usageText.WriteString(name)
				}
				if args := ev.Item.ArgumentsString(); args != "" {
					toolCallArgsByID[callID] = args
					usageText.WriteString(args)
				}
			}

		case "response.function_call_arguments.delta":
			itemID := strings.TrimSpace(ev.ItemID)
			callID := toolCallCanonicalIDByItemID[itemID]
			if callID == "" {
				callID = itemID
			}
			if callID != "" {
				toolCallArgsByID[callID] += ev.Delta
				usageText.WriteString(ev.Delta)
			}

		case "response.function_call_arguments.done":
			// final/corrected arguments — overwrite any accumulated delta
			itemID := strings.TrimSpace(ev.ItemID)
			callID := toolCallCanonicalIDByItemID[itemID]
			if callID == "" {
				callID = itemID
			}
			if callID != "" && ev.Delta != "" {
				toolCallArgsByID[callID] = ev.Delta
			}

		case "response.completed", "response.incomplete":
			sawTerminal = true
			if ev.Type == "response.incomplete" {
				finishReason = "length"
			}
			if ev.Response == nil {
				break
			}
			r := ev.Response
			if r.ID != "" {
				responsesResp.ID = r.ID
			}
			if r.Object != "" {
				responsesResp.Object = r.Object
			}
			if r.Model != "" {
				responsesResp.Model = r.Model
			}
			if r.CreatedAt != 0 {
				responsesResp.CreatedAt = r.CreatedAt
			}
			if r.Usage != nil {
				responsesResp.Usage = r.Usage
			}
			// Prefer the completed snapshot output; fall back to incrementally built output.
			if len(r.Output) > 0 {
				responsesResp.Output = r.Output
				// Patch in any streamed args that arrived via delta events.
				for i := range responsesResp.Output {
					out := &responsesResp.Output[i]
					if out.Type != "function_call" {
						continue
					}
					callID := strings.TrimSpace(out.CallId)
					if callID == "" {
						callID = strings.TrimSpace(out.ID)
					}
					if name := toolCallNameByID[callID]; out.Name == "" && name != "" {
						out.Name = name
					}
					if args := toolCallArgsByID[callID]; out.ArgumentsString() == "" && args != "" {
						// store as JSON string literal
						quoted, _ := common.Marshal(args)
						out.Arguments = quoted
					}
				}
			}

		case "error":
			// top-level error event from OpenAI Responses streaming API
			if ev.Error != nil && ev.Error.Message != "" {
				oaiErr := types.OpenAIError{
					Type:    ev.Error.Type,
					Code:    ev.Error.Code,
					Message: ev.Error.Message,
					Param:   ev.Error.Param,
				}
				return types.WithOpenAIError(oaiErr, http.StatusInternalServerError)
			}
			return types.NewOpenAIError(fmt.Errorf("responses stream error event"), types.ErrorCodeBadResponse, http.StatusInternalServerError)

		case "response.error", "response.failed":
			if ev.Response != nil {
				if oaiErr := ev.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Type != "" {
					return types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
				}
			}
			return types.NewOpenAIError(fmt.Errorf("responses stream error: %s", ev.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		return nil
	}

	reqCtx := c.Request.Context()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, helper.InitialScannerBufferSize), helper.DefaultMaxScannerBufferSize)
	var dataLines []string
	for scanner.Scan() {
		select {
		case <-reqCtx.Done():
			return nil, types.NewOpenAIError(reqCtx.Err(), types.ErrorCodeBadResponse, http.StatusGatewayTimeout)
		default:
		}
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(dataLines) > 0 {
				if apiErr := handleEvent(strings.Join(dataLines, "\n")); apiErr != nil {
					return nil, apiErr
				}
				dataLines = dataLines[:0]
			}
			continue
		}
		if strings.HasPrefix(trimmed, ":") || strings.HasPrefix(trimmed, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			if len(dataLines) > 0 {
				if apiErr := handleEvent(strings.Join(dataLines, "\n")); apiErr != nil {
					return nil, apiErr
				}
				dataLines = dataLines[:0]
			}
			break
		}
		dataLines = append(dataLines, data)
	}
	if len(dataLines) > 0 {
		if apiErr := handleEvent(strings.Join(dataLines, "\n")); apiErr != nil {
			return nil, apiErr
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	// P1: reject truncated streams that never delivered a terminal event.
	if !sawTerminal {
		logger.LogError(c, "responses stream (non-stream aggregation) ended without terminal event")
		return nil, types.NewOpenAIError(fmt.Errorf("responses stream ended without completion event"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	// Fill in defaults for fields not set via stream events.
	if responsesResp.Object == "" {
		responsesResp.Object = "response"
	}
	if responsesResp.CreatedAt == 0 {
		responsesResp.CreatedAt = int(time.Now().Unix())
	}
	if responsesResp.Model == "" {
		responsesResp.Model = info.UpstreamModelName
	}

	// If the completed event had no Output, synthesize from accumulated deltas.
	if len(responsesResp.Output) == 0 {
		// P3: synthesize function_call outputs from delta maps.
		for callID, name := range toolCallNameByID {
			if name == "" {
				continue
			}
			args := toolCallArgsByID[callID]
			quoted, _ := common.Marshal(args)
			responsesResp.Output = append(responsesResp.Output, dto.ResponsesOutput{
				Type:      "function_call",
				CallId:    callID,
				Name:      name,
				Arguments: quoted,
			})
		}
		// synthesize text output if no tool calls were found.
		if len(responsesResp.Output) == 0 && len(outputTextByIndex) > 0 {
			combined := strings.Builder{}
			for i := 0; ; i++ {
				t, ok := outputTextByIndex[i]
				if !ok {
					break
				}
				combined.WriteString(t)
			}
			if combined.Len() > 0 {
				responsesResp.Output = []dto.ResponsesOutput{
					{
						Type: "message",
						Role: "assistant",
						Content: []dto.ResponsesOutputContent{
							{Type: "output_text", Text: combined.String()},
						},
					},
				}
			}
		}
	}

	chatResp, usage, err := service.ResponsesResponseToChatCompletionsResponse(responsesResp, chatID)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	// P2: propagate length finish_reason for incomplete streams.
	if finishReason == "length" && len(chatResp.Choices) > 0 && chatResp.Choices[0].FinishReason == "stop" {
		chatResp.Choices[0].FinishReason = "length"
	}
	if usage == nil || usage.TotalTokens == 0 {
		text := service.ExtractOutputTextFromResponses(responsesResp)
		if text == "" {
			text = usageText.String()
		}
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		chatResp.Usage = *usage
	}

	var responseBody []byte
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		claudeResp := service.ResponseOpenAI2Claude(chatResp, info)
		responseBody, err = common.Marshal(claudeResp)
	case types.RelayFormatGemini:
		geminiResp := service.ResponseOpenAI2Gemini(chatResp, info)
		responseBody, err = common.Marshal(geminiResp)
	default:
		responseBody, err = common.Marshal(chatResp)
	}
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	resp.Header.Del("Transfer-Encoding")
	resp.Header.Set("Content-Type", "application/json")
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiResponsesToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var responsesResp dto.OpenAIResponsesResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	if err := common.Unmarshal(body, &responsesResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := responsesResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	chatId := helper.GetResponseID(c)
	chatResp, usage, err := service.ResponsesResponseToChatCompletionsResponse(&responsesResp, chatId)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if usage == nil || usage.TotalTokens == 0 {
		text := service.ExtractOutputTextFromResponses(&responsesResp)
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		chatResp.Usage = *usage
	}

	var responseBody []byte
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		claudeResp := service.ResponseOpenAI2Claude(chatResp, info)
		responseBody, err = common.Marshal(claudeResp)
	case types.RelayFormatGemini:
		geminiResp := service.ResponseOpenAI2Gemini(chatResp, info)
		responseBody, err = common.Marshal(geminiResp)
	default:
		responseBody, err = common.Marshal(chatResp)
	}
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiResponsesToChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	responseId := helper.GetResponseID(c)
	createAt := time.Now().Unix()
	model := info.UpstreamModelName

	var (
		usage        = &dto.Usage{}
		outputText   strings.Builder
		usageText    strings.Builder
		sentStart    bool
		sentStop     bool
		sawToolCall  bool
		sawCompleted bool
		finishReason = "stop"
		streamErr    *types.NewAPIError
	)

	toolCallIndexByID := make(map[string]int)
	toolCallNameByID := make(map[string]string)
	toolCallArgsByID := make(map[string]string)
	toolCallNameSent := make(map[string]bool)
	toolCallCanonicalIDByItemID := make(map[string]string)
	hasSentReasoningSummary := false
	needsReasoningSummarySeparator := false
	//reasoningSummaryTextByKey := make(map[string]string)

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	}

	sendChatChunk := func(chunk *dto.ChatCompletionsStreamResponse) bool {
		if chunk == nil {
			return true
		}
		if info.RelayFormat == types.RelayFormatOpenAI {
			if err := helper.ObjectData(c, chunk); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		}

		chunkData, err := common.Marshal(chunk)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		if err := HandleStreamFormat(c, info, string(chunkData), false, false); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		return true
	}

	sendStartIfNeeded := func() bool {
		if sentStart {
			return true
		}
		if !sendChatChunk(helper.GenerateStartEmptyResponse(responseId, createAt, model, nil)) {
			return false
		}
		sentStart = true
		return true
	}

	//sendReasoningDelta := func(delta string) bool {
	//	if delta == "" {
	//		return true
	//	}
	//	if !sendStartIfNeeded() {
	//		return false
	//	}
	//
	//	usageText.WriteString(delta)
	//	chunk := &dto.ChatCompletionsStreamResponse{
	//		Id:      responseId,
	//		Object:  "chat.completion.chunk",
	//		Created: createAt,
	//		Model:   model,
	//		Choices: []dto.ChatCompletionsStreamResponseChoice{
	//			{
	//				Index: 0,
	//				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
	//					ReasoningContent: &delta,
	//				},
	//			},
	//		},
	//	}
	//	if err := helper.ObjectData(c, chunk); err != nil {
	//		streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	//		return false
	//	}
	//	return true
	//}

	sendReasoningSummaryDelta := func(delta string) bool {
		if delta == "" {
			return true
		}
		if needsReasoningSummarySeparator {
			if strings.HasPrefix(delta, "\n\n") {
				needsReasoningSummarySeparator = false
			} else if strings.HasPrefix(delta, "\n") {
				delta = "\n" + delta
				needsReasoningSummarySeparator = false
			} else {
				delta = "\n\n" + delta
				needsReasoningSummarySeparator = false
			}
		}
		if !sendStartIfNeeded() {
			return false
		}

		usageText.WriteString(delta)
		chunk := &dto.ChatCompletionsStreamResponse{
			Id:      responseId,
			Object:  "chat.completion.chunk",
			Created: createAt,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ReasoningContent: &delta,
					},
				},
			},
		}
		if !sendChatChunk(chunk) {
			return false
		}
		hasSentReasoningSummary = true
		return true
	}

	sendToolCallDelta := func(callID string, name string, argsDelta string) bool {
		if callID == "" {
			return true
		}
		if info.RelayFormat != types.RelayFormatClaude && outputText.Len() > 0 {
			// Keep non-Claude streaming behavior aligned with the non-stream bridge.
			return true
		}
		if !sendStartIfNeeded() {
			return false
		}

		idx, ok := toolCallIndexByID[callID]
		if !ok {
			idx = len(toolCallIndexByID)
			toolCallIndexByID[callID] = idx
		}
		if name != "" {
			toolCallNameByID[callID] = name
		}
		if toolCallNameByID[callID] != "" {
			name = toolCallNameByID[callID]
		}

		tool := dto.ToolCallResponse{
			ID:   callID,
			Type: "function",
			Function: dto.FunctionResponse{
				Arguments: argsDelta,
			},
		}
		tool.SetIndex(idx)
		if name != "" && !toolCallNameSent[callID] {
			tool.Function.Name = name
			toolCallNameSent[callID] = true
		}

		chunk := &dto.ChatCompletionsStreamResponse{
			Id:      responseId,
			Object:  "chat.completion.chunk",
			Created: createAt,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ToolCalls: []dto.ToolCallResponse{tool},
					},
				},
			},
		}
		if !sendChatChunk(chunk) {
			return false
		}
		sawToolCall = true

		// Include tool call data in the local builder for fallback token estimation.
		if tool.Function.Name != "" {
			usageText.WriteString(tool.Function.Name)
		}
		if argsDelta != "" {
			usageText.WriteString(argsDelta)
		}
		return true
	}

	emitCompletedFallbackOutput := func(response *dto.OpenAIResponsesResponse) bool {
		if response == nil || outputText.Len() > 0 || sawToolCall {
			return true
		}

		text := service.ExtractOutputTextFromResponses(response)
		if text != "" {
			if !sendStartIfNeeded() {
				return false
			}
			outputText.WriteString(text)
			usageText.WriteString(text)
			delta := text
			chunk := &dto.ChatCompletionsStreamResponse{
				Id:      responseId,
				Object:  "chat.completion.chunk",
				Created: createAt,
				Model:   model,
				Choices: []dto.ChatCompletionsStreamResponseChoice{
					{
						Index: 0,
						Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
							Content: &delta,
						},
					},
				},
			}
			if !sendChatChunk(chunk) {
				return false
			}
		}

		for i, out := range response.Output {
			if out.Type != "function_call" {
				continue
			}
			name := strings.TrimSpace(out.Name)
			if name == "" {
				continue
			}
			callID := strings.TrimSpace(out.CallId)
			if callID == "" {
				callID = strings.TrimSpace(out.ID)
			}
			if callID == "" {
				callID = fmt.Sprintf("call_%d", i)
			}
			if !sendToolCallDelta(callID, name, out.ArgumentsString()) {
				return false
			}
		}
		return true
	}

	finalizeStream := func(reason string) bool {
		if !sendStartIfNeeded() {
			return false
		}
		if sentStop {
			return true
		}
		if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
			info.ClaudeConvertInfo.Usage = usage
		}
		if sawToolCall {
			reason = "tool_calls"
		}
		stop := helper.GenerateStopResponse(responseId, createAt, model, reason)
		if !sendChatChunk(stop) {
			return false
		}
		sentStop = true
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, "failed to unmarshal responses stream event: "+err.Error())
			sr.Error(err)
			return
		}

		switch streamResp.Type {
		case "response.created":
			if streamResp.Response != nil {
				if streamResp.Response.Model != "" {
					model = streamResp.Response.Model
				}
				if streamResp.Response.CreatedAt != 0 {
					createAt = int64(streamResp.Response.CreatedAt)
				}
			}

		//case "response.reasoning_text.delta":
		//if !sendReasoningDelta(streamResp.Delta) {
		//	sr.Stop(streamErr)
		//	return
		//}

		//case "response.reasoning_text.done":

		case "response.reasoning_summary_text.delta":
			if !sendReasoningSummaryDelta(streamResp.Delta) {
				sr.Stop(streamErr)
				return
			}

		case "response.reasoning_summary_text.done":
			if hasSentReasoningSummary {
				needsReasoningSummarySeparator = true
			}

		//case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		//	key := responsesStreamIndexKey(strings.TrimSpace(streamResp.ItemID), streamResp.SummaryIndex)
		//	if key == "" || streamResp.Part == nil {
		//		break
		//	}
		//	// Only handle summary text parts, ignore other part types.
		//	if streamResp.Part.Type != "" && streamResp.Part.Type != "summary_text" {
		//		break
		//	}
		//	prev := reasoningSummaryTextByKey[key]
		//	next := streamResp.Part.Text
		//	delta := stringDeltaFromPrefix(prev, next)
		//	reasoningSummaryTextByKey[key] = next
		//	if !sendReasoningSummaryDelta(delta) {
		//		sr.Stop(streamErr)
		//		return
		//	}

		case "response.output_text.delta":
			if !sendStartIfNeeded() {
				sr.Stop(streamErr)
				return
			}

			if streamResp.Delta != "" {
				outputText.WriteString(streamResp.Delta)
				usageText.WriteString(streamResp.Delta)
				delta := streamResp.Delta
				chunk := &dto.ChatCompletionsStreamResponse{
					Id:      responseId,
					Object:  "chat.completion.chunk",
					Created: createAt,
					Model:   model,
					Choices: []dto.ChatCompletionsStreamResponseChoice{
						{
							Index: 0,
							Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
								Content: &delta,
							},
						},
					},
				}
				if !sendChatChunk(chunk) {
					sr.Stop(streamErr)
					return
				}
			}

		case "response.output_item.added", "response.output_item.done":
			if streamResp.Item == nil {
				break
			}
			if streamResp.Item.Type != "function_call" {
				break
			}

			itemID := strings.TrimSpace(streamResp.Item.ID)
			callID := strings.TrimSpace(streamResp.Item.CallId)
			if callID == "" {
				callID = itemID
			}
			if itemID != "" && callID != "" {
				toolCallCanonicalIDByItemID[itemID] = callID
			}
			name := strings.TrimSpace(streamResp.Item.Name)
			if name != "" {
				toolCallNameByID[callID] = name
			}

			newArgs := streamResp.Item.ArgumentsString()
			prevArgs := toolCallArgsByID[callID]
			argsDelta := ""
			if newArgs != "" {
				if strings.HasPrefix(newArgs, prevArgs) {
					argsDelta = newArgs[len(prevArgs):]
				} else {
					argsDelta = newArgs
				}
				toolCallArgsByID[callID] = newArgs
			}

			if !sendToolCallDelta(callID, name, argsDelta) {
				sr.Stop(streamErr)
				return
			}

		case "response.function_call_arguments.delta":
			itemID := strings.TrimSpace(streamResp.ItemID)
			callID := toolCallCanonicalIDByItemID[itemID]
			if callID == "" {
				callID = itemID
			}
			if callID == "" {
				break
			}
			toolCallArgsByID[callID] += streamResp.Delta
			if !sendToolCallDelta(callID, "", streamResp.Delta) {
				sr.Stop(streamErr)
				return
			}

		case "response.function_call_arguments.done":

		case "response.completed":
			sawCompleted = true
			finishReason = "stop"
			if streamResp.Response != nil {
				if streamResp.Response.Model != "" {
					model = streamResp.Response.Model
				}
				if streamResp.Response.CreatedAt != 0 {
					createAt = int64(streamResp.Response.CreatedAt)
				}
				if streamResp.Response.Usage != nil {
					applyResponsesUsage(usage, streamResp.Response.Usage)
				}
				if !emitCompletedFallbackOutput(streamResp.Response) {
					streamErr = types.NewOpenAIError(fmt.Errorf("emit completed fallback output failed"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
					sr.Stop(streamErr)
					return
				}
			}

			if !sendStartIfNeeded() {
				sr.Stop(streamErr)
				return
			}
			if !finalizeStream(finishReason) {
				sr.Stop(streamErr)
				return
			}

		case "response.error", "response.failed":
			if streamResp.Response != nil {
				if oaiErr := streamResp.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Type != "" {
					streamErr = types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
					sr.Stop(streamErr)
					return
				}
			}
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return

		case "response.incomplete":
			sawCompleted = true
			finishReason = "length"
			if streamResp.Response != nil {
				if streamResp.Response.Model != "" {
					model = streamResp.Response.Model
				}
				if streamResp.Response.CreatedAt != 0 {
					createAt = int64(streamResp.Response.CreatedAt)
				}
				if streamResp.Response.Usage != nil {
					applyResponsesUsage(usage, streamResp.Response.Usage)
				}
				if !emitCompletedFallbackOutput(streamResp.Response) {
					streamErr = types.NewOpenAIError(fmt.Errorf("emit incomplete fallback output failed"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
					sr.Stop(streamErr)
					return
				}
			}
			if !finalizeStream(finishReason) {
				sr.Stop(streamErr)
				return
			}
			sr.Done()
			return

		default:
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}

	if !sawCompleted {
		logger.LogError(c, "responses stream ended without response.completed event")
		return nil, types.NewOpenAIError(fmt.Errorf("responses stream ended without completion event"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	if usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, usageText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	if !sentStart {
		if !sendChatChunk(helper.GenerateStartEmptyResponse(responseId, createAt, model, nil)) {
			return nil, streamErr
		}
	}
	if !sentStop {
		if !finalizeStream(finishReason) {
			return nil, streamErr
		}
	}
	if info.RelayFormat == types.RelayFormatOpenAI && info.ShouldIncludeUsage && usage != nil {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseId, createAt, model, *usage)); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
	}

	if info.RelayFormat == types.RelayFormatOpenAI {
		helper.Done(c)
	}
	return usage, nil
}
