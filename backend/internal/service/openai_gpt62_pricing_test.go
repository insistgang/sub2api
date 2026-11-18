//go:build unit

package service

import (
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGPT62PricingFallbackAndLongContext(t *testing.T) {
	for model, inputRate := range map[string]float64{"gpt-6.2-astra": 8e-6, "gpt-6.2-sol": 2e-6} {
		for source, svc := range map[string]*BillingService{
			"static":                newTestBillingService(),
			"missing dynamic entry": NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{"gpt-5.4": {InputCostPerToken: 99e-6, OutputCostPerToken: 99e-6}}}),
		} {
			for _, alias := range []string{model, "openai/" + model + "-max", model + "-ultra", model + "-openai-compact"} {
				t.Run(source+"/"+alias, func(t *testing.T) {
					p, err := svc.GetModelPricing(alias)
					require.NoError(t, err)
					require.InDelta(t, inputRate, p.InputPricePerToken, 1e-15)
					require.InDelta(t, inputRate*5, p.OutputPricePerToken, 1e-15)
					require.InDelta(t, inputRate/20, p.CacheReadPricePerToken, 1e-15)
					require.InDelta(t, inputRate*1.25, p.CacheCreationPricePerToken, 1e-15)
					for _, total := range []int{272000, 272001} {
						for tier, multiplier := range map[string]float64{"": 1, "fast": 2, "priority": 2} {
							tokens := UsageTokens{InputTokens: total - 3000, CacheReadTokens: 2000, CacheCreationTokens: 1000, OutputTokens: 500}
							cost, err := svc.CalculateCostWithServiceTier(alias, tokens, 1, tier)
							require.NoError(t, err)
							inputMultiplier, outputMultiplier := 1.0, 1.0
							if total > 272000 {
								inputMultiplier, outputMultiplier = 2, 1.5
							}
							require.InDelta(t, float64(tokens.InputTokens)*inputRate*inputMultiplier*multiplier, cost.InputCost, 1e-10)
							require.InDelta(t, 2000*inputRate/20*inputMultiplier*multiplier, cost.CacheReadCost, 1e-10)
							require.InDelta(t, 1000*inputRate*1.25*inputMultiplier*multiplier, cost.CacheCreationCost, 1e-10)
							require.InDelta(t, 500*inputRate*5*outputMultiplier*multiplier, cost.OutputCost, 1e-10)
							require.Equal(t, total > 272000, cost.LongContextBillingApplied)
						}
					}
				})
			}
		}
	}
}

func TestGPT62UltrafastPacificCutoverAndDynamicOverrides(t *testing.T) {
	ptr := func(value float64) *float64 { return &value }
	cutover := time.Date(2027, time.March, 1, 8, 0, 0, 0, time.UTC)
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		svc := newTestBillingService()
		for _, tc := range []struct {
			at         time.Time
			multiplier float64
		}{{cutover.Add(-time.Nanosecond), 3}, {cutover, 6}} {
			p, err := svc.getModelPricingAt(model, tc.at)
			require.NoError(t, err)
			cost := svc.computeTokenBreakdown(p, UsageTokens{InputTokens: 100, OutputTokens: 100}, 1, OpenAIFastTierUltrafast, true)
			require.InDelta(t, 100*(p.InputPricePerToken+p.OutputPricePerToken)*tc.multiplier, cost.TotalCost, 1e-12)
		}
		custom := &LiteLLMModelPricing{InputCostPerToken: 7e-6, OutputCostPerToken: 11e-6, InputCostPerTokenPriority: 9e-6, OutputCostPerTokenPriority: 13e-6, CacheReadInputTokenCost: 0.7e-6, CacheCreationInputTokenCost: 0, CacheCreationInputTokenCostExplicit: true}
		dynamic := NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{model: custom}})
		p, err := dynamic.getModelPricingAt(model, cutover.Add(-time.Nanosecond))
		require.NoError(t, err)
		require.Equal(t, custom.InputCostPerToken, p.InputPricePerToken)
		require.Equal(t, custom.InputCostPerTokenPriority, p.InputPricePerTokenPriority)
		require.Zero(t, p.CacheCreationPricePerToken)
		require.Zero(t, p.LongContextInputThreshold, "explicit dynamic policy is authoritative")
		channel := &ChannelModelPricing{InputPrice: ptr(3e-6), OutputPrice: ptr(4e-6), CacheWritePrice: ptr(0)}
		p, err = dynamic.GetModelPricingWithChannel(model, channel)
		require.NoError(t, err)
		require.Equal(t, 3e-6, p.InputPricePerToken)
		require.Zero(t, p.CacheCreationPricePerToken)
	}
}

func TestGPT62BundledPriceResource(t *testing.T) {
	body, err := os.ReadFile("../../resources/model-pricing/model_prices_and_context_window.json")
	require.NoError(t, err)
	prices, err := (&PricingService{}).parsePricingData(body)
	require.NoError(t, err)
	for model, inputRate := range map[string]float64{"gpt-6.2-astra": 8e-6, "gpt-6.2-sol": 2e-6} {
		price := prices[model]
		require.NotNil(t, price)
		require.InDelta(t, inputRate, price.InputCostPerToken, 1e-15)
		require.InDelta(t, inputRate*5, price.OutputCostPerToken, 1e-15)
		require.InDelta(t, inputRate/20, price.CacheReadInputTokenCost, 1e-15)
		require.InDelta(t, inputRate*1.25, price.CacheCreationInputTokenCost, 1e-15)
		require.Equal(t, 272000, price.LongContextInputTokenThreshold)
		// Query the exact key: dots in published IDs are not JSON path separators.
		entry := gjson.ParseBytes(body).Map()[model]
		require.EqualValues(t, 1050000, entry.Get("context_window").Int())
		require.EqualValues(t, 922000, entry.Get("max_input_tokens").Int())
		require.EqualValues(t, 128000, entry.Get("max_output_tokens").Int())
		require.False(t, entry.Get("supports_none_reasoning_effort").Bool())
		require.False(t, entry.Get("cache_creation_input_token_cost_above_1hr").Exists())
	}
}
