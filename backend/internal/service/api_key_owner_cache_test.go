//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAuthenticatedOwnerRoleForAllKeysAcrossL1AndL2(t *testing.T) {
	for _, role := range []string{RoleAdmin, RoleUser} {
		for _, layer := range []string{"L1", "L2"} {
			t.Run(role+"/"+layer, func(t *testing.T) {
				ctx := context.Background()
				calls := 0
				keys := map[string]*APIKey{"owner-key-one": {ID: 11, UserID: 2, Status: StatusActive, User: &User{ID: 2, Role: role, Status: StatusActive, Concurrency: 1}}, "owner-key-two": {ID: 12, UserID: 2, Status: StatusActive, User: &User{ID: 2, Role: role, Status: StatusActive, Concurrency: 1}}}
				repo := &authRepoStub{getByKeyForAuth: func(_ context.Context, key string) (*APIKey, error) { calls++; return keys[key], nil }}
				cache := &authCacheStub{}
				cfg := &config.Config{}
				if layer == "L1" {
					cfg.APIKeyAuth.L1Size = 1000
					cfg.APIKeyAuth.L1TTLSeconds = 60
				} else {
					cfg.APIKeyAuth.L2TTLSeconds = 60
				}
				svc := NewAPIKeyService(repo, nil, nil, nil, nil, cache, cfg)
				if svc.authCacheL1 != nil {
					t.Cleanup(svc.authCacheL1.Close)
				}
				if layer == "L2" {
					entries := map[string][]byte{}
					for key, value := range keys {
						raw, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(ctx, value)})
						require.NoError(t, err)
						entries[svc.authCacheKey(key)] = raw
					}
					cache.getAuthCache = func(_ context.Context, key string) (*APIKeyAuthCacheEntry, error) {
						var entry APIKeyAuthCacheEntry
						err := json.Unmarshal(entries[key], &entry)
						return &entry, err
					}
				}
				for key := range keys {
					loaded, err := svc.GetByKey(ctx, key)
					require.NoError(t, err)
					require.Equal(t, role, AuthenticatedAPIKeyOwnerRole(loaded, 2))
					require.Equal(t, 1, loaded.User.Concurrency)
				}
				if layer == "L1" {
					svc.authCacheL1.Wait()
					require.Equal(t, 2, calls)
				} else {
					require.Zero(t, calls)
				}
				callsBefore := calls
				for key := range keys {
					loaded, err := svc.GetByKey(ctx, key)
					require.NoError(t, err)
					require.Equal(t, role, AuthenticatedAPIKeyOwnerRole(loaded, 2))
					require.Equal(t, 1, loaded.User.Concurrency)
				}
				require.Equal(t, callsBefore, calls, "both owner keys must retain role metadata on cache hits")
			})
		}
	}
}
