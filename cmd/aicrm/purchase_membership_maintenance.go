package main

import (
	"context"
	"encoding/json"
	"flag"
	config "github.com/qianlan33333-png/AI-CRM-standard/internal/platform/config"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// This operation is an OS-root authenticated, one-shot workbench command.
// It never starts HTTP, workers, payments, fulfilment, scoring or commissions.
func runPurchaseMembershipMaintenance(args []string, out io.Writer) int {
	if os.Geteuid() != 0 {
		json.NewEncoder(out).Encode(map[string]any{"error": "os_root_required"})
		return 2
	}
	flags := flag.NewFlagSet("purchase-memberships", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	campaign := flags.Int64("campaign-id", 0, "")
	products := flags.String("compatible-product-ids", "", "")
	apply := flags.Bool("apply", false, "")
	dryRun := flags.Bool("dry-run", false, "")
	reference := flags.String("authorization-reference", "", "")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *campaign < 1 || *reference == "" || (*apply && *dryRun) {
		json.NewEncoder(out).Encode(map[string]any{"error": "invalid_arguments"})
		return 2
	}
	var ids []int64 // Omitted preserves the campaign's current configuration.
	flags.Visit(func(v *flag.Flag) {
		if v.Name == "compatible-product-ids" {
			ids = []int64{}
		}
	})
	if *products != "" {
		for _, part := range strings.Split(*products, ",") {
			id, e := strconv.ParseInt(part, 10, 64)
			if e != nil || id < 1 {
				json.NewEncoder(out).Encode(map[string]any{"error": "invalid_product"})
				return 2
			}
			ids = append(ids, id)
		}
	}
	cfg, e := config.Load()
	if e != nil {
		json.NewEncoder(out).Encode(map[string]any{"error": "runtime_config_unavailable"})
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	app, e := compose(context.WithValue(ctx, purchaseMembershipMaintenanceContextKey{}, true), cfg)
	if e != nil {
		json.NewEncoder(out).Encode(map[string]any{"error": "composition_unavailable"})
		return 1
	}
	defer app.Close()
	if app.referralMembership == nil || app.referralMaintenance == nil {
		json.NewEncoder(out).Encode(map[string]any{"error": "referral_unavailable"})
		return 1
	}
	c, e := app.referralMaintenance.ConfigurePurchaseCompatibilityForOperator(ctx, *campaign, ids, *reference, *apply)
	if e != nil {
		json.NewEncoder(out).Encode(map[string]any{"error": "configuration_rejected"})
		return 1
	}
	// Preview must evaluate the proposed scope without persisting configuration.
	result, e := app.referralMembership.ReconcilePurchaseMembersWithProducts(ctx, *campaign, c.CompatibleProductIDs, *apply)
	if e != nil {
		json.NewEncoder(out).Encode(map[string]any{"error": "membership_reconciliation_incomplete", "result": result, "apply": *apply})
		return 1
	}
	json.NewEncoder(out).Encode(map[string]any{"campaign_id": c.ID, "campaign_version": c.Version, "compatible_product_ids": c.CompatibleProductIDs, "apply": *apply, "result": result, "release_sha": cfg.ReleaseSHA})
	return 0
}

type purchaseMembershipMaintenanceContextKey struct{}

func purchaseMembershipMaintenanceComposition(ctx context.Context) bool {
	v, _ := ctx.Value(purchaseMembershipMaintenanceContextKey{}).(bool)
	return v
}
