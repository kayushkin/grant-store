package grantstore

import (
	"fmt"
	"strings"
)

// The relation vocabulary is served (GET /relations) so no caller builds a
// picker out of whatever values happen to be in the rows. Each relation names
// the resource types it may pair with; a grant pairing a relation with a type
// outside that list is a 400 naming the allowed ones.
//
// can_view, can_edit and can_administer are board access, enforced by
// kanban-store when it runs with principal enforcement on. This store keeps
// them as three independent tuples; kanban-store decides that can_administer
// includes can_edit and can_edit includes can_view, because what a relation on
// a board lets someone do is the board owner's rule, not this store's.
//
// Enforced relations are the ones llm-bridge-server reads at session start:
// a session with a principal is offered only the tools and skills it can_use,
// may only run as an agent it can_run_as, and may only start on an instance
// or machine it can_dispatch_on. works_with is advisory — it is the list
// principal-store used to keep, moved here so there is one place to look —
// and nothing refuses anything because of it.
//
// Nothing here is normalised: the relation arrives in a body and is stored as
// a join key, so "Can_Use" is a 400 naming the right spelling, not a rewrite.

const (
	RelationCanUse        = "can_use"
	RelationCanRunAs      = "can_run_as"
	RelationCanDispatchOn = "can_dispatch_on"
	RelationWorksWith     = "works_with"
	RelationCanView       = "can_view"
	RelationCanEdit       = "can_edit"
	RelationCanAdminister = "can_administer"
)

// RelationDefinition is everything a caller needs to offer one relation.
type RelationDefinition struct {
	Name          string   `json:"name"`
	ResourceTypes []string `json:"resource_types"`
	// Enforced says llm-bridge-server filters a session's offer by this
	// relation. False means the relation is a list someone reads, not a lock.
	Enforced    bool   `json:"enforced"`
	Description string `json:"description"`
}

// Relations is every relation a grant can carry, in the order GET /relations
// serves them.
var Relations = []RelationDefinition{
	{
		Name:          RelationCanUse,
		ResourceTypes: []string{ResourceTypeTool, ResourceTypeSkill},
		Enforced:      true,
		Description:   "may be offered this tool or skill in a session started as this principal",
	},
	{
		Name:          RelationCanRunAs,
		ResourceTypes: []string{ResourceTypeAgent},
		Enforced:      true,
		Description:   "may start a session as this agent",
	},
	{
		Name:          RelationCanDispatchOn,
		ResourceTypes: []string{ResourceTypeInstance, ResourceTypeMachine},
		Enforced:      true,
		Description:   "may start a session on this harness instance or machine",
	},
	{
		Name:          RelationCanView,
		ResourceTypes: []string{ResourceTypeBoard},
		Enforced:      true,
		Description:   "may see this kanban board and its cards; kanban-store enforces it",
	},
	{
		Name:          RelationCanEdit,
		ResourceTypes: []string{ResourceTypeBoard},
		Enforced:      true,
		Description:   "may create, move, assign and annotate cards on this kanban board, and includes can_view; kanban-store enforces it",
	},
	{
		Name:          RelationCanAdminister,
		ResourceTypes: []string{ResourceTypeBoard},
		Enforced:      true,
		Description:   "may change this kanban board's settings, columns, rules and triggers, or delete it, and includes can_edit; kanban-store enforces it",
	},
	{
		Name:          RelationWorksWith,
		ResourceTypes: []string{ResourceTypeAgent, ResourceTypeInstance, ResourceTypeMachine, ResourceTypeSkill, ResourceTypeTool},
		Enforced:      false,
		Description:   "advisory: what this principal usually works with; a card's dispatch picker reads it to put an assignee's instances first, and nothing refuses anything because of it",
	},
}

// RelationNames is the names alone, for error messages.
var RelationNames = func() []string {
	names := make([]string, 0, len(Relations))
	for _, definition := range Relations {
		names = append(names, definition.Name)
	}
	return names
}()

// lookupRelation resolves a caller's relation to its definition, or the 400
// that names the whole vocabulary.
func lookupRelation(raw string) (RelationDefinition, error) {
	for _, definition := range Relations {
		if raw == definition.Name {
			return definition, nil
		}
	}
	return RelationDefinition{}, fmt.Errorf("%w: unknown relation %q: use one of %s",
		ErrInvalidGrant, raw, strings.Join(RelationNames, ", "))
}

// allowsResourceType says whether a relation may pair with a resource type.
func (d RelationDefinition) allowsResourceType(resourceType string) bool {
	for _, allowed := range d.ResourceTypes {
		if allowed == resourceType {
			return true
		}
	}
	return false
}
