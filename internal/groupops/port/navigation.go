package port

import (
	"context"
	"time"
)

// Navigation is an employee-driven, open-only progress projection, never a send queue.
type NavigationTarget struct {
	AssetID int64  `json:"group_asset_id,string"`
	ChatID  string `json:"chat_id"`
	Name    string `json:"name"`
}
type NavigationObservation struct {
	Step       int       `json:"step"`
	ChatID     string    `json:"chat_id"`
	ObservedAt time.Time `json:"observed_at"`
}
type NavigationRun struct {
	ID           int64                   `json:"run_id,string"`
	PlanID       int64                   `json:"plan_id,string"`
	PlanName     string                  `json:"plan_name"`
	PlanRevision int64                   `json:"plan_revision"`
	StaffID      int64                   `json:"-"`
	SenderUserID string                  `json:"-"`
	Revision     int64                   `json:"revision"`
	State        string                  `json:"state"`
	ResumeState  string                  `json:"-"`
	Cursor       int                     `json:"cursor"`
	Targets      []NavigationTarget      `json:"targets"`
	Observations []NavigationObservation `json:"observations"`
	Sequence     *SequenceExecution      `json:"sequence,omitempty"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
}
type NavigationView struct {
	Run   *NavigationRun `json:"run"`
	Plans []SidebarPlan  `json:"plans"`
}
type NavigationCommand struct {
	Action              string
	PlanID              int64
	PlanRevision        int64
	RunID               int64
	Revision            int64
	Step                int
	ChatID              string
	Key                 string
	Mode                string
	AccountStaffID      int64
	AccountSenderUserID string
	AccountIssuedAt     time.Time
}
type Navigation interface {
	ReadNavigation(context.Context, SidebarActor) (NavigationView, error)
	MutateNavigation(context.Context, SidebarActor, NavigationCommand) (NavigationRun, error)
}
