package repository

import (
	"database/sql"
	"errors"
	"github.com/lib/pq"
)

var ErrInvalidExploreArticleState = errors.New("saved or skipped boolean is required")

type ExploreArticleState struct {
	NormalizedURL string `json:"normalized_url"`
	Saved         bool   `json:"saved"`
	Skipped       bool   `json:"skipped"`
}
type ExploreArticleStatePatch struct {
	Saved   *bool `json:"saved"`
	Skipped *bool `json:"skipped"`
}

func (r *ExploreRepository) UpdateArticleState(userID, articleID int, patch ExploreArticleStatePatch) (*ExploreArticleState, error) {
	if patch.Saved == nil && patch.Skipped == nil {
		return nil, ErrInvalidExploreArticleState
	}
	q, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, err
	}
	defer rollback()
	bound := r.WithQuerier(q)
	detail, err := bound.GetVisibleArticle(userID, articleID)
	if err != nil {
		return nil, err
	}
	// Serialize with catalog refresh/retention before reading the article again.
	// If cleanup won the race, return not-found instead of saving a missing item.
	if err = lockExploreSourceForWrite(q, detail.SourceID); err != nil {
		return nil, err
	}
	var url string
	if err = q.QueryRow(`SELECT normalized_url FROM explore_articles WHERE id=$1`, articleID).Scan(&url); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrExploreNotFound
		}
		return nil, err
	}
	state := ExploreArticleState{NormalizedURL: url}
	err = q.QueryRow(`INSERT INTO explore_article_states(user_id,normalized_url,saved,skipped,saved_at)
 VALUES ($1,$2,COALESCE($3,false),COALESCE($4,false),CASE WHEN $3 THEN NOW() END)
 ON CONFLICT (user_id,normalized_url) DO UPDATE SET
 saved=COALESCE($3,explore_article_states.saved),
 skipped=COALESCE($4,explore_article_states.skipped),
 saved_at=CASE WHEN $3=true AND NOT explore_article_states.saved THEN NOW() WHEN $3=false THEN NULL ELSE explore_article_states.saved_at END,
 updated_at=NOW()
 RETURNING saved,skipped`, userID, url, patch.Saved, patch.Skipped).Scan(&state.Saved, &state.Skipped)
	if err != nil {
		return nil, err
	}
	if err = commit(); err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *ExploreRepository) getSavedPage(userID int, params ExploreListParams) (*ExplorePage, error) {
	formalURLs, err := visibleFormalNormalizedURLs(r.db, userID)
	if err != nil {
		return nil, err
	}
	page := &ExplorePage{Articles: []ExploreArticleListItem{}, Interests: []string{}}
	// One saved URL appears once even when multiple feeds publish the same article.
	query := `SELECT explore_articles.id,explore_articles.source_id,source.title,
 explore_articles.title,explore_articles.url,COALESCE(explore_articles.excerpt,''),explore_articles.thumbnail_url,
 explore_articles.published_at,explore_articles.fetched_at,COALESCE(NULLIF(topic.topic,''),NULLIF(source.category,''),observation.topic,''),'',source.normalized_url=ANY($2),state.saved,state.skipped,explore_articles.normalized_url
 FROM explore_article_states state
 JOIN LATERAL (SELECT * FROM explore_articles WHERE normalized_url=state.normalized_url ORDER BY fetched_at DESC,id DESC LIMIT 1) explore_articles ON true
 JOIN recommended_feeds source ON source.id=explore_articles.source_id
 LEFT JOIN LATERAL (
 SELECT batch_source.topic FROM explore_batch_sources batch_source
 JOIN explore_batches batch ON batch.id=batch_source.batch_id AND batch.user_id=batch_source.user_id
 WHERE batch_source.user_id=$1 AND batch_source.source_id=source.id AND batch.status='done'
 ORDER BY batch.slot_at DESC,batch.id DESC LIMIT 1
 ) topic ON true
 LEFT JOIN LATERAL (
 SELECT provider.topic FROM explore_source_observations item
 JOIN explore_registry_providers provider ON provider.id=item.provider_id
 WHERE item.source_id=source.id AND provider.enabled
 ORDER BY item.last_seen_at DESC,item.id DESC LIMIT 1
 ) observation ON true
 WHERE state.user_id=$1 AND state.saved AND ($3='' OR COALESCE(NULLIF(topic.topic,''),NULLIF(source.category,''),observation.topic,'')=$3)
 ` + ArticleOrderClause(ArticleAliasExplore, params.Sort, params.Dir) + `, explore_articles.id ` + params.Dir.sql() + ` LIMIT $4 OFFSET $5`
	rows, err := r.db.Query(query, userID, pq.Array(formalURLs), params.Topic, params.Limit+1, params.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ExploreArticleListItem
		if err = rows.Scan(&item.ID, &item.SourceID, &item.SourceTitle, &item.Title, &item.URL, &item.Excerpt, &item.ThumbnailURL, &item.PublishedAt, &item.FetchedAt, &item.Topic, &item.Reason, &item.IsSubscribed, &item.Saved, &item.Skipped, &item.NormalizedURL); err != nil {
			return nil, err
		}
		page.Articles = append(page.Articles, normalizeExploreArticleListItem(item))
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Articles) > params.Limit {
		page.HasMore = true
		page.Articles = page.Articles[:params.Limit]
	}
	return page, nil
}
