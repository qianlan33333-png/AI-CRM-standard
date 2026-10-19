package port

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrConversationSequenceConflict = errors.New("local RPA send conflict")

type RPAIntentCommand struct {
	RunID                    int64
	GroupIndex, MessageIndex int
	DeviceID, StaffID        int64
	SenderUserID, ChatID     string
	Payload                  json.RawMessage
}
type RPAIntent struct {
	ID        int64
	State     string
	Grant     string
	ExpiresAt time.Time
}
type RPAOutcomeCommand struct {
	IntentID, DeviceID, StaffID int64
	Grant, Outcome              string
	EvidenceDigest              [32]byte
}

// Every method participates in the caller's UoW. No scheduler/lease is owned
// by this port; the existing effects queue expires a single-use UI grant.
type ConversationSequenceSender interface {
	AcceptRPAWithin(context.Context, RPAIntentCommand) (RPAIntent, error)
	CompleteRPAWithin(context.Context, RPAOutcomeCommand) (RPAIntent, error)
	// ReconcileRPAWithin records an explicitly observed prior submission. It
	// never authorizes another UI send and retains the original effect/intent.
	ReconcileRPAWithin(context.Context, RPAOutcomeCommand) (RPAIntent, error)
	ReadRPAWithin(context.Context, int64, int64, int64) (RPAIntent, error)
}
