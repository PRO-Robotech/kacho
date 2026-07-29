// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter states how kacho-vpc is laid out for the public-List
// gate. The analysis itself — and why it parses instead of grepping — lives in
// tools/listfiltergate.
//
// # This service's shape
//
// vpc colocates transport and use-cases per resource: internal/apps/kacho/api/<res>
// holds a `Handler` whose `List` delegates to a per-RPC use-case, which reads the
// page and then narrows it through `FilterVisibleIDs`. The transport type is called
// `Handler` in every package, so the PACKAGE is what tells one resource from
// another.
//
// The filter call is worth noting: in most packages it does NOT sit in the use-case
// body but in a package-local helper the use-case calls (filterVisibleNetworks and
// its siblings). That is why the gate follows the calls a List makes instead of
// reading one method — and equally why it must not simply search the package, which
// would let any filtered neighbour vouch for an unfiltered List.
//
// # What the previous gate keyed on, and why that was a defect
//
// It collected candidates with `grep -rl 'func .* List(' --include='handler.go'` —
// that is, it recognised a resource by a FILE being called handler.go — and then
// searched that file and its sibling list.go for the filter's name. Two
// consequences:
//
//   - renaming handler.go, or moving the List declaration into any other file of
//     the package, removed the resource from the gate's view, and its List went
//     unjudged;
//   - a text search cannot tell a call from a sentence about a call, nor from an
//     interface method declaration. Deleting the filter and leaving the comment that
//     described it kept the gate green.
//
// On top of both, a missing internal/apps/kacho/api exited 0 with a message on
// stderr, so "the gate could not find the tree" and "the tree is clean" were the
// same verdict.
//
// # Whitelist
//
// addresspool is admin-only: it is an Internal RPC gated on system_admin in
// middleware, a cluster-wide pool inventory with no per-object grants to narrow to.
// It is the only exclusion, and the gate reports an exclusion with no subject as a
// finding, so it cannot outlive the resource.
package auditlistfilter

import "github.com/PRO-Robotech/kacho/tools/listfiltergate"

// Profile describes kacho-vpc to the analyser.
var Profile = listfiltergate.Profile{
	Service:    "vpc",
	AnchorRoot: "internal/apps/kacho/api",
	// One package per resource, all declaring the same transport type.
	PerPackage:     true,
	ReceiverSuffix: "Handler",
	Filters:        []string{"FilterVisibleIDs", "FilterVisiblePage"},
	Banned:         []string{"ListAllowedIDs", "ListObjects"},
}
