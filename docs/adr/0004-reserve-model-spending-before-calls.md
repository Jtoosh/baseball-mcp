---
status: superseded by ADR-0005
---

# Reserve model spending before paid calls

The hosted application must stop questions at a $10 monthly allowance rather than rely on delayed provider alerts. Commit a conservative spending reservation before every production model call, with validated cost bounds and no refund for uncertain billed work. This may stop questions before actual spending reaches $10, but avoids authorizing concurrent calls beyond the allowance under the validated pricing assumptions.

Superseded by [ADR 0005](0005-rely-on-prepaid-provider-balance.md) for ongoing public access.
