# Feature Specification: Persistent Admin Session & In-App QRIS Payment Page

**Feature Branch**: `004-session-qris-payment`

**Created**: 2026-08-01

**Status**: Draft

**Input**: User description: "1. Perbaiki masalah session untuk admin — tiap refresh halaman admin harus login lagi (session tidak persist). 2. Ganti alur pembayaran: alih-alih menampilkan halaman Snap Midtrans, tampilkan halaman detail order yang menampilkan QRIS bila status payment belum PAID, dengan timer expiry dan tombol check status; ketika pembayaran berhasil (webhook dari payment gateway diterima) halaman otomatis terupdate dan menunjukkan pembayaran berhasil."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Admin session survives a page refresh (Priority: P1)

An admin signs in once at the start of a shift and works across the admin area — events, orders, attendees, ticket validation — reloading pages, opening links in new tabs, and returning to the browser after a break. Their signed-in state is remembered until their session genuinely expires or they sign out; refreshing a page never sends them back to the login screen.

**Why this priority**: The current behaviour makes the whole admin area effectively unusable — validating tickets at a venue door means constant reloads, and each one forces a re-login. Every other admin feature already shipped (specs 002 and 003) is blocked by this defect.

**Independent Test**: Sign in, land on any admin page, press browser reload. The same page renders with the admin still signed in. Repeat on every admin route and after opening an admin URL directly in a new tab.

**Acceptance Scenarios**:

1. **Given** an admin signed in with a session that has not expired, **When** they reload any admin page, **Then** the page renders its content with the admin still signed in and no login screen appears at any point during loading.
2. **Given** an admin signed in in one tab, **When** they open an admin URL directly in a second tab, **Then** the second tab renders signed in without asking for credentials.
3. **Given** an admin whose session has expired, **When** they open or reload an admin page, **Then** they are sent to the login screen and told their session expired.
4. **Given** a signed-in admin, **When** they choose sign out, **Then** they are returned to the login screen, and reloading any admin page afterwards keeps them at the login screen.
5. **Given** an admin signed in in two tabs, **When** they sign out in one tab, **Then** the other tab stops showing admin content on its next navigation or refresh.
6. **Given** a visitor who has never signed in, **When** they open an admin URL directly, **Then** they are sent to the login screen and, after signing in, arrive at the admin area.

---

### User Story 2 - Guest pays with QRIS on the order page (Priority: P1)

After submitting checkout, a guest stays on the site and lands on an order detail page that shows what they bought, what they owe, and — because the order is not paid yet — a QRIS code they can scan with any Indonesian banking or e-wallet app. A countdown shows how long the code is valid. The guest never leaves the site for an external payment page.

**Why this priority**: This is the core of the requested change. It replaces the redirect to the payment provider's hosted page with an on-site payment experience, and it is a prerequisite for the live status updates in User Story 3.

**Independent Test**: Complete checkout as a guest and confirm the browser stays on the site, shows an order detail page with order number, items, buyer details, total, an unpaid status, a scannable QRIS code, and a countdown that decreases.

**Acceptance Scenarios**:

1. **Given** a guest who has just completed checkout, **When** the order is created, **Then** they land on the order detail page for that order without being redirected to an external payment site.
2. **Given** an order that is not yet paid, **When** the order detail page loads, **Then** it shows the order number, purchased ticket types with quantities, buyer name/email, total amount, an "awaiting payment" status, a scannable QRIS code, and the amount to pay.
3. **Given** an unpaid order with a payment deadline in the future, **When** the page is open, **Then** a countdown shows the remaining time and decreases every second.
4. **Given** an unpaid order, **When** the guest reloads the page or reopens its URL later on any device, **Then** the same QRIS code and the correctly-recalculated remaining time are shown.
5. **Given** an order that is already paid, **When** the order detail page loads, **Then** no QRIS code and no countdown are shown — only the paid confirmation.
6. **Given** an order number that does not exist, **When** someone opens its detail page, **Then** a clear "order not found" message is shown and no order data is revealed.

---

### User Story 3 - Page updates itself the moment payment succeeds (Priority: P1)

The guest scans the QRIS code and pays in their banking app. Within seconds of the payment provider notifying the system, the order page the guest is still looking at switches by itself to a paid confirmation — no manual reload. A "check payment status" button is also available for guests who would rather confirm on demand.

**Why this priority**: Without this, the QRIS page is a dead end — the guest pays and has no idea whether it worked. It is the payoff of User Story 2 and ships with it as the same MVP slice.

**Independent Test**: With the order page open, deliver a successful payment notification for that order, then observe the page switch to the paid state without any user interaction. Separately, press "check payment status" on an unpaid order and confirm it reports the current status.

**Acceptance Scenarios**:

1. **Given** a guest with the order page open for an unpaid order, **When** the payment provider notifies the system that the payment succeeded, **Then** the page replaces the QRIS code and countdown with a payment-successful confirmation within 10 seconds, with no user action.
2. **Given** the paid confirmation is shown, **When** the guest reads it, **Then** it states that tickets are being emailed to the buyer address, shows that address, and offers a way to browse more events.
3. **Given** an unpaid order, **When** the guest presses "check payment status", **Then** the page reports the current status — still awaiting payment, paid, or expired — and shows a clear result for that press even when the status has not changed.
4. **Given** the guest presses "check payment status" repeatedly, **When** presses come faster than the system permits, **Then** the button is temporarily disabled with a visible cooldown instead of failing.
5. **Given** the order page is open, **When** the connection to the server is lost, **Then** the page shows that live updating is interrupted and keeps retrying, and it recovers automatically when the connection returns.
6. **Given** the payment provider sends the same success notification more than once, **When** each arrives, **Then** the order stays paid, exactly one set of tickets is issued, and exactly one confirmation email is sent.

---

### User Story 4 - Expired or failed payment is handled cleanly (Priority: P2)

If the guest does not pay in time, or the payment is declined, the page tells them plainly and stops presenting a code that no longer works. The tickets they were holding go back on sale for everyone else.

**Why this priority**: Needed for correctness of inventory and to avoid guests scanning dead codes, but the happy path (Stories 2 and 3) delivers value first.

**Independent Test**: Open an unpaid order whose deadline has passed and confirm the page shows an expired state with no QRIS code, and that the ticket quota released back to the event.

**Acceptance Scenarios**:

1. **Given** an unpaid order with an open page, **When** the countdown reaches zero, **Then** the page switches to an expired state, hides the QRIS code, and explains the order can no longer be paid.
2. **Given** an order whose payment expired, **When** the system records the expiry, **Then** the order status becomes expired and the reserved ticket quota is returned to the ticket types.
3. **Given** an expired or cancelled order, **When** the guest opens its detail page, **Then** the status is shown and they are offered a way to start a new order for the same event.
4. **Given** a payment the provider reports as failed, cancelled, or denied, **When** the notification arrives, **Then** the page shows the payment did not succeed and the quota is returned.
5. **Given** an order whose payment deadline has passed, **When** a late success notification arrives for it, **Then** the system records the outcome consistently — the order does not end up both expired and holding no quota while the guest has been charged; the discrepancy is surfaced to admins on the orders list.

---

### Edge Cases

- **Session at the boundary**: an admin whose session expires while a page is open — the next admin action reports the expiry and sends them to login rather than failing silently.
- **Tampered stored session**: a corrupted or hand-edited stored session is treated as no session at all, with a clean redirect to login and no error page.
- **Admin returns after long absence**: reopening a laptop after the session lifetime has passed shows the login screen, not a broken admin page.
- **Slow payment code issuance**: if the QRIS code is not ready the instant the guest lands on the order page, the page shows a waiting state and displays the code as soon as it exists, rather than showing an error.
- **Payment code could not be issued at all**: the guest is told the payment could not be started, nothing is charged, the held quota is released, and they can retry from the event page.
- **Guest closes the page mid-payment**: reopening the order URL restores the same code and remaining time; payment already completed shows the paid state.
- **Clock differences**: the countdown reflects the server's deadline, so a device with a wrong clock does not show a wildly wrong remaining time.
- **Order page shared or guessed**: order URLs are only useful to someone holding the order number, and the page never exposes payment credentials or admin-only data.
- **Notification arrives while the guest is offline**: on reconnecting, the page shows the up-to-date status.
- **Zero-amount or already-paid order revisited**: no QRIS code is issued or displayed for an order that is not awaiting payment.

## Requirements *(mandatory)*

### Functional Requirements

#### Admin session persistence

- **FR-001**: The system MUST keep an admin signed in across full page reloads, direct URL entry, browser back/forward navigation, and new tabs, for the entire lifetime of their session.
- **FR-002**: While the system determines whether a stored session is valid on load, it MUST NOT display the login screen or navigate the admin away from the page they requested; it MUST show a neutral loading state until the decision is made.
- **FR-003**: After signing in, the admin MUST be taken to the admin area, and MUST land on the admin page they originally requested when they arrived via a direct URL.
- **FR-004**: The system MUST end the session and require a new sign-in when the session lifetime has elapsed, when the admin signs out, or when the server rejects the session as invalid.
- **FR-005**: When a session ends, the system MUST tell the admin why (expired versus signed out) on the login screen.
- **FR-006**: Signing out in one tab MUST take effect in other open tabs on their next navigation or refresh.
- **FR-007**: An unreadable, incomplete, or tampered stored session MUST be discarded and treated as "not signed in", without an error page.
- **FR-008**: The admin session MUST remain a convenience on the client only — every admin capability MUST continue to be enforced by the server on each request, so a forged client-side session grants no access.

#### On-site order detail and QRIS payment

- **FR-009**: On successful checkout, the system MUST take the guest to the order detail page for the new order instead of redirecting them to any external payment page.
- **FR-010**: The order detail page MUST be reachable by order number without an account, and MUST show: order number, event name, ticket types with quantities and prices, buyer name and email, total amount, and current payment status.
- **FR-011**: When an order is awaiting payment, the system MUST issue a QRIS payment code for the order amount and display it on the order detail page in a form scannable by standard Indonesian QRIS-compatible apps.
- **FR-012**: The system MUST show the payment deadline as a live countdown, computed from a server-supplied deadline so it stays correct regardless of the device clock, and MUST show the exact amount to be paid alongside the code.
- **FR-013**: The system MUST reuse the same payment code and deadline for an order across reloads and devices for as long as the code is valid — reopening the page MUST NOT create a new payment attempt.
- **FR-014**: The system MUST NOT display a QRIS code, countdown, or check-status control for an order that is paid, expired, or cancelled.
- **FR-015**: If the payment code cannot be issued, the system MUST tell the guest payment could not be started, state that nothing was charged, release the held ticket quota, and offer a path to retry.

#### Live status updates and manual check

- **FR-016**: While an order is awaiting payment and its page is open, the system MUST update the displayed status without user action within 10 seconds of the payment provider's notification being processed.
- **FR-017**: The order detail page MUST offer a "check payment status" control that re-reads the authoritative order status on demand and always gives visible feedback, including when nothing changed.
- **FR-018**: The system MUST limit how frequently a single client can request a status check and MUST communicate the wait to the guest rather than returning an error.
- **FR-019**: On confirmed payment, the page MUST show a success state that replaces the code and countdown, and MUST state that tickets are being emailed to the buyer's address.
- **FR-020**: Automatic status updates MUST stop once the order reaches a final state (paid, expired, or cancelled), so a settled page places no further load on the system.
- **FR-021**: When live updating is interrupted, the page MUST show that it is not currently live, keep retrying, and recover on its own when the connection returns.
- **FR-022**: The publicly reachable order status MUST expose only what the guest needs — order, items, amount, status, payment code, and deadline — and MUST NOT expose ticket codes, other orders, or any admin-only data.

#### Payment lifecycle

- **FR-023**: The system MUST record a payment deadline for every order awaiting payment and MUST treat an order past its deadline as expired.
- **FR-024**: On expiry, cancellation, denial, or failure of a payment, the system MUST return the reserved ticket quota to the affected ticket types exactly once, no matter how many notifications arrive.
- **FR-025**: Repeated notifications for an order that is already paid MUST leave it paid, MUST NOT issue duplicate tickets, and MUST NOT send duplicate emails.
- **FR-026**: The system MUST continue to issue tickets and email them to the buyer after payment succeeds, without delaying either the provider's notification response or the guest's page update.
- **FR-027**: Payment notifications MUST continue to be accepted only when their authenticity is verified; unverifiable notifications MUST change nothing.
- **FR-028**: The system MUST record every payment notification it processes, including outcome, so an admin can reconstruct what happened to an order.

### Key Entities

- **Admin Session**: An admin's proof of having signed in, with an issue time and an expiry the client can read. Survives page reloads; ends on expiry, sign-out, or server rejection.
- **Order**: An existing entity — buyer, items, attendees, total, and status (awaiting payment, paid, cancelled, expired). Gains a payment deadline so the countdown and expiry are well-defined.
- **Payment Instruction**: The QRIS code content plus the amount and deadline for one order's payment attempt. Belongs to exactly one order, is stable while valid, and is not shown once the order reaches a final state.
- **Payment Notification**: An authenticated message from the payment provider reporting an outcome for an order. Recorded for audit; processed at most once in effect.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An admin can reload any admin page an unlimited number of times within their session lifetime and is asked to sign in zero times.
- **SC-002**: 100% of admin routes, opened directly by URL while signed in, render their content without a visible flash of the login screen.
- **SC-003**: A guest completing checkout reaches a page displaying a scannable payment code in under 5 seconds, without leaving the site.
- **SC-004**: 95% of guests who pay see the success state appear on their open page within 10 seconds of paying, with no manual refresh.
- **SC-005**: The remaining-time countdown shown to the guest is within 2 seconds of the true deadline regardless of the device's clock setting.
- **SC-006**: Reopening an unpaid order's page any number of times produces the same payment code and never creates a second payment attempt for that order.
- **SC-007**: Reserved ticket quota is returned within 1 minute of an order expiring or failing, and total quota never drifts from expected after repeated or duplicate provider notifications.
- **SC-008**: Duplicate payment notifications result in exactly one set of tickets and exactly one confirmation email per order, verified across at least 3 repeated deliveries.
- **SC-009**: Support contacts about "I paid but the page didn't change" drop to zero in testing across the supported browsers.

## Assumptions

- **Session mechanics unchanged in kind**: the fix restores persistence of the existing admin sign-in mechanism and its server-side enforcement; it does not introduce new account types, roles, password reset, or multi-factor sign-in. The existing 12-hour session lifetime is kept as-is, and no idle timeout is added.
- **Single payment method**: replacing the provider's hosted checkout means QRIS is the only payment method offered in this flow. Card, virtual account, and e-wallet redirect methods that the hosted page previously offered are intentionally dropped.
- **Payment deadline default**: 15 minutes from order creation, matching common QRIS practice. It is a configurable value, not hard-coded.
- **The payment provider remains the source of truth**: the system does not decide a payment succeeded on its own; a verified provider notification (or a provider status read triggered by the check-status control) is required.
- **The provider can issue QRIS codes**: the configured gateway supports generating a QRIS payment code with an explicit expiry, and can deliver it to the system at order-creation time.
- **Codes are rendered on demand**: consistent with the project's no-object-storage constraint, the QRIS image is produced from the code content when the page is rendered rather than stored as a file.
- **Order URLs are unguessable enough**: order numbers are the only credential needed to view an order's public status; no separate access token is introduced for this MVP.
- **Existing purchase flow is unchanged upstream**: event browsing, ticket selection, attendee entry, quota deduction during checkout, ticket generation, and the emailed PDF all keep their current behaviour.
- **No refunds, retries, or partial payments**: an expired order is not revived; the guest starts a new order. This stays within the MVP scope boundaries.
- **Scope of the countdown**: the countdown is a display of the server's deadline; expiry itself is decided server-side, so a page left open past zero cannot be used to pay.

## Dependencies

- The payment provider's QRIS capability and its webhook notifications must be available in the sandbox environment used for development.
- The existing webhook endpoint, quota-restoration logic, ticket generation, and email delivery built in earlier features are reused rather than rebuilt.
- The existing admin sign-in endpoint and server-side authorization on admin routes are reused unchanged.

## Out of Scope

- New payment methods beyond QRIS (cards, virtual accounts, e-wallet redirects).
- Refunds, partial payments, coupons, or promotions.
- Reviving or extending an expired order's payment window.
- Admin-initiated payment retries or manual payment marking.
- Guest accounts, order history, or authenticated order lookup.
