package apicompat

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGPT61TerraChatReasoningAndSampling(t *testing.T) {
	sampling := 0.7
	for _, effort := range []string{"", "none", "low", "medium", "high", "xhigh", "max"} {
		out, err := ChatCompletionsToResponses(&ChatCompletionsRequest{Model: "gpt-6.1-terra", ReasoningEffort: effort, Temperature: &sampling, TopP: &sampling, Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}}})
		require.NoError(t, err)
		if effort == "none" {
			require.NotNil(t, out.Temperature)
			require.NotNil(t, out.TopP)
		} else {
			require.Nil(t, out.Temperature)
			require.Nil(t, out.TopP)
		}
		if effort != "" {
			require.Equal(t, effort, out.Reasoning.Effort)
		}
	}
	for _, effort := range []string{"minimal", "ultra", "invalid"} {
		_, err := ChatCompletionsToResponses(&ChatCompletionsRequest{Model: "gpt-6.1-terra", ReasoningEffort: effort})
		require.Error(t, err)
	}
}
