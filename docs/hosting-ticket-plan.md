# Proposed hosting tickets

Parent specification: https://github.com/Jtoosh/baseball-mcp/issues/9

Status: approved and published. Plan positions map to GitHub issues in hosting-publication.json; dependencies are native GitHub blocking relationships.

## 1. Serve hosted questions behind invited email sign-in

**Blocked by:** None.

**What it delivers:** An invited user can sign in with an email code and use the browser question flow against the packaged season snapshot in a hosted environment, using controlled model responses until paid-call guards are installed.

- [ ] Explicit local, preview and production modes honor the hosted port, validate required settings and preserve local development.
- [ ] A reproducible private read-only snapshot retains all available metrics and reference values; public routes cannot download it.
- [ ] Clerk invite-only email OTP works with server-verified identity, issuer and current user eligibility. Uninvited, unauthenticated and removed users cannot execute questions.
- [ ] Sign-out clears displayed answers and pending context; failures use friendly messages and preserve established Markdown/loading behavior.
- [ ] Free dashboard invitation/removal behavior is documented without paid Allowlist or ban dependencies.
- [ ] Public HTTP tests use real SQLite and controlled supported authentication/model transports. Hosted live paid calls remain disabled until the spending guard is available.

## 2. Resume confirmed questions safely across hosted instances

**Blocked by:** 1.

**What it delivers:** A user can clarify an ambiguous question and confirm or revise it even when the next request reaches a different server instance.

- [ ] Free PostgreSQL operational storage has durable migrations and rejects unavailable or incompatible state.
- [ ] Opaque fifteen-minute confirmations bind verified user, original question, resolved intent and logical question identity.
- [ ] Atomic consumption permits one valid execution and rejects other users, altered questions, expiry and replay before SQL.
- [ ] Revisions retain the logical question identity and can replace pending clarification without retaining general conversation history.
- [ ] Two independent HTTP servers sharing real PostgreSQL verify confirmation, revision, concurrent consume and outage behavior.

## 3. Enforce the monthly allowance before every model call

**Blocked by:** 2.

**What it delivers:** Production questions run only when every model call has sufficient reserved funds; exhausted funds return a friendly UTC reset message.

- [ ] Atomic integer accounting keeps settled spending plus outstanding reservations within $10 per UTC calendar month, with no override.
- [ ] Planning, correction and answer calls for both strategies reserve before contacting the provider; calls retain their admission month across rollover.
- [ ] Validated configurable model/pricing profiles bound complete inputs and billed outputs, including cache categories and reasoning tokens. Unknown assumptions deny paid calls; model selection is never silently substituted.
- [ ] SDK retries/fallbacks cannot bypass accounting. Trusted usage settles safely; timeout, ambiguous failure, missing usage and settlement outages retain conservative reservations.
- [ ] Concurrent public HTTP questions and controlled SDK transports verify bounds, reservation-before-call, insufficient funds, rounding, rollover and uncertain failures without paid inference.
- [ ] Denial supplies no result table or internal pricing records and preserves the previous browser answer.

## 4. Enforce fair admission and the whole-request deadline

**Blocked by:** 2, 3.

**What it delivers:** Each invited user gets twenty independent questions per rolling hour, one active question at a time, and a bounded wait with useful completed comparison results.

- [ ] Cross-instance atomic admission enforces twenty questions per rolling sixty minutes and a recoverable single-active-request lease.
- [ ] Valid confirmation/revision reuses admission; forged/replayed confirmations do not consume new admissions; admitted failures count.
- [ ] Lease expiry recovers abandoned requests and stale owners cannot release a newer request's lease or refund uncertain spending.
- [ ] A 120-second parent deadline covers planning, correction, SQL and answer generation; preserve ten-second SQL limits and one correction per strategy.
- [ ] A completed comparison outcome survives another strategy timing out. Friendly denial/timeout stops animation and retains the marked previous answer.
- [ ] Concurrent public HTTP tests with real shared storage and controlled time cover quota recovery, confirmation reuse, lease recovery and cancellation without long waits.

## 5. Provide controlled previews and private hosted diagnostics

**Blocked by:** 1, 2.

**What it delivers:** Testers can exercise a visibly labelled protected preview cheaply, while owners receive useful safe error diagnostics without hosted question histories.

- [ ] Preview composition uses truthful snapshot-grounded fixture responses for supported examples and explains unsupported fixture questions.
- [ ] Preview activity uses separate authentication/state and never invokes production model services, even if an unexpected key is present.
- [ ] Private hosted diagnostics contain bounded request/stage/error classifications, without full questions, SQL, results, answers or sensitive credentials.
- [ ] Hosted evaluation histories are disabled; local optional evaluations continue to work.
- [ ] HTTP/browser and artifact checks verify preview notices, controlled outcomes, privacy, environment separation and zero production provider calls.

## 6. Publish test-gated deployments on the custom subdomain

**Blocked by:** 4, 5.

**What it delivers:** Successful main changes automatically reach the protected production app at baseball.jamesteuscher.click; failing changes leave the live deployment intact.

- [ ] The actual deployment path runs automated tests with real operational database fixtures and controlled authentication/model dependencies before publication. No production credentials or paid inference are needed for ordinary tests.
- [ ] Clean-checkout production artifacts include the private reproducible snapshot and start correctly in the supported Go runtime.
- [ ] Main is the production branch; other branch/PR deployments are protected controlled previews with isolated settings. Initial project setup cannot accidentally expose an unconfigured live app.
- [ ] Only the baseball subdomain is attached using exact project-provided DNS records. HTTPS and Clerk redirects work without changing unrelated DNS.
- [ ] Owner provisioning and secure secret-entry guidance covers free Vercel, Clerk and Neon accounts, dedicated model credentials, invitations/removal, UTC budget resets and service quota failures.
- [ ] Verify protected preview and custom-domain access/sign-in/question flows where owner credentials permit. Report any unperformed account, DNS or live smoke checks accurately; paid smoke calls pass the allowance guard.
