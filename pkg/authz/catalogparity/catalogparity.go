// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package catalogparity compares a service's in-process authz.RPCMap against the
// generated permission catalog — the artefact the api-gateway enforces.
//
// # Why this exists
//
// Two independent authorization decisions are taken on every public RPC: the
// api-gateway checks the catalog entry (relation + scope object type), and the
// owning service re-checks its own PermissionMap on the same call. The catalog is
// the AUTHORITY (it is generated from the proto annotations); the in-service map
// MUST mirror it on both axes — required relation AND scope object type.
//
// A silent divergence is not cosmetic. If the service asks for a WEAKER relation
// than the catalog, the service-side check stops being a defence-in-depth layer
// and becomes a hole the moment anything reaches the service without passing the
// gateway (internal listener, port-forward, a future direct peer edge). If the
// service asks for a STRONGER relation, principals the catalog admits get a 403
// from the backend — the catalog says yes, the service says no. Either way the
// deployed behaviour is no longer described by the catalog, which is the document
// operators read.
//
// The scope axis matters just as much: the same relation checked against the
// PROJECT rather than against the OBJECT is a different question. Anchoring on the
// project where the catalog anchors on the object turns a per-object check into a
// project-wide one (the anti-BOLA guarantee is lost); anchoring on the object
// where the catalog anchors on the project asks about an object the caller never
// named.
//
// # How the scope object type is recovered
//
// authz.RPCEntry stores the scope as a closure (ObjectExtractor), not as data, so
// it cannot be read directly. It CAN be invoked: the extractor's own contract is
// (request) -> (objectType, objectID). This package resolves each method's request
// message from the global proto registry — which yields the same GENERATED Go type
// the extractor type-asserts on — and invokes the extractor against a zero-valued
// instance. The id comes back empty (nobody filled the field); the object type
// comes back exactly as the service declared it.
//
// # The lane axis comes first
//
// Before "which relation on which object" there is "who decides at all". A method
// the owning service declares scope-filtered — there is no single object to ask
// about, so it narrows the answer itself, per element — cannot at the same time be
// a method the catalog says the edge gates on a relation. Comparing the relations
// in that case is meaningless: by the service's own account the edge is not asking
// the question. Left uncompared, that disagreement is worse than a drift, because
// the relation such rows named was `viewer` on the `cluster` singleton, which the
// cluster bootstrap grants to `user:*` so every tenant can read the global
// reference catalog — a check that admits every authenticated subject while
// reading, in the catalog, exactly like one that narrows.
//
// The rule is asymmetric on purpose: a catalog row that declares NO edge check
// (`<exempt>`) alongside a checking service map is not a contradiction — the
// catalog's statement about the edge stays true and the service adds a layer above
// it. A declaration may be exceeded; it may not be contradicted.
//
// # Scope of the comparison
//
// Only methods present in BOTH artefacts are compared. Methods whose lanes agree
// that no per-RPC relation is checked at the edge (catalog `<exempt>` /
// `scope_filtered`, service Public / ScopeFiltered) are reported separately rather
// than compared, because for them "no per-RPC relation" is the declared design,
// not a drift.
package catalogparity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// CatalogPath is the repo-relative location of the generated catalog embedded
// into the api-gateway binary. It is the artefact the gateway actually enforces.
const CatalogPath = "gateway/internal/middleware/embed/permission_catalog.json"

// ExemptPermission marks a catalog row that bypasses per-RPC authz entirely.
const ExemptPermission = "<exempt>"

// Entry is the subset of a catalog row this comparison needs.
type Entry struct {
	FQN              string `json:"fqn"`
	Permission       string `json:"permission"`
	RequiredRelation string `json:"required_relation"`
	ScopeExtractor   struct {
		ObjectType                 string `json:"object_type"`
		FromRequestField           string `json:"from_request_field"`
		ObjectTypeFromRequestField string `json:"object_type_from_request_field"`
	} `json:"scope_extractor"`
	// ScopeFiltered — the catalog's own declaration that the OWNING SERVICE
	// authorizes this call over the data it answers with, so the edge
	// authenticates and runs no per-RPC Check. It is the catalog-side counterpart
	// of authz.RPCEntry.ScopeFiltered, and Compare requires the two to agree.
	ScopeFiltered bool `json:"scope_filtered"`
}

// LoadCatalog reads the generated catalog, keyed by gRPC full method
// ("/kacho.cloud.storage.v1.VolumeService/Get") so it joins directly against
// authz.RPCMap keys. The catalog stores FQNs without the leading slash.
//
// dir is any directory inside the module; the repo root is located by walking up
// to the go.mod that declares the module.
func LoadCatalog(dir string) (map[string]Entry, error) {
	root, err := moduleRoot(dir)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(root, CatalogPath))
	if err != nil {
		return nil, fmt.Errorf("read permission catalog: %w", err)
	}
	var rows []Entry
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode permission catalog: %w", err)
	}
	out := make(map[string]Entry, len(rows))
	for _, r := range rows {
		out["/"+r.FQN] = r
	}
	return out, nil
}

// moduleRoot walks up from dir until it finds the go.mod of this module.
func moduleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		abs = parent
	}
}

// Lane names WHO decides, before any question of which relation on which object.
// It is the coarsest axis and the one that has to agree first: when the two
// artefacts disagree about who authorizes a call, comparing the relation they
// each name is meaningless.
const (
	// LaneEdgeChecks — the edge runs a per-RPC Check (a relation on a scope).
	LaneEdgeChecks = "edge-checks"
	// LaneScopeFiltered — the owning service authorizes over the data it answers
	// with; the edge authenticates and checks nothing.
	LaneScopeFiltered = "scope-filtered"
	// LaneExempt — the catalog declares no per-RPC authz at the edge at all.
	LaneExempt = "exempt"
	// LanePublic — the service map exempts the method from its own per-RPC Check.
	LanePublic = "public"
)

// Divergence is one method whose in-service requirement contradicts the catalog.
type Divergence struct {
	Method string
	// Kind is "lane", "relation" or "scope".
	Kind             string
	CatalogRelation  string
	ServiceRelation  string
	CatalogScopeType string
	ServiceScopeType string
	CatalogLane      string
	ServiceLane      string
}

func (d Divergence) String() string {
	switch d.Kind {
	case "lane":
		return fmt.Sprintf("%s: catalog declares lane %q, service map declares %q",
			d.Method, d.CatalogLane, d.ServiceLane)
	case "relation":
		return fmt.Sprintf("%s: catalog requires relation %q, service map requires %q",
			d.Method, d.CatalogRelation, d.ServiceRelation)
	default:
		return fmt.Sprintf("%s: catalog anchors scope on object type %q, service map anchors on %q",
			d.Method, d.CatalogScopeType, d.ServiceScopeType)
	}
}

// catalogLane classifies the catalog row. A row with `scope_filtered` names the
// service as the decider; `<exempt>` (or, historically, a row carrying no
// relation at all) says only that the edge does not decide; anything else
// declares an edge check.
func catalogLane(e Entry) string {
	switch {
	case e.ScopeFiltered:
		return LaneScopeFiltered
	case e.Permission == ExemptPermission || e.RequiredRelation == "":
		return LaneExempt
	default:
		return LaneEdgeChecks
	}
}

// serviceLane classifies the in-service map entry by the same question.
func serviceLane(e authz.RPCEntry) string {
	switch {
	case e.ScopeFiltered:
		return LaneScopeFiltered
	case e.Public:
		return LanePublic
	default:
		return LaneEdgeChecks
	}
}

// lanesContradict reports whether the two declarations cannot both be true.
//
// The rule is deliberately ASYMMETRIC, because the two artefacts do not make
// symmetric promises. The catalog is what an operator reads to learn what the
// EDGE does; the service map is what the SERVICE does.
//
//   - service scope-filtered vs catalog edge-checks — CONTRADICTION. The catalog
//     tells the operator the edge narrows the call by checking a relation on an
//     object; the owning service's position is that no single object can answer
//     for this call at all. Whatever the edge asks is then not the question, and
//     in practice such rows named a relation satisfied by a wildcard tuple — a
//     check with the shape of authorization and none of the substance.
//   - catalog scope-filtered vs anything but service scope-filtered —
//     CONTRADICTION. `scope_filtered` is a POSITIVE promise that the service
//     narrows per element, and the edge stops checking on the strength of it. If
//     the service performs one ordinary check (or none, being public), the promise
//     is unbacked and the two layers between them narrow nothing per element.
//   - catalog exempt vs a checking service map — NOT a contradiction. The catalog
//     says "the edge does not authorize this", which remains true; the service
//     adds a layer above it. A declaration may be exceeded, never contradicted.
func lanesContradict(cat, svc string) bool {
	if svc == LaneScopeFiltered && cat == LaneEdgeChecks {
		return true
	}
	if cat == LaneScopeFiltered && svc != LaneScopeFiltered {
		return true
	}
	return false
}

// Report is the full outcome of comparing one service map against the catalog.
type Report struct {
	// Divergences are the contradictions: same method, different answer.
	Divergences []Divergence
	// Compared is the number of methods checked on both axes.
	Compared int
	// SkippedExempt lists methods the catalog marks `<exempt>` (nothing to mirror).
	SkippedExempt []string
	// SkippedNonChecking lists methods the service map declares Public or
	// ScopeFiltered — authorization is taken elsewhere by design.
	SkippedNonChecking []string
	// NotInCatalog lists in-service methods absent from the catalog (internal
	// listener RPCs that were never published through the gateway).
	NotInCatalog []string
}

// Compare joins the service's RPCMap against the catalog and reports every
// method where the two disagree on the required relation or on the scope object
// type. Methods that cannot be compared are reported, not silently dropped.
func Compare(serviceMap authz.RPCMap, catalog map[string]Entry) Report {
	var rep Report
	methods := make([]string, 0, len(serviceMap))
	for m := range serviceMap {
		methods = append(methods, m)
	}
	sort.Strings(methods)

	for _, method := range methods {
		entry := serviceMap[method]
		cat, ok := catalog[method]
		if !ok {
			rep.NotInCatalog = append(rep.NotInCatalog, method)
			continue
		}
		// Lane FIRST: who decides. A disagreement here makes the relation and
		// scope axes moot, so it is reported on its own and the method is not
		// compared further.
		catLane, svcLane := catalogLane(cat), serviceLane(entry)
		if lanesContradict(catLane, svcLane) {
			rep.Divergences = append(rep.Divergences, Divergence{
				Method: method, Kind: "lane",
				CatalogLane: catLane, ServiceLane: svcLane,
			})
			continue
		}
		if svcLane == LaneScopeFiltered || svcLane == LanePublic {
			rep.SkippedNonChecking = append(rep.SkippedNonChecking, method)
			continue
		}
		if catLane == LaneExempt {
			rep.SkippedExempt = append(rep.SkippedExempt, method)
			continue
		}
		rep.Compared++

		if cat.RequiredRelation != entry.Relation {
			rep.Divergences = append(rep.Divergences, Divergence{
				Method: method, Kind: "relation",
				CatalogRelation: cat.RequiredRelation, ServiceRelation: entry.Relation,
			})
		}

		// Scope axis. A polymorphic catalog scope (object type taken from a
		// request field at call time) has no single declared type to compare.
		if cat.ScopeExtractor.ObjectTypeFromRequestField != "" {
			continue
		}
		svcScope, ok := ScopeObjectType(method, entry)
		if !ok {
			continue
		}
		if cat.ScopeExtractor.ObjectType != svcScope {
			rep.Divergences = append(rep.Divergences, Divergence{
				Method: method, Kind: "scope",
				CatalogScopeType: cat.ScopeExtractor.ObjectType, ServiceScopeType: svcScope,
			})
		}
	}
	return rep
}

// Keys renders the report's divergences as a sorted, stable set of strings, so a
// caller can diff "what diverges now" against "what was known to diverge". The
// diff is what makes the enumeration a gate rather than a comment: a NEW
// divergence appears as an unexpected key, and a divergence that has been resolved
// appears as a missing one — which must delete its entry from the known list
// instead of leaving a stale exemption behind.
func (r Report) Keys() []string {
	out := make([]string, 0, len(r.Divergences))
	for _, d := range r.Divergences {
		out = append(out, d.String())
	}
	sort.Strings(out)
	return out
}

// Diff compares the report's divergences against an expected set, returning the
// ones that are new (present now, not expected) and the ones that are stale
// (expected, no longer present).
func (r Report) Diff(known []string) (unexpected, resolved []string) {
	have := map[string]bool{}
	for _, k := range r.Keys() {
		have[k] = true
	}
	want := map[string]bool{}
	for _, k := range known {
		want[k] = true
	}
	for k := range have {
		if !want[k] {
			unexpected = append(unexpected, k)
		}
	}
	for k := range want {
		if !have[k] {
			resolved = append(resolved, k)
		}
	}
	sort.Strings(unexpected)
	sort.Strings(resolved)
	return unexpected, resolved
}

// ScopeObjectType recovers the FGA object type an RPCEntry's extractor declares,
// by invoking it against a zero-valued instance of the method's request message
// resolved from the global proto registry. ok=false when the request type is not
// registered, when the entry carries no extractor, or when the extractor is
// request-dependent (it errors or panics on an empty request) — in which case
// there is no single declared type to compare.
func ScopeObjectType(fullMethod string, entry authz.RPCEntry) (objectType string, ok bool) {
	if entry.Extract == nil {
		return "", false
	}
	req, ok := zeroRequest(fullMethod)
	if !ok {
		return "", false
	}
	defer func() {
		// A scope-conditional extractor may dereference fields that only exist on
		// a populated request. That is not a divergence, it is "no static answer".
		if r := recover(); r != nil {
			objectType, ok = "", false
		}
	}()
	ot, _, err := entry.Extract(req)
	if err != nil || ot == "" {
		return "", false
	}
	return ot, true
}

// zeroRequest resolves the request message of fullMethod ("/pkg.Service/Method")
// from the global proto registry and returns a zero-valued instance of the
// GENERATED Go type — the same concrete type the service's extractor asserts on.
func zeroRequest(fullMethod string) (any, bool) {
	trimmed := strings.TrimPrefix(fullMethod, "/")
	slash := strings.LastIndex(trimmed, "/")
	if slash < 0 {
		return nil, false
	}
	svcName, methodName := trimmed[:slash], trimmed[slash+1:]

	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(svcName))
	if err != nil {
		return nil, false
	}
	sd, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, false
	}
	md := sd.Methods().ByName(protoreflect.Name(methodName))
	if md == nil {
		return nil, false
	}
	mt, err := protoregistry.GlobalTypes.FindMessageByName(md.Input().FullName())
	if err != nil {
		return nil, false
	}
	return mt.New().Interface(), true
}
