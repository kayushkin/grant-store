package grantstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// RegisterHandlers mounts the API on mux. Routes are rooted at / — dash adds
// its own /api/grants prefix and supplies the auth this service has none of,
// exactly as it does for principal-store. That is also why main.go binds
// 127.0.0.1 and not *: with no auth of its own, the front door has to be the
// only door.
//
// directory is principal-store and checker is the resource owners. Both are
// required: a nil one panics here, at boot, rather than at the first POST.
func RegisterHandlers(mux *http.ServeMux, s *Store, directory PrincipalDirectory, checker ResourceChecker) {
	if directory == nil {
		panic("grant-store: RegisterHandlers needs a PrincipalDirectory; without one a grant's principal cannot be checked against principal-store")
	}
	if checker == nil {
		panic("grant-store: RegisterHandlers needs a ResourceChecker; without one a grant's resource cannot be checked against its owner")
	}
	h := &handler{s: s, directory: directory, checker: checker}
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /relations", h.relations)
	mux.HandleFunc("GET /resource-types", h.resourceTypes)

	mux.HandleFunc("GET /grants", h.listGrants)
	mux.HandleFunc("POST /grants", h.createGrant)
	mux.HandleFunc("GET /grants/{id}", h.getGrant)
	mux.HandleFunc("POST /grants/{id}/revoke", h.revokeGrant)

	mux.HandleFunc("GET /principals/{id}/effective", h.effective)
}

type handler struct {
	s         *Store
	directory PrincipalDirectory
	checker   ResourceChecker
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	counts, err := h.s.Counts()
	if respondStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "counts": counts})
}

func (h *handler) relations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Relations)
}

func (h *handler) resourceTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ResourceTypes)
}

func (h *handler) listGrants(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	grants, err := h.s.List(Filter{
		PrincipalID:    q.Get("principal_id"),
		Relation:       q.Get("relation"),
		ResourceType:   q.Get("resource_type"),
		ResourceID:     q.Get("resource_id"),
		IncludeRevoked: isTrue(q.Get("include_revoked")),
		Limit:          int(atoi64(q.Get("limit"))),
		Offset:         int(atoi64(q.Get("offset"))),
	})
	if respondStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, grants)
}

func (h *handler) createGrant(w http.ResponseWriter, r *http.Request) {
	var request GrantRequest
	if !decode(w, r, &request) {
		return
	}
	grant, created, err := h.s.Create(r.Context(), h.directory, h.checker, request)
	if respondStoreError(w, err) {
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, grant)
}

func (h *handler) getGrant(w http.ResponseWriter, r *http.Request) {
	grant, err := h.s.Get(r.PathValue("id"))
	if respondStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

func (h *handler) revokeGrant(w http.ResponseWriter, r *http.Request) {
	grant, err := h.s.Revoke(r.PathValue("id"))
	if respondStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

func (h *handler) effective(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	grants, err := h.s.Effective(r.Context(), h.directory, r.PathValue("id"), q.Get("relation"), q.Get("resource_type"))
	if respondStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, grants)
}

// decode reads a JSON body, rejecting unknown fields so a misspelled key is a
// 400 rather than a write that silently drops it.
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		fmt.Printf("grant-store: encode response: %v\n", err)
	}
}

func writeErr(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func respondStoreError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalidGrant):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrOwnerUnavailable):
		// principal-store or a resource owner, not this store and not the
		// caller, is what failed.
		writeErr(w, http.StatusBadGateway, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
	return true
}

func atoi64(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func isTrue(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
