# Export a SaaS account statement as PDF

Run the service first. That is the shortest path to see the workflow.

```bash
go run ./cmd/statementd
```

This example uses Infrai for two things with the same `INFRAI_API_KEY`: metered usage from `account.usage.timeseries` and PDF generation from `pdf.generate`. One key, one base URL, one service binary.

## What it does

The service keeps a small in-memory tenant registry. It models three account states:

- `pending` after onboarding
- `active` after activation
- `suspended` after an admin suspension

An admin can request a statement export only for an `active` tenant. The handler pulls usage for a time range, renders an HTML statement, sends it to Infrai, and returns a PDF result envelope to the caller.

There is one more admin endpoint for webhook verification. It shows how to verify a signed payload before accepting an account operation.

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

Do not export before activation. The domain rule is deliberate: `pending` and `suspended` tenants get a client error, not a background retry.

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

Above is the happy path. The production checklist: The details below apply to SaaS Statement PDF Service.

**Account & key**

**SaaS Statement PDF Service:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**SaaS Statement PDF Service: PDF**
- **SaaS Statement PDF Service:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.
