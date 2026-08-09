// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package catalogderive

import (
	"fmt"
	"sort"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogparity"
)

// Diff compares a hand-written service map against the map derived from the same
// service's annotations, and returns every disagreement as a sorted, stable line.
//
// # Why this exists, and why it is scaffolding
//
// Switching a service from its hand-written map to the derived one changes what
// the service asks on every RPC. Doing that without first enumerating the
// differences would change somebody's access silently — the exact failure the
// derivation is meant to end. So the switch is preceded by this comparison, every
// line it prints is adjudicated by hand, and the disagreement is closed by
// correcting whichever side is wrong (the proto annotation, per the standing
// rule that the service wins wherever it is the stricter of the two).
//
// It is deleted together with the last hand-written map: a comparison whose left
// operand no longer exists is a check with nothing left to check.
func Diff(manual, derived authz.RPCMap) []string {
	var out []string

	for method, m := range manual {
		d, ok := derived[method]
		if !ok {
			out = append(out, fmt.Sprintf("%s: present in the hand-written map, absent from the "+
				"annotations", method))
			continue
		}
		out = append(out, compareEntry(method, m, d)...)
	}
	for method := range derived {
		if _, ok := manual[method]; !ok {
			out = append(out, fmt.Sprintf("%s: declared by the annotations, absent from the "+
				"hand-written map", method))
		}
	}

	sort.Strings(out)
	return out
}

func compareEntry(method string, m, d authz.RPCEntry) []string {
	var out []string

	// Lane first: who decides at all. When the two disagree here, comparing the
	// relation each names answers a question neither is asking.
	if ml, dl := lane(m), lane(d); ml != dl {
		return []string{fmt.Sprintf("%s: lane — hand-written map says %q, annotations say %q",
			method, ml, dl)}
	}

	if m.HideExistence != d.HideExistence {
		out = append(out, fmt.Sprintf("%s: hide-existence — hand-written map=%t, annotations=%t",
			method, m.HideExistence, d.HideExistence))
	}

	// Relation and scope are compared only where they DECIDE. In the exempt and
	// scope-filtered lanes the interceptor never reads them (it branches on the
	// lane first), so a value there is decoration, and reporting it would spend
	// adjudication on entries that change no access. `Permission` is not compared
	// for the same reason and one more: the interceptor does not read it in ANY
	// lane, and the derived map takes it from the same annotations the catalog
	// does, so after the switch there is one string where there were two.
	if lane(m) != LaneEdgeChecks {
		return out
	}

	if m.Relation != d.Relation {
		out = append(out, fmt.Sprintf("%s: relation — hand-written map requires %q, annotations "+
			"require %q", method, m.Relation, d.Relation))
	}

	ms, mok := catalogparity.ScopeObjectType(method, m)
	ds, dok := catalogparity.ScopeObjectType(method, d)
	switch {
	case mok != dok:
		out = append(out, fmt.Sprintf("%s: scope — one side declares a static object type and the "+
			"other does not (hand-written map=%t, annotations=%t)", method, mok, dok))
	case mok && ms != ds:
		out = append(out, fmt.Sprintf("%s: scope — hand-written map anchors on %q, annotations "+
			"anchor on %q", method, ms, ds))
	}
	return out
}

// Lane names of the in-process map, mirroring catalogparity's names for the
// catalog side so a divergence line reads the same on both.
const (
	LaneScopeFiltered = "scope-filtered"
	LaneExempt        = "exempt"
	LaneEdgeChecks    = "edge-checks"
)

func lane(e authz.RPCEntry) string {
	switch {
	case e.ScopeFiltered:
		return LaneScopeFiltered
	case e.Public:
		return LaneExempt
	default:
		return LaneEdgeChecks
	}
}
