package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/model"
)

// listCategoriesResponse is the payload of GET /api/v1/categories.
type listCategoriesResponse struct {
	Count      int              `json:"count"`
	Categories []model.Category `json:"categories"`
}

// handleListCategories returns every category, including the two the API itself
// depends on.
func (s *Server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := s.svc.ListCategories(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if categories == nil {
		categories = []model.Category{}
	}
	s.writeJSON(w, http.StatusOK, listCategoriesResponse{
		Count:      len(categories),
		Categories: categories,
	})
}

// handleCreateCategory adds a category.
func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var in model.CategoryInput
	if err := decodeJSON(w, r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	category, err := s.svc.CreateCategory(r.Context(), in)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/categories/"+formatID(category.ID))
	s.writeJSON(w, http.StatusCreated, category)
}

// handleGetCategory returns one category.
func (s *Server) handleGetCategory(w http.ResponseWriter, r *http.Request) {
	id, err := parseCategoryID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	category, err := s.svc.GetCategory(r.Context(), id)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, category)
}

// handlePatchCategory renames or recolours one category.
func (s *Server) handlePatchCategory(w http.ResponseWriter, r *http.Request) {
	id, err := parseCategoryID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var in model.CategoryInput
	if err := decodeJSON(w, r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	category, err := s.svc.UpdateCategory(r.Context(), id, in)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, category)
}

// handleDeleteCategory removes a category, moving its events to uncategorized.
func (s *Server) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := parseCategoryID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.svc.DeleteCategory(r.Context(), id); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseCategoryID reads the {id} segment for a category.
//
// Category ids are now 0 (the default) or a positive user id: the -1 that used
// to mean "deleted" became a column on the event, so a negative id is not a
// category and is reported as a bad parameter.
func parseCategoryID(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, apperr.Param("id", "must be a non-negative integer")
	}
	return id, nil
}
