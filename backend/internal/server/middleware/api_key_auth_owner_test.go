//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthPreservesEveryOwnerKeyRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{config.RunModeSimple, config.RunModeStandard} {
		for _, google := range []bool{false, true} {
			for _, role := range []string{service.RoleAdmin, service.RoleUser} {
				name := mode + "/" + role
				if google {
					name += "/google"
				}
				t.Run(name, func(t *testing.T) {
					group := &service.Group{ID: 42, Status: service.StatusActive, Platform: service.PlatformOpenAI, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1}
					keys := map[string]*service.APIKey{"owner-key-one": {ID: 77, UserID: 202, Status: service.StatusActive, User: &service.User{ID: 202, Role: role, Status: service.StatusActive, Balance: 10, Concurrency: 1}, GroupID: &group.ID, Group: group}, "owner-key-two": {ID: 78, UserID: 202, Status: service.StatusActive, User: &service.User{ID: 202, Role: role, Status: service.StatusActive, Balance: 10, Concurrency: 1}, GroupID: &group.ID, Group: group}}
					repo := &stubApiKeyRepo{getByKey: func(_ context.Context, key string) (*service.APIKey, error) {
						if value := keys[key]; value != nil {
							return value, nil
						}
						return nil, service.ErrAPIKeyNotFound
					}}
					cfg := &config.Config{RunMode: mode}
					svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
					router := gin.New()
					if google {
						router.Use(APIKeyAuthGoogle(svc, cfg))
					} else {
						router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
					}
					router.GET("/t", func(c *gin.Context) {
						key, ok := GetAPIKeyFromContext(c)
						require.True(t, ok)
						subject, ok := GetAuthSubjectFromContext(c)
						require.True(t, ok)
						require.Equal(t, role, service.AuthenticatedAPIKeyOwnerRole(key, subject.UserID))
						require.Equal(t, 1, subject.Concurrency)
						c.Status(http.StatusOK)
					})
					for key := range keys {
						request := httptest.NewRequest(http.MethodGet, "/t", nil)
						request.Header.Set("Authorization", "Bearer "+key)
						recorder := httptest.NewRecorder()
						router.ServeHTTP(recorder, request)
						require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					}
				})
			}
		}
	}
}
