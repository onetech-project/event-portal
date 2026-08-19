# Phase 1 Contracts: Admin Console Pagination

Authoritative contract remains [`api/openapi.yml`](../../../api/openapi.yml); this document
is the delta to apply there and the reference for implementers. Field semantics and
clamping rules live in [data-model.md](../data-model.md) and are not repeated.

All five endpoints below require `adminAuth` and are unchanged in that respect: an
unauthenticated request still gets **401**, for any page (FR-018).

---

## Shared schemas to add to `components.schemas`

### `PageMeta`

```yaml
PageMeta:
  type: object
  required: [page, page_size, total, total_pages]
  properties:
    page:        { type: integer, minimum: 1, description: The page actually served, after clamping. }
    page_size:   { type: integer, minimum: 1, maximum: 100 }
    total:       { type: integer, format: int64, minimum: 0, description: Rows matching the filters, ignoring paging. }
    total_pages: { type: integer, minimum: 0, description: 0 when total is 0. }
```

### Shared parameters to add to `components.parameters`

```yaml
PageParam:
  name: page
  in: query
  required: false
  description: >
    1-based page number. Out-of-range, negative, zero and unparseable values are
    corrected to the nearest valid page rather than rejected; a page beyond the last
    resolves to the last page. Inspect the returned `page` to see what was served.
  schema: { type: integer, minimum: 1, default: 1 }

PageSizeParam:
  name: page_size
  in: query
  required: false
  description: >
    Rows per page. Values above the maximum are reduced to it; values below 1 and
    unparseable values fall back to the default. Never rejected.
  schema: { type: integer, minimum: 1, maximum: 100, default: 20 }
```

Each paged response is expressed in the project's existing `allOf` style, with `data`
becoming an object rather than an array:

```yaml
allOf:
  - $ref: '#/components/schemas/Envelope'
  - type: object
    properties:
      data:
        allOf:
          - $ref: '#/components/schemas/PageMeta'
          - type: object
            required: [items]
            properties:
              items:
                type: array
                items: { $ref: '#/components/schemas/OrderSummary' }   # per endpoint
```

---

## 1. `GET /api/v1/admin/orders` — MODIFIED

`api/openapi.yml` line ~1572.

- **Added parameters**: `PageParam`, `PageSizeParam`.
- **Unchanged parameters**: `status` (enum `PENDING|PAID|CANCELLED|EXPIRED`), `event_id`
  (uuid). Both optional, both combine, and both still return **400** when malformed — the
  asymmetry with paging params is deliberate ([research.md R8](../research.md)).
- **Changed response**: `data` was `OrderSummary[]`; it is now `PageMeta & { items: OrderSummary[] }`.
- **Ordering**: `created_at DESC, id DESC`.
- **Note**: `event_id` is resolved to the event's ticket-type ids before matching, and the
  count applies the same resolution. An event with no ticket types yields
  `total: 0, items: []`, not an unfiltered count.

Example:

```http
GET /api/v1/admin/orders?status=PAID&page=2&page_size=20
```

```json
{
  "code": 200000,
  "message": "Success",
  "data": {
    "items": [ { "id": "…", "order_number": "…", "status": "PAID", "…": "…" } ],
    "page": 2,
    "page_size": 20,
    "total": 137,
    "total_pages": 7
  }
}
```

---

## 2. `GET /api/v1/admin/attendees` — MODIFIED

`api/openapi.yml` line ~1611.

- **Added parameters**: `PageParam`, `PageSizeParam`.
- **Unchanged parameters**: `order_id` (uuid — the order UUID, not the order number),
  `event_id` (uuid).
- **Changed response**: `data` was `AttendeeSummary[]`; now `PageMeta & { items: AttendeeSummary[] }`.
- **Ordering**: `orders.created_at DESC, attendees.name ASC, attendees.id ASC`.
- **Note**: ticket-type names are still resolved through the injected `EventLookup` in one
  batched call — now for the page's rows only, which makes that lookup strictly cheaper
  than it is today.

---

## 3. `GET /api/v1/admin/events` — MODIFIED

`api/openapi.yml` line ~742.

- **Added parameters**: `PageParam`, `PageSizeParam`.
- **Changed response**: `data` was `EventAdminView[]`; now `PageMeta & { items: EventAdminView[] }`.
- **Ordering**: `start_date DESC, id DESC`.
- **Breaking-change note for implementers**: this is the endpoint that also fed the event
  filter dropdown on the Orders and Attendees pages. Those callers move to
  `/admin/events/options` (§5) in the same change. Any caller left on this endpoint expecting
  the whole catalogue will now silently see one page — that is the defect §5 exists to
  prevent, so check for other callers before merging.

---

## 4. `GET /api/v1/admin/fees` — MODIFIED

`api/openapi.yml` line ~1684.

- **Added parameters**: `PageParam`, `PageSizeParam`.
- **Changed response**: `data` was `FeeAdminView[]`; now `PageMeta & { items: FeeAdminView[] }`.
- **Ordering**: `position, name, id`.
- **Unchanged**: `POST`, `PUT /{id}`, `DELETE /{id}` keep their current request and response
  shapes exactly. Only the collection `GET` changes.

---

## 5. `GET /api/v1/admin/events/options` — NEW

For filter dropdowns. Deliberately not paginated and deliberately tiny.

```yaml
/api/v1/admin/events/options:
  get:
    tags: [Admin - Events]
    summary: Every event as an id/name pair, for filter selectors
    operationId: adminListEventOptions
    security: [{ adminAuth: [] }]
    description: >
      The full catalogue in selector form, ordered by name. Not paginated: a filter that
      could only name the first page of events would be a filter that quietly lies. Kept
      to two columns so that staying unpaginated is cheap.
    responses:
      '200':
        description: Every event, as id and name.
        content:
          application/json:
            schema:
              allOf:
                - $ref: '#/components/schemas/Envelope'
                - type: object
                  properties:
                    data:
                      type: array
                      items: { $ref: '#/components/schemas/EventOption' }
      '401':
        $ref: '#/components/responses/Unauthorized'
```

```yaml
EventOption:
  type: object
  required: [id, name]
  properties:
    id:   { type: string, format: uuid }
    name: { type: string }
```

**Route placement**: mount `/admin/events/options` **before** any `/admin/events/:id`
route in the Echo group, or `options` will be parsed as an event id and fail UUID
validation.

---

## 6. Frontend consumer contract

`frontend/lib/queries.ts`:

| Hook | Before | After |
|---|---|---|
| `useAdminOrders` | `(status?, eventId?)` → `OrderSummary[]` | `(params: ListParams)` → `Page<OrderSummary>` |
| `useAdminAttendees` | `(orderId?, eventId?)` → `AttendeeSummary[]` | `(params: ListParams)` → `Page<AttendeeSummary>` |
| `useAdminEvents` | `()` → `EventAdminView[]` | `(params: ListParams)` → `Page<EventAdminView>` |
| `useAdminFees` | `()` → `FeeAdminView[]` | `(params: ListParams)` → `Page<FeeAdminView>` |
| `useAdminEventOptions` | — | `()` → `EventOption[]` *(new)* |

Query keys must include page and page size, or TanStack Query will serve page 1's rows for
page 2. The existing mutation invalidations key off the list root and continue to work
unchanged.

---

## 7. What does not change

Stated explicitly so a reviewer can confirm the blast radius:

- The `{code, message, data}` envelope and every numeric code in the registry.
- Every guest-facing endpoint, including the public event, ticket-type and package lists.
- Admin authentication, the JWT middleware, and every authorization rule.
- Ticket validation, the `USED` transition, cache refresh, payment notifications and holds.
- All fee, event, ticket-type and package **write** endpoints.
- Every database table, column, and index — there is no migration
  ([research.md R9](../research.md)).
