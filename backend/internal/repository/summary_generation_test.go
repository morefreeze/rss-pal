package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/bytedance/rss-pal/internal/model"
	"testing"
	"time"
)

func TestSummaryGenerationAtomicWriteAndInvalidation(t *testing.T) {
	repo, db, _, id := seedLinkSetParent(t)
	if _, err := db.Exec(`UPDATE articles SET title='Title',content='body',summary_brief='old',summary_detailed='old' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	var legacy []byte
	if err := db.QueryRow(`SELECT summary_generation FROM articles WHERE id=$1`, id).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 0 {
		t.Fatal("legacy falsely labeled")
	}
	sum := sha256.Sum256([]byte("body"))
	g := &model.SummaryGeneration{SchemaVersion: 1, PolicyVersion: "test-policy", ContentHash: hex.EncodeToString(sum[:]), SourceTitle: "Title", GeneratedAt: time.Now().UTC()}
	token, err := repo.ClaimSummary(context.Background(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.CompleteSummary(id, token, "body", "new", "details", g); err != nil || !ok {
		t.Fatalf("save %v %v", ok, err)
	}
	var policy string
	if err := db.QueryRow(`SELECT summary_generation->>'policy_version' FROM articles WHERE id=$1`, id).Scan(&policy); err != nil || policy != "test-policy" {
		t.Fatalf("policy %s %v", policy, err)
	}
	if _, err := db.Exec(`UPDATE articles SET title='Changed title' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT summary_generation FROM articles WHERE id=$1`, id).Scan(&legacy); err != nil || len(legacy) != 0 {
		t.Fatalf("title change retained stale metadata %s %v", legacy, err)
	}
	token, err = repo.ClaimSummary(context.Background(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.CompleteSummary(id, token, "body", "stale title", "details", g); err != nil || ok {
		t.Fatalf("title CAS %v %v", ok, err)
	}
}

func TestSummaryGenerationClearsOnLegacyAndContentWrites(t *testing.T) {
	repo, db, _, id := seedLinkSetParent(t)
	for _, mutation := range []string{`UPDATE articles SET content='changed' WHERE id=$1`, `UPDATE articles SET summary_brief='changed' WHERE id=$1`, `UPDATE articles SET summary_detailed=NULL WHERE id=$1`} {
		if _, err := db.Exec(`UPDATE articles SET title='Title',content='body',summary_brief='brief',summary_detailed='details',summary_generation='{"policy_version":"old"}' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(mutation, id); err != nil {
			t.Fatal(err)
		}
		var metadata []byte
		if err := db.QueryRow(`SELECT summary_generation FROM articles WHERE id=$1`, id).Scan(&metadata); err != nil || len(metadata) > 0 {
			t.Fatalf("stale after %s: %s %v", mutation, metadata, err)
		}
	}
	if _, err := db.Exec(`UPDATE articles SET summary_brief='same',summary_detailed='same',summary_generation='{"policy_version":"old"}' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateSummary(id, "same", "same"); err != nil {
		t.Fatal(err)
	}
	var metadata []byte
	if err := db.QueryRow(`SELECT summary_generation FROM articles WHERE id=$1`, id).Scan(&metadata); err != nil || len(metadata) > 0 {
		t.Fatalf("legacy write retained provenance %s %v", metadata, err)
	}
}

func TestSummaryGenerationRejectsEditedTemplateAtCommit(t *testing.T) {
	repo, db, _, id := seedLinkSetParent(t)
	if _, err := db.Exec(`UPDATE articles SET title='Title',content='body' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	var templateID int
	var brief, detailed string
	if err := db.QueryRow(`SELECT id,brief_prompt,detailed_prompt FROM summary_templates WHERE is_system LIMIT 1`).Scan(&templateID, &brief, &detailed); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("body"))
	g := &model.SummaryGeneration{SchemaVersion: 1, ContentHash: hex.EncodeToString(sum[:]), SourceTitle: "Title", GeneratedAt: time.Now().UTC()}
	g.BindTemplate(templateID, brief, detailed)
	token, err := repo.ClaimSummary(context.Background(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE summary_templates SET detailed_prompt=detailed_prompt || ' edited' WHERE id=$1`, templateID); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.CompleteSummary(id, token, "body", "brief", "details", g); err != nil || ok {
		t.Fatalf("edited template accepted %v %v", ok, err)
	}
	var metadata []byte
	if err := db.QueryRow(`SELECT summary_generation FROM articles WHERE id=$1`, id).Scan(&metadata); err != nil || len(metadata) > 0 {
		t.Fatalf("rejected result wrote metadata %s %v", metadata, err)
	}
}

func TestSummaryGenerationWaitsForConcurrentTemplateEdit(t *testing.T) {
	repo, db, _, id := seedLinkSetParent(t)
	if _, err := db.Exec(`UPDATE articles SET title='Title',content='body',summary_brief='original',summary_detailed='original' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	var templateID int
	var brief, detailed string
	if err := db.QueryRow(`SELECT id,brief_prompt,detailed_prompt FROM summary_templates WHERE is_system LIMIT 1`).Scan(&templateID, &brief, &detailed); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("body"))
	g := &model.SummaryGeneration{ContentHash: hex.EncodeToString(sum[:]), SourceTitle: "Title"}
	g.BindTemplate(templateID, brief, detailed)
	token, err := repo.ClaimSummary(t.Context(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	editor, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer editor.Rollback()
	var editorPID int
	if err := editor.QueryRow(`SELECT pg_backend_pid()`).Scan(&editorPID); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Exec(`UPDATE summary_templates SET brief_prompt=brief_prompt || ' edited' WHERE id=$1`, templateID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		saved bool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		saved, err := repo.CompleteSummary(id, token, "body", "new", "new", g)
		done <- result{saved, err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case r := <-done:
			t.Fatalf("completion did not wait for template editor: %+v", r)
		default:
		}
		var blocked bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, editorPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completion never acquired template guard")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := editor.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.saved {
			t.Fatalf("accepted stale template %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completion did not finish")
	}
	var saved string
	if err := db.QueryRow(`SELECT summary_brief FROM articles WHERE id=$1`, id).Scan(&saved); err != nil || saved != "original" {
		t.Fatalf("prior result overwritten %q %v", saved, err)
	}
}
