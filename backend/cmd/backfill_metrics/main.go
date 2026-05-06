package main

import (
	"database/sql"
	"log"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/rss"
)

// Recomputes word_count / reading_minutes for every article whose word_count is 0
// and content is non-empty. Safe to re-run (idempotent on already-set rows).
func main() {
	cfg := config.Load()
	db, err := repository.NewDB(&cfg.Database)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, content FROM articles WHERE word_count = 0 AND content IS NOT NULL AND content != ''`)
	if err != nil {
		log.Fatalf("query: %v", err)
	}
	defer rows.Close()

	updated := 0
	for rows.Next() {
		var id int
		var content sql.NullString
		if err := rows.Scan(&id, &content); err != nil {
			log.Printf("scan: %v", err)
			continue
		}
		wc, rm := rss.ComputeMetrics(content.String)
		if _, err := db.Exec(`UPDATE articles SET word_count = $1, reading_minutes = $2 WHERE id = $3`, wc, rm, id); err != nil {
			log.Printf("update id=%d: %v", id, err)
			continue
		}
		updated++
		if updated%100 == 0 {
			log.Printf("backfilled %d articles…", updated)
		}
	}
	log.Printf("done; backfilled %d articles", updated)
}
