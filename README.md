# MLB batting questions

A local Go browser application that answers natural-language questions against
fixed MLB batting snapshots. OpenAI generates a read query; SQLite supplies the
results; a second model call explains them. SQL and database details stay on the
server. A reusable question workflow will support an MCP adapter in a later phase.

## Run locally

Requires Go 1.26 or later. SQLite is compiled into Go through `go-sqlite3`; no CGO
compiler or external database server is required.

```sh
go mod download
go run ./cmd/import -csv advanced-mlb-24-26.csv -db mlb.db
cp config.example.json config.json
```

Set `model` in `config.json` to the OpenAI model identifier you want to use. Set
`OPENAI_API_KEY` in the server's environment, using your normal secret-management
process. The key belongs on the server and is never entered in the browser.

```sh
go run . -config config.json
```

Open <http://127.0.0.1:8080> and ask, for example, “What was Luis Vázquez's OPS in
2025?” The supplied snapshot records OPS 0.448 over 53 plate appearances. The
“Show supporting results” switch hides or reveals the friendly result table.

Answers, clarification questions, and scope explanations render Markdown,
including headings, emphasis, lists, and tables. Links open in a new tab;
embedded HTML and images are excluded. The switch controls only database
result tables, so Markdown tables in the answer remain visible.

A small baseball spins during every request, including confirmation and revision.
It stays stationary when the browser requests reduced motion. Previous results
remain visible while loading. If the new request fails, the application labels
the previous answer and shows the new error separately. A successful comparison
strategy remains visible alongside a failed strategy.

Try the browser's example questions for MLB-wide leaders, dense-rank ties,
three-way top-ten overlap, and advanced metrics:

- “Who led MLB in home runs in 2024, 2025, and 2026?” returns Aaron Judge (58),
  Cal Raleigh (60), and both Kyle Schwarber and Pete Crow-Armstrong (45).
- “Who led MLB in OPS?” defaults to the latest imported season, 2026, and returns
  Yordan Alvarez at the exported 1.035 over 691 plate appearances.
- “Which qualifying players share tenth place by dense rank for home runs in
  2024?” includes Pete Alonso and Cal Raleigh, both with 34.
- “Is any qualifying player in the top ten for HR, 1B, and 2B in 2026?” has no
  matches. That is a normal answer: 1B means singles and 2B means doubles.
- “What was Alec Bohm's average best speed in 2024?” preserves 100.768220847.

Rankings filter to at least 502 plate appearances before computing dense ranks.
Top-ten results include every tie at tenth, and overlap questions require players
to appear in every requested ranking within the same season. Direct lookups also
include nonqualifying players and their plate appearances. All imported metrics
are available with baseball labels; exported rates are used without recalculation.

Configuration changes take effect on restart. `database` selects the snapshot;
`listen` defaults to localhost; `developer_log` defaults to the private
`.private/developer.log` file. Set `default_strategy` to `schema` (schema and instructions) or `examples`
(the same context plus worked question/SQL examples). Set `comparison_mode` to
`true` to run both automatically. Comparison resolves an ambiguous question once,
then shows independent labelled answers. A failure in one strategy preserves the
other result. The model and dataset are held constant.
Private logs and local configuration are ignored by Git and are never static
assets. Keep these artifacts outside any externally configured file server.

## Dataset and access rules

The supplied CSV contains 1,879 combined player-season records for 857 player IDs
across 2024–2026. All 49 exported fields are retained; source IDs distinguish the
two players named Max Muncy. Rate values use the exported precision, and advanced
metrics remain available alongside traditional statistics. Numeric fields are
stored as SQLite integer or real values without rounding on import; the original
CSV remains the source of exact decimal lexemes. The source only covers records
with at least ten plate appearances and is not a live MLB feed.

The importer checks numeric values, player-name consistency, headers, and unique
player-season keys before atomically replacing the snapshot. Updates are manual
and separate from the serving workflow. Compatible exports without a singles
column derive it from hits minus doubles, triples, and home runs. The server opens each query connection
read-only and permits one SELECT or read-only CTE over the baseball tables. SQLite
itself validates statement boundaries; an authorizer rejects metadata,
administrative operations, writes, and unapproved functions. Each execution has a
ten-second deadline. Result tables and model inputs never contain more than 500
rows. Oversized results produce a partial answer based on the first 500 matching
rows and ask the user to narrow the question. The overflow-detection row never
reaches the model or browser. Counts, aggregates, and rankings still consider all
relevant database records; the cap applies only to returned results. Exactly 500
rows and no-match results remain normal answers.

## Verify

```sh
go test ./...
go test -race ./...
go vet ./...
```

The suite sends public HTTP requests against a real imported SQLite database and
controlled model responses. A local OpenAI HTTP stub exercises the real Go SDK
without reading a key or making a paid call. It checks the verified OPS value,
qualification-independent lookup, imported coverage and precision, SQL restrictions,
read-only CTEs, deadlines, row caps, private routes, and failure presentation.
Ranking HTTP tests compare the held-out reference literals for season leaders,
OPS, dense-rank tenth-place ties, qualifying counts, and normal empty overlaps.
A controlled CSV fixture checks exclusion of a high-stat nonqualifying player
and equal exported-rate ties.
Live model accuracy is a separate optional evaluation, not implied by this suite.

Optional private JSONL evaluation records and the explicit live comparison runner
are documented in [evaluation and assignment evidence](docs/evaluation.md), with a
schema diagram, twelve-case reference harness, six additional demo examples, and
an accurate record of browser-verification limitations. Set `evaluation_file` to a
private path to enable recording; leave it absent to keep recording disabled.

## Invited hosted version

The approved hosting extension targets `baseball.jamesteuscher.click` on Vercel Hobby. Start with [hosted setup and operation](docs/hosting/setup.md); the owner-only credential wizard is `bash scripts/setup-hosting.sh`. Keep secrets out of chat and source control. Automatic deployments remain disabled until owner setup and smoke checks are complete.

Hosted integration tests use real PostgreSQL in addition to SQLite. Set `TEST_DATABASE_URL` to a disposable local PostgreSQL database and `REQUIRE_HOSTED_TESTS=1` to require these tests rather than skip them. CI supplies this fixture without production credentials or paid model calls.
