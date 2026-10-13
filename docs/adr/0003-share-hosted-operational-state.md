# Share hosted operational state separately from batting snapshots

Hosted instances need consistent confirmations, question admissions and spending reservations despite process restarts and concurrent requests. Use free Neon PostgreSQL with atomic transactions for this operational state while retaining the read-only SQLite season snapshot for baseball queries. This adds a hosted dependency and fails closed when it is unavailable, while preserving the existing local workflow and reproducible batting data.
