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
// # Scope of the comparison
//
// Only methods present in BOTH artefacts are compared. Methods the catalog marks
// `<exempt>` carry no relation to mirror. Entries the service marks Public or
// ScopeFiltered take their authorization elsewhere (a data-level filter, or an
// explicit exemption) and are reported separately rather than compared, because
// for them "no per-RPC relation" is the declared design, not a drift.
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

// Divergence is one method whose in-service requirement contradicts the catalog.
type Divergence struct {
	Method string
	// Kind is "relation" or "scope".
	Kind             string
	CatalogRelation  string
	ServiceRelation  string
	CatalogScopeType string
	ServiceScopeType string
}

func (d Divergence) String() string {
	if d.Kind == "relation" {
		return fmt.Sprintf("%s: catalog requires relation %q, service map requires %q",
			d.Method, d.CatalogRelation, d.ServiceRelation)
	}
	return fmt.Sprintf("%s: catalog anchors scope on object type %q, service map anchors on %q",
		d.Method, d.CatalogScopeType, d.ServiceScopeType)
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
		if cat.Permission == ExemptPermission || cat.RequiredRelation == "" {
			rep.SkippedExempt = append(rep.SkippedExempt, method)
			continue
		}
		if entry.Public || entry.ScopeFiltered {
			rep.SkippedNonChecking = append(rep.SkippedNonChecking, method)
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
