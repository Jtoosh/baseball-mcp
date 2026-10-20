# Hosting implementation verification

Historical review of the invitation-based release. Public-access spec #25 and ADRs 0005/0006 supersede its sign-in, account-removal and spending checks; current instructions are in [setup.md](setup.md).

Reviewed the implementation since `c022f9f`, the approved hosting specification and ticket breakdown. Two independent read-only reviewers checked Standards and Spec as required by the implement/code-review skills.

## Standards

Initial review found no hard documented-standard violation. It found two substantive risks: Clerk resource notifications cleared responses without an identity change, and hosted confirmation I/O unnecessarily held the local map mutex. Both are corrected. Player-choice validation now uses one domain rule, and operational fixture cleanup lives in test code. The follow-up reviewer found no remaining substantive Standards issue.

## Spec

Initial review found the same Clerk notification regression against the requirement to retain previous answers and clear context on sign-out. Stable user/session identity now controls clearing, and stale-account network errors cannot restore old context. The follow-up reviewer found no remaining substantive Spec concern. Owner provisioning and live deployment verification remain pending.

## Checks performed

- The complete deployment gate passed: JavaScript syntax, Go vet, full `go test -race ./...` with required real PostgreSQL hosted tests, and the server build. All packages passed; no live model calls were made.
- GitHub Actions workflow passed actionlint; both shell scripts passed syntax and ShellCheck checks; the diff passed whitespace checks.
- Targeted race-enabled tests after review fixes covered clarification, revision, cross-instance confirmation, private approval, stale lease release, shared budget concurrency, trustworthy usage settlement and UTC reset. A red/green regression additionally ensures a paused allowance denies clarification before creating pending state.
- Native T3 browser checks used the actual local HTTP application and a controlled local SDK provider for the reference OPS answer, Markdown and spinner behavior. Controlled browser denials verified previous-answer retention. Controlled external Clerk notifications reproduced the clearing bug before the fix, then verified same-identity retention, in-flight delivery and sign-out during a late network failure after the fix. A controlled hosted configuration failure hid the question form and displayed the preview notice. These checks do not establish live Clerk email delivery.
- A Linux/amd64 cross-build passed, and a startup artifact check rejected a non-verified PostgreSQL connection before serving requests.
- Vercel Hobby account access, new project linkage, Go framework, build command, deployment protection and custom-subdomain attachment were verified. The project's exact DNS recommendation was retrieved; no unrelated DNS record or existing project was changed.

No production Clerk, Neon or OpenAI secret was inspected. Live provider setup, DNS/HTTPS, actual cloud artifact publication and production smoke testing require the owner-only setup procedure. The $10 accounting guard is deliberately conservative: the default full-context reservation is $5.28072 per call and can stop questions early.
