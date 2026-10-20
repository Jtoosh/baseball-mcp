# José Ramírez omitted from player comparison

Investigated October 16, 2026. Reported question:
`Compare Jose Ramirez and junior caminero`. The user reports the same failure
locally and in the hosted app: Junior Caminero's statistics appear, while the
answer says José Ramírez's statistics are unavailable.

## Finding and confidence

An unaccented SQL name filter reproducibly excludes José Ramírez from the real
imported database and sends only Junior Caminero's row to answer generation.
Changing only that filter's spelling to the stored `Ramírez, José` returns both
players. This is the strongest explanation for the reported failure.

The original model-generated SQL and answer payload were not supplied. These
experiments control the planner response; they establish the retrieval failure
and missing safeguards, rather than proving what the live model generated in
the original requests. No live model calls were made, and no production code was
changed. Fix and post-fix verification are outside this investigation.

## Why it happens

1. The importer preserves the CSV name verbatim (`internal/data/data.go`,
   `Import`). The name is stored as `Ramírez, José`, with accents on both names.
   The schema defines ordinary `TEXT`, with no accent-insensitive collation.
2. The planner receives the question, schema, metric meanings, and seasons,
   but no general list of matching stored player names or IDs (`internal/app/llm.go`,
   `OpenAIModel.Plan`). It must generate name predicates without that grounding.
   The examples strategy even demonstrates an exact name equality filter.
   The existing private identity mechanism handles a single confirmed player;
   it does not resolve both names in this comparison. Hosted interpretation
   bypasses the local same-name clarification mechanism.
3. If the generated filter uses `Ramirez, Jose`, SQLite finds no match for
   José Ramírez. Junior Caminero's unaccented name still matches. Lowercasing,
   `LIKE`, and `COLLATE NOCASE` do not remove the accents.
4. This is a successful SQL execution. `execution.go`, `executeStrategy`,
   retries only on execution, measure-validation, or formatting errors. There
   is no check that a named-player comparison retrieved every requested player.
5. `OpenAIModel.Answer` receives only the returned table and is instructed to
   answer from that table. In this scenario it has no José Ramírez statistics
   to report and no independent lookup step to distinguish a failed name match
   from missing data. Public-text validation does not check this distinction.

Both local and hosted questions share this query/answer path. The hosted
snapshot is imported from the embedded CSV (`internal/app/snapshot.go`). Hosted diagnostics
do not retain successful SQL, prompts, tables, or answers, so the original
query cannot be recovered from those application logs.

## Evidence

Importing the committed CSV and querying by player identity returned:

| Player | Season | HR | OPS | PA |
| --- | ---: | ---: | ---: | ---: |
| Junior Caminero | 2024 | 6 | 0.723 | 177 |
| José Ramírez | 2024 | 39 | 0.872 | 682 |
| Junior Caminero | 2025 | 45 | 0.846 | 653 |
| José Ramírez | 2025 | 30 | 0.863 | 673 |
| Junior Caminero | 2026 | 42 | 0.884 | 702 |
| José Ramírez | 2026 | 12 | 0.688 | 544 |

José Ramírez is present and qualifying in every available season. Missing
records or qualification cannot explain his omission from the latest-season
comparison. The omitted year defaults to 2026 under the documented rules.

| Name predicate | Matching players |
| --- | ---: |
| `name = 'Ramirez, Jose'` | 0 |
| `name = 'Ramírez, José'` | 1 |
| `name = 'Ramirez, Jose' COLLATE NOCASE` | 0 |
| `lower(name) LIKE '%ramirez%'` | 0 |
| `name LIKE 'Ramírez, Jos%'` | 1 |

A temporary Go replay used the original question, production direct
interpretation, the real importer and QueryService, and a controlled planner
response. Its assertion required both players in the result sent to the answer
model. With an unaccented name filter it failed twice, returning only:

```text
[[Caminero, Junior 2026 42 0.884 702]]
```

Changing only the filter spelling to `Ramírez, José` passed twice, returning:

```text
[[Caminero, Junior 2026 42 0.884 702] [Ramírez, José 2026 12 0.688 544]]
```

These are captured inputs to answer generation, not live model responses.
The temporary replay was removed after diagnosis.

To reproduce the database evidence from the repository root:

```sh
go run ./cmd/import -csv data/advanced-mlb-24-26.csv -db /tmp/ramirez-investigation.db
sqlite3 -readonly -header -column /tmp/ramirez-investigation.db <<'SQL'
SELECT p.name,s.year,s.home_run,s.on_base_plus_slg,s.pa
FROM players p JOIN stats s ON p.player_id=s.player_id
WHERE p.player_id IN (608070,691406) ORDER BY s.year,p.name;
SELECT p.name,s.year,s.home_run,s.on_base_plus_slg,s.pa
FROM players p JOIN stats s ON p.player_id=s.player_id
WHERE p.name IN ('Ramirez, Jose','Caminero, Junior')
AND s.year=(SELECT MAX(year) FROM stats);
SELECT p.name,s.year,s.home_run,s.on_base_plus_slg,s.pa
FROM players p JOIN stats s ON p.player_id=s.player_id
WHERE p.name IN ('Ramírez, José','Caminero, Junior')
AND s.year=(SELECT MAX(year) FROM stats);
SQL
```

## Recommended correction

Resolve names against the actual player catalog using Unicode normalization,
case folding, and accent-insensitive matching. Supply all resolved private IDs
and canonical names to planning, and use IDs for lookup filters. Preserve source
names for display and handle multiple matching identities without silently
combining them. Add a completeness check for named-player comparisons so a
lookup failure is not presented as proof that statistics are absent.

Regression coverage should use the exact reported question, verify both
players' rows reach answer generation, and cover accented and unaccented inputs,
mixed casing, Unicode combining accents, multiple identities sharing a name,
and a genuinely absent player. Prompt instructions alone, `LOWER`, or `NOCASE`
would not establish reliable accent-insensitive player resolution.

To confirm the historical incident specifically, inspect the failed local SQL
if retained, or capture a new local request with `developer_sql: true` on a
loopback configuration. Inspect both the player predicate and retrieved rows.
Enabling that developer control does not expose SQL in the hosted app.
