package service

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

type liveOwnerBudgetCache struct {
	liveTestConcurrencyCache
	userFull    bool
	accountFull bool
	userMax     int
	accountMax  int
	userID      int64
	apiKeyID    int64
	replacing   bool
}

func (c *liveOwnerBudgetCache) AcquireLiveLease(_ context.Context, _ int64, accountMax int, userID int64, userMax int, apiKeyID int64, _ string, replacing bool) (bool, error) {
	c.userMax = userMax
	c.accountMax = accountMax
	c.userID = userID
	c.apiKeyID = apiKeyID
	c.replacing = replacing
	return !c.accountFull && !(c.userFull && userMax > 0), nil
}
func TestLiveAdminOwnerOnlyBypassesPersistentUserBudget(t *testing.T) {
	for _, tc := range []struct {
		name, role  string
		accountFull bool
		want        bool
		userMax     int
	}{
		{"admin saturated user", RoleAdmin, false, true, 0},
		{"ordinary saturated user", RoleUser, false, false, 1},
		{"unknown owner", "", false, false, 1},
		{"unrecognized role", "superadmin", false, false, 1},
		{"admin saturated account", RoleAdmin, true, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &liveOwnerBudgetCache{userFull: true, accountFull: tc.accountFull}
			identity := LiveCallIdentity{APIKeyID: 77, UserID: 202, OwnerRole: tc.role}
			acquired, err := acquireLiveCallLease(context.Background(), cache, &Account{ID: 10, Concurrency: 3}, identity, 1, "lease")
			require.NoError(t, err)
			require.Equal(t, tc.want, acquired)
			require.Equal(t, tc.userMax, cache.userMax)
			require.Equal(t, 3, cache.accountMax)
			require.Equal(t, int64(202), cache.userID)
			require.Equal(t, int64(77), cache.apiKeyID)
			require.True(t, cache.replacing)
		})
	}
}

func TestLiveCreateAlwaysAppliesAuthenticatedOwnerBudget(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "openai_live.go", nil, 0)
	require.NoError(t, err)
	calls := 0
	for _, declaration := range source.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "CreateLiveCall" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "acquireLiveCallLease" {
				calls++
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				require.NotEqual(t, "AcquireLiveLease", sel.Sel.Name, "Live creation must use the owner-aware lease boundary")
			}
			return true
		})
	}
	require.Equal(t, 1, calls)
}
func TestLiveOwnerRoleIsInternalOnly(t *testing.T) {
	raw, err := json.Marshal(LiveCallIdentity{APIKeyID: 77, UserID: 202, OwnerRole: RoleAdmin})
	require.NoError(t, err)
	require.NotContains(t, string(raw), "admin")
	require.NotContains(t, string(raw), "OwnerRole")
	var identity LiveCallIdentity
	require.NoError(t, json.Unmarshal([]byte(`{"APIKeyID":77,"UserID":202,"OwnerRole":"admin","owner_role":"admin"}`), &identity))
	require.Empty(t, identity.OwnerRole)
}
