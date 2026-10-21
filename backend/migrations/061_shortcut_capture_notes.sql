-- Pending shortcut notes remain private until explicit batch confirmation.
ALTER TABLE article_capture_drafts
 ADD COLUMN IF NOT EXISTS capture_note TEXT NOT NULL DEFAULT ''
 CHECK (char_length(capture_note) <= 10000);
