# Baseball question application

Working design decisions from the project interview. The browser-first application is implemented; the approved hosting extension is specified below.

## Scope and data

- Go application with a browser interface first; real MCP compatibility is a later extension.
- SQLite database built from fixed MLB regular-season batting snapshots for 2024–2026, updated manually outside the question workflow.
- Player table with ID and name; statistics table with one combined record per player and season. Team-specific analysis is outside the dataset's scope.
- Retain both qualifying and nonqualifying player-seasons. Rankings use `PA >= 502`; other queries include plate appearances in their output by default.
- "The league" means MLB combined. `1B` means singles and `2B` means doubles.
- Rankings use dense rank, including everyone tied at rank ten. Top-ten overlap is the intersection of rankings for the same season.
- Missing seasons default to the latest season in the dataset, disclosed to the user.
- Preserve MLB's exported rate-statistic values and use those values for ranking. If singles are absent, derive them as hits minus doubles, triples, and home runs.
- Import every available exported metric, including traditional batting statistics, advanced metrics, and player age for each season. Preserve the two-table design, with player identity in the player table and season-dependent values in the statistics table.

## Question workflow

- Questions are independent; retain context only for pending interpretation confirmation.
- Ambiguous interpretations require confirmation before SQL execution. Explicit questions can execute immediately.
- Generate SQL using an LLM supplied with the database schema, run the permitted read query, and send its results to the LLM for a natural-language answer.
- Validate generated SQL before execution: permit a single read query over the baseball data, including read-only CTEs; reject multiple statements, writes, and database-administration queries. Validation failures can use the single correction attempt.
- Allow one SQL correction attempt after a failure, then return a natural-language error.
- Allow one read statement per strategy attempt, with at most one correction attempt per strategy. Comparison mode executes strategies independently.
- Set a ten-second timeout per database execution.
- Limit both displayed tables and results supplied to answer generation to 500 rows. Detect overflow, mark the answer as based on partial results, and ask the user to narrow the question. This limit concerns returned rows, not how many database rows an aggregate can consider.
- If answer generation fails, return only a natural-language error; do not send the result table.
- Empty results are a normal outcome, not an error.
- Send out-of-scope questions and the schema to the LLM so it can explain why the question is unsupported; keep the explanation free of schema details.

## Presentation

- Present a natural-language answer and a table formatted for a general audience, with a switch to hide the table.
- Never expose generated SQL or database schema details in browser output, including errors and comparison results.
- Save error messages in a developer log unavailable to end users.
- Comparison mode runs both prompting strategies automatically and presents separately labelled answers and optional tables.
- If one strategy fails, retain the other strategy's successful answer and optional table. The failed strategy presents only a natural-language error.

## Configuration and composition

- Use an admin JSON configuration file with a default prompting strategy and optional comparison mode.
- Use two strategies: schema plus instructions, and the same prompt plus worked question/SQL examples. Evaluation questions are separate from worked prompt examples.
- Use the OpenAI Go SDK inside `llm.go`. The admin chooses the model through configuration; initially hold the model constant across both strategies and SQL/answer generation.
- `main.go`: startup and composition.
- `handlers.go`: HTTP request and response handling.
- `query.go`: interpretation, confirmation, SQL generation, execution, correction, and answer generation.
- `llm.go`: model calls.
- `database.go`: database access.
- `static/index.html`: browser interface.
- Defer `mcp.go`; a later MCP adapter can call the same question workflow.
- Preserve local development and add hosted deployment under the approved [hosting specification](hosting-spec.md).

## Evaluation

Controlled regression tests use independently checked reference answers from the imported snapshots. They cover the initial questions, ties, qualification rules, missing seasons, and empty results.

`evaluation/reference-answers.json` contains twelve reference cases for the supplied CSV, including the initial HR/OPS questions, top-ten overlaps, ties, qualification rules, and ambiguous questions. Core statistical results were independently cross-checked using Python Decimal calculations and in-memory SQLite queries. These fixtures support the controlled regression suite.

## Verified dataset

The corrected export, `advanced-mlb-24-26.csv`, has unique player-season keys and stable numeric player IDs, but two distinct players share the name Max Muncy. Use IDs for identity and request clarification for ambiguous names. See `data-profile.md` for the inspected fields, coverage, and reference results.

The corrected CSV includes the traditional batting statistics needed for all initial examples, including singles and exported OPS. Import all available metrics, preserving their exported values. Source filter settings and retrieval dates should be recorded when available; the CSV itself does not contain them.

MCP protocol details and hosted deployment operations are deferred to their later extensions.

## Hosted extension

- Serve baseball.jamesteuscher.click on Vercel Hobby, with dashboard-managed Clerk invitations and email OTP. Validate access on the server for each question.
- Keep the immutable read-only SQLite batting snapshot; use free Neon PostgreSQL for shared confirmations, question admissions, leases and spending reservations.
- Allow twenty questions per user per rolling hour and one active question per user. Bound each hosted request to two minutes, retaining the existing ten-second SQL ceiling.
- Reserve every production model call against a $10 UTC-calendar-month allowance before contacting the provider. Unknown pricing or uncertain accounting fails closed; there is no budget override.
- Use protected, visibly labelled controlled previews with isolated state and no production model credentials. Successful main builds deploy automatically only after tests pass.
- Retain safe private error diagnostics and necessary operational state; disable hosted evaluation histories. Owner account provisioning and secret entry use secure mechanisms outside chat.
