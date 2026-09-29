package aiusage

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// Recorder writes synchronously with a bounded independent context so a client
// disconnect cannot discard already billed usage. Failure never fails AI output.
func Recorder(db *sql.DB) func(Record) {
	return func(r Record) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var input, cached, output any
		if r.Known {
			input = r.Input
			cached = r.Cached
			output = r.Output
		}
		var pi, pc, po, cost any
		if p, ok := PriceFor(r); ok {
			pi = p.Input
			pc = p.Cached
			po = p.Output
			if r.Known {
				cost = p.Cost(r)
			}
		}
		_, err := db.ExecContext(ctx, `INSERT INTO ai_token_usage(provider,model,user_id,input_tokens,cached_tokens,output_tokens,input_price,cached_price,output_price,cost_usd) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.Provider, r.Model, r.Owner, input, cached, output, pi, pc, po, cost)
		if err != nil {
			log.Printf("AI token usage persistence failed: %v", err)
		}
	}
}

type ModelCost struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Calls    int64    `json:"calls"`
	Missing  int64    `json:"missing_usage"`
	Unpriced int64    `json:"unpriced_calls"`
	Input    int64    `json:"input_tokens"`
	Cached   int64    `json:"cached_tokens"`
	Output   int64    `json:"output_tokens"`
	Cost     *float64 `json:"cost_usd"`
}
type Rate struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Price
}
type Summary struct {
	Currency     string      `json:"currency"`
	Since        time.Time   `json:"collection_since"`
	Rates        []Rate      `json:"rates"`
	Models       []ModelCost `json:"by_model"`
	Today        *float64    `json:"estimated_today"`
	TodayMissing int64       `json:"today_missing"`
}

func Snapshot(ctx context.Context, db *sql.DB, start, end time.Time) (Summary, error) {
	s := Summary{Currency: "USD", Models: []ModelCost{}, Rates: []Rate{{Provider: "api.z.ai", Model: "glm-5.3-flash", Price: Price{.15, .03, .50}}, {Provider: "api.z.ai", Model: "glm-5.3", Price: Price{1.4, .26, 4.4}}}}
	if err := db.QueryRowContext(ctx, `SELECT started_at FROM ai_token_collection WHERE id`).Scan(&s.Since); err != nil {
		return s, err
	}
	if err := db.QueryRowContext(ctx, `SELECT sum(cost_usd),count(*) FILTER(WHERE cost_usd IS NULL) FROM ai_token_usage WHERE at >= date_trunc('day',$1::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AND at <= $1`, end).Scan(&s.Today, &s.TodayMissing); err != nil {
		return s, err
	}
	rows, err := db.QueryContext(ctx, `SELECT provider,model,count(*),count(*) FILTER(WHERE input_tokens IS NULL),count(*) FILTER(WHERE input_price IS NULL),coalesce(sum(input_tokens),0),coalesce(sum(cached_tokens),0),coalesce(sum(output_tokens),0),sum(cost_usd) FROM ai_token_usage WHERE at >= $1 AND at <= $2 GROUP BY provider,model ORDER BY provider,model`, start, end)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var m ModelCost
		if err = rows.Scan(&m.Provider, &m.Model, &m.Calls, &m.Missing, &m.Unpriced, &m.Input, &m.Cached, &m.Output, &m.Cost); err != nil {
			return s, err
		}
		s.Models = append(s.Models, m)
	}
	return s, rows.Err()
}
