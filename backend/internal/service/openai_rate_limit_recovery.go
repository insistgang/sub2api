package service

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"
)

// Presence is private recovery evidence: ordinary quota reads and JSON output
// retain their scalar fields, while omitted/null fields cannot masquerade as 0%.
func (w *OpenAIRateLimitWindow) UnmarshalJSON(data []byte) error {
	type plain OpenAIRateLimitWindow
	decoded := plain(*w)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*w = OpenAIRateLimitWindow(decoded)
	w.recoveryFieldsPresent = openAIQuotaJSONFieldsPresent(data, "used_percent", "limit_window_seconds", "reset_after_seconds")
	return nil
}

func (r *OpenAIRateLimit) UnmarshalJSON(data []byte) error {
	type plain OpenAIRateLimit
	decoded := plain(*r)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = OpenAIRateLimit(decoded)
	r.recoveryFieldsPresent = openAIQuotaJSONFieldsPresent(data, "allowed", "limit_reached", "primary_window", "secondary_window")
	return nil
}

func openAIQuotaJSONFieldsPresent(data []byte, keys ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return false
	}
	for _, key := range keys {
		value, ok := fields[key]
		if !ok || strings.TrimSpace(string(value)) == "null" {
			return false
		}
	}
	return true
}

func healthyOpenAIQuotaWindow(used float64, windowSeconds, resetSeconds int64) bool {
	return !math.IsNaN(used) && !math.IsInf(used, 0) && used >= 0 && used < 100 &&
		resetSeconds > 0 && resetSeconds <= windowSeconds
}

func normalOpenAIQuotaWindowPair(first, second int64) bool {
	const fiveHours, sevenDays = 5 * 60 * 60, 7 * 24 * 60 * 60
	return (first == fiveHours && second == sevenDays) || (first == sevenDays && second == fiveHours)
}

func healthyOpenAIQuotaUsage(usage *OpenAIQuotaUsage) bool {
	if usage == nil || usage.RateLimit == nil {
		return false
	}
	r := usage.RateLimit
	first, second := r.PrimaryWindow, r.SecondaryWindow
	return r.recoveryFieldsPresent && r.Allowed && !r.LimitReached &&
		first != nil && second != nil && first.recoveryFieldsPresent && second.recoveryFieldsPresent &&
		normalOpenAIQuotaWindowPair(first.LimitWindowSeconds, second.LimitWindowSeconds) &&
		healthyOpenAIQuotaWindow(first.UsedPercent, first.LimitWindowSeconds, first.ResetAfterSeconds) &&
		healthyOpenAIQuotaWindow(second.UsedPercent, second.LimitWindowSeconds, second.ResetAfterSeconds)
}

func healthyOpenAICodexProbeResponse(resp *http.Response) bool {
	if resp == nil || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return false
	}
	s := ParseCodexRateLimitHeaders(resp.Header)
	if s == nil || s.PrimaryUsedPercent == nil || s.PrimaryWindowMinutes == nil || s.PrimaryResetAfterSeconds == nil ||
		s.SecondaryUsedPercent == nil || s.SecondaryWindowMinutes == nil || s.SecondaryResetAfterSeconds == nil {
		return false
	}
	// Match known durations before multiplying untrusted header integers.
	if !((*s.PrimaryWindowMinutes == 300 && *s.SecondaryWindowMinutes == 10080) ||
		(*s.PrimaryWindowMinutes == 10080 && *s.SecondaryWindowMinutes == 300)) {
		return false
	}
	return healthyOpenAIQuotaWindow(*s.PrimaryUsedPercent, int64(*s.PrimaryWindowMinutes)*60, int64(*s.PrimaryResetAfterSeconds)) &&
		healthyOpenAIQuotaWindow(*s.SecondaryUsedPercent, int64(*s.SecondaryWindowMinutes)*60, int64(*s.SecondaryResetAfterSeconds))
}

type openAIRateLimitRecoveryRepository interface {
	ClearRateLimitIfObserved(context.Context, int64, time.Time, time.Time) (bool, error)
}

// Call only with fresh healthy upstream evidence and the account observed BEFORE
// the request. Matching both timestamps preserves a concurrently re-armed 429,
// even when its new reset deadline is equal or shorter. Other blocks stay intact.
func clearOpenAIRateLimitAfterRecovery(ctx context.Context, repo AccountRepository, observed *Account) error {
	if observed == nil || !observed.IsOpenAIOAuth() || observed.IsShadow() ||
		observed.RateLimitedAt == nil || observed.RateLimitResetAt == nil || ctx.Err() != nil {
		return nil
	}
	recovery, ok := repo.(openAIRateLimitRecoveryRepository)
	if !ok {
		return nil
	}
	_, err := recovery.ClearRateLimitIfObserved(ctx, observed.ID, *observed.RateLimitedAt, *observed.RateLimitResetAt)
	return err
}
