package port

import (
	"context"
	"time"
)

// Sidebar plans expose neither message content nor webhook configuration.
type SidebarPlan struct {
	ID       int64      `json:"plan_id,string"`
	Name     string     `json:"name"`
	Status   PlanStatus `json:"status"`
	Revision int64      `json:"revision"`
}

type SidebarPlans struct {
	Bindings   []SidebarPlan `json:"bindings"`
	Candidates []SidebarPlan `json:"candidates"`
	Total      int64         `json:"total"`
	Limit      int32         `json:"limit"`
	Offset     int32         `json:"offset"`
}

// SidebarPlanReader runs inside the caller's Unit of Work. Group Ops owns
// every table involved; there is no Customer/OneID dependency.
type SidebarPlanReader interface {
	ReadSidebarPlans(context.Context, int64, string, string, int32, int32) (SidebarPlans, error)
}

type SidebarActor struct {
	StaffID     int64
	WeComUserID string
	DisplayName string
}

type SidebarContext struct {
	ChatID    string    `json:"chat_id"`
	Name      string    `json:"name"`
	OwnerName string    `json:"owner_name"`
	CheckedAt time.Time `json:"checked_at"`
	SidebarPlans
	ProviderReadExecuted bool `json:"provider_read_executed"`
}

type SidebarBindCommand struct {
	ChatID           string
	PlanID           int64
	ExpectedRevision int64
	IdempotencyKey   string
}

type SidebarBindResult struct {
	State string      `json:"state"`
	Plan  SidebarPlan `json:"plan"`
}

type SidebarBindings interface {
	SidebarContext(context.Context, SidebarActor, string, string, int32, int32) (SidebarContext, error)
	BindSidebarGroup(context.Context, SidebarActor, SidebarBindCommand) (SidebarBindResult, error)
}
