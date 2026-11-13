package opsmonitor

import (
	"context"
	"database/sql"
	"time"
)

type TaskAdmission struct {
	FirstRecordedAt time.Time              `json:"first_recorded_at"`
	ByTask          []TaskAdmissionOutcome `json:"by_task"`
}

type TaskAdmissionOutcome struct {
	TaskType string `json:"task_type"`
	Personal int64  `json:"personal"`
	Public   int64  `json:"public"`
	System   int64  `json:"system"`
	Denied   int64  `json:"denied"`
}

func (s *Service) taskAdmission(ctx context.Context, r *Response) error {
	var first sql.NullTime
	if err := s.db.QueryRowContext(ctx, `SELECT min(at) FROM operations_events WHERE kind='task_admission'`).Scan(&first); err != nil {
		return err
	}
	if !first.Valid {
		return nil
	}
	summary := &TaskAdmission{FirstRecordedAt: first.Time, ByTask: []TaskAdmissionOutcome{}}
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_type,
			coalesce(sum(count) FILTER (WHERE reason='personal'),0),
			coalesce(sum(count) FILTER (WHERE reason='public'),0),
			coalesce(sum(count) FILTER (WHERE reason='system'),0),
			coalesce(sum(count) FILTER (WHERE reason='denied'),0)
		FROM operations_events
		WHERE kind='task_admission' AND at >= $1 AND at <= $2
		GROUP BY task_type ORDER BY task_type`, r.WindowStart, r.WindowEnd)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row TaskAdmissionOutcome
		if err := rows.Scan(&row.TaskType, &row.Personal, &row.Public, &row.System, &row.Denied); err != nil {
			return err
		}
		summary.ByTask = append(summary.ByTask, row)
	}
	r.TaskAdmission = summary
	if r.Status == "no_data" && len(summary.ByTask) > 0 {
		r.Status = "available"
		if r.WindowStart.Before(r.CollectionAvailableSince) || r.CollectionDropped > 0 {
			r.Status = "partial"
		}
	}
	return rows.Err()
}
