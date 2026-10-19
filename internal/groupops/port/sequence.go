package port

import (
	"context"
	"time"
)

type SequenceMessage struct {
	ID           string `json:"id" yaml:"id"`
	Kind         string `json:"kind" yaml:"kind"`
	Text         string `json:"text,omitempty" yaml:"text,omitempty"`
	RelativePath string `json:"relative_path,omitempty" yaml:"relative_path,omitempty"`
	SHA256       string `json:"sha256,omitempty" yaml:"sha256,omitempty"`
	ByteCount    int64  `json:"byte_count,omitempty" yaml:"byte_count,omitempty"`
}
type SequenceConfig struct {
	PlanID   int64             `json:"plan_id,string"`
	Revision int64             `json:"revision"`
	Messages []SequenceMessage `json:"messages"`
	Key      string            `json:"-"`
}
type SequenceExecution struct {
	Messages          []SequenceMessage `json:"messages"`
	MessageIndex      int               `json:"message_index"`
	DeviceID          int64             `json:"device_id,string"`
	IntentID          int64             `json:"intent_id,string"`
	VerifiedAt        time.Time         `json:"verified_at"`
	AccountAfter      time.Time         `json:"account_after"`
	AccountVerifiedAt time.Time         `json:"account_verified_at"`
	Receipts          []SequenceReceipt `json:"receipts"`
}
type SequenceReceipt struct {
	GroupIndex   int    `json:"group_index"`
	MessageIndex int    `json:"message_index"`
	IntentID     int64  `json:"intent_id,string"`
	State        string `json:"state"`
}
type SequenceStep struct {
	PackageID     int64             `json:"package_id,string,omitempty"`
	PackageSHA256 string            `json:"package_sha256,omitempty"`
	PlanName      string            `json:"plan_name"`
	GroupCount    int               `json:"group_count"`
	MessageCount  int               `json:"message_count"`
	Materials     []SequenceMessage `json:"materials"`
	Command       string            `json:"command"`
	RunID         int64             `json:"run_id,string"`
	Revision      int64             `json:"revision"`
	GroupIndex    int               `json:"group_index"`
	MessageIndex  int               `json:"message_index"`
	ChatID        string            `json:"chat_id"`
	GroupName     string            `json:"group_name"`
	SenderUserID  string            `json:"sender_userid"`
	State         string            `json:"state"`
	Message       *SequenceMessage  `json:"message,omitempty"`
	IntentID      int64             `json:"intent_id,string,omitempty"`
	Grant         string            `json:"grant,omitempty"`
	CanStart      bool              `json:"can_start"`
}
type SequenceStepView struct {
	Step *SequenceStep `json:"step"`
}
type SequenceCommand struct {
	Action                    string
	RunID, Revision, DeviceID int64
	GroupIndex, MessageIndex  int
	Grant, Outcome            string
	EvidenceDigest            [32]byte
}
type Sequences interface {
	ReadSequenceConfig(context.Context, SidebarActor, int64) (SequenceConfig, error)
	SaveSequenceConfig(context.Context, SidebarActor, SequenceConfig) (SequenceConfig, error)
	ReadSequenceStep(context.Context, SidebarActor, int64) (SequenceStepView, error)
	MutateSequence(context.Context, SidebarActor, SequenceCommand) (SequenceStepView, error)
}
