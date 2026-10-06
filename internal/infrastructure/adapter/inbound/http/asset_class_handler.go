package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/go-chi/chi/v5"
)

type AssetClassHandler struct {
	assetClassUseCase inbound.AssetClassUseCase
}

func NewAssetClassHandler(assetClassUseCase inbound.AssetClassUseCase) *AssetClassHandler {
	return &AssetClassHandler{
		assetClassUseCase: assetClassUseCase,
	}
}

func (h *AssetClassHandler) GetAvailableAssetClasses(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	res, err := h.assetClassUseCase.GetAvailableAssetClasses(r.Context(), userId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	classes := make([]dto.AssetClassResponse, len(res.Classes))
	for i, c := range res.Classes {
		classes[i] = toAssetClassResponse(c)
	}
	writeJSON(w, http.StatusOK, dto.AvailableAssetClassesResponse{
		Defaults: res.Defaults,
		InUse:    res.InUse,
		All:      res.All,
		Classes:  classes,
	})
}

// classChecks collects a class body's problems, field by field, with the messages the API answers.
type classChecks struct {
	problems []errors.ValidationError
}

func (c *classChecks) fail(field, message string) {
	c.problems = append(c.problems, errors.ValidationError{Field: field, Message: message})
}

func (c *classChecks) name(v string) {
	if strings.TrimSpace(v) == "" {
		c.fail("name", "Name is required")
	} else if _, err := model.NewAssetClass(v); err != nil {
		c.fail("name", fmt.Sprintf("Name must be at most %d characters", model.MaxAssetClassLength))
	}
}

func (c *classChecks) color(v string) {
	if _, err := model.NewColor(v); err != nil {
		c.fail("color", err.Error())
	}
}

func (c *classChecks) expectedReturn(v float64) {
	if !validReturn(v) {
		c.fail("expectedReturnPct", model.ErrExpectedReturnOutOfRange.Error())
	}
}

// CreateAssetClass is POST /asset-classes.
func (h *AssetClassHandler) CreateAssetClass(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	var req dto.CreateAssetClassRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}
	var c classChecks
	c.name(req.Name)
	if req.Color != nil {
		c.color(*req.Color)
	}
	if req.ExpectedReturnPct != nil {
		c.expectedReturn(*req.ExpectedReturnPct)
	}
	if len(c.problems) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(c.problems))
		return
	}

	created, err := h.assetClassUseCase.CreateAssetClass(r.Context(), inbound.CreateAssetClassCommand{
		UserId: userId, Name: req.Name, Color: req.Color, Liquid: req.Liquid, ExpectedReturnPct: req.ExpectedReturnPct,
	})
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	res := toAssetClassResponse(created)
	w.Header().Set("Location", "/api/v1/asset-classes/"+res.Id)
	writeJSON(w, http.StatusCreated, res)
}

// UpdateAssetClass is PATCH /asset-classes/{id}: the fields sent change (null clears color and the return,
// and sets liquid back to its default). A new name renames the class on all its holdings, or, with
// mergeIfExists, merges it into another the user has.
func (h *AssetClassHandler) UpdateAssetClass(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	var req dto.UpdateAssetClassRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	var c classChecks
	cmd := inbound.UpdateAssetClassCommand{UserId: userId, Id: chi.URLParam(r, "id"), MergeIfExists: req.MergeIfExists}
	if sent := req.Name; sent.Set {
		c.name(sent.Value) // null reads as blank: required
		cmd.Name = &sent.Value
	}
	cmd.Color = change(req.Color, c.color)
	cmd.Liquid = change(req.Liquid, func(bool) {})
	cmd.ExpectedReturnPct = change(req.ExpectedReturnPct, c.expectedReturn)
	if len(c.problems) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(c.problems))
		return
	}

	updated, err := h.assetClassUseCase.UpdateAssetClass(r.Context(), cmd)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAssetClassResponse(updated))
}

// DeleteAssetClass is DELETE /asset-classes/{id}?moveTo=<class>: its holdings, if any, move to moveTo.
func (h *AssetClassHandler) DeleteAssetClass(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	cmd := inbound.DeleteAssetClassCommand{UserId: userId, Id: chi.URLParam(r, "id")}
	if query := r.URL.Query(); query.Has("moveTo") {
		moveTo := query.Get("moveTo")
		cmd.MoveTo = &moveTo
	}
	if err := h.assetClassUseCase.DeleteAssetClass(r.Context(), cmd); err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAssetClassResponse(c inbound.AssetClassView) dto.AssetClassResponse {
	return dto.AssetClassResponse{
		Id:                model.AssetClassId(c.Name),
		Name:              c.Name.Value(),
		Color:             dto.ColorOf(c.Color),
		Liquid:            c.Liquid,
		ExpectedReturnPct: optionalFloat(c.ExpectedReturnPct),
		IsDefault:         c.IsDefault,
		HoldingsCount:     c.HoldingsCount,
		ValueUsd:          c.Value.Float64(),
	}
}

// writeJSON answers with status and body as JSON.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
