# Issue tracker: GitHub

Issues and specs live in GitHub Issues for `jtoosh/baseball-mcp`.
Use the `gh` CLI. Pass `--repo jtoosh/baseball-mcp` explicitly.

## Conventions

- Create: `gh issue create --repo jtoosh/baseball-mcp --title "..." --body-file <path>`
- Read: `gh issue view <number> --repo jtoosh/baseball-mcp --comments`
- List: `gh issue list --repo jtoosh/baseball-mcp --state open --json number,title,body,labels`
- Comment: `gh issue comment <number> --repo jtoosh/baseball-mcp --body-file <path>`
- Apply/remove labels: `gh issue edit <number> --repo jtoosh/baseball-mcp --add-label "..." --remove-label "..."`
- Close: `gh issue close <number> --repo jtoosh/baseball-mcp --comment "..."`

For multiline bodies, write the text to a temporary file and use
`--body-file`. Use the label mapping in `triage-labels.md`.

## Pull requests as a triage surface

**PRs as a request surface: no.**

## Skill operations

When a skill says "publish to the issue tracker", create a GitHub issue.
When it says "fetch the relevant ticket", read the issue with comments.

## Wayfinding operations

- Map: one issue labelled `wayfinder:map`, containing Notes,
  Decisions-so-far, and Fog.
- Child ticket: a GitHub sub-issue of the map, labelled
  `wayfinder:<type>` (`research`, `prototype`, `grilling`, or `task`).
  If sub-issues are unavailable, use a task list in the map and
  `Part of #<map>` in each child.
- Blocking: use native GitHub issue dependencies through `gh api`
  under `repos/jtoosh/baseball-mcp`. If unavailable, record
  `Blocked by: #<n>, #<n>` near the top of the child.
  A ticket is unblocked when every blocker is closed.
- Frontier: scan open children in map order; select the first
  unblocked ticket without an assignee.
- Claim: assign the ticket to `@me` before starting work.
- Resolve: comment with the answer, close the ticket, and append
  a gist plus link to the map's Decisions-so-far.
