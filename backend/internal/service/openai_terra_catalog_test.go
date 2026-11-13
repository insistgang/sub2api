//go:build unit

package service

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGPT61TerraCatalogAndAliases(t *testing.T) {
	for _, id := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra-max", "GPT_6.1_TERRA", "gpt-6.1-terra-none", "gpt-6.1-terra-openai-compact"} {
		require.Equal(t, "gpt-6.1-terra", normalizeKnownOpenAICodexModel(id))
		require.Equal(t, "gpt-6.1-terra", normalizeCodexModel(id))
		require.True(t, shouldAutoInjectPromptCacheKeyForCompat(id))
	}
	body, err := BuildCodexModelsManifest([]string{"gpt-6.1-terra"})
	require.NoError(t, err)
	models := decodeCodexManifestModels(t, body)
	require.Len(t, models, 1)
	m := models[0]
	require.Equal(t, "GPT-6.1 Terra", m["display_name"])
	require.EqualValues(t, 500000, m["context_window"])
	require.EqualValues(t, 872000, m["max_context_window"])
	require.Equal(t, "low", m["default_reasoning_level"])
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max", "ultra"}, effortsFromManifestModel(t, m))
	var original map[string]any
	require.NoError(t, json.Unmarshal(openai.CodexGPT61TerraMetadata, &original))
	for key, value := range original["model_messages"].(map[string]any) {
		require.Equal(t, value, m["model_messages"].(map[string]any)[key], key)
	}
	account := newCodexModelsAPIKeyTestAccount("https://api.openai.com/v1")
	completed, err := completeAPIKeyCodexModelsManifestMetadata([]byte(`{"models":[{"slug":"gpt-6.1-terra"}]}`), true, account)
	require.NoError(t, err)
	apiModels := decodeCodexManifestModels(t, completed)
	require.Equal(t, false, apiModels[0]["use_responses_lite"])
	require.EqualValues(t, 500000, apiModels[0]["context_window"])
}
