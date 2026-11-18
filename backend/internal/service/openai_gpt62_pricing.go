package service

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// Official Standard/30-minute cache rates. Keep these separate from older
// Astra/Sol prices; dynamic and operator price cards remain authoritative.
// Source: https://developers.openai.com/api/docs/pricing#text-tokens
var openAIGPT62FallbackPrices = map[string]*LiteLLMModelPricing{
	"gpt-6.2-astra": newGPT62FallbackPrice(8e-6),
	"gpt-6.2-sol":   newGPT62FallbackPrice(2e-6),
}

func newGPT62FallbackPrice(input float64) *LiteLLMModelPricing {
	return &LiteLLMModelPricing{
		InputCostPerToken: input, InputCostPerTokenPriority: input * 2,
		OutputCostPerToken: input * 5, OutputCostPerTokenPriority: input * 10,
		CacheReadInputTokenCost: input / 20, CacheReadInputTokenCostPriority: input / 10,
		CacheCreationInputTokenCost: input * 1.25, CacheCreationInputTokenCostPriority: input * 2.5,
		CacheCreationInputTokenCostExplicit: true,
		LongContextInputTokenThreshold:      272000, LongContextInputCostMultiplier: 2, LongContextOutputCostMultiplier: 1.5,
		SupportsServiceTier: true, LiteLLMProvider: "openai", Mode: "chat", SupportsPromptCaching: true,
	}
}

func gpt62UltrafastMultiplierAt(at time.Time) float64 {
	if at.IsZero() {
		at = timezone.Now()
	}
	// March 1, 2027 00:00 America/Los_Angeles (still Pacific Standard Time).
	if at.Before(time.Date(2027, time.March, 1, 8, 0, 0, 0, time.UTC)) {
		return 3
	}
	return 6
}
