package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	distributionapp "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/app"
	distributiondomain "github.com/qianlan33333-png/AI-CRM-standard/internal/distribution/domain"
	identityquery "github.com/qianlan33333-png/AI-CRM-standard/internal/identity/query"
	orderdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/order/domain"
	orderport "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	orderstore "github.com/qianlan33333-png/AI-CRM-standard/internal/order/store"
	paymentport "github.com/qianlan33333-png/AI-CRM-standard/internal/payment/port"
	platformconfig "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
	platformjobqueue "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/jobqueue"
	productapp "github.com/qianlan33333-png/AI-CRM-standard/internal/product/app"
	productstore "github.com/qianlan33333-png/AI-CRM-standard/internal/product/store"
	referraldomain "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/domain"
	referralport "github.com/qianlan33333-png/AI-CRM-standard/internal/referral/port"
	"github.com/riverqueue/river"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPostgreSQLReferralShopPurchaseMembershipJourney(t *testing.T) {
	f := newPromotionCheckoutFixture(t)
	defer f.close()
	ctx := context.Background()
	now := time.Now().UTC()
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	maintenanceContext := context.WithValue(ctx, purchaseMembershipMaintenanceContextKey{}, true)
	app, e := compose(maintenanceContext, platformconfig.Runtime{Role: platformconfig.RoleAPI, DatabaseURL: f.pool.Config().ConnConfig.ConnString(), PublicOrigin: "https://crm.example.test", ReleaseSHA: "shop-membership-test", WorkerOwner: "shop-membership-test", WorkerLimit: 1, Referral: platformconfig.Referral{TokenDataKey: key}, GroupOps: platformconfig.GroupOps{WebhookSecret: "synthetic-shop-membership"}, Survey: platformconfig.Survey{DataKey: key, IdentityPhoneDataKey: key}, WeChatPay: platformconfig.WeChatPay{AppID: "wx-promotion-checkout", AppScope: "wechat-app:promotion-checkout"}})
	if e != nil {
		t.Fatal(e)
	}
	defer app.Close()
	// The integration fixture represents already verified Provider facts. Provider adapter and refresh CAS are tested separately.
	app.referralMembership.BindPurchaseMembershipRefresh(nil)
	productRepository, _ := productstore.NewPostgreSQL(f.pool, f.uow)
	if e = f.orders.BindShopQualificationCatalog(productapp.ShopCatalog{Store: productRepository}); e != nil {
		t.Fatal(e)
	}
	var shopProduct, identity, campaign, shopOrder int64
	must := func(query string, args ...any) {
		t.Helper()
		if _, e := f.pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	if e = f.pool.QueryRow(ctx, `INSERT INTO products(product_code,name,price_minor,currency,stock_quantity,created_by,source_kind,legacy_admin_projection) VALUES('shop-membership-product','同一课程小店SKU',680,'CNY',0,1,'wechat_shop','{}') RETURNING id`).Scan(&shopProduct); e != nil {
		t.Fatal(e)
	}
	must(`INSERT INTO product_wechat_shop_skus(app_id,source_product_id,source_sku_id,product_id,imported_at) VALUES('shop-app','source-product','source-sku',$1,$2)`, shopProduct, now)
	if e = f.pool.QueryRow(ctx, `INSERT INTO customer_identities(customer_id,kind,scope_key,normalized_value,assurance,source,normalizer_version,verified_at) VALUES($1,'mp_openid','wechat-app:promotion-checkout','shop-only-member','verified','synthetic-test',1,$2) RETURNING id`, f.buyer, now).Scan(&identity); e != nil {
		t.Fatal(e)
	}
	createdCampaign, err := app.referralMaintenance.CreateCampaign(ctx, referralport.CreateCampaignCommand{ActorAdminID: 9001, Name: "活动6兼容回归", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), TeamMode: referraldomain.TeamModeIndividual, QualificationMode: referraldomain.QualificationProductPurchase, ProductID: f.productID, ProductType: "standard_product", LeaderboardMetric: referraldomain.LeaderboardInvites, IdempotencyKey: "shop-journey-create"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := app.referralMaintenance.SetCampaignState(ctx, referralport.SetCampaignStateCommand{CampaignID: createdCampaign.ID, ExpectedVersion: createdCampaign.Version, ActorAdminID: 9001, Target: referraldomain.CampaignActive, IdempotencyKey: "shop-journey-active"})
	if err != nil {
		t.Fatal(err)
	}
	campaign = active.ID
	// No native qualifying purchase exists for this customer.
	if e = f.pool.QueryRow(ctx, `INSERT INTO orders(provider,source_system,source_key,merchant_order_no,provider_transaction_no,payer_customer_id,beneficiary_customer_id,amount_minor,currency,status,record_origin,effect_eligible,version,created_at,updated_at) VALUES('wechat_shop','wechat-shop:shop-app','shop-only-order','shop-only-order','shop-paid-tx',$1,$1,680,'CNY','paid','external',false,2,$2,$2) RETURNING id`, f.buyer, now.Add(-time.Minute)).Scan(&shopOrder); e != nil {
		t.Fatal(e)
	}
	must(`INSERT INTO order_items(order_id,line_no,product_id,product_code,product_name,unit_amount_minor,quantity,line_amount_minor) VALUES($1,1,NULL,'shop-membership-product','同一课程小店SKU',680,1,680)`, shopOrder)
	digest := sha256.Sum256([]byte("verified-shop-union"))
	must(`INSERT INTO order_wechat_shop_facts(order_id,app_id,paid_snapshot,paid_at,activation_eligible,dispatch_complete,contact_ciphertext,contact_key_version,source_updated_at,immutable_digest,buyer_union_scope,buyer_union_digest,buyer_association_state,qualification_refund_state,qualification_checked_at) VALUES($1,'shop-app','{}',$2,true,true,'x',1,$2,$3,'wechat-open-platform:shop-fixture',$3,'linked','clear',$2)`, shopOrder, now.Add(-time.Minute), digest[:])
	must(`INSERT INTO order_wechat_shop_lines(order_id,line_no,source_product_id,source_sku_id) VALUES($1,1,'source-product','source-sku')`, shopOrder)
	var oldInvitation int64
	if e = f.pool.QueryRow(ctx, `INSERT INTO referral_invitations(campaign_id,inviter_customer_id,token_digest,state,created_at,expires_at) VALUES($1,$2,$3,'active',$4,$5) RETURNING id`, campaign, f.promoter.CustomerID, digest[:], now, now.Add(time.Hour)).Scan(&oldInvitation); e != nil {
		t.Fatal(e)
	}
	must(`INSERT INTO referral_current_relationships(customer_id,referrer_customer_id,source_campaign_id,invitation_id,version,effective_at) VALUES($1,$2,$3,$4,1,$5)`, f.buyer, f.promoter.CustomerID, campaign, oldInvitation, now)
	snapshot := func() string {
		var v string
		if e = f.pool.QueryRow(ctx, `SELECT json_build_array((SELECT count(*) FROM order_paid_events),(SELECT count(*) FROM distribution_commissions),(SELECT count(*) FROM distribution_order_attributions),(SELECT count(*) FROM external_effects),(SELECT row_to_json(r) FROM referral_current_relationships r WHERE customer_id=$1))::text`, f.buyer).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	before := snapshot()
	result, e := app.referralMembership.ReconcilePurchaseMembers(ctx, campaign, f.buyer, true)
	if e != nil || result.Created != 0 {
		t.Fatalf("unconfigured compatibility: %+v %v", result, e)
	}
	ids := []int64{shopProduct}
	configured, e := app.referralMaintenance.ConfigurePurchaseCompatibilityForOperator(ctx, campaign, ids, "synthetic-approved-plan", true)
	if e != nil {
		t.Fatal(e)
	}
	if len(configured.CompatibleProductIDs) != 1 {
		t.Fatal("configuration not retained")
	}
	// Missing/conflicting verified identity and a different original SKU must
	// not be converted into purchase proof merely because the names match.
	for _, test := range []struct{ name, mutate, restore string }{
		{"identity-pending", `UPDATE order_wechat_shop_facts SET buyer_association_state='pending' WHERE order_id=$1`, `UPDATE order_wechat_shop_facts SET buyer_association_state='linked' WHERE order_id=$1`},
		{"identity-conflict", `UPDATE order_wechat_shop_facts SET buyer_association_state='conflict' WHERE order_id=$1`, `UPDATE order_wechat_shop_facts SET buyer_association_state='linked' WHERE order_id=$1`},
		{"missing-scoped-union", `UPDATE order_wechat_shop_facts SET buyer_union_scope='',buyer_union_digest=NULL WHERE order_id=$1`, `UPDATE order_wechat_shop_facts SET buyer_union_scope='wechat-open-platform:shop-fixture',buyer_union_digest=immutable_digest WHERE order_id=$1`},
		{"wrong-sku", `UPDATE order_wechat_shop_lines SET source_sku_id='different-sku' WHERE order_id=$1`, `UPDATE order_wechat_shop_lines SET source_sku_id='source-sku' WHERE order_id=$1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			must(test.mutate, shopOrder)
			r, err := app.referralMembership.ReconcilePurchaseMembers(ctx, campaign, f.buyer, false)
			if err != nil || r.Created != 0 {
				t.Fatalf("untrusted proof granted membership: %+v %v", r, err)
			}
			must(test.restore, shopOrder)
		})
	}
	if e = f.uow.Within(ctx, func(tx context.Context) error {
		_, e := app.nativeOrders.ReadShopQualificationEvidenceWithin(tx, []int64{f.buyer}, shopProduct)
		return e
	}); e != nil {
		t.Fatal("shop evidence query", e)
	}
	preview, e := app.referralMembership.ReconcilePurchaseMembers(ctx, campaign, f.buyer, false)
	if e != nil || preview.Created != 1 {
		t.Fatalf("preview %+v %v", preview, e)
	}
	var count int
	f.pool.QueryRow(ctx, `SELECT count(*) FROM referral_participations WHERE campaign_id=$1`, campaign).Scan(&count)
	if count != 0 {
		t.Fatal("preview wrote membership")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	recoveryActor := referralport.TrustedSessionActor{CustomerID: f.buyer, IdentityID: identity, AppID: "wx-promotion-checkout", AppScope: "wechat-app:promotion-checkout", Channel: "mini_program", OccurredAt: now}
	for n := 0; n < 6; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if n == 0 {
				// A token encountered after purchase is not historical invitation proof.
				_, err := app.referralMembership.JoinCampaign(ctx, referralport.JoinCampaignCommand{Actor: recoveryActor, CampaignID: campaign, InvitationToken: "rfi_" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)), IdempotencyKey: "shop-entry-recovery-other-link"})
				errs <- err
				return
			}
			_, e := app.referralMembership.ReconcilePurchaseMembers(ctx, campaign, f.buyer, true)
			errs <- e
		}(n)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	f.pool.QueryRow(ctx, `SELECT count(*) FROM referral_participations WHERE campaign_id=$1`, campaign).Scan(&count)
	if count != 1 || snapshot() != before {
		t.Fatal("duplicate membership or payment/commission/relationship side effects")
	}
	actor := referralport.TrustedSessionActor{CustomerID: f.buyer, IdentityID: identity, AppID: "wx-promotion-checkout", AppScope: "wechat-app:promotion-checkout", Channel: "mini_program", OccurredAt: now}
	sessions, _ := distributionapp.NewBrowserSessionService(f.uow, f.distributions)
	session, _, e := sessions.Issue(ctx, actor)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(app.handler)
	defer server.Close()
	if *domesticReleaseSmokeInputFile != "" {
		preparePaidReferralInstalledDirectory(t, f, []int64{f.buyer, f.promoter.CustomerID, f.otherPromoter.CustomerID})
		inputs, binary, root := referralInstalledArtifact(t)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.Addr().String()
		listener.Close()
		environment := paidReferralInstalledEnvironment(f.pool.Config().ConnConfig.ConnString(), inputs.InstalledSHA, address)
		for n, value := range environment {
			k, _, _ := strings.Cut(value, "=")
			if k == "AICRM_PUBLIC_ORIGIN" || k == "AICRM_H5_PUBLIC_ORIGIN" {
				environment[n] = k + "=https://crm.example.test"
			}
		}
		process := startDomesticSmokeProcess(t, binary, root, environment)
		defer func() {
			if !process.finished {
				_ = process.command.Process.Signal(os.Interrupt)
				select {
				case err := <-process.done:
					process.finished = true
					if err != nil {
						t.Error(err)
					}
				case <-time.After(8 * time.Second):
					_ = process.command.Process.Kill()
					<-process.done
					process.finished = true
					t.Error("installed API stop timeout")
				}
			}
		}()
		waitDomesticReleaseReady(t, process, "http://"+address, inputs.InstalledSHA)
		target, _ := url.Parse("http://" + address)
		server.Config.Handler = httputil.NewSingleHostReverseProxy(target)
		t.Logf("shop_membership_installed_http: source=%s installed=%s (service/financial fixture remains source composition)", inputs.SourceSHA, inputs.InstalledSHA)
	}
	path := fmt.Sprintf("/api/v1/referral/campaigns/%d/me", campaign)
	readMe := func() map[string]any {
		t.Helper()
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		req.AddCookie(&http.Cookie{Name: "aicrm_distribution_session", Value: session})
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var body map[string]any
		json.NewDecoder(res.Body).Decode(&body)
		if res.StatusCode != 200 {
			t.Fatalf("me %d %+v", res.StatusCode, body)
		}
		return body
	}
	body := readMe()
	if body["participation"] == nil || body["invitation_available"] != true {
		t.Fatalf("no durable poster access %+v", body)
	}
	link, e := app.referralMembership.IssueInvitation(ctx, referralport.IssueInvitationCommand{Actor: actor, CampaignID: campaign, IdempotencyKey: "shop-own-invitation"})
	if e != nil || !strings.Contains(link.URL, "/referral/invite/") {
		t.Fatalf("own invitation %+v %v", link, e)
	}
	// Exercise real Order checkout -> attribution -> paid event -> commission using only Shop purchase proof.
	if e = f.qualification.BindPurchaseScopes(f.distributions); e != nil {
		t.Fatal(e)
	}
	if e = f.promotion.BindPaidInvitationCheckoutReader(app.referralMembership); e != nil {
		t.Fatal(e)
	}
	if e = f.promotion.BindProfitSharingSettlement(shopReceiverPreparation{}); e != nil {
		t.Fatal(e)
	}
	if e = f.promotion.BindIdentitySubjects(identityquery.NewPostgreSQL()); e != nil {
		t.Fatal(e)
	}
	if e = f.orders.SetProductSaleCheckoutCoordinator(app.referralMembership); e != nil {
		t.Fatal(e)
	}
	workers := river.NewWorkers()
	if e = river.AddWorkerSafely[distributionapp.CommissionDueJobArgs](workers, distributionapp.NewCommissionDueWorker()); e != nil {
		t.Fatal(e)
	}
	jobs, e := platformjobqueue.NewInsertClient(f.pool, workers)
	if e != nil {
		t.Fatal(e)
	}
	due, e := distributionapp.NewRiverCommissionDueEnqueuer(jobs)
	if e != nil {
		t.Fatal(e)
	}
	commissions, e := distributionapp.NewCommissionService(f.distributions, due, f.qualification)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.orders.SetPaidEventConsumer(orderPaidEventFanout{commerce: noPaymentConsumerFunc(func(context.Context, orderport.PaidEvent) error { return nil }), purchase: noPaymentConsumerFunc(func(context.Context, orderport.PaidEvent) error { return nil }), distribution: commissions, referral: app.referralMembership}); e != nil {
		t.Fatal(e)
	}
	token := strings.TrimPrefix(link.URL, "https://crm.example.test/referral/invite/")
	var purchase orderdomain.Snapshot
	if e = f.uow.Within(ctx, func(tx context.Context) error {
		var err error
		purchase, err = f.orders.CreatePaymentOrderWithin(tx, orderport.PaymentOrderCommand{Provider: orderdomain.ProviderWeChatPay, MerchantOrderNo: "M-shop-promoted-native", PayerCustomerID: f.promoter.CustomerID, BeneficiaryCustomerID: f.promoter.CustomerID, ProductID: f.productID, ProductCode: "promotion-checkout-product", ProductName: "分销归因商品", ProductVersion: 1, ProductType: "standard_product", UnitAmountMinor: 1000, Currency: "CNY", ReferralActivityContext: token, ActorScope: "checkout:shop-referral", IdempotencyKey: "shop-referral-checkout"})
		return err
	}); e != nil {
		t.Fatal(e)
	}
	var marked bool
	if e = f.pool.QueryRow(ctx, `SELECT profit_sharing_required FROM order_checkout_snapshots WHERE order_id=$1`, purchase.ID).Scan(&marked); e != nil || !marked {
		t.Fatalf("not split-marked before payment: %v %v", marked, e)
	}
	if e = f.uow.Within(ctx, func(tx context.Context) error {
		_, e := f.orders.SettlePaymentWithin(tx, orderport.PaymentSettlementCommand{OrderID: purchase.ID, ProviderTransactionNo: "synthetic-shop-promoted-tx", OccurredAt: purchase.CreatedAt.Add(time.Millisecond), ReceiptKey: "shop-promoted-paid"})
		return e
	}); e != nil {
		t.Fatal(e)
	}
	var attributionID, commissionMinor int64
	var commissionState string
	if e = f.pool.QueryRow(ctx, `SELECT a.id,c.initial_minor,c.status FROM distribution_order_attributions a JOIN distribution_commissions c ON c.attribution_id=a.id JOIN distribution_distributors d ON d.id=a.distributor_id WHERE a.order_id=$1 AND d.customer_id=$2`, purchase.ID, f.buyer).Scan(&attributionID, &commissionMinor, &commissionState); e != nil || commissionMinor != 100 || commissionState != "pending" {
		t.Fatalf("real commission %d %s %v", commissionMinor, commissionState, e)
	}
	var frozenInviter, frozenInvitation int64
	if e = f.pool.QueryRow(ctx, `SELECT inviter_customer_id,invitation_id FROM referral_participations WHERE campaign_id=$1 AND customer_id=$2`, campaign, f.promoter.CustomerID).Scan(&frozenInviter, &frozenInvitation); e != nil || frozenInviter != f.buyer || frozenInvitation < 1 {
		t.Fatalf("paid member lost frozen inviter: %d/%d %v", frozenInviter, frozenInvitation, e)
	}
	// Frozen scope survives removing compatibility for future entries.
	if _, e = app.referralMaintenance.ConfigurePurchaseCompatibilityForOperator(ctx, campaign, []int64{}, "synthetic-removal", true); e != nil {
		t.Fatal(e)
	}
	body = readMe()
	if body["participation"] == nil || body["invitation_available"] != false {
		t.Fatalf("removed compatibility ignored %+v", body)
	}
	if e = f.uow.Within(ctx, func(tx context.Context) error {
		q, e := f.qualification.CheckAttributionWithin(tx, attributionID, f.buyer, f.productID, distributiondomain.ProductTypeStandard)
		if e != nil {
			return e
		}
		if !q.AllowsPromotion() {
			return fmt.Errorf("frozen scope lost: %s", q.State)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	// A newly observed refund invalidates the old frozen scope as well; only the rule scope is frozen, never money facts.
	must(`UPDATE order_wechat_shop_facts SET qualification_refund_state='pending' WHERE order_id=$1`, shopOrder)
	if e = f.uow.Within(ctx, func(tx context.Context) error {
		q, e := f.qualification.CheckAttributionWithin(tx, attributionID, f.buyer, f.productID, distributiondomain.ProductTypeStandard)
		if e != nil {
			return e
		}
		if q.State != distributiondomain.QualificationSuspended {
			return fmt.Errorf("refund bypassed: %s", q.State)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	// Restore configured scope and exercise null-product-ID historic SKU recovery through the Product port.
	if _, e = app.referralMaintenance.ConfigurePurchaseCompatibilityForOperator(ctx, campaign, ids, "synthetic-readd", true); e != nil {
		t.Fatal(e)
	}
	// The original fixture item intentionally has no local product ID.
	must(`UPDATE order_wechat_shop_facts SET qualification_refund_state='clear' WHERE order_id=$1`, shopOrder)
	if body = readMe(); body["invitation_available"] != true {
		t.Fatalf("historical SKU mapping lost %+v", body)
	}
	// Compare-and-swap prevents stale official reads from clearing a later refund.
	store, _ := orderstore.NewPostgreSQL(f.pool, f.uow)
	var targets []orderport.ShopQualificationRefreshTarget
	if e = f.uow.Within(ctx, func(tx context.Context) error {
		var err error
		targets, err = store.ListShopQualificationRefreshCoordinatesWithin(tx, []int64{f.buyer}, "shop-app", "source-product", "source-sku")
		return err
	}); e != nil || len(targets) != 1 {
		t.Fatalf("legacy refresh targets %d %v", len(targets), e)
	}
	target := targets[0]
	must(`UPDATE order_wechat_shop_facts SET qualification_refund_state='pending',qualification_checked_at=clock_timestamp() WHERE order_id=$1`, shopOrder)
	source := orderport.ShopOrder{ID: target.SourceOrderID, AppID: target.AppID, TransactionID: target.TransactionID, PaidAt: target.PaidAt, PaidMinor: target.PaidMinor, QualificationRefundState: "clear"}
	if e = f.uow.Within(ctx, func(tx context.Context) error {
		return store.UpdateShopQualificationRefreshWithin(tx, target, source, nil)
	}); e == nil {
		t.Fatal("stale official result overwrote later pending refund")
	}
	var state string
	f.pool.QueryRow(ctx, `SELECT qualification_refund_state FROM order_wechat_shop_facts WHERE order_id=$1`, shopOrder).Scan(&state)
	if state != "pending" {
		t.Fatal("refund state regressed")
	}
	if _, e = app.referralMembership.IssueInvitation(ctx, referralport.IssueInvitationCommand{Actor: actor, CampaignID: campaign, IdempotencyKey: "shop-own-invitation"}); e == nil {
		t.Fatal("invitation receipt replay bypassed refund")
	}
	// A restored purchase cannot undo an operator's membership revocation.
	must(`UPDATE order_wechat_shop_facts SET qualification_refund_state='clear' WHERE order_id=$1`, shopOrder)
	must(`UPDATE referral_participations SET state='reversed' WHERE campaign_id=$1 AND customer_id=$2`, campaign, f.buyer)
	if r, err := app.referralMembership.ReconcilePurchaseMembers(ctx, campaign, f.buyer, true); err != nil || r.Created != 0 || r.Existing != 1 {
		t.Fatalf("revoked member was recreated: %+v %v", r, err)
	}
	if body = readMe(); body["invitation_available"] != false {
		t.Fatalf("revoked member can promote: %+v", body)
	}
	t.Log("real HTTP/PG: shop-only qualification, configured scope, automatic idempotent membership, preserved history, frozen financial scope, refund rejection and historic SKU resolution passed")
}

type shopReceiverPreparation struct {
	paymentport.DistributionSettlementPort
}

func (shopReceiverPreparation) PrepareProfitSharingReceiverWithin(_ context.Context, c paymentport.ReceiverPreparation) (paymentport.ReceiverReadiness, error) {
	return paymentport.ReceiverReadiness{CustomerID: c.CustomerID, AppID: c.AppID, Reference: "receiver-shop-qualification-fixture", Ready: false, State: "accepted"}, nil
}
