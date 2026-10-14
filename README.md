# MLB batting questions

Ask baseball questions in plain English and get answers backed by MLB batting
statistics. The app supports player lookups, season leaders, top-ten overlaps,
and advanced metrics, with an optional table of supporting results.

## Use the hosted app

Open [baseball.jamesteuscher.click](https://baseball.jamesteuscher.click).
Access is invitation-only: ask the site owner for an invitation and approval,
then sign in with the verification code sent to your email.

Ask a question, confirm the interpretation if prompted, and read the answer.
Use **Show supporting results** to reveal the result table.

Try these questions:

- “Who led MLB in home runs in 2024, 2025, and 2026?”
- “What was Luis Vázquez's OPS in 2025?”
- “Which qualifying players share tenth place by dense rank for home runs in 2024?”
- “Is any qualifying player in the top ten for HR, 1B, and 2B in 2026?”
- “What was Alec Bohm's average best speed in 2024?”

Hosted access allows 20 new questions per user per rolling hour and one active
request at a time. A shared monthly model-spending allowance can pause questions.

## Data and answer rules

The supplied snapshot covers MLB regular-season batting in **2024–2026**:
1,879 player-season records for 857 players, with all 49 exported fields retained.
It includes players with at least ten plate appearances and combines each
player's performance across teams. Updates are manual; this is a fixed snapshot.

- Questions without a season default to **2026**, the latest imported season.
- Rankings use players with **at least 502 PA**; direct lookups include others.
- Rankings use **dense rank**: ties share a rank, and the next distinct value gets
  the next rank. Top-ten lists include all players tied at rank ten.
- Top-ten overlaps require a player to appear in every requested ranking in the
  same season. In batting questions, **1B means singles** and **2B means doubles**.
- Rates use exported values without recalculation. No matches is a normal answer.
- Results above 500 rows produce a partial answer and a request to narrow the
  question. Aggregates and rankings still consider all relevant records.

See the [statistics glossary](docs/statistics.md) for metric definitions and the
[data profile](docs/data-profile.md) for dataset details and reference results.

## How it works

A Go server uses OpenAI to generate a read query, runs it against a read-only
SQLite snapshot, then asks the model to explain the results. SQL and database
schema details stay on the server. Queries are restricted to baseball data and
have a ten-second execution deadline.

Automated tests check database results and application behavior with controlled
model responses; live model accuracy is assessed separately. MCP support is
planned as a later extension.

Further reading:

- [Design and prompting strategies](docs/design.md)
- [Evaluation and verification](docs/evaluation.md)
- [Hosted setup and operation](docs/hosting/setup.md)
- [Managing invited access](docs/hosting/access.md)

## Appendix: run locally

Requires **Go 1.26.6 or later** and an OpenAI API key. SQLite is bundled;
no external database server or CGO compiler is needed.

```sh
go mod download
go run ./cmd/import -csv advanced-mlb-24-26.csv -db mlb.db
cp config.example.json config.json
```

Set `model` in `config.json` to your OpenAI model identifier and provide
`OPENAI_API_KEY` in the server's environment. Then start the app:

```sh
go run . -config config.json
```

Open <http://127.0.0.1:8080>. Configuration changes require a restart.
`database` selects the snapshot; `listen` defaults to localhost.
Set `default_strategy` to `schema` or `examples`; `comparison_mode: true` runs both.
Local configuration and developer logs are private and ignored by Git.

To verify local changes:

```sh
go test ./...
go test -race ./...
go vet ./...
```

For hosted integration tests, set `TEST_DATABASE_URL` to a disposable PostgreSQL
database and `REQUIRE_HOSTED_TESTS=1` to require them rather than skip them.
