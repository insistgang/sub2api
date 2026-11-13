package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthenticatedAPIKeyOwnerRole(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  *APIKey
		uid  int64
		want string
	}{
		{"admin", &APIKey{UserID: 2, User: &User{ID: 2, Role: RoleAdmin}}, 2, RoleAdmin},
		{"ordinary", &APIKey{UserID: 2, User: &User{ID: 2, Role: RoleUser}}, 2, RoleUser},
		{"nil", nil, 2, ""}, {"missing user", &APIKey{UserID: 2}, 2, ""},
		{"wrong owner", &APIKey{UserID: 3, User: &User{ID: 2, Role: RoleAdmin}}, 2, ""},
		{"wrong hydrated user", &APIKey{UserID: 2, User: &User{ID: 3, Role: RoleAdmin}}, 2, ""},
		{"missing identity", &APIKey{User: &User{Role: RoleAdmin}}, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, AuthenticatedAPIKeyOwnerRole(tc.key, tc.uid)) })
	}
}
func TestOwnerRolePreservedForEveryKeyThroughAuthCacheJSON(t *testing.T) {
	svc := &APIKeyService{}
	for _, role := range []string{RoleAdmin, RoleUser, ""} {
		for _, id := range []int64{11, 12} {
			key := &APIKey{ID: id, UserID: 2, User: &User{ID: 2, Role: role, Concurrency: 1}}
			raw, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)})
			require.NoError(t, err)
			var entry APIKeyAuthCacheEntry
			require.NoError(t, json.Unmarshal(raw, &entry))
			loaded, used, err := svc.applyAuthCacheEntry("test-key", &entry)
			require.NoError(t, err)
			require.True(t, used)
			require.Equal(t, role, AuthenticatedAPIKeyOwnerRole(loaded, 2))
			require.Equal(t, 1, loaded.User.Concurrency)
		}
	}
}
