package repository

// GetMediaByID returns the stored enclosure URL (and its media type) for an
// article without any owner scoping. It backs the public audio relay route,
// which is why repositories used with it are wired to a bypass-RLS pool —
// browser media requests carry no JWT, so there is no per-request tx to set
// app.user_id from.
func (r *ArticleRepository) GetMediaByID(id int) (mediaURL, mediaType string, err error) {
	const query = `
		SELECT COALESCE(media_url, ''), COALESCE(media_type, '')
		FROM articles
		WHERE id = $1`
	err = r.db.QueryRow(query, id).Scan(&mediaURL, &mediaType)
	if err != nil {
		return "", "", err
	}
	return mediaURL, mediaType, nil
}
