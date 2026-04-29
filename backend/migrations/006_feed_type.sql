-- Add feed_type to distinguish RSS/Atom vs HTML-scraped feeds
ALTER TABLE feeds ADD COLUMN IF NOT EXISTS feed_type VARCHAR(20) DEFAULT 'rss';
