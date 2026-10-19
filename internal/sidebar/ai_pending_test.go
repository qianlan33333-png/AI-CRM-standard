package sidebar

import (
	"context"
	customerdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	"strings"
	"testing"
)

func TestAISuggestionPendingPollKeepsReceiptKeyAndRejectsChangedSnapshot(t *testing.T) {
	snapshot := aiFixture()
	keys := []string{}
	calls := 0
	h := aiHandler(t, aiReaderFunc(func(context.Context, Principal, customerdomain.CustomerID) (AIWarehouseSnapshot, error) {
		return snapshot, nil
	}), aiGeneratorFunc(func(_ context.Context, in AISuggestionInput) (string, error) {
		keys = append(keys, in.IdempotencyKey)
		calls++
		if calls == 1 {
			return "", &AIGenerationPending{ID: 7, State: "queued"}
		}
		return "人工审核后使用的建议", nil
	}))
	token := aiToken(t, h)
	queued := aiCall(h, "POST", "/api/sidebar/v2/ai-operations/suggestions", aiBody(token), true)
	if queued.Code != 202 || !strings.Contains(queued.Body.String(), `"request_id":7`) {
		t.Fatal(queued.Code, queued.Body.String())
	}
	result := aiCall(h, "POST", "/api/sidebar/v2/ai-operations/suggestions", aiBody(token), true)
	if result.Code != 200 || keys[0] != keys[1] {
		t.Fatal("poll changed acceptance key")
	}
	snapshot.SourceVersion = "product-price-changed"
	stale := aiCall(h, "POST", "/api/sidebar/v2/ai-operations/suggestions", aiBody(token), true)
	if stale.Code != 409 || calls != 2 {
		t.Fatal("stale facts caused another model acceptance")
	}
}
