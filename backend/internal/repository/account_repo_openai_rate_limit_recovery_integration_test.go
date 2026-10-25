//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountRepositoryClearRateLimitIfObservedOpenAI(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, platform, accountType string
		shadow, clear               bool
	}{
		{name: "normal OAuth", platform: service.PlatformOpenAI, accountType: service.AccountTypeOAuth, clear: true},
		{name: "API key", platform: service.PlatformOpenAI, accountType: service.AccountTypeAPIKey},
		{name: "shadow OAuth", platform: service.PlatformOpenAI, accountType: service.AccountTypeOAuth, shadow: true},
		{name: "other platform", platform: service.PlatformAnthropic, accountType: service.AccountTypeOAuth},
		{name: "Grok unchanged", platform: service.PlatformGrok, accountType: service.AccountTypeOAuth, clear: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := testEntTx(t)
			cache := &schedulerCacheRecorder{}
			repo := newAccountRepositoryWithSQL(tx.Client(), tx, cache)
			now := time.Now().UTC().Truncate(time.Microsecond)
			limitedAt, resetAt := now.Add(-time.Hour), now.Add(time.Hour)
			a := &service.Account{Name: "recovery-gate", Platform: tc.platform, Type: tc.accountType,
				RateLimitedAt: &limitedAt, RateLimitResetAt: &resetAt, Priority: 10}
			if tc.shadow {
				parent := mustCreateAccount(t, tx.Client(), &service.Account{Name: "recovery-parent", Platform: service.PlatformOpenAI})
				a.ParentAccountID, a.QuotaDimension = &parent.ID, service.QuotaDimensionSpark
			}
			a = mustCreateAccount(t, tx.Client(), a)
			cleared, err := repo.ClearRateLimitIfObserved(ctx, a.ID, *a.RateLimitedAt, *a.RateLimitResetAt)
			require.NoError(t, err)
			require.Equal(t, tc.clear, cleared)
			got, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, a.Schedulable, got.Schedulable)
			require.Equal(t, a.Priority, got.Priority)
			require.Len(t, cache.setAccounts, 1, "both applied and stale CAS must refresh the local snapshot")
			if tc.clear {
				require.Nil(t, got.RateLimitedAt)
				require.Nil(t, got.RateLimitResetAt)
				require.True(t, got.IsSchedulable())
				require.Nil(t, cache.setAccounts[0].RateLimitResetAt)
			} else {
				require.Equal(t, a.RateLimitedAt, got.RateLimitedAt)
				require.Equal(t, a.RateLimitResetAt, got.RateLimitResetAt)
			}
		})
	}
}

func TestAccountRepositoryOpenAIRecoveryPreservesRearmedAndIndependentState(t *testing.T) {
	ctx := context.Background()
	for _, shorter := range []bool{false, true} {
		t.Run(map[bool]string{false: "equal reset", true: "shorter reset"}[shorter], func(t *testing.T) {
			tx := testEntTx(t)
			repo := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
			now := time.Now().UTC().Truncate(time.Microsecond)
			limitedAt, resetAt := now.Add(-time.Hour), now.Add(time.Hour)
			overload, temp := time.Now().Add(2*time.Hour), time.Now().Add(3*time.Hour)
			a := mustCreateAccount(t, tx.Client(), &service.Account{Name: "recovery-rearm", Platform: service.PlatformOpenAI,
				Type: service.AccountTypeOAuth, RateLimitedAt: &limitedAt, RateLimitResetAt: &resetAt, OverloadUntil: &overload,
				Extra: map[string]any{"model_rate_limits": map[string]any{"test": "keep"}, "unrelated": "keep"}, Priority: 10})
			_, err := tx.Client().Account.UpdateOneID(a.ID).SetSchedulable(false).SetTempUnschedulableUntil(temp).Save(ctx)
			require.NoError(t, err)
			observed, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			cleared, err := repo.ClearRateLimitIfObserved(ctx, a.ID, *observed.RateLimitedAt, *observed.RateLimitResetAt)
			require.NoError(t, err)
			require.True(t, cleared)
			after, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, observed.OverloadUntil, after.OverloadUntil)
			require.Equal(t, observed.TempUnschedulableUntil, after.TempUnschedulableUntil)
			require.Equal(t, observed.Extra, after.Extra)
			require.False(t, after.Schedulable)
			require.Equal(t, 10, after.Priority)

			newLimitedAt, newResetAt := observed.RateLimitedAt.Add(time.Second), *observed.RateLimitResetAt
			if shorter {
				newResetAt = newResetAt.Add(-30 * time.Minute)
			}
			_, err = tx.Client().Account.UpdateOneID(a.ID).SetRateLimitedAt(newLimitedAt).SetRateLimitResetAt(newResetAt).Save(ctx)
			require.NoError(t, err)
			cleared, err = repo.ClearRateLimitIfObserved(ctx, a.ID, *observed.RateLimitedAt, *observed.RateLimitResetAt)
			require.NoError(t, err)
			require.False(t, cleared, "stale success must preserve a clear/re-arm generation")
			got, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, newLimitedAt, *got.RateLimitedAt)
			require.Equal(t, newResetAt, *got.RateLimitResetAt)
		})
	}
}
