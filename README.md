# MLB batting questions

Ask baseball questions in plain English and get answers backed by MLB batting
statistics. The app supports player lookups, season leaders, top-ten overlaps,
and advanced metrics, with answers formatted in Markdown.

## How it works

A Go server sends user input to ChatGPT via API key. The user input is sent with the database schema, and the LLM generates a read-only query. The Go server runs it against a read-only SQLite snapshot, then asks the model to explain the results. Queries are restricted to baseball data and
have a ten-second execution deadline.

Automated tests check database results and application behavior with controlled
model responses; live model accuracy is assessed separately. Team and position comparisons and MCP support are
planned as later extensions.

Further reading:

- [Design and prompting strategies](docs/design.md)
- [Player name matching](docs/player-name-matching.md)
- [Hosted setup and operation](docs/hosting/setup.md)
- [Managing invited access](docs/hosting/access.md)

## How I built it

This application was built entirely by coding agents, orchestrated by myself, James Teuscher. My development workflow utilizes the popular agent skills [from Matt Pocock](https://github.com/mattpocock/skills) to produce better results. My basic process looks like this:

1. Describe the next feature or task to implement to the agent. Instruct the agent to invoke the `grill-me` skill to ask questions about any decision points it encounters. This allows me to make the decisions about the feature and be more in control of the application's architecture and design patterns.
2. After the agent finished using `grill-me`, I instruct it to use `to-spec` to create a comprehensive specification for agents. This provides user stories, implementation decisions, and clear non-goals to the agents.
3. Next, I invoke `to-tickets` to break the user stories and implementation into specific packages for the agents to work on and commit in increments. Once these tickets are generated and verified, I invoke the `implement` skill. This skill will use agent-invoked skills like `tdd` and `code-review` to implement, test, iterate, and review code independently.
4. Before merging any pull requests into main, I test all changes manually on my local machine. Any bugs or mistakes found I report to the agent, usually invoking the `diagnosing-bugs` skill.
5. Once the feature/fix looks correct, I instruct the agent to merge the PR and push to main, deploying the changes. Then I manually verify the result in production before marking the feature as complete.

I primarily used T3 Code and Superset as my agent control planes for this project. ChatGPT 6.1-Sol was the model used.

## Use the hosted app

Open [baseball.jamesteuscher.click](https://baseball.jamesteuscher.click).
Access is invitation-only to protect from API key abuse. Ask the site owner for an invitation and approval,
then sign in with the verification code sent to your email.

Ask a question and read the answer, including any tables in the model's response.
The hosted app lets the model interpret vague questions and same-name players
directly. To clarify your intent, ask a new, more specific question; each question
is independent.

Try these questions:

- “Who led MLB in home runs in 2024, 2025, and 2026?”
- “What was Luis Vázquez's OPS in 2025?”
- “Which qualifying players share tenth place by dense rank for home runs in 2024?”
- “Is any qualifying player in the top ten for HR, 1B, and 2B in 2026?”

Hosted access allows 20 new questions per user per rolling hour and one active
request at a time. Questions use the owner's prepaid provider balance, with no application spending ceiling.

## More info

For more details about data and answer rules injected into the LLM prompts, and local usage, see the [readme appendix](docs/README.appendix.md).

Project layout:

- `cmd/`: server and snapshot-import commands.
- `internal/app/`: application, HTTP handlers, embedded browser assets, and tests.
- `internal/data/`: read-only SQLite access and CSV import.
- `config/`: example local configuration and embedded hosted defaults.
- `data/`: the fixed MLB batting snapshot.
- `evaluation/`: independently verified reference answers.
- `docs/` and `scripts/`: design, setup instructions, and deployment tooling.
