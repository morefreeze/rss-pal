# Versioned summary evaluation corpora

[summary-eval-registry.v1.json](summary-eval-registry.v1.json) binds reusable case IDs to frozen source, rubric and control hashes. It publishes metadata and provenance notes. Full private sources, FAQ text, cached summaries, raw web captures, reviewer quotations and credentials remain in the operator's private QA directory. Nothing in this delivery regenerates historical summaries or changes serving prompts, budgets, models or configuration.

The comparison baseline is mainline `6e3f5372691f583128d91e3e1c6b8223034a82f5`, policy `faithfulness-2026-11-02-v2`, prompt `summary-prompts-2026-11-02-v1`. The recorded configuration revision 3 selects ZAI `glm-5.3-flash` for the small role and `glm-5.3` for the large role. These are two configured size roles; provider catalog capabilities do not imply active configuration. Resolve the current configuration read-only before a later model run. Experimental candidate v7, editor profiles and their prompts are not this baseline.

## Corpus boundaries

| Registry ID | Executable via `--suite-file` | Frozen scope |
| --- | --- | --- |
| `builtin-9-v3` | Yes; also the unchanged default without flags | `summary-fidelity-2026-11-02-v3`, 9 cases / 7 controls. The embedded JSON bytes and all original title/content pairs are unchanged. |
| `private-transfer-17-v3` | Yes, with explicit private path | `summary-fidelity-transfer-proposal-2026-11-04-v3`, 17 cases / 17 controls. Five classes: metric attribution, nested termination, excluded domain, conditional cardinality and claim status. Each class has two distinct development-heldout source inputs and positive/negative controls. |
| `private-full-source-7-v1` | **No** | Seven additional exact stored full-source inputs with separate private quote/criteria sidecars. This is a manifest for manual full-source regression, not a `summaryeval.Suite`. The loader does not convert or execute it. |

The original eight development holdouts were exposed during development. The later claim-status variants use already known material, and the FAQ topic and cached omission were known. No case here is advertised as a blind unseen benchmark. Constructed positive/negative control pairs are expected labels for calibration, not newly generated model results or executed semantic verdicts. The FAQ input contains its full captured body under the existing `frozen_excerpt` schema enum; that does not make the other excerpts full articles.

The seven manual full-source IDs are `full-3346`, `full-3352`, `full-3360`, `full-3401`, `full-3420`, `full-3460`, and `full-3464`. Their exact stored UTF-8 title/content strings, source URLs, combined input hashes and rubric hashes are recorded in the registry. Source captures and full text remain private. The full-source manifest SHA-256 is `8dca54322a0e254a574e156bc037bebeecc8d3a2280b96a78351cd9516622a75`; its reviewer-sidecar file SHA-256 is `c01bc00ebe0a947348125235fea8c79f08a054072a0c3d0b26b2e21fa51717f6`.

Keep the AUM source boundary explicit: full article 3420 defines its margin/logits at **epoch t**. The old 930-rune 019 excerpt shows a mean indexed by `t=1..T` without naming that index unit. Do not transplant the full article's epoch definition into the excerpt, or delete the explicit epoch definition when reviewing the full article. The built-in nine-case fixture and its historical criterion ID `epoch_average` remain unchanged; a rubric name or historical expected wording does not add evidence to the excerpt. The private 17-case corpus contains the independently frozen source-grounded correction.

## Reproducible private-suite commands

From `backend`, set the private root and executable path. These variables contain file paths, never keys. `R` is the private QA root used for this evaluation; the example reflects the freeze location. The file must remain byte-identical.

```sh
R=/Users/bytedance/.codex/qa/rss-pal/2026-11-04/complete-todos
EVAL=/absolute/path/to/clean-build/summary-eval
SUITE="$R/task2/quality-first-transfer-suite-proposal-v3.json"
SUITE_SHA=03b9d11cddf47e31067063aa3cdd6bfa44fd6bfc27b34da8e9c444f54b44dd7a

"$EVAL" --validate --suite-file "$SUITE"
"$EVAL" --validate --suite-file "$SUITE" --suite-sha256 "$SUITE_SHA"
```

Both commands are offline and print schema counts, `suite_file_sha256` and `suite_sha256`. The canonical typed suite hash must be `07e186f1238e92da91495e91ce9f99e4403135e50838cf11652568b8a1723806`. File SHA-256 binds exact original bytes; `Suite.Hash` hashes the validated typed JSON. They are intentionally distinct. Validation is not semantic acceptance.

External suite files use the existing schema and limits: 2 MiB total, depth 64, 256 KiB per case content. Unknown, repeated, case-aliased or non-ASCII schema keys; invalid UTF-8; lone UTF-16 surrogate escapes; trailing JSON; missing evidence; incomplete class coverage; duplicate held-out bodies; and variants with copied content or a parent outside the same training class are rejected. Independence comparisons ignore whitespace only; input hashes and source strings remain exact. Valid surrogate pairs and literal U+FFFD characters are accepted without normalization.

The file must be regular and nonempty. Each path component is opened relative to a pinned directory descriptor with no symlink following; final FIFO/device/directory reads are rejected and FIFOs cannot block the loader. Paths containing `.`/`..`, duplicate separators or trailing aliases are rejected. Use the actual file path, including `/private/tmp` rather than a `/tmp` symlink on macOS. These evaluation CLI file operations support the project's macOS/Linux environments.

`--suite-sha256` requires `--suite-file`; an explicitly empty value is invalid. Every external `--live` run requires the expected file checksum. File/checksum/schema failures occur before provider credentials are read, an artifact is created, a request is made or an attempt is counted. An offline validation can omit the checksum to prepare the guarded command. Omitting both new flags preserves the embedded default.

Build a clean evaluator outside nested worktrees and inspect `go version -m "$EVAL"`. Set `REV` to the exact reviewed evaluator commit, with embedded `vcs.modified=false`, then use its optional guard:

```sh
REV=REPLACE_WITH_REVIEWED_40_HEX_EVALUATOR_COMMIT
"$EVAL" --live --suite-file "$SUITE" --suite-sha256 "$SUITE_SHA" \
  --expected-revision "$REV" --model glm-5.3-flash --max-calls 48 \
  --out "$R/task2/NEW-small-default-ungraded.json"
"$EVAL" --live --suite-file "$SUITE" --suite-sha256 "$SUITE_SHA" \
  --expected-revision "$REV" --model glm-5.3 --max-calls 48 \
  --out "$R/task2/NEW-large-default-ungraded.json"
```

Supply the matching provider key/base URL through process memory using the [existing credential procedure](summary-quality-evaluation.md#explicit-live-generation); never paste or save keys in these commands. The model ID is pinned and implicit article routing is disabled in the evaluator. A large-model run on a short excerpt is still a short-source model comparison, not a test of long-source routing. The read-only `summary-eval-site --source-runes N --binary "$EVAL" -- ...` wrapper can resolve each configured size role and forwards the suite/revision flags, but it is a separate DB-reading operation; the DB-free loader never queries it. The wrapper refuses unsupported native protocols. This delivery performs no provider/DB operations.

The revision guard requires exactly 40 hex characters (uppercase accepted), a matching revision in the **executing binary's** Go BuildInfo and an explicit clean setting. Missing metadata, dirty/unknown source or a mismatch reject before key use or artifacts. Ambient Git cannot satisfy this guard. Without the flag, legacy provenance fallback remains compatible and may identify ambient Git when BuildInfo is absent; that is not binary identity proof.

## Generation entrances and physical request caps

Run both size-role models across the applicable entrances. Model role and generation entrance are separate dimensions.

| Entrance | CLI routes / separate boundary | Source and evidence requirements |
| --- | --- | --- |
| Default server text | `default`, `stream` | Exact suite title/content; normal production text prompts. |
| Custom server text | `custom`, `custom-stream` | Exact suite title/content plus explicitly frozen UTF-8 template files. Record both template hashes. |
| Server vision | `vision`, `vision-stream` | Explicit source-associated local JPEGs via `--images`; record image hashes/sizes. The text suite has no bundled visual truth. Dispatch tests cannot establish visual faithfulness. |
| Browser-local default/custom | Separate browser/API acceptance; no CLI `local` route | `GET .../summary/input` prepares prompts; the browser calls its selected provider and `PUT .../summary/result` commits a bound result. Verify exact source and template, client-request hashes, content/summary binding and persisted output separately. The CLI does not exercise browser key handling, browser provider requests or persistence. |

For each new artifact, `--max-calls` is a hard physical request cap of 1–256, shared by generation, all length retries, vision fallback, calibration and candidate judging. Failed attempts count. Each successful generation normally needs two calls; retries and fallback can add calls. Budgets below are retry-free arithmetic, not promised consumption:

| Selection from private 17 suite | Ungraded minimum | With all 17 controls and one candidate judgment per result |
| --- | --- | --- |
| All 17 cases, one route | 34 | 68 |
| All 10 development-heldouts, one route | 20 | 47 |
| One case, all six server routes | 12 | 35 |
| All 17 cases, four text routes | 136 | 221 |
| All 17 cases, all six routes | 204 | 323, above the cap: split into bounded new artifacts |

Examples for one model; repeat with the other configured role, always using a new artifact filename:

```sh
"$EVAL" --live --suite-file "$SUITE" --suite-sha256 "$SUITE_SHA" \
  --expected-revision "$REV" --model glm-5.3-flash \
  --routes default,stream,custom,custom-stream --max-calls 180 \
  --brief-template-file /private/frozen-brief.txt \
  --detailed-template-file /private/frozen-detailed.txt \
  --out "$R/task2/NEW-small-text-entrances-ungraded.json"

# Choose a case and actual source images with an independent visual rubric.
"$EVAL" --live --suite-file "$SUITE" --suite-sha256 "$SUITE_SHA" \
  --expected-revision "$REV" --model glm-5.3 --cases 018 \
  --routes vision,vision-stream --images /private/source-image.jpg \
  --max-calls 12 --out "$R/task2/NEW-large-vision-ungraded.json"
```

The images example is an invocation shape, not a claim that case 018 has a frozen visual rubric. Do not attach unrelated images and describe that as multimodal quality acceptance. The seven full-source inputs require their own explicitly authorized manual generation/review harness; `--suite-file` on their manifest fails schema validation. They are not included in the table's executable case count.

## Evidence and remaining grader limitations

Artifacts record the selected corpus version, canonical suite hash and exact external file hash, current policy/prompt/grader versions, executing-source provenance, requested and provider-reported models, source input hashes, template and request/message hashes, routes, completion metadata, physical request counts and descriptive lengths. They contain private input/rubric/output snapshots; keep artifacts private. Older artifacts without a prompt/file hash stay readable and must not be retroactively assigned those values.

No grader prompt, segmentation or calibration requirement changes in this delivery. `semantic-evidence-v3` still requests source quotation text from the model and checks it literally; known real calibration runs stopped when an otherwise positive 018 judgment retyped source wording/punctuation. The old prompt grades brief and detailed together, has one output quote per criterion and is weaker than a field-segmented conditional/claim-status protocol. The private corpus encodes same-field conditions in its rubrics, but loading them does not prove the old grader enforces them reliably. Known failures remain failures or incomplete results; no qualification is claimed for this grader on the new five-class suite.

Use real-model **ungraded** outputs as frozen candidates for independent, whole-output source review. Review brief and detailed separately for selected claims, then their consistency; retain literal source/output evidence and distinguish instruction, proposal, hypothesis, reported delivery and verified result. Automatic `--grade` remains opt-in: all controls must decisively calibrate first, malformed/unknown evidence cannot pass, and calibration failure stops candidate generation. Passing mock tests, schema validation, a higher cap or a successful wire dispatch never establishes semantic quality or production acceptance. Even `graded_pass` requires independent review. Compare new results against the current deployed v2 baseline, preserving historical artifacts and avoiding bulk history recomputation.

Before adding or replacing a corpus, freeze source/rubric/control data privately, assign a new version and registry entry, verify exact title/content/rubric/control hashes, and document exposure and source scope. Do not rewrite an existing frozen entry to relabel a failed experiment. Default unit tests verify the builtin registry against actual embedded bytes and bind the original nine input hashes to the private-17 registry; private-file comparisons must also be performed with the operator's immutable files.
