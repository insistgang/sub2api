package handler

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestLiveCallIdentityOnlyCarriesVerifiedAPIKeyOwnerRole(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  *service.APIKey
		want string
	}{
		{"admin", adminConcurrencyKey(77, service.RoleAdmin), service.RoleAdmin},
		{"ordinary", adminConcurrencyKey(77, service.RoleUser), service.RoleUser},
		{"unknown", &service.APIKey{ID: 77, UserID: 202}, ""},
		{"wrong owner", &service.APIKey{ID: 77, UserID: 303, User: &service.User{ID: 202, Role: service.RoleAdmin}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newHelperTestContext(http.MethodPost, "/v1/live")
			identity := liveCallIdentity(c, tc.key, 202, nil)
			require.Equal(t, tc.want, identity.OwnerRole)
			require.Equal(t, int64(202), identity.UserID)
			require.Equal(t, int64(77), identity.APIKeyID)
		})
	}
}

func TestLiveTemporaryUserSlotUsesVerifiedOwner(t *testing.T) {
	for _, role := range []string{service.RoleAdmin, service.RoleUser} {
		t.Run(role, func(t *testing.T) {
			cache := &helperConcurrencyCacheStub{}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Millisecond)
			release, acquired, err := helper.TryAcquireUserSlotForOwner(context.Background(), 202, 1, adminConcurrencyKey(77, role))
			require.NoError(t, err)
			require.Equal(t, role == service.RoleAdmin, acquired)
			if acquired {
				require.NotNil(t, release)
				release()
				require.Zero(t, cache.userAcquireCalls)
				require.Zero(t, cache.userReleaseCalls)
			} else {
				require.Nil(t, release)
				require.Equal(t, 1, cache.userAcquireCalls)
			}
		})
	}
}
func TestLiveCreateCarriesOwnerToTemporarySlot(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "openai_live.go", nil, 0)
	require.NoError(t, err)
	calls := 0
	ast.Inspect(source, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "TryAcquireUserSlotForOwner" {
			return true
		}
		calls++
		require.Len(t, call.Args, 4)
		owner, ok := call.Args[3].(*ast.Ident)
		require.True(t, ok)
		if ok {
			require.Equal(t, "apiKey", owner.Name)
		}
		return true
	})
	require.Equal(t, 1, calls)
}
