package domain

import (
	"time"

	"github.com/google/uuid"
)

type Creator struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}
