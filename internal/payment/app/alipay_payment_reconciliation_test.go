package app

import (
	"context"
	"errors"
	"testing"
	"time"

	effectport "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/payment/domain"
	paymentport "github.com/qianlan33333-png/AI-CRM-standard/internal/payment/port"
)

type alipayPaymentReconcilerStub struct {
	query        paymentport.AlipayPaymentQuery
	queriedOrder string
	queryCalls   int
}

func (s *alipayPaymentReconcilerStub) QueryPayment(_ context.Context, merchantOrderNo string) (paymentport.AlipayPaymentQuery, error) {
	s.queriedOrder = merchantOrderNo
	s.queryCalls++
	return s.query, nil
}

func (*alipayPaymentReconcilerStub) QueryRefund(context.Context, string, string) (paymentport.AlipayRefundQuery, error) {
	return paymentport.AlipayRefundQuery{}, errors.New("unexpected refund query")
}

func TestAlipayClosedTradeKeepsVerifiedPaidPaymentAndRejectsMismatchedIdentity(t *testing.T) {
	now := time.Date(2026, 10, 17, 2, 22, 52, 0, time.UTC)
	transactionNo := "ALI-TRADE-1361"
	transactionDigest := effectport.Hash("alipay.transaction", transactionNo)
	for _, test := range []struct {
		name      string
		mutate    func(*paymentport.AlipayPaymentQuery)
		wantError bool
	}{
		{name: "same paid transaction"},
		{name: "different transaction", mutate: func(query *paymentport.AlipayPaymentQuery) {
			query.TradeNo = "ALI-OTHER-TRADE"
			query.TransactionDigest = effectport.Hash("alipay.transaction", query.TradeNo)
		}, wantError: true},
		{name: "missing transaction", mutate: func(query *paymentport.AlipayPaymentQuery) {
			query.TradeNo = ""
			query.TransactionDigest = effectport.Hash("alipay.transaction", query.TradeNo)
		}, wantError: true},
		{name: "mismatched transaction digest", mutate: func(query *paymentport.AlipayPaymentQuery) {
			query.TransactionDigest = effectport.Hash("alipay.transaction", "ALI-OTHER-TRADE")
		}, wantError: true},
		{name: "wrong amount", mutate: func(query *paymentport.AlipayPaymentQuery) {
			query.AmountMinor++
		}, wantError: true},
		{name: "wrong order", mutate: func(query *paymentport.AlipayPaymentQuery) {
			query.MerchantOrderNo = "other-order"
		}, wantError: true},
		{name: "missing evidence digest", mutate: func(query *paymentport.AlipayPaymentQuery) {
			query.EvidenceDigest = ""
		}, wantError: true},
		{name: "missing observation time", mutate: func(query *paymentport.AlipayPaymentQuery) {
			query.OccurredAt = time.Time{}
		}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			paidAt := now.Add(-time.Minute)
			store := &storeStub{payment: domain.Payment{
				ID: 1361, OrderID: 4001, Provider: domain.ProviderAlipay,
				MerchantOrderNo: "fixture-paid-alipay-order-1361", ProviderTransactionReference: transactionNo,
				ProviderTransactionDigest: string(transactionDigest), AmountMinor: 9900, Currency: "CNY",
				Status: domain.StatusPaid, Version: 4, PaidConfirmedAt: &paidAt, UpdatedAt: paidAt,
			}}
			query := paymentport.AlipayPaymentQuery{
				MerchantOrderNo: store.payment.MerchantOrderNo, TradeNo: transactionNo, TradeStatus: "TRADE_CLOSED",
				Currency: "CNY", AmountMinor: 9900, OccurredAt: now,
				EvidenceDigest: effectport.Hash("alipay.query", "closed-after-refund"), TransactionDigest: transactionDigest,
			}
			if test.mutate != nil {
				test.mutate(&query)
			}
			orders := &recordingOrderStub{}
			adapter := &alipayPaymentReconcilerStub{query: query}
			service := NewService(uowStub{}, store, orders, sessionStub{}, &effectStub{})
			if err := service.SetAlipayReconciler(adapter); err != nil {
				t.Fatal(err)
			}

			result, err := service.ReconcileAlipayPayment(context.Background(), store.payment.ID)
			if adapter.queriedOrder != store.payment.MerchantOrderNo {
				t.Fatalf("query did not use original merchant order: %q", adapter.queriedOrder)
			}
			if test.wantError {
				if !errors.Is(err, paymentport.ErrConflict) {
					t.Fatalf("mismatched Provider evidence was accepted: %v", err)
				}
				if store.paymentReconciliationCalls != 0 || store.paymentSettlementUpdates != 0 || orders.settlementCount != 0 {
					t.Fatalf("rejected evidence changed state: receipts=%d payment_updates=%d order_settlements=%d", store.paymentReconciliationCalls, store.paymentSettlementUpdates, orders.settlementCount)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != domain.StatusPaid || result.ProviderTransactionReference != transactionNo || result.ProviderTransactionDigest != string(transactionDigest) || result.AmountMinor != 9900 || result.PaidConfirmedAt == nil || !result.PaidConfirmedAt.Equal(paidAt) {
				t.Fatalf("original paid fact changed: %+v", result)
			}
			if store.paymentSettlementUpdates != 0 || orders.settlementCount != 0 || store.paymentReconciliationCalls != 1 || store.paymentReconciliationOutcome != "final_failed" {
				t.Fatalf("closed trade was settled over prior payment: receipts=%d outcome=%s payment_updates=%d order_settlements=%d", store.paymentReconciliationCalls, store.paymentReconciliationOutcome, store.paymentSettlementUpdates, orders.settlementCount)
			}
			if _, err = service.ReconcileAlipayPayment(context.Background(), store.payment.ID); err != nil {
				t.Fatalf("duplicate closed-trade query: %v", err)
			}
			if adapter.queryCalls != 2 || store.paymentSettlementUpdates != 0 || orders.settlementCount != 0 || store.payment.Status != domain.StatusPaid {
				t.Fatalf("duplicate closed-trade query changed settlement: calls=%d payment=%s payment_updates=%d order_settlements=%d", adapter.queryCalls, store.payment.Status, store.paymentSettlementUpdates, orders.settlementCount)
			}
		})
	}
}

func TestAlipayClosedTradeStillFailsUnpaidPayment(t *testing.T) {
	now := time.Date(2026, 10, 17, 2, 22, 52, 0, time.UTC)
	store := &storeStub{payment: domain.Payment{
		ID: 1362, OrderID: 4002, Provider: domain.ProviderAlipay,
		MerchantOrderNo: "unpaid-order-1362", AmountMinor: 9900, Currency: "CNY",
		Status: domain.StatusAwaitingPayment, Version: 2, UpdatedAt: now.Add(-time.Minute),
	}}
	orders := &recordingOrderStub{}
	service := NewService(uowStub{}, store, orders, sessionStub{}, &effectStub{})
	if err := service.SetAlipayReconciler(&alipayPaymentReconcilerStub{query: paymentport.AlipayPaymentQuery{
		MerchantOrderNo: store.payment.MerchantOrderNo, TradeStatus: "TRADE_CLOSED", Currency: "CNY", AmountMinor: 9900,
		OccurredAt: now, EvidenceDigest: effectport.Hash("alipay.query", "closed-unpaid"),
	}}); err != nil {
		t.Fatal(err)
	}

	result, err := service.ReconcileAlipayPayment(context.Background(), store.payment.ID)
	if err != nil || result.Status != domain.StatusFailed || store.paymentSettlementUpdates != 1 || orders.settlementCount != 1 || !orders.settlement.Failed {
		t.Fatalf("unpaid closed trade was not settled as failed: payment=%s updates=%d order_settlements=%d err=%v", result.Status, store.paymentSettlementUpdates, orders.settlementCount, err)
	}
}
