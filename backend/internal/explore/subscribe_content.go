package explore

import "github.com/lib/pq"

// WithContentCache supplies an admin cache reader. It is used only after source
// authorization and target ownership checks; request RLS stays enabled.
func (s *SubscribeService) WithContentCache(db Querier) *SubscribeService {
	if s != nil {
		s.contentCache = db
	}
	return s
}

type copiedOriginal struct{ url, content string }

func loadCopiedOriginals(db Querier, candidates []exploreArticleCopyCandidate) (map[int]copiedOriginal, error) {
	result := make(map[int]copiedOriginal)
	if db == nil || len(candidates) == 0 {
		return result, nil
	}
	ids := make([]int, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.id)
	}
	rows, err := db.Query(`SELECT article_id,source_url,content FROM explore_article_content WHERE article_id=ANY($1) AND status='fetched'`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var body copiedOriginal
		if err = rows.Scan(&id, &body.url, &body.content); err != nil {
			return nil, err
		}
		result[id] = body
	}
	return result, rows.Err()
}
