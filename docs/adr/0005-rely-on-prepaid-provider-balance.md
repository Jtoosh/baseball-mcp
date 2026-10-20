# Rely on prepaid provider balance for public questions

The owner accepts exhaustion of the prepaid provider balance to keep public demo access simple, so production calls have no application monetary ceiling, pricing expiry gate or spending reservation. This supersedes ADR 0004; historical accounting remains inert, while independent output-token, service-tier, retry, deadline, correction and shared admission controls remain active. ADR 0003 still requires atomic shared admission that fails closed when operational state is unavailable; recognized provider quota failures give temporary unavailability without a sticky lockout or a monthly-reset promise.

Hosted startup selects the 2,048-output-token ceiling and default service tier independently of spending accounting for every plan, correction and answer. Ordinary local and evaluation runs retain provider defaults for those parameters; disabled SDK retries and existing request deadlines remain unchanged.
