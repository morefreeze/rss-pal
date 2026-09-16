package opsmonitor

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"sort"
	"time"
)

type Service struct {
	db       *sql.DB
	cfg      Config
	policies taskbudget.Policies
}

func NewService(db *sql.DB, c Config, p taskbudget.Policies) *Service { return &Service{db, c, p} }
func (s *Service) Snapshot(parent context.Context, hours int, before int64, limit int) (Response, error) {
	r := Response{Hours: hours, RetentionDays: 30, TimeSeries: []Point{}, Groups: []Group{}, Queues: []Queue{}, Alerts: []Alert{}, RecentEvents: []Event{}, CaptchaExpiresAt: s.cfg.CaptchaExpiresAt}
	if hours != 1 && hours != 24 && hours != 168 {
		return r, fmt.Errorf("invalid hours")
	}
	if limit < 1 || limit > 50 {
		limit = 50
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	if err := s.db.QueryRowContext(ctx, `SELECT clock_timestamp(),greatest(started_at,now()-interval '30 days'),dropped FROM operations_collection WHERE id`).Scan(&r.GeneratedAt, &r.CollectionAvailableSince, &r.CollectionDropped); err != nil {
		return r, err
	}
	r.WindowEnd = r.GeneratedAt
	r.WindowStart = r.WindowEnd.Add(-time.Duration(hours) * time.Hour)
	r.Status = "available"
	if r.CollectionAvailableSince.After(r.WindowStart) || r.CollectionDropped > 0 {
		r.Status = "partial"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind,reason,task_type,user_id,sum(count) FROM operations_events WHERE at >= $1 AND at <= $2 GROUP BY kind,reason,task_type,user_id ORDER BY sum(count) DESC LIMIT 200`, r.WindowStart, r.WindowEnd)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var g Group
		if err = rows.Scan(&g.Kind, &g.Reason, &g.TaskType, &g.UserID, &g.Count); err != nil {
			rows.Close()
			return r, err
		}
		r.Groups = append(r.Groups, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	// Aggregate independently of bounded detail groups.
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(sum(count) FILTER(WHERE kind='registration'),0),COALESCE(sum(count) FILTER(WHERE kind='registration' AND reason='success'),0),COALESCE(sum(count) FILTER(WHERE kind='registration' AND reason='failed'),0),COALESCE(sum(count) FILTER(WHERE kind='captcha' AND reason='success'),0),COALESCE(sum(count) FILTER(WHERE kind='captcha' AND reason='rejected'),0),COALESCE(sum(count) FILTER(WHERE kind='captcha' AND reason='unavailable'),0),COALESCE(sum(count) FILTER(WHERE kind='limit'),0) FROM operations_events WHERE at >= $1 AND at <= $2`, r.WindowStart, r.WindowEnd).Scan(&r.Registration.Attempts, &r.Registration.Success, &r.Registration.Failed, &r.Captcha.Success, &r.Captcha.Rejected, &r.Captcha.Unavailable, &r.LimitTotal)
	if err != nil {
		return r, err
	}
	if len(r.Groups) == 0 && r.CollectionDropped == 0 {
		r.Status = "no_data"
	}
	interval := 3600
	if hours == 1 {
		interval = 300
	}
	rows, err = s.db.QueryContext(ctx, `SELECT to_timestamp(floor(extract(epoch from at)/$3)*$3),COALESCE(sum(count) FILTER(WHERE kind='registration' AND reason='success'),0),COALESCE(sum(count) FILTER(WHERE kind='registration' AND reason='failed'),0),COALESCE(sum(count) FILTER(WHERE kind='captcha' AND reason='rejected'),0),COALESCE(sum(count) FILTER(WHERE kind='captcha' AND reason='unavailable'),0),COALESCE(sum(count) FILTER(WHERE kind='limit'),0) FROM operations_events WHERE at >= $1 AND at <= $2 GROUP BY 1 ORDER BY 1`, r.WindowStart, r.WindowEnd, interval)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var p Point
		if err = rows.Scan(&p.At, &p.RegistrationSuccess, &p.RegistrationFailed, &p.CaptchaRejected, &p.CaptchaUnavailable, &p.LimitDenied); err != nil {
			rows.Close()
			return r, err
		}
		r.TimeSeries = append(r.TimeSeries, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id,at,kind,reason,task_type,user_id,count,retry_at FROM operations_events WHERE at >= $1 AND at <= $2 AND ($3::bigint=0 OR id<$3) ORDER BY id DESC LIMIT $4`, r.WindowStart, r.WindowEnd, before, limit+1)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.ID, &e.At, &e.Kind, &e.Reason, &e.TaskType, &e.UserID, &e.Count, &e.RetryAt); err != nil {
			rows.Close()
			return r, err
		}
		r.RecentEvents = append(r.RecentEvents, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	if len(r.RecentEvents) > limit {
		r.RecentEvents = r.RecentEvents[:limit]
		id := r.RecentEvents[limit-1].ID
		r.NextBeforeID = &id
	}
	if err = s.queues(ctx, &r); err != nil {
		return r, err
	}
	if err = s.cost(ctx, &r); err != nil {
		return r, err
	}
	var unavailable, total, limits int
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(sum(count) FILTER(WHERE kind='captcha' AND reason='unavailable'),0),COALESCE(sum(count) FILTER(WHERE kind='captcha'),0),COALESCE(sum(count) FILTER(WHERE kind='limit'),0) FROM operations_events WHERE at>=$1 AND at<=$2`, r.WindowEnd.Add(-15*time.Minute), r.WindowEnd).Scan(&unavailable, &total, &limits)
	if err != nil {
		return r, err
	}
	if unavailable >= s.cfg.CaptchaMinFailures && total > 0 && float64(unavailable)/float64(total) > s.cfg.CaptchaFailureRatio {
		r.Alerts = append(r.Alerts, Alert{"captcha_unavailable", "critical", fmt.Sprintf("最近15分钟验票服务故障 %d 次（次数阈值 %d），故障率超过 %.1f%%", unavailable, s.cfg.CaptchaMinFailures, s.cfg.CaptchaFailureRatio*100), float64(unavailable) / float64(total), s.cfg.CaptchaFailureRatio})
	}
	if limits >= s.cfg.LimitThreshold {
		r.Alerts = append(r.Alerts, Alert{"limit_denied", "warning", "最近15分钟限流达到阈值", float64(limits), float64(s.cfg.LimitThreshold)})
	}
	return r, nil
}
func (s *Service) queues(ctx context.Context, r *Response) error {
	q := Queue{Name: "summary", Status: "partial", SnapshotAt: r.GeneratedAt, Note: "可执行摘要：活跃订阅、无摘要、正文超过100字符、音视频已获取转录。无持久化执行/失败状态，数量不可用；等待时间按 fetched_at。"}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(greatest(0,extract(epoch from ($1::timestamptz-min(a.fetched_at)))),0) FROM articles a JOIN feeds f ON f.id=a.feed_id WHERE f.status='active' AND f.is_active AND (a.summary_brief IS NULL OR a.summary_brief='') AND length(a.content)>100 AND NOT(a.media_type IS NOT NULL AND (a.media_type LIKE 'video/%' OR a.media_type LIKE 'audio/%') AND a.transcript_fetched_at IS NULL)`, r.GeneratedAt).Scan(&q.Waiting, &q.OldestWaitSeconds); err != nil {
		return err
	}
	r.Queues = append(r.Queues, q)
	for _, table := range []string{"explore_fetch_queue", "explore_related_tasks"} {
		running, failed, expired := 0, 0, 0
		q = Queue{Name: table, Status: "available", SnapshotAt: r.GeneratedAt, Running: &running, Failed: &failed, Expired: &expired, Note: "等待仅计 pending、未绑定运行且 not_before 已到期；failed 为 invalid，expired 为已过期 leased；等待时间按创建时间。"}
		query := `SELECT count(*) FILTER(WHERE status='pending' AND run_id IS NULL AND not_before<=$1),count(*) FILTER(WHERE status='leased' AND lease_expires_at>$1),count(*) FILTER(WHERE status='invalid'),count(*) FILTER(WHERE status='leased' AND lease_expires_at<=$1),COALESCE(greatest(0,extract(epoch from ($1::timestamptz-min(created_at) FILTER(WHERE status='pending' AND run_id IS NULL AND not_before<=$1)))),0) FROM ` + table
		if err := s.db.QueryRowContext(ctx, query, r.GeneratedAt).Scan(&q.Waiting, &running, &failed, &expired, &q.OldestWaitSeconds); err != nil {
			return err
		}
		r.Queues = append(r.Queues, q)
	}
	for _, q := range r.Queues {
		if q.Waiting > 0 && q.OldestWaitSeconds > s.cfg.QueueWaitSeconds {
			r.Alerts = append(r.Alerts, Alert{"queue_" + q.Name, "warning", q.Name + " 可执行任务等待超过阈值", q.OldestWaitSeconds, s.cfg.QueueWaitSeconds})
		}
	}
	return nil
}
func (s *Service) cost(ctx context.Context, r *Response) error {
	r.Cost = Cost{UTCDay: r.GeneratedAt.UTC().Format("2006-01-02"), LedgerRetentionNote: "额度账本按 UTC 日聚合；窗口调用量包含与窗口相交的完整 UTC 日期，不能精确裁剪小时。账本由现有清理任务保留，最早可用日期见实际记录；次数为准入尝试，不是 token 或账单。", EstimateStatus: "not_configured", Currency: "CNY", ByTask: []TaskCost{}, ByUser: []UserCost{}, DailyAlertBudget: s.cfg.DailyCostBudget}
	var first sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT min(day)::text FROM task_budget_daily`).Scan(&first); err != nil {
		return err
	}
	if first.Valid {
		r.Cost.LedgerRetentionNote += " 最早保留 UTC 日期：" + first.String
	} else {
		r.Cost.LedgerRetentionNote += " 尚无账本记录。"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT bucket,COALESCE(sum(used) FILTER(WHERE day >= $1::date),0),COALESCE(sum(used) FILTER(WHERE day=$2::date),0) FROM task_budget_daily WHERE owner_id=0 AND day >= $1::date AND day <= $2::date GROUP BY bucket`, r.WindowStart.UTC().Format("2006-01-02"), r.Cost.UTCDay)
	if err != nil {
		return err
	}
	m := map[string]TaskCost{}
	for rows.Next() {
		var v TaskCost
		if err = rows.Scan(&v.TaskType, &v.Attempts, &v.TodayAttempts); err != nil {
			rows.Close()
			return err
		}
		m[v.TaskType] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for k := range s.policies {
		if _, ok := m[k]; !ok {
			m[k] = TaskCost{TaskType: k}
		}
	}
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sum := 0.
	allPriced := true
	hasPrice := false
	for _, k := range keys {
		v := m[k]
		v.GlobalDailyLimit = s.policies[k].GlobalDaily
		if v.GlobalDailyLimit > 0 {
			v.UsageRatio = float64(v.TodayAttempts) / float64(v.GlobalDailyLimit)
		}
		if p, ok := s.cfg.UnitPrices[k]; ok {
			v.UnitPrice = &p
			est := p * float64(v.Attempts)
			v.EstimatedCost = &est
			sum += p * float64(v.TodayAttempts)
			hasPrice = true
		} else if v.TodayAttempts > 0 {
			allPriced = false
		}
		r.Cost.ByTask = append(r.Cost.ByTask, v)
		if v.UsageRatio >= s.cfg.QuotaWarningRatio {
			severity := "warning"
			if v.UsageRatio >= 1 {
				severity = "critical"
			}
			r.Alerts = append(r.Alerts, Alert{"quota_" + k, severity, k + " 今日 UTC 全局额度达到阈值", v.UsageRatio, s.cfg.QuotaWarningRatio})
		}
	}
	if hasPrice {
		r.Cost.EstimateStatus = "configured"
		if !allPriced {
			r.Cost.EstimateStatus = "partial"
		}
		r.Cost.EstimatedToday = &sum
		if s.cfg.DailyCostBudget != nil && sum >= *s.cfg.DailyCostBudget {
			r.Alerts = append(r.Alerts, Alert{"estimated_cost", "warning", "今日已配置单价任务估算金额达到阈值（非账单）", sum, *s.cfg.DailyCostBudget})
		}
	}
	rows, err = s.db.QueryContext(ctx, `SELECT owner_id,bucket,sum(used) FROM task_budget_daily WHERE owner_id>0 AND day >= $1::date AND day <= $2::date GROUP BY owner_id,bucket ORDER BY sum(used) DESC,owner_id,bucket LIMIT 50`, r.WindowStart.UTC().Format("2006-01-02"), r.Cost.UTCDay)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v UserCost
		if err = rows.Scan(&v.UserID, &v.TaskType, &v.Attempts); err != nil {
			return err
		}
		r.Cost.ByUser = append(r.Cost.ByUser, v)
	}
	return rows.Err()
}
