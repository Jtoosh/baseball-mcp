# README appendix

## Data and answer rules

The supplied snapshot covers MLB regular-season batting in **2024–2026**:
1,879 player-season records for 857 players, with all 49 exported fields retained.
It includes players with at least ten plate appearances and combines each
player's performance across teams. Updates are manual; this is a fixed snapshot.

- Questions without a season default to **2026**, the latest imported season.
- Rankings use players with **at least 502 PA**; direct lookups include others.
- Rankings use **dense rank**: ties share a rank, and the next distinct value gets
  the next rank. Top-ten lists include all players tied at rank ten.
- Results above 500 rows produce a partial answer and a request to narrow the
  question.

See the [statistics glossary](./statistics.md) for metric definitions and the
[data profile](./data-profile.md) for dataset details and reference results.

## Run locally

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
