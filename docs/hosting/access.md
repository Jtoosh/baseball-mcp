# Public website access

Anyone can open the production website at **baseball.jamesteuscher.click** and ask a question immediately. Accounts, email verification, invitations and sign-in are no longer required. This replaces the invitation policy from [hosting issue #9](https://github.com/Jtoosh/baseball-mcp/issues/9); see ADR 0006.

Question submission is supported only through this website. The server requires the exact configured Origin, same-origin Fetch Metadata, a JSON content type and the website submission marker. The browser sends these through its normal same-origin fetch. Missing Origin/metadata, cross-site forms and requests, incompatible content types and public API preflights are denied before network admission or model work. No CORS-enabled public API is offered. This is a best-effort boundary: scripts can imitate website requests, so it is not human verification or a guarantee that automation is excluded. No CAPTCHA or cookie-based quota is introduced.

Visitors sharing a network IP share the [network allowance and active-question slot](admission.md). Retrying from a different browser does not get a separate allowance. The owner accepts that changing networks can bypass this control. A paused deployment leaves the website accessible with a temporary-unavailability message; see [setup and operation](setup.md).

Controlled previews retain Vercel deployment protection, a visible fixture notice and separate operational state. Once platform access is granted, they need no application sign-in and use only predetermined responses, even if a production model key is accidentally present. Keep production public and previews protected in the platform settings.

The application no longer reads `CLERK_PUBLISHABLE_KEY`, `CLERK_SECRET_KEY` or `CLERK_ISSUER`. They are unnecessary and may be removed manually from deployment settings; old `clerk_issuer` and `clerk_publishable_key` JSON settings must be removed from administrator configuration. No external accounts, secrets, DNS records or provider resources are automatically deleted. Historical Clerk-user state is inert and does not identify or limit new anonymous visitors.
