package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestShopQualificationOfficialAftersaleStatesFailClosed(t *testing.T) {
	for _, v := range []struct{ status, want string }{
		{"USER_CANCELD", "clear"}, {"MERCHANT_REJECT_REFUND", "clear"}, {"MERCHANT_PROCESSING", "pending"}, {"PLATFORM_REFUND_FAIL", "pending"}, {"MERCHANT_WAIT_RECEIPT", "pending"}, {"NEW_UNKNOWN_STATE", "unknown"}, {"MERCHANT_REFUND_SUCCESS", "refunded"},
	} {
		t.Run(v.status, func(t *testing.T) {
			p, _ := NewWeChatShop(ShopConfig{Enabled: true, AppID: "shop", AppSecret: "fixture", APIBaseURL: "https://api.weixin.qq.com"}, loaderStub{}, doerFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == shopTokenPath {
					return shopResponse(`{"access_token":"fixture","expires_in":7200}`), nil
				}
				return shopResponse(`{"errcode":0,"after_sale_order":{"after_sale_order_id":"a","order_id":"o","status":"` + v.status + `","refund_info":{"amount":68}}}`), nil
			}))
			_, state, e := p.shopRefundFact(context.Background(), "o", "a")
			if e != nil || state != v.want {
				t.Fatalf("%s %v", state, e)
			}
		})
	}
}

func TestShopQualificationRefundCompletionCannotBeMaskedByOtherAftersales(t *testing.T) {
	var firstOrderRequest time.Time
	p, _ := NewWeChatShop(ShopConfig{Enabled: true, AppID: "shop", AppSecret: "fixture", APIBaseURL: "https://api.weixin.qq.com"}, loaderStub{}, doerFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case shopTokenPath:
			return shopResponse(`{"access_token":"fixture","expires_in":7200}`), nil
		case shopOrderPath:
			firstOrderRequest = time.Now().UTC()
			return shopResponse(`{"errcode":0,"order":{"order_id":"o","create_time":1700000000,"update_time":1700010000,"order_detail":{"price_info":{"order_price":68},"pay_info":{"payment_method":1,"pay_time":1700009999,"transaction_id":"tx"},"product_infos":[{"product_id":"p","sku_id":"s","title":"课程","sale_price":68,"real_price":68,"sku_cnt":1}]},"aftersale_detail":{"aftersale_order_list":[{"aftersale_order_id":"complete"},{"aftersale_order_id":"pending"},{"aftersale_order_id":"unknown"}]}}}`), nil
		default:
			var v map[string]string
			if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
				return nil, err
			}
			status := map[string]string{"complete": "MERCHANT_RETURN_SUCCESS", "pending": "MERCHANT_PROCESSING", "unknown": "FUTURE_UNKNOWN"}[v["after_sale_order_id"]]
			return shopResponse(`{"errcode":0,"after_sale_order":{"after_sale_order_id":"` + v["after_sale_order_id"] + `","order_id":"o","status":"` + status + `","refund_info":{"amount":0}}}`), nil
		}
	}))
	v, err := p.ReadShopOrder(context.Background(), "o")
	if err != nil || v.QualificationRefundState != "refunded" {
		t.Fatalf("completed return was masked: %s %v", v.QualificationRefundState, err)
	}
	if v.QualificationReadStartedAt.IsZero() || firstOrderRequest.IsZero() || v.QualificationReadStartedAt.After(firstOrderRequest) {
		t.Fatal("observation time did not precede the Provider order request")
	}
}
