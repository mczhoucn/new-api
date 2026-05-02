package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

func TestResponseText2UsageFillsResponsesStyleTokenFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	usage := ResponseText2Usage(c, "hello world", "gpt-5.5", 7)
	if usage.PromptTokens != usage.InputTokens {
		t.Fatalf("expected input_tokens to match prompt_tokens, got %#v", usage)
	}
	if usage.CompletionTokens != usage.OutputTokens {
		t.Fatalf("expected output_tokens to match completion_tokens, got %#v", usage)
	}
	if usage.TotalTokens != usage.InputTokens+usage.OutputTokens {
		t.Fatalf("expected total_tokens to match input+output tokens, got %#v", usage)
	}
}

func TestBuildClaudeUsageFromOpenAIUsageReadsResponsesStyleTokenFields(t *testing.T) {
	claudeUsage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		InputTokens:  11,
		OutputTokens: 5,
		TotalTokens:  16,
	})
	if claudeUsage == nil {
		t.Fatalf("expected Claude usage")
	}
	if claudeUsage.InputTokens != 11 || claudeUsage.OutputTokens != 5 {
		t.Fatalf("expected Claude usage to use input/output token fields, got %#v", claudeUsage)
	}
}
