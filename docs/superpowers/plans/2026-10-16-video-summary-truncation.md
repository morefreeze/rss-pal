# Video summary completion repair

Goal: never persist a token-limited completion as success, recognize captured video URLs, and repair article 3250 using available video sources.

Design: parse finish_reason in shared text/vision responses. Retry nonstream length once with double the budget (cap 16000); reject further length finishes. Streamed partial output cannot be replayed safely: return an error on length or missing completion marker. Avoid vision fallback after partial output. Infer capture media from the page URL before HTML media, preserving existing URL validation.

- [x] Add regression tests and reproduce failures against current code.
- [x] Implement completion checks, bounded retry, and video capture detection.
- [x] Run backend verification and independent code review.
- [ ] Integrate into origin/master without touching dirty primary checkout; push and wait for Tencent deployment.
- [ ] Back up article 3250, repair media metadata, request subtitle extraction and summary regeneration; verify persisted result and API/UI.

Evidence: Tencent 9fdd247; article 3250 has NULL media_type, no transcript, 891-character detailed summary ending mid-sentence. Output usage 4000 matches detailed token cap. No content or credentials belong in logs or committed artifacts.

Validation: baseline affected-package tests passed; new regressions failed before implementation and passed after. Full `go test ./...`, affected `go vet`, and independent review passed. Database-dependent tests can skip without a configured test DSN.
