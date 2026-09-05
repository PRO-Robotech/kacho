// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter states how kacho-geo is laid out for the public-List
// gate. The analysis itself lives in pkg/listfiltergate.
//
// # Why this exists
//
// geo had no analyser of this class. Unlike iam, whose absence hid 30 unjudged
// listing methods, geo's absence hid something subtler and worth writing down: geo's
// two listings are SUPPOSED to be unnarrowed, and nothing recorded that as a
// decision anywhere a check could read it.
//
// That is not a harmless difference. "This service is exempt" and "nobody ever
// looked at this service" are indistinguishable from the outside, and the second one
// is what actually obtained. The exemption now lives in a declaration that expires
// with its method, next to the reason for it.
//
// # This service's shape
//
// geo keeps its transport in one flat package, internal/handler, with a handler type
// per resource (RegionHandler, ZoneHandler) — compute's layout, not vpc's. Two
// listing methods, both named List; there are no child listings.
//
// # Why both are ClusterScoped
//
// Region and Zone are the admin-curated global catalog of the placement axis. Every
// authenticated tenant must be able to read every row, because zoneId and regionId
// are where any placeable resource gets launched: a per-object narrowing would leave
// a tenant with no bindings unable to create anything at all.
//
// This is the documented project-scope exemption in .claude/rules/security.md — the
// four public read RPCs carry `permission = "<exempt>"` in the proto, so the gateway
// makes no per-RPC scope check. Two properties of that exemption are worth keeping
// straight, because the gate's silence must not be read as covering them:
//
//   - authN is NOT exempt. An unauthenticated caller still gets UNAUTHENTICATED;
//     the exemption removed the project-scope authorization, nothing else. That is
//     the gateway's boot guard's subject, not this gate's;
//   - the infra-sensitive projection is NOT on this surface. Raw status and the
//     infra fields live on InternalRegion/InternalZone on the internal listener,
//     which declares no listing method at all — so there is nothing here for this
//     gate to be silent about.
package auditlistfilter

import "github.com/PRO-Robotech/kacho/pkg/listfiltergate"

// placementCatalog is the shared reason for both listings.
const placementCatalog = "Region and Zone are the admin-curated global catalog of the placement " +
	"axis: every authenticated tenant must read every row to launch any placeable resource at " +
	"all, so there are no per-object grants to narrow to. This is the documented project-scope " +
	"exemption (security.md); authN remains required, and the infra-sensitive projection stays " +
	"on the internal listener. The exclusion expires with its method — retire the RPC and this " +
	"entry becomes a finding."

// Profile describes kacho-geo to the analyser.
var Profile = listfiltergate.Profile{
	Service:    "geo",
	AnchorRoot: "internal/handler",
	// One flat package, a handler type per resource.
	PerPackage:     false,
	ReceiverSuffix: "Handler",
	// Named for completeness: geo has no per-object filter because it has nothing to
	// narrow. If a listing here ever stops being catalog data, its declaration has to
	// change and these are the calls the new shape would be asserted against.
	Filters: []string{"FilterVisiblePage", "FilterVisibleIDs"},
	Banned:  []string{"ListAllowedIDs", "ListObjects"},
	// Why there is no EnumerationSources here, and why that is a DECLARATION rather
	// than the silence it used to be (#684).
	//
	// Until this line, geo printed "no enumeration source declared" on every run —
	// the gate saying that the enumerate-then-narrow ban was the two hand-written
	// names and nothing else. For its four neighbours the remedy is to name the
	// surfaces the ban is derived from. geo has none to name, and naming one anyway
	// — copying a neighbour's answer — would be a declaration with nothing behind
	// it, which is the class this gate exists to refuse.
	//
	// The measurement, so the claim is checkable rather than asserted: geo declares
	// no authorization client of its own (no internal/clients, no internal/authzfilter,
	// no internal/check), and its per-RPC Check is the shared interceptor's, wired in
	// the composition root. Predicate: `grep -rn "BatchCheck\|listnarrow" services/geo
	// --include=*.go` → nothing outside tests.
	//
	// What is DECLARED instead is the property the gate can prove: both listings are
	// the admin-curated placement catalog, answered without narrowing, so no page
	// here can be taken from an enumeration. The gate counts the narrowing listings
	// on every run and this entry is a FINDING the moment that count stops being
	// zero — so the exemption cannot outlive its subject, and it cannot be reached
	// for by a service that simply has not looked.
	EnumerationInapplicable: "Region and Zone are answered from the global placement catalog without " +
		"narrowing, so there is no page here that could be taken from an enumeration: the ban has " +
		"nothing to apply to. geo declares no authorization surface of its own — its per-RPC Check is " +
		"the shared interceptor's — so there is no method set to derive the ban from either. The " +
		"exemption expires the moment any listing here narrows.",
	SubjectScopers: []string{"ListForCaller"},

	Listings: map[string]listfiltergate.Listing{
		"region.List": {Shape: listfiltergate.ClusterScoped, Reason: placementCatalog},
		"zone.List":   {Shape: listfiltergate.ClusterScoped, Reason: placementCatalog},
	},
}
