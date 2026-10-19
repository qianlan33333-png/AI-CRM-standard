package port

import (
	"context"
	"time"
)

type OperatingNotes struct {
	ProductID ID        `json:"product_id"`
	Notes     string    `json:"notes"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}
type SaveOperatingNotes struct {
	ProductID       ID
	Notes           string
	ExpectedVersion int64
	Actor           int64
	IdempotencyKey  string
}
type OperatingNotesService interface {
	ReadOperatingNotes(context.Context, ID) (OperatingNotes, error)
	SaveOperatingNotes(context.Context, SaveOperatingNotes) (OperatingNotes, error)
}
