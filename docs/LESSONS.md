# devctl lesson index

The structured source of truth is `knowledge/lessons.yaml`. This file is a quick index for a new session.

## LESSON-0001 — Healthy long-running tools can exceed a fixed timeout

Area: process execution.

A fixed wall-clock timeout terminated a healthy Gradle workload. The correction was per-check hard and inactivity timeouts, streamed output tracking, and complete process-tree cleanup.

## LESSON-0002 — Local verification can pass with required ignored files

Area: repository reproducibility.

A required source file existed locally but was excluded by `.gitignore`. Local verification passed, while clean CI failed because the file was absent from the repository.

The current deterministic safeguard is clean CI verification. A future repository-state check may classify suspicious ignored or untracked source-like files, but should not treat every ignored file as a defect.

## Lesson workflow

```text
failure
→ root cause
→ why existing checks missed it
→ deterministic rule or review question
→ regression coverage
→ structured lesson
```

Do not record ordinary coding mistakes unless they reveal a reusable weakness in the engineering or verification system.
