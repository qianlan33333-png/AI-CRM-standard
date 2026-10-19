package port

import (
	"context"
	"time"
)

// ProfileField is a display projection, not an identity or a current intent.
type ProfileField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

type CustomerProfile struct {
	Mapped     bool
	Version    string
	UpdatedAt  time.Time
	Basic      []ProfileField
	Background []ProfileField
	Diagnosis  []ProfileField
}

// CustomerProfileReader accepts only the unique provider-verified UnionID
// selected by Identity in the configured open-platform scope. Values remain
// transient inside composition/provider; neither browser nor logs receive it.
type CustomerProfileReader interface {
	ReadCustomerProfile(context.Context, string) (CustomerProfile, error)
}
