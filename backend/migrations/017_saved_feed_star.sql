-- Rename bookmarklet feed title from 📑 收藏 to ⭐ 收藏 for already-existing rows.
UPDATE feeds SET title = '⭐ 收藏'
WHERE feed_type = 'saved' AND title = '📑 收藏';
