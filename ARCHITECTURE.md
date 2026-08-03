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

DB Transactions (Checkout): POST /api/v1/checkout MUST wrap the creation of orders, order_items, attendees, and atomic deduction of quota in a single SQL Transaction (BEGIN ... COMMIT). Pass pgx.Tx via context or explicitly to queries.

Quota Restoration (Abandoned Cart): The webhook handler MUST listen for expire, cancel, or deny statuses. Upon receipt, update order status to Expired/Cancelled and atomically restore the quota in the ticket_types table.

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
    A["Guest opens /events"] --> B["GET /api/v1/events — cache: no-store,<br/>so remaining quota is never stale"]
    B --> C["Pick an event and its ticket types"]
    C --> D["Fill buyer details and one attendee per ticket"]
    D --> E["POST /api/v1/checkout"]

    E --> F{"On sale, and enough quota?"}
    F -- no --> G["400 TICKET_TYPE_NOT_ON_SALE<br/>or INSUFFICIENT_QUOTA"]
    G --> C
    F -- yes --> H["TX1 commits: quota deducted,<br/>order + items + attendees written,<br/>status = PENDING"]

    H --> I{"Gateway created a QRIS session?"}
    I -- no --> J["Compensating TX: order → CANCELLED,<br/>quota restored"]
    J --> K["502 PAYMENT_INITIATION_FAILED —<br/>safe to retry, nothing is held"]
    K --> C
    I -- yes --> L["TX2 stamps qr_string<br/>and payment_expires_at"]

    L --> M["In-app route to the order page.<br/>No redirect off-site: the QR is rendered<br/>from qr_string on demand"]
    M --> N["Poll GET /api/v1/orders/:orderNumber every 3s,<br/>even while the tab is backgrounded"]

    N --> O{"Order status"}
    O -- "PENDING" --> P{"Past payment_expires_at?"}
    P -- no --> N
    P -- yes --> Q["Sweeper, or the guest's own refresh,<br/>expires it: status → EXPIRED,<br/>quota returned to the pool"]
    Q --> R["QR hidden, guest can start a new order"]
    R --> C
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
    participant MT as Midtrans Core API

    G->>FE: Choose tickets, fill buyer and attendees
    FE->>API: POST /api/v1/checkout
    API->>OS: Checkout(req)
    OS->>OS: req.Validate() — reject before any I/O opens a transaction

    rect rgb(238, 244, 255)
    Note over OS,DB: TX1 — one transaction (§3.4)
    OS->>EV: TicketTypeForCheckout(tx, id)
    EV->>DB: SELECT price, name, sales window
    OS->>OS: Reject if outside the sales window
    OS->>EV: CheckAndDeductQuota(tx, id, qty)
    EV->>DB: UPDATE ticket_types SET quota = quota - qty WHERE quota >= qty
    Note right of DB: The guard is in the WHERE clause, not in Go.<br/>Zero rows means someone else took the last seat.<br/>The row lock is held to COMMIT.
    OS->>DB: INSERT orders, order_items, attendees
    Note over OS,DB: Total is recomputed from server-side prices —<br/>the client never sends one
    DB-->>OS: COMMIT
    end

    OS->>MT: CreateTransaction — outside TX1, no lock held

    alt Provider call fails
        OS->>DB: Compensating TX — order → CANCELLED, quota restored
        Note right of OS: Guarded on the order still being PENDING,<br/>so a webhook that beat us here wins instead
        OS-->>FE: 502 PAYMENT_INITIATION_FAILED
    else QRIS session created
        MT-->>OS: qr_string, expiry
        OS->>DB: TX2 — UPDATE orders SET payment_qr_string, payment_expires_at
        OS-->>FE: order_number, PENDING, total
        FE->>G: Route in-app to /orders/:orderNumber
    end
```

*Settlement — three paths racing for the same order*

A webhook, the guest's refresh button, and the expiry sweeper can all reach the
same order at once. They converge on one guarded transition — `UPDATE orders SET
status … WHERE status = 'PENDING'` — which is what makes quota restoration and
ticket generation happen exactly once no matter who wins.

```mermaid
sequenceDiagram
    autonumber
    actor G as Guest
    participant FE as Next.js
    participant API as Echo router
    participant PS as payment.Service
    participant DB as PostgreSQL
    participant MT as Midtrans
    participant TS as ticket.Service
    participant NS as notification.Service
    participant SW as Expiry sweeper

    par Guest watches the page
        loop Every 3s until the status is final
            FE->>API: GET /api/v1/orders/:orderNumber
            API-->>FE: status, total, payment.expires_at
        end
        G->>FE: Press "check payment status"
        FE->>API: POST /api/v1/orders/:orderNumber/payment/refresh
        Note right of API: Rate limited to ~1 press per 5s per IP:<br/>each one costs an outbound provider call
        API->>PS: RefreshStatus
        PS->>MT: FetchStatus
        MT-->>PS: transaction_status
        PS->>PS: applyProviderResult — the same path a webhook takes
    and Provider notifies
        MT->>API: POST /api/v1/payment/webhook/midtrans
        API->>PS: HandleNotification
        PS->>PS: VerifyWebhook — SHA-512 over the body's signature_key,<br/>compared in constant time
        Note right of PS: Only a bad signature answers non-2xx.<br/>Unknown orders and unrecognized statuses are<br/>acknowledged and logged, or the provider<br/>retries something we can never process
        PS->>DB: INSERT payments — the audit row is written first,<br/>even for answers that change nothing
        alt Order is already PAID
            PS-->>MT: 200 OK — idempotent no-op, no second email
        else Still PENDING
            PS->>DB: TX — UPDATE orders SET status WHERE status = 'PENDING'
            PS-->>MT: 200 OK — returned before fulfillment runs
            PS->>TS: IssueTicketsForOrder — goroutine
            TS->>DB: INSERT tickets, one per attendee, idempotent
            PS->>NS: SendTicketEmail — goroutine
            NS->>NS: Render PDF and QR from ticket_code, nothing stored on disk
            NS->>DB: UPDATE orders SET email_sent = true
            Note right of NS: Delivery failure leaves the tickets valid<br/>and email_sent false — an admin resends
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