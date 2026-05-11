package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestSanitizeInvalidClaudeThinkingBlocksJSONDropsOnlyInvalidThinking(t *testing.T) {
	raw := []byte(`{"model":"claude-opus-4-7","extra":true,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"thinking","thinking":"bad","signature":""},{"type":"thinking","thinking":"good","signature":"sig_ok"},{"type":"redacted_thinking","data":"abc"},{"type":"text","text":"answer"}]},{"role":"assistant","content":[{"type":"thinking","thinking":{"text":"bad"},"signature":"sig_bad"}]}]}`)

	sanitized, changed, err := sanitizeInvalidClaudeThinkingBlocksJSON(raw)
	if err != nil {
		t.Fatalf("sanitizeInvalidClaudeThinkingBlocksJSON returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected invalid thinking blocks to be removed")
	}

	var got map[string]any
	if err := common.Unmarshal(sanitized, &got); err != nil {
		t.Fatalf("unmarshal sanitized request: %v", err)
	}
	if got["extra"] != true {
		t.Fatalf("sanitizer dropped unrelated request fields: %#v", got)
	}

	messages := got["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected empty assistant thinking-only message to be removed, got %d messages", len(messages))
	}
	assistant := messages[1].(map[string]any)
	content := assistant["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("expected one invalid thinking block removed, got content: %#v", content)
	}
	if content[0].(map[string]any)["signature"] != "sig_ok" {
		t.Fatalf("valid signed thinking block was not preserved: %#v", content)
	}
}
