// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subject_extractor.go — Subject extraction from verified JWT claims.
//
// The api-gateway authz middleware needs an FGA-shaped subject id
// ("user:<usr_xxx>" / "service_account:<sva_xxx>" / "workload:<wid_xxx>")
// for every per-RPC Check. The verified token's `sub` claim is the external
// Hydra subject; the `ext_claims.kaname_*` fields (filled by the Hydra
// token_hook) carry the kacho-native principal id.
//
// Resolution priority:
//
//  0. The reserved "nobody" word (operations.AnonymousPrincipalID) on `sub` or
//     on the principal claim — resolves to NOTHING. It labels a request that
//     presented no credential, and while it resolved, every downstream gate of
//     the form "a subject came out ⇒ authenticated" read true for exactly the
//     caller those gates exist to stop.
//  1. `ext_claims.kaname_principal_type` + `ext_claims.kaname_principal_id`
//     — explicit unified shape, populated by token_hook for EVERY flow it
//     mints: interactive User, ServiceAccount client_credentials, federated
//     assertion and personal access token. A stated principal whose type is not
//     an authenticable kind (`system`, …) does not resolve, but rule 2 still
//     gets its turn — it is a positive identity assertion. Rule 3 does not:
//     see below.
//  2. `ext_claims.kaname_user_id` (User flow).
//  3. Hydra `sub` claim as the final fallback when none of the above are
//     present — yields an `external:<sub>` subject for diagnostic purposes.
//     Skipped when rule 1 stated a non-authenticable principal: this rule mints
//     a subject out of a bare `sub`, and doing so there would overrule the
//     token's own declaration of what it is.
//     Nothing downstream gates on SubjectKindExternal today, so this fallback
//     ONLY carries a stable identifier into the access log; FGA denies it by
//     construction (no such object type). Enabled by allowExternalFallback.
//
// # Здесь стояли ещё две полосы, и снятие их — не сокращение, а приведение к факту
//
// `ext_claims.kaname_sa_id` и `ext_claims.kaname_workload_id` читались как
// самостоятельные полосы резолва. Ни одно из двух имён НЕ ЧЕКАНИТСЯ: машинную
// личность — и клиентскую, и федеративную — выпуск называет единой формой
// (`kaname_principal_type=service_account` + `kaname_principal_id=<svaId>`),
// которую разбирает правило 1, и оно же выигрывает всегда. Полосы не срабатывали
// ни на одном токене, который этот продукт выпускает.
//
// У второй из них цена была выше пустой ветви: `workload` не является типом
// субъекта модели прав ВОВСЕ, поэтому `workload:<id>` отвергался бы by
// construction — ровно как диагностический `external:<sub>`. Но, в отличие от
// него, полоса не объявляла себя диагностической и стояла ВЫШЕ него: сработав,
// она молча подменила бы опознаваемый в журнале идентификатор на тот, который
// модель назвать не может.
//
// Empty / structurally-invalid claims return ok=false; the middleware then
// treats this as "no subject" (401 Unauthenticated).
package middleware

import (
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Subject prefix vocabulary. Mirrors the authorization model's subject
// types: user / service_account / workload.
const (
	subjectPrefixUser           = "user"
	subjectPrefixServiceAccount = "service_account"
	subjectPrefixWorkload       = "workload"
	// subjectPrefixExternal is a diagnostic fallback when the token has no
	// kacho-native identity in ext_claims. Not a real FGA type — Check will
	// always deny these.
	subjectPrefixExternal = "external"
)

// SubjectKind classifies a resolved subject — convenience for callers that
// want to gate on type without parsing the prefix.
type SubjectKind int

const (
	// SubjectKindUnknown — empty / unresolvable.
	SubjectKindUnknown SubjectKind = iota
	// SubjectKindUser — natural-person User (`usr_*`).
	SubjectKindUser
	// SubjectKindServiceAccount — ServiceAccount (`sva_*`).
	SubjectKindServiceAccount
	// SubjectKindWorkload — federated Workload identity.
	SubjectKindWorkload
	// SubjectKindExternal — diagnostic fallback (`external:<sub>`); fails
	// FGA Check by construction.
	SubjectKindExternal
)

// ResolvedSubject — the output of SubjectExtractor.Extract.
type ResolvedSubject struct {
	// FGA — FGA-shaped subject string ("user:usr_abc"). Empty when ok=false.
	FGA string
	// Kind — classification of the subject.
	Kind SubjectKind
	// ID — bare id (no prefix).
	ID string
	// Source — which ext_claims field (or `sub`) the value came from.
	// Used in audit logs to diagnose unexpected fallbacks.
	Source string
}

// String makes ResolvedSubject stringer-friendly for log fields.
func (r ResolvedSubject) String() string {
	if r.FGA == "" {
		return "<unknown>"
	}
	return r.FGA
}

// SubjectExtractor — stateless extractor; constructed once and shared.
type SubjectExtractor struct {
	// allowExternalFallback controls whether a raw `sub`-only token resolves
	// to `external:<sub>` (true; production-strict gates this further at the
	// middleware layer) or to ok=false (false; pre-emptively reject).
	allowExternalFallback bool
}

// NewSubjectExtractor constructs an extractor. allowExternalFallback=true
// matches production-non-strict behaviour where unknown tokens get a
// `external:<sub>` shape that always FGA-denies — useful so the access log
// records a stable identifier for forensics.
func NewSubjectExtractor(allowExternalFallback bool) *SubjectExtractor {
	return &SubjectExtractor{allowExternalFallback: allowExternalFallback}
}

// Extract reads kaname_principal_* / kaname_user_id from the verified token's
// ext_claims and returns the most-specific resolution. Returns ok=false when
// nothing matches and allowExternalFallback=false.
func (e *SubjectExtractor) Extract(t *VerifiedToken) (ResolvedSubject, bool) {
	if t == nil {
		return ResolvedSubject{}, false
	}
	ext := t.ExtClaims

	// 0. The reserved "nobody" word is never a subject — on `sub` or on the
	// principal claim. A request with no credential is labelled with it, and
	// while it resolves, every "did a subject come out?" gate reads true for an
	// unauthenticated caller.
	if isAnonymousSubjectID(t.Subject) {
		return ResolvedSubject{}, false
	}

	// statedNonTenant records that rule 1 found an explicit principal whose type
	// is not an authenticable kind (`system`, …). Rule 2 may still resolve — it
	// is a POSITIVE identity assertion and outranks a malformed rule-1 shape.
	// Rule 3 may not: it invents `external:<sub>` out of a bare `sub`, which
	// would overrule the token's own declaration of not being a tenant.
	statedNonTenant := false

	// 1. Unified ext_claims.kaname_principal_*.
	if ext != nil {
		pType, _ := ext["kaname_principal_type"].(string)
		pID, _ := ext["kaname_principal_id"].(string)
		pType = strings.TrimSpace(pType)
		pID = strings.TrimSpace(pID)
		if isAnonymousSubjectID(pID) {
			return ResolvedSubject{}, false
		}
		if pType != "" && pID != "" {
			kind, prefix := classifyPrincipal(pType)
			if kind != SubjectKindUnknown {
				return ResolvedSubject{
					FGA:    prefix + ":" + pID,
					Kind:   kind,
					ID:     pID,
					Source: "ext_claims.kaname_principal_*",
				}, true
			}
			statedNonTenant = true
		}
	}

	// 2. ext_claims.kaname_user_id.
	if ext != nil {
		if uid, ok := ext["kaname_user_id"].(string); ok && uid != "" {
			return ResolvedSubject{
				FGA:    subjectPrefixUser + ":" + uid,
				Kind:   SubjectKindUser,
				ID:     uid,
				Source: "ext_claims.kaname_user_id",
			}, true
		}
	}

	// 3. Hydra `sub` fallback — diagnostic only, and never over a token that
	// already declared itself a non-tenant principal (see statedNonTenant).
	if e.allowExternalFallback && !statedNonTenant && t.Subject != "" {
		return ResolvedSubject{
			FGA:    subjectPrefixExternal + ":" + t.Subject,
			Kind:   SubjectKindExternal,
			ID:     t.Subject,
			Source: "jwt.sub",
		}, true
	}

	return ResolvedSubject{}, false
}

// isAnonymousSubjectID reports whether an id is the reserved "nobody" word the
// edge stamps on a credential-less request. It is checked on both the `sub` and
// the principal claim because either one reaching a resolution rule is enough to
// turn "unknown who" into a subject — and a subject is what every
// authentication gate downstream looks for.
func isAnonymousSubjectID(id string) bool {
	return strings.TrimSpace(id) == operations.AnonymousPrincipalID
}

// classifyPrincipal maps the kacho-native principal-type string to a Kind
// and the matching FGA prefix. Unknown types return SubjectKindUnknown +
// empty string — callers fall through to the next resolution rule.
func classifyPrincipal(t string) (SubjectKind, string) {
	switch strings.ToLower(t) {
	case "user", "usr":
		return SubjectKindUser, subjectPrefixUser
	case "service_account", "service-account", "serviceaccount", "sva":
		return SubjectKindServiceAccount, subjectPrefixServiceAccount
	case "workload", "wid":
		return SubjectKindWorkload, subjectPrefixWorkload
	default:
		return SubjectKindUnknown, ""
	}
}
