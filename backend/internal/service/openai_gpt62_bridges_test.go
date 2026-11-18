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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGPT62ResponsesBridgesKeepToolsImagesAndStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
			for _, effort := range []string{"max", "ultra"} {
				for _, stream := range []bool{false, true} {
					for _, messages := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/%s/stream=%t/messages=%t", model, accountType, effort, stream, messages), func(t *testing.T) {
							body := []byte(fmt.Sprintf(`{"model":"public","stream":%t,"reasoning_effort":"%s","messages":[{"role":"user","content":[{"type":"text","text":"inspect"},{"type":"image_url","image_url":{"url":"https://example.com/photo.png"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, stream, effort))
							if messages {
								body = []byte(fmt.Sprintf(`{"model":"public","stream":%t,"max_tokens":100,"output_config":{"effort":"%s"},"messages":[{"role":"user","content":[{"type":"text","text":"inspect"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aW1hZ2U="}}]}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`, stream, effort))
							}
							events := []string{
								`{"type":"response.created","response":{"id":"resp_gpt62","model":"` + model + `","output":[]}}`,
								`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":""}}`,
								`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{}"}`,
								`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}}`,
								`{"type":"response.completed","response":{"id":"resp_gpt62","model":"` + model + `","status":"completed","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}],"usage":{"input_tokens":100,"output_tokens":10}}}`,
							}
							var sse strings.Builder
							for _, event := range events {
								sse.WriteString("data: " + event + "\n\n")
							}
							upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse.String()))}}
							svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
							account := &Account{ID: 1, Platform: PlatformOpenAI, Type: accountType, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture", "access_token": "fixture", "chatgpt_account_id": "fixture", "base_url": "https://api.openai.com", "model_mapping": map[string]any{"public": model}}}
							recorder := httptest.NewRecorder()
							c, _ := gin.CreateTestContext(recorder)
							c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
							var result *OpenAIForwardResult
							var err error
							if messages {
								result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
							} else {
								result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
							}
							require.NoError(t, err)
							require.NotNil(t, result)
							require.NotNil(t, upstream.lastReq)
							require.Contains(t, upstream.lastReq.URL.Path, "responses")
							require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
							require.Equal(t, effort, gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
							require.Equal(t, "lookup", gjson.GetBytes(upstream.lastBody, "tools.0.name").String())
							require.Contains(t, string(upstream.lastBody), "input_image")
							require.Contains(t, recorder.Body.String(), "lookup")
						})
					}
				}
			}
		}
	}
}

func TestGPT62ChatOnlyFallbacksRejectTools(t *testing.T) {
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		for _, responses := range []bool{false, true} {
			body := []byte(`{"model":"public","max_tokens":100,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`)
			if responses {
				body = []byte(`{"model":"public","input":"hi","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"public": model}}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			var err error
			if responses {
				_, err = svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, body, time.Now())
			} else {
				_, err = svc.forwardAnthropicViaRawChatCompletions(context.Background(), c, account, body, "")
			}
			require.ErrorContains(t, err, "requires Responses")
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		}
	}
}

func TestGPT62WebSocketMappedTurnsRejectNoneAnd12h(t *testing.T) {
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		for _, field := range []string{`"reasoning":{"effort":"none"}`, `"prompt_cache_options":{"ttl":"12h"}`} {
			t.Run(model+"/"+field, func(t *testing.T) {
				controlCtx, cancel := context.WithCancelCause(context.Background())
				defer cancel(context.Canceled)
				upstream := newStagedPassthroughConn()
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_first","model":"` + model + `","usage":{"input_tokens":1,"output_tokens":1}}}`)
				account := passthroughLifecycleAccount()
				svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
				hooks := &OpenAIWSIngressHooks{MapRequestModel: func(int, string) (string, error) { return model, nil }}
				server, serverErr := startPassthroughHookRecordingServer(t, controlCtx, svc, account, hooks)
				defer server.Close()
				client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"public"}`)
				defer func() { _ = client.CloseNow() }()
				require.Equal(t, model, gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, time.Second), "model").String())
				_, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
				require.NoError(t, err)
				writeCtx, cancelWrite := context.WithTimeout(context.Background(), time.Second)
				err = client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"public",`+field+`}`))
				cancelWrite()
				require.NoError(t, err)
				// Read the close frame so the peer can complete its close handshake.
				_, err = readPassthroughLifecycleFrame(t, client, 3*time.Second)
				require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err))
				if strings.Contains(field, "12h") {
					require.ErrorContains(t, err, "30m")
				}
				select {
				case err := <-serverErr:
					require.Error(t, err)
					if strings.Contains(field, "12h") {
						require.ErrorContains(t, err, "30m")
					} else {
						require.ErrorContains(t, err, "none")
					}
				case <-time.After(3 * time.Second):
					t.Fatal("mapped WebSocket turn was not rejected")
				}
			})
		}
	}
}
