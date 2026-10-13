# Domain docs

## Layout and reading rules

This repo uses a single-context layout:

- `GLOSSARY.md` at the repo root defines domain terms.
- `docs/adr/` contains architectural decision records.

Before exploring the codebase, read the glossary and ADRs relevant
to the area being explored.

If these files do not exist, proceed silently. The domain-modeling
skill creates them lazily when terms or decisions are resolved.

## Vocabulary

Use glossary terms in issue titles, proposals, hypotheses, and test
names. Follow any documented preferences over synonyms.

If a needed concept is absent, reconsider invented terminology or
note the gap for domain-modeling.

## ADR conflicts

Explicitly identify proposals that contradict an existing ADR,
including the ADR reference and reason for reopening the decision.
