package port

import "context"

type SalesSourceSelection struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id,omitempty"`
}
type SalesSource struct {
	ID         int64  `json:"id"`
	ProductID  ID     `json:"product_id"`
	Name       string `json:"name"`
	Version    int64  `json:"version"`
	PublicPath string `json:"public_path"`
}
type SalesSourcePage struct {
	Items        []SalesSource `json:"items"`
	StandardPath string        `json:"standard_path"`
}
type SalesSourceSnapshot struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}
type SalesSourceCommand struct {
	ProductID                        ID
	ProductType                      ProductOptionType
	SourceID, ExpectedVersion, Actor int64
	Name, IdempotencyKey             string
}
type SalesSourceApplication interface {
	ListSalesSources(context.Context, ID, ProductOptionType) (SalesSourcePage, error)
	SaveSalesSource(context.Context, SalesSourceCommand) (SalesSource, error)
}
type SalesSourceResolver interface {
	ResolveSalesSourceWithin(context.Context, ID, *SalesSourceSelection) (*SalesSourceSnapshot, error)
}
