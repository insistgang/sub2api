package openai

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGPT61TerraIdentityReasoningAndInstructions(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6.1-terra")
	for _, id := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra-max", "GPT_6.1_TERRA", "gpt-6.1-terra-none", "gpt-6.1-terra-openai-compact"} {
		require.True(t, IsGPT61TerraModelSpelling(id), id)
	}
	for _, id := range []string{"gpt-6.1", "gpt-6.1-terrain", "gpt-6.1-terra-preview", "gpt-5.6-terra"} {
		require.False(t, IsGPT61TerraModelSpelling(id), id)
	}
	for _, effort := range []string{"", "none", "low", "medium", "high", "xhigh", "max"} {
		require.NoError(t, ValidateGPT61TerraReasoningEffort("gpt-6.1-terra", effort))
	}
	for _, effort := range []string{"minimal", "ultra", "off", "invalid"} {
		require.Error(t, ValidateGPT61TerraReasoningEffort("gpt-6.1-terra", effort))
		require.NoError(t, ValidateGPT61TerraReasoningEffort("gpt-6.1-sol", effort))
	}
	var metadata struct {
		ModelMessages struct {
			InstructionsTemplate string `json:"instructions_template"`
		} `json:"model_messages"`
	}
	require.NoError(t, json.Unmarshal(CodexGPT61TerraMetadata, &metadata))
	require.NotEmpty(t, metadata.ModelMessages.InstructionsTemplate)
	require.Equal(t, metadata.ModelMessages.InstructionsTemplate, CodexBaseInstructionsForModel("gpt-6.1-terra"))
	require.NotEqual(t, CodexBaseInstructionsForModel("gpt-6-astra"), CodexBaseInstructionsForModel("gpt-6.1-terra"))
}
