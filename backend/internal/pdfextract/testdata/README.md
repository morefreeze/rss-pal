# pdfextract testdata

These PDFs are committed for unit-test reproducibility. Sizes are intentionally small.

- `digital.pdf` — short typed PDF, two pages, mixed English + Chinese paragraphs. Used to verify text extraction + page split.
- `scanned.pdf` — page images of `digital.pdf` with no text layer. Used to verify the ErrNoText threshold.
- `mixed.pdf` — `digital.pdf` concatenated with `scanned.pdf`. Used to verify partial-page OCR fallback.
- `image_heavy.pdf` — 150 images including 30 duplicates by SHA. Verifies dedup + 100-cap.
- `corrupt.pdf` — literally the text "NOT A PDF". Verifies error path.
- `knuth-1980.pdf` — Knuth's 1980 paper on typography from https://gwern.net/doc/design/typography/1980-knuth.pdf — real-world digital PDF regression fixture.

To regenerate: see the commands in `docs/superpowers/plans/2026-05-25-pdf-clip.md` Task 4.
