package redeem

import (
	"testing"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
)

func TestFindSubscriptionAppliedByRedemption(t *testing.T) {
	subscriptions := []sub2api.AdminSubscription{
		{ID: 10, Notes: "manual renewal"},
		{ID: 11, Notes: "Brevyn retry rr_existing"},
	}

	subscription, ok := findSubscriptionAppliedByRedemption(subscriptions, "rr_existing")
	if !ok {
		t.Fatalf("expected existing redemption subscription to be detected")
	}
	if subscription.ID != 11 {
		t.Fatalf("expected subscription 11, got %d", subscription.ID)
	}
}

func TestLatestRenewableSub2APISubscriptionFiltersInactiveAndExpired(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	subscriptions := []sub2api.AdminSubscription{
		{ID: 10, Status: "revoked", ExpiresAt: now.Add(72 * time.Hour)},
		{ID: 11, Status: "active", ExpiresAt: now.Add(-1 * time.Hour)},
		{ID: 12, Status: "active", ExpiresAt: now.Add(24 * time.Hour)},
		{ID: 13, Status: "active", ExpiresAt: now.Add(48 * time.Hour)},
	}

	subscription, ok := latestRenewableSub2APISubscription(subscriptions, now)
	if !ok {
		t.Fatalf("expected renewable subscription")
	}
	if subscription.ID != 13 {
		t.Fatalf("expected latest active future subscription 13, got %d", subscription.ID)
	}
}

func TestLatestRenewableSub2APISubscriptionReturnsFalseWhenNoneRenewable(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	subscriptions := []sub2api.AdminSubscription{
		{ID: 10, Status: "expired", ExpiresAt: now.Add(24 * time.Hour)},
		{ID: 11, Status: "active", ExpiresAt: now.Add(-1 * time.Hour)},
	}

	if _, ok := latestRenewableSub2APISubscription(subscriptions, now); ok {
		t.Fatalf("expected no renewable subscription")
	}
}
