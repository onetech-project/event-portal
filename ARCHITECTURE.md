# The AI MUST enforce the Modular Monolith pattern strictly.

**3.1. Directory Structure Rule**

All business logic MUST strictly follow this domain-driven directory structure. Do not use MVC layer-first grouping.
Plaintext

```
backend/
├── cmd/
│   └── api/
│       └── main.go                 # App entry point, Router setup, DI setup
├── internal/
│   ├── admin/                      # Domain: JWT Auth, Admin Management
│   ├── event/                      # Domain: Events & Ticket Types
│   ├── order/                      # Domain: Checkout, Order Items, Attendees
│   ├── payment/                    # Domain: Gateway interfaces, Webhooks
│   ├── ticket/                     # Domain: QR Code, Validation, Ticket Gen
│   └── notification/               # Domain: SMTP Email, PDF Maroto Gen
├── pkg/                            # Shared utilities (DB connection, config, logger)
└── migrations/                     # golang-migrate up/down pairs (also the sqlc source)
```

**3.2. Cross-Domain Communication (Strict Rule)**

Rule: Domains MUST NOT import repositories from other domains.

Rule: Domains MUST NOT execute SQL JOINs across tables belonging to different domains for Write/Update operations.

Solution: Use Interfaces. For example, if the order domain needs to check event quota, it must do so by calling an interface injected into OrderService.


```
Go

// Example inside internal/order/service.go
type EventProvider interface {
    CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int) error
}
```

**3.3. DTO Isolation**

Rule: sqlc generated database structs MUST NOT be leaked to the HTTP response. Every domain must have a dto.go defining the exact JSON shape.

**3.4. Critical Data Flows & Mitigations**

DB Transactions (Booking, spec 008): POST /api/v1/ticket/book MUST wrap the creation of orders, order_items, empty attendee slots, and atomic deduction of quota in a single SQL Transaction (BEGIN ... COMMIT). Pass pgx.Tx via context or explicitly to queries. POST /api/v1/ticket/checkout/:order_id saves the visitor forms in its own transaction and calls the payment gateway only after it commits.

Quota Restoration (Abandoned Cart): The webhook handler MUST listen for expire, cancel, or deny statuses. Upon receipt, update order status to Expired/Cancelled and atomically restore the quota in the ticket_types table.

Quota Re-deduction (Settle after expiry, spec 012): The one webhook outcome that takes quota rather than restoring it. A completed-payment notification for an order this system itself expired MUST settle it — the gateway can be asked to redeliver a notification, so a callback lost to an outage is recovered by replaying it rather than by a person recording the payment by hand. Settling re-deducts every ticket type the order holds inside the same transaction as the status change, all-or-nothing: if any type is short, nothing moves, the order stays EXPIRED, and the handler still answers 200 with a body naming the shortfall (a non-200 would burn the gateway's retry budget on an attempt guaranteed to fail identically). Because that 200 shares its status line with every ordinary acknowledgement, the refusal is carried by the envelope's numeric code — `200001` / `TICKETS_UNAVAILABLE` against `200000` — and not by the status; anything asserting on this endpoint must read the code. An order the *gateway* rejected or cancelled MUST NOT be settled this way — only this system's own expiry verdict is reversible. No path other than a gateway webhook may move an order to PAID.

Next.js Caching: Frontend fetch requests for Event Lists and Quotas MUST disable Next.js aggressive caching (e.g., cache: 'no-store').

Webhook Idempotency: Webhook handlers MUST check order status. If status == "PAID", immediately return 200 OK.

Async Processing & Visibility: Post-payment processing (PDF generation, QR creation, SMTP dispatch) MUST run in a non-blocking Go Goroutine. Update the email_sent = true flag upon successful email delivery. The webhook must return 200 OK instantly to the payment provider.

**3.5. Payment Gateway Abstraction**

The internal/payment domain must define an interface to allow swapping gateways without touching the order domain:

```
Go

type Gateway interface {
    CreateTransaction(order *dto.OrderInfo) (url string, err error)
    VerifyWebhook(payload []byte, signature string) (*dto.WebhookResult, error)
}
```

**3.6. Schema Migrations**

Rule: the schema is owned by `golang-migrate`, not by the application. The API
binary MUST NOT migrate on startup, and the runtime image does not ship the
migration files.

Every version is a pair — `{version}_{name}.up.sql` and `{version}_{name}.down.sql`
— and the `down` must genuinely reverse the `up`. Applied versions are recorded in
`schema_migrations`, which is what makes a migration run exactly once and lets a
database report the version it is on.

`docker compose up` runs a one-shot `migrate` service that the API `depends_on`
with `service_completed_successfully`, so the binary never serves against a schema
it was not generated for. `sqlc.yaml` points `schema:` at the directory: sqlc
replays the `up` files in order and ignores the `down` ones.

**3.7. Guest Flow**

The guest never authenticates. Everything below is reachable with an order number
or a ticket code alone, which is why the public reads are deliberately narrower
than the admin ones and the ticket lookup is rate limited per IP.

*Journey and decision points*

```mermaid
flowchart TD
    A["Guest opens the event grid at /"] --> B["GET /api/v1/event, then the detail<br/>at /event/:slug; the selection page reads<br/>GET /ticket/:slug + /packages/:slug —<br/>cache: no-store, so quota is never stale"]
    B --> C["Pick ticket types and bundles"]
    C --> D["Agree to the T&C in the booking dialog<br/>(GET /ticket/terms-condition/:slug)"]
    D --> E["POST /api/v1/ticket/book, then<br/>POST /ticket/terms-condition/:order_id"]

    E --> F{"Terms authored, on sale,<br/>and enough quota?"}
    F -- no --> G["409001 no authored terms,<br/>400 not-on-sale or insufficient quota"]
    G --> C
    F -- yes --> H["TX-B commits: quota deducted,<br/>order + items + EMPTY attendee slots,<br/>status = PENDING, 1-hour hold"]

    H --> I{"Forms submitted<br/>(POST /ticket/checkout/:order_id)<br/>and gateway created a QRIS session?"}
    I -- no --> J["No compensation: the hold and the<br/>saved forms are kept"]
    J --> K["502001 PAYMENT_INITIATION_FAILED —<br/>the guest retries from the same order"]
    K --> I
    I -- yes --> L["TX-P stamps qr_string and the<br/>14-minute payment_expires_at"]

    L --> M["Same order page flips to the QR phase.<br/>No redirect off-site: the QR is rendered<br/>from qr_string on demand"]
    M --> N["Poll GET /api/v1/ticket/order/:order_id every 3s<br/>+ SSE /ticket/checkout/:order_id/status,<br/>even while the tab is backgrounded"]

    N --> O{"Order status"}
    O -- "PENDING" --> P{"Past payment_expires_at?"}
    P -- no --> N
    P -- yes --> Q["Sweeper expires it: status → EXPIRED,<br/>quota returned to the pool"]
    Q --> R["QR hidden, guest can start a new order"]
    R --> C
    Q -.->|"callback was lost and the guest did pay —<br/>ops tops up quota and asks the gateway to RESEND,<br/>moving EXPIRED to PAID and re-taking the seats"| S
    O -- "PAID" --> S["Tickets issued and emailed<br/>off the request path"]
    S --> T["Guest can look up GET /api/v1/tickets/:code<br/>— rate limited, and answers with far less<br/>than the admin view"]
```

*Checkout — the reservation sequence*

The gateway round-trip sits deliberately **outside** TX1: the quota-deducting
`UPDATE` holds its row lock until commit, so a 200–500 ms provider call inside the
transaction would serialize every concurrent buyer of the same ticket type.

```mermaid
sequenceDiagram
    autonumber
    actor G as Guest
    participant FE as Next.js
    participant API as Echo router
    participant OS as order.Service
    participant EV as event.Service
    participant DB as PostgreSQL
    participant MJ as Manjo gateway

    G->>FE: Choose tickets, agree to the T&C
    FE->>API: POST /api/v1/ticket/book
    API->>OS: Book(req)
    OS->>OS: req.Validate() — reject before any I/O opens a transaction
    OS->>EV: CurrentTerms(eventID) — 409001 if none authored

    rect rgb(238, 244, 255)
    Note over OS,DB: TX-B — one transaction (§3.4)
    OS->>EV: TicketTypeForCheckout(tx, id)
    EV->>DB: SELECT price, name, sales window
    OS->>OS: Reject if outside the sales window
    OS->>EV: CheckAndDeductQuota(tx, id, qty)
    EV->>DB: UPDATE ticket_types SET quota = quota - qty WHERE quota >= qty
    Note right of DB: The guard is in the WHERE clause, not in Go.<br/>Zero rows means someone else took the last seat.<br/>The row lock is held to COMMIT.
    OS->>DB: INSERT orders (1-hour hold), order_items,<br/>EMPTY attendee slots
    Note over OS,DB: Total is recomputed from server-side prices —<br/>the client never sends one
    DB-->>OS: COMMIT
    end

    OS-->>FE: order_id, PENDING, expires_at
    FE->>API: POST /api/v1/ticket/terms-condition/:order_id
    FE->>G: Route in-app to /events/:slug/orders/:order_id

    G->>FE: Fill one holder form per ticket / bundle unit,<br/>Continue to Payment (no buyer form — spec 011)
    FE->>API: POST /api/v1/ticket/checkout/:order_id
    API->>OS: CheckoutOrder(orderID, forms)
    Note right of OS: The TOPMOST form's holder (first slot in<br/>canonical order) is the primary contact
    OS->>DB: TX-D — fill attendee slots (gender_id FK) +<br/>snapshot the primary contact into orders.buyer_*
    OS->>MJ: CreateTransaction — outside any TX, no lock held<br/>six fields, no requested expiry, reference = order number

    alt Gateway call fails transiently
        Note right of OS: Retried at most twice under the SAME reference —<br/>the gateway refuses a reference it has already<br/>issued a code for, so a retry can never create<br/>a second live code
        OS-->>FE: 502001 PAYMENT_INITIATION_FAILED
        Note right of OS: No compensation: the hold and the saved forms<br/>are kept, and the guest retries from the same order
    else Duplicate reference
        MJ-->>OS: error — a code exists that we will never hold
        OS->>DB: Release quota NOW + SESSION_DUPLICATE marker
        OS-->>FE: 409006 PAYMENT_SESSION_DUPLICATE — start again
        Note right of OS: Terminal. No call returns an existing code, so<br/>holding the seats to a deadline that cannot end<br/>in a payment would help nobody
    else QRIS session created
        MJ-->>OS: qr_r (payload), eri, qr_ea (the gateway's own expiry)
        OS->>DB: TX-P — UPDATE orders SET payment_qr_string,<br/>payment_expires_at = qr_ea (adopted verbatim)<br/>WHERE payment_qr_string IS NULL
        OS-->>FE: qr_string, expires_at, qr_image_url
    end
```

*Settlement — three paths racing for the same order*

A gateway notification and the expiry sweeper can reach the same order at once. They converge on one guarded transition — `UPDATE orders SET
status_id … WHERE status_id = <PENDING>` — which is what makes quota restoration
and ticket generation happen exactly once no matter who wins. (Migration 0013
made the column a reference to the `order_statuses` master list; callers still
pass and read the status NAME, which the SQL resolves. Still one atomic
statement, so the guarantee is unchanged.)

```mermaid
sequenceDiagram
    autonumber
    actor G as Guest
    participant FE as Next.js
    participant API as Echo router
    participant PS as payment.Service
    participant DB as PostgreSQL
    participant MJ as Manjo gateway
    participant TS as ticket.Service
    participant NS as notification.Service
    participant SW as Expiry sweeper

    par Guest watches the page
        FE->>API: GET /api/v1/ticket/checkout/:order_id/status (SSE)
        API-->>FE: snapshot, then a named keep-alive every 25s
        Note right of FE: No interval poll. The client watches the beat and<br/>falls back to periodic reads ONLY while the stream<br/>is failing — a proxy that accepts the connection and<br/>forwards nothing produces onopen and no error, so<br/>connection state alone reports health that is not there
    and Gateway notifies
        MJ->>API: POST /v1.0/callback/exec
        Note right of API: Mounted at the root, not under /api/v1:<br/>the path is fixed by the gateway's dispatch code
        API->>PS: HandleNotification
        PS->>PS: VerifyWebhook — bearer token compared in constant time
        Note right of PS: Only a bad token answers non-2xx.<br/>The gateway retries ANY other answer 3 times,<br/>10s apart, with a 5s timeout and no dead-letter —<br/>so unknown references, withdrawals, and<br/>unrecognised statuses are all recorded and 200'd
        PS->>DB: INSERT payments — the audit row is written first,<br/>even for answers that change nothing
        alt Order is already PAID
            PS-->>MJ: 200 OK — idempotent no-op, no second email
        else Still PENDING
            PS->>DB: TX — UPDATE orders SET status_id WHERE status_id = PENDING
            PS-->>MJ: 200 OK — returned before fulfillment runs
            PS->>TS: IssueTicketsForOrder — goroutine
            TS->>DB: INSERT tickets, one per attendee, idempotent
            PS->>NS: SendTicketEmail — goroutine
            NS->>NS: One email to orders.buyer_email (spec 011 FR-012):<br/>every ticket in the order as one PDF + the receipt.<br/>Holder emails are identity, never delivery targets
            NS->>NS: Render PDF and QRs from ticket_code, nothing stored on disk
            NS->>DB: UPDATE orders SET email_sent = true —<br/>only after that send succeeded
            Note right of NS: A failed send leaves the tickets valid and<br/>email_sent false — resend repeats the same<br/>single delivery to the buyer
        end
    and Nobody pays
        SW->>DB: DueForExpiry(now) — PENDING orders past their deadline
        SW->>DB: TX — status → EXPIRED, quota restored
        Note right of SW: Not the provider's expire notification alone:<br/>an abandoned cart nobody reopens would<br/>otherwise hold its seats forever
    end
```

**3.8. Admin Flow**

Every `/admin/*` route except login sits behind the JWT middleware. The frontend's
route guard is a convenience redirect only — the API rejects an unauthenticated
request whatever the browser chose to render.

*Journey and decision points*

```mermaid
flowchart TD
    A["Admin opens /admin/*"] --> B{"Session in localStorage?"}
    B -- no --> C["Redirect to /admin/login?next=…"]
    C --> D["POST /api/v1/admin/login"]
    D --> E{"Credentials valid?"}
    E -- no --> F["401 INVALID_CREDENTIALS —<br/>identical for unknown email and wrong password"]
    F --> C
    E -- yes --> G["JWT + expires_at stored;<br/>admin returns to where they were headed"]
    B -- yes --> H["Bearer token on every call"]
    G --> H

    H --> I{"What are they doing?"}

    I -- "Catalog" --> J["Events and ticket types CRUD"]
    J --> K{"Delete requested?"}
    K -- "has orders" --> L["409 — the order domain vetoes it<br/>through an interface, not a JOIN"]
    K -- "no orders" --> M["Deleted"]

    I -- "Sales" --> N["GET /admin/orders, /admin/attendees"]
    N --> O{"Email never arrived?"}
    O -- yes --> P["POST /admin/orders/:id/resend-email"]

    I -- "At the door" --> Q["Scan QR → POST /admin/tickets/validate"]
    Q --> R{"Result"}
    R -- "INVALID" --> S["Unknown or revoked code — refuse entry"]
    R -- "ALREADY_USED" --> T["Show when it was admitted — refuse entry"]
    R -- "VALID" --> U["Show attendee and event, admit"]
    U --> V["POST /admin/tickets/:code/use"]
    V --> W{"Guarded UPDATE applied?"}
    W -- yes --> X["Ticket → USED"]
    W -- no --> Y["409 ALREADY_USED —<br/>another door scanner won the race"]
```

*Login and door validation*

```mermaid
sequenceDiagram
    autonumber
    actor A as Admin
    participant FE as Next.js /admin
    participant API as Echo router
    participant AS as admin.Service
    participant TS as ticket.Service
    participant DB as PostgreSQL

    A->>FE: Submit email and password
    FE->>API: POST /api/v1/admin/login
    API->>AS: Login(req)
    AS->>DB: SELECT id, email, password_hash FROM admins WHERE email = $1

    alt No such admin
        AS->>AS: bcrypt against a fixed dummy hash
        Note right of AS: Burns the same work a real comparison would,<br/>so the response cannot be used to enumerate accounts
        AS-->>FE: 401 INVALID_CREDENTIALS
    else Password mismatch
        AS-->>FE: 401 INVALID_CREDENTIALS — byte-identical message
    else Valid
        AS->>AS: Issue JWT with the configured TTL
        AS-->>FE: token, expires_at
        FE->>FE: Persist to localStorage
        Note over FE: Resolved in an effect after mount — localStorage exists<br/>neither during SSR nor hydration, and redirecting from<br/>that first render is what used to bounce every refresh
    end

    Note over FE,API: Every later /admin/* call carries the bearer token.<br/>An expired one redirects back to login with reason=expired

    A->>FE: Scan a ticket QR at the door
    FE->>API: POST /api/v1/admin/tickets/validate
    API->>TS: Validate(code)
    TS->>TS: NormalizeCode — trim and upper-case
    TS->>DB: SELECT ticket + attendee + event by ticket_code

    alt Unknown code
        TS-->>FE: INVALID — a normal answer at a door, not a 500
    else Status REVOKED
        TS-->>FE: INVALID — no separate door action exists for it
    else Status USED
        TS-->>FE: ALREADY_USED, with when it was admitted
    else Status ACTIVE
        TS-->>FE: VALID, with attendee and event details
        A->>FE: Confirm entry
        FE->>API: POST /api/v1/admin/tickets/:code/use
        API->>TS: MarkUsed(code)
        TS->>DB: UPDATE tickets SET status = 'USED' WHERE ticket_code = $1 AND status = 'ACTIVE'
        Note right of DB: Single guarded UPDATE — two scanners hitting<br/>the same code cannot both be told "admitted"
        alt One row updated
            TS-->>FE: USED
        else Zero rows
            TS->>DB: Re-read the status to say why
            TS-->>FE: 409 ALREADY_USED, or 404 if the code is unknown
        end
    end
```

***