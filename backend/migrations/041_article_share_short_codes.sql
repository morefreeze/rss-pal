ALTER TABLE article_shares
    ADD COLUMN IF NOT EXISTS short_code VARCHAR(12) COLLATE "C";

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'article_shares'::regclass
          AND conname = 'article_shares_short_code_format'
    ) THEN
        ALTER TABLE article_shares
            ADD CONSTRAINT article_shares_short_code_format
            CHECK (short_code IS NULL OR short_code ~ '^[0-9A-Za-z]{12}$');
    END IF;
END
$$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_article_shares_short_code
    ON article_shares (short_code)
    WHERE short_code IS NOT NULL;
