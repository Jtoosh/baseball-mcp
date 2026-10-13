# Hosted setup and operation

The hosting implementation keeps local configuration private and adds a separate committed administrator configuration for deployment. It does not require a local OpenAI key for controlled previews or automated tests.

## Prepared infrastructure

- Hobby team: `jtooshs-projects`; linked project: `baseball-mcp`.
- Go framework, gated build command and Vercel deployment protection are configured.
- `baseball.jamesteuscher.click` is attached to this project. Existing apex DNS and the `exercise-app` project are untouched.
- DNS still requires owner action at Spaceship: the project currently recommends CNAME `baseball` → `ae774825b448a93a.vercel-dns-017.com.` Recheck with `npx vercel domains verify baseball.jamesteuscher.click --scope jtooshs-projects` before editing.
- GitHub project/org variables are set, and automatic publishing is initially disabled until credentials and smoke checks are complete.

## Owner-only setup

Run `bash scripts/setup-hosting.sh` in a private terminal. It opens the provider dashboards and captures secret values with hidden input; never run it under terminal recording or paste credentials into chat. Its ignored private file is not loaded automatically by the application. The script covers separate Clerk instances, separate free Neon operational databases, a dedicated production OpenAI credential, a GitHub deployment token and subdomain DNS. Re-running replaces the named settings in this new project; it does not change other projects.

Use free plans with upgrades disabled. Production and preview require `CLERK_PUBLISHABLE_KEY`, `CLERK_SECRET_KEY`, `CLERK_ISSUER`, and `DATABASE_URL`. Production also requires `PUBLIC_ORIGIN=https://baseball.jamesteuscher.click` and `OPENAI_API_KEY`. Preview derives its exact origin from `VERCEL_URL` and ignores any OpenAI key. See [invited access](access.md) for invitation and private approval metadata settings. Provision the production Clerk domain and its required DNS records using Clerk's current dashboard instructions; they are separate from the baseball CNAME.

The application uses the PostgreSQL URLs only on the server. Use pooled URLs with `sslmode=verify-full`; ordinary tests use a separate local PostgreSQL service. Hosted startup creates the operational schema transactionally, checks its version and denies startup on unavailable/incompatible state. No batting statistics enter PostgreSQL.

## Verify before enabling automatic production

1. Commit/push the hosting implementation so the workflow is available. Keep Vercel's Git repository integration disconnected: this repository publishes through GitHub Actions after its build tests pass, and also disables Git deployment in Vercel configuration.
2. After secure setup, run the workflow manually with environment **preview**. It builds with a disposable PostgreSQL fixture and controlled model/auth transports, then publishes only if tests succeed. Fork pull requests receive tests without deployment credentials. The first deployment in a new Vercel project can be classified as production by the platform; it contains preview credentials and must remain protected, with live requests failing closed if classification conflicts. Create and inspect a subsequent preview before allowing production.
3. Open the protected preview as a Vercel team member. Verify the visible controlled-preview notice, real Clerk email-code sign-in, rejection of unapproved users, dashboard approval/removal, the OPS example, clarification behavior covered by the automated tests, sign-out and retained-answer error presentation. Preview supports only the explicitly listed single-season leader questions.
4. Verify DNS and HTTPS; run the workflow manually with environment **production** only after live Clerk settings, state and pricing are ready. Verify an uninvited user is denied and an invited approved user can ask one question. This live call consumes the guarded monthly allowance; deterministic tests do not establish live model accuracy.
5. After successful smoke checks, set `HOSTING_DEPLOY_ENABLED=true` with `gh variable set HOSTING_DEPLOY_ENABLED --repo jtoosh/baseball-mcp --body true`. Subsequent main pushes publish production automatically; other branch pushes publish protected controlled previews. A failing build never reaches the publication step, leaving the live version intact.

The CI token needs access only to the Hobby team/project and belongs in the repository secret `VERCEL_TOKEN`. No Clerk, database or OpenAI keys are GitHub test secrets. Vercel's private environment pull during deployment uses the project's environment settings; avoid debugging or printing pulled files.

## Spending and limits

Every planning, correction and answer call first reserves its maximum validated cost in the shared ledger. The default profile preserves `gpt-6.1-sol`, standard/global processing, a 1,050,000-token full-context bound and a 2,048-output-token ceiling, including reasoning. It reserves $5.28072 per call using the highest documented input/cache-write and output rates; no paid token-count endpoint or estimated character count is used. Trusted completed usage reduces that reservation using the same conservative rates. Actual charges can be lower.

This intentionally conservative profile can block simultaneous paid calls and can stop questions when less than $5.28072 remains. Missing usage or uncertain failure keeps the reservation for that month and may pause questions immediately. There is no override or uncertain-refund operation. Each UTC calendar month gets a new $10 allowance; calls completing after rollover retain their original month's accounting. Price assumptions must be reviewed before the profile expires on 2026-11-15. The reviewed model registry also supports `gpt-4.1-mini` if the administrator explicitly selects it; the default never changes automatically. Optional profile settings can increase conservative bounds/rates or record a reviewed expiry, but cannot lower the verified context/rate floors. A model outside the reviewed registry fails closed until its documented profile is added. This is an application guard under validated rates, not a provider-account spending guarantee.

Twenty newly admitted questions per user are allowed in a rolling hour. Valid clarification confirmation/revision reuses the admission; normal admitted failures count. Requests have a 120-second parent deadline, ten seconds per SQL execution and at most one correction per strategy. One active request per user uses a 125-second recoverable lease. Lease recovery does not refund uncertain model spending.

## Diagnostics and verification status

Hosted errors go to administrator runtime logs with bounded stage/class identifiers. Raw prompts, SQL, tables, answers and connection/provider errors are not logged. Full evaluation histories are disabled in hosting; the local optional evaluation workflow remains available. Operational records contain confirmation state, admissions, leases and budget accounting only. Expired confirmations are inaccessible immediately and removed during subsequent confirmation creation; old hourly admissions are pruned on admission.

At implementation time, real local PostgreSQL and SQLite integration tests and controlled SDK/auth transports are verified. Vercel project linkage/protection/framework settings and the subdomain attachment are verified. Live Clerk/Neon provisioning, email delivery, DNS/HTTPS, actual cloud build/publication and live model smoke checks remain unverified until owner credentials and DNS are configured. Do not interpret prepared code or deterministic fixtures as a completed production deployment.
