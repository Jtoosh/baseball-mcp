# Hosted setup and operation

Production is a public website with no application sign-in. Hosted questions use shared IP limits; controlled previews remain protected by Vercel and use fixture responses without paid model calls. Ordinary local development does not require the hosted IP-limit key or Neon.

## Update the existing deployment

Use this path when the Vercel project, databases, production model credential and domain are already configured. Adding the IP-limit key does not require repeating account, database or DNS provisioning.

### Add `IP_LIMIT_KEY` in Vercel

`IP_LIMIT_KEY` is a random secret you generate. The server uses it to turn an IP address into a private, stable rate-limit identifier. It is not issued by OpenAI, Neon or an IP-address service.

1. Open the **baseball-mcp** project under **jtooshs-projects**, then **Settings → Environment Variables**: [open project environment variables](https://vercel.com/jtooshs-projects/baseball-mcp/settings/environment-variables).
2. In a private terminal, generate the Production value:

   ```sh
   openssl rand -hex 32
   ```

   This produces 32 random bytes as 64 hexadecimal characters, which satisfies startup validation. Keep a private copy for future setup runs; Vercel Secret values are hidden after saving.

3. Add a variable named **`IP_LIMIT_KEY`**, paste that generated value, choose type **Secret**, select **Production only**, and save.
4. Run the command again to generate a different value. Add **`IP_LIMIT_KEY`** again with type **Secret**, selecting **Preview only** and applying it to all preview branches. Both environments use the same variable name, with different values:

   | Variable name | Environment | Value |
   | --- | --- | --- |
   | `IP_LIMIT_KEY` | Production | First generated secret |
   | `IP_LIMIT_KEY` | Preview | Second generated secret |

5. Keep each value stable across deployments. All instances in one environment need the same key; changing it resets that environment's IP identities and allowances. If the variables already exist, retain their current values.
6. Deploy the updated code through the workflow below. Saved variables apply to new deployments; they do not update an existing deployment in place. See Vercel's [environment-variable instructions](https://vercel.com/docs/environment-variables/managing-environment-variables) and [Secret type documentation](https://vercel.com/docs/environment-variables/sensitive-environment-variables).

The application reads `IP_LIMIT_KEY` from its server environment in Vercel. Adding it only as a GitHub Actions secret, putting it in administrator JSON, or creating a local `.env` file does not configure the deployed application. Local mode needs no value. A missing or short key prevents hosted startup with `configure a shared server-only IP_LIMIT_KEY of at least 32 bytes`.

### Deploy and verify the updated code

1. Push the branch containing the public-access implementation to `jtoosh/baseball-mcp` so GitHub Actions can select it. Confirm the selected branch contains the new code; redeploying an older invitation-based revision will retain its sign-in behavior.
2. Open [GitHub Actions](https://github.com/jtoosh/baseball-mcp/actions), select **Verify and deploy hosted app**, and choose **Run workflow**. Select the code branch and set the workflow's **environment** input to **preview**. Manual runs deploy even if `HOSTING_DEPLOY_ENABLED` is unset or false.
3. The workflow runs the build gate with disposable PostgreSQL, then publishes the Preview artifact only if the tests and build succeed. Open the protected Preview as a Vercel team member. Check the visible controlled-preview notice, immediately usable form and `Who led MLB in OPS?` example. Preview questions use fixture responses without paid model calls. Keep Preview deployment protection enabled.
4. Run the same workflow from the intended code branch with **environment = production**. Production uses the Production variables, including its own `IP_LIMIT_KEY`, and serves `baseball.jamesteuscher.click`. Keep Production publicly accessible.
5. Open Production in a fresh browser and verify the form appears without application sign-in. A submitted live question consumes prepaid provider balance. Verify question submission and clear quota/busy/pause errors as needed; see [the recorded implementation checks](public-access-review.md) for what controlled tests already cover.

The repository publishes through this test-gated workflow, with Vercel Git deployments disabled. Once the implementation is on `main`, successful main pushes deploy Production when the repository variable `HOSTING_DEPLOY_ENABLED=true`; other pushed branches deploy Preview. Pull requests run verification without deployment credentials. A failed build leaves the active deployment intact. Enable automatic publishing after the intended deployment is verified:

```sh
gh variable set HOSTING_DEPLOY_ENABLED --repo jtoosh/baseball-mcp --body true
```

## Hosted environment reference

Set application values in the Vercel project's Environment Variables page:

| Name | Production | Preview |
| --- | --- | --- |
| `IP_LIMIT_KEY` | Required Secret; stable random value | Required Secret; a different stable random value |
| `DATABASE_URL` | Required pooled Neon URL | Required separate pooled Neon URL |
| `OPENAI_API_KEY` | Required server-only production credential | Unused; fixtures never contact the production provider |
| `PUBLIC_ORIGIN` | `https://baseball.jamesteuscher.click` | Derived from Vercel's `VERCEL_URL` automatically |
| `HOURLY_QUESTION_LIMIT` | Optional positive integer; default `20` | Optional positive integer; default `20` |
| `QUESTIONS_PAUSED` | Optional `true` or `false`; default `false` | Optional `true` or `false`; default `false` |

Public origins must be exact HTTPS origins without a path or trailing slash. Clerk settings and invitation approval are unnecessary; existing Clerk environment variables are ignored and can be removed manually. See [public website access](access.md) for retired configuration and website submission checks. Missing required hosted settings prevent startup rather than selecting local mode.

Use separate Production and Preview PostgreSQL credentials with pooled connections. Startup strengthens Neon's managed `sslmode=require` URLs to `verify-full` in memory, requiring certificate and hostname verification. Explicitly insecure SSL modes are rejected. Startup upgrades operational schema version 1 to version 2 transactionally; old user, confirmation and accounting records remain inert. Batting statistics stay in the bundled read-only SQLite snapshot.

GitHub Actions separately needs repository secret `VERCEL_TOKEN` and repository variables `VERCEL_ORG_ID` and `VERCEL_PROJECT_ID`. Application secrets belong in Vercel; the workflow selects and pulls the target environment during deployment. Automated tests use their own disposable PostgreSQL and controlled provider fixtures.

## First-time provisioning

The deployment target is Vercel project `baseball-mcp` in team `jtooshs-projects`, using the existing Go application and custom domain. Hosted configuration, CSV data and browser assets are embedded in the executable; the build compiles `cmd/server/main.go` through `./cmd/server`.

For a new installation, run `bash scripts/setup-hosting.sh` from the repository root in a private terminal. The wizard configures separate Neon databases, generates separate IP-limit keys, accepts a production model credential, configures deployment credentials and walks through custom-domain DNS. Use free plans with upgrades disabled. Existing deployments should use the update procedure above rather than repeat this provisioning flow. Domain/DNS recommendations recorded by older deployments must be checked against the current project's instructions before changing records; preserve unrelated domains and DNS.

The wizard saves owner values in ignored `.private/hosting-owner.env`. Its backup names `PRODUCTION_IP_LIMIT_KEY` and `PREVIEW_IP_LIMIT_KEY` are local to that file; each is uploaded to Vercel under the name `IP_LIMIT_KEY` in its respective environment. The Go application does not load the owner file. The wizard reuses saved keys, but generates new ones if those backup entries are absent. If you configured keys manually in the dashboard, preserve the same values in these backup entries before a later wizard run to avoid resetting allowances.

## Spending and limits

Production model calls rely on the owner's prepaid provider balance. There is no application monetary ceiling, pricing-expiry gate, reservation, settlement or monthly reset. The owner accepts balance exhaustion. Historical allowance and reservation records remain inert for safe schema compatibility; they never deny new questions. Existing `pricing_profile` administrator configuration is accepted and ignored.

The configured model and strategy remain explicit. Every hosted planning, correction and answer call keeps a 2,048-output-token ceiling (including reasoning), the explicit `default` service tier and disabled automatic SDK retries/fallbacks. Local and evaluation runs retain their existing provider defaults. The application does not impose a spending-only model registry. Provider quota or depleted-balance responses produce a safe temporary-unavailability message; the browser keeps its previous answer and stops loading. A later ordinary request can succeed when the provider accepts work again. The application does not inspect or claim to know the provider balance.

Twenty newly admitted questions per network IP are allowed in a rolling hour by default, configurable through `HOURLY_QUESTION_LIMIT`. Production and preview also require separate stable server-only `IP_LIMIT_KEY` values; the setup script generates and preserves them. See [network question admission](admission.md) for the trusted Vercel IP contract, key handling and cleanup. Each new question, including a more specific question clarifying an earlier answer, uses a new admission; normal admitted failures count. Obsolete confirmation/revision requests are rejected before admission. Requests have a 120-second parent deadline, ten seconds per SQL execution and at most one correction per strategy. One active request per network IP uses a 125-second recoverable lease.

## Pause new questions

Set the server environment variable `QUESTIONS_PAUSED=true` to pause new questions. It defaults to `false`; only the exact values `true` and `false` are accepted, and an invalid value prevents startup. The optional administrator JSON setting `questions_paused` is also a boolean, defaults to `false`, and is overridden by a nonempty `QUESTIONS_PAUSED` value. Existing local configuration remains valid.

For Vercel, change `QUESTIONS_PAUSED` in the intended environment (Production or Preview), then create a new deployment through the test-gated workflow for that environment. Editing an environment variable does not change existing deployments: the updated setting takes effect when traffic reaches the newly deployed version. Old instances and requests admitted before that cutover may finish under their existing deadlines. This is a deployment-time switch, with no polling or instant global cancellation. Set `QUESTIONS_PAUSED=false` and deploy again to resume; rolling back to an older deployment restores that deployment's setting.

While paused, the page and assets stay available and display “Questions are temporarily unavailable”. New question submissions return HTTP 503 with the same safe message before acquiring an admission or active-request lease or starting model work. The browser retains its previous answer and stops its loading indicator. Pause and resume leave existing question allowances and leases intact. Reload the page after a deployment to refresh the displayed pause notice; the server always enforces its current setting on submissions.

## Diagnostics and verification status

Hosted errors go to administrator runtime logs with bounded stage/class identifiers. Raw prompts, SQL, tables, answers and connection/provider errors are not logged. Full evaluation histories are not recorded. New operational records contain anonymous admissions and leases only; historical spending records are retained without enforcement. Legacy confirmation tables remain for schema compatibility, but hosted requests cannot create or resume confirmations; anonymous admissions and leases are pruned in bounded batches on successful admission, including expired identities that never return. Legacy user admissions remain inert.

The implementation checks are recorded in [public-access review](public-access-review.md): real PostgreSQL/SQLite integration tests, controlled provider responses, browser behavior and the test-gated local deployment build. Earlier live checks established hosting under the retired invitation policy; they do not establish that this revision is currently deployed. This guide does not inspect the current Vercel account or deployment state. Verify the selected code revision and fresh-browser behavior after deployment.
