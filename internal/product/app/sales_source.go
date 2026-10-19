package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type salesSourceStore interface {
	ListSalesSources(context.Context, productport.ID) ([]productport.SalesSource, error)
	ReadSalesSource(context.Context, productport.ID, int64) (productport.SalesSource, error)
	SaveSalesSource(context.Context, productport.SalesSourceCommand, time.Time) (productport.SalesSource, error)
}

func (s *Service) salesSourceProduct(ctx context.Context, id productport.ID, kind productport.ProductOptionType) (string, error) {
	var p productport.Product
	var e error
	if kind == productport.ProductOptionServicePeriod {
		repo, ok := s.store.(ServicePeriodStore)
		if !ok {
			return "", ErrUnavailable
		}
		p, e = repo.GetServicePeriodProduct(ctx, id)
	} else if kind == productport.ProductOptionStandard {
		p, e = s.store.Get(ctx, id)
	} else {
		return "", ErrInvalidProduct
	}
	if e != nil {
		return "", e
	}
	if IsServicePeriodProjection(p.LegacyAdminProjection) != (kind == productport.ProductOptionServicePeriod) {
		return "", ErrNotFound
	}
	prefix := "/p/"
	if kind == productport.ProductOptionServicePeriod {
		prefix = "/s/"
	}
	return prefix + url.PathEscape(p.ProductCode), nil
}
func (s *Service) ListSalesSources(ctx context.Context, id productport.ID, kind productport.ProductOptionType) (out productport.SalesSourcePage, err error) {
	if !ready(s) || id < 1 {
		return out, ErrInvalidProduct
	}
	repo, ok := s.store.(salesSourceStore)
	if !ok {
		return out, ErrUnavailable
	}
	err = s.uow.Within(ctx, func(tx context.Context) error {
		var e error
		out.StandardPath, e = s.salesSourceProduct(tx, id, kind)
		if e != nil {
			return e
		}
		out.Items, e = repo.ListSalesSources(tx, id)
		if e != nil {
			return e
		}
		for i := range out.Items {
			out.Items[i].PublicPath = out.StandardPath + "?sales_source=" + strconv.FormatInt(out.Items[i].ID, 10)
		}
		return nil
	})
	return out, classify(err)
}
func (s *Service) SaveSalesSource(ctx context.Context, c productport.SalesSourceCommand) (out productport.SalesSource, err error) {
	c.Name = strings.TrimSpace(c.Name)
	if !ready(s) || c.ProductID < 1 || c.Actor < 1 || c.Name == "" || len(c.Name) > 500 || strings.ContainsAny(c.Name, "\x00\r\n") || !validIdempotencyKey(c.IdempotencyKey) || c.SourceID < 0 || (c.SourceID > 0 && c.ExpectedVersion < 1) {
		return out, ErrInvalidProduct
	}
	repo, ok := s.store.(salesSourceStore)
	if !ok {
		return out, ErrUnavailable
	}
	payload, _ := json.Marshal(struct {
		ProductID   productport.ID
		Type        productport.ProductOptionType
		ID, Version int64
		Name        string
	}{c.ProductID, c.ProductType, c.SourceID, c.ExpectedVersion, c.Name})
	at := s.now().UTC()
	actor := fmt.Sprintf("admin:%d", c.Actor)
	digest := sha256.Sum256(payload)
	reservation := Reservation{Operation: "sales_source.save", ActorScope: actor, KeyDigest: sha256.Sum256([]byte(c.IdempotencyKey)), PayloadDigest: digest, CreatedAt: at}
	err = s.uow.Within(ctx, func(tx context.Context) error {
		path, e := s.salesSourceProduct(tx, c.ProductID, c.ProductType)
		if e != nil {
			return e
		}
		receipt, owned, e := s.store.Reserve(tx, reservation)
		if e != nil {
			return e
		}
		if receipt.PayloadDigest != digest {
			return ErrConflict
		}
		if !owned {
			if receipt.State != "completed" || json.Unmarshal(receipt.ResultSnapshot, &out) != nil {
				return ErrUnavailable
			}
			return nil
		}
		out, e = repo.SaveSalesSource(tx, c, at)
		if e != nil {
			return e
		}
		out.PublicPath = path + "?sales_source=" + strconv.FormatInt(out.ID, 10)
		eventPayload, _ := json.Marshal(map[string]any{"product_id": c.ProductID, "actor": c.Actor, "source_id": out.ID, "name": out.Name, "version": out.Version})
		key := sha256.Sum256([]byte(actor + "\x00" + c.IdempotencyKey))
		if _, e = s.events.Append(tx, productport.Event{Type: "product.sales_source.saved", Payload: eventPayload, OccurredAt: at, IdempotencyKey: "product.sales-source:" + hex.EncodeToString(key[:])}); e != nil {
			return e
		}
		snapshot, _ := json.Marshal(out)
		_, e = s.store.Complete(tx, receipt.ID, snapshot, at)
		return e
	})
	return out, classify(err)
}
func (s *Service) ResolveSalesSourceWithin(ctx context.Context, productID productport.ID, selection *productport.SalesSourceSelection) (*productport.SalesSourceSnapshot, error) {
	if selection == nil {
		return nil, nil
	}
	out := &productport.SalesSourceSnapshot{Kind: "unrecorded"}
	if selection.Kind == "standard" && selection.ID == 0 {
		out.Kind = "standard"
		return out, nil
	}
	if selection.Kind != "channel" || selection.ID < 1 {
		return out, nil
	}
	repo, ok := s.store.(salesSourceStore)
	if !ok {
		return nil, ErrUnavailable
	}
	source, e := repo.ReadSalesSource(ctx, productID, selection.ID)
	if errors.Is(e, productport.ErrProductReadNotFound) || errors.Is(e, ErrNotFound) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	return &productport.SalesSourceSnapshot{Kind: "channel", ID: source.ID, Name: source.Name}, nil
}
