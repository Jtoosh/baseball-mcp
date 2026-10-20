# Hosted setup and operation

The hosting implementation keeps local configuration private and adds a separate committed administrator configuration for deployment. It does not require a local OpenAI key for controlled previews or automated tests.

## Prepared infrastructure

- Hobby team: `jtooshs-projects`; linked project: `baseball-mcp`.
- Go framework, gated build command and Vercel deployment protection are configured.
- The server entry point is `cmd/server/main.go`; the gated build compiles `./cmd/server`. Hosted defaults in `config/hosted.json`, the CSV in `data/`, and browser assets in `internal/app/static/` are embedded in the executable, so deployment does not depend on its working directory.
- `baseball.jamesteuscher.click` is attached to this project. Existing apex DNS and the `exercise-app` project are untouched.
- DNS and HTTPS are verified for CNAME `baseball` → `ae774825b448a93a.vercel-dns-017.com.` Recheck with `npx vercel domains verify baseball.jamesteuscher.click --scope jtooshs-projects` before future DNS changes.
- GitHub project/org variables are set. Automatic test-gated publishing is enabled after successful Preview and Production smoke checks.

## Owner-only setup

Run `bash scripts/setup-hosting.sh` in a private terminal. It opens the provider dashboards and captures secret values with hidden input; never run it under terminal recording or paste credentials into chat. Its ignored private file is not loaded automatically by the application. The script covers separate free Neon operational databases, a dedicated production OpenAI credential, a GitHub deployment token and subdomain DNS. Re-running replaces the named settings in this new project; it does not change other projects.

Use free plans with upgrades disabled. Production and preview require `DATABASE_URL` and a stable server-only `IP_LIMIT_KEY` of at least 32 bytes. Production also requires `PUBLIC_ORIGIN=https://baseball.jamesteuscher.click` and `OPENAI_API_KEY`. Preview derives its exact origin from `VERCEL_URL` and ignores any OpenAI key. Public origins must be exact HTTPS origins without a path or trailing slash. Clerk settings and invitation approval are unnecessary; see [public website access](access.md) for the retired configuration and best-effort submission boundary. Local startup remains independent of these hosted settings. Missing hosted configuration fails startup instead of selecting local mode.

For an existing deployment, add a stable `IP_LIMIT_KEY` separately to Production and Preview before deploying this revision; the setup script generates and preserves these keys. Keep Production public and Preview platform-protected. The new release upgrades operational schema version 1 to version 2 transactionally and ignores old user admissions, active leases, confirmations and spending records for anonymous questions. Existing Clerk environment values may remain harmlessly until manually removed. This change does not deploy itself or delete external resources.

The application uses the PostgreSQL URLs only on the server. Use pooled URLs. Hosted startup strengthens Neon's managed `sslmode=require` URLs to `verify-full` in memory, requiring certificate and hostname verification without a plaintext fallback; you do not need to edit integration-owned variables. Explicitly insecure SSL modes are rejected. Ordinary tests use a separate local PostgreSQL service. Hosted startup creates the operational schema transactionally, checks its version and denies startup on unavailable/incompatible state. No batting statistics enter PostgreSQL.

## Verify before enabling automatic production

1. Commit/push the hosting implementation so the workflow is available. Keep Vercel's Git repository integration disconnected: this repository publishes through GitHub Actions after its build tests pass, and also disables Git deployment in Vercel configuration.
2. After secure setup, run the workflow manually with environment **preview**. It builds with a disposable PostgreSQL fixture and controlled model transports, then publishes only if tests succeed. Fork pull requests receive tests without deployment credentials. The first deployment in a new Vercel project can be classified as production by the platform; it contains preview credentials and must remain protected, with live requests failing closed if classification conflicts. Create and inspect a subsequent preview before allowing production.
3. Open the protected preview as a Vercel team member. Verify the visible controlled-preview notice, immediately usable question form, the OPS example, direct answers without ambiguity confirmation, anonymous IP limits and retained-answer error presentation. Preview supports only the explicitly listed single-season leader questions. Application sign-in is unnecessary.
4. Verify DNS and HTTPS; run the workflow manually with environment **production** only after operational state, public origin, network key and production model credential are ready. Verify a fresh anonymous browser can open the form. If the owner chooses to submit a live question, it consumes prepaid provider balance; deterministic tests do not establish live model accuracy. Verify same-origin submissions, quota/busy messages and the pause switch; do not disable Preview protection.
5. After successful smoke checks, set `HOSTING_DEPLOY_ENABLED=true` with `gh variable set HOSTING_DEPLOY_ENABLED --repo jtoosh/baseball-mcp --body true`. Subsequent main pushes publish production automatically; other branch pushes publish protected controlled previews. A failing build never reaches the publication step, leaving the live version intact.

The CI token needs access only to the Hobby team/project and belongs in the repository secret `VERCEL_TOKEN`. No database or OpenAI keys are GitHub test secrets. Vercel's private environment pull during deployment uses the project's environment settings; avoid debugging or printing pulled files.

## Spending and limits

Production model calls rely on the owner's prepaid provider balance. There is no application monetary ceiling, pricing-expiry gate, reservation, settlement or monthly reset. The owner accepts balance exhaustion. Historical allowance and reservation records remain inert for safe schema compatibility; they never deny new questions. Existing `pricing_profile` administrator configuration is accepted and ignored.

The configured model and strategy remain explicit. Every planning, correction and answer call keeps a 2,048-output-token ceiling (including reasoning), the explicit `default` service tier and disabled automatic SDK retries/fallbacks. The application does not impose a spending-only model registry. Provider quota or depleted-balance responses produce a safe temporary-unavailability message; the browser keeps its previous answer and stops loading. A later ordinary request can succeed when the provider accepts work again. The application does not inspect or claim to know the provider balance.

Twenty newly admitted questions per network IP are allowed in a rolling hour by default, configurable through `HOURLY_QUESTION_LIMIT`. Production and preview also require separate stable server-only `IP_LIMIT_KEY` values; the setup script generates and preserves them. See [network question admission](admission.md) for the trusted Vercel IP contract, key handling and cleanup. Each new question, including a more specific question clarifying an earlier answer, uses a new admission; normal admitted failures count. Obsolete confirmation/revision requests are rejected before admission. Requests have a 120-second parent deadline, ten seconds per SQL execution and at most one correction per strategy. One active request per network IP uses a 125-second recoverable lease.

## Pause new questions

Set the server environment variable `QUESTIONS_PAUSED=true` to pause new questions. It defaults to `false`; only the exact values `true` and `false` are accepted, and an invalid value prevents startup. The optional administrator JSON setting `questions_paused` is also a boolean, defaults to `false`, and is overridden by a nonempty `QUESTIONS_PAUSED` value. Existing local configuration remains valid.

For Vercel, change `QUESTIONS_PAUSED` in the intended environment (Production or Preview), then create a new deployment through the test-gated workflow for that environment. Editing an environment variable does not change existing deployments: the updated setting takes effect when traffic reaches the newly deployed version. Old instances and requests admitted before that cutover may finish under their existing deadlines. This is a deployment-time switch, with no polling or instant global cancellation. Set `QUESTIONS_PAUSED=false` and deploy again to resume; rolling back to an older deployment restores that deployment's setting.

While paused, the page and assets stay available and display “Questions are temporarily unavailable”. New question submissions return HTTP 503 with the same safe message before acquiring an admission or active-request lease or starting model work. The browser retains its previous answer and stops its loading indicator. Pause and resume leave existing question allowances and leases intact. Reload the page after a deployment to refresh the displayed pause notice; the server always enforces its current setting on submissions.

## Diagnostics and verification status

Hosted errors go to administrator runtime logs with bounded stage/class identifiers. Raw prompts, SQL, tables, answers and connection/provider errors are not logged. Full evaluation histories are not recorded. New operational records contain anonymous admissions and leases only; historical spending records are retained without enforcement. Legacy confirmation tables remain for schema compatibility, but hosted requests cannot create or resume confirmations; anonymous admissions and leases are pruned in bounded batches on successful admission, including expired identities that never return. Legacy user admissions remain inert.


The current public-access revision is verified with real PostgreSQL/SQLite integration tests, controlled provider responses, browser assets and the test-gated local deployment build. Earlier live hosting checks established the custom domain, HTTPS and Neon connectivity under the retired invitation policy; they do not verify a deployment of this anonymous-access revision. No new deployment, production account change or paid smoke request is performed by this implementation. After deployment, use the verification steps above before claiming the public production flow is live.
