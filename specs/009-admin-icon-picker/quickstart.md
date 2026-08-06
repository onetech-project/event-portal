# Quickstart: Validating the Admin Visual Icon Picker

**Feature**: 009-admin-icon-picker | Contracts: [content-icon-contract.md](contracts/content-icon-contract.md)

## Prerequisites

- Docker + Docker Compose (PostgreSQL): `docker compose up -d` from repo root
- Backend: Go 1.24+ — run from `backend/`
- Frontend: Node + npm — run from `frontend/`
- An admin account and at least one event (seed or create via the admin UI)

## Setup / regeneration

```bash
# 1. Install the picker component (implementation step, one-time)
cd frontend && npx shadcn@latest add "https://icon-picker.alan-courtois.fr/r/icon-picker"

# 2. (Re)generate the catalog file the backend embeds — required after any
#    lucide-react version change, and once during implementation
node scripts/generate-icon-keys.mjs
git diff --stat ../backend/internal/event/iconkeys/icon_keys.txt  # expect ~2,007 lines
```

## Automated validation

```bash
# Backend — validation contract, legacy keys, catalog sanity
cd backend && go test ./internal/event/...

# Frontend — catalog sync, ContentIcon fallback, IconField interaction
cd frontend && npx vitest run
```

Expected: all green. The catalog-sync vitest test failing means `icon_keys.txt` is out
of date — re-run the generation script.

## Manual validation scenarios

Start both apps (`go run ./cmd/...` in `backend/`, `npm run dev` in `frontend/`), log
into the admin panel, open an event's **Content** tab.

**US1 — pick visually (P1)**
1. In "Add activity", open the Icon field → a grid of icon **glyphs** with names
   appears (not a text list).
2. Click a glyph → trigger shows the chosen glyph; save the activity → it appears in
   the list with that glyph.
3. Create another activity choosing "no icon" → saves cleanly, no glyph shown.
4. Edit flow: an existing pre-feature block (e.g. icon `music`) shows that icon as the
   current selection when its form is opened.

**US2 — search the broad catalog (P2)**
1. Open the picker, type `anch` → grid narrows as you type; select `anchor` (an icon
   outside the legacy 20) → save succeeds (no 400004).
2. Type gibberish (`zzzz`) → "no icons match" message, not a blank panel.
3. Clear the search → full catalog browsable again.
4. Open the public event page → the `anchor` glyph renders on the guest content
   section.

**US3 — glyphs in admin lists (P3)**
- The activities and guidelines lists show real glyphs where `(music)`-style text
  suffixes used to be; blocks without icons show nothing extra.

**Edge — crafted request still refused (FR-006)**

```bash
curl -s -X POST "http://localhost:8080/api/v1/admin/events/<EVENT_ID>/activities" \
  -H "Authorization: Bearer <ADMIN_TOKEN>" -H "Content-Type: application/json" \
  -d '{"title":"X","description":"Y","icon":"definitely-not-real","position":0}'
# Expect: HTTP 400, {"code":400004,...}
```

**Edge — keyboard only (SC-006)**
- Tab to the Icon field trigger → Enter opens → type to search → Tab/arrow to a glyph →
  Enter selects → popover closes with selection applied; Tab reaches the clear button
  ("Remove icon") when a value is set.

**Performance spot-check (SC-005 / FR-011)**
- First open of the picker completes < 2 s on a dev machine; typing filters visibly
  < 1 s. The admin content page's network tab shows picker chunks loading only on first
  open, not with the page.
