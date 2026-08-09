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
// # Declarations
//
// vpc has by far the widest listing surface of the three services this analyser
// drives: 21 listing methods across 8 resources. The previous gate saw 8 of them —
// the ones named exactly `List` — and was silent about the other 13, which are the
// child collections and the operation histories. Each is now declared.
//
//   - the eight `List` RPCs are the project-scoped collections and stay RowFilter;
//   - eleven child listings (ListSubnets, ListSecurityGroups, ListRouteTables,
//     ListUsedAddresses and the seven ListOperations) are ParentGate: the handler
//     reads the containing resource through get.Execute and returns on its error
//     before the page is read. The gate is named "get.Execute" rather than
//     "Execute" because the page read is ALSO an Execute — matching the bare method
//     name would let the page read vouch for its own gate;
//   - address.ListBySubnet is EdgeGate: nothing in the service reads the subnet, and
//     the handler's own comment says so — the per-RPC authorization at the edge is
//     what settles it. That is a real check, so the gate verifies it in the proto
//     rather than taking the word for it: rpc ListBySubnet must carry a
//     required_relation and a scope_extractor on subnet_id. Had it resolved its
//     scope from "*", the relation would be satisfied by the wildcard tuple the
//     cluster catalog is opened with, and the declaration would be documenting a
//     check that narrows nothing;
//   - addresspool's two listings are ClusterScoped. It used to be excluded with
//     --allow=addresspool, which excluded the RESOURCE — and so also covered
//     ListAddresses, a method the exclusion was never written about. Both now say
//     so separately.
package auditlistfilter

import "github.com/PRO-Robotech/kacho/tools/listfiltergate"

// parentGate is the shape of every child listing in this service: read the
// containing resource first, return on its error, then read the page.
func parentGate() listfiltergate.Listing {
	return listfiltergate.Listing{Shape: listfiltergate.ParentGate, Gate: "get.Execute"}
}

// adminPool is the shared reason for addresspool's two listings.
const adminPool = "AddressPool is an Internal admin RPC gated on system_admin in middleware: a " +
	"cluster-wide pool inventory with no per-object grants to narrow to. The exclusion expires " +
	"with its method — retire the RPC and this entry becomes a finding."

// Profile describes kacho-vpc to the analyser.
var Profile = listfiltergate.Profile{
	Service:    "vpc",
	AnchorRoot: "internal/apps/kacho/api",
	// One package per resource, all declaring the same transport type.
	PerPackage:     true,
	ReceiverSuffix: "Handler",
	Filters:        []string{"listnarrow.Page", "listnarrow.IDs"},
	Banned:         []string{"ListAllowedIDs", "ListObjects"},
	SubjectScopers: []string{"ListForCaller"},
	ProtoFiles:     []string{"kacho/cloud/vpc/v1/address_service.proto"},
	FGAModel:       "kacho/cloud/iam/v1/fga_model.fga",

	Listings: map[string]listfiltergate.Listing{
		"address.List":          {Shape: listfiltergate.RowFilter},
		"gateway.List":          {Shape: listfiltergate.RowFilter},
		"network.List":          {Shape: listfiltergate.RowFilter},
		"networkinterface.List": {Shape: listfiltergate.RowFilter},
		"routetable.List":       {Shape: listfiltergate.RowFilter},
		"securitygroup.List":    {Shape: listfiltergate.RowFilter},
		"subnet.List":           {Shape: listfiltergate.RowFilter},

		"address.ListOperations":          parentGate(),
		"gateway.ListOperations":          parentGate(),
		"network.ListOperations":          parentGate(),
		"network.ListRouteTables":         parentGate(),
		"network.ListSecurityGroups":      parentGate(),
		"network.ListSubnets":             parentGate(),
		"networkinterface.ListOperations": parentGate(),
		"routetable.ListOperations":       parentGate(),
		"securitygroup.ListOperations":    parentGate(),
		"subnet.ListOperations":           parentGate(),
		"subnet.ListUsedAddresses":        parentGate(),

		"address.ListBySubnet": {
			Shape:       listfiltergate.EdgeGate,
			ProtoFile:   "kacho/cloud/vpc/v1/address_service.proto",
			ParentField: "subnet_id",
		},

		"addresspool.List":          {Shape: listfiltergate.ClusterScoped, Reason: adminPool},
		"addresspool.ListAddresses": {Shape: listfiltergate.ClusterScoped, Reason: adminPool},
	},
}

// InternalProfile describes kacho-vpc's OTHER transport package.
//
// vpc is the only service with two: the per-resource packages under
// internal/apps/kacho/api, and a flat internal/handler holding the internal
// listener's handlers. Profile above covers the first. This covers the second, and
// until it existed the gate's census read "8 resources, 21 listing methods" while
// the tree held 22 — the missing one being an RPC that returns NIC attachments for
// instance ids THE CALLER NAMES, with no per-RPC check behind it at all
// (scope_filtered), which makes it among the least suitable methods in the
// repository to be going unjudged.
//
// This is the residual the widened predicate did NOT reach: the method was outside
// the anchor root, not merely outside the name match. Worth stating plainly, because
// "we widened the predicate" is the kind of sentence that gets read as "and therefore
// everything is now seen". Coverage of the tree is a separate property from coverage
// of the names, and it has its own check — census_test.go compares what each analyser
// judged against every transport listing method in the service.
var InternalProfile = listfiltergate.Profile{
	Service:    "vpc-internal",
	AnchorRoot: "internal/handler",
	// A flat package with a handler type per resource — compute's layout.
	PerPackage:     false,
	ReceiverSuffix: "Handler",
	// "svc.ListByInstance" is the delegation into nicinternal.Service, one package
	// over, where the per-NIC narrowing lives; the analyser's walk does not leave the
	// package it is judging. Named the same way as iam's listOp.Execute and safe for
	// the same two reasons: renaming the field turns the gate RED rather than quiet,
	// and the far side is asserted by internal_nic_test.go in this package instead of
	// being assumed.
	Filters:        []string{"listnarrow.Page", "listnarrow.IDs", "svc.ListByInstance"},
	Banned:         []string{"ListAllowedIDs", "ListObjects"},
	SubjectScopers: []string{"ListForCaller"},

	Listings: map[string]listfiltergate.Listing{
		"internal_network_interface.ListByInstance": {Shape: listfiltergate.RowFilter},
	},
}
