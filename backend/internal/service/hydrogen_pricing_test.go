package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPricingService_HydrogenTiersUseDeterministicBasePricing(t *testing.T) {
	base := &LiteLLMModelPricing{InputCostPerToken: 1.5e-6, OutputCostPerToken: 7.5e-6, CacheReadInputTokenCost: 0.075e-6}
	svc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"gemini-4-hydrogen": base}}
	for _, model := range []string{
		"gemini-4-hydrogen", "gemini-4-hydrogen-low", "gemini-4-hydrogen-medium",
		"gemini-4-hydrogen-high", "gemini-4-hydrogen-xhigh", "models/gemini-4-hydrogen-xhigh",
		"projects/p/locations/global/publishers/google/models/gemini-4-hydrogen-medium",
	} {
		t.Run(model, func(t *testing.T) { require.Same(t, base, svc.GetModelPricing(model)) })
	}
	for _, model := range []string{"gemini-4-hydrogen-other", "gemini-4-hydrogen-tiered", "gemini-4-hydrogen-high-other", "not-gemini-4-hydrogen"} {
		t.Run(model, func(t *testing.T) { require.Nil(t, svc.GetModelPricing(model)) })
	}
}

func TestBillingService_HydrogenFallbackPricesAllConfirmedTiers(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	for _, model := range []string{
		"gemini-4-hydrogen", "gemini-4-hydrogen-low", "gemini-4-hydrogen-medium",
		"gemini-4-hydrogen-high", "gemini-4-hydrogen-xhigh", "models/gemini-4-hydrogen-xhigh",
	} {
		t.Run(model, func(t *testing.T) {
			cost, err := svc.CalculateCost(model, UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000}, 1)
			require.NoError(t, err)
			require.InDelta(t, 1.5, cost.InputCost, 1e-12)
			require.InDelta(t, 7.5, cost.OutputCost, 1e-12)
			require.InDelta(t, 0.075, cost.CacheReadCost, 1e-12)
			require.InDelta(t, 9.075, cost.TotalCost, 1e-12)
			pricing, err := svc.GetModelPricing(model)
			require.NoError(t, err)
			require.Zero(t, pricing.CacheCreationPricePerToken)
		})
	}
	for _, model := range []string{"gemini-4-hydrogen-other", "gemini-4-hydrogen-tiered", "not-gemini-4-hydrogen"} {
		_, err := svc.GetModelPricing(model)
		require.ErrorIs(t, err, ErrModelPricingUnavailable, model)
	}
}

func TestDefaultPricingIncludesHydrogenRatesAndLimits(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)
	var entries map[string]struct {
		Input  int `json:"max_input_tokens"`
		Output int `json:"max_output_tokens"`
	}
	require.NoError(t, json.Unmarshal(data, &entries))
	require.Equal(t, 1048576, entries["gemini-4-hydrogen"].Input)
	require.Equal(t, 262144, entries["gemini-4-hydrogen"].Output)
	svc := &PricingService{}
	svc.pricingData, err = svc.parsePricingData(data)
	require.NoError(t, err)
	for _, model := range []string{"gemini-4-hydrogen", "gemini-4-hydrogen-low", "gemini-4-hydrogen-medium", "gemini-4-hydrogen-high", "gemini-4-hydrogen-xhigh"} {
		pricing := svc.GetModelPricing(model)
		require.NotNil(t, pricing, model)
		require.Equal(t, "gemini", pricing.LiteLLMProvider)
		require.InDelta(t, 1.5e-6, pricing.InputCostPerToken, 1e-15)
		require.InDelta(t, 7.5e-6, pricing.OutputCostPerToken, 1e-15)
		require.InDelta(t, 0.075e-6, pricing.CacheReadInputTokenCost, 1e-15)
		require.Zero(t, pricing.CacheCreationInputTokenCost)
	}
}
