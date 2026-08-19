# Stage 7F-C - Deterministic cross-project knowledge search

Stage 7F-C adds retrieval above the Stage 7F-B Knowledge Vault. It searches
authoritative project-local and global lesson files directly. It does not use
embeddings, an AI service, MCP, VS Code integration or semantic inference.

## Search boundary

```text
query and explicit filters
        |
        v
project-local authoritative lessons + global authoritative lessons
        |
        v
metadata matching and deterministic score
        |
        v
bounded SearchResult candidates with lifecycle and provenance
```

Search identifies previously stored candidate knowledge. It does not claim
that a matching lesson applies to the current defect, and it never changes a
lesson status or verification result.

## Frozen command surface

```text
devctl knowledge search [flags] [query]
devctl knowledge show [--global] [<root>] <lesson-id-or-display-id>
```

`knowledge search` uses the current directory as the project root by default.
`--project-root` selects the local project and `--global-root` selects the
global Knowledge Vault root. The query can be plain text or empty when only
explicit filters are supplied.

Supported filters are:

```text
--check       --failure       --technology    --version
--platform    --tag           --adapter      --path
--symptom     --limit         --include-history
```

The existing `lessons query` command remains the older 7E advisory interface.
It is not used by 7F-C search.

## Trust and lifecycle rules

The default search includes only the current `VERIFIED` revision from each
machine ID. `CANDIDATE`, `REQUIRES_REVIEW`, `SUPERSEDED` and `REJECTED` are not
silently presented as reusable knowledge. `--include-history` is an explicit
request to inspect all stored revisions, and every result retains its status.

Search never upgrades a result. A `VERIFIED` result is historical evidence of
the source lesson's review state, not proof that it solves the current failure.
Current deterministic verification remains authoritative over every result.

## Matching and ordering

Text is lowercased and split into deterministic alphanumeric tokens. Matching
uses stored fields only: title, statement, problem, root cause, correction,
technologies, versions, platform, tags, adapters, check IDs, failure IDs,
normalized errors, affected paths and symptoms.

Explicit identifier filters use normalized exact matching for check IDs,
failure IDs, technologies, versions, platforms, tags and adapters. Symptoms
and affected paths use bounded substring matching because those fields describe
observed text and locations. Text matches add fixed field weights, and a text
query must match independently even when a metadata filter matches. Identical
inputs always use the same ordering: score, trusted lifecycle rank,
project-local before global, machine ID and revision.
No current repository state is inferred from similarity.

## Bounded result and provenance

Search returns at most 20 results and defaults to 10. JSON uses a response
envelope with `total`, `returned`, `truncated` and `results`, so byte or limit
truncation is visible to the caller. The exact indented JSON written by the
CLI, including its final newline, is capped at 16 KiB. Text, collections and
version maps are bounded before the final size check.

Each result contains the machine ID, display ID, scope, revision, lifecycle
status, score, match reasons, a bounded title/statement, compatibility fields,
limitations and source Fix Record or source Lesson IDs. For project-local
results, known Fix Record evidence references may be returned as bounded paths;
raw evidence contents are never returned. Global results retain their source
lesson and Fix Record linkage without copying raw project evidence into the
search response.

## Acceptance evidence

The permanent tests cover project-local plus global search, fixed ordering,
metadata filters, default exclusion of rejected lessons, explicit history,
display-ID lookup, bounded serialization and source provenance without raw
output.

Embeddings and AI ranking are deliberately outside the 7F-C boundary.
