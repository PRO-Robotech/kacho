// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter states how kacho-nlb is laid out for the public-List
// gate. The analysis itself — and why it parses instead of grepping — lives in
// tools/listfiltergate.
//
// # This service's shape
//
// nlb colocates transport and use-cases per resource: internal/apps/kacho/api/<res>
// holds a `Handler` whose `List` delegates to a per-RPC use-case, and that use-case
// runs the page it just read through `authzfilter.FilterVisiblePage`. The transport
// type is called `Handler` in every package, so the PACKAGE is what tells one
// resource from another; the proof of filtering is a call reachable from the List
// declaration, wherever in the package it happens to live.
//
// # What the previous gate keyed on, and why that was a defect
//
// It iterated internal/apps/kacho/api/*/list.go — that is, it recognised a resource
// by a FILE being called list.go — and then searched that file's text for
// `authzfilter.Filter` and `authzfilter.FilterVisiblePage(`. Two consequences:
//
//   - moving the List use-case into any other file of the same package (splitting a
//     package by RPC is exactly what this service already does elsewhere) removed
//     the resource from the gate's view, and its List went unjudged;
//   - a text search cannot tell a call from a sentence about a call. Deleting the
//     filter and leaving the comment that described it kept the gate green.
//
// On top of both, a missing internal/apps/kacho/api exited 0 with a message on
// stderr, so "the gate could not find the tree" and "the tree is clean" were the
// same verdict.
//
// # Declarations
//
// Nothing here is excluded, and the census says so out loud: it names every listing
// method it judged, so a passing run reads as "these six were judged" rather than as
// the word OK. `announce`, `internal_lifecycle` and `operation` are not excluded
// either — they declare no listing method at all, so they are outside the gate by
// construction rather than by exception.
//
// The three ListOperations were invisible to the previous gate, which matched the
// method name `List` exactly. They are SubjectScoped: the handler delegates to a
// use-case in the same package, and that use-case narrows by operations.ListForCaller
// — the owner comes from the request context, not from an id the caller supplied. The
// analyser can see this one because the hop stays inside the package; where the
// use-case lives elsewhere (compute) the shape has to be asserted differently.
package auditlistfilter

import "github.com/PRO-Robotech/kacho/tools/listfiltergate"

// Profile describes kacho-nlb to the analyser.
var Profile = listfiltergate.Profile{
	Service:    "nlb",
	AnchorRoot: "internal/apps/kacho/api",
	// One package per resource, all declaring the same transport type.
	PerPackage:     true,
	ReceiverSuffix: "Handler",
	Filters:        []string{"FilterVisiblePage", "FilterVisibleIDs"},
	Banned:         []string{"ListAllowedIDs", "ListObjects"},
	SubjectScopers: []string{"ListForCaller"},

	Listings: map[string]listfiltergate.Listing{
		"listener.List":     {Shape: listfiltergate.RowFilter},
		"loadbalancer.List": {Shape: listfiltergate.RowFilter},
		"targetgroup.List":  {Shape: listfiltergate.RowFilter},

		"listener.ListOperations":     {Shape: listfiltergate.SubjectScoped},
		"loadbalancer.ListOperations": {Shape: listfiltergate.SubjectScoped},
		"targetgroup.ListOperations":  {Shape: listfiltergate.SubjectScoped},
	},
}
