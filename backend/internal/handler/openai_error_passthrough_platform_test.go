//go:build unit

package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type failoverPlatformRulesRepo struct {
	service.ErrorPassthroughRepository
	rules []*model.ErrorPassthroughRule
}

func (r *failoverPlatformRulesRepo) List(context.Context) ([]*model.ErrorPassthroughRule, error) {
	return r.rules, nil
}

func TestOpenAIFailoverRulePlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openAIStatus, miniMaxStatus := http.StatusTeapot, http.StatusPaymentRequired
	openAIMessage, miniMaxMessage := "safe OpenAI rule", "safe MiniMax rule"
	openAIRule := &model.ErrorPassthroughRule{
		Name: "OpenAI fixture", Enabled: true, Priority: 0,
		Platforms: []string{service.PlatformOpenAI}, ErrorCodes: []int{http.StatusPaymentRequired},
		Keywords: []string{"fixture provider failure"}, MatchMode: model.MatchModeAll,
		ResponseCode: &openAIStatus, CustomMessage: &openAIMessage,
	}
	miniMaxRule := &model.ErrorPassthroughRule{
		Name: "MiniMax fixture", Enabled: true, Priority: 1,
		Platforms: []string{service.PlatformMiniMax}, ErrorCodes: []int{http.StatusPaymentRequired},
		Keywords: []string{"fixture provider failure"}, MatchMode: model.MatchModeAll,
		ResponseCode: &miniMaxStatus, CustomMessage: &miniMaxMessage,
	}

	for _, test := range []struct {
		name, platform, message string
		rules                   []*model.ErrorPassthroughRule
		status                  int
	}{
		{"minimax uses its provider rule", service.PlatformMiniMax, miniMaxMessage, []*model.ErrorPassthroughRule{openAIRule, miniMaxRule}, miniMaxStatus},
		{"minimax ignores OpenAI-only rule", service.PlatformMiniMax, "Upstream request failed", []*model.ErrorPassthroughRule{openAIRule}, http.StatusBadGateway},
		{"genuine OpenAI keeps its rule", service.PlatformOpenAI, openAIMessage, []*model.ErrorPassthroughRule{openAIRule, miniMaxRule}, openAIStatus},
		{"blank legacy keeps OpenAI rule", "", openAIMessage, []*model.ErrorPassthroughRule{openAIRule, miniMaxRule}, openAIStatus},
		{"OpenAI ignores MiniMax-only rule", service.PlatformOpenAI, "Upstream request failed", []*model.ErrorPassthroughRule{miniMaxRule}, http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			h := &OpenAIGatewayHandler{errorPassthroughService: service.NewErrorPassthroughService(
				&failoverPlatformRulesRepo{rules: test.rules}, nil,
			)}
			h.handleFailoverExhausted(c, &service.UpstreamFailoverError{
				UpstreamPlatform: test.platform, StatusCode: http.StatusPaymentRequired,
				ResponseBody: []byte(`{"error":{"message":"fixture provider failure","access_token":"must-not-leak"}}`),
			}, false)

			require.Equal(t, test.status, recorder.Code)
			require.Equal(t, test.message, gjson.Get(recorder.Body.String(), "error.message").String())
			require.Equal(t, "upstream_error", gjson.Get(recorder.Body.String(), "error.type").String())
			require.NotContains(t, recorder.Body.String(), "fixture provider failure")
			require.NotContains(t, recorder.Body.String(), "access_token")
			require.NotContains(t, recorder.Body.String(), "must-not-leak")
		})
	}
}

func TestOpenAIFailoverRulePlatformGrokBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct {
		path, body string
	}{
		{"responses", `{"model":"grok","input":"fixture","stream":false}`},
		{"chat/completions", `{"model":"grok","messages":[{"role":"user","content":"fixture"}],"stream":false}`},
		{"videos/generations", `{"model":"grok-imagine-video","prompt":"fixture"}`},
		{"audio/speech", `{"input":"fixture"}`},
	} {
		t.Run(endpoint.path, func(t *testing.T) {
			h, _, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "all_429")
			defer cleanup()
			expectedHits := []int64{801, 802}
			if endpoint.path == "videos/generations" {
				// Media already applies provider rules before entering failover.
				expectedHits = []int64{801}
			}
			if endpoint.path == "audio/speech" {
				upstream.rateLimitIDs[803] = true
				expectedHits = append(expectedHits, 803)
				router.POST("/openai/v1/audio/speech", func(c *gin.Context) { h.GrokVoice(c, "tts") })
			}
			status, message := http.StatusTeapot, "safe Grok rule"
			h.errorPassthroughService = service.NewErrorPassthroughService(&failoverPlatformRulesRepo{
				rules: []*model.ErrorPassthroughRule{{
					Name: "Grok fixture", Enabled: true, Platforms: []string{service.PlatformGrok},
					ErrorCodes: []int{http.StatusTooManyRequests}, MatchMode: model.MatchModeAll,
					ResponseCode: &status, CustomMessage: &message,
				}},
			}, nil)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/openai/v1/"+endpoint.path, bytes.NewBufferString(endpoint.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, status, recorder.Code, recorder.Body.String())
			require.Equal(t, message, gjson.Get(recorder.Body.String(), "error.message").String())
			require.Equal(t, expectedHits, upstream.accountHits(), "provider rules must preserve the endpoint's failover policy")
			require.NotContains(t, recorder.Body.String(), "healthy-access")
		})
	}
}
