# Feature Specification: Admin Visual Icon Picker

**Feature Branch**: `009-admin-icon-picker`

**Created**: 2026-08-05

**Status**: Draft

**Input**: User description: "currently the icon picker on the admin uses default text, i want to implement an icon picker, try to search any library, you can use lucide or react-icons for broader option"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Pick an icon visually (Priority: P1)

An admin managing an event's content (activities and guidelines) currently chooses an icon from a plain text dropdown listing raw identifier strings such as "cup-soda" or "gamepad-2". They must guess what each identifier looks like. With this feature, the admin opens a visual picker that displays the actual icon glyphs, sees at a glance what each option looks like, and selects the one that fits their content block.

**Why this priority**: This is the core of the request — replacing text-only selection with visual selection. Without it, nothing else in this feature delivers value.

**Independent Test**: Can be fully tested by opening the activity or guideline form in the admin event content page, opening the icon field, visually browsing icon glyphs, selecting one, saving the block, and confirming the saved block carries that icon.

**Acceptance Scenarios**:

1. **Given** an admin is creating a new activity, **When** they open the icon field, **Then** they see a visual grid of icon glyphs (not a list of text identifiers), each labeled with its name.
2. **Given** the visual picker is open, **When** the admin clicks an icon glyph, **Then** that icon becomes the selection, the picker reflects it as selected, and the form shows the chosen glyph.
3. **Given** an admin has selected an icon, **When** they save the content block, **Then** the block is saved successfully with that icon and it appears with the block in the admin list.
4. **Given** an admin does not want an icon, **When** they choose the "no icon" option (or clear an existing selection), **Then** the block saves without an icon, exactly as today.

---

### User Story 2 - Search a broad icon catalog (Priority: P2)

The current choice is limited to 20 icons. The admin wants a substantially broader catalog so they can find an icon matching almost any kind of activity or guideline (sports, food, safety, transport, entertainment, …). Because a broad catalog is too large to scroll, the picker offers a search box: the admin types a keyword ("car", "food", "music") and the grid narrows to matching icons as they type.

**Why this priority**: "Broader options" is the second half of the user's request, but visual picking (US1) is already valuable over the fixed 20 icons. Search only becomes necessary once the catalog grows.

**Independent Test**: Can be tested by opening the picker, typing a keyword, observing the grid filter to matching icons, selecting one of the matches, and saving the block successfully — including icons that were not part of the original 20.

**Acceptance Scenarios**:

1. **Given** the picker is open, **When** the admin types a search term, **Then** the visible icons narrow to those whose name or keywords match, updating as they type.
2. **Given** a search term that matches nothing, **When** the results are empty, **Then** the picker shows a clear "no icons match" message instead of a blank area.
3. **Given** the admin selects an icon that is outside the original 20-icon set, **When** they save the block, **Then** the save succeeds and the icon renders on both the admin list and the public event page.
4. **Given** the admin clears the search box, **When** the term is empty, **Then** the full catalog is browsable again.

---

### User Story 3 - See real icons everywhere the identifier shows today (Priority: P3)

Today the admin block lists show the icon as a text suffix like "(music)" next to the block title. Once icons are picked visually, the admin should also see the real glyph in the block lists, so they can review at a glance what guests will see.

**Why this priority**: Polish on top of US1/US2 — improves review and consistency but the picker is fully usable without it.

**Independent Test**: Can be tested by saving blocks with icons and confirming the admin activities/guidelines lists render the glyphs instead of (or alongside) raw identifier text.

**Acceptance Scenarios**:

1. **Given** an activity or guideline with an icon exists, **When** the admin views the block list, **Then** the actual icon glyph is displayed with the block instead of the raw text identifier.
2. **Given** a block without an icon, **When** the admin views the list, **Then** no icon placeholder or broken glyph is shown.

---

### Edge Cases

- Existing content blocks were saved with icons from the original 20-icon set. When an admin edits such a block, the picker must show that icon as the current selection, and the block must keep rendering everywhere it does today.
- A previously saved icon identifier no longer exists in the catalog (e.g., after a future catalog update). The public event page and admin list must degrade gracefully (generic fallback icon or no icon) and never break the page.
- The catalog contains many hundreds of icons. Opening the picker and typing in the search box must remain smooth — no noticeable freeze or lag — and the admin content page must not become noticeably slower to load because the catalog exists.
- An icon identifier submitted outside the picker (e.g., a crafted request) that is not part of the catalog must still be rejected by the system, as unknown identifiers are today.
- Search terms with different casing or partial words ("Mus", "mus") should match the same icons.
- The picker must be operable by keyboard (open, navigate options, select, close) and announce options meaningfully to assistive technology.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The admin content forms for activities and guidelines MUST offer icon selection through a visual picker that displays each option's actual glyph with its name, replacing the current text-identifier dropdown.
- **FR-002**: The picker MUST offer a catalog substantially broader than the current 20 icons, covering common event-content themes (entertainment, food & drink, transport, safety, rules, facilities), sourced from an established icon collection.
- **FR-003**: The picker MUST provide a search box that filters the catalog by icon name and associated keywords, case-insensitively, with results updating as the admin types.
- **FR-004**: The picker MUST include an explicit "no icon" choice and allow clearing an existing selection; a block saved without an icon behaves exactly as today.
- **FR-005**: The currently selected icon MUST be visibly indicated in the form (glyph shown) both while picking and after selection.
- **FR-006**: Every icon offered by the picker MUST be accepted when the block is saved; the system MUST continue to reject icon identifiers that are not part of the recognized catalog.
- **FR-007**: All icon identifiers already saved on existing content blocks MUST remain valid: they render unchanged on the public event page and appear as the preselected choice when the block is edited.
- **FR-008**: The public event page MUST render every icon selectable in the picker; an unrecognized or stale identifier MUST degrade gracefully (fallback or omitted icon) and never break the page.
- **FR-009**: The admin block lists for activities and guidelines MUST display the saved icon as its glyph rather than as raw identifier text.
- **FR-010**: The picker MUST be keyboard-operable and expose meaningful names for each icon to assistive technology.
- **FR-011**: The presence of the broad catalog MUST NOT noticeably degrade the admin content page's load or interaction responsiveness; browsing and searching the catalog must feel instant.

### Key Entities

- **Content Block (Activity / Guideline)**: Existing entity shown on the public event page and managed in the admin CMS; carries an optional icon reference. Its stored shape does not change — only how the icon is chosen and displayed.
- **Icon Catalog**: The recognized set of selectable icons. Each entry has a unique identifier, a visual glyph, and a searchable name (plus optional keywords). The catalog is the single source of truth for what the picker offers and what the system accepts as valid.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An admin can locate and select a fitting icon for a new content block in under 30 seconds without prior knowledge of any icon's name.
- **SC-002**: The selectable catalog offers at least 10× the current choice (200+ icons).
- **SC-003**: 100% of icons chosen through the picker save successfully — zero "unknown icon" rejections originate from picker-made selections.
- **SC-004**: 100% of content blocks created before this feature continue to display their icons unchanged on the public event page and in the admin lists.
- **SC-005**: Search results appear within 1 second of typing (perceived as instant), and opening the picker takes under 2 seconds.
- **SC-006**: Icon-related admin actions (open picker, search, select, clear, save) are all achievable using only a keyboard.

## Assumptions

- "Broader option" means expanding the selectable catalog well beyond the current 20 icons by adopting the full (or near-full) set of one established open-source icon collection; the user suggested Lucide or React Icons as candidates and later designated a reference implementation (see References), which is built on the Lucide collection — planning should start from that reference and may deviate only with documented rationale.
- The rule that the system rejects unrecognized icon identifiers stays in force; expanding the catalog means the recognized set grows, not that validation is removed. The picker and the validation registry must agree so that FR-006 holds.
- The current 20 icon identifiers are part of the broader catalog (they are standard icon names), so existing data needs no migration.
- Only activities and guidelines have icons; guest stars do not, and adding icons to new block types is out of scope.
- Custom icon uploads (admin-provided images/SVGs) are out of scope — the project has no file storage by constitutional rule, and the request is about picking from a library.
- Icon search operates on English icon names/keywords, consistent with the admin UI language.
- Guest-facing behavior is unchanged except that more icons can now appear; no guest-facing interaction is added.

## References

- **User-designated reference implementation**: [shadcn-iconpicker](https://github.com/alan-crts/shadcn-iconpicker) (MIT, Alan Courtois) — an icon picker component built on shadcn/ui and the Lucide icon collection, distributed through the shadcn registry (`npx shadcn@latest add "https://icon-picker.alan-courtois.fr/r/icon-picker"`). Provides visual grid selection, real-time search, optional category grouping, progressive loading for large catalogs, controlled value/open state, and dark-mode support. Supplied by the user on 2026-08-05 as the starting point for `/speckit-plan`; it maps directly onto FR-001–FR-003, FR-005, and FR-011. Planning must still verify it satisfies FR-004 (explicit "no icon"/clear), FR-006 (agreement with server-side identifier validation), and FR-010 (keyboard and assistive-technology access), and cover any gaps.
