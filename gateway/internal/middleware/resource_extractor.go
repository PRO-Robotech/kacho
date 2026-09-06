// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// resource_extractor.go — Resolve the scope/resource id of an in-flight
// request using catalog metadata (`scope_extractor.from_request_field`) +
// proto reflection.
//
// The middleware drives FGA `Check(subject, action, resource_type:resource_id,
// context)`. `resource_type` comes from the catalog directly; `resource_id`
// must be plucked out of the request payload.
//
// Strategies, in order:
//
//  1. **proto reflection** — when the request is a proto.Message we can walk
//     the message looking up the named field. This is the canonical path for
//     gRPC server-interceptors where the request is already typed.
//
//  2. **HTTP path/query/body parsing** — when called from the HTTP path (REST
//     gateway), the request body has not yet been decoded into a typed proto
//     message, so the scope is read out of the raw request. The source is
//     decided by the ROUTE CONTRACT, not by convenience: it is whichever source
//     grpc-gateway will itself bind the field from when it builds the handler's
//     message (see httpScopeField). The subject of the authorization check is
//     therefore the subject of the action.
//
//  3. **wildcard fallback** — for List/Search RPCs (no specific resource_id)
//     OR when extraction fails, we return the FGA wildcard "*". The FGA
//     Authorization Model contains a "viewer" relation on the `cluster` /
//     `organization` root that resolves wildcards correctly.
//
// Special-case keys recognised in `scope_extractor.from_request_field`:
//
//	"*"       — always wildcard (declared in catalog for catch-all RPCs).
//	"subject" — the subject id is its own scope (AuthorizeService.Check).
//	"resource"— a `ResourceRef{type,id}` message-field — extracts the id.
package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// ResourceID — the scope identifier the middleware passes to FGA. Wildcard
// ("*") is permitted and indicates a List/Search-style RPC.
type ResourceID string

// IsWildcard reports whether the id is the FGA wildcard.
func (r ResourceID) IsWildcard() bool { return r == "*" }

// String renders the id as a plain string (no prefix).
func (r ResourceID) String() string { return string(r) }

// ResourceExtractor — stateless lookup driven by CatalogEntry metadata.
type ResourceExtractor struct {
	// httpFallbackPaths maps FQN → a URL-template (grpc-gateway style
	// `/iam/v1/projects/{project_id}`) for the HTTP fallback. Optional; an
	// empty map disables HTTP-path extraction.
	httpFallbackPaths map[string]string
}

// NewResourceExtractor constructs an extractor. The optional fallbackPaths
// map enables HTTP-path-style resource extraction.
func NewResourceExtractor(httpFallbackPaths map[string]string) *ResourceExtractor {
	cp := make(map[string]string, len(httpFallbackPaths))
	for k, v := range httpFallbackPaths {
		cp[k] = v
	}
	return &ResourceExtractor{httpFallbackPaths: cp}
}

// ExtractFromProto applies the catalog `from_request_field` directive to a
// typed proto request, returning the resolved ResourceID + ok.
//
// Returns ok=true even for wildcard results — the boolean signals "no error",
// not "specific id". Callers distinguish with ResourceID.IsWildcard.
func (e *ResourceExtractor) ExtractFromProto(req any, entry CatalogEntry) (ResourceID, bool) {
	field := strings.TrimSpace(entry.ScopeExtractor.FromRequestField)
	if field == "" || field == "*" {
		return ResourceID("*"), true
	}
	if req == nil {
		return ResourceID("*"), true
	}
	msg, ok := protoMessageFromAny(req)
	if !ok {
		// Non-proto request. On the production authz path the intercepted gRPC
		// request is always a proto.Message (authz.decisionRequest.ProtoReq),
		// so this is unreachable in prod; fail closed to the wildcard scope.
		return ResourceID("*"), true
	}
	return extractByProtoReflect(msg, field), true
}

// ScopeConflict reports a request whose authorization scope cannot be read as a
// single value: it is written in more than one place, or under more than one
// spelling of one field, and the writings disagree. The caller MUST refuse such
// a request instead of picking a winner — whichever it picked, the subject of
// the check and the subject of the action could be different objects.
//
// It is also raised when the scope appears ONLY where the handler will not read
// it: that too is a request that says one thing to the edge and another to the
// handler, and the caller learns which place to put it instead of receiving an
// unexplained refusal for a scope it thought it had named.
type ScopeConflict struct {
	// Field — the catalog scope field, snake_case as declared in the catalog
	// (`project_id`). Named in the refusal so the caller can fix the request.
	Field string
}

// ExtractFromHTTP extracts the resource id from an HTTP request when the proto
// body hasn't been decoded yet.
//
//  1. Recognised path template (e.g. `/iam/v1/projects/{project_id}` →
//     parse {project_id}). The template registry is supplied via
//     `httpFallbackPaths` keyed by FQN. Path placeholders bind first and cannot
//     be overridden — in grpc-gateway or here.
//  2. Otherwise the single source the route contract binds the field from
//     (httpScopeField): the request body for a body-bearing method, the query
//     string for one without a body.
//  3. Wildcard when the request names no scope at all.
//
// A non-nil *ScopeConflict means the request's scope cannot be read as a single
// value (see ScopeConflict); the returned id is the best handler-visible reading,
// but the caller must refuse rather than proceed on it.
func (e *ResourceExtractor) ExtractFromHTTP(r *http.Request, fqn string, entry CatalogEntry) (ResourceID, *ScopeConflict) {
	if r == nil {
		return ResourceID("*"), nil
	}
	field := strings.TrimSpace(entry.ScopeExtractor.FromRequestField)
	if field == "" || field == "*" {
		return ResourceID("*"), nil
	}
	// 1. Template-aware extraction. We accept simple `{name}` placeholders.
	if tmpl, ok := e.httpFallbackPaths[fqn]; ok && tmpl != "" {
		if id, ok := extractByPathTemplate(r.URL.Path, tmpl, field); ok && id != "" {
			return ResourceID(id), nil
		}
	}
	id, conflict := httpScopeField(r, field)
	if conflict {
		return wildcardIfEmpty(id), &ScopeConflict{Field: field}
	}
	return wildcardIfEmpty(id), nil
}

// httpScopeField reads one catalog-named scope field out of a raw HTTP request
// using the SAME source grpc-gateway will bind it from when it decodes the
// request into the handler's message. That equality is the whole point: the edge
// must authorize the object the handler is about to act on, not a different one
// the caller nominated on the side.
//
// The binding rule, which mirrors `google.api.http`:
//
//   - A method that carries a body binds the WHOLE body (`body: "*"` — the form
//     every Kachō route with a body uses). For that binding grpc-gateway does not
//     read query parameters AT ALL, so a query parameter names nothing the
//     handler will see. The body is the source.
//   - A bodyless method binds its non-path fields from the query string. The
//     query is the source — this is how `List` narrows by parent
//     (`?projectId=…`), and it must keep working.
//
// Which methods fall on which side is decided by methodCarriesBody.
//
// # What conflict=true means
//
// The scope field can be written in more than one place, and the two sides do not
// resolve the duplicates the same way, so "read it the way the handler does" is
// not enough on its own — the handler's own answer has to be a single one. Both
// duplications are therefore reported for refusal rather than resolved here:
//
//   - TWO SPELLINGS OF ONE FIELD. The catalog names the snake_case proto field
//     (`account_id`); REST carries camelCase (`accountId`); both resolve to the
//     same field. grpc-gateway walks the query as a Go MAP, so with both present
//     the surviving value is decided by map iteration order and differs between
//     identical requests. There is no "the handler's value" to agree with.
//   - THE SOURCE THE HANDLER WILL NOT READ naming the field differently. That
//     combination has no innocent reading — the ignored source can only mislead.
//
// Equal values are never a conflict: a caller that harmlessly echoes the scope is
// not contradicting itself, and the handler has nothing else to pick.
func httpScopeField(r *http.Request, field string) (value string, conflict bool) {
	query, queryAmbiguous := httpQueryField(r, field)
	if !methodCarriesBody(r.Method) {
		return query, queryAmbiguous
	}
	// From here the body is the source the handler is built from.
	body, bodyAmbiguous := jsonBodyField(r, field)
	if bodyAmbiguous {
		return body, true
	}
	if query != "" && query != body {
		return body, true
	}
	return body, false
}

// httpQueryField reads a field from the query string under both its snake_case
// (catalog) and camelCase (REST) spellings. ambiguous=true when both spellings
// are present and disagree — see httpScopeField for why that cannot be resolved
// by preferring one.
//
// A single key repeated (`?a=1&a=2`) needs no handling here: grpc-gateway rejects
// more than one value for a singular field outright, so the handler never acts on
// such a request.
func httpQueryField(r *http.Request, field string) (value string, ambiguous bool) {
	q := r.URL.Query()
	return pickOneSpelling(q.Get(field), q.Get(snakeToCamel(field)))
}

// pickOneSpelling collapses the two spellings of one field into a single value,
// reporting ambiguous=true when both are given and differ.
func pickOneSpelling(snake, camel string) (value string, ambiguous bool) {
	switch {
	case snake != "" && camel != "" && snake != camel:
		return snake, true
	case snake != "":
		return snake, false
	default:
		return camel, false
	}
}

// methodCarriesBody reports whether an HTTP method binds its request message
// from a body. It enumerates the BODYLESS methods and treats everything else as
// body-bearing, which is the fail-safe direction: mistaking a body-bearing
// method for a bodyless one would hand the scope back to the query string on a
// route whose handler reads the body, which is the whole class this guards
// against. Mistaking the other way only costs an unresolved scope, i.e. a denial.
//
// The method is upper-cased first because RestRouter.Resolve upper-cases before
// picking the route — and therefore the catalog row. Both halves of one decision
// must normalise identically, or a request could be routed as a create while
// being scoped as a read.
func methodCarriesBody(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

// wildcardIfEmpty renders "no scope named" as the FGA wildcard, which
// AuthorizeService.Check refuses as an unscoped resource.
func wildcardIfEmpty(id string) ResourceID {
	if id == "" {
		return ResourceID("*")
	}
	return ResourceID(id)
}

// definitionTierField is the redesign-2026 F4 scope-anchor message field
// (`DefinitionTier{tier_type, tier_id}`) carried by CreateRoleRequest. When
// present + resolvable it is the CANONICAL authz scope, superseding the legacy
// account_id/project_id catalog extraction (proto precedence: "when
// definition_tier is set it takes precedence").
const definitionTierField = "definition_tier"

// definitionTierScopedFQNs — the CLOSED set of RPCs whose request message
// declares the `definition_tier` anchor and may therefore have their authz scope
// resolved from it. Today that is exactly RoleService/Create (the only request
// message in proto/ carrying the field).
//
// The binding is a SECURITY invariant, not a micro-optimisation. The anchor is
// read from the RAW request payload — on the HTTP path straight out of the JSON
// body, BEFORE grpc-gateway decodes it and silently drops unknown keys. Without
// an FQN binding a caller could smuggle
// `{"definitionTier":{"tierType":"iam.project","tierId":"<a project I own>"}}`
// into the body of ANY JSON-bodied RPC and re-point the FGA Check at a scope of
// their own choosing while the backend still acted on the real (victim) scope —
// a BOLA/privilege-escalation primitive (security.md §object-scoped authz).
// Scope resolution must never depend on unauthenticated client input that the
// handler itself ignores.
//
// A new RPC gains the anchor by adding `definition_tier` to its request message
// AND its FQN here (both, deliberately — the code-driven allow-list keeps the
// generated permission-catalog byte-identical).
var definitionTierScopedFQNs = map[string]struct{}{
	"kaname.cloud.iam.v1.RoleService/Create": {},
}

// definitionTierAnchored reports whether fqn is allowed to resolve its authz
// scope from the `definition_tier` anchor.
func definitionTierAnchored(fqn string) bool {
	_, ok := definitionTierScopedFQNs[strings.TrimPrefix(strings.TrimSpace(fqn), "/")]
	return ok
}

// definitionTierObjectType maps a dotted definition-tier type to its FGA object
// type: iam.account→account, iam.project→project. iam.cluster (system roles are
// seeded, never API-created) and any unknown type → ok=false, so the caller keeps
// the legacy scope and the iam handler surfaces the canonical INVALID_ARGUMENT
// ("Illegal argument definitionTier"). Mirrors domain.CustomDefinitionTierToScope
// (the gateway cannot import the iam domain; these are stable wire values).
func definitionTierObjectType(tierType string) (string, bool) {
	switch tierType {
	case "iam.account":
		return "account", true
	case "iam.project":
		return "project", true
	default:
		return "", false
	}
}

// ResolveDefinitionTierScope inspects a typed proto request for the redesign-2026
// F4 `definition_tier` anchor and, when present + resolvable, returns the FGA
// (objectType, id) the authz scope MUST use — SUPERSEDING the legacy
// account_id/project_id catalog extraction (proto precedence semantics). ok=false
// when fqn does not declare the anchor (definitionTierScopedFQNs), the message has
// no definition_tier field, it is empty, or its tier_type is unresolvable
// (iam.cluster / unknown) — the caller then keeps the catalog's static scope
// extraction (legacy account_id/project_id). Code-driven (like the nested
// ResourceRef `.id` handling) so the byte-identical permission-catalog is
// untouched.
//
// fqn is REQUIRED and gates the whole resolution: the anchor may only re-scope the
// RPCs that declare it (see definitionTierScopedFQNs).
func (e *ResourceExtractor) ResolveDefinitionTierScope(fqn string, req any) (objectType, id string, ok bool) {
	if !definitionTierAnchored(fqn) {
		return "", "", false
	}
	msg, okp := protoMessageFromAny(req)
	if !okp {
		return "", "", false
	}
	fd := msg.Descriptor().Fields().ByName(definitionTierField)
	if fd == nil || fd.Kind() != protoreflect.MessageKind {
		return "", "", false
	}
	sub := msg.Get(fd).Message()
	if sub == nil || !sub.IsValid() {
		return "", "", false
	}
	tierType := subMessageString(sub, "tier_type")
	tierID := subMessageString(sub, "tier_id")
	if tierType == "" || tierID == "" {
		return "", "", false
	}
	ot, mapped := definitionTierObjectType(tierType)
	if !mapped {
		return "", "", false
	}
	return ot, tierID, true
}

// ResolveDefinitionTierScopeHTTP is the REST-path analogue of
// ResolveDefinitionTierScope: it reads the `definitionTier{tierType,tierId}`
// object out of the JSON request body (camelCase, grpc-gateway spelling) and
// restores the body for the downstream handler. Same precedence + resolvability
// contract; ok=false → caller keeps the legacy static extraction.
//
// The fqn gate is load-bearing HERE above all: this is the arm that reads
// UNAUTHENTICATED client input (the raw body) — an unbound resolution would let
// any caller re-scope any JSON-bodied RPC (see definitionTierScopedFQNs).
func (e *ResourceExtractor) ResolveDefinitionTierScopeHTTP(fqn string, r *http.Request) (objectType, id string, ok bool) {
	if !definitionTierAnchored(fqn) {
		return "", "", false
	}
	if r == nil {
		return "", "", false
	}
	tierType, tierID := extractDefinitionTierFromJSONBody(r)
	if tierType == "" || tierID == "" {
		return "", "", false
	}
	ot, mapped := definitionTierObjectType(tierType)
	if !mapped {
		return "", "", false
	}
	return ot, tierID, true
}

// legacyProjectScopeField is the LEGACY project-scope alternative
// (`CreateRoleRequest.project_id`) that the proto keeps accepted for back-compat
// when `definition_tier` is omitted: a custom Role is account- XOR
// project-scoped, and the catalog `scope_extractor` can name only ONE field
// (`account_id`). Without this arm a legacy project-scoped Create extracted to
// the `account:*` wildcard, which AuthorizeService.Check rejects as "no path:
// unscoped resource" → 403 at the edge, before the backend that honours the
// promise ever saw the request.
const legacyProjectScopeField = "project_id"

// legacyProjectScopedFQNs — the CLOSED set of RPCs whose request message carries
// the legacy account_id/project_id scope pair and may therefore have their authz
// scope resolved from `project_id`. Today that is exactly RoleService/Create.
//
// The binding carries the same SECURITY weight as definitionTierScopedFQNs: on
// the REST arm `project_id` is read straight out of the raw JSON body — before
// grpc-gateway decodes it and silently drops unknown keys — i.e. from
// unauthenticated client input. Unbound, a caller could smuggle
// `{"accountId":"acc_victim","projectId":"<a project I own>"}` into any
// JSON-bodied RPC and re-point the FGA Check at a scope of their own choosing
// while the backend still acted on the real (victim) scope — a
// BOLA/privilege-escalation primitive (security.md §object-scoped authz).
//
// A new RPC joins the set by carrying the pair in its request message AND being
// listed here (both, deliberately — the code-driven allow-list keeps the
// generated permission-catalog byte-identical).
var legacyProjectScopedFQNs = map[string]struct{}{
	"kaname.cloud.iam.v1.RoleService/Create": {},
}

// legacyProjectAnchored reports whether fqn is allowed to resolve its authz
// scope from the legacy `project_id` field.
func legacyProjectAnchored(fqn string) bool {
	_, ok := legacyProjectScopedFQNs[strings.TrimPrefix(strings.TrimSpace(fqn), "/")]
	return ok
}

// ResolveLegacyProjectScope reads the legacy `project_id` scope alternative off a
// typed proto request. It returns the PROJECT id the authz scope must use;
// ok=false when fqn does not declare the pair, the message has no `project_id`
// field, or it is empty.
//
// The XOR side of the precedence (`definition_tier` > `account_id` >
// `project_id`) is enforced by the CALLER, which only consults this resolver
// once the canonical anchor and the catalog's `account_id` have both come up
// empty — so a present account_id is never displaced by a project the caller
// happens to own. The resulting scope is the SAME anchor the backend acts on
// (role/create.go XOR switch + DB CHECK roles_definition_tier_xor), which is what
// makes it anti-BOLA-correct rather than merely permissive.
func (e *ResourceExtractor) ResolveLegacyProjectScope(fqn string, req any) (id string, ok bool) {
	if !legacyProjectAnchored(fqn) {
		return "", false
	}
	msg, okp := protoMessageFromAny(req)
	if !okp {
		return "", false
	}
	fd := msg.Descriptor().Fields().ByName(legacyProjectScopeField)
	if fd == nil || fd.Kind() != protoreflect.StringKind {
		return "", false
	}
	v := msg.Get(fd).String()
	if v == "" {
		return "", false
	}
	return v, true
}

// ResolveLegacyProjectScopeHTTP is the REST-path analogue of
// ResolveLegacyProjectScope: it reads `projectId` (both spellings) out of the
// JSON request body and restores the body for the downstream handler. Same
// contract; the fqn gate is load-bearing here above all — this is the arm that
// reads unauthenticated client input (see legacyProjectScopedFQNs).
func (e *ResourceExtractor) ResolveLegacyProjectScopeHTTP(fqn string, r *http.Request) (id string, ok bool) {
	if !legacyProjectAnchored(fqn) {
		return "", false
	}
	if r == nil {
		return "", false
	}
	v := extractFromJSONBody(r, legacyProjectScopeField)
	if v == "" {
		return "", false
	}
	return v, true
}

// subMessageString reads a named scalar string field off a sub-message, "" when
// absent/empty.
func subMessageString(sub protoreflect.Message, name string) string {
	fd := sub.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil || fd.Kind() != protoreflect.StringKind {
		return ""
	}
	return sub.Get(fd).String()
}

// extractDefinitionTierFromJSONBody reads `definitionTier.{tierType,tierId}` from
// the JSON body and restores the body. Returns ("","") when absent / unparseable.
// Both snake_case and camelCase spellings are accepted for the object key and its
// members (the middleware runs before grpc-gateway decodes the camelCase body).
func extractDefinitionTierFromJSONBody(r *http.Request) (tierType, tierID string) {
	doc, ok := inspectJSONBody(r)
	if !ok {
		return "", ""
	}
	var raw json.RawMessage
	for _, key := range []string{"definition_tier", "definitionTier"} {
		if v, present := doc[key]; present {
			raw = v
			break
		}
	}
	if len(raw) == 0 {
		return "", ""
	}
	var tier map[string]json.RawMessage
	if json.Unmarshal(raw, &tier) != nil {
		return "", ""
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, present := tier[k]; present {
				var s string
				if json.Unmarshal(v, &s) == nil && s != "" {
					return s
				}
			}
		}
		return ""
	}
	return pick("tier_type", "tierType"), pick("tier_id", "tierId")
}

// ScopeTypeFromProto reads the named top-level string field off a typed proto
// request and returns its raw value, or "" when the field is absent/empty.
//
// Unlike ExtractFromProto it does NOT wildcard-default: an empty result means
// "no dynamic object type present, use the catalog's static object_type". Used
// for scope-polymorphic RPCs whose FGA object type is carried by a request
// field (catalog `object_type_from_request_field`).
func (e *ResourceExtractor) ScopeTypeFromProto(req any, field string) string {
	field = strings.TrimSpace(field)
	if field == "" || req == nil {
		return ""
	}
	msg, ok := protoMessageFromAny(req)
	if !ok {
		// Non-proto request — unreachable on the production authz path (see
		// ExtractFromProto). No dynamic object type available.
		return ""
	}
	id := extractByProtoReflect(msg, field)
	if id.IsWildcard() {
		return ""
	}
	return id.String()
}

// ScopeTypeFromHTTP reads the named field from an HTTP request and returns its
// raw value, or "" when absent. Like ScopeTypeFromProto it does NOT
// wildcard-default.
//
// It reads from the SAME source as ExtractFromHTTP (httpScopeField), so the two
// halves of one scope — the object type and the object id — can never be taken
// from different places, and reports the same contradiction the same way.
func (e *ResourceExtractor) ScopeTypeFromHTTP(r *http.Request, field string) (string, *ScopeConflict) {
	field = strings.TrimSpace(field)
	if r == nil || field == "" {
		return "", nil
	}
	v, conflict := httpScopeField(r, field)
	if conflict {
		return v, &ScopeConflict{Field: field}
	}
	return v, nil
}

// extractFromJSONBody reads a top-level string field out of the request's JSON
// body. Returns "" when the body is absent / unparseable / the field is missing.
//
// It is the value-only form of jsonBodyField, for the callers whose field cannot
// be written twice in one body (the `definition_tier` anchor and the legacy
// `project_id` alternative both name exactly one spelling in the proto).
func extractFromJSONBody(r *http.Request, snakeField string) string {
	v, _ := jsonBodyField(r, snakeField)
	return v
}

// jsonBodyField reads a top-level string field out of the request's JSON body
// under both its snake_case and camelCase spellings, and restores the body for
// downstream consumers. ambiguous=true when both spellings are present and
// disagree (see httpScopeField).
//
// The media type is NOT consulted. The REST mux registers its JSON marshaler
// under runtime.MIMEWildcard, so the handler decodes the body as JSON whatever
// the request labelled it; gating on `Content-Type` here would let a caller pick
// a media type that hides the scope from the check while leaving it visible to
// the handler. A body that is not JSON simply fails to parse, which costs one
// discarded parse and yields no scope.
func jsonBodyField(r *http.Request, snakeField string) (value string, ambiguous bool) {
	doc, ok := inspectJSONBody(r)
	if !ok {
		return "", false
	}
	pick := func(key string) string {
		raw, present := doc[key]
		if !present {
			return ""
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return ""
	}
	return pickOneSpelling(pick(snakeField), pick(snakeToCamel(snakeField)))
}

// inspectJSONBody decodes the request body's top-level JSON object for
// inspection and leaves the request readable by the downstream handler.
//
// The media type is NOT consulted. The REST mux registers its JSON marshaler
// under runtime.MIMEWildcard, so the handler decodes the body as JSON whatever
// the request labelled it; gating on `Content-Type` here would let a caller pick
// a media type that hides a scope from the check while leaving it visible to the
// handler. A body that is not JSON simply fails to parse and yields nothing.
//
// Only a bounded prefix is buffered — control-plane bodies are tiny and an
// oversized one must not be held whole. The remainder is NOT consumed: the
// prefix is spliced back ahead of the untouched reader, so the handler receives
// the request byte-for-byte. A body longer than the cap is cut mid-document,
// so it does not parse and yields nothing — fail closed at the edge, intact
// downstream. (Buffering the prefix and dropping the rest, as this once did,
// silently corrupts every request over the cap.)
func inspectJSONBody(r *http.Request) (map[string]json.RawMessage, bool) {
	if r.Body == nil || r.ContentLength == 0 {
		return nil, false
	}
	head, err := io.ReadAll(io.LimitReader(r.Body, bodyInspectCap))
	rest := r.Body
	r.Body = bodyReadCloser{Reader: io.MultiReader(bytes.NewReader(head), rest), Closer: rest}
	if err != nil || len(head) == 0 {
		return nil, false
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(head, &doc) != nil {
		return nil, false
	}
	return doc, true
}

// bodyInspectCap — сколько байт тела буферизуется для извлечения области.
//
// Вынесен в область пакета не ради стиля: он связан с потолком тела запроса
// (EdgeMaxRequestBodyBytes). Тело, которое край принимает, обязано целиком
// поддаваться осмотру — иначе законный запрос в пределах потолка резался бы
// посередине документа, область не извлекалась бы и он получал бы отказ по
// правам. Соотношение закреплено пробой предпосылки.
const bodyInspectCap = 1 << 20

// bodyReadCloser pairs a spliced reader with the original body's Closer, so
// restoring an inspected body does not drop the request's cleanup.
type bodyReadCloser struct {
	io.Reader
	io.Closer
}

// snakeToCamel converts `account_id` -> `accountId` (REST JSON spelling).
func snakeToCamel(s string) string {
	var b strings.Builder
	up := false
	for _, r := range s {
		if r == '_' {
			up = true
			continue
		}
		if up && r >= 'a' && r <= 'z' {
			b.WriteRune(r - 'a' + 'A')
			up = false
			continue
		}
		up = false
		b.WriteRune(r)
	}
	return b.String()
}

// protoMessageFromAny tries to assert the supplied interface as a
// proto.Message. We use a tight `ProtoReflect()` interface check (every
// generated proto v1.27+ message implements it) so the resource extractor
// stays decoupled from the proto package import graph.
func protoMessageFromAny(req any) (protoreflect.Message, bool) {
	if pm, ok := req.(interface{ ProtoReflect() protoreflect.Message }); ok {
		return pm.ProtoReflect(), true
	}
	return nil, false
}

// extractByProtoReflect walks the message fields looking up the named field.
// Supports nested ResourceRef (`from_request_field:"resource"`
// where `resource` is a ResourceRef message → read `.id`).
//
// A DOTTED name ("address.project_id") names the scope inside an embedded body
// message and is walked segment by segment. It exists for requests that wrap a
// whole creation body instead of restating its fields, where the scope of the
// call is a field of the wrapped message. Without it such a call cannot state its
// scope in the catalog at all, and the check its owning service performs would
// have no counterpart in the artefact operators read. An unresolvable segment
// yields the wildcard, exactly as an unknown flat field does.
func extractByProtoReflect(msg protoreflect.Message, field string) ResourceID {
	if base, rest, ok := strings.Cut(field, "."); ok {
		fd := msg.Descriptor().Fields().ByName(protoreflect.Name(base))
		if fd == nil || fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return ResourceID("*")
		}
		sub := msg.Get(fd).Message()
		if sub == nil || !sub.IsValid() {
			return ResourceID("*")
		}
		return extractByProtoReflect(sub, rest)
	}
	// Find the field descriptor matching the supplied name. We try direct
	// match first; common field aliases (snake_case vs camelCase) are
	// covered by protobuf's standard JSON marshalling so we don't need
	// special handling beyond ByName.
	fields := msg.Descriptor().Fields()
	fd := fields.ByName(protoreflect.Name(field))
	if fd == nil {
		// Some catalog entries use snake_case `resource_id` while the proto
		// might declare it as oneof/camelCase. Try a few normalised lookups.
		for _, alt := range []string{toSnakeCase(field), strings.ToLower(field)} {
			if alt == field {
				continue
			}
			fd = fields.ByName(protoreflect.Name(alt))
			if fd != nil {
				break
			}
		}
	}
	if fd == nil {
		return ResourceID("*")
	}
	val := msg.Get(fd)
	// Embedded ResourceRef → read `.id`.
	if fd.Kind() == protoreflect.MessageKind {
		sub := val.Message()
		if sub == nil || !sub.IsValid() {
			return ResourceID("*")
		}
		// Try .id (the canonical AuthorizeService ResourceRef shape) and
		// fall back to the first scalar string field.
		if idFD := sub.Descriptor().Fields().ByName("id"); idFD != nil {
			if s := sub.Get(idFD).String(); s != "" {
				return ResourceID(s)
			}
		}
		// Walk sub-fields for the first non-empty string.
		var found string
		sub.Range(func(_ protoreflect.FieldDescriptor, fv protoreflect.Value) bool {
			if s := fv.String(); s != "" {
				found = s
				return false
			}
			return true
		})
		if found != "" {
			return ResourceID(found)
		}
		return ResourceID("*")
	}
	// Scalar string / bytes — directly return.
	switch fd.Kind() {
	case protoreflect.StringKind, protoreflect.BytesKind:
		s := val.String()
		if s == "" {
			return ResourceID("*")
		}
		return ResourceID(s)
	}
	return ResourceID("*")
}

// extractByPathTemplate parses a grpc-gateway-style path template
// (`/iam/v1/projects/{project_id}`) against the incoming URL path and
// returns the value of the placeholder matching the catalog's
// `from_request_field` (`project_id` in this example). Empty string when
// the path does not match the template or the placeholder is missing.
//
// Handles grpc-gateway suffix-action segments where a `:verb` is glued to
// the final segment: template `{subnet_id}:add-cidr-blocks` against request
// segment `xyz:add-cidr-blocks` extracts `xyz` (the verb part is matched
// as a literal). Without this, suffix-action RPCs would fall through to
// wildcard `*` and surface as `no path: unscoped resource` 403 from the
// authz check.
func extractByPathTemplate(reqPath, template, field string) (string, bool) {
	// Tokenize both — slashed segments, very small templates only (no
	// wildcards / collections). This mirrors how grpc-gateway parses
	// `google.api.http` simple path bindings.
	tparts := strings.Split(strings.Trim(template, "/"), "/")
	pparts := strings.Split(strings.Trim(reqPath, "/"), "/")
	if len(tparts) != len(pparts) {
		return "", false
	}
	var found string
	for i, t := range tparts {
		p := pparts[i]

		// Suffix-action handling: split off optional `:verb` from BOTH
		// template and request, then compare the verbs as literals and
		// fall through to placeholder matching on the leading half.
		tBase, tVerb := splitVerbSuffix(t)
		pBase, pVerb := splitVerbSuffix(p)
		if tVerb != pVerb {
			return "", false
		}

		if isPlaceholder(tBase) {
			name := tBase[1 : len(tBase)-1]
			if name == field {
				found = pBase
			}
		} else if tBase != pBase {
			return "", false
		}
	}
	return found, found != ""
}

// splitVerbSuffix peels off an optional grpc-gateway `:verb` action from a
// path segment. `"x:add"` → ("x","add"); `"x"` → ("x",""). Only the final
// colon is treated as a separator (defensive against colon-in-id ids,
// which Kachō does not currently produce but cheap to guard against).
func splitVerbSuffix(seg string) (base, verb string) {
	if idx := strings.LastIndexByte(seg, ':'); idx > 0 {
		return seg[:idx], seg[idx+1:]
	}
	return seg, ""
}

// toSnakeCase — minimal CamelCase → snake_case for field-name normalisation.
func toSnakeCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
