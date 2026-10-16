# Reliable player lookups and comparisons

Approved specification: [#22](https://github.com/jtoosh/baseball-mcp/issues/22).
Implementation tickets: [#23](https://github.com/jtoosh/baseball-mcp/issues/23)
and [#24](https://github.com/jtoosh/baseball-mcp/issues/24).

## Problem Statement

The question `Compare Jose Ramirez and junior caminero` can return only Junior
Caminero and incorrectly imply that José Ramírez has no statistics, despite both
players being present. The lookup depends on the model guessing accented source
spellings, and incomplete comparisons pass through as successful queries.

## Solution

Recognize full player names regardless of accents or capitalization, resolve
them to database identities, and check that direct comparisons retrieve every
requested player-season before generating an answer.

## User Stories

1. As a user, I want unaccented player names recognized, so that I can type naturally.
2. As a user, I want mixed capitalization and extra whitespace accepted, so that formatting does not change the answer.
3. As a user, I want equivalent Unicode spellings recognized, so that pasted names work.
4. As a user, I want every requested player included in a comparison, so that the answer is complete.
5. As a user, I want original player spellings displayed, so that names remain accurate.
6. As a user, I want distinct players with the same name kept separate, so that their statistics are not combined.
7. As a user, I want missing seasons described precisely, so that a missing record is not mistaken for an absent player.
8. As a user, I want the same matching locally and in the hosted app, so that results are consistent.

## Implementation Decisions

- Build a read-only in-memory player catalog when opening the season snapshot.
- Normalize searchable full names and question text with Unicode NFD, removal of combining accent marks, case folding, and collapsed whitespace. Match at word boundaries.
- Preserve source names for display. Do not change the snapshot schema or data.
- Supply all matched canonical names and private player IDs to initial planning and corrections; use IDs in player lookup filters.
- Keep distinct IDs when normalized names collide and preserve existing local clarification and hosted interpretation behavior.
- For direct player comparisons, verify coverage of resolved players and requested seasons before answer generation. Do not apply this requirement to rankings or intentional filtered subsets.
- Reuse the existing single correction attempt for omitted available player-seasons. Stop with a helpful failure if correction still omits them. Explain genuinely missing seasons accurately.
- Keep SQL and IDs private, preserving existing deadlines, spending controls, and independent prompting strategies.

## Testing Decisions

- Agreed primary seam: the existing question HTTP endpoint, using the real imported snapshot and controlled responses at the external model-provider boundary.
- Assert user-visible answers, retrieved values, safe failures, and the result payload sent to the provider. Do not test normalization helpers directly.
- Follow existing player-lookup, controlled-provider, correction-limit, and ambiguity workflow tests.
- Cover the exact reported question, accents, combining accents, mixed case, whitespace, name boundaries, duplicate names, omitted results, genuinely missing player-seasons, and local/hosted interpretation modes.
- Tests establish deterministic application behavior; live model accuracy is a separate concern.

## Out of Scope

Typo correction, nicknames, surname-only searches, fuzzy matching, new UI flows,
live data, changes to the database schema, and deployment.

## Further Notes

The method is documented separately in the player name matching guide. The
investigation reproduced the retrieval failure with controlled SQL; the
original failed model-generated SQL was not retained.
