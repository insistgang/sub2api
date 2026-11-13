package handler

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func adminConcurrencyKey(id int64, role string) *service.APIKey {
	return &service.APIKey{ID: id, UserID: 202, User: &service.User{ID: 202, Role: role, Concurrency: 1}}
}

func TestAdminOwnerUserSlotsSkipFullQueueAndKeepAllKeyStats(t *testing.T) {
	cache := &helperConcurrencyCacheStub{waitAllowed: false}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Millisecond)
	var releases []func()
	for _, id := range []int64{77, 78} {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
		c.Set(string(middleware2.ContextKeyAPIKey), adminConcurrencyKey(id, service.RoleAdmin))
		// Async image submission copies the authenticated Gin context.
		c = c.Copy()
		for range 5 {
			started := false
			release, err := helper.acquireUserSlotWithWaitTimeout(c, 202, 1, time.Millisecond, false, &started)
			require.NoError(t, err)
			require.NotNil(t, release)
			require.False(t, started)
			releases = append(releases, release)
		}
	}
	require.Zero(t, cache.userAcquireCalls)
	require.Zero(t, cache.waitIncrementCalls)
	require.Zero(t, cache.waitDecrementCalls)
	require.Equal(t, 10, cache.apiKeyTrackCalls)
	for _, release := range releases {
		release()
	}
	require.Zero(t, cache.userReleaseCalls)
	require.Equal(t, 10, cache.apiKeyReleaseCalls)
	require.Equal(t, []int64{77, 77, 77, 77, 77, 78, 78, 78, 78, 78}, cache.apiKeyTrackIDs)
	_, acquired, err := helper.TryAcquireAccountSlot(context.Background(), 10, 1)
	require.NoError(t, err)
	require.False(t, acquired)
	require.Equal(t, 1, cache.accountAcquireCalls)
}

func TestNonAdminOwnerStillUsesUserSlotAndFullQueue(t *testing.T) {
	cases := map[string]*service.APIKey{"missing key": nil, "missing user": {ID: 77, UserID: 202}, "ordinary": adminConcurrencyKey(77, service.RoleUser), "unknown role": adminConcurrencyKey(77, "superadmin"), "key owner mismatch": {ID: 77, UserID: 303, User: &service.User{ID: 202, Role: service.RoleAdmin}}, "user identity mismatch": {ID: 77, UserID: 202, User: &service.User{ID: 303, Role: service.RoleAdmin}}}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			cache := &helperConcurrencyCacheStub{waitAllowed: false}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Millisecond)
			c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
			if key != nil {
				c.Set(string(middleware2.ContextKeyAPIKey), key)
			}
			c.Set(string(middleware2.ContextKeyUserRole), service.RoleAdmin) // An unrelated role marker cannot authorize bypass.
			started := false
			release, err := helper.acquireUserSlotWithWaitTimeout(c, 202, 1, time.Millisecond, false, &started)
			var queueErr *WaitQueueFullError
			require.ErrorAs(t, err, &queueErr)
			require.Nil(t, release)
			require.Equal(t, 1, cache.userAcquireCalls)
			require.Equal(t, 1, cache.waitIncrementCalls)
			require.Zero(t, cache.apiKeyTrackCalls)
		})
	}
}

type ownerErrorConcurrencyCache struct{ helperConcurrencyCacheStub }

func (c *ownerErrorConcurrencyCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	c.userAcquireCalls++
	return false, errors.New("user cache unavailable")
}
func TestAdminOwnerBypassesUserCacheErrorOnly(t *testing.T) {
	for _, role := range []string{service.RoleAdmin, service.RoleUser} {
		t.Run(role, func(t *testing.T) {
			cache := &ownerErrorConcurrencyCache{}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Millisecond)
			c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
			c.Set(string(middleware2.ContextKeyAPIKey), adminConcurrencyKey(77, role))
			started := false
			release, err := helper.acquireUserSlotWithWaitTimeout(c, 202, 1, time.Millisecond, false, &started)
			if role == service.RoleAdmin {
				require.NoError(t, err)
				release()
				require.Zero(t, cache.userAcquireCalls)
				require.Equal(t, 1, cache.apiKeyReleaseCalls)
			} else {
				require.ErrorContains(t, err, "user cache unavailable")
				require.Nil(t, release)
				require.Equal(t, 1, cache.userAcquireCalls)
			}
		})
	}
}

func TestWebSocketOwnerReacquisitionKeepsAPIKeyStats(t *testing.T) {
	cache := &helperConcurrencyCacheStub{}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Millisecond)
	owner := adminConcurrencyKey(77, service.RoleAdmin)
	for _, stage := range []string{"first", "reacquire", "later turn"} {
		t.Run(stage, func(t *testing.T) {
			release, acquired, err := helper.TryAcquireUserSlotForAPIKey(context.Background(), 202, 1, owner)
			require.NoError(t, err)
			require.True(t, acquired)
			release()
		})
	}
	require.Zero(t, cache.userAcquireCalls)
	require.Equal(t, 3, cache.apiKeyTrackCalls)
	require.Equal(t, 3, cache.apiKeyReleaseCalls)
}
func TestWebSocketAllUserSlotCallsCarryAuthenticatedOwner(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "openai_gateway_handler.go", nil, 0)
	require.NoError(t, err)
	calls := 0
	ast.Inspect(source, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "TryAcquireUserSlotForAPIKey" {
			return true
		}
		calls++
		require.Len(t, call.Args, 4)
		owner, ok := call.Args[3].(*ast.Ident)
		require.True(t, ok, "every raw-context WS call must carry the API key, not only its ID")
		if ok {
			require.Equal(t, "apiKey", owner.Name)
		}
		return true
	})
	require.Equal(t, 3, calls, "cover first acquisition, failover reacquisition, and later turns")
}
