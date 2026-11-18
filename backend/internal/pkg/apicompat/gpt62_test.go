package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGPT62ConvertersPreserveReasoningAndRejectDisabled(t *testing.T) {
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		for _, effort := range []string{"low", "medium", "high", "xhigh", "max", "ultra", "none", "minimal"} {
			t.Run(model+"/"+effort, func(t *testing.T) {
				chat, chatErr := ChatCompletionsToResponses(&ChatCompletionsRequest{Model: model, ReasoningEffort: effort, Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}}})
				messages, messagesErr := AnthropicToResponses(&AnthropicRequest{Model: model, OutputConfig: &AnthropicOutputConfig{Effort: effort}, Messages: []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}}})
				if effort == "none" || effort == "minimal" {
					require.Error(t, chatErr)
					require.Error(t, messagesErr)
					return
				}
				require.NoError(t, chatErr)
				require.NoError(t, messagesErr)
				require.Equal(t, effort, chat.Reasoning.Effort)
				require.Equal(t, effort, messages.Reasoning.Effort)
			})
		}
		_, err := AnthropicToResponses(&AnthropicRequest{Model: model, Thinking: &AnthropicThinking{Type: "disabled"}})
		require.Error(t, err)
	}
}
