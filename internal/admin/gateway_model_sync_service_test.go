package admin

import (
	"testing"

	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
)

func TestSupportedGatewayModelsUsesWildcardPricingForMappedModel(t *testing.T) {
	inputPrice := 0.000001
	channel := sub2api.AdminChannel{
		ModelMapping: map[string]map[string]string{
			"anthropic": {
				"claude-sonnet-4-6": "claude-sonnet-4-6",
			},
		},
		ModelPricing: []sub2api.ChannelModelPricing{
			{
				Platform:    "anthropic",
				Models:      []string{"claude-*"},
				BillingMode: "token",
				InputPrice:  &inputPrice,
			},
		},
	}

	models := supportedGatewayModels(channel)
	if len(models) != 1 {
		t.Fatalf("expected one model, got %d", len(models))
	}
	if models[0].ModelID != "claude-sonnet-4-6" {
		t.Fatalf("expected mapped model, got %q", models[0].ModelID)
	}
	if models[0].Pricing == nil || models[0].Pricing.InputPrice == nil || *models[0].Pricing.InputPrice != inputPrice {
		t.Fatalf("expected wildcard pricing to be attached, got %#v", models[0].Pricing)
	}
}

func TestLookupModelPricingPrefersExactBeforeWildcard(t *testing.T) {
	wildcardPrice := 0.000001
	exactPrice := 0.000003
	index := buildGatewayPricingIndex([]sub2api.ChannelModelPricing{
		{
			Platform:   "anthropic",
			Models:     []string{"claude-*"},
			InputPrice: &wildcardPrice,
		},
		{
			Platform:   "anthropic",
			Models:     []string{"claude-sonnet-4-6"},
			InputPrice: &exactPrice,
		},
	})

	pricing := lookupModelPricing(index, "anthropic", "claude-sonnet-4-6")
	if pricing == nil || pricing.InputPrice == nil || *pricing.InputPrice != exactPrice {
		t.Fatalf("expected exact pricing before wildcard, got %#v", pricing)
	}
}
