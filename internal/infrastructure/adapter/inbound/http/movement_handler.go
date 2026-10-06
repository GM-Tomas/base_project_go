package http

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/go-chi/chi/v5"
)

const (
	defaultMovementsLimit = 50
	maxMovementsLimit     = 200
	dateLayout            = "2006-01-02"
)

type MovementHandler struct {
	movementUseCase inbound.MovementUseCase
}

func NewMovementHandler(movementUseCase inbound.MovementUseCase) *MovementHandler {
	return &MovementHandler{movementUseCase: movementUseCase}
}

// parseWhen reads a date (YYYY-MM-DD) or an RFC 3339 instant. A date stands for the given time of that day,
// in UTC; an instant is kept as sent, in UTC.
func parseWhen(raw string, dayTime time.Duration) (time.Time, bool) {
	if day, err := time.Parse(dateLayout, raw); err == nil {
		return day.Add(dayTime), true
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	return at.UTC(), err == nil
}

// A date-only occurredAt is noon UTC: no time zone within ±11 h moves it to another day.
const noon = 12 * time.Hour

// parseHoldingRef reads a holding id sent in a body: one that isn't a UUID can't exist, so it's not found,
// as an id in a path is.
func parseHoldingRef(raw string) (*model.HoldingId, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := model.ParseHoldingId(raw)
	if err != nil {
		return nil, errors.NewResourceNotFoundError("Holding " + raw + " not found")
	}
	return &id, nil
}

func (h *MovementHandler) RecordMovement(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	var req dto.CreateMovementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	cmd := inbound.RecordMovementCommand{
		UserId: userId, Kind: req.Kind, AmountUsd: req.AmountUsd, FeeUsd: req.FeeUsd, Note: req.Note,
	}
	if req.AmountUsd > maxHoldingValueUsd || req.FeeUsd > maxHoldingValueUsd {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{{Field: "amountUsd", Message: "Amount is too large"}}))
		return
	}
	if req.OccurredAt != "" {
		at, ok := parseWhen(req.OccurredAt, noon)
		if !ok {
			middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
				{Field: "occurredAt", Message: "occurredAt must be a date (YYYY-MM-DD) or a date and time (RFC 3339)"},
			}))
			return
		}
		cmd.OccurredAt = &at
	}
	for _, ref := range []struct {
		raw string
		dst **model.HoldingId
	}{{req.HoldingId, &cmd.HoldingId}, {req.FromHoldingId, &cmd.FromHoldingId}, {req.ToHoldingId, &cmd.ToHoldingId}} {
		if *ref.dst, err = parseHoldingRef(ref.raw); err != nil {
			middleware.HandleError(w, r, err)
			return
		}
	}
	if req.DebtId != "" {
		// Like a holding's: an id that isn't a UUID can't exist.
		id, err := model.ParseDebtId(req.DebtId)
		if err != nil {
			middleware.HandleError(w, r, errors.NewResourceNotFoundError("Debt "+req.DebtId+" not found"))
			return
		}
		cmd.DebtId = &id
	}
	if n := req.ToNewHolding; n != nil {
		cmd.ToNewHolding = &inbound.NewHoldingInput{Name: n.Name, AssetClass: n.AssetClass, Platform: n.Platform}
	}

	view, err := h.movementUseCase.RecordMovement(r.Context(), cmd)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/movements/"+view.Movement.Id.String())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toMovementResponse(view))
}

// ListMovements is the activity log, newest first, a page at a time: ?holdingId, ?kind (comma-separated),
// ?from and ?to (dates cover the whole UTC day), ?limit (1 to 200, 50 by default) and ?cursor (the
// nextCursor of the page before).
func (h *MovementHandler) ListMovements(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	query, problems := parseMovementQuery(r)
	if len(problems) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(problems))
		return
	}

	list, err := h.movementUseCase.ListMovements(r.Context(), userId, query)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	res := dto.MovementListResponse{Items: make([]dto.MovementResponse, len(list.Items))}
	for i, view := range list.Items {
		res.Items[i] = toMovementResponse(view)
	}
	if list.Next != nil {
		cursor := encodeCursor(*list.Next)
		res.NextCursor = &cursor
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func parseMovementQuery(r *http.Request) (inbound.MovementQuery, []errors.ValidationError) {
	q := r.URL.Query()
	query := inbound.MovementQuery{Limit: defaultMovementsLimit}
	var problems []errors.ValidationError
	fail := func(field, message string) {
		problems = append(problems, errors.ValidationError{Field: field, Message: message})
	}

	if raw := q.Get("holdingId"); raw != "" {
		if id, err := model.ParseHoldingId(raw); err != nil {
			fail("holdingId", "holdingId must be a holding's id")
		} else {
			query.Filter.HoldingId = &id
		}
	}
	if raw := q.Get("debtId"); raw != "" {
		if id, err := model.ParseDebtId(raw); err != nil {
			fail("debtId", "debtId must be a debt's id")
		} else {
			query.Filter.DebtId = &id
		}
	}
	if raw := q.Get("kind"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			kind, err := model.ParseMovementKind(strings.TrimSpace(part))
			if err != nil {
				fail("kind", "kind must be a comma-separated list of OPENING, CLOSING, GAIN, LOSS, DEPOSIT, WITHDRAWAL, TRANSFER, ADJUSTMENT, DEBT_PAYMENT, DEBT_CHARGE, DEBT_INTEREST")
				break
			}
			query.Filter.Kinds = append(query.Filter.Kinds, kind)
		}
	}
	query.Filter.From, query.Filter.To = parsePeriod(q, fail)
	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maxMovementsLimit {
			fail("limit", "limit must be between 1 and 200")
		} else {
			query.Limit = limit
		}
	}
	if raw := q.Get("cursor"); raw != "" {
		cursor, ok := decodeCursor(raw)
		if !ok {
			fail("cursor", "cursor is not one this API gave")
		} else {
			query.After = &cursor
		}
	}
	return query, problems
}

// SummarizeMovements is GET /movements/summary?from&to: what the movements of a period add up to, by bucket,
// and what they did to the net worth. from and to as in ListMovements (a date covers its whole UTC day);
// without them, since 1970 and until now.
func (h *MovementHandler) SummarizeMovements(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	var problems []errors.ValidationError
	from, to := parsePeriod(r.URL.Query(), func(field, message string) {
		problems = append(problems, errors.ValidationError{Field: field, Message: message})
	})
	if len(problems) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(problems))
		return
	}
	result, err := h.movementUseCase.SummarizeMovements(r.Context(), userId, from, to)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	s := result.Summary
	totals := make(map[string]float64, len(s.Totals))
	for bucket, total := range s.Totals {
		totals[string(bucket)] = total.Round(2).InexactFloat64()
	}
	writeJSON(w, http.StatusOK, dto.MovementsSummaryResponse{
		From: result.From, To: result.To, Count: s.Count, Transfers: s.Transfers, TotalsUsd: totals,
		NetWorthEffectUsd: dto.NetWorthEffectDTO{
			Investments:  s.Effect.Investments.Round(2).InexactFloat64(),
			Saving:       s.Effect.Saving.Round(2).InexactFloat64(),
			AddedRemoved: s.Effect.AddedRemoved.Round(2).InexactFloat64(),
			Corrections:  s.Effect.Corrections.Round(2).InexactFloat64(),
		},
	})
}

// parsePeriod reads ?from and ?to: a date covers the whole UTC day, from its first instant to its last.
func parsePeriod(q url.Values, fail func(field, message string)) (from, to *time.Time) {
	for _, bound := range []struct {
		name    string
		dayTime time.Duration
		dst     **time.Time
	}{{"from", 0, &from}, {"to", 24*time.Hour - time.Nanosecond, &to}} {
		if raw := q.Get(bound.name); raw != "" {
			at, ok := parseWhen(raw, bound.dayTime)
			if !ok {
				fail(bound.name, bound.name+" must be a date (YYYY-MM-DD) or a date and time (RFC 3339)")
				continue
			}
			*bound.dst = &at
		}
	}
	return from, to
}

func (h *MovementHandler) RevertMovement(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := model.ParseMovementId(idStr)
	if err != nil {
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("Movement "+idStr+" not found"))
		return
	}

	if err := h.movementUseCase.RevertMovement(r.Context(), userId, id); err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// A cursor is opaque to clients: the last item's position, base64url-encoded.
func encodeCursor(c model.MovementCursor) string {
	raw := c.OccurredAt.UTC().Format(time.RFC3339Nano) + "|" + c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.Id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (model.MovementCursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return model.MovementCursor{}, false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return model.MovementCursor{}, false
	}
	occurredAt, err1 := time.Parse(time.RFC3339Nano, parts[0])
	createdAt, err2 := time.Parse(time.RFC3339Nano, parts[1])
	id, err3 := model.ParseMovementId(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return model.MovementCursor{}, false
	}
	return model.MovementCursor{OccurredAt: occurredAt, CreatedAt: createdAt, Id: id}, true
}

func toMovementHoldingResponse(ref *model.HoldingRef, exists bool) *dto.MovementHoldingResponse {
	if ref == nil {
		return nil
	}
	return &dto.MovementHoldingResponse{
		Id: ref.Id.String(), Name: ref.Name, Platform: ref.Platform.Value(), AssetClass: ref.AssetClass.Value(), Exists: exists,
	}
}

func optionalUsd(m *model.Money) *float64 {
	if m == nil {
		return nil
	}
	v := m.Float64()
	return &v
}

func toMovementResponse(view inbound.MovementView) dto.MovementResponse {
	m := view.Movement
	res := dto.MovementResponse{
		Id:               m.Id.String(),
		Kind:             string(m.Kind),
		OccurredAt:       m.OccurredAt,
		CreatedAt:        m.CreatedAt,
		AmountUsd:        m.Amount.Float64(),
		Holding:          toMovementHoldingResponse(m.Holding, view.HoldingExists),
		ToHolding:        toMovementHoldingResponse(m.ToHolding, view.ToHoldingExists),
		Debt:             toMovementDebtResponse(m.Debt, view.DebtExists),
		PreviousValueUsd: optionalUsd(m.PreviousValue),
		NewValueUsd:      optionalUsd(m.NewValue),
		Revertible:       view.Revertible,
	}
	if m.Kind == model.MovementTransfer {
		res.FeeUsd = optionalUsd(&m.Fee)
	}
	if m.Note != "" {
		res.Note = &m.Note
	}
	return res
}

func toMovementDebtResponse(ref *model.DebtRef, exists bool) *dto.MovementDebtResponse {
	if ref == nil {
		return nil
	}
	return &dto.MovementDebtResponse{Id: ref.Id.String(), Name: ref.Name, Lender: optionalText(ref.Lender), Exists: exists}
}
