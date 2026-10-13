# Use read-only SQLite season snapshots

The application uses SQLite populated from fixed MLB regular-season CSV snapshots, with one combined record per player-season. Serving questions opens the database read-only; dataset updates happen manually outside the question workflow, normally once a year. This keeps local setup simple and makes evaluations reproducible without a live data dependency, while retaining nonqualifying player-seasons for direct statistical lookups and applying the 502-plate-appearance threshold to rankings.
