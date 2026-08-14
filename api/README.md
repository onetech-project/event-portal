# API specification

[openapi.yml](openapi.yml) is the complete HTTP surface of the Go API — 61 operations across 42
paths, every route registered in [backend/cmd/api/main.go](../backend/cmd/api/main.go).

OpenAPI **3.0.3**, deliberately rather than 3.1: 3.0 is what every client tool imports without
argument, and this file exists to be imported.

## Import it

**Postman** — *Import* → *Files* → `openapi.yml`. Choose "Generate collection from this API" and
Postman builds a request per operation, with the example bodies filled in.

**Bruno** — *Import Collection* → *OpenAPI V3 Spec* → `openapi.yml`.

**Anything else** — it is a plain spec: `redocly preview-docs`, Swagger UI, `openapi-generator`,
or `curl` with the paths read straight off it.

After importing, set the base URL to your API origin (`http://localhost:8080` by default —
`APP_PORT` in [backend/.env](../backend/.env)) and get a token from `POST /api/v1/admin/login`
before calling anything under `/api/v1/admin/`.

## What it covers

| Group | |
| --- | --- |
| Health & Ops | `/healthz`, `/metrics`, admin cache flush |
| Public catalogue | events, ticket types, packages, Terms & Conditions |
| Booking & checkout | availability, book, agreement, order read, genders, checkout, QR PNG, SSE status |
| Public tickets | code lookup, guest resend |
| Payment gateway | the settlement webhook at `/v1.0/callback/exec` |
| Admin | auth, events, ticket types, packages, CMS content, orders, attendees, fees, ticket validation, payment ops |

Two security schemes, and they are not interchangeable: `adminAuth` is the JWT from
`/api/v1/admin/login`; `gatewayCallbackAuth` is `PG_CALLBACK_TOKEN`, which only the gateway
callback accepts.

## Reading it

- **Everything is enveloped.** Success is `{ code: 200000, message, data }` whatever the 2xx status
  is; failure carries an error code in the same shape. Responses are modelled as `allOf: [Envelope,
  { data: <the actual type> }]`, so the payload type is on `data`, not at the top level. The two
  exceptions are `/healthz` and the QR PNG.
- **Public routes take slugs, admin routes take UUIDs.** An order has both a public order number
  and an admin-side UUID; the path parameters say which one each operation wants.
- **Some 200s are refusals.** `POST /ticket/availability` answers "not available" with a 200 and a
  list of reasons, and ticket validation answers INVALID with a 200 — in both cases the question
  was well formed and correctly answered, and the client branches on the body.
- **Rate limits are part of the contract.** Booking, availability, checkout, ticket lookup and the
  guest resend each carry a 429 for their own per-IP budget.

## Keeping it honest

The spec is hand-maintained and is not generated from the Go code, so nothing enforces that it
stays true — changing a route or a DTO in `backend/` means changing this file in the same commit,
the same way [SCHEMA.md](../SCHEMA.md) travels with a migration.

It was verified against a running API: all 61 operations reach a handler, and the request bodies
were exercised end to end for every admin write path.
