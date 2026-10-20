# Batting CSV profile

Inspected file: `data/advanced-mlb-24-26.csv`.

SHA-256: `60de4620c5e90e112a1c1142cd1504af42e46ece8ba816a1d21ea530a6b318ca`.

## Coverage and identity

| Season label | Rows | Player-seasons with PA >= 502 |
| --- | ---: | ---: |
| 2024 | 617 | 129 |
| 2025 | 632 | 145 |
| 2026 | 630 | 135 |

There are 1,879 rows and 857 distinct player IDs. No `(player_id, year)` pair is duplicated, and each ID has a consistent name. `Muncy, Max` belongs to two distinct players, IDs `571970` and `691777`; names cannot serve as unique identity. Hosted questions let the model interpret same-name lookups directly; local evaluation retains its configured clarification behavior.

The minimum plate-appearance count is 10 in each season. Answers concern records present in this dataset, which is not every MLB player-season. Season labels come from the CSV; retrieval dates and source filtering settings are not encoded in it.

## Available fields

The export has 49 columns:

- `last_name, first_name`
- `player_id`
- `year`
- `player_age`
- `ab`
- `pa`
- `hit`
- `single`
- `double`
- `triple`
- `home_run`
- `strikeout`
- `walk`
- `k_percent`
- `bb_percent`
- `batting_avg`
- `slg_percent`
- `on_base_percent`
- `on_base_plus_slg`
- `babip`
- `b_rbi`
- `r_total_stolen_base`
- `xba`
- `xslg`
- `woba`
- `xwoba`
- `xobp`
- `xiso`
- `avg_swing_speed`
- `fast_swing_rate`
- `blasts_contact`
- `blasts_swing`
- `squared_up_contact`
- `squared_up_swing`
- `avg_swing_length`
- `swords`
- `attack_angle`
- `attack_direction`
- `ideal_angle_rate`
- `vertical_swing_path`
- `exit_velocity_avg`
- `launch_angle_avg`
- `sweet_spot_percent`
- `barrel_batted_rate`
- `hard_hit_percent`
- `avg_best_speed`
- `avg_hyper_speed`
- `whiff_percent`
- `swing_percent`

`on_base_plus_slg` provides exported OPS. Singles are already present as `single`; every row satisfies `hit = single + double + triple + home_run`. Preserve exported rate values for rankings. All numeric fields parse successfully and no fields are blank.

## Reference results

| Question | Result in this CSV |
| --- | --- |
| Qualifying HR leader, 2024 | Aaron Judge: 58 HR, 704 PA |
| Qualifying HR leader, 2025 | Cal Raleigh: 60 HR, 705 PA |
| Qualifying HR leaders, 2026 | Kyle Schwarber: 45 HR, 701 PA; Pete Crow-Armstrong: 45 HR, 726 PA |
| Qualifying OPS leader, 2026 | Yordan Alvarez: 1.035 OPS, 691 PA |
| Qualifying top-ten dense-rank overlap of HR/singles/doubles | No matches in 2024, 2025, or 2026 |

These results were independently cross-checked with Python Decimal calculations and in-memory SQLite queries. See `evaluation/reference-answers.json` at the repo root for machine-readable expected data and behavioral cases.
