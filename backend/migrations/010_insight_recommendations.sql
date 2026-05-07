-- 010_insight_recommendations.sql
-- Add a structured "recommendations" payload to each insight: a JSONB array of
-- direction objects, each with a list of (article_id, reason). Existing rows
-- stay NULL and the API surfaces NULL/empty as "no recommendations yet".

ALTER TABLE user_insights
  ADD COLUMN IF NOT EXISTS recommendations JSONB;
