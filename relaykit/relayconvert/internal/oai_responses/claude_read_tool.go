package oairesponses

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const claudeReadToolName = "Read"

func SanitizeClaudeReadToolArguments(name string, arguments string) string {
	if name != claudeReadToolName || arguments == "" {
		return arguments
	}

	input, ok := parseJSONObject(arguments)
	if !ok {
		return arguments
	}
	pages, exists := input["pages"]
	if !exists {
		return arguments
	}
	var pageRange string
	if err := kitutil.Unmarshal(pages, &pageRange); err != nil || pageRange != "" {
		return arguments
	}

	delete(input, "pages")
	sanitized, err := kitutil.Marshal(input)
	if err != nil {
		return arguments
	}
	return string(sanitized)
}

func ChooseClaudeReadToolArguments(name string, primary string, fallback string) string {
	if name != claudeReadToolName {
		if primary != "" {
			return primary
		}
		return fallback
	}
	if _, ok := parseJSONObject(primary); ok {
		return SanitizeClaudeReadToolArguments(name, primary)
	}
	if _, ok := parseJSONObject(fallback); ok {
		return SanitizeClaudeReadToolArguments(name, fallback)
	}
	if primary != "" {
		return primary
	}
	return fallback
}

func SanitizeClaudeReadToolArgumentsInResponsesOutput(outputs []dto.ResponsesOutput) {
	for i := range outputs {
		output := &outputs[i]
		name := strings.TrimSpace(output.Name)
		if output.Type != responsesOutputTypeFunctionCall || name != claudeReadToolName {
			continue
		}
		arguments := output.ArgumentsString()
		sanitized := SanitizeClaudeReadToolArguments(name, arguments)
		if sanitized == arguments {
			continue
		}
		quoted, err := kitutil.Marshal(sanitized)
		if err == nil {
			output.Arguments = json.RawMessage(quoted)
		}
	}
}

func parseJSONObject(arguments string) (map[string]json.RawMessage, bool) {
	if arguments == "" {
		return nil, false
	}
	var input map[string]json.RawMessage
	if err := kitutil.Unmarshal([]byte(arguments), &input); err != nil || input == nil {
		return nil, false
	}
	return input, true
}
