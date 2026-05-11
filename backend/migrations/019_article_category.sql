-- 019_article_category.sql
-- Coarse, closed-enum article category for the /articles 分组 view.
-- Distinct from articles.topic (fine-grained AI-generated, sparsely populated
-- because it was gated on user engagement). Category is assigned to every
-- article by the worker classify pass and drawn from a 10-value enum
-- validated at the application layer (no DB CHECK so the enum can grow).

ALTER TABLE articles ADD COLUMN IF NOT EXISTS category VARCHAR(20);

CREATE INDEX IF NOT EXISTS idx_articles_category
  ON articles(category) WHERE category IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_articles_no_category
  ON articles(id) WHERE category IS NULL;

CREATE TABLE IF NOT EXISTS interest_categories (
  id                 SERIAL PRIMARY KEY,
  user_id            INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category           VARCHAR(20) NOT NULL,
  weight             FLOAT NOT NULL DEFAULT 0,
  last_reinforced_at TIMESTAMP NOT NULL DEFAULT NOW(),
  UNIQUE(user_id, category)
);

CREATE INDEX IF NOT EXISTS idx_interest_categories_user_weight
  ON interest_categories(user_id, weight DESC);
