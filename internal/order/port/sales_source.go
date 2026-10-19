package port

import (
	"context"
	"strconv"
)

// SalesSourceSnapshot is a frozen Order fact, never a buyer authorization.
type SalesSourceSnapshot struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func (s *SalesSourceSnapshot) Valid() bool {
	if s == nil {
		return true
	}
	switch s.Kind {
	case "standard", "unrecorded":
		return s.ID == 0 && s.Name == ""
	case "channel":
		return s.ID > 0 && s.Name != "" && len(s.Name) <= 500
	}
	return false
}
func (s *SalesSourceSnapshot) Label() string {
	if s == nil {
		return "未记录"
	}
	if s.Kind == "standard" {
		return "标准渠道"
	}
	if s.Kind == "channel" {
		return s.Name
	}
	return "未记录"
}
func ValidSalesSourceFilter(s string) bool {
	if s == "" || s == "standard" || s == "unrecorded" {
		return true
	}
	id, e := strconv.ParseInt(s, 10, 64)
	return e == nil && id > 0 && strconv.FormatInt(id, 10) == s
}

type SalesSourceReader interface {
	ReadOrderSalesSources(context.Context, []int64) (map[int64]*SalesSourceSnapshot, error)
}
