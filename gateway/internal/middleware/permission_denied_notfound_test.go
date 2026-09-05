// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import "testing"

// TestNotFoundMessage_ContractTone locks the hide-existence 404 message to the
// Kachō contract tone "<Resource> <id> not found" (api-conventions.md) for every
// vpc / nlb object-scoped resource. Before the fix the gateway emitted the raw
// FGA object type with no id ("vpc_subnet not found"), which (a) violated the
// contract tone and (b) was distinguishable from the backend's real NotFound
// ("Subnet <id> not found") — an existence oracle. The hide-existence message
// MUST byte-match what the owning service returns for a genuine miss so a denied
// caller cannot tell "exists but forbidden" from "does not exist".
//
// Expected texts are taken verbatim from the repo-layer NotFound of each service
// (services/vpc/internal/repo/kacho/pg/*.go, services/nlb/.../load_balancer_repo.go)
// and are the exact strings the Newman get-conf / get / get-unknown cases assert.
func TestNotFoundMessage_ContractTone(t *testing.T) {
	const id = "enpsnapshotnonexist01" // caller-supplied id echoed back (no leak)
	tests := []struct {
		name         string
		resourceType string
		resourceID   string
		want         string
	}{
		// vpc — must equal services/vpc/internal/repo/kacho/pg/*.go NotFound text.
		{"vpc network", "vpc_network", id, "Network " + id + " not found"},
		{"vpc subnet", "vpc_subnet", id, "Subnet " + id + " not found"},
		{"vpc address", "vpc_address", id, "Address " + id + " not found"},
		{"vpc route_table", "vpc_route_table", id, "Route table " + id + " not found"},
		{"vpc security_group", "vpc_security_group", id, "Security group SecurityGroup.Id(value=" + id + ") not found"},
		{"vpc gateway", "vpc_gateway", id, "Gateway " + id + " not found"},
		{"vpc network_interface", "vpc_network_interface", id, "Network interface " + id + " not found"},
		// nlb — must equal services/nlb/internal/repo/kacho/pg/*_repo.go.
		{"nlb load balancer", "nlb_network_load_balancer", id, "NetworkLoadBalancer " + id + " not found"},
		{"nlb listener", "nlb_listener", id, "Listener " + id + " not found"},
		{"nlb target group", "nlb_target_group", id, "TargetGroup " + id + " not found"},
		// iam — must equal services/iam/internal/repo/kaname/pg/*_repo.go.
		{"iam account", "account", id, "Account " + id + " not found"},
		{"iam project", "project", id, "Project " + id + " not found"},
		{"iam user", "iam_user", id, "User " + id + " not found"},
		{"iam group", "iam_group", id, "Group " + id + " not found"},
		{"iam service account", "iam_service_account", id, "ServiceAccount " + id + " not found"},
		{"iam access binding", "iam_access_binding", id, "AccessBinding " + id + " not found"},
		// compute — must equal services/compute/internal/repo/instance_repo.go.
		// Disk / Image / Snapshot are gone with the retired block-storage duplicate;
		// they now take the fallback, asserted with the other unmapped types below.
		{"compute instance", "compute_instance", id, "Instance " + id + " not found"},
		// registry — must equal what wrapPgErr composes in
		// services/registry/internal/repo/kacho/pg/errmap.go.
		{"registry registry", "registry_registry", id, "Registry " + id + " not found"},

		// Fallback (unmapped type / no concrete id) is the NEUTRAL "not found":
		// echoing the FGA object type would leak the internal type dictionary and
		// be distinguishable from every backend miss — the oracle this map closes.
		// See TestNotFoundMessage_UnmappedTypeIsNeutral for the dedicated lock.
		{"unmapped type fallback", "some_other_type", "", "not found"},
		{"unmapped type fallback with id", "some_other_type", id, "not found"},
		// Empty resource type → neutral fallback.
		{"empty type", "", "", "not found"},
		// Mapped type but no concrete id (wildcard/empty scope) → neutral fallback
		// rather than a malformed "Subnet  not found".
		{"mapped type wildcard id", "vpc_subnet", "*", "not found"},
		{"mapped type empty id", "vpc_subnet", "", "not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := notFoundMessage(permissionDeniedDescriptor{
				ResourceType: tc.resourceType,
				ResourceID:   tc.resourceID,
			})
			if got != tc.want {
				t.Fatalf("notFoundMessage(%q, %q) = %q; want %q",
					tc.resourceType, tc.resourceID, got, tc.want)
			}
		})
	}
}
