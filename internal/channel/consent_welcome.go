package channel

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	channelport "github.com/qianlan33333-png/AI-CRM-standard/internal/channel/port"
	customerdomain "github.com/qianlan33333-png/AI-CRM-standard/internal/customer/domain"
	effectport "github.com/qianlan33333-png/AI-CRM-standard/internal/externaleffects/port"
)

func (store *EntrantActionStore) acceptConsentWelcome(ctx context.Context, tx pgx.Tx, command channelport.CallbackWelcomeCommand) error {
	// All consent callbacks for an original add share one business source and
	// lock. Callback replay and concurrent distinct consent events therefore
	// cannot create multiple welcome effects for that add.
	source := effectport.Hash("channel.welcome.consent.source.v1", command.PriorCallbackID)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(source)); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_welcome_intents WHERE source_ref_digest=$1)`, source).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var config welcomeConfig
	var customerID customerdomain.CustomerID
	var snapshot json.RawMessage
	var state string
	err := tx.QueryRow(ctx, `SELECT i.channel_id,i.config_version,COALESCE(i.customer_id,0),i.welcome_material_snapshot,i.state,v.welcome_message
  FROM channel_welcome_intents i JOIN channel_config_versions v ON (v.channel_id,v.config_version)=(i.channel_id,i.config_version)
  WHERE i.callback_id=$1 FOR UPDATE OF i`, command.PriorCallbackID).Scan(&config.ChannelID, &config.ConfigVersion, &customerID, &snapshot, &state, &config.WelcomeMessage)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// A final failure is known not to have sent the welcome. Never replace an
	// executed, unknown or in-flight effect with a fresh credential. Numeric
	// Provider error whitelists are unnecessary: the existing outcome contract
	// already distinguishes definite failure from an ambiguous write.
	if state != "final_failed" || len(snapshot) == 0 {
		return nil
	}
	status, err := lockedChannelStatus(ctx, tx, config.ChannelID)
	if err != nil {
		return err
	}
	if inactiveChannelStatus(status) {
		return nil
	}
	return store.queueCallbackWelcome(ctx, tx, command, config, snapshot,
		string(effectport.Hash("channel.welcome.consent.material.v1", string(snapshot))), source, customerID)
}
