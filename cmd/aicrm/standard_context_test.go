package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	accessdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	automationapp "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/app"
	automationport "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/port"
	automationstore "github.com/qianlan33333-png/AI-CRM-standard/internal/automation/store"
	customerapp "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/app"
	customerdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	customerport "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/port"
	customerstore "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/store"
	"github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects"
	effectport "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
	archiveport "github.com/qianlan33333-png/AI-CRM-standard/internal/messagearchive/port"
	operationapp "github.com/qianlan33333-png/AI-CRM-standard/internal/operationcycle/app"
	operationhttp "github.com/qianlan33333-png/AI-CRM-standard/internal/operationcycle/http"
	operationstore "github.com/qianlan33333-png/AI-CRM-standard/internal/operationcycle/store"
	orderport "github.com/qianlan33333-png/AI-CRM-standard/internal/order/port"
	platformaudit "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/audit"
	platformjobqueue "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/jobqueue"
	platformoutbox "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/outbox"
	platformpostgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
	productapp "github.com/qianlan33333-png/AI-CRM-standard/internal/product/app"
	productport "github.com/qianlan33333-png/AI-CRM-standard/internal/product/port"
	productstore "github.com/qianlan33333-png/AI-CRM-standard/internal/product/store"
	surveyport "github.com/qianlan33333-png/AI-CRM-standard/internal/survey/port"
	"github.com/riverqueue/river"
)

type standardMessages struct {
	archiveport.CustomerMessageReader
	items  []archiveport.MessageItem
	scopes []int64
}

func (s *standardMessages) CustomerMessages(_ context.Context, q archiveport.CustomerQuery) (out archiveport.CustomerPage, e error) {
	s.scopes = append(s.scopes, q.StaffUserID)
	for _, m := range s.items {
		if q.StaffUserID != 7 {
			continue
		}
		if q.AfterID > 0 && !m.OccurredAt.Before(q.AfterAt) {
			continue
		}
		out.Items = append(out.Items, m)
		if len(out.Items) == q.Limit {
			break
		}
	}
	return
}

type standardSurveys struct {
	surveyport.CustomerHistoryReader
}

func (standardSurveys) CustomerHistoryWindow(_ context.Context, q surveyport.CustomerHistoryQuery) (out surveyport.CustomerHistoryWindow, e error) {
	for i := 25; i > 0; i-- {
		id := surveyport.ID(i)
		at := time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC)
		if q.AfterID > 0 && !at.Before(q.AfterAt) {
			continue
		}
		out.Items = append(out.Items, surveyport.Submission{ID: id, SubmittedAt: at, QuestionnaireTitle: fmt.Sprintf("历史问卷%d", i)})
		if len(out.Items) == int(q.Limit) {
			break
		}
	}
	return
}

type standardProfiles struct {
	customerport.SidebarProfileService
}

func (standardProfiles) ReadSidebarProfile(_ context.Context, id customerdomain.CustomerID) (customerport.SidebarProfile, error) {
	return customerport.SidebarProfile{CustomerID: id, DisplayName: "合成客户", ProfileVersion: 1, NeedsBlockersFollowup: "人工确认：暂不推销", UpdatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, nil
}

type standardTags struct{ customerport.CustomerTagReader }

func (standardTags) CustomerTags(context.Context, customerdomain.CustomerID) (customerport.TagPage, error) {
	return customerport.TagPage{}, nil
}

type standardOrders struct {
	orderport.Query
	fail bool
}

func (s *standardOrders) List(context.Context, orderport.ListQuery) (orderport.Page, error) {
	if s.fail {
		return orderport.Page{}, errors.New("unavailable")
	}
	return orderport.Page{}, nil
}

type standardProducts struct {
	productport.ProductOptionReader
	price   int64
	enabled bool
}

func (s *standardProducts) ListProductOptions(context.Context, productport.ProductOptionQuery) (productport.ProductOptionPage, error) {
	return productport.ProductOptionPage{Total: 1, Items: []productport.ProductOption{{ID: 1, Code: "current", Name: "当前商品", Currency: "CNY", PriceMinor: s.price, Enabled: s.enabled, Version: s.price}}}, nil
}

type standardNotes struct {
	productport.OperatingNotesService
}

func (standardNotes) ReadOperatingNotes(context.Context, productport.ID) (productport.OperatingNotes, error) {
	return productport.OperatingNotes{Notes: "不得承诺收益", Version: 1}, nil
}

type standardModelPolicy struct{}

type standardOperationSecurity struct{}

func (standardOperationSecurity) Authenticate(context.Context, *http.Request) (accessdomain.Principal, error) {
	return accessdomain.Principal{Kind: accessdomain.KindAdmin, InternalID: 9, Roles: []accessdomain.Role{accessdomain.RoleAdmin}}, nil
}
func (s standardOperationSecurity) AuthorizeCSRF(ctx context.Context, r *http.Request) (accessdomain.Principal, error) {
	return s.Authenticate(ctx, r)
}

func (standardModelPolicy) GenerationModelPolicy(context.Context) (automationport.GenerationModelPolicy, error) {
	return automationport.GenerationModelPolicy{Mode: "enabled", Endpoint: "https://fixture.example.invalid/chat/completions", Model: "fixture", Temperature: 0.4}, nil
}

func TestStandardPostgreSQLHistoryScopeReceiptsAndProductNotes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	url, clean := adminAccessCompositionDatabase(t, ctx)
	defer clean()
	native, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer native.Close()
	wrapped, e := platformpostgres.Wrap(native, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	uow, e := platformpostgres.NewUnitOfWork(wrapped)
	if e != nil {
		t.Fatal(e)
	}
	var id int64
	if e = native.QueryRow(ctx, `INSERT INTO customers DEFAULT VALUES RETURNING id`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	contexts := &customerapp.ContextService{UOW: uow, Store: customerstore.NewPostgreSQL()}
	workers := river.NewWorkers()
	river.AddWorker(workers, externaleffects.NewWorker(nil, nil))
	client, e := platformjobqueue.NewInsertClient(native, workers)
	if e != nil {
		t.Fatal(e)
	}
	effects, e := externaleffects.NewRepository(native, client)
	if e != nil {
		t.Fatal(e)
	}
	repo, e := automationstore.NewPostgreSQL(native, uow)
	if e != nil {
		t.Fatal(e)
	}
	aux := &automationapp.AuxiliaryRuntime{UOW: uow, Store: repo, Effects: effects, Policy: standardModelPolicy{}, Contexts: contexts}
	messages := &standardMessages{}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 250; i > 0; i-- {
		messages.items = append(messages.items, archiveport.MessageItem{ID: int64(i), OccurredAt: at.Add(time.Duration(i) * time.Minute), MessageType: "text", ContentText: fmt.Sprintf("历史%d %s", i, strings.Repeat("资料", 50))})
	}
	products := &standardProducts{price: 9900, enabled: true}
	orders := &standardOrders{}
	s := &standardContext{base: dynamicGenerationContextAdapter{messages: messages, questionnaires: standardSurveys{}, profiles: standardProfiles{}, tags: standardTags{}}, contexts: contexts, orders: orders, products: products, notes: standardNotes{}, aux: aux, modelEnabled: true}
	key := customerport.ContextScope{CustomerID: customerdomain.CustomerID(id), StaffID: 7}
	prepared, e := s.prepare(ctx, key)
	if e != nil {
		t.Fatal(e)
	}
	if prepared.TextCount != 276 || len(prepared.Chunks) < 3 {
		t.Fatalf("history coverage=%d chunks=%d", prepared.TextCount, len(prepared.Chunks))
	}
	var jobs int
	if e = native.QueryRow(ctx, `SELECT count(*) FROM river_job`).Scan(&jobs); e != nil {
		t.Fatal(e)
	}
	if _, e = s.prepare(ctx, key); e != nil {
		t.Fatal(e)
	}
	var replayJobs int
	_ = native.QueryRow(ctx, `SELECT count(*) FROM river_job`).Scan(&replayJobs)
	if jobs != replayJobs {
		t.Fatal("repeated prepare duplicated model jobs")
	}
	var lastAux automationport.AuxiliaryGeneration
	for _, chunk := range prepared.Chunks {
		raw, _ := json.Marshal(chunk.Evidence)
		v, e := aux.RequestAuxiliary(ctx, automationport.AuxiliaryCommand{Purpose: "context", Scope: key, SourceVersion: chunk.Hash, IdempotencyKey: chunk.Hash, RolePrompt: contextExtractionRole, TaskPrompt: "整理这一段有来源的客户文字资料，只返回指定 JSON。保留时间和不确定性；摘要不得声称覆盖输入之外的内容。", Context: automationport.GenerationContext{CustomerContext: string(raw)}})
		if e != nil {
			t.Fatal(e)
		}
		a := customerport.ContextAnalysis{Summary: "来源可回查的历史摘要", SummaryEvidenceRefs: []string{chunk.Evidence[0].Ref}, Uncertainties: []customerport.ContextStatement{{Text: "历史资料仅供参考，当前需要核对", EvidenceRefs: []string{chunk.Evidence[0].Ref}}}}
		body, _ := json.Marshal(a)
		e = uow.Within(ctx, func(tx context.Context) error {
			_, e := aux.CompleteAuxiliary(tx, automationport.GenerationCompletion{EffectID: v.EffectID, State: effectport.StateExecuted, Artifact: effectport.ResultArtifact{Payload: body}, CompletedAt: time.Now().UTC()})
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		lastAux = v
	}
	current, found, e := contexts.ReadCustomerContext(ctx, key)
	if e != nil || !found || current.AnalysisSourceVersion != current.SourceVersion {
		t.Fatalf("completion not persisted: %v", e)
	}
	other, found, e := contexts.ReadCustomerContext(ctx, customerport.ContextScope{CustomerID: key.CustomerID, StaffID: 8})
	if e != nil || found || other.Analysis != nil {
		t.Fatal("cross staff cache leak")
	}
	if _, e = aux.ReadAuxiliary(ctx, lastAux.ID, customerport.ContextScope{CustomerID: key.CustomerID, StaffID: 8}); e == nil {
		t.Fatal("cross staff auxiliary leak")
	}
	latest := archiveport.MessageItem{ID: 251, OccurredAt: at.Add(251 * time.Minute), MessageType: "text", ContentText: "现在明确暂停，别推销"}
	messages.items = append([]archiveport.MessageItem{latest}, messages.items...)
	next, e := s.prepare(ctx, key)
	if e != nil {
		t.Fatal(e)
	}
	if next.SourceVersion == current.SourceVersion || len(next.ChunkAnalyses) < 1 {
		t.Fatal("incremental chunk reuse missing")
	}
	frozen, e := s.freezeCurrent(ctx, key, next)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(frozen.CustomerContext, "现在明确暂停") || !json.Valid([]byte(frozen.CustomerContext)) {
		t.Fatal("latest evidence omitted or malformed")
	}
	first := frozen.ProductCatalog
	products.price = 19900
	frozen, e = s.freezeCurrent(ctx, key, next)
	if e != nil || first == frozen.ProductCatalog || !strings.Contains(frozen.ProductCatalog, "19900") {
		t.Fatal("current product price lost")
	}
	products.enabled = false
	retired, e := s.productFacts(ctx)
	if e != nil || strings.Contains(retired, "当前商品") {
		t.Fatal("retired product recommended")
	}
	orders.fail = true
	if _, e = s.freezeCurrent(ctx, key, next); e == nil {
		t.Fatal("unavailable orders presented as no purchase")
	}
	productRepo, e := productstore.NewPostgreSQL(native, uow)
	if e != nil {
		t.Fatal(e)
	}
	audit, _ := platformaudit.NewService(platformaudit.NewPostgreSQLStore())
	events, _ := productstore.NewTransactionalEventAppender(audit, platformoutbox.NewPostgreSQL())
	catalog := productapp.NewService(uow, productRepo, events)
	p, e := catalog.Create(ctx, productport.CreateCommand{ProductCode: "standard-notes-fixture", Name: "合成商品", PriceMinor: 100, Currency: "CNY", Images: []string{}, Actor: 1, IdempotencyKey: "standard-product-create", LegacyAdminProjection: productapp.DefaultLegacyAdminProjection()})
	if e != nil {
		t.Fatal(e)
	}
	command := productport.SaveOperatingNotes{ProductID: p.ID, Notes: "内部机密运营说明", Actor: 1, IdempotencyKey: "standard-notes-save", ExpectedVersion: 0}
	saved, e := catalog.SaveOperatingNotes(ctx, command)
	if e != nil {
		t.Fatal(e)
	}
	replayed, e := catalog.SaveOperatingNotes(ctx, command)
	if e != nil || saved.Version != replayed.Version {
		t.Fatal("notes replay changed version")
	}
	command.IdempotencyKey = "standard-notes-conflict"
	if _, e = catalog.SaveOperatingNotes(ctx, command); !errors.Is(e, productapp.ErrConflict) {
		t.Fatalf("stale overwrite=%v", e)
	}
	public, _ := json.Marshal(p)
	if strings.Contains(string(public), "内部机密") {
		t.Fatal("private notes leaked into public product")
	}
	lifecycle := productapp.NewLocalProductLifecycleService(uow, productRepo, events)
	if _, e = lifecycle.SetLocalProductEnabled(ctx, productport.SetLocalProductEnabledCommand{ID: p.ID, ExpectedVersion: p.Version, Enabled: true, Actor: 1, IdempotencyKey: "standard-enable-active"}); e != nil {
		t.Fatal(e)
	}
	options, e := catalog.ListProductOptions(ctx, productport.ProductOptionQuery{})
	if e != nil || len(options.Items) != 1 || !options.Items[0].Enabled {
		t.Fatalf("enabled Product projection missing: %+v %v", options, e)
	}
	journal := operationstore.NewEventJournal()
	cycles := operationapp.NewService(uow, operationstore.NewRepository(), journal, journal)
	handler, e := operationhttp.NewHandler(cycles, standardOperationSecurity{}, "")
	if e != nil {
		t.Fatal(e)
	}
	body := `{"strategy_key":"standard-test-cycle","title":"合成定期策略","definition":{"schedule":"每天","indicator_color":"#3370ff","primary_action":"start_review","stages":[{"key":"review","label":"人工审核","color":"#3370ff","state":"pending"}],"native_generation":{"package_id":1,"interval_hours":24,"actor_id":888}}}`
	request := httptest.NewRequest(http.MethodPost, "/api/admin/operation-cycles/strategies", strings.NewReader(body))
	request.Header.Set("Idempotency-Key", "standard-native-http-create")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 201 {
		t.Fatalf("native strategy HTTP=%d %s", response.Code, response.Body.String())
	}
	var actor int64
	if e = native.QueryRow(ctx, `SELECT (definition->'native_generation'->>'actor_id')::bigint FROM operation_cycle_strategies WHERE strategy_key='standard-test-cycle'`).Scan(&actor); e != nil || actor != 9 {
		t.Fatalf("client forged scheduler actor: %d %v", actor, e)
	}
	t.Logf("full history=%d; model chunks=%d; repeated prepare reused %d River jobs; staff isolation, current prices, missing order source, notes CAS verified", prepared.TextCount, len(prepared.Chunks), jobs)
}
