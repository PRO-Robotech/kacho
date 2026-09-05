// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// permission_denied_response.go — shape Permission-Denied responses for both
// gRPC and HTTP/REST entry points.
//
// gRPC: returns a `status.Status{Code: PermissionDenied}` enriched with
// `google.rpc.PreconditionFailure` violations — one per deny reason. UI
// clients introspect the violations to render actionable error messages
// (e.g. "step up your session" vs "no permission").
//
// HTTP: returns 403 Forbidden with a JSON body shaped like the gRPC-gateway
// default (`{code, message, details}`) plus a `WWW-Authenticate` header when
// the deny reason indicates step-up is needed (mfa_fresh / non-CRITICAL
// freshness violation). The WWW-Authenticate value follows RFC 9470 §3:
// `Bearer error="insufficient_user_authentication", acr_values="3"`.
//
// The catalog dictates the WWW-Authenticate construction (required_acr_min);
// runtime deny-reasons inform `error_description` for diagnostics.
package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// safeDenyDescription returns a generic, NON-LEAKING violation description for a
// 403 deny reason. The raw reason (e.g. "no path: account:acc_secret has no
// v_delete for user:usr_x") names the resource + the FGA relation — an existence/
// authz oracle that must never reach the wire (registry-authz finding, #64). This
// preserves the ONE actionable distinction the client needs — whether re-auth with
// a stronger session would help (step-up) vs a plain lack of authorization — WITHOUT
// echoing the reason; the machine-readable violation TYPE (classifyDenyReasonType)
// still carries the category for programmatic handling, and the raw reason stays in
// the server logs only.
func safeDenyDescription(reason string) string {
	// "unauthenticated" reports the CALLER's own auth state (they know it) — not a
	// resource oracle — and clients/observability key on it, so it is preserved.
	if strings.Contains(strings.ToLower(reason), "unauthenticated") {
		return "unauthenticated: authentication required"
	}
	if shouldStepUpChallenge([]string{reason}) {
		return "step-up authentication required"
	}
	return "no authorization path to the resource"
}

// permissionDeniedDescriptor — common subject/action/resource tuple included
// in every deny violation (forensic correlation).
type permissionDeniedDescriptor struct {
	Subject      string
	Action       string
	ResourceType string
	ResourceID   string
	FQN          string
}

// buildGRPCDenyStatus constructs a *status.Status with attached
// PreconditionFailure violations. The message argument is the headline shown
// to clients; reasons populate violation.Subject for machine-readable triage.
func buildGRPCDenyStatus(desc permissionDeniedDescriptor, reasons []string) *status.Status {
	msg := "permission denied"
	if desc.Action != "" {
		msg = "permission denied: " + desc.Action
	}
	st := status.New(codes.PermissionDenied, msg)

	pf := &errdetails.PreconditionFailure{}
	if len(reasons) == 0 {
		pf.Violations = append(pf.Violations, &errdetails.PreconditionFailure_Violation{
			Type:        "authz.no_path",
			Subject:     resourceLabel(desc),
			Description: "no authorization path to the resource",
		})
	} else {
		for _, r := range reasons {
			pf.Violations = append(pf.Violations, &errdetails.PreconditionFailure_Violation{
				Type:        classifyDenyReasonType(r),
				Subject:     resourceLabel(desc),
				Description: safeDenyDescription(r),
			})
		}
	}

	info := &errdetails.ErrorInfo{
		Reason: "AUTHZ_DENIED",
		Domain: "kacho.cloud.iam.v1",
		Metadata: map[string]string{
			"subject":  desc.Subject,
			"action":   desc.Action,
			"resource": resourceLabel(desc),
			"fqn":      desc.FQN,
			// deny_reasons intentionally OMITTED — the joined raw reasons name the
			// resource + FGA relation (existence/authz oracle). Kept in logs only.
		},
	}

	stWithDetails, derr := st.WithDetails(pf, info)
	if derr != nil {
		// Should not happen — PreconditionFailure + ErrorInfo are
		// well-known. Fall back to the bare status.
		return st
	}
	return stWithDetails
}

// buildGRPCNotFoundStatus constructs a *status.Status{Code: NotFound} for a
// hide-existence read deny. It carries NO PreconditionFailure / deny reasons /
// ErrorInfo — the flat message IS the contract, and any reason text would leak
// the existence of (and the authz path to) the resource. The message comes from
// notFoundMessage: for a mapped object type it byte-matches the owning service's
// genuine NotFound, so a denied caller cannot distinguish "exists but forbidden"
// from "does not exist"; otherwise it is the neutral "not found" (see the
// fallback caveat on notFoundMessage).
func buildGRPCNotFoundStatus(desc permissionDeniedDescriptor) *status.Status {
	return status.New(codes.NotFound, notFoundMessage(desc))
}

// hideExistenceNotFoundFormats maps an FGA object type to the Kachō contract-tone
// NotFound message for that resource. The gateway's authz Check runs BEFORE the
// owning service, so a verb-bearing read-deny is answered here (hide existence).
// To keep that 404 indistinguishable from a genuine miss, the message MUST
// byte-match what the owning service returns for a real NotFound — otherwise a
// denied caller can tell "exists but forbidden" (gateway text) from "does not
// exist" (backend text), an existence oracle (security.md hardening-invariant #6).
// Each format has a single %s for the caller-supplied resource id (echoed back —
// the caller already knows it, so no leak).
//
// Every text is copied VERBATIM from the owning service's repo-layer NotFound.
// That repo format is the wire text: each service's error mapper strips only the
// sentinel prefix (iam shared.MapRepoErr → iamerr.StripSentinel, compute
// service.mapRepoErr → stripSentinel, nlb shared.StripSentinel, registry
// wrapPgErr) and passes the message through unchanged. Sources:
//
//	iam       services/iam/internal/repo/kaname/pg/{account,project,user_pool,group,service_account,access_binding}_repo.go
//	compute   services/compute/internal/repo/instance_repo.go
//	vpc       services/vpc/internal/repo/kacho/pg/*.go (+ repo/helpers/sg.go)
//	storage   services/storage/internal/repo/pg/{volume,snapshot,image}_repo.go
//	nlb       services/nlb/internal/repo/kacho/pg/{load_balancer,listener,target_group}_repo.go
//	registry  services/registry/internal/repo/kacho/pg/errmap.go (wrapPgErr composes
//	          "<Resource> %s not found" from the resource name its caller passes)
//
// The map must cover every object type reachable through
// CatalogEntry.HidesExistenceOnDeny; the drift guard
// TestHideExistenceMap_CoversCatalogReachableTypes derives that set from the
// embedded catalog and fails when a new object-scoped resource is added without
// its entry. A new resource adds its line here (text taken from its repo layer,
// never invented) in the same change that makes it object-scoped.
//
// The REVERSE direction is guarded too, and only because it was not:
// TestHideExistenceTablesCarryNoUnreachableType (internal/repohygiene) fails on a
// row no catalog entry can reach. Three block-storage rows survived the retirement
// of compute's duplicate that way, and the source list above went on naming
// repository files that had been deleted.
var hideExistenceNotFoundFormats = map[string]string{
	// iam — services/iam/internal/repo/kaname/pg/*.go
	"account":             "Account %s not found",
	"project":             "Project %s not found",
	"iam_user":            "User %s not found",
	"iam_group":           "Group %s not found",
	"iam_service_account": "ServiceAccount %s not found",
	"iam_access_binding":  "AccessBinding %s not found",
	// compute — services/compute/internal/repo/instance_repo.go. Disk / Image /
	// Snapshot are NOT here: that duplicate of block storage was retired
	// (kacho-storage owns those), and their rows went with it.
	"compute_instance": "Instance %s not found",
	// Ключ входа в машину — services/compute/internal/repo/guest_access_key_repo.go.
	"compute_guest_access_key": "GuestAccessKey %s not found",
	// Группа размещения — services/compute/internal/repo/placement_group_repo.go.
	"compute_placement_group": "PlacementGroup %s not found",
	// vpc — services/vpc/internal/repo/kacho/pg/*.go
	"vpc_network":     "Network %s not found",
	"vpc_subnet":      "Subnet %s not found",
	"vpc_address":     "Address %s not found",
	"vpc_route_table": "Route table %s not found",
	// NOTE: the vpc security-group text carries a leaked debug rendering of the
	// id ("SecurityGroup.Id(value=%s)") rather than the plain contract tone. It is
	// reproduced verbatim ON PURPOSE: byte-identity with the backend is what closes
	// the oracle, so this side may only change together with
	// services/vpc/internal/repo/helpers/sg.go (+ the vpc Newman verbatim-text pack).
	"vpc_security_group":    "Security group SecurityGroup.Id(value=%s) not found",
	"vpc_gateway":           "Gateway %s not found",
	"vpc_network_interface": "Network interface %s not found",
	// CidrGroup — текст слоя хранения vpc дословно
	// (services/vpc/internal/repo/kacho/pg/cidr_group.go).
	"vpc_cidr_group": "CidrGroup %s not found",
	// storage — services/storage/internal/repo/pg/{volume,snapshot,image}_repo.go.
	// These three became reachable the moment storage's object-self reads moved off
	// the tier onto `v_get`: before that, HidesExistenceOnDeny answered false for
	// them and a deny left the gateway as a plain 403, so there was nothing to
	// byte-match. Texts are the repo-layer NotFound verbatim (serviceerr strips only
	// the sentinel prefix — see its errmap contract test).
	"storage_volume":   "Volume %s not found",
	"storage_snapshot": "Snapshot %s not found",
	"storage_image":    "Image %s not found",
	// nlb — services/nlb/internal/repo/kacho/pg/*.go
	"nlb_network_load_balancer": "NetworkLoadBalancer %s not found",
	"nlb_listener":              "Listener %s not found",
	"nlb_target_group":          "TargetGroup %s not found",
	// registry — services/registry/internal/repo/kacho/pg/registry.go
	"registry_registry": "Registry %s not found",
}

// notFoundMessage — the stable hide-existence message.
//
// For a mapped object type with a concrete caller-supplied id it returns the
// Kachō contract tone "<Resource> <id> not found" — byte-identical to the owning
// service's real NotFound, so hide-existence cannot be told apart from a genuine
// miss (see hideExistenceNotFoundFormats). It never includes the subject or any
// deny reason.
//
// Fallback (unmapped object type, or a wildcard/absent scope id): the neutral,
// resource-free "not found". It deliberately does NOT echo the FGA object type:
// that token is an internal identifier that appears nowhere on the public surface
// (the resource is `Disk`, the path `/disks`, the field `diskId`), so emitting it
// both leaks the internal type dictionary and — being unlike any backend text —
// is itself the existence oracle this function exists to close.
//
// Truthfully: the fallback is NOT byte-identical to a genuine miss (the backend
// would name the resource and echo the id). Full indistinguishability holds only
// for mapped types; the fallback is the fail-closed remainder, deliberately kept
// as the least-informative option for the two cases where byte-identity is
// impossible — an object type with no owning-service implementation (nothing to
// match), and a wildcard/absent scope id (no id to echo, and "<Resource>  not
// found" would itself be a distinguishable, malformed text). The drift guard
// keeps that remainder from silently growing.
func notFoundMessage(desc permissionDeniedDescriptor) string {
	if f, ok := hideExistenceNotFoundFormats[desc.ResourceType]; ok &&
		desc.ResourceID != "" && desc.ResourceID != "*" {
		return fmt.Sprintf(f, desc.ResourceID)
	}
	return "not found"
}

// writeHTTPNotFound renders a 404 response for a hide-existence read deny. Body
// shape matches the gRPC-gateway default `{code, message}` with code 5
// (NOT_FOUND) and NO details — no deny reasons, no resource id (existence-leak
// guard).
func writeHTTPNotFound(w http.ResponseWriter, desc permissionDeniedDescriptor) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	body := map[string]any{
		"code":    5, // gRPC code NotFound
		"message": notFoundMessage(desc),
	}
	_ = json.NewEncoder(w).Encode(body)
}

// buildGRPCUnauthStatus constructs a *status.Status for missing/invalid
// credentials. Returns Unauthenticated (16) with attached ErrorInfo so the
// client can distinguish "no credentials" from "authenticated but denied".
// It must be code 16 (Unauthenticated), not 7 (PermissionDenied).
func buildGRPCUnauthStatus(desc permissionDeniedDescriptor, reasons []string) *status.Status {
	msg := "unauthenticated: credentials required"
	st := status.New(codes.Unauthenticated, msg)

	info := &errdetails.ErrorInfo{
		Reason: "AUTHN_REQUIRED",
		Domain: "kacho.cloud.iam.v1",
		Metadata: map[string]string{
			"fqn":          desc.FQN,
			"deny_reasons": strings.Join(reasons, "; "),
		},
	}

	stWithDetails, derr := st.WithDetails(info)
	if derr != nil {
		return st
	}
	return stWithDetails
}

// writeHTTPUnauth renders a 401 Unauthorized response for missing/invalid
// credentials. The JSON body uses code 16 (gRPC Unauthenticated) so that
// REST clients receive a machine-readable status consistent with the gRPC
// surface. Also sets WWW-Authenticate: Bearer per RFC 7235. Must be 401 +
// code 16, not 403 + code 7.
func writeHTTPUnauth(w http.ResponseWriter, desc permissionDeniedDescriptor, reasons []string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="kacho"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	body := map[string]any{
		"code":    16, // gRPC code Unauthenticated
		"message": "unauthenticated: credentials required",
		"details": []map[string]any{
			{
				"@type":  "type.googleapis.com/google.rpc.ErrorInfo",
				"reason": "AUTHN_REQUIRED",
				"domain": "kacho.cloud.iam.v1",
				"metadata": map[string]string{
					"fqn":          desc.FQN,
					"deny_reasons": strings.Join(reasons, "; "),
				},
			},
		},
	}
	_ = json.NewEncoder(w).Encode(body)
}

// writeHTTPDeny renders a 403 response with the same descriptor / reasons.
// `acrChallenge` — when non-empty, sets WWW-Authenticate per RFC 9470 §3.
func writeHTTPDeny(w http.ResponseWriter, desc permissionDeniedDescriptor, reasons []string, acrChallenge string) {
	if acrChallenge != "" {
		w.Header().Set("WWW-Authenticate", acrChallenge)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)

	details := make([]map[string]any, 0, len(reasons)+1)
	if len(reasons) == 0 {
		details = append(details, map[string]any{
			"@type": "type.googleapis.com/google.rpc.PreconditionFailure",
			"violations": []map[string]any{{
				"type":        "authz.no_path",
				"subject":     resourceLabel(desc),
				"description": "no authorization path to the resource",
			}},
		})
	} else {
		viol := make([]map[string]any, 0, len(reasons))
		for _, r := range reasons {
			viol = append(viol, map[string]any{
				"type":        classifyDenyReasonType(r),
				"subject":     resourceLabel(desc),
				"description": safeDenyDescription(r),
			})
		}
		details = append(details, map[string]any{
			"@type":      "type.googleapis.com/google.rpc.PreconditionFailure",
			"violations": viol,
		})
	}
	details = append(details, map[string]any{
		"@type":  "type.googleapis.com/google.rpc.ErrorInfo",
		"reason": "AUTHZ_DENIED",
		"domain": "kacho.cloud.iam.v1",
		"metadata": map[string]string{
			"subject":  desc.Subject,
			"action":   desc.Action,
			"resource": resourceLabel(desc),
			"fqn":      desc.FQN,
			// deny_reasons intentionally OMITTED — existence/authz oracle (logs only).
		},
	})

	body := map[string]any{
		"code":    7, // gRPC code PermissionDenied
		"message": "permission denied: " + desc.Action,
		"details": details,
	}
	_ = json.NewEncoder(w).Encode(body)
}

// classifyDenyReasonType inspects a deny reason string and assigns it a
// machine-readable type. The leftmost token (before ':') is the convention
// the IAM service uses ("mfa_fresh: acr=2 (need 3)" → "mfa_fresh").
func classifyDenyReasonType(reason string) string {
	if reason == "" {
		return "authz.unspecified"
	}
	idx := strings.IndexByte(reason, ':')
	if idx <= 0 {
		return "authz." + sanitizeReasonToken(reason)
	}
	return "authz." + sanitizeReasonToken(reason[:idx])
}

// sanitizeReasonToken — stable lower-case identifier (alnum + underscore).
// Anything else collapses to "unspecified".
func sanitizeReasonToken(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "unspecified"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	out = strings.Trim(out, "_")
	if out == "" {
		return "unspecified"
	}
	return out
}

// resourceLabel returns the canonical "<type>:<id>" string used as the
// violation Subject.
func resourceLabel(d permissionDeniedDescriptor) string {
	if d.ResourceType == "" {
		return d.ResourceID
	}
	if d.ResourceID == "" {
		return d.ResourceType + ":*"
	}
	return d.ResourceType + ":" + d.ResourceID
}

// shouldStepUpChallenge reports whether the deny reasons indicate the client
// can satisfy the gate by re-authenticating with a stronger ACR. Used to
// decide whether to attach WWW-Authenticate.
func shouldStepUpChallenge(reasons []string) bool {
	for _, r := range reasons {
		lr := strings.ToLower(r)
		if strings.HasPrefix(lr, "mfa_fresh") {
			return true
		}
		if strings.HasPrefix(lr, "non_expired") {
			// Token-related — refreshing the token will help; recommend
			// re-auth.
			return true
		}
	}
	return false
}

// invalidResourceIDMessage — the flat-message contract for a malformed resource
// id, matching the Kachō handler-side convention (`corevalidate.ResourceID` →
// "invalid <res> id '<X>'"). The gateway uses the neutral resource type
// "resource" because it does not know the concrete type.
func invalidResourceIDMessage(id string) string {
	return "invalid resource id '" + id + "'"
}

// scopeSourceConflictMessage — the flat-message contract for a request whose
// authorization scope cannot be read as a single value: the field is written in
// more than one place, or under more than one spelling, and the writings
// disagree. It names the field so the caller can find it, and states the rule
// rather than a winner — there is no winner, which is why the request is
// refused. Deliberately free of either value: the caller supplied both, and the
// message is also rendered to callers who supplied only one.
func scopeSourceConflictMessage(field string) string {
	return "ambiguous " + field + ": give it exactly once, " +
		"in the request body for a request that has one, otherwise in the query string"
}

// buildGRPCInvalidArgStatus constructs a *status.Status{Code: InvalidArgument}
// with the supplied flat message. No PreconditionFailure / ErrorInfo details —
// the flat message IS the contract (mirrors corevalidate.ResourceID).
func buildGRPCInvalidArgStatus(message string) *status.Status {
	return status.New(codes.InvalidArgument, message)
}

// writeHTTPInvalidArg renders a 400 response carrying the supplied flat message.
// Body shape matches the gRPC-gateway default `{code, message}` with code 3
// (INVALID_ARGUMENT).
func writeHTTPInvalidArg(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	body := map[string]any{
		"code":    3, // gRPC code InvalidArgument
		"message": message,
	}
	_ = json.NewEncoder(w).Encode(body)
}
