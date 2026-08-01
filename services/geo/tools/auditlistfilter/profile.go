// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter states how kacho-geo is laid out for the public-List
// gate. The analysis itself lives in tools/listfiltergate.
//
// # Why this exists
//
// geo had no analyser of this class. Unlike iam, whose absence hid 31 unjudged
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

import "github.com/PRO-Robotech/kacho/tools/listfiltergate"

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
	Filters:        []string{"FilterVisiblePage", "FilterVisibleIDs"},
	Banned:         []string{"ListAllowedIDs", "ListObjects"},
	SubjectScopers: []string{"ListForCaller"},

	Listings: map[string]listfiltergate.Listing{
		"region.List": {Shape: listfiltergate.ClusterScoped, Reason: placementCatalog},
		"zone.List":   {Shape: listfiltergate.ClusterScoped, Reason: placementCatalog},
	},
}
