# Master System Specification: Event Ticketing MVP
**Target Delivery:** 5 Days  
**Status:** 🔒 LOCKED

This document serves as the absolute source of truth for the AI coding agent. It contains the Product Requirements, Database Schema, and Architectural Guidelines.

---

## PART 1: Product Requirement Document (PRD)

### 1.1. Objective
Build an MVP web application that allows users to purchase event tickets as guest users, complete payment through a payment gateway, receive QR Code tickets via email, and provide a simple admin interface to manage events and orders. 

The goal of this MVP is validation, not scalability. High availability, microservices, Kafka, Kubernetes, Redis Cluster, and other enterprise components are intentionally out of scope. However, the backend must be built using a **Modular Monolith** architecture to ensure a seamless transition to microservices in the future.

### 1.2. Tech Stack
*   **Frontend:** Next.js (App Router), TypeScript, TailwindCSS, TanStack Query, React Hook Form, Zod
*   **Backend:** Golang 1.24+, **Echo v4**, PostgreSQL, **sqlc** (for type-safe SQL queries)
*   **Infrastructure:** Docker, Docker Compose (PostgreSQL)
*   **Payment:** Payment Gateway Abstraction (Initial: Midtrans SNAP Sandbox)
*   **Email & PDF:** SMTP (`go-mail/mail` or `net/smtp`), PDF Generator (`maroto` or `gofpdf`), QR Generator (`go-qrcode`).

### 1.3. User Roles
*   **Guest:** Browse events, purchase tickets, checkout without login, pay, receive tickets by email.
*   **Admin:** Login (JWT Auth), create/edit/delete events, manage ticket types, view orders/attendees, validate tickets, resend ticket emails.

### 1.4. Functional Requirements
**Event & Ticket Types:**
*   Admin can CRUD events (Name, Slug, Description, Venue, Address, Start/End Date, Banner, Status).
*   Admin can create multiple ticket types per event (Name, Price, Quota, Sales Start/End).
*   *Deletion Constraint:* Admin is strictly prohibited from deleting an `Event` or `Ticket Type` if there is at least one associated `Order`. API must return `400 Bad Request`.

**Checkout & Order:**
*   Guest selects quantities for multiple ticket types.
*   Guest enters Buyer Info and **Dynamic Attendee Info** (Name & Email per ticket).
*   System creates a unique `Order Number`, deducts quota safely, and returns a Payment URL.

**Payment & Delivery:**
*   Webhook updates Order status (`Pending` -> `Paid` / `Cancelled` / `Expired`).
*   On `Paid`: Backend generates 1 Ticket per Attendee (with unique Ticket Code + QR Code) and sends 1 Email to Buyer containing the PDF.

**Admin QR Validation:**
*   Admin validator accepts manual `Ticket Code` input as primary, or Camera QR scan as secondary.
*   Status lookup: `Valid`, `Already Used`, `Invalid`.
*   Admin can mark `Valid` tickets as `Used`.
*   Admin can manually trigger "Resend Ticket Email".

### 1.5. API Specification
**Public APIs** (spec 008 — every response wrapped in `{code, message, data}`)
*   `GET /api/v1/event`
*   `GET /api/v1/event/:slug` (content-only detail: description, activities, guest stars, guidelines)
*   `GET /api/v1/ticket/:event_slug` (sellable ticket types, live quota)
*   `GET /api/v1/packages/:event_slug`
*   `GET /api/v1/ticket/terms-condition/:event_slug`
*   `POST /api/v1/ticket/book` (creates the PENDING order + 1h hold)
*   `POST /api/v1/ticket/terms-condition/:order_id` (records the T&C agreement)
*   `POST /api/v1/ticket/checkout/:order_id` (saves visitor forms, opens the QRIS charge)
*   `POST /api/v1/ticket/checkout/:order_id/refresh-qr`
*   `GET /api/v1/ticket/checkout/:order_id/status` (SSE)
*   `GET /api/v1/ticket/order/:order_id` (+ `/qris.png`)
*   `POST /api/v1/ticket/order/:order_id/payment/refresh`
*   `POST /api/v1/ticket/resend-email` (`{order_id}` body)
*   `POST /api/v1/payment/webhook/:provider`
*   `GET /api/v1/tickets/:code`

**Admin APIs (JWT Protected)**
*   `POST /api/v1/admin/login`
*   `CRUD /api/v1/admin/events`
*   `CRUD /api/v1/admin/ticket-types`
*   `CRUD /api/v1/admin/packages`
*   `GET /api/v1/admin/packages/:id/availability`
*   `GET /api/v1/admin/orders`
*   `POST /api/v1/admin/orders/:id/resend-email`
*   `GET /api/v1/admin/attendees`
*   `POST /api/v1/admin/tickets/validate`

`GET /api/v1/packages/:event_slug` returns packages whose `available_units` and `purchasable` are both derived per request from the remaining quota of their constituent ticket types — a package stores no inventory of its own, so neither field is ever cached or persisted. `POST /api/v1/ticket/book` accepts a line referencing **either** a `ticket_type_id` or a `package_id`, never both.

A package request that carries any quota-like field (`quota`, `stock`, `inventory`, `remaining`, `capacity`) is **rejected**, not silently ignored: ignoring it would teach clients that packages hold stock.

### 1.6. Out of Scope
Microservices (deployment), Kafka/RabbitMQ, Redis, Kubernetes, CQRS, Event Sourcing, Loyalty Points, Leaderboard, Multi-Organizer, Refunds, Coupons, Promotions, Waiting Room, Queue System, Seat Selection, Multi-Currency, Multi-Language.

**Packages (bundles) are in scope and are not "Promotions".** A promotion is a rule that alters the price of a purchase; a package is a sellable product with its own identity, price, and sales window that draws down the quota of the ticket types it contains. The out-of-scope ban on promotions still stands: there are no discount codes, percentage-off rules, or price-modifying engines.

---