## Problem Statement

People exploring MLB batting statistics should be able to ask questions in ordinary language without knowing SQL, the database schema, or how to join player records to season statistics. They need answers grounded in the supplied season snapshots, meaningful ranking and qualification rules, and a clear explanation when a question is ambiguous, unsupported, incomplete, or unsuccessful.

The project also needs a reproducible way to compare prompting strategies and inspect their queries privately for a class assignment, while keeping database implementation details out of the general-audience browser interface.

## Solution

Build a local Go application with a browser interface and a read-only SQLite database populated from the supplied 2024–2026 MLB batting CSV. Import every available metric. Use the OpenAI Go SDK to interpret questions, generate a permitted SQL query using the schema, and turn retrieved data into a natural-language answer.

Present the answer alongside an optional, clearly labelled result table. Ask for confirmation before executing SQL when the question requires a meaningful interpretation. Keep SQL, schema details, raw errors, and developer records out of browser responses. Allow administrators to configure the model, prompting strategy, and optional comparison mode through JSON configuration.

## User Stories

1. As a baseball fan, I want to ask a batting-statistics question in ordinary language, so that I can explore the data without writing SQL.
2. As a baseball fan, I want an answer grounded in the imported season snapshots, so that I know which data supports the response.
3. As a baseball fan, I want to ask about any available batting metric, so that I can explore both traditional and advanced statistics.
4. As a baseball fan, I want to compare statistics across 2024, 2025, and 2026, so that I can understand changes between seasons.
5. As a baseball fan, I want a missing season to default to the latest available season, so that common questions remain convenient.
6. As a baseball fan, I want the selected season disclosed, so that I do not mistake a default for an explicit request.
7. As a baseball fan, I want "the league" to mean MLB combined, so that leaders are selected consistently across the dataset.
8. As a baseball fan, I want player-season totals combined across teams, so that a team change does not split a player's season performance.
9. As a baseball fan, I want rankings to consider only qualifying player-seasons, so that leader comparisons apply the agreed eligibility rule.
10. As a baseball fan, I want ranking eligibility to require at least 502 plate appearances, so that the qualification rule is predictable.
11. As a baseball fan, I want direct statistical lookups to include nonqualifying player-seasons, so that I can ask about players with limited playing time.
12. As a baseball fan, I want plate appearances included by default in those lookups, so that I can understand the amount of playing time behind a statistic.
13. As a baseball fan, I want rankings to use dense rank, so that equal values share a rank without gaps between distinct values.
14. As a baseball fan, I want all players tied at tenth included, so that tied performances are not arbitrarily excluded.
15. As a baseball fan, I want a top-ten overlap question to find players in every requested ranking for the same season, so that I can identify players strong in multiple statistics.
16. As a baseball fan, I want 1B and 2B understood as singles and doubles in batting questions, so that those requests do not become fielding-position questions.
17. As a baseball fan, I want exported rate-statistic values preserved, so that displayed values and ranking ties agree with the supplied data.
18. As a baseball fan, I want explicitly stated questions answered directly, so that clear requests do not require unnecessary confirmation.
19. As a baseball fan, I want the application to propose a best-guess interpretation of an ambiguous question, so that I can clarify my intent without starting over.
20. As a baseball fan, I want to confirm or revise that interpretation before SQL executes, so that an assumption does not silently determine my answer.
21. As a baseball fan, I want same-name players distinguished before a lookup, so that I receive statistics for the intended person.
22. As a baseball fan, I want questions to be independent, so that an earlier request does not silently alter a later answer.
23. As a baseball fan, I want an explanation when a question is outside the available scope, so that I know how to revise it.
24. As a baseball fan, I want no matching records treated as a normal result, so that an empty intersection is not presented as a failure.
25. As a baseball fan, I want a readable natural-language answer, so that I can understand the result without database expertise.
26. As a baseball fan, I want an optional result table with baseball terminology, so that I can inspect supporting values.
27. As a baseball fan, I want to hide or show the table with a switch, so that I can choose my preferred amount of detail.
28. As a baseball fan, I want no SQL or database schema details in the interface, so that the experience stays focused on baseball.
29. As a baseball fan, I want oversized results clearly identified as partial, so that I do not assume the answer covers all matches.
30. As a baseball fan, I want a partial answer accompanied by a request to narrow my question, so that I can obtain a more complete result.
31. As a baseball fan, I want an unsuccessful query corrected once automatically, so that a recoverable generation error need not end my request.
32. As a baseball fan, I want a plain-language error after the correction fails, so that I understand the outcome without seeing internal errors.
33. As a baseball fan, I want a plain-language error without a table if answer generation fails, so that the application follows a consistent presentation contract.
34. As a baseball fan, I want query execution bounded by a timeout, so that an expensive query does not run indefinitely.
35. As an administrator, I want to select the model through JSON configuration, so that I can change models without changing application code.
36. As an administrator, I want to select a default prompting strategy, so that normal requests use the intended strategy.
37. As an administrator, I want an optional comparison mode, so that each question can be evaluated with both strategies automatically.
38. As a comparison user, I want separately labelled answers and optional tables, so that I can compare strategy outcomes without seeing SQL.
39. As a comparison user, I want one strategy's successful answer preserved when the other fails, so that a partial comparison failure does not discard useful output.
40. As a developer, I want private error logs, so that I can diagnose failures without exposing internal information to end users.
41. As a developer, I want optional private evaluation records containing the question, strategy, SQL, results, and final answer, so that I can inspect accuracy and prepare assignment examples.
42. As a student, I want to compare schema-and-instructions prompting with prompting that adds worked examples, so that I can report how the strategies affect results.
43. As a student, I want independently checked reference answers separate from prompt examples, so that evaluation measures generalization rather than memorization.
44. As a dataset maintainer, I want a separate manual import process, so that I can update fixed season snapshots without giving the question workflow write access.
45. As a dataset maintainer, I want player IDs and player-season uniqueness preserved, so that repeated names or imports do not corrupt identity.
46. As a dataset maintainer, I want every exported metric retained with its source precision, so that importing the data does not narrow the supported statistics.
47. As a developer, I want the question workflow separate from HTTP handling, so that a later MCP adapter can reuse the behavior.

## Implementation Decisions

- **Composition:** Use Go with a startup module, a thin HTTP module, a question-workflow module, a model-call module, a database-access module, a separate importer, and a static browser interface. Keep interpretation, confirmation, generation, execution, correction, and answer generation in the question-workflow module. The HTTP module translates public requests and results; it does not independently orchestrate model calls.
- **Database:** Use SQLite. The player table stores the source player ID and name. The statistics table references the player and uses player ID plus season as its unique player-season key. Store player age and all exported batting metrics as season-dependent values. Retain qualifying and nonqualifying player-seasons; do not add team or league subdivisions absent from the export.
- **Import:** Populate the database from the corrected export with 1,879 player-seasons, 857 players, and 49 source columns. Use source player IDs rather than names for identity. Check duplicate keys, numeric parsing, and source consistency. Import outside the running question workflow. Preserve integer counts and exported rate precision. Singles are present in this export; if a future compatible export omits them, derive singles from hits minus doubles, triples, and home runs when those counts exist.
- **Dataset scope:** Treat the data as fixed season snapshots, normally updated manually once per year. Disclose seasons and ground claims in the imported dataset. The current export has a minimum of ten plate appearances in each season, so it does not contain every MLB player-season. Source retrieval dates and filters are supplementary metadata, not facts to invent from the filename.
- **Domain rules:** "The league" means MLB combined. Rankings apply the fixed 502-plate-appearance threshold before ranking. Other statistical lookups include nonqualifying player-seasons and disclose plate appearances by default. Use dense rank over exported statistic values; include all players with ranks through ten in top-ten queries. A top-ten overlap is the intersection of the requested rankings within one season.
- **Default season:** Determine the latest available season from the imported data, rather than the current calendar year. Use it when the question omits a season and disclose it without requiring confirmation solely for that default. The supplied data defaults to 2026.
- **Public interface:** Accept a question and return a typed answer, clarification, unsupported-scope explanation, or natural-language error. Answer outcomes carry the selected season or seasons, labelled strategy outcomes, an optional presentation table, and partial-result disclosure when relevant. A clarification can be confirmed or revised through the same public workflow. Exact route names and transport types are implementation details.
- **Question independence:** Retain only the context necessary to complete a pending clarification. Do not infer general follow-up context from previous completed questions. A fresh question starts a fresh workflow.
- **Ambiguity:** Propose a natural-language best guess when an important meaning is unresolved, and await confirmation before executing any generated SQL for that question. Ambiguous player identity also requires clarification; the supplied data contains two distinct players named Max Muncy. Explicit questions can proceed immediately. In comparison mode, resolve shared user intent before running the strategy attempts rather than comparing answers to conflicting interpretations.
- **Model integration:** Use the OpenAI Go SDK behind the model-call module. The administrator supplies the model identifier through JSON configuration. Keep model and dataset constant across both strategies and initially use the same model for SQL and answer generation. Load the API key on the server from an environment variable and keep it out of browser content and evaluation records.
- **Prompting:** Support schema plus instructions, and the same prompt plus worked question/SQL examples. Both strategies receive the domain rules. Keep held-out evaluation questions separate from worked prompt examples. Normal mode uses the configured default; comparison mode automatically runs both strategies with separately labelled outcomes.
- **Execution:** Validate generated SQL before executing it. Permit one read statement over the baseball data per strategy attempt, including read-only CTEs. Reject multiple statements, writes, schema-management and database-administration operations, and queries outside the permitted data. Also open SQLite read-only; a prompt alone does not provide the execution restriction.
- **Correction budget:** A validation or execution failure can trigger one model-assisted correction for that strategy. Revalidate the corrected query. After a second failure, return a natural-language error for that strategy and stop. Each execution has a ten-second deadline. Comparison mode gives each strategy its own attempt and correction budget.
- **Result limit:** Supply at most 500 result rows to answer generation and display at most 500 rows. Detect whether additional rows exist without including the overflow row in the model input or browser table. When truncated, generate an answer explicitly limited to the returned portion and ask the user to narrow the question. This limits query-result rows, not the database rows a count, aggregate, or ranking may consider.
- **Grounded answers:** Send retrieved results, the question and resolved interpretation, season context, and truncation status to answer generation. Preserve empty results as a normal outcome. The model should explain an unsupported question using the question and schema, without executing an unsupported query or exposing schema details in that explanation.
- **Failure behavior:** If answer generation fails, return a natural-language error and no result table for that strategy, even if its query succeeded. If only one comparison strategy fails, retain the other strategy's successful answer and optional table. Never replace a failure with a guessed statistical answer.
- **Presentation:** Return only approved user-facing information to the browser. Tables use baseball labels and readable formats, rather than raw schema column names, database identities, or SQL aliases as the presentation contract. A switch controls table visibility. SQL, schema descriptions, raw model payloads, raw database errors, and private records are not public response fields or static assets; hiding them with CSS is insufficient.
- **Private diagnostics:** Record errors in a developer log unavailable through browser routes. Support optional evaluation records containing question, strategy, generated SQL, results, and final answer. These records are developer artifacts and follow the same server-only access rule as logs.
- **Configuration:** Use a local administrator-controlled JSON file for the selected model, default strategy, comparison mode, and diagnostic/evaluation settings. Keep the ten-second execution limit and 500-row result cap as first-version acceptance requirements. Configuration is applied at startup; changes can use a restart rather than a live administration interface.
- **Extension shape:** Run locally for the first version. Keep the question workflow reusable by a future MCP adapter, while deferring MCP protocol implementation and hosted deployment operations.

## Testing Decisions

- **Confirmed primary seam:** Exercise the complete question workflow through the application's public HTTP interface. This is the highest available test seam: requests and confirmations enter through the same interface as the browser, and assertions inspect public answer, clarification, error, comparison, and table outcomes. Test the question-workflow, HTTP, database-access, and model-call modules through this interface rather than adding separate public interfaces for each module's tests.
- **Controlled dependencies:** Use real SQLite populated from a deterministic fixture and controlled model responses supplied through an internal model dependency or transport. This makes correction counts, timeouts, ambiguity, and model failures reproducible without requiring paid API calls in the ordinary test suite. Do not add production-only test endpoints or assert private helper invocation order.
- **Behavior assertions:** A good test checks returned values, qualification, ranking, confirmation behavior, limits, privacy, and independent strategy outcomes. Avoid asserting exact SQL text, internal types, helper structure, or the precise wording of a valid natural-language answer. Generated statements may differ while producing the same correct data.
- **Data preparation:** Verify the imported fixture preserves source player IDs, unique player-season records, all metric values, and counts of 617, 632, and 630 records across the three seasons. Source qualifying counts are 129, 145, and 135. Import verification is a fixture/artifact check supporting the HTTP suite, not a reason to expose another application testing interface.
- **Statistical references:** Use the twelve existing reference cases, whose core results were cross-checked using independent Decimal calculations and SQLite queries. Cover HR leaders by season, both 2026 HR co-leaders, exported OPS, top-ten overlaps, dense-rank ties, nonqualifying lookups with plate appearances, latest-season disclosure, same-name ambiguity, and normal empty results.
- **Ranking precision:** Test ties at dense rank ten and ties introduced by equal exported rate values. Test the intersection of all three requested rankings, rather than a union or three unrelated leader lists. Include a nonqualifying player with a high value to ensure the qualification filter is applied before ranking.
- **Interpretation:** Verify explicit questions can run immediately; ambiguous metric and player-name questions execute no generated SQL before confirmation. Verify confirmation or revision resumes the intended question, and completed questions do not supply hidden conversation context to new requests.
- **Execution controls:** Test single SELECT and read-only CTE acceptance; reject writes, multiple statements, metadata and administrative requests. Ensure the underlying database remains unchanged. Verify a failed or rejected query gets at most one correction, each corrected query is validated, and execution stops after the budget is exhausted. Check deadline cancellation through controlled timing rather than requiring every test to wait ten seconds.
- **Result boundaries:** Exercise exactly 500 rows and overflow beyond 500. Verify neither the model's answer input nor the public table receives more than 500 rows. Assert that overflow results remain answer outcomes with explicit partial-result disclosure and a request to narrow the question; aggregate answers still consider all relevant stored records.
- **Failure paths:** Cover query errors, exhausted correction, model failures, unsupported questions, and zero-row results. Verify that answer-generation failure suppresses its table, while one comparison failure preserves the other strategy's successful answer.
- **Privacy:** Inspect actual HTTP responses, including clarification, out-of-scope, partial, and error responses, for SQL and schema leakage. Verify logs, evaluation artifacts, secrets, and raw database files are unavailable through public routes. Confirm tables expose presentation labels rather than schema identifiers.
- **Browser verification:** Perform a focused browser check for question submission, clarification confirmation/revision, labelled comparison output, readable statistics, and the table visibility switch. This checks UI behavior alongside the primary HTTP suite rather than duplicating every workflow case in browser automation.
- **Live evaluation:** Run an explicit, optional evaluation against the configured OpenAI model with both strategies and a fixed dataset. Compare retrieved data and final wording separately against the reference answers, inspect private evaluation records, and report discrepancies. Passing deterministic tests alone is not proof of live model accuracy.
- **Prior art:** At initial specification, design documentation, the verified CSV and independently checked reference answers provided the testing foundation. The completed browser implementation now adds public HTTP tests, controlled model transports, real SQLite fixtures and browser asset checks; the hosting extension builds on these.

## Out of Scope

- Real MCP protocol compatibility, transport selection, and MCP SDK or manual protocol implementation.
- Hosting was deferred from this browser-first phase; the approved hosting extension now defines Vercel deployment, invited access, shared operational state and spending controls. Hosted evaluation histories remain excluded.
- Live MLB fetching, automatic refreshes, in-browser uploads, and data mutation through natural-language requests.
- Team-specific or AL/NL analysis not represented by the supplied export.
- Pitching, postseason statistics, or unsupported seasons and metrics; explain these requests instead of fabricating data.
- General multi-turn conversation memory beyond pending clarification.
- Public SQL views, schema explorers, public developer logs, or an in-browser administration interface.
- Comparing different models or datasets as part of the initial prompting-strategy comparison.

## Further Notes

- The browser-first phase is implemented. See the approved [hosting extension](hosting-spec.md) for deployment, access, operational state and budget decisions.

- This spec implements the agreed browser-first version even though the repository name refers to MCP. A later MCP adapter should reuse the question workflow.
- The corrected CSV's SHA-256 is `60de4620c5e90e112a1c1142cd1504af42e46ece8ba816a1d21ea530a6b318ca`. Reference values are authoritative for that supplied snapshot, not a claim about a live MLB feed.
- Snapshot HR leaders are Aaron Judge with 58 in 2024; Cal Raleigh with 60 in 2025; and Kyle Schwarber and Pete Crow-Armstrong tied at 45 in 2026. The qualifying 2026 OPS leader is Yordan Alvarez at 1.035 over 691 plate appearances. No qualifying player is in all three HR/singles/doubles top-ten dense-rank lists in any supplied season.
- Dense-rank tenth place in 2024 HR includes Pete Alonso and Cal Raleigh at 34 home runs. This provides a concrete tie fixture.
- The fixed 502-plate-appearance rule is the project's accepted qualification policy. Do not expand it into unagreed exception rules or season-to-date thresholds.
- Respect the accepted decisions to use read-only SQLite snapshots and keep SQL/schema out of browser output. The domain glossary is the vocabulary source for implementation and evaluation.
- Assignment evaluation should include the database description and schema picture, successful and unsuccessful examples where available, at least six additional examples, and observations comparing the two prompting strategies. The assignment's reading, Canvas submission, and classmate-review tasks remain the student's responsibility.
- The initial model choice belongs to the administrator. Do not invent a fixed model requirement in implementation.
