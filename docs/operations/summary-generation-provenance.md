# Summary generation provenance

New summaries use one shared faithfulness policy (`ai.SummaryPolicyVersion`) across worker generation, manual generation, custom templates, streaming, vision and browser-local AI. Custom templates retain their existing formatting and length semantics. There is no additional production model reviewer.

`articles.summary_generation` is nullable JSONB. Migration 072 adds the column and invalidation trigger without rewriting or enqueuing any historical summary. Missing metadata means unknown provenance, not a failed or current-policy summary. Provenance is returned on authenticated article detail; it does not certify semantic quality.

| Field | Meaning |
| --- | --- |
| `schema_version` | Provenance JSON shape |
| `policy_version`, `policy_hash` | Named factual policy version and SHA256 of the complete system guardrail |
| `prompt_version` | Built-in prompt assembly version; change alongside prompt/anchor/compression behavior |
| `content_hash` | SHA256 of the exact stored article body before preparation/truncation |
| `input_hash` | SHA256 of UTF-8 title, NUL separator, and stored body |
| `template_kind`, `template_id`, `template_hash` | Default/custom selection; optional DB identity; SHA256 of brief-template text, NUL separator, detailed-template text. The prompt version and effective prompt hashes identify built-in templates |
| `generated_at` | Server completion/receipt time in UTC |
| `entry_point` | Worker automatic, manual API, manual stream or browser-local save |
| `execution` | Server execution or explicitly client-reported local execution |
| `stages` | Accepted brief and detailed requests, with requested model, model-source label and prompt/request fingerprints |

Server request fingerprints hash the exact credential-free JSON body passed to the HTTP client **after** native-provider conversion and token-budget retry. Prompt fingerprints hash the provider message JSON, including native system blocks and selected image bytes when present. Failed attempts are not output lineage. A detailed-only vision fallback records a vision brief and text detailed stage separately. The model ID is the requested configuration, not an assertion about a provider's underlying weights or model alias.

Browser request fingerprints hash the exact serialized provider JSON in the browser after a complete accepted result. The server receives only two hashes, a bounded model ID and summary results. Local prompt fingerprints hash the prepared user prompt, including brief context in the detailed stage. They intentionally have a different scope from server message-JSON fingerprints. Local model, execution and request hashes are client-reported; the server cannot attest which remote model the browser called. API keys, URLs and provider configuration are not saved with the result.

The existing browser `summary_version` remains a compare-and-swap hash of the previous brief and detailed output. It is unrelated to `policy_version`. `generation_binding` separately binds content/title, policy version/hash, prompt version, template ID/text and prepared prompts. Stale bindings require a refresh and new generation; old open clients cannot silently label old-policy output as current.

Results and metadata commit in the same lease-protected SQL update. Content, title, prior browser output and current template text are checked. A selected template row is held `FOR SHARE` until commit to serialize concurrent edits. Content/title/reset/repair updates clear stale metadata; untraced legacy writes explicitly clear it. Saved-article backups preserve nullable metadata through normal, clip and child restore paths. Existing share snapshots remain historical snapshots.

## Run an evaluation with the site's configured model

The DB-free evaluator and frozen semantic fixtures are described in [Summary quality evaluation](summary-quality-evaluation.md). For a deployed environment, compile both commands from the reviewed commit and invoke `summary-eval-site` in the established API-container environment:

```sh
summary-eval-site --inspect --source-runes 500
summary-eval-site --binary /tmp/summary-eval --source-runes 500 -- \
  --live --grade --max-calls 40 --timeout 15m --out /tmp/new-evaluation.json
```

Choose a source length matching the evaluated inputs' routing bucket. Frozen excerpts are short inputs; testing them through the short route does not validate long-article context selection. The wrapper resolves the protected admin configuration using a read-only database session, checks that its revision did not change during resolution, and hands only the selected provider credentials and network environment to the evaluator. It does not run migrations, write usage/article rows or start background processing. `--inspect` makes no model call. The current wrapper supports the evaluator's OpenAI-compatible protocol; unsupported native protocols fail before calls.

Build from a clean checkout outside an enclosing repository and verify the binary with `go version -m` before use. Some Go toolchains stamp a nested linked worktree with its enclosing repository revision; reject a mismatched revision or dirty build.

Keep source commit, executable SHA256, route-selection metadata, model outputs, completion evidence and human semantic review together. A graded pass is limited to the tested fixtures/model/settings; it does not close an unchanged historical article's cached-summary defect. Do not bulk recompute old summaries as part of this workflow.
