-- Per-article cached image dimensions, used by the frontend to reserve
-- layout space for <img> tags so reading-progress doesn't regress when
-- lazy-loaded images settle. Shape: {"<image-url>": [width, height], ...}.
-- NULL = not yet probed; renderer falls back to height:auto and the existing
-- ResizeObserver rescale.
ALTER TABLE articles ADD COLUMN IF NOT EXISTS image_dimensions JSONB;
