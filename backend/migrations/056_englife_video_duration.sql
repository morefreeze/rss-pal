ALTER TABLE englife_jobs ADD COLUMN IF NOT EXISTS duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0);
