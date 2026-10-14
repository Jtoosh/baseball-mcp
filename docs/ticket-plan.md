# Published tracer-bullet tickets

Parent spec: https://github.com/Jtoosh/baseball-mcp/issues/1

Published with the ready-for-agent label and verified native GitHub blocking relationships. The numbered sections identify plan slices; each links to its actual GitHub issue. Every slice includes public HTTP verification and its relevant browser behavior.

## 1. Answer a player-season OPS question from the browser

**GitHub issue:** [#2](https://github.com/Jtoosh/baseball-mcp/issues/2).

**Blocked by:** None (can start immediately).

**What it delivers:** Import the supplied snapshot and let a user ask an explicit player-season OPS question, receive a grounded natural-language answer, and toggle a readable supporting table.

### Acceptance criteria

- [ ] Build the Go startup, HTTP, question-workflow, model-call, database-access, import, and browser modules around one complete request path; keep orchestration in the reusable question workflow.
- [ ] Provide a separate manual import into player and player-season tables. Retain all 49 exported fields in their appropriate identity or season record, source IDs, all 1,879 player-seasons and 857 players, and exported metric precision. Check uniqueness and numeric validity; do not collapse the two Max Muncy identities.
- [ ] Load administrator JSON configuration and a server-side API key; use the OpenAI Go SDK with the configured model and the schema-plus-instructions strategy.
- [ ] Accept an explicit, unambiguous player-season question through the browser, generate SQL with the schema, execute it, and send actual results to answer generation.
- [ ] Demonstrate the supplied Luis Vázquez 2025 lookup: exported OPS 0.448 over 53 plate appearances. Nonqualifying player-seasons remain accessible for direct lookups, with plate appearances disclosed.
- [ ] Open serving SQLite connections read-only. Validate a single read query over permitted baseball data, allow read-only CTEs, reject writes/multiple statements/administrative access, and impose the ten-second execution deadline from the first executable slice.
- [ ] Apply the 500-row model-input/table cap from the first slice. Until the partial-answer slice lands, oversized results may return a plain request to narrow the question rather than silently truncate.
- [ ] Present only natural-language answers and friendly baseball-labelled tables with a visibility switch. Do not expose SQL, schema, raw errors, logs, secrets, or database artifacts through responses or static routes.
- [ ] Return plain-language errors for failures; if answer generation fails, return no table. Record errors in private developer logs.
- [ ] Verify the path through public HTTP with real imported SQLite and controlled model responses; include focused browser checks and documented local import/configuration/startup instructions.

## 2. Answer MLB-wide leader and top-ten overlap questions

**GitHub issue:** [#3](https://github.com/Jtoosh/baseball-mcp/issues/3).

**Blocked by:** [#2](https://github.com/Jtoosh/baseball-mcp/issues/2).

**What it delivers:** Answer ranking, multi-season, and advanced-metric questions using the imported snapshot, including qualification, dense-rank ties, and top-ten intersections.

### Acceptance criteria

- [ ] Use all imported metrics, with friendly labels and preserved exported rate values. Use stored singles in this export and support derivation from hit counts when a compatible future import lacks singles.
- [ ] Treat the league as MLB combined and apply at least 502 plate appearances before rankings. Continue to allow nonqualifying direct lookups with plate appearances.
- [ ] Default an omitted season to the latest available imported season and disclose the choice without confirmation solely for that default.
- [ ] Support leader questions across supplied seasons. Verify snapshot HR leaders: Aaron Judge 58 in 2024; Cal Raleigh 60 in 2025; Kyle Schwarber and Pete Crow-Armstrong tied at 45 in 2026.
- [ ] Verify the exported 2026 qualifying OPS leader is Yordan Alvarez at 1.035 over 691 plate appearances; do not recalculate a different rate for ranking.
- [ ] Use dense rank rather than row numbering or gap-producing rank. Include all players tied at tenth; the 2024 HR dense-rank tenth place includes Pete Alonso and Cal Raleigh at 34.
- [ ] Interpret 1B and 2B as singles and doubles. Compute a top-ten overlap as the intersection of all requested rankings for one season, not a union.
- [ ] Treat zero matching players as a normal answer; the qualifying HR/singles/doubles intersection is empty in all three supplied seasons.
- [ ] Add public HTTP reference tests and browser examples covering these behaviors, including a high-stat nonqualifying player excluded before ranking and an advanced-metric lookup.

## 3. Confirm ambiguous questions and explain unsupported requests

**GitHub issue:** [#4](https://github.com/Jtoosh/baseball-mcp/issues/4).

**Blocked by:** [#2](https://github.com/Jtoosh/baseball-mcp/issues/2).

**What it delivers:** Let a user confirm or revise a proposed interpretation before querying, distinguish same-name players, and receive an LLM-generated explanation for unsupported questions.

### Acceptance criteria

- [ ] Return a natural-language best-guess interpretation for unresolved meanings such as 'best hitter', and allow confirmation or revision in the browser.
- [ ] Execute no generated SQL for an ambiguous question until its interpretation has been confirmed.
- [ ] Handle ambiguous Max Muncy lookups without silently choosing between source player IDs 571970 and 691777.
- [ ] Allow explicit questions to execute immediately. Missing seasons still use the disclosed latest available season without a confirmation step solely for that default.
- [ ] Retain only pending clarification context; a new or completed question does not create general conversation memory.
- [ ] Send out-of-scope questions and schema context to the LLM for a user-facing explanation, without an unsupported query or schema disclosure. Cover pitching, absent seasons, and unavailable team/league splits.
- [ ] Expose the resolved question intent through the existing question workflow so later comparison mode can share one confirmed interpretation.
- [ ] Verify request, clarification, confirmation, revision, unsupported, and independent-question outcomes through public HTTP and a focused browser interaction.

## 4. Correct a failed query once and preserve clear failure outcomes

**GitHub issue:** [#5](https://github.com/Jtoosh/baseball-mcp/issues/5).

**Blocked by:** [#2](https://github.com/Jtoosh/baseball-mcp/issues/2).

**What it delivers:** Recover a failed or rejected generated query once automatically, then give a plain-language error without leaking internals or presenting an ungenerated answer.

### Acceptance criteria

- [ ] A SQL validation or execution failure triggers at most one model-assisted correction for that strategy; provide correction context privately.
- [ ] Revalidate every corrected query and enforce one read statement per attempt, read-only database access, and a ten-second deadline on each execution.
- [ ] After a second failure, stop and return a natural-language error. Verify no additional SQL attempt is made.
- [ ] Ensure timeout/cancellation and rejected writes or multi-statement queries cannot mutate stored data; record diagnostic details privately.
- [ ] On answer-generation failure after successful SQL, suppress the result table and return only a natural-language error; do not fabricate a statistical answer.
- [ ] Make each strategy's success/error outcome and correction budget independent, preparing the existing workflow for comparison without adding a public testing interface.
- [ ] Add public HTTP tests for corrected success, exhausted correction, validation failures, timeouts, model failures, and private error handling; verify readable errors in the browser.

## 5. Answer partial results within the 500-row cap

**GitHub issue:** [#6](https://github.com/Jtoosh/baseball-mcp/issues/6).

**Blocked by:** [#2](https://github.com/Jtoosh/baseball-mcp/issues/2).

**What it delivers:** Answer an oversized question from its bounded returned portion, disclose that the result is partial, and ask the user to narrow the question.

### Acceptance criteria

- [ ] Detect overflow while returning at most 500 rows to answer generation and to the optional browser table; do not pass an overflow-detection row to either.
- [ ] Include truncation context in answer generation and public presentation, so the answer explicitly concerns the returned portion and asks for a narrower question.
- [ ] Return a partial answer rather than automatically refusing every oversized request. If its answer generation fails, still return only a natural-language error without a table.
- [ ] Apply the limit to query-result rows rather than the database rows considered by counts, aggregations, or rankings; a qualifying count must still consider all stored relevant records.
- [ ] Handle exactly 500 rows without a false overflow notice, and preserve no-match queries as normal results.
- [ ] Verify row boundaries, model inputs, aggregate correctness, partial answer disclosure, and table toggle behavior through public HTTP and a focused browser check.

## 6. Compare two prompting strategies for the same question

**GitHub issue:** [#7](https://github.com/Jtoosh/baseball-mcp/issues/7).

**Blocked by:** [#4](https://github.com/Jtoosh/baseball-mcp/issues/4), [#5](https://github.com/Jtoosh/baseball-mcp/issues/5).

**What it delivers:** Use admin configuration to run the default strategy or automatically compare schema-and-instructions prompting with prompting that adds worked examples, showing independent labelled results.

### Acceptance criteria

- [ ] Support the two agreed prompts: schema plus instructions, and the same prompt plus worked question/SQL examples. Include common domain rules in both; keep evaluation questions out of worked examples.
- [ ] Select the default strategy and optional comparison mode in administrator JSON configuration; the browser has no public admin configuration interface.
- [ ] Resolve any ambiguous user intent once before the strategy attempts and run both against the same confirmed question, model, and dataset.
- [ ] Run one query per strategy attempt with one independent correction allowance per strategy; do not make one strategy execute the other's SQL as its own result.
- [ ] Display separately labelled answers and optional friendly tables. Apply existing privacy, table visibility, row-cap, and answer-generation-failure behavior independently to each result.
- [ ] If one strategy fails, preserve the other's successful answer and optional table beside a plain-language error for the failed strategy.
- [ ] Verify configuration selection, shared confirmation, two independent executions, isolated correction budgets, full success and partial failure through public HTTP and browser comparison checks.

## 7. Evaluate the complete browser workflow against reference answers

**GitHub issue:** [#8](https://github.com/Jtoosh/baseball-mcp/issues/8).

**Blocked by:** [#3](https://github.com/Jtoosh/baseball-mcp/issues/3), [#6](https://github.com/Jtoosh/baseball-mcp/issues/6), [#7](https://github.com/Jtoosh/baseball-mcp/issues/7).

**What it delivers:** Run reproducible controlled checks through the finished request path and deliver the local demo and assignment evidence.

### Acceptance criteria

- [ ] Keep logs inaccessible through public responses and static routes; verify privacy across answer, clarification, unsupported, partial, comparison, and error outcomes.
- [ ] Exercise the twelve independently checked reference cases through the public workflow and evaluate retrieved values separately from answer faithfulness, allowing equivalent SQL and wording.
- [ ] Use controlled model responses and real SQLite for ordinary automated tests.
- [ ] Include additional checks for exactly 500 rows, partial results, dense-rank/rate ties, clarification, correction exhaustion, answer-generation failure, and independent comparison outcomes.
- [ ] Perform final browser verification for question submission, confirmation/revision, default-season disclosure, friendly statistics, optional tables, comparison labels, and error presentation.
- [ ] Provide the database description and schema picture, reproducible import/startup/configuration guidance, successful/unsuccessful evaluation examples where available, at least six additional examples, and prompting-comparison observations from any live evaluation actually performed. Do not claim unrun live model accuracy.
- [ ] Keep MCP implementation, Vercel deployment, live refresh, and hosted log persistence outside this delivery; leave the question workflow reusable for their later extensions.

