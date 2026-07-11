package permissions

import "strings"

type Policy struct {
	grants []Grant
}

func NewPolicy(grants []Grant) *Policy {
	return &Policy{grants: append([]Grant(nil), grants...)}
}

func (p *Policy) Check(request Request) Decision {
	if request.SubjectID == "" {
		return Decision{Allowed: false, Reason: "subject id is required"}
	}
	if request.Capability == "" {
		return Decision{Allowed: false, Reason: "capability is required"}
	}

	for _, grant := range p.grants {
		if grant.SubjectID != request.SubjectID {
			continue
		}
		if grant.Capability != request.Capability {
			continue
		}
		if scopeMatches(grant.Scope, request.Scope) {
			return Decision{Allowed: true}
		}
	}

	return Decision{Allowed: false, Reason: "permission grant not found"}
}

func scopeMatches(grantScope string, requestScope string) bool {
	if grantScope == requestScope {
		return true
	}
	if grantScope == "*" {
		return true
	}
	if !strings.HasSuffix(grantScope, "*") {
		return false
	}
	return strings.HasPrefix(requestScope, strings.TrimSuffix(grantScope, "*"))
}
