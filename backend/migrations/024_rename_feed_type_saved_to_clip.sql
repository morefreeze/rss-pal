-- 024_rename_feed_type_saved_to_clip.sql
-- Rename the 网摘 pseudo-feed type from 'saved' to 'clip'. The "saved" name
-- collided with user_preferences.signal_type='save' (per-article star/bookmark),
-- which is a different concept. After this migration, 'saved' must no longer
-- appear as a feed_type value.

UPDATE feeds SET feed_type = 'clip' WHERE feed_type = 'saved';
