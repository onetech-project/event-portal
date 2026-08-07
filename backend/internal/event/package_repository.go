package event

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/internal/event/eventsql"
)

// PackageRow is the internal view of a packages row.
type PackageRow struct {
	ID          uuid.UUID
	EventID     uuid.UUID
	Name        string
	Description *string
	Price       decimal.Decimal
	SalesStart  time.Time
	SalesEnd    time.Time
	IsActive    bool
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

// PackageRowWithAvailability attaches the derived availability figures to a
// package row. Purchasable is a *bool defensively — the SQL expression never
// evaluates to NULL, but sqlc types it nullable because it goes through a LEFT
// JOIN — and callers must dereference it to a definite bool.
type PackageRowWithAvailability struct {
	PackageRow
	AvailableUnits       int32
	LimitingTicketTypeID uuid.NullUUID
	Purchasable          *bool
}

// PackageComponentRow is one constituent, resolved with the ticket's own name
// and sales window so rendering and checkout validation share one query.
type PackageComponentRow struct {
	PackageID       uuid.UUID
	TicketTypeID    uuid.UUID
	QuantityPerUnit int32
	Name            string
	Price           decimal.Decimal
	Quota           int32
	SalesStart      time.Time
	SalesEnd        time.Time
}

// AdminPackageParams carries the server-validated fields for a package write.
type AdminPackageParams struct {
	EventID     uuid.UUID
	Name        string
	Description *string
	Price       decimal.Decimal
	SalesStart  time.Time
	SalesEnd    time.Time
	IsActive    bool
}

// AdminPackageComponentParams is one constituent to persist.
type AdminPackageComponentParams struct {
	TicketTypeID    uuid.UUID
	QuantityPerUnit int32
}

// --- Package reads --------------------------------------------------------

// ListPackagesWithAvailabilityByEventID returns every package of an event with
// its derived availability, in one query (contracts/checkout-transaction.md
// §1.2). A componentless package still appears — as unavailable — so an
// administrator can see it as broken rather than watch it vanish.
func (r *Repository) ListPackagesWithAvailabilityByEventID(ctx context.Context, eventID uuid.UUID) ([]PackageRowWithAvailability, error) {
	rows, err := r.queries.ListPackagesWithAvailabilityByEventID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list packages with availability: %w", err)
	}

	out := make([]PackageRowWithAvailability, 0, len(rows))
	for _, row := range rows {
		out = append(out, PackageRowWithAvailability{
			PackageRow: PackageRow{
				ID: row.ID, EventID: row.EventID, Name: row.Name,
				Description: row.Description, Price: row.Price,
				SalesStart: row.SalesStart, SalesEnd: row.SalesEnd,
				IsActive: row.IsActive, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			},
			AvailableUnits:       row.AvailableUnits,
			LimitingTicketTypeID: row.LimitingTicketTypeID,
			Purchasable:          row.Purchasable,
		})
	}
	return out, nil
}

// GetPackageAvailability returns the derived availability for one package, for
// the admin diagnostic endpoint.
func (r *Repository) GetPackageAvailability(ctx context.Context, packageID uuid.UUID) (PackageAvailability, error) {
	row, err := r.queries.GetPackageAvailability(ctx, packageID)
	if err != nil {
		return PackageAvailability{}, fmt.Errorf("get package availability: %w", err)
	}
	return PackageAvailability{
		PackageID:            packageID,
		AvailableUnits:       row.AvailableUnits,
		Purchasable:          row.AvailableUnits > 0 && row.ComponentCount > 0 && row.AllComponentsOnSale,
		LimitingTicketTypeID: row.LimitingTicketTypeID,
	}, nil
}

// ListPackageComponentsByPackageIDs returns the composition of many packages in
// one batched query (contracts/checkout-transaction.md §1.3) — never N+1.
func (r *Repository) ListPackageComponentsByPackageIDs(ctx context.Context, packageIDs []uuid.UUID) ([]PackageComponentRow, error) {
	if len(packageIDs) == 0 {
		return nil, nil
	}

	rows, err := r.queries.ListPackageComponentsByPackageIDs(ctx, packageIDs)
	if err != nil {
		return nil, fmt.Errorf("list package components: %w", err)
	}

	out := make([]PackageComponentRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, PackageComponentRow{
			PackageID:       row.PackageID,
			TicketTypeID:    row.TicketTypeID,
			QuantityPerUnit: row.QuantityPerUnit,
			Name:            row.TicketTypeName,
			Price:           row.Price,
			Quota:           row.Quota,
			SalesStart:      row.SalesStart,
			SalesEnd:        row.SalesEnd,
		})
	}
	return out, nil
}

// GetPackageByID returns one package regardless of status.
func (r *Repository) GetPackageByID(ctx context.Context, id uuid.UUID) (PackageRow, error) {
	row, err := r.queries.GetPackageByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return PackageRow{}, ErrNotFound
	}
	if err != nil {
		return PackageRow{}, fmt.Errorf("get package: %w", err)
	}
	return toPackageRow(eventsql.CreatePackageRow(row)), nil
}

// PackageForCheckout returns the server-side truth about a package and its full
// composition, inside the caller's transaction so checkout sees a consistent
// snapshot without borrowing a second pool connection (models.md §2.3).
//
// Both the header row and the component batch run on the same transaction, so
// the package cannot change composition between the two reads.
func (r *Repository) PackageForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (PackageForCheckout, error) {
	queries := r.queries
	if tx != nil {
		queries = queries.WithTx(tx)
	}

	row, err := queries.PackageForCheckout(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return PackageForCheckout{}, ErrNotFound
	}
	if err != nil {
		return PackageForCheckout{}, fmt.Errorf("package for checkout: %w", err)
	}

	rows, err := queries.ListPackageComponentsByPackageIDs(ctx, []uuid.UUID{id})
	if err != nil {
		return PackageForCheckout{}, fmt.Errorf("package components for checkout: %w", err)
	}

	pkg := PackageForCheckout{
		ID:         row.ID,
		EventID:    row.EventID,
		Name:       row.Name,
		Price:      row.Price,
		SalesStart: row.SalesStart,
		SalesEnd:   row.SalesEnd,
		IsActive:   row.IsActive,
		Components: make([]PackageComponent, 0, len(rows)),
	}
	for _, r := range rows {
		pkg.Components = append(pkg.Components, PackageComponent{
			TicketTypeID: r.TicketTypeID,
			Name:         r.TicketTypeName,
			PerUnit:      r.QuantityPerUnit,
			SalesStart:   r.SalesStart,
			SalesEnd:     r.SalesEnd,
		})
	}
	return pkg, nil
}

// ListPackagesByTicketTypeID returns the packages a ticket type feeds, so the
// ticket-type delete guard can name what must be unpicked first (FR-038).
func (r *Repository) ListPackagesByTicketTypeID(ctx context.Context, ticketTypeID uuid.UUID) ([]PackageNameRow, error) {
	rows, err := r.queries.ListPackagesByTicketTypeID(ctx, ticketTypeID)
	if err != nil {
		return nil, fmt.Errorf("list packages by ticket type: %w", err)
	}
	return toPackageNameRows(rows), nil
}

// ListPackagesByTicketTypeIDs is the batched form used when deleting an event,
// which must consider every one of its ticket types.
func (r *Repository) ListPackagesByTicketTypeIDs(ctx context.Context, ticketTypeIDs []uuid.UUID) ([]PackageNameRow, error) {
	if len(ticketTypeIDs) == 0 {
		return nil, nil
	}
	rows, err := r.queries.ListPackagesByTicketTypeIDs(ctx, ticketTypeIDs)
	if err != nil {
		return nil, fmt.Errorf("list packages by ticket types: %w", err)
	}
	return toPackageNameRowsBatch(rows), nil
}

// CountPackagesByEventID reports how many packages an event has, for the event
// delete guard.
func (r *Repository) CountPackagesByEventID(ctx context.Context, eventID uuid.UUID) (int64, error) {
	count, err := r.queries.CountPackagesByEventID(ctx, eventID)
	if err != nil {
		return 0, fmt.Errorf("count packages: %w", err)
	}
	return count, nil
}

// PackageDisplayRecord labels one package with the event it belongs to.
type PackageDisplayRecord struct {
	PackageName    string
	EventName      string
	EventSlug      string
	EventVenue     string
	EventAddress   string
	EventStartDate time.Time
	EventEndDate   time.Time
}

// PackageDisplaysByIDs resolves package ids to their display labels and owning
// event in one query, so the order domain can name bundle lines without JOINing
// tables it does not own (Constitution Principle II).
func (r *Repository) PackageDisplaysByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]PackageDisplayRecord, error) {
	displays := make(map[uuid.UUID]PackageDisplayRecord, len(ids))
	if len(ids) == 0 {
		return displays, nil
	}

	rows, err := r.queries.ListPackageDisplaysByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list package displays: %w", err)
	}
	for _, row := range rows {
		displays[row.ID] = PackageDisplayRecord{
			PackageName:    row.Name,
			EventName:      row.EventName,
			EventSlug:      row.EventSlug,
			EventVenue:     row.EventVenue,
			EventAddress:   row.EventAddress,
			EventStartDate: row.EventStartDate,
			EventEndDate:   row.EventEndDate,
		}
	}
	return displays, nil
}

// --- Package writes -------------------------------------------------------

// CreatePackage inserts a package header inside the caller's transaction, so an
// admin create can write header and composition atomically. There is deliberately
// no quota column to write (contracts/schema.md §3.1).
func (r *Repository) CreatePackage(ctx context.Context, tx pgx.Tx, p AdminPackageParams) (PackageRow, error) {
	queries := r.queries
	if tx != nil {
		queries = queries.WithTx(tx)
	}
	row, err := queries.CreatePackage(ctx, eventsql.CreatePackageParams{
		EventID:     p.EventID,
		Name:        p.Name,
		Description: p.Description,
		Price:       p.Price,
		SalesStart:  p.SalesStart,
		SalesEnd:    p.SalesEnd,
		IsActive:    p.IsActive,
	})
	if err != nil {
		return PackageRow{}, fmt.Errorf("create package: %w", err)
	}
	return toPackageRow(row), nil
}

// UpdatePackage replaces a package's fields inside the caller's transaction, so
// header and composition changes commit together. event_id is never written: a
// package does not move between events because its composition is bound to one
// event by the composite foreign keys.
func (r *Repository) UpdatePackage(ctx context.Context, tx pgx.Tx, id uuid.UUID, p AdminPackageParams) (PackageRow, error) {
	queries := r.queries
	if tx != nil {
		queries = queries.WithTx(tx)
	}
	row, err := queries.UpdatePackage(ctx, eventsql.UpdatePackageParams{
		ID:          id,
		Name:        p.Name,
		Description: p.Description,
		Price:       p.Price,
		SalesStart:  p.SalesStart,
		SalesEnd:    p.SalesEnd,
		IsActive:    p.IsActive,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PackageRow{}, ErrNotFound
	}
	if err != nil {
		return PackageRow{}, fmt.Errorf("update package: %w", err)
	}
	return toPackageRow(eventsql.CreatePackageRow(row)), nil
}

// ReplacePackageComponents rewrites a package's composition wholesale as a
// delete-plus-insert inside the caller's transaction. Both FKs resolve against
// the same event_id, so a component from another event is rejected by the
// database even when application validation is bypassed (R-008).
func (r *Repository) ReplacePackageComponents(ctx context.Context, tx pgx.Tx, packageID, eventID uuid.UUID, components []AdminPackageComponentParams) error {
	queries := r.queries.WithTx(tx)

	if _, err := queries.DeletePackageComponents(ctx, packageID); err != nil {
		return fmt.Errorf("replace package components (clear): %w", err)
	}
	for _, c := range components {
		if _, err := queries.CreatePackageComponent(ctx, eventsql.CreatePackageComponentParams{
			PackageID:    packageID,
			TicketTypeID: c.TicketTypeID,
			EventID:      eventID,
			Quantity:     c.QuantityPerUnit,
		}); err != nil {
			return fmt.Errorf("replace package components (insert): %w", err)
		}
	}
	return nil
}

// DeletePackage removes a package inside the caller's transaction, reporting
// whether it existed. Composition rows cascade with it (ON DELETE CASCADE).
func (r *Repository) DeletePackage(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	affected, err := r.withTx(tx).DeletePackage(ctx, id)
	if err != nil {
		return false, fmt.Errorf("delete package: %w", err)
	}
	return affected > 0, nil
}

func toPackageRow(row eventsql.CreatePackageRow) PackageRow {
	return PackageRow{
		ID:          row.ID,
		EventID:     row.EventID,
		Name:        row.Name,
		Description: row.Description,
		Price:       row.Price,
		SalesStart:  row.SalesStart,
		SalesEnd:    row.SalesEnd,
		IsActive:    row.IsActive,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func toPackageNameRows(rows []eventsql.ListPackagesByTicketTypeIDRow) []PackageNameRow {
	out := make([]PackageNameRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, PackageNameRow{ID: row.ID, Name: row.Name})
	}
	return out
}

func toPackageNameRowsBatch(rows []eventsql.ListPackagesByTicketTypeIDsRow) []PackageNameRow {
	out := make([]PackageNameRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, PackageNameRow{ID: row.ID, Name: row.Name})
	}
	return out
}

// PackageNameRow is one package's identity, used by the delete guards' messages.
type PackageNameRow struct {
	ID   uuid.UUID
	Name string
}
