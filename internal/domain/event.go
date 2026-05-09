package domain

import (
	"time"

	"github.com/google/uuid"
)

type EventType string

const (
	EventSubscription EventType = "subscription"
	EventGift         EventType = "gift"
	EventTip          EventType = "tip"
)

const (
	Tier1Cents int64 = 499
	Tier2Cents int64 = 999
	Tier3Cents int64 = 2499
)

var TierCents = map[int]int64{
	1: Tier1Cents,
	2: Tier2Cents,
	3: Tier3Cents,
}

type Event struct {
	ID          uuid.UUID `json:"id"`
	CreatorID   uuid.UUID `json:"creator_id"`
	ViewerID    string    `json:"viewer_id"`
	EventType   EventType `json:"event_type"`
	Tier        *int      `json:"tier,omitempty"`
	Quantity    int       `json:"quantity"`
	AmountCents int64     `json:"amount_cents"`
	CreatedAt   time.Time `json:"created_at"`
}
