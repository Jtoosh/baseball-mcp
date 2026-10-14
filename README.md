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

## Statistics glossary

Each batting metric in the database's `stats` table has a one-line summary below,
with links to the corresponding [MLB glossary](https://www.mlb.com/glossary) entries.
Definitions describe the metrics; the application preserves the exported values.
K% and BB% use batting plate appearances here, adapting MLB's pitcher-focused
entries. Percentage fields use percentage points (for example, `14.2` means
14.2%); AVG, SLG, OBP, OPS, BABIP, and the weighted/expected batting rates use
decimal values (for example, `0.280`).

MLB has no standalone glossary entries for xOBP, xISO, average bat speed, EV50,
adjusted exit velocity, whiff percentage, or swing percentage. Those rows link to
related glossary definitions and MLB's Baseball Savant or explanatory articles;
xISO applies the ISO definition to expected rates.

### Player and season fields

These three columns identify or describe a player-season rather than batting performance.

| Column | Field | One-line summary |
| --- | --- | --- |
| `player_id` | Player ID | Source identifier linking the player-season to its player in the `players` table. |
| `year` | Season | Year of the MLB regular season represented by the row. |
| `player_age` | [Player age](https://www.mlb.com/glossary/miscellaneous/baseball-age) | Exported season age; MLB's baseball-age convention uses a player's age on June 30. |

### Traditional batting statistics

| Column | Statistic | One-line summary |
| --- | --- | --- |
| `ab` | [At-bats (AB)](https://www.mlb.com/glossary/standard-stats/at-bat) | Official batting turns resulting in hits, errors, fielder's choices, or non-sacrifice outs; excludes walks and other exempt outcomes. |
| `pa` | [Plate appearances (PA)](https://www.mlb.com/glossary/standard-stats/plate-appearance) | Completed turns batting, counting every outcome rather than only outcomes that qualify as official at-bats. |
| `hit` | [Hits (H)](https://www.mlb.com/glossary/standard-stats/hit) | Fair balls that let the batter reach base without an error or fielder's choice, including all four hit types. |
| `single` | [Singles (1B)](https://www.mlb.com/glossary/standard-stats/single) | Hits that credit the batter with one base, without an intervening error or a play on another runner. |
| `double` | [Doubles (2B)](https://www.mlb.com/glossary/standard-stats/double) | Hits that credit the batter with two bases, without an intervening error or a play on another runner. |
| `triple` | [Triples (3B)](https://www.mlb.com/glossary/standard-stats/triple) | Hits that credit the batter with three bases, without an intervening error or a play on another runner. |
| `home_run` | [Home runs (HR)](https://www.mlb.com/glossary/standard-stats/home-run) | Fair-ball hits allowing the batter to circle all bases and score without an error, including inside-the-park homers. |
| `strikeout` | [Strikeouts (SO/K)](https://www.mlb.com/glossary/standard-stats/strikeout) | Batting turns ending with a third strike, still counted when the batter reaches base on an uncaught third strike. |
| `walk` | [Walks (BB)](https://www.mlb.com/glossary/standard-stats/walk) | First-base awards for four balls, with intentional walks also included in the batter's walk total. |
| `k_percent` | [Strikeout rate (K%)](https://www.mlb.com/glossary/advanced-stats/strikeout-rate) | Percentage of the batter's plate appearances ending in strikeouts. |
| `bb_percent` | [Walk rate (BB%)](https://www.mlb.com/glossary/advanced-stats/walk-rate) | Percentage of the batter's plate appearances resulting in walks. |
| `batting_avg` | [Batting average (AVG)](https://www.mlb.com/glossary/standard-stats/batting-average) | Hits divided by official at-bats, treating every hit equally and excluding walks and hit-by-pitches. |
| `slg_percent` | [Slugging percentage (SLG)](https://www.mlb.com/glossary/standard-stats/slugging-percentage) | Total bases from hits divided by at-bats, weighting singles through home runs as one through four bases. |
| `on_base_percent` | [On-base percentage (OBP)](https://www.mlb.com/glossary/standard-stats/on-base-percentage) | Frequency of reaching base through hits, walks, or hit-by-pitches, with sacrifice bunts excluded from the denominator. |
| `on_base_plus_slg` | [On-base plus slugging (OPS)](https://www.mlb.com/glossary/standard-stats/on-base-plus-slugging) | On-base percentage plus slugging percentage, combining a hitter's ability to reach base with production from hits. |
| `babip` | [Batting average on balls in play (BABIP)](https://www.mlb.com/glossary/advanced-stats/babip) | Hit frequency on balls in play, excluding home runs and strikeouts while including sacrifice flies in opportunities. |
| `b_rbi` | [Runs batted in (RBI)](https://www.mlb.com/glossary/standard-stats/runs-batted-in) | Runs credited to a batter's plate appearance, generally excluding runs resulting from errors or ground-ball double plays. |
| `r_total_stolen_base` | [Stolen bases (SB)](https://www.mlb.com/glossary/standard-stats/stolen-base) | Bases successfully taken by a runner's steal attempt, excluding advances credited to errors or defensive indifference. |

### Expected and weighted batting statistics

| Column | Statistic | One-line summary |
| --- | --- | --- |
| `xba` | [Expected batting average (xBA)](https://www.mlb.com/glossary/statcast/expected-batting-average) | Estimated batting average from contact quality and actual strikeouts, using exit velocity, launch angle, and sometimes sprint speed. |
| `xslg` | [Expected slugging percentage (xSLG)](https://www.mlb.com/glossary/statcast/expected-slugging-percentage) | Estimated total bases per at-bat from contact quality and expected probabilities of each hit type. |
| `woba` | [Weighted on-base average (wOBA)](https://www.mlb.com/glossary/advanced-stats/weighted-on-base-average) | Offensive rate weighting hits, unintentional walks, and hit-by-pitches by their contribution to scoring runs. |
| `xwoba` | [Expected weighted on-base average (xwOBA)](https://www.mlb.com/glossary/statcast/expected-woba) | Contact-quality estimate of wOBA using expected hit outcomes and actual walks, strikeouts, and hit-by-pitches. |
| `xobp` | [Expected on-base percentage (xOBP)](https://www.mlb.com/news/juan-soto-on-base-skills-best-in-mlb) | Estimated on-base percentage based on contact quality and actual plate-discipline outcomes rather than observed hits. |
| `xiso` | Expected isolated power ([ISO definition](https://www.mlb.com/glossary/advanced-stats/isolated-power)) | Expected extra bases per at-bat: xSLG minus xBA, applying MLB's SLG-minus-AVG definition to expected rates. |

### Statcast swing statistics

| Column | Statistic | One-line summary |
| --- | --- | --- |
| `avg_swing_speed` | [Average bat speed](https://baseballsavant.mlb.com/leaderboard/bat-tracking) | Average competitive-swing speed in mph, measured six inches from the bat's head at the contact or intercept point. |
| `fast_swing_rate` | [Fast-swing rate](https://www.mlb.com/glossary/statcast/fast-swing-rate) | Percentage of swings with bat speed of at least 75 mph. |
| `blasts_contact` | [Blasts / contact](https://www.mlb.com/glossary/statcast/bat-tracking-blasts) | Contact percentage combining high bat speed with efficient contact. |
| `blasts_swing` | [Blasts / swing](https://www.mlb.com/glossary/statcast/bat-tracking-blasts) | Same blast criterion, as a percentage of swings. |
| `squared_up_contact` | [Squared-up / contact](https://www.mlb.com/glossary/statcast/squared-up) | Percentage of contacts reaching ≥80% of possible exit velocity. |
| `squared_up_swing` | [Squared-up / swing](https://www.mlb.com/glossary/statcast/squared-up) | Same threshold, as a percentage of swings. |
| `avg_swing_length` | [Average swing length](https://www.mlb.com/glossary/statcast/swing-length) | Average distance in feet traveled by the bat barrel from the start of tracking to the impact point. |
| `swords` | [Swords](https://www.mlb.com/glossary/statcast/sword) | Count of incomplete, low-speed swinging strikes meeting Statcast's bat-crossing and deceleration criteria. |
| `attack_angle` | [Attack angle](https://www.mlb.com/glossary/statcast/attack-angle) | Average vertical angle in degrees of the bat's movement relative to the ground at contact or the swing-and-miss intercept. |
| `attack_direction` | [Attack direction](https://www.mlb.com/glossary/statcast/attack-direction) | Average horizontal direction of bat movement in degrees at contact or intercept, relative to center field, toward pull or opposite field. |
| `ideal_angle_rate` | [Ideal attack angle rate](https://www.mlb.com/glossary/statcast/ideal-attack-angle) | Percentage of competitive swings with an attack angle between 5° and 20°. |
| `vertical_swing_path` | [Swing path tilt](https://www.mlb.com/glossary/statcast/swing-path-tilt) | Average tilt in degrees of the bat's swing plane relative to the ground; higher values indicate steeper swings. |

### Statcast batted-ball and plate-discipline statistics

| Column | Statistic | One-line summary |
| --- | --- | --- |
| `exit_velocity_avg` | [Average exit velocity](https://www.mlb.com/glossary/statcast/exit-velocity) | Average speed in mph of the baseball immediately after bat contact across batted-ball events. |
| `launch_angle_avg` | [Average launch angle](https://www.mlb.com/glossary/statcast/launch-angle) | Average vertical angle in degrees at which batted balls leave the bat, excluding bunts. |
| `sweet_spot_percent` | [Launch-angle sweet-spot percentage](https://www.mlb.com/glossary/statcast/sweet-spot) | Percentage of batted balls with launch angles between 8° and 32°. |
| `barrel_batted_rate` | [Barrel rate](https://www.mlb.com/glossary/statcast/barrel) | Percentage of batted balls with exit-velocity/launch-angle combinations historically yielding at least .500 batting average and 1.500 slugging. |
| `hard_hit_percent` | [Hard-hit percentage](https://www.mlb.com/glossary/statcast/hard-hit-rate) | Percentage of batted balls with exit velocity of at least 95 mph. |
| `avg_best_speed` | [EV50 (average best speed)](https://baseballsavant.mlb.com/leaderboard/custom) | Average exit velocity in mph of the batter's hardest-hit 50% of batted balls. |
| `avg_hyper_speed` | [Adjusted EV (average hyper speed)](https://baseballsavant.mlb.com/leaderboard/custom) | Average exit velocity in mph after raising each batted ball's exit velocity to a minimum of 88 mph. |
| `whiff_percent` | [Whiff percentage](https://www.mlb.com/news/pitchers-with-strong-barrel-rate-and-whiff-rate-in-2023) | Percentage of swings that miss the ball. |
| `swing_percent` | [Swing percentage](https://baseballsavant.mlb.com/visuals/profile) | Percentage of pitches at which the batter swings: swings divided by swings plus takes. |

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
