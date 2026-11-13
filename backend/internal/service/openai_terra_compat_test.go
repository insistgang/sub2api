//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

func TestGPT61TerraReasoningValidationPreservesNoneAndMax(t *testing.T) {
	for _, effort := range []string{"none", "low", "medium", "high", "xhigh", "max"} {
		body := []byte(`{"model":"public","reasoning":{"effort":"` + effort + `"}}`)
		require.NoError(t, validateGPT61SolCompatRequest(body, "gpt-6.1-terra"))
		require.Equal(t, effort, normalizeOpenAIReasoningEffortForModel(effort, "gpt-6.1-terra"))
		req := &apicompat.AnthropicRequest{Model: "openai/gpt-6.1-terra-" + effort}
		applyOpenAICompatModelNormalization(req)
		require.Equal(t, "gpt-6.1-terra", req.Model)
		require.Equal(t, effort, req.OutputConfig.Effort)
	}
	for _, effort := range []string{"minimal", "ultra", "bogus"} {
		for _, field := range []string{`"reasoning":{"effort":"` + effort + `"}`, `"reasoning_effort":"` + effort + `"`, `"output_config":{"effort":"` + effort + `"}`} {
			body := []byte(`{"model":"public",` + field + `}`)
			require.Error(t, validateGPT61SolCompatRequest(body, "gpt-6.1-terra"))
			_, _, err := normalizeOpenAIResponsesReasoningMode(body, "gpt-6.1-terra")
			require.Error(t, err)
		}
	}
}
func TestGPT61TerraChatBridgePreservesToolsImagesAndStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, effort := range []string{"none", "max"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", accountType, effort, stream), func(t *testing.T) {
					body := []byte(fmt.Sprintf(`{"model":"public","stream":%t,"reasoning_effort":"%s","messages":[{"role":"user","content":[{"type":"text","text":"inspect"},{"type":"image_url","image_url":{"url":"https://example.com/photo.png","detail":"high"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}]}`, stream, effort))
					events := []string{
						`{"type":"response.created","response":{"id":"resp_terra","model":"gpt-6.1-terra","output":[]}}`,
						`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":""}}`,
						`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"q\":\"ok\"}"}`,
						`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"ok\"}"}}`,
						`{"type":"response.completed","response":{"id":"resp_terra","model":"gpt-6.1-terra","status":"completed","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"ok\"}"}],"usage":{"input_tokens":100,"output_tokens":10}}}`,
					}
					var sse strings.Builder
					for _, event := range events {
						sse.WriteString("data: " + event + "\n\n")
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse.String()))}}
					svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
					account := &Account{ID: 1, Platform: PlatformOpenAI, Type: accountType, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture", "access_token": "fixture", "chatgpt_account_id": "fixture", "base_url": "https://api.openai.com", "model_mapping": map[string]any{"public": "gpt-6.1-terra"}}}
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
					result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
					require.NoError(t, err)
					require.NotNil(t, result)
					require.NotNil(t, upstream.lastReq)
					require.Contains(t, upstream.lastReq.URL.Path, "responses")
					require.Equal(t, "gpt-6.1-terra", gjson.GetBytes(upstream.lastBody, "model").String())
					require.Equal(t, effort, gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
					require.Equal(t, "lookup", gjson.GetBytes(upstream.lastBody, "tools.0.name").String())
					require.Contains(t, string(upstream.lastBody), "https://example.com/photo.png")
					require.Contains(t, string(upstream.lastBody), "input_image")
					require.Contains(t, recorder.Body.String(), "tool_calls")
					require.Contains(t, recorder.Body.String(), "lookup")
				})
			}
		}
	}
}
func TestGPT61TerraChatOnlyAccountRejectsToolsEvenWithNone(t *testing.T) {
	for _, effort := range []string{"none", "max"} {
		body := []byte(`{"model":"gpt-6.1-terra","reasoning_effort":"` + effort + `","tools":[{"type":"function","function":{"name":"lookup"}}],"messages":[{"role":"user","content":"hi"}]}`)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		svc := &OpenAIGatewayService{cfg: &config.Config{}}
		_, err := svc.forwardAsRawChatCompletions(context.Background(), c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, body, "")
		require.ErrorContains(t, err, "requires Responses")
		require.Equal(t, 400, recorder.Code)
	}
}
