-- Rename bookmarklet feed title from ⭐ 收藏 to ⭐ 网摘 to distinguish
-- from the "saved article" (signal_type='save') concept.
UPDATE feeds SET title = '⭐ 网摘'
WHERE feed_type = 'saved' AND title IN ('⭐ 收藏', '📑 收藏');
