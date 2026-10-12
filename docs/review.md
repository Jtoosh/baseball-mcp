# Final implementation review

The review compared `095e830...64505f7` against the repository's agent/domain
instructions, glossary, ADRs, and approved implementation spec. Two parallel
reviewers reported the findings below; the privacy finding was independently
reported on both axes. All findings were addressed in one implementer branch.

## Standards

Two findings:

1. **High: SQL text breached the presentation ADR.** Public text filtering matched
   a literal space after SELECT and missed newline, tab, and comment separators.
   The shared guard now normalizes SQL comments and detects recognizable SQL
   syntax, SQL fences, schema identifiers and descriptions across model-generated
   answers, clarifications and scope explanations. HTTP regressions cover these
   forms, including DDL, constant/function SELECTs and qualified identifiers.
   Ordinary baseball instructions such as “Select a season” remain accepted.
2. **Low: duplicated failure logging and message.** The top-level failure path
   now uses the existing shared log function and public message constant.

## Spec

Three findings:

1. **High: public SQL/schema leakage.** This overlaps the Standards finding;
   the same guard and HTTP regressions address both reports. Failed answer
   validation returns an error without its supporting table. Recognizable syntax
   checks do not prove arbitrary model prose can never describe internal details;
   prompts and the public response contract also restrict that content.
2. **Medium: same-name clarification could not be revised.** Identity resolution
   now uses the revision. A newly named player can replace the original player;
   an age-only revision uses the pending candidates; an unresolved identity asks
   again. Player-choice panels now also provide revision controls. HTTP checks
   verify both Max Muncy age 23 and changing to Luis Vázquez.
3. **Medium: correction could change shared seasons.** Every attempt validates
   its selected seasons against server-owned resolved intent. A drifting corrected
   plan fails its strategy while the other comparison answer remains available.

## Verification

Each behavioral fix began with a failing public HTTP regression, using real
SQLite and controlled model responses. The complete suite, race detector,
`go vet ./...`, and `git diff --check` passed after the fixes. Native browser
checks use controlled model output and real SQLite; see `docs/evaluation.md` for
scope. No paid model calls or live-accuracy claims were made.
