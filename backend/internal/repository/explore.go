package repository

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

const (
	MaxExplorePageSize         = 50
	MaxExploreListExcerptRunes = 500
	ExploreExposureDedupeTime  = 10 * time.Minute

	// 0x45585049 is ASCII "EXPI". Combined with user_id through PostgreSQL's
	// two-int advisory-lock namespace, it serializes only one user's interest
	// replacement while leaving other users independent.
	exploreInterestReplacementAdvisoryNamespace = 0x45585049
)

var (
	ErrExploreNotFound        = errors.New("explore resource not found")
	ErrInvalidExploreFeedback = errors.New("invalid explore feedback")
	ErrInvalidExploreEvent    = errors.New("invalid explore article event")
	ErrInvalidExploreInterest = errors.New("invalid explore interest")
)

// exploreInterestVocabulary is intentionally server-owned. Provider output is
// not accepted as arbitrary profile input through the interests endpoint.
var exploreInterestVocabulary = []string{
	"ai", "ai_eng", "blog", "business", "chinese-independent", "cn_tech",
	"enterprise", "health", "news", "podcast", "programming", "recently-added",
	"security", "self-hosted", "technology", "web-development", "youtube",
}

var exploreInterestSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(exploreInterestVocabulary))
	for _, value := range exploreInterestVocabulary {
		set[value] = struct{}{}
	}
	return set
}()

type ExploreRepository struct {
	db Querier
}

func NewExploreRepository(db *sql.DB) *ExploreRepository {
	return &ExploreRepository{db: db}
}

func (r *ExploreRepository) WithQuerier(db Querier) *ExploreRepository {
	return &ExploreRepository{db: db}
}

func (r *ExploreRepository) WithCtx(c ctxkey.CtxGetter) *ExploreRepository {
	if value, ok := c.Get(ctxkey.Tx); ok {
		if db, ok := value.(Querier); ok {
			return r.WithQuerier(db)
		}
	}
	return r
}

type ExploreListParams struct {
	Limit  int
	Offset int
	Sort   SortMode
	Dir    SortDir
	Topic  string
}

type ExploreSnapshotStatus struct {
	ID            int        `json:"id"`
	SlotAt        time.Time  `json:"slot_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Generating    bool       `json:"generating"`
	UsingFallback bool       `json:"using_fallback"`
	RefreshFailed bool       `json:"refresh_failed"`
	NextRefreshAt *time.Time `json:"next_refresh_at,omitempty"`
}

type ExploreArticleListItem struct {
	ID           int        `json:"id"`
	SourceID     int        `json:"source_id"`
	SourceTitle  string     `json:"source_title"`
	Title        string     `json:"title"`
	URL          string     `json:"url"`
	Excerpt      string     `json:"excerpt"`
	PublishedAt  *time.Time `json:"published_at"`
	FetchedAt    time.Time  `json:"fetched_at"`
	Topic        string     `json:"topic"`
	Reason       string     `json:"reason"`
	IsSubscribed bool       `json:"is_subscribed"`
}

type ExplorePage struct {
	Snapshot ExploreSnapshotStatus    `json:"snapshot"`
	Articles []ExploreArticleListItem `json:"articles"`
	HasMore  bool                     `json:"has_more"`
}

type ExploreSourceItem struct {
	ID                 int      `json:"id"`
	Title              string   `json:"title"`
	URL                string   `json:"url"`
	SiteURL            *string  `json:"site_url,omitempty"`
	Rank               int      `json:"rank"`
	Topic              string   `json:"topic"`
	Reason             string   `json:"reason"`
	HealthScore        *float64 `json:"health_score,omitempty"`
	ValidationStatus   string   `json:"validation_status"`
	IsBroken           bool     `json:"is_broken"`
	MergedIntoSourceID *int     `json:"merged_into_source_id,omitempty"`
	RecentArticleCount int      `json:"recent_article_count"`
	Selected           bool     `json:"selected"`
	IsHidden           bool     `json:"is_hidden"`
	IsSubscribed       bool     `json:"is_subscribed"`
}

type ExploreArticleDetail struct {
	ID          int        `json:"id"`
	SourceID    int        `json:"source_id"`
	SourceTitle string     `json:"source_title"`
	SourceURL   string     `json:"source_url"`
	SiteURL     *string    `json:"site_url,omitempty"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Content     *string    `json:"content"`
	Excerpt     *string    `json:"excerpt,omitempty"`
	PublishedAt *time.Time `json:"published_at"`
	FetchedAt   time.Time  `json:"fetched_at"`
}

type ExploreFeedbackInput struct {
	FeedbackType string
	SourceID     *int
	Topic        *string
}

func (r *ExploreRepository) GetPage(userID int, params ExploreListParams) (*ExplorePage, error) {
	params = normalizeExploreListParams(params)
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, err
	}
	defer rollback()

	status, err := readExploreSnapshotStatus(tx, userID)
	if err != nil {
		return nil, err
	}
	page := &ExplorePage{Snapshot: status, Articles: []ExploreArticleListItem{}}
	if status.ID == 0 {
		if err := commit(); err != nil {
			return nil, err
		}
		return page, nil
	}

	args := []any{userID, status.ID}
	if params.Topic != "" {
		args = append(args, params.Topic)
	}
	query := buildExplorePageQuery(params)

	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	articles := make([]ExploreArticleListItem, 0, MaxExploreSnapshotSources*5)
	for rows.Next() {
		var item ExploreArticleListItem
		if err := rows.Scan(
			&item.ID, &item.SourceID, &item.SourceTitle, &item.Title, &item.URL,
			&item.Excerpt, &item.PublishedAt, &item.FetchedAt, &item.Topic,
			&item.Reason, &item.IsSubscribed,
		); err != nil {
			rows.Close()
			return nil, err
		}
		item = normalizeExploreArticleListItem(item)
		articles = append(articles, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	articles = stableDiversifyExploreArticles(articles)
	if params.Offset < len(articles) {
		end := params.Offset + params.Limit
		if end > len(articles) {
			end = len(articles)
		}
		page.Articles = articles[params.Offset:end]
		page.HasMore = end < len(articles)
	}
	if err := commit(); err != nil {
		return nil, err
	}
	return page, nil
}

func normalizeExploreArticleListItem(item ExploreArticleListItem) ExploreArticleListItem {
	runes := []rune(item.Excerpt)
	if len(runes) > MaxExploreListExcerptRunes {
		item.Excerpt = string(runes[:MaxExploreListExcerptRunes])
	}
	return item
}

func buildExplorePageQuery(params ExploreListParams) string {
	topicClause := ""
	if params.Topic != "" {
		topicClause = " AND COALESCE(batch_source.topic, '') = $3"
	}
	requestedOrder := ArticleOrderClause(ArticleAliasExplore, params.Sort, params.Dir)
	return `
		SELECT explore_articles.id, explore_articles.source_id, source.title,
		       explore_articles.title, explore_articles.url,
		       COALESCE(explore_articles.excerpt, ''), explore_articles.published_at,
		       explore_articles.fetched_at, COALESCE(batch_source.topic, ''),
		       COALESCE(batch_source.reason, ''),
		       EXISTS (
		           SELECT 1 FROM feeds formal_feed
		           WHERE formal_feed.url=source.url
		             AND (formal_feed.owner_id IS NULL OR formal_feed.owner_id=$1)
		       ) AS is_subscribed
		FROM explore_batches batch
		JOIN explore_batch_sources batch_source
		  ON batch_source.batch_id=batch.id AND batch_source.user_id=$1
		JOIN recommended_feeds source ON source.id=batch_source.source_id
		JOIN LATERAL (
			SELECT explore_articles.* FROM explore_articles
			WHERE explore_articles.source_id=source.id
			ORDER BY COALESCE(explore_articles.published_at, explore_articles.fetched_at) DESC,
			         explore_articles.fetched_at DESC, explore_articles.id DESC
			LIMIT 5
		) explore_articles ON true
		WHERE batch.id=$2 AND batch.user_id=$1 AND batch.status='done'
		  AND source.validation_status='valid'
		  AND source.is_broken=false
		  AND source.merged_into_source_id IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM explore_feedback hidden
		      WHERE hidden.user_id=$1 AND hidden.feedback_type='hide_source'
		        AND hidden.source_id=source.id
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM explore_feedback dampened
		      WHERE dampened.user_id=$1 AND dampened.feedback_type='dampen_topic'
		        AND dampened.topic=batch_source.topic
		  )` + topicClause + `
		` + requestedOrder + `, explore_articles.id ` + params.Dir.sql()
}

func normalizeExploreListParams(params ExploreListParams) ExploreListParams {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > MaxExplorePageSize {
		params.Limit = MaxExplorePageSize
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	if params.Sort != SortCaptured {
		params.Sort = SortPublished
	}
	if params.Dir != SortAsc {
		params.Dir = SortDesc
	}
	params.Topic = strings.TrimSpace(params.Topic)
	return params
}

func readExploreSnapshotStatus(db Querier, userID int) (ExploreSnapshotStatus, error) {
	var status ExploreSnapshotStatus
	var latestStatus string
	var latestSlot time.Time
	err := db.QueryRow(`
		SELECT status, slot_at FROM explore_batches
		WHERE user_id=$1 ORDER BY slot_at DESC, id DESC LIMIT 1
	`, userID).Scan(&latestStatus, &latestSlot)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return status, err
	}
	err = db.QueryRow(`
		SELECT id, slot_at, completed_at FROM explore_batches
		WHERE user_id=$1 AND status='done'
		ORDER BY slot_at DESC, id DESC LIMIT 1
	`, userID).Scan(&status.ID, &status.SlotAt, &status.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	} else if err != nil {
		return status, err
	}
	status.Generating = latestStatus == model.ExploreBatchPending && (status.ID == 0 || latestSlot.After(status.SlotAt))
	status.RefreshFailed = latestStatus == model.ExploreBatchFailed
	status.UsingFallback = status.RefreshFailed && status.ID != 0 && latestSlot.After(status.SlotAt)
	return status, nil
}

func stableDiversifyExploreArticles(in []ExploreArticleListItem) []ExploreArticleListItem {
	remaining := append([]ExploreArticleListItem(nil), in...)
	out := make([]ExploreArticleListItem, 0, len(in))
	lastSource, runLength := 0, 0
	for len(remaining) > 0 {
		index := 0
		if runLength >= 2 && remaining[0].SourceID == lastSource {
			index = -1
			for candidate := 1; candidate < len(remaining); candidate++ {
				if remaining[candidate].SourceID != lastSource {
					index = candidate
					break
				}
			}
			if index < 0 {
				index = 0
			}
		}
		item := remaining[index]
		remaining = append(remaining[:index], remaining[index+1:]...)
		if item.SourceID == lastSource {
			runLength++
		} else {
			lastSource, runLength = item.SourceID, 1
		}
		out = append(out, item)
	}
	return out
}

func (r *ExploreRepository) GetSources(userID int) ([]ExploreSourceItem, error) {
	rows, err := r.db.Query(`
		WITH latest_done AS (
			SELECT id FROM explore_batches WHERE user_id=$1 AND status='done'
			ORDER BY slot_at DESC, id DESC LIMIT 1
		)
		SELECT source.id, source.title, source.url, source.site_url,
		       batch_source.rank, COALESCE(batch_source.topic, ''),
		       COALESCE(batch_source.reason, ''), source.health_score,
		       source.validation_status, source.is_broken, source.merged_into_source_id,
		       (SELECT COUNT(*) FROM explore_articles article WHERE article.source_id=source.id),
		       false,
		       EXISTS (SELECT 1 FROM explore_feedback feedback
		               WHERE feedback.user_id=$1 AND feedback.source_id=source.id
		                 AND feedback.feedback_type='hide_source'),
		       EXISTS (SELECT 1 FROM feeds formal_feed WHERE formal_feed.url=source.url
		               AND (formal_feed.owner_id IS NULL OR formal_feed.owner_id=$1))
		FROM latest_done batch
		JOIN explore_batch_sources batch_source
		  ON batch_source.batch_id=batch.id AND batch_source.user_id=$1
		JOIN recommended_feeds source ON source.id=batch_source.source_id
		ORDER BY batch_source.rank, source.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExploreSourceItem{}
	for rows.Next() {
		var item ExploreSourceItem
		if err := rows.Scan(
			&item.ID, &item.Title, &item.URL, &item.SiteURL, &item.Rank, &item.Topic,
			&item.Reason, &item.HealthScore, &item.ValidationStatus,
			&item.IsBroken, &item.MergedIntoSourceID,
			&item.RecentArticleCount, &item.Selected, &item.IsHidden, &item.IsSubscribed,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *ExploreRepository) GetVisibleArticle(userID, articleID int) (*ExploreArticleDetail, error) {
	var detail ExploreArticleDetail
	err := r.db.QueryRow(`
		SELECT article.id, article.source_id, source.title, source.url, source.site_url,
		       article.title, article.url, article.content, article.excerpt,
		       article.published_at, article.fetched_at
		FROM explore_articles article
		JOIN recommended_feeds source ON source.id=article.source_id
		WHERE article.id=$2 AND (
			EXISTS (
				SELECT 1 FROM explore_batch_sources batch_source
				JOIN explore_batches batch
				  ON batch.id=batch_source.batch_id AND batch.user_id=batch_source.user_id
				WHERE batch_source.user_id=$1 AND batch_source.source_id=article.source_id
				  AND batch.status='done' AND batch.completed_at >= NOW() - INTERVAL '30 days'
			) OR EXISTS (
				SELECT 1 FROM feeds formal_feed
				WHERE formal_feed.url=source.url
				  AND (formal_feed.owner_id IS NULL OR formal_feed.owner_id=$1)
			)
		)
	`, userID, articleID).Scan(
		&detail.ID, &detail.SourceID, &detail.SourceTitle, &detail.SourceURL,
		&detail.SiteURL, &detail.Title, &detail.URL, &detail.Content, &detail.Excerpt,
		&detail.PublishedAt, &detail.FetchedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrExploreNotFound
	}
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

func (r *ExploreRepository) CreateFeedback(userID int, input ExploreFeedbackInput) (*model.ExploreFeedback, error) {
	if input.Topic != nil {
		topic := strings.TrimSpace(*input.Topic)
		input.Topic = &topic
	}
	if err := validateExploreFeedback(input); err != nil {
		return nil, err
	}
	if input.FeedbackType == model.ExploreFeedbackHideSource {
		var visible bool
		err := r.db.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM explore_batch_sources batch_source
				JOIN explore_batches batch
				  ON batch.id=batch_source.batch_id AND batch.user_id=batch_source.user_id
				WHERE batch_source.user_id=$1 AND batch_source.source_id=$2
				  AND batch.status='done'
				  AND batch.id=(SELECT id FROM explore_batches WHERE user_id=$1 AND status='done' ORDER BY slot_at DESC,id DESC LIMIT 1)
			)
		`, userID, *input.SourceID).Scan(&visible)
		if err != nil {
			return nil, err
		}
		if !visible {
			return nil, ErrExploreNotFound
		}
	}
	if input.FeedbackType == model.ExploreFeedbackDampenTopic {
		var visible bool
		err := r.db.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM explore_batch_sources batch_source
				JOIN explore_batches batch
				  ON batch.id=batch_source.batch_id AND batch.user_id=batch_source.user_id
				WHERE batch_source.user_id=$1 AND batch_source.topic=$2
				  AND batch.status='done'
				  AND batch.id=(SELECT id FROM explore_batches WHERE user_id=$1 AND status='done' ORDER BY slot_at DESC,id DESC LIMIT 1)
			)
		`, userID, *input.Topic).Scan(&visible)
		if err != nil {
			return nil, err
		}
		if !visible {
			return nil, ErrExploreNotFound
		}
	}
	feedback := &model.ExploreFeedback{}
	var err error
	if input.SourceID != nil {
		err = r.db.QueryRow(`
			INSERT INTO explore_feedback(user_id,source_id,feedback_type)
			VALUES ($1,$2,$3)
			ON CONFLICT (user_id,source_id,feedback_type) WHERE source_id IS NOT NULL
			DO UPDATE SET created_at=explore_feedback.created_at
			RETURNING id,user_id,source_id,topic,feedback_type,created_at
		`, userID, *input.SourceID, input.FeedbackType).Scan(
			&feedback.ID, &feedback.UserID, &feedback.SourceID, &feedback.Topic,
			&feedback.FeedbackType, &feedback.CreatedAt,
		)
	} else {
		err = r.db.QueryRow(`
			INSERT INTO explore_feedback(user_id,topic,feedback_type)
			VALUES ($1,$2,$3)
			ON CONFLICT (user_id,topic,feedback_type) WHERE topic IS NOT NULL
			DO UPDATE SET created_at=explore_feedback.created_at
			RETURNING id,user_id,source_id,topic,feedback_type,created_at
		`, userID, *input.Topic, input.FeedbackType).Scan(
			&feedback.ID, &feedback.UserID, &feedback.SourceID, &feedback.Topic,
			&feedback.FeedbackType, &feedback.CreatedAt,
		)
	}
	if err != nil {
		return nil, err
	}
	return feedback, nil
}

func validateExploreFeedback(input ExploreFeedbackInput) error {
	switch input.FeedbackType {
	case model.ExploreFeedbackHideSource:
		if input.SourceID == nil || *input.SourceID <= 0 || input.Topic != nil {
			return ErrInvalidExploreFeedback
		}
	case model.ExploreFeedbackDampenTopic:
		if input.SourceID != nil || input.Topic == nil || *input.Topic == "" || len([]rune(*input.Topic)) > 100 {
			return ErrInvalidExploreFeedback
		}
	case model.ExploreFeedbackBoostTopic:
		if input.SourceID != nil || input.Topic == nil || !IsExploreInterest(*input.Topic) {
			return ErrInvalidExploreFeedback
		}
	default:
		return ErrInvalidExploreFeedback
	}
	return nil
}

func (r *ExploreRepository) DeleteFeedback(userID, feedbackID int) error {
	result, err := r.db.Exec(`DELETE FROM explore_feedback WHERE id=$1 AND user_id=$2`, feedbackID, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrExploreNotFound
	}
	return nil
}

// ClearNegativeFeedback removes only the two feedback kinds that suppress
// Explore candidates. Positive interests are profile input and deliberately
// survive this recovery action.
func (r *ExploreRepository) ClearNegativeFeedback(userID int) (int, error) {
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return 0, err
	}
	defer rollback()
	result, err := tx.Exec(`
		DELETE FROM explore_feedback
		WHERE user_id=$1 AND feedback_type IN ('hide_source','dampen_topic')
	`, userID)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := commit(); err != nil {
		return 0, err
	}
	return int(count), nil
}

func IsExploreInterest(value string) bool {
	_, ok := exploreInterestSet[strings.TrimSpace(value)]
	return ok
}

func (r *ExploreRepository) ReplaceInterests(userID int, topics []string) ([]model.ExploreFeedback, error) {
	unique := make([]string, 0, len(topics))
	seen := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		topic = strings.TrimSpace(topic)
		if !IsExploreInterest(topic) {
			return nil, ErrInvalidExploreInterest
		}
		if _, exists := seen[topic]; exists {
			continue
		}
		seen[topic] = struct{}{}
		unique = append(unique, topic)
	}
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return nil, err
	}
	defer rollback()
	if err := lockExploreInterestReplacement(tx, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM explore_feedback WHERE user_id=$1 AND feedback_type='boost_topic'`, userID); err != nil {
		return nil, err
	}
	result := make([]model.ExploreFeedback, 0, len(unique))
	for _, topic := range unique {
		var feedback model.ExploreFeedback
		if err := tx.QueryRow(`
			INSERT INTO explore_feedback(user_id,topic,feedback_type)
			VALUES ($1,$2,'boost_topic')
			RETURNING id,user_id,source_id,topic,feedback_type,created_at
		`, userID, topic).Scan(
			&feedback.ID, &feedback.UserID, &feedback.SourceID, &feedback.Topic,
			&feedback.FeedbackType, &feedback.CreatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, feedback)
	}
	if err := commit(); err != nil {
		return nil, err
	}
	return result, nil
}

type exploreInterestLockExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func lockExploreInterestReplacement(db exploreInterestLockExecutor, userID int) error {
	_, err := db.Exec(`SELECT pg_advisory_xact_lock($1,$2)`, exploreInterestReplacementAdvisoryNamespace, userID)
	return err
}

func (r *ExploreRepository) RecordArticleEvent(userID, articleID int, eventType string, occurredAt time.Time) (bool, error) {
	if articleID <= 0 || occurredAt.IsZero() || !isExploreEventType(eventType) {
		return false, ErrInvalidExploreEvent
	}
	tx, commit, rollback, err := txOrBegin(r.db)
	if err != nil {
		return false, err
	}
	defer rollback()
	bound := r.WithQuerier(tx)
	if _, err := bound.GetVisibleArticle(userID, articleID); err != nil {
		return false, err
	}
	if eventType == model.ExploreArticleEventExposure {
		if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1,$2)`, userID, articleID); err != nil {
			return false, err
		}
		var duplicate bool
		if err := tx.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM explore_article_events
				WHERE user_id=$1 AND explore_article_id=$2 AND event_type='exposure'
				  AND occurred_at >= $3
			)
		`, userID, articleID, occurredAt.Add(-ExploreExposureDedupeTime)).Scan(&duplicate); err != nil {
			return false, err
		}
		if duplicate {
			if err := commit(); err != nil {
				return false, err
			}
			return false, nil
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO explore_article_events(user_id,explore_article_id,event_type,occurred_at)
		VALUES ($1,$2,$3,$4)
	`, userID, articleID, eventType, occurredAt); err != nil {
		return false, err
	}
	if err := commit(); err != nil {
		return false, err
	}
	return true, nil
}

func isExploreEventType(value string) bool {
	return value == model.ExploreArticleEventExposure ||
		value == model.ExploreArticleEventClick ||
		value == model.ExploreArticleEventCompletedRead
}
