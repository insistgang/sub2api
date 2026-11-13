//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIFailoverPlatformAccountFactories(t *testing.T) {
	svc := &OpenAIGatewayService{}
	for _, platform := range []string{PlatformMiniMax, PlatformDeepseek, PlatformOpenAI, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			account := &Account{ID: 41, Platform: platform, Type: AccountTypeAPIKey}
			body := []byte(`{"error":{"message":"fixture provider failure"}}`)
			for _, err := range []*UpstreamFailoverError{
				svc.newOpenAIAccountFailoverError(account, http.StatusPaymentRequired, nil, body, "fixture provider failure", false, false),
				svc.newOpenAIAccountFailoverErrorWithClassificationHeaders(account, http.StatusPaymentRequired, nil, nil, body, "fixture provider failure", false, false),
			} {
				require.Equal(t, platform, err.UpstreamPlatform)
				require.Equal(t, http.StatusPaymentRequired, err.StatusCode)
				require.Equal(t, body, err.ResponseBody)
			}
		})
	}
	legacy := svc.newOpenAIAccountFailoverError(nil, http.StatusBadGateway, nil, nil, "", false, false)
	require.Empty(t, legacy.UpstreamPlatform)
}

func TestOpenAIFailoverPlatformHTTPAndStreamPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 42, Platform: PlatformMiniMax, Type: AccountTypeAPIKey}
	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		return c
	}
	body := []byte(`{"error":{"message":"fixture provider failure"}}`)
	for attempt := 0; attempt < 2; attempt++ {
		err := svc.failoverOpenAIUpstreamHTTPError(context.Background(), newContext(), account,
			&http.Response{StatusCode: http.StatusPaymentRequired, Header: http.Header{}}, body, "fixture provider failure", "fixture-model")
		require.NotNil(t, err)
		require.Equal(t, PlatformMiniMax, err.UpstreamPlatform)
	}
	payload := []byte(`{"type":"response.failed","response":{"error":{"code":"server_error","message":"fixture stream failure"}}}`)
	streamErr := svc.newOpenAIStreamFailoverErrorWithModel(newContext(), account, false, "fixture-request", payload, "fixture stream failure", "fixture-model")
	require.Equal(t, PlatformMiniMax, streamErr.UpstreamPlatform)
	// Stream failures replace the provider payload with a compatibility envelope.
	// That rewrite must keep the failed account's platform.
	require.NotEqual(t, payload, streamErr.ResponseBody)
}

func TestOpenAIFailoverPlatformRetryWrapperKeepsOrigin(t *testing.T) {
	original := &UpstreamFailoverError{StatusCode: http.StatusBadGateway}
	original.SetUpstreamPlatformIfEmpty(PlatformMiniMax)
	wrapped := newOpenAIWSCurrentTurnFailoverError(original, []byte(`{"model":"fixture-model"}`))
	var retried *UpstreamFailoverError
	require.ErrorAs(t, wrapped, &retried)
	retried.SetUpstreamPlatformIfEmpty(PlatformGrok)
	require.Equal(t, PlatformMiniMax, retried.UpstreamPlatform, "a retry boundary must preserve the explicit origin")
	payload, ok := OpenAIWSCurrentTurnRetryPayload(wrapped)
	require.True(t, ok)
	payload[0] = '['
	again, ok := OpenAIWSCurrentTurnRetryPayload(wrapped)
	require.True(t, ok)
	require.Equal(t, byte('{'), again[0], "retry payload cloning must not alter the error metadata")
}
