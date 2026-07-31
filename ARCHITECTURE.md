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
└── migrations/                     # SQL schemas (sqlc source)
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


***