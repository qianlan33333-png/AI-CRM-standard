package app

import (
	"context"
	"errors"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"testing"
	"time"
)

type sourceTestStore struct {
	*productTestStore
	sources []productport.SalesSource
	readErr error
	reads   int
}

func (s *sourceTestStore) ListSalesSources(context.Context, productport.ID) ([]productport.SalesSource, error) {
	return append([]productport.SalesSource{}, s.sources...), nil
}
func (s *sourceTestStore) ReadSalesSource(_ context.Context, p productport.ID, id int64) (productport.SalesSource, error) {
	s.reads++
	if s.readErr != nil {
		return productport.SalesSource{}, s.readErr
	}
	for _, v := range s.sources {
		if v.ID == id && v.ProductID == p {
			return v, nil
		}
	}
	return productport.SalesSource{}, productport.ErrProductReadNotFound
}
func (s *sourceTestStore) SaveSalesSource(_ context.Context, c productport.SalesSourceCommand, _ time.Time) (productport.SalesSource, error) {
	if c.SourceID == 0 {
		v := productport.SalesSource{ID: int64(len(s.sources) + 1), ProductID: c.ProductID, Name: c.Name, Version: 1}
		s.sources = append(s.sources, v)
		return v, nil
	}
	for i, v := range s.sources {
		if v.ID == c.SourceID && v.ProductID == c.ProductID && v.Version == c.ExpectedVersion {
			v.Name = c.Name
			v.Version++
			s.sources[i] = v
			return v, nil
		}
	}
	return productport.SalesSource{}, ErrConflict
}
func TestSalesSourceSelectionIsAttributionOnly(t *testing.T) {
	repo := &sourceTestStore{productTestStore: &productTestStore{}, sources: []productport.SalesSource{{ID: 8, ProductID: 5, Name: "A", Version: 1}}}
	svc := NewService(&productTestUoW{}, repo, &productTestEvents{})
	ctx := context.Background()
	for _, tc := range []struct {
		input *productport.SalesSourceSelection
		kind  string
		id    int64
	}{{nil, "", 0}, {&productport.SalesSourceSelection{Kind: "standard"}, "standard", 0}, {&productport.SalesSourceSelection{Kind: "channel", ID: 8}, "channel", 8}, {&productport.SalesSourceSelection{Kind: "channel", ID: 9}, "unrecorded", 0}, {&productport.SalesSourceSelection{Kind: "standard", ID: 8}, "unrecorded", 0}, {&productport.SalesSourceSelection{Kind: "bogus"}, "unrecorded", 0}} {
		got, e := svc.ResolveSalesSourceWithin(ctx, 5, tc.input)
		if e != nil {
			t.Fatal(e)
		}
		if tc.input == nil {
			if got != nil {
				t.Fatal("old client was relabeled")
			}
			continue
		}
		if got.Kind != tc.kind || got.ID != tc.id {
			t.Fatalf("got %+v for %+v", got, tc.input)
		}
	}
	got, e := svc.ResolveSalesSourceWithin(ctx, 6, &productport.SalesSourceSelection{Kind: "channel", ID: 8})
	if e != nil || got.Kind != "unrecorded" {
		t.Fatal("cross-product attribution", got, e)
	}
	repo.readErr = errors.New("read unavailable")
	if _, e = svc.ResolveSalesSourceWithin(ctx, 5, &productport.SalesSourceSelection{Kind: "channel", ID: 8}); e == nil {
		t.Fatal("read error silently attributed")
	}
}
func TestSalesSourceCreateRenameReplayAndFreshList(t *testing.T) {
	repo := &sourceTestStore{productTestStore: &productTestStore{products: []productport.Product{validTestProduct(5)}}}
	events := &productTestEvents{}
	svc := NewService(&productTestUoW{}, repo, events)
	c := productport.SalesSourceCommand{ProductID: 5, ProductType: productport.ProductOptionStandard, Name: "Channel A", Actor: 7, IdempotencyKey: "source-create-0001"}
	first, e := svc.SaveSalesSource(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := svc.SaveSalesSource(context.Background(), c)
	if e != nil || first.ID != replay.ID || len(repo.sources) != 1 || len(events.events) != 1 {
		t.Fatal("duplicate create", replay, e)
	}
	c.Name = "changed same key"
	if _, e = svc.SaveSalesSource(context.Background(), c); !errors.Is(e, ErrConflict) {
		t.Fatal("same key changed request", e)
	}
	c.SourceID = first.ID
	c.ExpectedVersion = 1
	c.Name = "群 B"
	c.IdempotencyKey = "source-rename-0001"
	renamed, e := svc.SaveSalesSource(context.Background(), c)
	if e != nil || renamed.Version != 2 || renamed.PublicPath != first.PublicPath {
		t.Fatal("rename", renamed, e)
	}
	page, e := svc.ListSalesSources(context.Background(), 5, productport.ProductOptionStandard)
	if e != nil || len(page.Items) != 1 || page.Items[0].Name != "群 B" {
		t.Fatal("fresh read", page, e)
	}
	c.IdempotencyKey = "source-stale-0001"
	if _, e = svc.SaveSalesSource(context.Background(), c); !errors.Is(e, ErrConflict) {
		t.Fatal("stale rename", e)
	}
}
