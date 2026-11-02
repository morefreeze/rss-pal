-- Add provenance for future generation only. Existing summaries stay untouched.
ALTER TABLE articles ADD COLUMN IF NOT EXISTS summary_generation JSONB;

-- All writers, including content repair, imports and legacy reset paths, must
-- drop provenance when replacing its inputs/output without a matching new trace.
CREATE OR REPLACE FUNCTION invalidate_summary_generation() RETURNS trigger AS $$
BEGIN
 IF (NEW.title IS DISTINCT FROM OLD.title OR NEW.content IS DISTINCT FROM OLD.content
     OR NEW.summary_brief IS DISTINCT FROM OLD.summary_brief
     OR NEW.summary_detailed IS DISTINCT FROM OLD.summary_detailed)
    AND NEW.summary_generation IS NOT DISTINCT FROM OLD.summary_generation THEN
  NEW.summary_generation := NULL;
 END IF;
 IF COALESCE(NEW.summary_brief,'')='' OR COALESCE(NEW.summary_detailed,'')='' THEN
  NEW.summary_generation := NULL;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS articles_summary_generation_invalidate ON articles;
CREATE TRIGGER articles_summary_generation_invalidate
 BEFORE UPDATE OF title,content,summary_brief,summary_detailed ON articles
 FOR EACH ROW EXECUTE FUNCTION invalidate_summary_generation();
