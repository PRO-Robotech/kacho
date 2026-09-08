// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"encoding/json"
	"strings"
	"testing"
)

// hideExistenceReachableTypes — the FGA object types that can actually reach
// notFoundMessage, i.e. the `scope_extractor.object_type` of every catalog entry
// for which CatalogEntry.HidesExistenceOnDeny reports true. Derived from the
// embedded permission catalog by TestHideExistenceMap_CoversCatalogReachableTypes
// below; this map additionally records the VERBATIM NotFound text the owning
// service produces for a genuine miss, so hide-existence can be locked to
// byte-identity (security.md hardening-invariant #6).
//
// Each `want` is copied from the owning service's repo layer (the sentinel
// prefix is stripped on the way out by that service's error mapper, so the
// repo format IS the wire text):
//
//	iam       services/iam/internal/repo/kaname/pg/*.go        (shared.MapRepoErr → iamerr.StripSentinel)
//	compute   services/compute/internal/repo/instance_repo.go  (service.mapRepoErr → stripSentinel)
//	vpc       services/vpc/internal/repo/kacho/pg/*.go
//	storage   services/storage/internal/repo/pg/*.go          (serviceerr strip)
//	nlb       services/nlb/internal/repo/kacho/pg/*.go        (shared.StripSentinel)
//	registry  services/registry/internal/repo/kacho/pg/errmap.go    (wrapPgErr, resource "Registry")
var hideExistenceReachableTypes = map[string]string{
	// iam — services/iam/internal/repo/kaname/pg/*.go
	"account":             "Account %s not found",        // account_repo.go
	"project":             "Project %s not found",        // project_repo.go
	"iam_user":            "User %s not found",           // user_pool_repo.go
	"iam_group":           "Group %s not found",          // group_repo.go
	"iam_service_account": "ServiceAccount %s not found", // service_account_repo.go
	"iam_access_binding":  "AccessBinding %s not found",  // access_binding_repo.go
	// compute — services/compute/internal/repo/instance_repo.go. Disk / Image /
	// Snapshot left with the retired block-storage duplicate: no catalog entry
	// anchors those object types any more, so nothing can reach their row.
	"compute_instance": "Instance %s not found", // instance_repo.go
	// Ключ входа в машину — services/compute/internal/repo/guest_access_key_repo.go.
	"compute_guest_access_key": "GuestAccessKey %s not found",
	"compute_placement_group":  "PlacementGroup %s not found",
	// vpc — services/vpc/internal/repo/kacho/pg/*.go
	"vpc_network":           "Network %s not found",
	"vpc_subnet":            "Subnet %s not found",
	"vpc_address":           "Address %s not found",
	"vpc_route_table":       "Route table %s not found",
	"vpc_security_group":    "Security group SecurityGroup.Id(value=%s) not found",
	"vpc_gateway":           "Gateway %s not found",
	"vpc_network_interface": "Network interface %s not found",
	"vpc_cidr_group":        "CidrGroup %s not found",
	// storage — services/storage/internal/repo/pg/*.go (serviceerr strips the
	// sentinel prefix, so the repo format is the wire text). Reachable since
	// storage's object-self reads moved from the tier onto `v_get`.
	"storage_volume":   "Volume %s not found",   // volume_repo.go
	"storage_snapshot": "Snapshot %s not found", // snapshot_repo.go
	"storage_image":    "Image %s not found",    // image_repo.go
	// nlb — services/nlb/internal/repo/kacho/pg/*.go
	"nlb_network_load_balancer": "NetworkLoadBalancer %s not found", // load_balancer_repo.go
	"nlb_listener":              "Listener %s not found",            // listener_repo.go
	"nlb_target_group":          "TargetGroup %s not found",         // target_group_repo.go
	// registry — services/registry/internal/repo/kacho/pg/registry.go
	"registry_registry": "Registry %s not found",
}

// hideExistenceNoBackendTypes — object types that are reachable per the catalog
// but have NO owning-service implementation yet, so there is no genuine-miss text
// to byte-match. They must fall through to the neutral message; inventing a text
// here would be a guess, and echoing the FGA token would be an oracle.
// Empty on purpose. The one entry it ever held — compute_instance_group — was a
// service declared in proto and routed at the edge with no implementation behind
// it anywhere; it has since been withdrawn from the contract entirely, so the
// exemption has nothing left to exempt. An entry here is a standing admission
// that a reachable route has no owner, so it stays empty unless that is true
// again and written down.
var hideExistenceNoBackendTypes = map[string]string{}

// TestNotFoundMessage_NoFGATokenLeak is the existence-oracle regression lock.
//
// A hide-existence 404 MUST be byte-identical to the owning service's genuine
// NotFound; if it is not, a denied caller distinguishes "exists but forbidden"
// (gateway text) from "does not exist" (backend text) — the oracle security.md
// hardening-invariant #6 forbids and the file docstring claims is closed.
//
// The pre-fix defect: only 8 of the reachable object types were mapped, so every
// other one fell through to `desc.ResourceType + " not found"` and printed the
// raw internal FGA token ("compute_disk not found", "iam_user not found",
// "registry_registry not found") — distinguishable from the backend's
// "Disk <id> not found", and a token that appears nowhere on the public surface.
func TestNotFoundMessage_NoFGATokenLeak(t *testing.T) {
	const id = "dsk1234567890abcdefg" // caller-supplied id, echoed back (they already know it)

	for objectType, format := range hideExistenceReachableTypes {
		t.Run(objectType, func(t *testing.T) {
			got := notFoundMessage(permissionDeniedDescriptor{
				ResourceType: objectType,
				ResourceID:   id,
			})
			want := strings.Replace(format, "%s", id, 1)
			if got != want {
				t.Errorf("hide-existence text is NOT byte-identical to the owning service's miss:\n got  = %q\n want = %q", got, want)
			}
			// Independent of the exact wording: the internal FGA object type must
			// never appear on the wire.
			if strings.Contains(got, objectType) {
				t.Errorf("FGA object type token %q leaked into the 404 message %q", objectType, got)
			}
		})
	}
}

// TestNotFoundMessage_UnmappedTypeIsNeutral locks the fail-closed fallback: an
// object type with no entry in hideExistenceNotFoundFormats must yield the bare
// neutral "not found" — never "<fga_token> not found". Echoing the token leaks
// the internal type dictionary and is trivially distinguishable from any real
// backend miss.
func TestNotFoundMessage_UnmappedTypeIsNeutral(t *testing.T) {
	cases := []struct {
		name         string
		resourceType string
		resourceID   string
	}{
		{"unknown object type with id", "some_future_type", "abc1234567890abcdefg"},
		{"unknown object type without id", "some_future_type", ""},
		{"no-backend type falls through", "compute_instance_group", "igp1234567890abcdefg"},
		{"mapped type but wildcard id", "vpc_subnet", "*"},
		{"mapped type but empty id", "vpc_subnet", ""},
		{"empty object type", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := notFoundMessage(permissionDeniedDescriptor{
				ResourceType: tc.resourceType,
				ResourceID:   tc.resourceID,
			})
			if got != "not found" {
				t.Errorf("fallback must be the neutral %q, got %q (leaks the internal object type)", "not found", got)
			}
		})
	}
}

// TestHideExistenceMap_CoversCatalogReachableTypes is the drift guard: it derives
// from the EMBEDDED catalog the full set of object types that can reach
// notFoundMessage, and fails when one is neither mapped to a backend-verbatim
// format nor explicitly declared as having no backend. A new object-scoped
// resource therefore cannot silently reintroduce the token leak.
func TestHideExistenceMap_CoversCatalogReachableTypes(t *testing.T) {
	var entries []CatalogEntry
	if err := json.Unmarshal(EmbeddedPermissionCatalogJSON(), &entries); err != nil {
		t.Fatalf("decode embedded catalog: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("embedded catalog is empty — cannot derive hide-existence reachability")
	}

	reachable := map[string]string{} // object type → one example FQN
	for _, e := range entries {
		if !e.HidesExistenceOnDeny(e.FQN) {
			continue
		}
		ot := strings.TrimSpace(e.ScopeExtractor.ObjectType)
		if ot == "" {
			continue
		}
		if _, seen := reachable[ot]; !seen {
			reachable[ot] = e.FQN
		}
	}
	if len(reachable) == 0 {
		t.Fatal("no hide-existence-reachable object type derived — reachability rule changed?")
	}

	for ot, fqn := range reachable {
		if _, mapped := hideExistenceNotFoundFormats[ot]; mapped {
			continue
		}
		if reason, known := hideExistenceNoBackendTypes[ot]; known {
			t.Logf("object type %q (%s) intentionally unmapped: %s", ot, fqn, reason)
			continue
		}
		t.Errorf("object type %q (reachable via %s) has no hide-existence format: its 404 would leak the FGA token instead of byte-matching the owning service's NotFound", ot, fqn)
	}

	// The expectation table used by TestNotFoundMessage_NoFGATokenLeak must track
	// the catalog too — otherwise a newly reachable type is "covered" by the map
	// without anyone checking the text against the owning service.
	for ot := range reachable {
		if _, ok := hideExistenceReachableTypes[ot]; ok {
			continue
		}
		if _, ok := hideExistenceNoBackendTypes[ot]; ok {
			continue
		}
		t.Errorf("object type %q is hide-existence-reachable but absent from the test expectation table — add its owning-service verbatim NotFound text", ot)
	}
}
