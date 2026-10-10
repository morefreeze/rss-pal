package opsmonitor

import (
	"context"
	"fmt"
	"time"
)

func (s *Service) quotaExhaustions(ctx context.Context, r *Response) error {
	r.QuotaExhaustions = []QuotaExhaustion{}
	rows, err := s.db.QueryContext(ctx, `SELECT d.bucket,d.owner_id,CASE WHEN d.owner_id=0 THEN 'global_daily' ELSE 'user_daily' END,d.used,COALESCE(u.is_admin,false) FROM task_budget_daily d LEFT JOIN users u ON u.id=d.owner_id WHERE d.day=$1::date ORDER BY d.bucket,d.owner_id`, r.GeneratedAt.UTC().Format("2006-01-02"))
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var q QuotaExhaustion
		var admin bool
		if err := rows.Scan(&q.TaskType, &q.UserID, &q.Reason, &q.Used, &admin); err != nil {
			return err
		}
		p := s.policies[q.TaskType]
		q.Limit = p.Daily
		if admin && p.AdminDaily > 0 {
			q.Limit = p.AdminDaily
		}
		if q.Reason == "global_daily" {
			q.Limit = p.GlobalDaily
		}
		key := fmt.Sprintf("%s/%d/%s", q.TaskType, q.UserID, q.Reason)
		if q.Limit <= 0 || q.Used < q.Limit || seen[key] {
			continue
		}
		seen[key] = true
		q.RetryAt = r.GeneratedAt.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		r.QuotaExhaustions = append(r.QuotaExhaustions, q)
	}
	return rows.Err()
}
