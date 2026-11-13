package opsmonitor

import (
	"context"
	"database/sql"
	"time"
)

type Traffic struct {
	CollectionSince time.Time       `json:"collection_since"`
	Partial         bool            `json:"partial"`
	PageViews       int64           `json:"page_views"`
	Visitors        int64           `json:"visitors"`
	TimeSeries      []TrafficPoint  `json:"time_series"`
	BySource        []TrafficSource `json:"by_source"`
}

type TrafficPoint struct {
	At        time.Time `json:"at"`
	PageViews int64     `json:"page_views"`
	Visitors  int64     `json:"visitors"`
}

type TrafficSource struct {
	Source    string `json:"source"`
	PageViews int64  `json:"page_views"`
}

func trafficSnapshot(ctx context.Context, db *sql.DB, start, end time.Time, bucketSeconds int) (*Traffic, error) {
	result := &Traffic{TimeSeries: []TrafficPoint{}, BySource: []TrafficSource{}}
	if err := db.QueryRowContext(ctx, `SELECT started_at FROM site_traffic_collection WHERE id`).Scan(&result.CollectionSince); err != nil {
		return nil, err
	}
	result.Partial = start.Before(result.CollectionSince)
	// Grouping sets calculate the whole-window UV independently of the per-bucket
	// counts, from one statement snapshot while page views continue to arrive.
	rows, err := db.QueryContext(ctx, `
		WITH visits AS (
			SELECT to_timestamp(floor(extract(epoch FROM at)/$3)*$3) AS bucket, source, visitor_id
			FROM site_page_views WHERE at >= $1 AND at < $2
		)
		SELECT bucket, source, count(*), count(DISTINCT visitor_id)
		FROM visits GROUP BY GROUPING SETS ((), (bucket), (source))
		ORDER BY bucket NULLS FIRST, source NULLS FIRST`, start, end, bucketSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var bucket sql.NullTime
		var source sql.NullString
		var views, visitors int64
		if err := rows.Scan(&bucket, &source, &views, &visitors); err != nil {
			return nil, err
		}
		switch {
		case bucket.Valid:
			result.TimeSeries = append(result.TimeSeries, TrafficPoint{bucket.Time, views, visitors})
		case source.Valid:
			result.BySource = append(result.BySource, TrafficSource{source.String, views})
		default:
			result.PageViews, result.Visitors = views, visitors
		}
	}
	return result, rows.Err()
}
