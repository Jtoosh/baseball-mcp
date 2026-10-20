# Public-access implementation review

Review target: `9c225b5280f2a341f66c7504ca336074e91833d9...efcfbd3`, implementing spec #25 and tickets #26–#29. The final review fixes address #27's unchanged-local-workflow requirement and #28's independent hosted provider controls.

## Standards report

The independent standards review found no documented-standard violations. It read the repository instructions, glossary, ADRs 0001–0006, design and implementation documents. It identified two optional speculative-generality cleanups: admission accepted an unused question request, and the obsolete `publicFailure` error retained unreachable friendly-message and logging branches. Both have been removed. The actual admission error and its retry guidance remain intact.

## Spec report

The independent spec review found one P2 issue: the shared model constructor applied the hosted 2,048-output-token ceiling and default service tier to ordinary local and evaluation runs. Production startup now selects that policy explicitly, independently of retired spending accounting. Local and evaluation requests retain their previous provider defaults. Hosted plan, correction and answer calls remain bounded, with SDK retries disabled and no monetary ceiling. ADR 0005 clarifies the distinction. No other missing or incorrect hosted requirements were found by the review.

## Verification

- Red/green: a local question sent through the public HTTP handler to a captured provider first failed because it contained `max_output_tokens: 2048` and `service_tier: default`; it passed after the fix, verifying that both parameters are omitted.
- Hosted regression coverage now starts the actual production composition with a real PostgreSQL operational store and embedded SQLite snapshot. A captured SDK transport checks the pinned provider endpoint and inspects every planning, correction and answer request while both comparison strategies complete successfully. All retain the hosted output ceiling, tier and configured model; no paid request is sent.
- Full hosted gate passed against an isolated local PostgreSQL database: JavaScript syntax validation, `go vet ./...`, `go test -race ./...` with `REQUIRE_HOSTED_TESTS=1` (app package: 93.391 seconds), and the trimmed server build. Shell syntax checks and `git diff --check` also passed.
- The parent implementation run passed native Chrome fixture verification of the website fetch contract and retention of the displayed answer across HTTP 429, HTTP 503 and controlled provider outcomes. These were controlled HTTP fixtures, not a live cloud deployment or paid-provider verification.

Sources: the separate standards and spec reviewer reports produced for the target above (`baseball-public-standards-review.md` and `baseball-public-spec-review.md` in the implementation run's temporary evidence directory). This document records their findings and resolutions so the review survives temporary-worktree cleanup. No cloud deployment or live paid-provider check is claimed.
