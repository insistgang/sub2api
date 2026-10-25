package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (r *autoResetTestAccountRepo) ClearRateLimitIfObserved(_ context.Context, id int64, limitedAt, resetAt time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.account
	if a.ID != id || !a.IsOpenAIOAuth() || a.IsShadow() || a.RateLimitedAt == nil || a.RateLimitResetAt == nil ||
		!a.RateLimitedAt.Equal(limitedAt) || !a.RateLimitResetAt.Equal(resetAt) {
		return false, nil
	}
	a.RateLimitedAt, a.RateLimitResetAt = nil, nil
	return true, nil
}

func healthyOpenAIRecoveryUsage() *OpenAIQuotaUsage {
	usage := &OpenAIQuotaUsage{
		FetchedAt: time.Now().Unix(),
		RateLimit: &OpenAIRateLimit{
			Allowed: true,
			PrimaryWindow: &OpenAIRateLimitWindow{
				UsedPercent: 0, LimitWindowSeconds: 5 * 60 * 60, ResetAfterSeconds: 4 * 60 * 60,
			},
			SecondaryWindow: &OpenAIRateLimitWindow{
				UsedPercent: 0, LimitWindowSeconds: 7 * 24 * 60 * 60, ResetAfterSeconds: 6 * 24 * 60 * 60,
			},
		},
		RateLimitResetCredits: &OpenAIRateLimitResetCredits{AvailableCount: 0},
	}
	// Recovery requires evidence from a decoded response, not a zero-valued literal.
	data, err := json.Marshal(usage)
	if err != nil {
		panic(err)
	}
	var decoded OpenAIQuotaUsage
	if err := json.Unmarshal(data, &decoded); err != nil {
		panic(err)
	}
	return &decoded
}

func TestOpenAIQuotaAutoResetService_HealthyQueryClearsObservedRateLimit(t *testing.T) {
	svc, repo, quota := newResetThresholdTestFixture(t, nil, 0)
	limitedAt, resetAt := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	repo.account.RateLimitedAt, repo.account.RateLimitResetAt = &limitedAt, &resetAt
	repo.account.Priority = 10
	quota.usage = healthyOpenAIRecoveryUsage()
	require.False(t, repo.account.IsSchedulable())

	require.NoError(t, svc.evaluateAccount(context.Background(), repo.account.ID))

	got, err := repo.GetByID(context.Background(), repo.account.ID)
	require.NoError(t, err)
	require.Nil(t, got.RateLimitedAt)
	require.Nil(t, got.RateLimitResetAt)
	require.True(t, got.IsSchedulable())
	require.Equal(t, 10, got.Priority)
	require.Zero(t, quota.resetCalls.Load(), "healthy recovery must not consume a reset credit")
}

func healthyOpenAIRecoveryHeaders() http.Header {
	return http.Header{
		"X-Codex-Primary-Used-Percent":          {"0"},
		"X-Codex-Primary-Window-Minutes":        {"300"},
		"X-Codex-Primary-Reset-After-Seconds":   {"14400"},
		"X-Codex-Secondary-Used-Percent":        {"1"},
		"X-Codex-Secondary-Window-Minutes":      {"10080"},
		"X-Codex-Secondary-Reset-After-Seconds": {"518400"},
	}
}

func TestAccountUsageService_ProbeReconcilesOnlyHealthyFreshNormalQuota(t *testing.T) {
	cases := []struct {
		name   string
		status int
		edit   func(*Account, http.Header)
		clear  bool
	}{
		{name: "healthy", status: 200, clear: true},
		{name: "reversed primary secondary", status: 200, clear: true, edit: func(_ *Account, h http.Header) {
			h.Set("x-codex-primary-window-minutes", "10080")
			h.Set("x-codex-primary-reset-after-seconds", "518400")
			h.Set("x-codex-secondary-window-minutes", "300")
			h.Set("x-codex-secondary-reset-after-seconds", "14400")
		}},
		{name: "429 with zero headers", status: 429},
		{name: "401 with healthy headers", status: 401},
		{name: "partial", status: 200, edit: func(_ *Account, h http.Header) { h.Del("x-codex-primary-used-percent") }},
		{name: "malformed", status: 200, edit: func(_ *Account, h http.Header) { h.Set("x-codex-primary-used-percent", "invalid") }},
		{name: "NaN", status: 200, edit: func(_ *Account, h http.Header) { h.Set("x-codex-primary-used-percent", "NaN") }},
		{name: "infinite", status: 200, edit: func(_ *Account, h http.Header) { h.Set("x-codex-primary-used-percent", "+Inf") }},
		{name: "negative", status: 200, edit: func(_ *Account, h http.Header) { h.Set("x-codex-primary-used-percent", "-1") }},
		{name: "exhausted", status: 200, edit: func(_ *Account, h http.Header) { h.Set("x-codex-secondary-used-percent", "100") }},
		{name: "missing reset", status: 200, edit: func(_ *Account, h http.Header) { h.Del("x-codex-primary-reset-after-seconds") }},
		{name: "negative reset", status: 200, edit: func(_ *Account, h http.Header) { h.Set("x-codex-primary-reset-after-seconds", "-1") }},
		{name: "reset exceeds window", status: 200, edit: func(_ *Account, h http.Header) { h.Set("x-codex-primary-reset-after-seconds", "999999") }},
		{name: "duplicate durations", status: 200, edit: func(_ *Account, h http.Header) { h.Set("x-codex-primary-window-minutes", "10080") }},
		{name: "unknown duration", status: 200, edit: func(_ *Account, h http.Header) { h.Set("x-codex-primary-window-minutes", "301") }},
		{name: "shadow", status: 200, edit: func(a *Account, _ http.Header) {
			parent := int64(99)
			a.ParentAccountID = &parent
			a.QuotaDimension = QuotaDimensionSpark
		}},
		{name: "api key", status: 200, edit: func(a *Account, _ http.Header) { a.Type = AccountTypeAPIKey }},
		{name: "different platform", status: 200, edit: func(a *Account, _ http.Header) { a.Platform = PlatformGrok }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limitedAt, resetAt := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
			a := &Account{ID: 321, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, RateLimitedAt: &limitedAt, RateLimitResetAt: &resetAt}
			h := healthyOpenAIRecoveryHeaders()
			if tc.edit != nil {
				tc.edit(a, h)
			}
			repo := &autoResetTestAccountRepo{account: a}
			observed, err := repo.GetByID(context.Background(), a.ID)
			require.NoError(t, err)
			svc := &AccountUsageService{accountRepo: repo}
			_, err = svc.handleOpenAICodexProbeResponse(context.Background(), observed, &http.Response{StatusCode: tc.status, Header: h})
			require.NoError(t, err, "quota headers remain available for display even when unhealthy")
			got, err := repo.GetByID(context.Background(), a.ID)
			require.NoError(t, err)
			if tc.clear {
				require.Nil(t, got.RateLimitResetAt)
				require.Nil(t, got.RateLimitedAt)
				require.True(t, got.IsSchedulable())
			} else {
				require.Equal(t, resetAt, *got.RateLimitResetAt)
				require.Equal(t, limitedAt, *got.RateLimitedAt)
			}
		})
	}
}

func TestOpenAIRateLimitRecovery_PreservesIndependentState(t *testing.T) {
	limitedAt, resetAt := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	overload, temp := time.Now().Add(time.Minute), time.Now().Add(2*time.Minute)
	a := &Account{ID: 321, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Schedulable: false, Priority: 10, RateLimitedAt: &limitedAt, RateLimitResetAt: &resetAt,
		OverloadUntil: &overload, TempUnschedulableUntil: &temp,
		Extra: map[string]any{"model_rate_limits": map[string]any{"gpt-5": "retained"}, "unrelated": "retained"}}
	repo := &autoResetTestAccountRepo{account: a}
	observed, err := repo.GetByID(context.Background(), a.ID)
	require.NoError(t, err)
	svc := &AccountUsageService{accountRepo: repo}
	_, err = svc.handleOpenAICodexProbeResponse(context.Background(), observed, &http.Response{StatusCode: 200, Header: healthyOpenAIRecoveryHeaders()})
	require.NoError(t, err)
	got, err := repo.GetByID(context.Background(), a.ID)
	require.NoError(t, err)
	require.Nil(t, got.RateLimitResetAt)
	require.False(t, got.Schedulable)
	require.Equal(t, 10, got.Priority)
	require.Equal(t, overload, *got.OverloadUntil)
	require.Equal(t, temp, *got.TempUnschedulableUntil)
	require.Equal(t, observed.Extra["model_rate_limits"], got.Extra["model_rate_limits"])
	require.Equal(t, "retained", got.Extra["unrelated"])
}

func TestOpenAIQuotaAutoResetService_HealthyQueryPreservesRearmedGeneration(t *testing.T) {
	for _, shorter := range []bool{false, true} {
		t.Run(map[bool]string{false: "equal deadline", true: "shorter deadline"}[shorter], func(t *testing.T) {
			svc, repo, quota := newResetThresholdTestFixture(t, nil, 0)
			limitedAt, resetAt := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
			repo.account.RateLimitedAt, repo.account.RateLimitResetAt = &limitedAt, &resetAt
			quota.usage = healthyOpenAIRecoveryUsage()
			newLimitedAt, newResetAt := time.Now(), resetAt
			if shorter {
				newResetAt = resetAt.Add(-30 * time.Minute)
			}
			quota.queryHook = func() {
				repo.mu.Lock()
				defer repo.mu.Unlock()
				repo.account.RateLimitedAt, repo.account.RateLimitResetAt = &newLimitedAt, &newResetAt
			}
			require.NoError(t, svc.evaluateAccount(context.Background(), repo.account.ID))
			got, err := repo.GetByID(context.Background(), repo.account.ID)
			require.NoError(t, err)
			require.Equal(t, newLimitedAt, *got.RateLimitedAt)
			require.Equal(t, newResetAt, *got.RateLimitResetAt)
			require.Zero(t, quota.resetCalls.Load())
		})
	}
}

func TestOpenAIQuotaAutoResetService_CachedHealthyUsageDoesNotClearRateLimit(t *testing.T) {
	svc, repo, quota := newResetThresholdTestFixture(t, nil, 0)
	limitedAt, resetAt := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	repo.account.RateLimitedAt, repo.account.RateLimitResetAt = &limitedAt, &resetAt
	repo.account.Extra["codex_7d_used_percent"] = 0.0
	require.NoError(t, svc.evaluateAccount(context.Background(), repo.account.ID))
	require.Zero(t, quota.queryCalls.Load())
	require.Equal(t, resetAt, *repo.account.RateLimitResetAt)
}

func TestOpenAIQuotaAutoResetService_UnhealthyQueryKeepsRateLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*OpenAIQuotaUsage)
	}{
		{name: "not allowed", edit: func(u *OpenAIQuotaUsage) { u.RateLimit.Allowed = false }},
		{name: "limit reached", edit: func(u *OpenAIQuotaUsage) { u.RateLimit.LimitReached = true }},
		{name: "partial", edit: func(u *OpenAIQuotaUsage) { u.RateLimit.SecondaryWindow = nil }},
		{name: "missing decoded health flag", edit: func(u *OpenAIQuotaUsage) { u.RateLimit.recoveryFieldsPresent = false }},
		{name: "missing decoded window field", edit: func(u *OpenAIQuotaUsage) { u.RateLimit.PrimaryWindow.recoveryFieldsPresent = false }},
		{name: "exhausted", edit: func(u *OpenAIQuotaUsage) { u.RateLimit.SecondaryWindow.UsedPercent = 100 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, quota := newResetThresholdTestFixture(t, nil, 0)
			limitedAt, resetAt := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
			repo.account.RateLimitedAt, repo.account.RateLimitResetAt = &limitedAt, &resetAt
			quota.usage = healthyOpenAIRecoveryUsage()
			tc.edit(quota.usage)
			require.NoError(t, svc.evaluateAccount(context.Background(), repo.account.ID))
			got, err := repo.GetByID(context.Background(), repo.account.ID)
			require.NoError(t, err)
			require.Equal(t, limitedAt, *got.RateLimitedAt)
			require.Equal(t, resetAt, *got.RateLimitResetAt)
			require.Zero(t, quota.resetCalls.Load())
		})
	}
}

func TestOpenAIQuotaRecovery_JSONRequiresExplicitCompleteHealthEvidence(t *testing.T) {
	data, err := json.Marshal(healthyOpenAIRecoveryUsage())
	require.NoError(t, err)
	valid := string(data)
	for _, tc := range []struct {
		name, old, replacement string
		healthy                bool
	}{
		{name: "complete zero", healthy: true},
		{name: "missing used percent", old: `"used_percent":0,`, replacement: ""},
		{name: "null used percent", old: `"used_percent":0`, replacement: `"used_percent":null`},
		{name: "missing allowed", old: `"allowed":true,`, replacement: ""},
		{name: "missing limit reached", old: `"limit_reached":false,`, replacement: ""},
		{name: "null limit reached", old: `"limit_reached":false`, replacement: `"limit_reached":null`},
		{name: "not allowed", old: `"allowed":true`, replacement: `"allowed":false`},
		{name: "limit reached", old: `"limit_reached":false`, replacement: `"limit_reached":true`},
		{name: "exhausted", old: `"used_percent":0`, replacement: `"used_percent":100`},
		{name: "missing reset", old: `"reset_after_seconds":14400,`, replacement: ""},
		{name: "unknown duration", old: `"limit_window_seconds":18000`, replacement: `"limit_window_seconds":18001`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			if tc.old != "" {
				require.Contains(t, input, tc.old)
				input = strings.Replace(input, tc.old, tc.replacement, 1)
			}
			var usage OpenAIQuotaUsage
			require.NoError(t, json.Unmarshal([]byte(input), &usage))
			require.Equal(t, tc.healthy, healthyOpenAIQuotaUsage(&usage))
			if tc.healthy {
				encoded, err := json.Marshal(usage)
				require.NoError(t, err)
				require.JSONEq(t, valid, string(encoded), "private presence evidence must not change the public JSON shape")
			}
		})
	}
	literal := *healthyOpenAIRecoveryUsage().RateLimit.PrimaryWindow
	literal.recoveryFieldsPresent = false
	usage := healthyOpenAIRecoveryUsage()
	usage.RateLimit.PrimaryWindow = &literal
	require.False(t, healthyOpenAIQuotaUsage(usage), "literal windows need explicit decoded evidence too")
}
