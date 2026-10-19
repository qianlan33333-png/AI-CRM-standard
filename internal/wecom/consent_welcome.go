package wecom

import (
	"context"
	"encoding/json"

	platformpostgres "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/postgres"
)

// ConsentWelcomeCorrelator restores protocol context that WeCom omits from a
// msg_audit_approved event. It does not resolve or provision a customer.
type ConsentWelcomeCorrelator interface {
	PriorWelcomeCallback(context.Context, CallbackEvent) (string, error)
}

type PostgreSQLConsentWelcomeCorrelator struct{}

func (PostgreSQLConsentWelcomeCorrelator) PriorWelcomeCallback(ctx context.Context, event CallbackEvent) (string, error) {
	if event.ChangeType != ChangeMsgAuditApproved || event.CorpID == "" || event.UserID == "" || event.ExternalUserID == "" || event.CreateTime <= 0 {
		return "", ErrMalformedXML
	}
	tx, err := platformpostgres.RequireTransaction(ctx)
	if err != nil {
		return "", err
	}
	// These are WeCom's own normalized authenticated events stored in the
	// shared platform Inbox. No Channel/Identity/Customer table is accessed.
	// Read the latest relationship event, not the latest attributed channel:
	// a delete or a subsequent add without State must supersede an older add.
	rows, err := tx.Query(ctx, `WITH lifecycle AS (
		SELECT id,idempotency_key,payload,(payload->>'create_time')::bigint AS event_time FROM webhook_inbox
		WHERE provider=$1 AND payload->>'corp_id'=$2 AND payload->>'userid'=$3 AND payload->>'external_userid'=$4
		AND payload->>'change_type' IN ('add_external_contact','add_half_external_contact','del_follow_user','del_external_contact')
	)
	SELECT idempotency_key,payload FROM lifecycle
	WHERE event_time=(SELECT max(event_time) FROM lifecycle) ORDER BY id DESC`, callbackProvider, event.CorpID, event.UserID, event.ExternalUserID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var key string
	var previous CallbackEvent
	for rows.Next() {
		var candidate string
		var raw []byte
		if err = rows.Scan(&candidate, &raw); err != nil {
			return "", err
		}
		var current CallbackEvent
		if err = json.Unmarshal(raw, &current); err != nil {
			return "", ErrMalformedXML
		}
		if key != "" {
			// Equal-time, distinct lifecycle facts have no trustworthy ordering.
			if !current.IsEntrant() || !previous.IsEntrant() || !current.StatePresent || current.StateDigest != previous.StateDigest {
				return "", nil
			}
			continue
		}
		key, previous = candidate, current
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if key == "" || !previous.IsEntrant() || previous.CreateTime > event.CreateTime || !previous.StatePresent {
		return "", nil
	}
	return key, nil
}
