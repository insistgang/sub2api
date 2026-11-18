package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGPT62CatalogReasoningAndToolProtocol(t *testing.T) {
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		t.Run(model, func(t *testing.T) {
			require.Contains(t, DefaultModelIDs(), model)
			require.True(t, RequiresResponsesForToolCalls(model))
			for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max", "ultra"} {
				require.NoError(t, ValidateModelReasoningEffort(model, effort))
			}
			for _, effort := range []string{"none", "minimal", "invalid"} {
				require.Error(t, ValidateModelReasoningEffort(model, effort))
			}
			for _, effort := range []string{"none", "minimal"} {
				require.Error(t, ValidateModelReasoningEffort("openai/"+model+"-"+effort, ""))
			}
			require.NotContains(t, CodexBaseInstructionsForModel(model), "based on GPT-5")
		})
	}
}
