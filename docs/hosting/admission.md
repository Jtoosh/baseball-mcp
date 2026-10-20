# Network question admission

Hosted questions share a network allowance: 20 admitted questions per rolling hour per exact canonical IP address, with one processing question at a time. Different accounts or browsers on the same office/home network share both controls. Processing failures consume an admission; malformed, obsolete and admission-denied requests do not. Local development has no hosted IP or Neon requirement. Sign-in remains required until the public-access ticket removes it.

## Configuration and privacy

Set `IP_LIMIT_KEY` as a server-only Vercel secret in both production and preview. Use at least 32 bytes of random secret material (for example `openssl rand -hex 32` in a private terminal). All instances of an environment must use the same stable key. The setup script generates separate production/preview keys once and preserves them in the ignored owner file for subsequent runs. Preserve that file when repeating setup; replacing a key changes identities and resets their allowance. Never send this secret to the browser or put it in committed configuration.

`HOURLY_QUESTION_LIMIT` is an optional positive integer, default `20`, overriding the `hourly_question_limit` administrator configuration. Zero, negative, empty and noninteger environment values prevent hosted startup. Changing either environment setting requires redeployment. Do not rotate the IP key routinely.

Only HMAC-SHA256 identifiers with the `ip:v1:` prefix enter anonymous operational tables. The application does not persist or log raw addresses, prompts, answer histories or user-agent fingerprints. The shared secret prevents recovery by merely enumerating the small IPv4 address space; a leaked secret compromises that protection. Existing Clerk-user and historical confirmation/accounting tables remain separate and are not consulted by anonymous admission.

## Trusted ingress contract

The hosted origin must be reachable only through Vercel's ingress. The authoritative address is the single `x-vercel-forwarded-for` header supplied there. Vercel documents that it overwrites external `x-forwarded-for` addresses to prevent spoofing and that `x-vercel-forwarded-for` carries the corresponding client address even when another proxy can overwrite `x-forwarded-for`. This contract was checked against [Vercel's primary request-header documentation](https://vercel.com/docs/headers/request-headers#x-vercel-forwarded-for) during implementation.

The application deliberately ignores `Forwarded`, `x-forwarded-for`, `x-real-ip`, arbitrary custom headers and the TCP peer as alternatives. Missing, duplicate, comma-separated or malformed authoritative addresses deny hosted work with HTTP 503. Addresses with a zone suffix, unspecified addresses and multicast addresses are invalid. IPv4-mapped IPv6 normalizes to IPv4; equivalent IPv6 spellings normalize identically. Ordinary IPv6 addresses remain exact addresses rather than subnet buckets.

This is a platform trust boundary, not proof supplied by the header's name. Do not expose the Go server directly or deploy it behind an ingress that accepts client-supplied authoritative headers. Adding an external proxy or Vercel Trusted Proxy changes the address observed at ingress and requires reviewing that contract; otherwise clients behind that proxy can share its allowance. Deterministic tests simulate Vercel's header boundary; they do not claim a live cloud spoofing exercise.

## Shared state, recovery and retention

Operational schema version 2 adds separate anonymous admissions and leases without deleting version 1 records. A per-network PostgreSQL row lock makes quota counting, admission and the 125-second active lease atomic across instances. Requests retain their 120-second overall deadline. A completed request releases only its own lease token; after an abandoned lease expires, an old request cannot release its replacement. Missing or incompatible shared state fails closed before model work.

HTTP 429 carries `Retry-After` in seconds for both the hourly allowance and an active request. The browser displays that guidance, stops loading and preserves the displayed answer. No automatic retry submits another paid question.

Every successful admission also deletes up to 256 expired admissions and 256 old lease identities from that operational namespace, ordered by expiry and skipping locked rows. Cleanup runs across networks, including visitors who never return; it needs no return visit from a particular identity. Expired admissions are excluded from counting even before physical cleanup. Lease identities become eligible after at least one hour past their last expiry/release. Cleanup progresses with traffic and does not run while the application is idle. Production and controlled preview retain separate operational namespaces/databases and separate IP keys.
