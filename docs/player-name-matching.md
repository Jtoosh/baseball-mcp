# Player name matching

Player lookups should recognize `Jose Ramirez` as `José Ramírez` while keeping
the source spelling in answers.

When the season snapshot opens, build an in-memory catalog of player names and
their private IDs. Convert `Last, First` into `First Last` for matching. Make a
search key by applying Unicode NFD decomposition, removing combining accent
marks, folding case, and collapsing whitespace. Apply the same normalization
to the question. Match complete names at word boundaries, not inside other
words. Preserve punctuation within names and the original spelling for display.

For example, `José Ramírez`, `Jose Ramirez`, and the equivalent spelling using
combining accents all have the search key `jose ramirez`.

Give the planner the matched players' canonical names and private IDs. Player
lookups use those IDs rather than guessed name spellings. Keep every candidate
when several IDs share a search key; retain the application's existing player
ambiguity behavior instead of silently merging their statistics.

The planner marks direct lookups with `lookup_player_ids`; rankings and
intentional filtered subsets leave that list empty. The server validates those
IDs against the matched catalog and checks stored name and season columns for
coverage. A correction cannot drop or change the selected identities.

Before summarizing a direct player comparison, check that its results cover
every resolved player-season requested. If an existing record was omitted, use
the existing single correction attempt. If coverage still fails, return a
helpful failure instead of claiming the player's statistics are unavailable.
A player genuinely missing a requested season should be described as missing
that season, rather than absent from the whole dataset.

This method applies to local and hosted questions. It changes neither the
read-only snapshot nor its original names. IDs and SQL stay private. Typo
correction, nicknames, surname-only searches, and general fuzzy matching are
outside its scope.
