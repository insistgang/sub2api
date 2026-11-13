//go:build unit

package service

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGPT61TerraPricingNeverFallsBackToAstra(t *testing.T) {
	sources := map[string]*BillingService{"billing fallback": newTestBillingService(), "catalog missing Terra": NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{"gpt-6-astra": {InputCostPerToken: 10e-6, OutputCostPerToken: 50e-6}}})}
	for source, svc := range sources {
		for _, model := range []string{"gpt-6.1-terra", "openai/gpt-6.1-terra-max", "gpt-6.1-terra-none"} {
			t.Run(source+"/"+model, func(t *testing.T) {
				p, err := svc.GetModelPricing(model)
				require.NoError(t, err)
				require.InDelta(t, 1e-6, p.InputPricePerToken, 1e-15)
				require.InDelta(t, 5e-6, p.OutputPricePerToken, 1e-15)
				require.InDelta(t, 0.05e-6, p.CacheReadPricePerToken, 1e-15)
				require.InDelta(t, 1.25e-6, p.CacheCreationPricePerToken, 1e-15)
				for _, total := range []int{272000, 272001} {
					for tier, mult := range map[string]float64{"": 1, "fast": 2, "priority": 2, "flex": 0.5} {
						tokens := UsageTokens{InputTokens: total - 3000, CacheReadTokens: 2000, CacheCreationTokens: 1000, OutputTokens: 500}
						cost, err := svc.CalculateCostWithServiceTier(model, tokens, 1, tier)
						require.NoError(t, err)
						im, om := 1.0, 1.0
						if total > 272000 {
							im = 2
							om = 1.5
						}
						require.InDelta(t, float64(tokens.InputTokens)*1e-6*im*mult, cost.InputCost, 1e-10)
						require.InDelta(t, 2000*0.05e-6*im*mult, cost.CacheReadCost, 1e-10)
						require.InDelta(t, 1000*1.25e-6*im*mult, cost.CacheCreationCost, 1e-10)
						require.InDelta(t, 500*5e-6*om*mult, cost.OutputCost, 1e-10)
						require.Equal(t, total > 272000, cost.LongContextBillingApplied)
					}
				}
			})
		}
	}
}
