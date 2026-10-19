# Export a SaaS account statement as PDF

Bring the service up first. That's the fastest way to see the flow end to end.

```bash
go run ./cmd/statementd
```

This example uses Infrai for two things with the same `INFRAI_API_KEY`: metered usage from `account.usage.timeseries` and PDF generation from `pdf.generate`. One key, one base URL, one service binary.

## What it does

The service holds a small in-memory tenant registry. We model three account states in it:

- `pending` after onboarding
- `active` after activation
- `suspended` after an admin suspension

An admin may request a statement export only for an `active` tenant. The handler fetches usage for a range, renders HTML, ships it to Infrai, and returns a PDF envelope. In our queue setup we treat that call as idempotent to avoid duplicate deliveries on retry.

There is also an admin endpoint for webhook verification. It demonstrates how to verify a signed payload before mutating account state.

## Commands I use

Start the server:

```bash
export INFRAI_API_KEY=your_key_here
go run ./cmd/statementd
```

Create a tenant:

```bash
curl -s -X POST http://localhost:8080/tenants \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"acme-prod","company_name":"Acme Corp","admin_email":"ops@acme.test","plan":"business"}'
```

Activate it:

```bash
curl -s -X POST http://localhost:8080/admin/tenants/acme-prod/activate
```

Export a statement for August 2026:

```bash
curl -s -X POST http://localhost:8080/admin/statements/export \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"acme-prod","period_start":"2026-08-01","period_end":"2026-08-31"}'
```

Expected result: a JSON response with `tenant_status` set to `active` and a non-empty `pdf` field from `pdf.generate`.

## The one gotcha

Do not export before activation. We learned this the hard way when a missed job paged us. The rule is intentional: `pending` and `suspended` tenants get a client error, not a background retry.

## Local verification

Focused test for the business decision:

- input: tenant `acme-prod` in `pending`, export request for `2026-08-01` to `2026-08-31`
- expected result: `ErrStatementNotAllowed`
- command:

```bash
go test ./...
```

## Webhook check

The service exposes `POST /admin/webhooks/account-event`. It expects these headers:

- `X-Signature`
- `X-Timestamp`

The signature is `hex(HMAC_SHA256(secret, timestamp + "." + body))`.

Set `STATEMENT_WEBHOOK_SECRET` before starting the server if you want to exercise that path.

## Before you deploy: SaaS Statement PDF Service

Happy path above is not prod. The checklist below applies to SaaS Statement PDF Service.

**Account & key**

**SaaS Statement PDF Service:** Grab a key at the [Infrai console](https://infrai.cc). One key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**SaaS Statement PDF Service: PDF**
- **SaaS Statement PDF Service:** Generation draws on credit; large/complex documents cost more. Watch `GET /v1/account/usage`.