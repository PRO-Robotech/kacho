// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package auditlistfilter

// Profile of notify, as the analyser reads it.
//
// # Transport
//
// internal/handler is one flat package; each gRPC service has its own transport
// type (internalNoticeHandler, publicNoticeHandler), and every method is one
// delegation to a use-case. The resource is the receiver TYPE's stem.
//
// # Listings
//
//   - public_notice.List / .ListByAccount — the page lives inside the project or the
//     account the request names. The use-case asks the owner of the model whether
//     the caller may read that scope (authzcheck.RequireScope, relation v_get),
//     returns on a denial, and only then reads the page of that scope
//     (publicnotice.Page). The RPCs are `<exempt>` at the edge with
//     HANDLER_DECIDES — the decision is the service's, and this is where it is read.
//   - internal_notice.List — InternalNoticeService on the internal listener only:
//     every notice of the installation in the internal projection, with no
//     per-object owners to narrow to. Access is the per-RPC relation, which the
//     carrier's authorization link checks from the compiled options; the gate reads
//     those options, not prose.
//
// # Narrowers
//
// notify builds a listnarrow narrower in two processes: notify-api (affected
// resources of a notice; authzwiring.NewListNarrower, its single construction —
// замысел З14 п.1) and notify-probe (visibility of feed rows to the subscription
// stream). Both are declared sites, both must be built over the kaname verdict
// client narrowiam.New, and a third construction is a finding.

const modulePath = "github.com/PRO-Robotech/kacho"

const appsPkg = modulePath + "/services/notify/internal/apps/kacho"

// internalSurfaceReason — why the installation-wide internal list narrows nothing
// per object, and what settles access instead.
//
// The relation is cluster#system_viewer. Whether a wildcard tuple satisfies it is a
// fact about the authorization model, which lives in the kaname tree, not here:
// there it is declared `[user, service_account]` — no `user:*`
// (kaname@8a84dcff6 internal/authzmodel/fga_model.fga, type cluster). This analyser
// cannot read that model and says so rather than claiming it checked it.
const internalSurfaceReason = "InternalNoticeService.List serves every notice of the installation in the " +
	"internal projection on the internal listener only; notices have no per-object owners to " +
	"narrow to. Access is the per-RPC relation cluster#system_viewer checked by the carrier's " +
	"authorization link from the compiled options (read below); that the relation admits no " +
	"wildcard subject is a fact of the kaname model, outside this tree."

// Profile describes notify to the analyser.
var Profile = Layout{
	Service:        "notify",
	TransportDir:   "internal/handler",
	ReceiverSuffix: "Handler",
	ModulePath:     modulePath,
	Listings: map[string]Listing{
		"public_notice.List":          scopeGate(),
		"public_notice.ListByAccount": scopeGate(),
		"internal_notice.List": {
			Shape:  AdminSurface,
			RPC:    "kacho.cloud.notify.v1.InternalNoticeService.List",
			Reason: internalSurfaceReason,
		},
	},
	NarrowerCtor: Func{Pkg: "github.com/PRO-Robotech/corelib/listnarrow", Name: "New"},
	NarrowerSites: []string{
		"cmd/notify-api/internal/authzwiring/authzwiring.go",
		"cmd/notify-probe/serve.go",
	},
	NarrowerClient: Func{Pkg: modulePath + "/pkg/listnarrow/narrowiam", Name: "New"},
	// The two surfaces through which notify asks kaname: the per-scope verdict of
	// the listing use-cases (authziam) and the page verdicts of both narrowers
	// (narrowiam). Both answer verdicts today; the first method added to either that
	// answers with a set of identifiers is banned in every listing use-case the day
	// it is written.
	AuthzSources: []TypeSource{
		{Pkg: modulePath + "/pkg/authz/authziam", Type: "checkClient"},
		{Pkg: modulePath + "/pkg/listnarrow/narrowiam", Type: "grpcAuthorizeClient"},
	},
}

func scopeGate() Listing {
	return Listing{
		Shape:        ScopeGate,
		Gate:         Func{Pkg: appsPkg + "/authzcheck", Name: "RequireScope"},
		Read:         Func{Pkg: appsPkg + "/api/publicnotice", Name: "Page"},
		ReadScopeArg: 2,
	}
}
