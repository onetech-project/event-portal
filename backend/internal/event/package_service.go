package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// --- Package reads consumed by the order domain via adapter ----------------

// GetPackageForCheckout returns a package with its full composition inside the
// caller's transaction, so checkout can aggregate demand without a second lookup.
// Callers (the order domain) apply their own business rules (status, sales
// window) on top of the returned values.
func (s *Service) GetPackageForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (PackageForCheckout, error) {
	pkg, err := s.repo.PackageForCheckout(ctx, tx, id)
	if errors.Is(err, ErrNotFound) {
		return PackageForCheckout{}, apperr.BadRequest(apperr.CodePackageNotFound,
			fmt.Sprintf("Package %s does not exist.", id))
	}
	if err != nil {
		return PackageForCheckout{}, err
	}
	return pkg, nil
}

// PackageDisplays resolves package ids to display labels and owning event for
// the admin/guest order views, never crossing the domain boundary via JOIN.
func (s *Service) PackageDisplays(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]PackageDisplayRecord, error) {
	return s.repo.PackageDisplaysByIDs(ctx, ids)
}

// --- Package admin surface ------------------------------------------------

// PackageAdminDetail is the admin view of a package with its availability.
type PackageAdminDetail struct {
	PackageAdminDTO
}

// ListPackagesByEvent returns every package of an event with derived
// availability for the admin dashboard.
func (s *Service) ListPackagesByEvent(ctx context.Context, eventID uuid.UUID) ([]PackageAdminDTO, error) {
	return cache.Through(ctx, s.cache, cache.PackagesAdminKey(eventID),
		func(ctx context.Context) ([]PackageAdminDTO, error) {
			return s.listPackagesByEvent(ctx, eventID)
		})
}

func (s *Service) listPackagesByEvent(ctx context.Context, eventID uuid.UUID) ([]PackageAdminDTO, error) {
	rows, err := s.repo.ListPackagesWithAvailabilityByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	components, err := s.repo.ListPackageComponentsByPackageIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byPackage := make(map[uuid.UUID][]PackageComponentRow, len(rows))
	for _, c := range components {
		byPackage[c.PackageID] = append(byPackage[c.PackageID], c)
	}

	// Sold counts are needed for admin views; batched across packages.
	soldMap := map[uuid.UUID]int{}
	if s.orders != nil {
		soldMap, err = s.orders.SoldCountByPackage(ctx, ids)
		if err != nil {
			return nil, err
		}
	}

	out := make([]PackageAdminDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toPackageAdminDTO(row.PackageRow, row.AvailableUnits, soldMap[row.ID], byPackage[row.ID]))
	}
	return out, nil
}

// --- Package admin writes ------------------------------------------------

// GetPackageAdmin returns one package's admin view, deriving availability and
// sold counts on the way out.
func (s *Service) GetPackageAdmin(ctx context.Context, id uuid.UUID) (PackageAdminDTO, error) {
	row, err := s.repo.GetPackageByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return PackageAdminDTO{}, packageNotFound()
	}
	if err != nil {
		return PackageAdminDTO{}, err
	}

	components, err := s.repo.ListPackageComponentsByPackageIDs(ctx, []uuid.UUID{id})
	if err != nil {
		return PackageAdminDTO{}, err
	}

	availability, err := s.repo.GetPackageAvailability(ctx, id)
	if err != nil {
		return PackageAdminDTO{}, err
	}

	sold := 0
	if s.orders != nil {
		soldMap, err := s.orders.SoldCountByPackage(ctx, []uuid.UUID{id})
		if err != nil {
			return PackageAdminDTO{}, err
		}
		sold = soldMap[id]
	}

	return toPackageAdminDTO(row, availability.AvailableUnits, sold, components), nil
}

// PackageAvailabilityDiagnostic explains why a package is or is not offered:
// each constituent's contribution, and which one is currently the binding
// constraint. It exists because "the bundle is unavailable" is not actionable on
// its own — the administrator needs to know which day ran out.
func (s *Service) PackageAvailabilityDiagnostic(ctx context.Context, id uuid.UUID) (PackageAvailabilityView, error) {
	if _, err := s.repo.GetPackageByID(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return PackageAvailabilityView{}, packageNotFound()
		}
		return PackageAvailabilityView{}, err
	}

	availability, err := s.repo.GetPackageAvailability(ctx, id)
	if err != nil {
		return PackageAvailabilityView{}, err
	}

	components, err := s.repo.ListPackageComponentsByPackageIDs(ctx, []uuid.UUID{id})
	if err != nil {
		return PackageAvailabilityView{}, err
	}

	view := PackageAvailabilityView{
		PackageID:      id.String(),
		AvailableUnits: availability.AvailableUnits,
		Purchasable:    availability.Purchasable,
		Components:     make([]PackageAvailabilityComponent, 0, len(components)),
	}

	for _, component := range components {
		// Whole sets only: integer division floors, matching the availability query.
		supported := component.Quota / component.QuantityPerUnit

		view.Components = append(view.Components, PackageAvailabilityComponent{
			TicketTypeID:    component.TicketTypeID.String(),
			Name:            component.Name,
			QuotaRemaining:  component.Quota,
			QuantityPerUnit: component.QuantityPerUnit,
			UnitsSupported:  supported,
		})

		if component.TicketTypeID == availability.LimitingTicketTypeID {
			view.LimitingTicketType = &PackageLimitingTicket{
				ID:             component.TicketTypeID.String(),
				Name:           component.Name,
				QuotaRemaining: component.Quota,
			}
		}
	}

	return view, nil
}

// CreatePackage validates and creates a package with its full composition in one
// transaction. The request body is parsed here rather than in a handler so the
// FR-036 rule — any quota-like field is rejected, not silently ignored — lives
// beside the rest of the validation and is directly testable.
func (s *Service) CreatePackage(ctx context.Context, body []byte) (PackageAdminDTO, error) {
	req, err := parsePackageRequest(body, true)
	if err != nil {
		return PackageAdminDTO{}, err
	}

	// Checked explicitly so an unknown event_id is a clear 400 rather than a raw
	// foreign-key violation (same pattern as CreateTicketType).
	if _, err := s.repo.GetEventByID(ctx, req.EventID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return PackageAdminDTO{}, apperr.BadRequest(apperr.CodeEventNotFound,
				fmt.Sprintf("Event %s does not exist.", req.EventID))
		}
		return PackageAdminDTO{}, err
	}
	if err := s.ensureComponentsInEvent(ctx, req.EventID, req.Components); err != nil {
		return PackageAdminDTO{}, err
	}

	var created PackageRow
	err = db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		created, err = s.repo.CreatePackage(ctx, tx, toAdminPackageParams(req))
		if err != nil {
			return err
		}
		if err := s.repo.ReplacePackageComponents(ctx, tx, created.ID, req.EventID, toAdminComponentParams(req.Components)); err != nil {
			return err
		}
		cache.InvalidateAfterCommit(ctx, s.cache, cache.Event(req.EventID))
		return nil
	})
	if err != nil {
		return PackageAdminDTO{}, err
	}

	return s.GetPackageAdmin(ctx, created.ID)
}

// UpdatePackage replaces a package's fields and composition. event_id in the body
// is ignored (a package never moves between events). A composition change is
// rejected with 409 while an open PENDING order holds the package (R-004): expiry
// reconstructs the hold from the current composition, so editing it would restore
// a different amount than was deducted. Name/price/window/status edits always
// succeed, even under an open order, because they never touch that reconstruction.
func (s *Service) UpdatePackage(ctx context.Context, id uuid.UUID, body []byte) (PackageAdminDTO, error) {
	req, err := parsePackageRequest(body, false)
	if err != nil {
		return PackageAdminDTO{}, err
	}

	existing, err := s.repo.GetPackageByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return PackageAdminDTO{}, packageNotFound()
	}
	if err != nil {
		return PackageAdminDTO{}, err
	}
	if err := s.ensureComponentsInEvent(ctx, existing.EventID, req.Components); err != nil {
		return PackageAdminDTO{}, err
	}

	current, err := s.repo.ListPackageComponentsByPackageIDs(ctx, []uuid.UUID{id})
	if err != nil {
		return PackageAdminDTO{}, err
	}

	err = db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if !sameComposition(current, req.Components) {
			if s.orders == nil {
				return errors.New("event: no order checker configured")
			}
			locked, err := s.orders.HasPendingOrdersForPackage(ctx, tx, id)
			if err != nil {
				return err
			}
			if locked {
				return apperr.Conflict(apperr.CodePackageCompositionLocked,
					"This package has open orders; its composition cannot change until they resolve.")
			}
			if err := s.repo.ReplacePackageComponents(ctx, tx, id, existing.EventID, toAdminComponentParams(req.Components)); err != nil {
				return err
			}
		}
		if _, err := s.repo.UpdatePackage(ctx, tx, id, toAdminPackageParams(req)); err != nil {
			return err
		}
		// Price, window, status and composition all feed the guest-facing package
		// summary and its derived availability. A composition change that the
		// open-order guard rejects rolls back, and takes this with it.
		cache.InvalidateAfterCommit(ctx, s.cache, cache.Event(existing.EventID))
		return nil
	})
	if err != nil {
		return PackageAdminDTO{}, err
	}

	return s.GetPackageAdmin(ctx, id)
}

// DeletePackage removes a package and its composition (which cascades), guarded
// inside the same transaction as the delete so no order can appear between the
// check and the DELETE. A package referenced by order_items or attendees cannot
// be deleted: the FKs are ON DELETE RESTRICT, so the guard returns a clean 400
// rather than surfacing a raw constraint violation (Constitution Principle VI).
func (s *Service) DeletePackage(ctx context.Context, id uuid.UUID) error {
	if s.orders == nil {
		return errors.New("event: no order checker configured")
	}

	// Resolved before the delete: afterwards the row is gone and with it the link
	// back to the event whose cached package list must be invalidated.
	var eventID uuid.UUID
	if row, lookupErr := s.repo.GetPackageByID(ctx, id); lookupErr == nil {
		eventID = row.EventID
	}

	var found bool

	err := db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		hasOrders, err := s.orders.HasOrdersForPackage(ctx, tx, id)
		if err != nil {
			return err
		}
		if hasOrders {
			return apperr.BadRequest(apperr.CodePackageHasOrders,
				"This package cannot be deleted because it has been ordered.")
		}

		found, err = s.repo.DeletePackage(ctx, tx, id)
		if err != nil {
			return err
		}
		if found && eventID != uuid.Nil {
			cache.InvalidateAfterCommit(ctx, s.cache, cache.Event(eventID))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return packageNotFound()
	}
	return nil
}

// --- Request parsing and helpers -----------------------------------------

// parsePackageRequest decodes an admin package body and runs every rule that is
// decidable without the database. The body is decoded once into a key map first
// so FR-036 can reject any quota-like field before it is silently dropped by the
// typed decode.
func parsePackageRequest(body []byte, requireEventID bool) (PackageRequest, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(body, &keys); err != nil {
		return PackageRequest{}, apperr.BadRequest(apperr.CodeValidation,
			"The request body could not be parsed.")
	}
	for key := range keys {
		if quotaLikeKeys[key] {
			return PackageRequest{}, apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("Field %q is not valid on a package: a package holds no inventory.", key))
		}
	}

	var req PackageRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return PackageRequest{}, apperr.BadRequest(apperr.CodeValidation,
			"The request body could not be parsed.")
	}
	if err := req.Validate(requireEventID); err != nil {
		return PackageRequest{}, err
	}
	return req, nil
}

// ensureComponentsInEvent rejects components that reference a ticket type that
// does not belong to the package's event. The composite FK on package_tickets
// enforces the same rule at the data layer (FR-015); this turns it into a clean
// 400 naming the offender.
func (s *Service) ensureComponentsInEvent(ctx context.Context, eventID uuid.UUID, components []PackageComponentRequest) error {
	ids, err := s.repo.ListTicketTypeIDsByEventID(ctx, nil, eventID)
	if err != nil {
		return err
	}
	valid := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		valid[id] = struct{}{}
	}
	for _, c := range components {
		if _, ok := valid[c.TicketTypeID]; !ok {
			return apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("Ticket type %s does not belong to this event.", c.TicketTypeID))
		}
	}
	return nil
}

// sameComposition reports whether the requested components match the current
// composition exactly, as ticket_type_id → quantity_per_unit. Used to decide
// whether a PUT is a composition edit (guarded) or a plain field edit (free).
func sameComposition(current []PackageComponentRow, requested []PackageComponentRequest) bool {
	if len(current) != len(requested) {
		return false
	}
	want := make(map[uuid.UUID]int32, len(requested))
	for _, r := range requested {
		want[r.TicketTypeID] = r.QuantityPerUnit
	}
	for _, c := range current {
		if want[c.TicketTypeID] != c.QuantityPerUnit {
			return false
		}
	}
	return true
}

func toAdminPackageParams(req PackageRequest) AdminPackageParams {
	return AdminPackageParams{
		EventID:     req.EventID,
		Name:        req.Name,
		Description: req.Description,
		Price:       req.Price.Decimal(),
		SalesStart:  req.SalesStart,
		SalesEnd:    req.SalesEnd,
		IsActive:    req.IsActive,
	}
}

func toAdminComponentParams(components []PackageComponentRequest) []AdminPackageComponentParams {
	out := make([]AdminPackageComponentParams, 0, len(components))
	for _, c := range components {
		out = append(out, AdminPackageComponentParams{TicketTypeID: c.TicketTypeID, QuantityPerUnit: c.QuantityPerUnit})
	}
	return out
}

func toPackageAdminDTO(row PackageRow, available int32, sold int, components []PackageComponentRow) PackageAdminDTO {
	componentDTOs := make([]PackageComponentDTO, 0, len(components))
	for _, c := range components {
		componentDTOs = append(componentDTOs, PackageComponentDTO{
			TicketTypeID:    c.TicketTypeID.String(),
			TicketTypeName:  c.Name,
			QuantityPerUnit: c.QuantityPerUnit,
		})
	}
	return PackageAdminDTO{
		ID:             row.ID.String(),
		EventID:        row.EventID.String(),
		Name:           row.Name,
		Description:    row.Description,
		Price:          money.From(row.Price),
		SalesStart:     row.SalesStart,
		SalesEnd:       row.SalesEnd,
		IsActive:       row.IsActive,
		Components:     componentDTOs,
		AvailableUnits: available,
		Sold:           int32(sold),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func packageNotFound() error {
	return apperr.NotFound(apperr.CodePackageNotFound, "Package not found.")
}
