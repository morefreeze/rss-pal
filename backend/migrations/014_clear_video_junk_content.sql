-- 014_clear_video_junk_content.sql
-- Existing video articles (video/youtube, video/bilibili) have junk content
-- scraped from their JS-heavy watch pages, because deep-fetch ran before
-- video-aware skip logic existed. Clear that junk so the article body
-- doesn't display useless text. Articles that already have a fetched
-- transcript section are left alone. Idempotent.

UPDATE articles
SET content = '',
    word_count = 0,
    reading_minutes = 0
WHERE media_type LIKE 'video/%'
  AND content NOT LIKE '%## 字幕%';
