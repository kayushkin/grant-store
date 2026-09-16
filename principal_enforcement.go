package grantstore

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"regexp"
)

// Principal enforcement: who may read and change grants.
//
// Off unless RegisterHandlersWithPrincipalEnforcement is used. With it on,
// every route except /health, /relations and /resource-types needs one of:
//
//   - X-Grant-Store-Service-Token matching the configured token: an internal
//     service (llm-bridge-server reading a session's grants at spawn,
//     kanban-store reading a request's board grants, an operator's script).
//     Unrestricted.
//   - X-Principal-Id, set by a gateway from a verified login and trusted as
//     sent. The principal must be a human principal-store knows and has not
//     disabled.
//
// Neither is 401. A principal may:
//
//   - create or revoke a board grant only on a board it holds can_administer on,
//     directly or through a group. Every other resource type — tools, skills,
//     agents, instances, machines — is the operator's, through the service
//     token.
//   - read a grant held by itself, or any grant on a board it administers.
//   - list grants only with principal_id set to itself, or with
//     resource_type=board and resource_id set to a board it administers.
//   - read GET /principals/{id}/effective only for its own id.
//
// A grant a principal may not read is 404, the same as one that does not exist.
// A board administrator can grant can_administer to someone else, and that is
// deliberate: handing a board over is part of administering it.

const (
	PrincipalIDHeader  = "X-Principal-Id"
	ServiceTokenHeader = "X-Grant-Store-Service-Token"
)

// PrincipalEnforcement configures RegisterHandlersWithPrincipalEnforcement.
type PrincipalEnforcement struct {
	// ServiceToken must be at least 32 characters; an empty one would match
	// every request that leaves the header out.
	ServiceToken string
}

var principalIDShape = regexp.MustCompile(`^principal_\d{6,}$`)

// RegisterHandlersWithPrincipalEnforcement is RegisterHandlers with the rules
// above applied to every route. It panics on a short service token, at boot.
func RegisterHandlersWithPrincipalEnforcement(mux *http.ServeMux, s *Store, directory PrincipalDirectory, checker ResourceChecker, enforcement PrincipalEnforcement) {
	if len(enforcement.ServiceToken) < 32 {
		panic("grant-store: principal enforcement needs a service token of at least 32 characters")
	}
	registerHandlers(mux, s, directory, checker, &enforcement)
}

// grantCaller is who a request acts as. unrestricted is the service token, or
// enforcement being off.
type grantCaller struct {
	unrestricted bool
	principalID  string
}

// identifyCaller answers the request itself (401 or 502) and returns false
// when the caller cannot be established.
func (h *handler) identifyCaller(w http.ResponseWriter, r *http.Request) (grantCaller, bool) {
	if h.enforcement == nil {
		return grantCaller{unrestricted: true}, true
	}
	if token := r.Header.Get(ServiceTokenHeader); token != "" {
		if subtle.ConstantTimeCompare([]byte(token), []byte(h.enforcement.ServiceToken)) != 1 {
			writeErr(w, http.StatusUnauthorized, ServiceTokenHeader+" does not match this store's service token")
			return grantCaller{}, false
		}
		return grantCaller{unrestricted: true}, true
	}
	principalID := r.Header.Get(PrincipalIDHeader)
	if principalID == "" {
		writeErr(w, http.StatusUnauthorized, "principal enforcement is on: send "+PrincipalIDHeader+" (set by the gateway from a login) or "+ServiceTokenHeader)
		return grantCaller{}, false
	}
	if !principalIDShape.MatchString(principalID) {
		writeErr(w, http.StatusUnauthorized, fmt.Sprintf("%s %q is not a principal-store id (principal_000001)", PrincipalIDHeader, principalID))
		return grantCaller{}, false
	}
	summary, err := h.directory.LookupPrincipal(r.Context(), principalID)
	if errors.Is(err, ErrPrincipalNotFound) {
		writeErr(w, http.StatusUnauthorized, fmt.Sprintf("%s %s does not exist in principal-store", PrincipalIDHeader, principalID))
		return grantCaller{}, false
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("could not read %s from principal-store, so the caller is unknown: %v", principalID, err))
		return grantCaller{}, false
	}
	if summary.Kind != "human" || summary.DisabledAt != 0 {
		writeErr(w, http.StatusUnauthorized, fmt.Sprintf("%s %s is not an active human principal, and only a person acts", PrincipalIDHeader, principalID))
		return grantCaller{}, false
	}
	return grantCaller{principalID: principalID}, true
}

// callerAdministersBoard reads the caller's effective can_administer grants,
// which include those held through a group.
func (h *handler) callerAdministersBoard(r *http.Request, caller grantCaller, boardID string) (bool, error) {
	if caller.unrestricted {
		return true, nil
	}
	grants, err := h.s.Effective(r.Context(), h.directory, caller.principalID, RelationCanAdminister, ResourceTypeBoard)
	if err != nil {
		return false, err
	}
	for _, grant := range grants {
		if grant.ResourceID == boardID {
			return true, nil
		}
	}
	return false, nil
}

// callerMayReadGrant: its own grant, or a grant on a board it administers.
func (h *handler) callerMayReadGrant(r *http.Request, caller grantCaller, grant *Grant) (bool, error) {
	if caller.unrestricted || grant.PrincipalID == caller.principalID {
		return true, nil
	}
	if grant.ResourceType != ResourceTypeBoard {
		return false, nil
	}
	return h.callerAdministersBoard(r, caller, grant.ResourceID)
}
