//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGPT62AliasesAndReasoningIntent(t *testing.T) {
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		for _, alias := range []string{model, "openai/" + model, strings.ToUpper(strings.ReplaceAll(model, "-", "_")), model + "-openai-compact"} {
			require.Equal(t, model, normalizeKnownOpenAICodexModel(alias), alias)
			require.Equal(t, model, normalizeCodexModel(alias), alias)
			require.Equal(t, model, NormalizeOpenAICompatRequestedModel(alias), alias)
			require.True(t, shouldAutoInjectPromptCacheKeyForCompat(alias), alias)
		}
		for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
			req := &apicompat.AnthropicRequest{Model: "openai/" + model + "-" + effort}
			applyOpenAICompatModelNormalization(req)
			require.Equal(t, model, req.Model)
			require.NotNil(t, req.OutputConfig)
			require.Equal(t, effort, req.OutputConfig.Effort)
			if effort != "none" && effort != "minimal" {
				require.Equal(t, effort, normalizeOpenAIReasoningEffortForModel(effort, model))
			}
		}
		for _, body := range []string{`{"reasoning":{"effort":"none"}}`, `{"reasoning_effort":"minimal"}`, `{"output_config":{"effort":"none"}}`, `{"thinking":{"type":"disabled"}}`, `{"model":"` + model + `-minimal"}`} {
			require.Error(t, validateGPT61SolCompatRequest([]byte(body), model))
		}
		body := []byte(`{"model":"public","reasoning":{"mode":"pro","effort":"ultra"},"temperature":0.5,"top_p":0.9,"logprobs":true,"include":["message.output_text.logprobs","reasoning.encrypted_content"]}`)
		out, _, err := normalizeOpenAIResponsesReasoningMode(body, model)
		require.NoError(t, err)
		require.Equal(t, "pro", gjson.GetBytes(out, "reasoning.mode").String())
		require.Equal(t, "ultra", gjson.GetBytes(out, "reasoning.effort").String())
		require.False(t, gjson.GetBytes(out, "temperature").Exists())
		require.False(t, gjson.GetBytes(out, "logprobs").Exists())
	}
	for _, invalid := range []string{"gpt-6.2", "gpt-6.2-solitude", "gpt-6.2-astra-preview", "gpt-6.2-sol-invalid"} {
		require.Empty(t, normalizeKnownOpenAICodexModel(invalid))
	}
}

func TestGPT62CodexMetadataAndPinnedProjection(t *testing.T) {
	for model, window := range map[string]int64{"gpt-6.2-astra": 272000, "gpt-6.2-sol": 500000} {
		descriptor := newConfiguredCodexModelDescriptor(model)
		require.Equal(t, window, descriptor.ContextWindow)
		require.EqualValues(t, 872000, descriptor.MaxContextWindow)
		require.Equal(t, "low", *descriptor.DefaultReasoningLevel)
		require.Equal(t, []string{"text", "image"}, descriptor.InputModalities)
		encoded, err := json.Marshal(descriptor)
		require.NoError(t, err)
		require.EqualValues(t, 128000, gjson.GetBytes(encoded, "max_output_tokens").Int())
		require.Equal(t, "freeform", gjson.GetBytes(encoded, "apply_patch_tool_type").String())
		require.Equal(t, "3000", gjson.GetBytes(encoded, "comp_hash").String())
		require.Equal(t, []string{"low", "medium", "high", "xhigh", "max", "ultra"}, effortsFromManifestModel(t, decodeCodexManifestModels(t, append(append([]byte(`{"models":[`), encoded...), []byte(`]}`)...))[0]))
		live := codexModelMetadataOverride{UpstreamModelMetadata: UpstreamModelMetadata{ContextWindow: 123000, MaxContextWindow: 456000, MaxOutputTokens: 10000}}
		applyUpstreamModelMetadataToCodexDescriptor(&descriptor, live)
		require.EqualValues(t, 123000, descriptor.ContextWindow)
		require.EqualValues(t, 456000, descriptor.MaxContextWindow)
		require.EqualValues(t, 10000, descriptor.MaxOutputTokens)
		oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"base_url": "https://chatgpt.com"}}
		capabilities := accountCodexToolCapabilities(oauth, model)
		require.JSONEq(t, `true`, string(capabilities["use_responses_lite"]))
		require.JSONEq(t, `"code_mode_only"`, string(capabilities["tool_mode"]))
		account := newCodexModelsAPIKeyTestAccount("https://api.openai.com/v1")
		completed, err := completeAPIKeyCodexModelsManifestMetadata([]byte(`{"models":[{"slug":"`+model+`","context_window":123000,"max_context_window":456000,"use_responses_lite":true}]}`), true, account)
		require.NoError(t, err)
		completed, err = adjustAPIKeyCodexModelsManifest(completed, account)
		require.NoError(t, err)
		require.EqualValues(t, 123000, gjson.GetBytes(completed, "models.0.context_window").Int())
		require.False(t, gjson.GetBytes(completed, "models.0.use_responses_lite").Bool())
		response := &OpenAIModelsResponse{Body: completed}
		require.NoError(t, ApplyPinnedCodexModelsMapping(response, account, &Group{Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{model}}}))
		require.Equal(t, model, gjson.GetBytes(response.Body, "models.0.slug").String())
	}
}

func TestGPT62RawChatToolsAndResponsesNoneRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"public": model}}}
		for _, field := range []string{`"reasoning_effort":"none"`, `"tools":[{"type":"function","function":{"name":"lookup"}}]`} {
			body := []byte(`{"model":"public",` + field + `,"messages":[{"role":"user","content":"hello"}]}`)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
			require.Error(t, err)
			require.Equal(t, 400, recorder.Code)
		}
		body := []byte(`{"model":"public","reasoning":{"effort":"none"},"input":"hello"}`)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
		svc := &OpenAIGatewayService{cfg: &config.Config{}}
		_, err := svc.Forward(context.Background(), c, account, body)
		require.Error(t, err)
		require.Equal(t, 400, recorder.Code)
	}
}

func TestGPT62UnsupportedCacheRetentionRejected(t *testing.T) {
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		body := []byte(`{"model":"public","input":"hello","prompt_cache_options":{"ttl":"12h"}}`)
		require.ErrorContains(t, validateGPT61SolCompatRequest(body, model), "30m")
		require.NoError(t, validateGPT61SolCompatRequest([]byte(`{"prompt_cache_options":{"ttl":"30m"}}`), model))
		account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"public": model}}}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
		svc := &OpenAIGatewayService{cfg: &config.Config{}}
		_, err := svc.Forward(context.Background(), c, account, body)
		require.ErrorContains(t, err, "30m")
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	}
	// A model-specific billing limitation must not change older model semantics.
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol"} {
		require.NoError(t, validateGPT61SolCompatRequest([]byte(`{"prompt_cache_options":{"ttl":"12h"}}`), model))
	}
}

func TestGPT62SyncedLimitsOverrideGeneratedDefaults(t *testing.T) {
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		account := newCodexModelsAPIKeyTestAccount("https://api.openai.com/v1")
		account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
			model: {ID: model, MaxContextWindow: 200000, MaxOutputTokens: 32000},
		}})
		generated, err := BuildCodexModelsManifest([]string{model})
		require.NoError(t, err)
		synced, err := applySyncedAPIKeyCodexModelMetadata(generated, account, true)
		require.NoError(t, err)
		require.EqualValues(t, 200000, gjson.GetBytes(synced, "models.0.context_window").Int())
		require.EqualValues(t, 200000, gjson.GetBytes(synced, "models.0.max_context_window").Int())
		require.EqualValues(t, 32000, gjson.GetBytes(synced, "models.0.max_output_tokens").Int())
	}
}

func TestGPT62ManifestSearchFallbackScope(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "custom-model", "gpt-6.2-astra", "gpt-6.2-sol"} {
		for _, tc := range []struct {
			name   string
			search map[string]bool
			want   bool
		}{
			{"nil map", nil, model == "gpt-6.2-astra" || model == "gpt-6.2-sol"},
			{"explicit false", map[string]bool{model: false}, false},
			{"explicit true", map[string]bool{model: true}, true},
		} {
			t.Run(model+"/"+tc.name, func(t *testing.T) {
				body, err := buildCodexModelsManifest([]string{model}, nil, tc.search, nil, nil)
				require.NoError(t, err)
				require.Equal(t, tc.want, gjson.GetBytes(body, "models.0.supports_search_tool").Bool())
			})
		}
		live := map[string]codexModelMetadataOverride{model: {UpstreamModelMetadata: UpstreamModelMetadata{
			CodexToolCapabilities: map[string]json.RawMessage{"supports_search_tool": json.RawMessage("true")},
		}}}
		body, err := buildCodexModelsManifest([]string{model}, nil, map[string]bool{model: false}, nil, live)
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(body, "models.0.supports_search_tool").Bool(), "explicit upstream metadata remains authoritative")
	}
}
