package event

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/internal/event/eventsql"
	"github.com/manjo/ticketing/backend/internal/event/iconkeys"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/sanitize"
)

// The CMS content blocks of an event (spec 008 US4, research R7): activities,
// guest stars, and guidelines, each position-ordered and admin-authored.
// Icons are NAMED KEYS from the generated lucide catalog (spec 009), rendered
// client-side by lucide-react — never URLs, never uploads (Constitution: no
// object storage).

// validateIcon accepts nil/empty (no icon) or a key from the recognized
// catalog embedded in iconkeys.
func validateIcon(icon *string) error {
	if icon == nil || *icon == "" {
		return nil
	}
	if !iconkeys.Valid(*icon) {
		return apperr.BadRequest(apperr.CodeUnknownIcon,
			fmt.Sprintf("Unknown icon key %q — pick one from the documented set.", *icon))
	}
	return nil
}

// --- DTOs -------------------------------------------------------------------

// ActivityDTO is one activity block, on both the admin and guest reads.
type ActivityDTO struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Icon        *string   `json:"icon"`
	Position    int32     `json:"position"`
}

// GuestStarDTO is one guest-star block.
type GuestStarDTO struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Position int32     `json:"position"`
}

// GuidelineDTO is one guideline block.
type GuidelineDTO struct {
	ID          uuid.UUID `json:"id"`
	Description string    `json:"description"`
	Icon        *string   `json:"icon"`
	Position    int32     `json:"position"`
}

// ActivityRequest is the admin write body for an activity.
type ActivityRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Icon        *string `json:"icon"`
	Position    int32   `json:"position"`
}

// GuestStarRequest is the admin write body for a guest star.
type GuestStarRequest struct {
	Name     string `json:"name"`
	Position int32  `json:"position"`
}

// GuidelineRequest is the admin write body for a guideline.
type GuidelineRequest struct {
	Description string  `json:"description"`
	Icon        *string `json:"icon"`
	Position    int32   `json:"position"`
}

// TermsUpsertRequest is the admin write body for the T&C document.
type TermsUpsertRequest struct {
	Content string `json:"content"`
}

// --- Repository -------------------------------------------------------------

func (r *Repository) ListActivities(ctx context.Context, eventID uuid.UUID) ([]eventsql.EventActivity, error) {
	rows, err := r.queries.ListEventActivities(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list activities: %w", err)
	}
	return rows, nil
}

func (r *Repository) CreateActivity(ctx context.Context, eventID uuid.UUID, req ActivityRequest) (eventsql.EventActivity, error) {
	row, err := r.queries.CreateEventActivity(ctx, eventsql.CreateEventActivityParams{
		EventID: eventID, Title: req.Title, Description: req.Description,
		Icon: req.Icon, Position: req.Position,
	})
	if err != nil {
		return eventsql.EventActivity{}, fmt.Errorf("create activity: %w", err)
	}
	return row, nil
}

func (r *Repository) UpdateActivity(ctx context.Context, id, eventID uuid.UUID, req ActivityRequest) (bool, error) {
	rows, err := r.queries.UpdateEventActivity(ctx, eventsql.UpdateEventActivityParams{
		ID: id, EventID: eventID, Title: req.Title, Description: req.Description,
		Icon: req.Icon, Position: req.Position,
	})
	if err != nil {
		return false, fmt.Errorf("update activity: %w", err)
	}
	return rows > 0, nil
}

func (r *Repository) DeleteActivity(ctx context.Context, id, eventID uuid.UUID) (bool, error) {
	rows, err := r.queries.DeleteEventActivity(ctx, eventsql.DeleteEventActivityParams{ID: id, EventID: eventID})
	if err != nil {
		return false, fmt.Errorf("delete activity: %w", err)
	}
	return rows > 0, nil
}

func (r *Repository) ListGuestStars(ctx context.Context, eventID uuid.UUID) ([]eventsql.EventGuestStar, error) {
	rows, err := r.queries.ListEventGuestStars(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list guest stars: %w", err)
	}
	return rows, nil
}

func (r *Repository) CreateGuestStar(ctx context.Context, eventID uuid.UUID, req GuestStarRequest) (eventsql.EventGuestStar, error) {
	row, err := r.queries.CreateEventGuestStar(ctx, eventsql.CreateEventGuestStarParams{
		EventID: eventID, Name: req.Name, Position: req.Position,
	})
	if err != nil {
		return eventsql.EventGuestStar{}, fmt.Errorf("create guest star: %w", err)
	}
	return row, nil
}

func (r *Repository) UpdateGuestStar(ctx context.Context, id, eventID uuid.UUID, req GuestStarRequest) (bool, error) {
	rows, err := r.queries.UpdateEventGuestStar(ctx, eventsql.UpdateEventGuestStarParams{
		ID: id, EventID: eventID, Name: req.Name, Position: req.Position,
	})
	if err != nil {
		return false, fmt.Errorf("update guest star: %w", err)
	}
	return rows > 0, nil
}

func (r *Repository) DeleteGuestStar(ctx context.Context, id, eventID uuid.UUID) (bool, error) {
	rows, err := r.queries.DeleteEventGuestStar(ctx, eventsql.DeleteEventGuestStarParams{ID: id, EventID: eventID})
	if err != nil {
		return false, fmt.Errorf("delete guest star: %w", err)
	}
	return rows > 0, nil
}

func (r *Repository) ListGuidelines(ctx context.Context, eventID uuid.UUID) ([]eventsql.EventGuideline, error) {
	rows, err := r.queries.ListEventGuidelines(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list guidelines: %w", err)
	}
	return rows, nil
}

func (r *Repository) CreateGuideline(ctx context.Context, eventID uuid.UUID, req GuidelineRequest) (eventsql.EventGuideline, error) {
	row, err := r.queries.CreateEventGuideline(ctx, eventsql.CreateEventGuidelineParams{
		EventID: eventID, Description: req.Description, Icon: req.Icon, Position: req.Position,
	})
	if err != nil {
		return eventsql.EventGuideline{}, fmt.Errorf("create guideline: %w", err)
	}
	return row, nil
}

func (r *Repository) UpdateGuideline(ctx context.Context, id, eventID uuid.UUID, req GuidelineRequest) (bool, error) {
	rows, err := r.queries.UpdateEventGuideline(ctx, eventsql.UpdateEventGuidelineParams{
		ID: id, EventID: eventID, Description: req.Description, Icon: req.Icon, Position: req.Position,
	})
	if err != nil {
		return false, fmt.Errorf("update guideline: %w", err)
	}
	return rows > 0, nil
}

func (r *Repository) DeleteGuideline(ctx context.Context, id, eventID uuid.UUID) (bool, error) {
	rows, err := r.queries.DeleteEventGuideline(ctx, eventsql.DeleteEventGuidelineParams{ID: id, EventID: eventID})
	if err != nil {
		return false, fmt.Errorf("delete guideline: %w", err)
	}
	return rows > 0, nil
}

// UpsertEventTerms writes (or replaces) the event's single terms document.
func (r *Repository) UpsertEventTerms(ctx context.Context, eventID uuid.UUID, content string) (TermsRow, error) {
	row, err := r.queries.UpsertEventTerms(ctx, eventsql.UpsertEventTermsParams{
		EventID: eventID, Content: content,
	})
	if err != nil {
		return TermsRow{}, fmt.Errorf("upsert event terms: %w", err)
	}
	return TermsRow{ID: row.ID, Content: row.Content, UpdatedAt: row.UpdatedAt}, nil
}

// --- Service (admin content surface) -----------------------------------------

// requireEvent resolves an admin-supplied event id to a clear 404.
func (s *Service) requireEvent(ctx context.Context, eventID uuid.UUID) error {
	if _, err := s.repo.GetEventByID(ctx, eventID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
		}
		return err
	}
	return nil
}

func blockNotFound() error {
	return apperr.NotFound(apperr.CodeNotFound, "Content block not found.")
}

// Activities ------------------------------------------------------------------

func (s *Service) ListActivities(ctx context.Context, eventID uuid.UUID) ([]ActivityDTO, error) {
	if err := s.requireEvent(ctx, eventID); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListActivities(ctx, eventID)
	if err != nil {
		return nil, err
	}
	out := make([]ActivityDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, ActivityDTO{ID: row.ID, Title: row.Title,
			Description: row.Description, Icon: row.Icon, Position: row.Position})
	}
	return out, nil
}

func (r ActivityRequest) validate() error {
	if r.Title == "" {
		return apperr.BadRequest(apperr.CodeValidation, "title is required.")
	}
	if r.Description == "" {
		return apperr.BadRequest(apperr.CodeValidation, "description is required.")
	}
	return validateIcon(r.Icon)
}

func (s *Service) CreateActivity(ctx context.Context, eventID uuid.UUID, req ActivityRequest) (ActivityDTO, error) {
	if err := req.validate(); err != nil {
		return ActivityDTO{}, err
	}
	if err := s.requireEvent(ctx, eventID); err != nil {
		return ActivityDTO{}, err
	}
	row, err := s.repo.CreateActivity(ctx, eventID, req)
	if err != nil {
		return ActivityDTO{}, err
	}
	return ActivityDTO{ID: row.ID, Title: row.Title, Description: row.Description,
		Icon: row.Icon, Position: row.Position}, nil
}

func (s *Service) UpdateActivity(ctx context.Context, id, eventID uuid.UUID, req ActivityRequest) error {
	if err := req.validate(); err != nil {
		return err
	}
	updated, err := s.repo.UpdateActivity(ctx, id, eventID, req)
	if err != nil {
		return err
	}
	if !updated {
		return blockNotFound()
	}
	return nil
}

func (s *Service) DeleteActivity(ctx context.Context, id, eventID uuid.UUID) error {
	deleted, err := s.repo.DeleteActivity(ctx, id, eventID)
	if err != nil {
		return err
	}
	if !deleted {
		return blockNotFound()
	}
	return nil
}

// Guest stars -----------------------------------------------------------------

func (s *Service) ListGuestStars(ctx context.Context, eventID uuid.UUID) ([]GuestStarDTO, error) {
	if err := s.requireEvent(ctx, eventID); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListGuestStars(ctx, eventID)
	if err != nil {
		return nil, err
	}
	out := make([]GuestStarDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, GuestStarDTO{ID: row.ID, Name: row.Name, Position: row.Position})
	}
	return out, nil
}

func (s *Service) CreateGuestStar(ctx context.Context, eventID uuid.UUID, req GuestStarRequest) (GuestStarDTO, error) {
	if req.Name == "" {
		return GuestStarDTO{}, apperr.BadRequest(apperr.CodeValidation, "name is required.")
	}
	if err := s.requireEvent(ctx, eventID); err != nil {
		return GuestStarDTO{}, err
	}
	row, err := s.repo.CreateGuestStar(ctx, eventID, req)
	if err != nil {
		return GuestStarDTO{}, err
	}
	return GuestStarDTO{ID: row.ID, Name: row.Name, Position: row.Position}, nil
}

func (s *Service) UpdateGuestStar(ctx context.Context, id, eventID uuid.UUID, req GuestStarRequest) error {
	if req.Name == "" {
		return apperr.BadRequest(apperr.CodeValidation, "name is required.")
	}
	updated, err := s.repo.UpdateGuestStar(ctx, id, eventID, req)
	if err != nil {
		return err
	}
	if !updated {
		return blockNotFound()
	}
	return nil
}

func (s *Service) DeleteGuestStar(ctx context.Context, id, eventID uuid.UUID) error {
	deleted, err := s.repo.DeleteGuestStar(ctx, id, eventID)
	if err != nil {
		return err
	}
	if !deleted {
		return blockNotFound()
	}
	return nil
}

// Guidelines ------------------------------------------------------------------

func (s *Service) ListGuidelines(ctx context.Context, eventID uuid.UUID) ([]GuidelineDTO, error) {
	if err := s.requireEvent(ctx, eventID); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListGuidelines(ctx, eventID)
	if err != nil {
		return nil, err
	}
	out := make([]GuidelineDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, GuidelineDTO{ID: row.ID, Description: row.Description,
			Icon: row.Icon, Position: row.Position})
	}
	return out, nil
}

func (r GuidelineRequest) validate() error {
	if r.Description == "" {
		return apperr.BadRequest(apperr.CodeValidation, "description is required.")
	}
	return validateIcon(r.Icon)
}

func (s *Service) CreateGuideline(ctx context.Context, eventID uuid.UUID, req GuidelineRequest) (GuidelineDTO, error) {
	if err := req.validate(); err != nil {
		return GuidelineDTO{}, err
	}
	if err := s.requireEvent(ctx, eventID); err != nil {
		return GuidelineDTO{}, err
	}
	row, err := s.repo.CreateGuideline(ctx, eventID, req)
	if err != nil {
		return GuidelineDTO{}, err
	}
	return GuidelineDTO{ID: row.ID, Description: row.Description,
		Icon: row.Icon, Position: row.Position}, nil
}

func (s *Service) UpdateGuideline(ctx context.Context, id, eventID uuid.UUID, req GuidelineRequest) error {
	if err := req.validate(); err != nil {
		return err
	}
	updated, err := s.repo.UpdateGuideline(ctx, id, eventID, req)
	if err != nil {
		return err
	}
	if !updated {
		return blockNotFound()
	}
	return nil
}

func (s *Service) DeleteGuideline(ctx context.Context, id, eventID uuid.UUID) error {
	deleted, err := s.repo.DeleteGuideline(ctx, id, eventID)
	if err != nil {
		return err
	}
	if !deleted {
		return blockNotFound()
	}
	return nil
}

// Terms (admin) ---------------------------------------------------------------

// AdminTermsForEvent returns the event's terms document by event id, 404002
// when none is authored yet.
func (s *Service) AdminTermsForEvent(ctx context.Context, eventID uuid.UUID) (EventTermsDTO, error) {
	if err := s.requireEvent(ctx, eventID); err != nil {
		return EventTermsDTO{}, err
	}
	row, err := s.repo.GetEventTermsByEventID(ctx, eventID)
	if errors.Is(err, ErrNotFound) {
		return EventTermsDTO{}, apperr.NotFound(apperr.CodeTermsMissing,
			"This event has no Terms & Conditions yet.")
	}
	if err != nil {
		return EventTermsDTO{}, err
	}
	return EventTermsDTO{ID: row.ID, Content: row.Content, UpdatedAt: row.UpdatedAt}, nil
}

// UpsertTerms writes the event's terms document, sanitized on write
// (Constitution: WYSIWYG HTML is UGC until proven otherwise). An edit
// overwrites in place — the document's id survives, but a guest mid-dialog is
// still protected by the 409002 staleness check at agreement time.
func (s *Service) UpsertTerms(ctx context.Context, eventID uuid.UUID, req TermsUpsertRequest) (EventTermsDTO, error) {
	if req.Content == "" {
		return EventTermsDTO{}, apperr.BadRequest(apperr.CodeValidation, "content is required.")
	}
	if err := s.requireEvent(ctx, eventID); err != nil {
		return EventTermsDTO{}, err
	}
	row, err := s.repo.UpsertEventTerms(ctx, eventID, sanitize.HTML(req.Content))
	if err != nil {
		return EventTermsDTO{}, err
	}
	return EventTermsDTO{ID: row.ID, Content: row.Content, UpdatedAt: row.UpdatedAt}, nil
}

// --- HTTP (admin routes) -----------------------------------------------------

// RegisterAdminContentRoutes mounts the CMS content surface under the same
// JWT-protected group as the other admin routes.
func (h *Handler) RegisterAdminContentRoutes(g *echo.Group) {
	g.GET("/admin/events/:id/terms", h.adminGetTerms)
	g.PUT("/admin/events/:id/terms", h.adminPutTerms)

	g.GET("/admin/events/:id/activities", h.adminListActivities)
	g.POST("/admin/events/:id/activities", h.adminCreateActivity)
	g.PUT("/admin/events/:id/activities/:blockId", h.adminUpdateActivity)
	g.DELETE("/admin/events/:id/activities/:blockId", h.adminDeleteActivity)

	g.GET("/admin/events/:id/guest-stars", h.adminListGuestStars)
	g.POST("/admin/events/:id/guest-stars", h.adminCreateGuestStar)
	g.PUT("/admin/events/:id/guest-stars/:blockId", h.adminUpdateGuestStar)
	g.DELETE("/admin/events/:id/guest-stars/:blockId", h.adminDeleteGuestStar)

	g.GET("/admin/events/:id/guidelines", h.adminListGuidelines)
	g.POST("/admin/events/:id/guidelines", h.adminCreateGuideline)
	g.PUT("/admin/events/:id/guidelines/:blockId", h.adminUpdateGuideline)
	g.DELETE("/admin/events/:id/guidelines/:blockId", h.adminDeleteGuideline)
}

// contentIDs pulls the (event, block) pair every nested route carries.
func contentIDs(c echo.Context) (eventID, blockID uuid.UUID, err error) {
	eventID, err = uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, apperr.BadRequest(apperr.CodeValidation, "The event id is not a valid UUID.")
	}
	if raw := c.Param("blockId"); raw != "" {
		blockID, err = uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, uuid.Nil, apperr.BadRequest(apperr.CodeValidation, "The block id is not a valid UUID.")
		}
	}
	return eventID, blockID, nil
}

func bindContent[T any](c echo.Context) (T, error) {
	var req T
	if err := c.Bind(&req); err != nil {
		return req, apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}
	return req, nil
}

func (h *Handler) adminGetTerms(c echo.Context) error {
	eventID, _, err := contentIDs(c)
	if err != nil {
		return err
	}
	terms, err := h.svc.AdminTermsForEvent(c.Request().Context(), eventID)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, terms)
}

func (h *Handler) adminPutTerms(c echo.Context) error {
	eventID, _, err := contentIDs(c)
	if err != nil {
		return err
	}
	req, err := bindContent[TermsUpsertRequest](c)
	if err != nil {
		return err
	}
	terms, err := h.svc.UpsertTerms(c.Request().Context(), eventID, req)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, terms)
}

func (h *Handler) adminListActivities(c echo.Context) error {
	eventID, _, err := contentIDs(c)
	if err != nil {
		return err
	}
	out, err := h.svc.ListActivities(c.Request().Context(), eventID)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, out)
}

func (h *Handler) adminCreateActivity(c echo.Context) error {
	eventID, _, err := contentIDs(c)
	if err != nil {
		return err
	}
	req, err := bindContent[ActivityRequest](c)
	if err != nil {
		return err
	}
	created, err := h.svc.CreateActivity(c.Request().Context(), eventID, req)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusCreated, created)
}

func (h *Handler) adminUpdateActivity(c echo.Context) error {
	eventID, blockID, err := contentIDs(c)
	if err != nil {
		return err
	}
	req, err := bindContent[ActivityRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.UpdateActivity(c.Request().Context(), blockID, eventID, req); err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, nil)
}

func (h *Handler) adminDeleteActivity(c echo.Context) error {
	eventID, blockID, err := contentIDs(c)
	if err != nil {
		return err
	}
	if err := h.svc.DeleteActivity(c.Request().Context(), blockID, eventID); err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, nil)
}

func (h *Handler) adminListGuestStars(c echo.Context) error {
	eventID, _, err := contentIDs(c)
	if err != nil {
		return err
	}
	out, err := h.svc.ListGuestStars(c.Request().Context(), eventID)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, out)
}

func (h *Handler) adminCreateGuestStar(c echo.Context) error {
	eventID, _, err := contentIDs(c)
	if err != nil {
		return err
	}
	req, err := bindContent[GuestStarRequest](c)
	if err != nil {
		return err
	}
	created, err := h.svc.CreateGuestStar(c.Request().Context(), eventID, req)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusCreated, created)
}

func (h *Handler) adminUpdateGuestStar(c echo.Context) error {
	eventID, blockID, err := contentIDs(c)
	if err != nil {
		return err
	}
	req, err := bindContent[GuestStarRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.UpdateGuestStar(c.Request().Context(), blockID, eventID, req); err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, nil)
}

func (h *Handler) adminDeleteGuestStar(c echo.Context) error {
	eventID, blockID, err := contentIDs(c)
	if err != nil {
		return err
	}
	if err := h.svc.DeleteGuestStar(c.Request().Context(), blockID, eventID); err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, nil)
}

func (h *Handler) adminListGuidelines(c echo.Context) error {
	eventID, _, err := contentIDs(c)
	if err != nil {
		return err
	}
	out, err := h.svc.ListGuidelines(c.Request().Context(), eventID)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, out)
}

func (h *Handler) adminCreateGuideline(c echo.Context) error {
	eventID, _, err := contentIDs(c)
	if err != nil {
		return err
	}
	req, err := bindContent[GuidelineRequest](c)
	if err != nil {
		return err
	}
	created, err := h.svc.CreateGuideline(c.Request().Context(), eventID, req)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusCreated, created)
}

func (h *Handler) adminUpdateGuideline(c echo.Context) error {
	eventID, blockID, err := contentIDs(c)
	if err != nil {
		return err
	}
	req, err := bindContent[GuidelineRequest](c)
	if err != nil {
		return err
	}
	if err := h.svc.UpdateGuideline(c.Request().Context(), blockID, eventID, req); err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, nil)
}

func (h *Handler) adminDeleteGuideline(c echo.Context) error {
	eventID, blockID, err := contentIDs(c)
	if err != nil {
		return err
	}
	if err := h.svc.DeleteGuideline(c.Request().Context(), blockID, eventID); err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, nil)
}
