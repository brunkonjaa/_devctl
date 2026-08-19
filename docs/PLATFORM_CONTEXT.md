# `_devctl` platform context and knowledge boundary

`_devctl` is the authority for repository state, policy, deterministic
verification, evidence, cache validity, and bounded repair. An AI agent is an
optional proposer and reader of compact context. It cannot turn an AI opinion
into `PASS`, choose an unallowlisted command, approve a patch, or bypass
rollback.

## Commands for an agent or a developer

```text
devctl context --json [project]
devctl status --json [project]
devctl evidence rebuild --json <project>
devctl history --json <project>
devctl lessons query --json --check <check> <project>
devctl fixes record --input <candidate.json> <project>
devctl fixes list --json <project>
devctl fixes show --json <project> <fix-id>
devctl cache status --json <project>
devctl cache inspect --json <project>
```

`context --json` is the normal first query. It returns the detected project,
HEAD, branch, dirty state, latest indexed verification failures, evidence
paths, relevant lessons, suggested checks and cache validity. It is bounded
and does not include raw logs. Current repository evidence wins over a
historical lesson. Evidence is marked current only when the stored full
worktree/index fingerprint matches the current one; a dirty boolean alone is
not sufficient. A cache entry is only reusable when its stored fingerprint
still matches the project inputs.

## Knowledge vault

The older advisory lesson interface stores project-local entries at
`.devctl/knowledge/lessons.json`. A lesson can describe a generalized
successful solution or a failed approach, but it is not proof that one exact
fix closed one exact failure. It remains for compatibility with the 7E
context boundary and is not Stage 7F-B authority.

Stage 7F-B authoritative project lessons are create-only revisions under
`.devctl/knowledge/authoritative-lessons`; global lessons use
`knowledge/authoritative-lessons` under the selected global root. Each lesson
has a UUID machine ID, a separate display ID, lifecycle state, compatibility
metadata, source Fix Record IDs and a content hash. A generated
`.devctl/knowledge/lesson-index.json` or `knowledge/lesson-index.json` is only
a rebuildable summary. `devctl knowledge review` and `devctl knowledge
promote` are explicit review boundaries; AI text alone cannot create a
`VERIFIED` lesson.

Stage 7F-A Fix Records are separate append-only files under
`.devctl/knowledge/fix-records/<fix-id>.json`. `_devctl` creates one only after
the exact pre-fix and post-fix reports, target check transitions, project
identity, provenance and current repository fingerprint satisfy the closure
rule. Candidate text cannot choose `VERIFIED`. Corrections use a new record
with `supersedes`; old bytes remain unchanged. `list` and `show` are read-only.

The repository's `knowledge/lessons.yaml` remains a human-maintained reusable
lesson index and is not executable configuration. Stage 7F-A does not promote
Fix Records into either lesson store. Semantic search, ranking and
staleness-aware cross-project retrieval remain Stage 7F-C work.

## Evidence and cache storage

Verification writes its existing evidence tree under
`.devctl/evidence/<run>`. `devctl evidence rebuild` creates a small,
rebuildable `.devctl/evidence/index.json` containing run, check, status and
evidence-path references. It does not rewrite historical reports.

Cache entries are under `.devctl/cache/entries`. Each entry carries a schema,
kind, timestamp, payload and fingerprint. The fingerprint can include project
identity, Git HEAD, dirty state, relevant file hashes, configuration and
policy hashes, check version and `_devctl` version. `devctl cache clear` is an
explicit invalidation operation. Correctness never depends on the cache.

## Repair boundary

`devctl repair` is a thin terminal adapter over `internal/repair.Run`. The
engine owns the clean baseline, canonical patch bytes, hash binding, policy
allowlist, provenance, pre-apply revalidation, application, rollback and
deterministic re-verification. The CLI only renders progress, shows the
engine-supplied diff/evidence, reads explicit approval, and formats the result.

The current CLI accepts a controlled proposal file through `--proposal` for
tests and local experiments. It does not connect an external AI service. A
source allowlist is supplied with `--allow`; an absent provider or
non-interactive approval is a framework outcome. Progress and prompts use
stderr. `--json` emits one result on stdout.

## Daily flow

1. Open the repository in VS Code.
2. Run `devctl context --json .` and give the bounded result to the coding
   agent.
3. Let the agent inspect only the named files and current evidence references.
4. Run `devctl verify .` for fresh deterministic evidence.
5. After a material fix has exact pre/post evidence, close its project-local
   record with `devctl fixes record --input <candidate.json> <project>`.
6. Generalize a reusable rule separately; do not treat the Fix Record itself
   as an automatically accepted lesson.
7. Rebuild the evidence index when searching history.
8. Use the controlled repair command only with a clean baseline, explicit
   allowlist and interactive approval.

No cloud service or AI account is required for ordinary verification,
evidence, lessons, cache inspection or history lookup.
