# Stage 7F-B - Authoritative knowledge and explicit promotion

Stage 7F-B adds the reusable-knowledge layer above Stage 7F-A Fix Records. It
keeps the layer narrow. It does not add semantic search, ranking or
staleness-aware retrieval; those remain Stage 7F-C work.

## Authority boundary

```text
verified Fix Record
        |
        v
local lesson candidate
        |
        v
objective source validation + human review
        |
        v
project-local VERIFIED lesson
        |
        v
explicit promotion approval + sanitization
        |
        v
global VERIFIED lesson
```

The Fix Record remains the objective source. A draft can describe a useful
rule, but its text cannot choose `VERIFIED`. A draft with no verified Fix
Record source is stored as `REQUIRES_REVIEW`. Approval does not replace the
missing evidence.

## Authoritative storage

Project-local revisions are stored as separate normal files under:

```text
<project>/.devctl/knowledge/authoritative-lessons/<uuid>-r0001.json
```

Global revisions are stored under the selected global repository root:

```text
<global-root>/knowledge/authoritative-lessons/<uuid>-r0001.json
```

Each revision has a UUID machine ID, a human display ID such as
`LESSON-PROMOTION`, a lifecycle status, compatibility metadata, source Fix
Record IDs, creation/review times and a content hash. The UUID is the
identity. The display ID is not used as a global key.

Revision files are create-only. A correction or supersession appends another
revision with the previous content hash. Existing bytes are not rewritten.
Reads fail closed for malformed JSON, unexpected files, renamed files,
missing predecessor revisions, broken hash links or content tampering.

The generated indexes are only disposable summaries:

```text
<project>/.devctl/knowledge/lesson-index.json
<global-root>/knowledge/lesson-index.json
```

`knowledge rebuild` reads authoritative files directly. Deleting or
corrupting an index does not delete lessons and does not prevent a rebuild.

The older `.devctl/knowledge/lessons.json` and `lessons query|add` interface
remain for the earlier 7E context boundary. Those records are advisory
compatibility data. They are not 7F-B authoritative lessons and are not used
to create a `VERIFIED` lesson.

## Lifecycle

```text
CANDIDATE -> VERIFIED
      |          |
      v          v
REQUIRES_REVIEW SUPERSEDED
      |
      v
  REJECTED
```

The actual transition is recorded as a new revision. `Supersede` is explicit,
and `Correct` creates a linked candidate revision. A global promotion creates
a new global UUID with `source_lesson_id` pointing to the local lesson; it is
not an automatic copy triggered by an AI explanation.

## Promotion checks

Promotion requires all of the following:

1. the local current revision is `VERIFIED`;
2. a reviewer identity and an explicit approval are supplied;
3. every source Fix Record can still be read and is valid;
4. secret-like values, private paths and raw-log material are absent;
5. technology, version, platform, verification scope, validation date,
   applicability and limitations remain in the published lesson;
6. the global destination accepts a new create-only revision.

The sanitization boundary fails closed for private-key material, access-key
patterns, long secret assignments, unrestricted evidence paths and raw-log
markers. A global lesson therefore contains the reusable rule and its limits,
not a copied report or private repository path.

## Frozen command surface

```text
devctl knowledge candidate --input <draft.json> <project>
devctl knowledge review --id <uuid> --reviewer <name> [--approve] <project>
devctl knowledge supersede --id <uuid> --reviewer <name> <project>
devctl knowledge promote --id <uuid> --global-root <root> --reviewer <name> --approve <project>
devctl knowledge rebuild [--global] <root>
devctl knowledge list [--global] <root>
devctl knowledge show [--global] <root> <uuid>
```

These commands do not run verification. They read existing Fix Records and
authoritative lesson files only. `candidate`, `review`, `supersede` and
`promote` are explicit state-changing operations and write append-only files.
`Correct` is currently an internal API used by the revision tests and is not
part of the frozen CLI surface; its Fix Record rebinding rules are the same as
candidate creation.

## Acceptance evidence

The focused tests cover:

- a draft without objective Fix Record evidence becoming `REQUIRES_REVIEW`;
- a real verified Fix Record becoming a candidate and then a reviewed lesson;
- invalid lifecycle transitions into `SUPERSEDED` being rejected;
- corrections rebinding valid Fix Record project provenance and downgrading
  missing source evidence to `REQUIRES_REVIEW`;
- explicit approval being required for global promotion;
- version metadata and other publishable strings being covered by promotion
  sanitization;
- local knowledge surviving index deletion and deterministic rebuild;
- tampered authoritative bytes failing content-hash validation;
- correction/supersession revisions retaining the previous revision and hash.

Stage 7F-C still owns semantic retrieval, ranking, compatibility matching and
staleness-aware presentation. Focused, full, race, vet, build, diff and clean
isolated-snapshot acceptance pass for this local implementation. Hosted GitHub
Actions and publication remain outside this local implementation until
explicitly authorized.
