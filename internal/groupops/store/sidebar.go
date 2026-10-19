package store

import (
	"context"

	p "github.com/qianlan33333-png/AI-CRM-standard/internal/groupops/port"
)

func (r *Repository) ReadSidebarPlans(ctx context.Context, owner int64, chat, query string, limit, offset int32) (out p.SidebarPlans, err error) {
	out = p.SidebarPlans{Bindings: []p.SidebarPlan{}, Candidates: []p.SidebarPlan{}, Limit: limit, Offset: offset}
	tx, err := transaction(ctx)
	if err != nil {
		return out, err
	}
	// Exactly the same sole-owner requirement as the binding command. A group
	// can occur in several plans; uniqueness is only (plan_id, asset_reference).
	const scope = ` FROM group_ops_plans p WHERE p.status<>'archived'
 AND EXISTS(SELECT 1 FROM group_ops_plan_members m WHERE m.plan_id=p.id AND m.staff_id=$1)
 AND (SELECT count(*) FROM group_ops_plan_members m WHERE m.plan_id=p.id)=1 `
	const bound = `EXISTS(SELECT 1 FROM group_ops_plan_group_assets a WHERE a.plan_id=p.id AND a.asset_reference=$2)`
	rows, err := tx.Query(ctx, `SELECT p.id,p.name,p.status,p.revision`+scope+` AND `+bound+` ORDER BY p.id`, owner, chat)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var plan p.SidebarPlan
		if err = rows.Scan(&plan.ID, &plan.Name, &plan.Status, &plan.Revision); err != nil {
			rows.Close()
			return out, err
		}
		out.Bindings = append(out.Bindings, plan)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	where := scope + ` AND p.status IN ('draft','paused') AND NOT ` + bound + ` AND ($3='' OR strpos(lower(p.name),lower($3))>0)`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, owner, chat, query).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT p.id,p.name,p.status,p.revision`+where+` ORDER BY p.updated_at DESC,p.id DESC LIMIT $4 OFFSET $5`, owner, chat, query, limit, offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var plan p.SidebarPlan
		if err = rows.Scan(&plan.ID, &plan.Name, &plan.Status, &plan.Revision); err != nil {
			return out, err
		}
		out.Candidates = append(out.Candidates, plan)
	}
	return out, rows.Err()
}
