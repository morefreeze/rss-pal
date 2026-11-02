# Summary faithfulness evaluation

`backend/cmd/summary-eval` freezes model outputs and optional semantic judgments in a private JSON artifact. It does not connect to the article database, enqueue generation, overwrite historical summaries, or add a second model to the serving path. Ordinary Go tests use in-process fake transports and never contact a model or source website.

## Frozen inputs and acceptance

The embedded, versioned suite is `backend/internal/summaryeval/testdata/fixtures.v1.json`. Version `summary-fidelity-2026-11-02-v3` has three development examples and six independently worded held-out variants, two per failure class. The seven controls comprise three faithful paraphrases and four known-wrong summaries. Version 3 adds a separate assigned-label-as-verified-truth negative control and strengthens the assigned-class rubric; version 2 had restored the unattended-execution condition in the 018 positive control. Every input explicitly identifies an excerpt or an authored variant; none represents a full-article evaluation. Evidence references are relative to the private QA root, `/Users/bytedance/.codex/qa/rss-pal/`. Only small public source excerpts are committed; authentication data, account state and screenshots are not copied.

| Class | Canonical evidence | Required distinction |
| --- | --- | --- |
| 017, metric attribution | `2026-11-01/heartbeat-1503/articles/weekly414-original.json`, `data.result.text`, 言论/2; companion review | Axel's **book-sales** income was enough to live on in 2024 and zero in 2026. The source gives no six-figure amount. Crawler visits producing no advertising income is a separate statement; it does not prove crawlers uniquely caused the book-sales decline. |
| 018, nested termination | Same QA directory, `loops-original.json`, 主动式循环 and its worked example; companion review | Events or schedules trigger work. Each goal-bounded task/current batch exits, while the enclosing routine remains active until the user switches it off. “Stop when done” alone loses a material boundary. |
| 019, excluded domain | `2026-11-02/heartbeat-0908/articles/human-data-original.json`, AUM text and `data.result.math`; companion review | `z_y - max_{i != y} z_i`, averaged across epochs. Assigned `y` need not win and is not necessarily the verified true class: the source explicitly describes a bird annotated as dog. Calling that assigned class “correct” falsely upgrades an annotation into verified truth. The maximum of the other classes is then the global maximum, so “global second-highest” is not a valid general replacement. |

The authored variants change names, units, language, workflow/error branches and numerical examples. They are transfer checks, not quotations or claims about real people. The checked-in split is a development convention, not a permanently blind benchmark. Add genuinely unseen cases and bump the suite version when policy development has used the held-outs.

Rubrics compare meaning: entities, metric/time binding, uncertainty, stopping scope, exclusion sets and aggregation. Faithful paraphrases are accepted; there is no exact expected-summary string. Length, Han-character and English-word counts are descriptive and never a quality gate, including English-source/Chinese-summary ratios.

The grader sees the source, candidate brief **and** detailed output, and per-criterion requirements/prohibitions. The generator sees only normal article inputs and the chosen production/template prompts. Calibration controls and rubrics are never supplied to the generator. A correct detailed paragraph cannot cancel a contradictory brief.

## Offline checks

From `backend`:

```sh
go test ./internal/summaryeval ./cmd/summary-eval
go test -race ./internal/summaryeval ./cmd/summary-eval
go vet ./internal/summaryeval ./cmd/summary-eval
go run ./cmd/summary-eval --validate
```

`--validate` checks schema, evidence references within excerpts, split coverage and positive/negative control structure. Its exit code 0 is a schema result; the message explicitly says semantic evaluation was not run. Unit tests cover fail-closed grading JSON, evidence quotes, controls that catch lenient or harsh judgments, transport/retry budgets, completion metadata, all six server API dispatches and secret redaction. These checks do not establish real-model quality.

## Explicit live generation

Supply `AI_API_KEY`, `AI_BASE_URL` and `AI_MODEL` through the process environment. The URL must be an OpenAI-compatible base URL with no userinfo, query or fragment. HTTPS is required except for loopback HTTP. No `.env` file is loaded implicitly and no API-key command-line flag exists. If using existing `CLAUDE_*` deployment settings, the operator must deliberately map the already resolved provider target into the `AI_*` variables in memory. Do not print or save those values.

The requested model is pinned with article routing disabled. Production's database-admin routing must be resolved separately before invoking this CLI; environment defaults alone may not be the serving target. Record which routing target and source-size category were selected. These are short source excerpts, so their results do not validate long-article model selection or full-article context retention. The wire model reported by the provider is recorded separately from the requested model; an omitted provider model remains unknown.

Create a private output directory, then run from `backend` with a **new** filename:

```sh
go run ./cmd/summary-eval --live --max-calls 24 \
  --out /private/evaluation-output/default-ungraded.json
```

This generates all nine cases with the default production summarizer. Two successful stages per case require 18 requests before retries. The result remains `ungraded` and exits 2; it is a frozen candidate for independent review, not a passing semantic run.

To enable model grading as well:

```sh
go run ./cmd/summary-eval --live --grade --max-calls 40 \
  --out /private/evaluation-output/default-graded.json
```

A retry-free run uses seven calibration calls plus nine times two generation stages and one grading call: **34 requests**. `--max-calls` must be explicitly set from 1 to 256 and is shared by generation, retries, control grading, candidate grading and any vision fallback. Once exhausted, additional underlying transport calls are denied. Failed attempts consume the cap. Grader requests have no automatic retry. The total deadline defaults to 15 minutes; `--timeout` can change it.

The optional grader defaults to the generator provider/model. Set `SUMMARY_EVAL_JUDGE_MODEL`, `SUMMARY_EVAL_JUDGE_BASE_URL` and `SUMMARY_EVAL_JUDGE_API_KEY` for a separate grader. A different endpoint requires its own explicit key. `--model`, `--base-url`, `--judge-model` and `--judge-base-url` override the non-secret configuration. This tool currently supports OpenAI-compatible JSON/SSE responses, not the native Anthropic protocol.

Useful selections:

```sh
# All held-outs; calibration still includes the seven fixed development controls.
go run ./cmd/summary-eval --live --grade --split held_out --max-calls 30 \
  --out /private/evaluation-output/held-out.json

# One canonical case across four text APIs; 7 + 4 * 3 = 19 calls before retries.
go run ./cmd/summary-eval --live --grade --cases 018 \
  --routes default,stream,custom,custom-stream --max-calls 24 \
  --out /private/evaluation-output/text-routes.json
```

`--brief-template-file` and `--detailed-template-file` accept UTF-8 custom templates with `{title}` and `{content}` placeholders, at most 64 KiB each. Default custom templates are neutral summary requests; they contain no rubric. `--routes vision,vision-stream` requires `--images` with 1–4 local source JPEG paths, each at most 8 MiB. Images are not downloaded; only their hashes/sizes are saved. Keep the supplied files unchanged during a run. Meaningful multimodal acceptance requires source-associated images and a rubric that covers their content; an API dispatch test alone cannot establish that. The nine bundled fixtures are text cases and do not include unrelated stand-in images.

## Grading and durable evidence

Before generating graded candidates, the grader must classify three faithful paraphrase controls as passing and four known-wrong controls as failing the specified criteria. Expected control labels are withheld from its prompt. A failure or uncertainty in calibration stops the experiment; it cannot certify candidate outputs.

Grader version `semantic-evidence-v3` explicitly separates assigned/reference labels, predicted winners and verified ground truth, judges their meaning in context, and checks both summary stages for contradictions. It requests verbatim copying of the supplied `criterion.source_evidence` spans, permitting a shorter exact substring while prohibiting LaTeX rendering or rewriting. Each judgment must contain exactly one verdict per criterion, a literal source quote, a literal output quote when passing, and an explanation of the semantic comparison. Duplicate/unknown JSON keys, fabricated quotations, missing criteria, invalid verdicts and malformed JSON are rejected. An omission can fail with an empty output quote and an explanation. A verdict of `unknown` is never accepted as a pass. Calibration is a sanity check, not proof of an infallible judge; independent human review remains required even for `graded_pass`, particularly if generator and grader are the same model.

The final attempt for each effective prompt must have an explicit `finish_reason=stop`. A provider that omits the reason cannot produce a completed evaluation, even though the serving client supports some older reason-less providers. `length`, filtering, interrupted SSE, invalid metadata, exhausted budgets and malformed grading fail closed. A successful replacement of a truncated request can recover that prompt; both attempts remain recorded. Vision fallback cannot hide an unresolved failed visual request.

| Status | Meaning | Exit |
| --- | --- | --- |
| `graded_pass` | All selected outputs completed, all controls calibrated, every criterion passed. Human review is still required. | 0 |
| `failed` | A semantic criterion failed/was unknown, or calibration was not decisive/correct. | 2 |
| `incomplete` | A generation/grading completion, budget, deadline, transport or parsing step was incomplete. No pass is asserted. | 2 |
| `ungraded` | Complete candidate outputs were saved without calibrated semantic grading. | 2 |
| Configuration/artifact error | No usable completed experiment can be reported. | 1 |

Artifacts include suite/policy/grader versions, frozen input and rubric snapshots, input/suite/template/request/message SHA-256 hashes, source scope/evidence reference, source/build revision and dirty state from Git or compiled Go VCS build settings, selected routes, outputs, descriptive counts, control and per-criterion verdicts, timestamps, requested/reported models, every request number, finish reasons and provider-reported token usage. If both Git and embedded VCS settings are unavailable, the revision remains `unknown` with dirty state true. Missing usage remains unknown. Cost is not inferred from token counts or an assumed price.

Both calibration and candidate results retain `judgment_text`: the actual model message text, including invalid grading JSON or invalid evidence quotes, before parsing. It is bounded by the 8 MiB response limit and excludes the provider envelope and headers. Safe `judgment_*` error categories distinguish invalid JSON, coverage, criterion values and source/output quotations; original exact-quote validation remains unchanged. This permits offline diagnosis and replay with `ParseJudgment` without additional model requests. Model text is untrusted and may require the same secret redaction as summary output.

Files are created exclusively with mode `0600`, synced, and never silently overwrite an older run. Configured secret values are redacted even if a provider echoes them into output or metadata. Endpoints, credentials, headers, request bodies and raw provider errors are excluded; errors are replaced before they can reach the summarizer's fallback logs. Request hashes identify effective prompts without persisting transport payloads. SIGINT/SIGTERM cancels the run and allows an incomplete artifact to be saved; a force-killed process or unwritable output path can still prevent final persistence.

Keep live artifacts in the private QA evidence directory, not in the repository. Review every live candidate against its frozen source and rubric, recording concrete evidence for each failed or uncertain condition. Report excerpt/held-out results separately from full-article UI acceptance. A passing new excerpt experiment does not repair or close an old cached-summary issue: that historical output needs its own authorized regeneration and persisted UI verification. This workflow performs no historical bulk recomputation and makes no runtime second-model judgment.
