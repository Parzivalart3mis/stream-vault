package domain

import "testing"

func TestTierCents(t *testing.T) {
	cases := []struct {
		tier  int
		cents int64
	}{
		{1, 499},
		{2, 999},
		{3, 2499},
	}
	for _, tc := range cases {
		got := TierCents[tc.tier]
		if got != tc.cents {
			t.Errorf("TierCents[%d] = %d, want %d", tc.tier, got, tc.cents)
		}
	}
}

func TestGiftAmount(t *testing.T) {
	cases := []struct {
		tier     int
		quantity int
		want     int64
	}{
		{1, 1, 499},
		{1, 5, 2495},
		{1, 10, 4990},
		{2, 3, 2997},
		{3, 2, 4998},
	}
	for _, tc := range cases {
		got := TierCents[tc.tier] * int64(tc.quantity)
		if got != tc.want {
			t.Errorf("gift tier=%d qty=%d: got %d, want %d", tc.tier, tc.quantity, got, tc.want)
		}
	}
}

func TestEventTypeValues(t *testing.T) {
	cases := []struct {
		et   EventType
		want string
	}{
		{EventSubscription, "subscription"},
		{EventGift, "gift"},
		{EventTip, "tip"},
	}
	for _, tc := range cases {
		if string(tc.et) != tc.want {
			t.Errorf("EventType %q != %q", tc.et, tc.want)
		}
	}
}
