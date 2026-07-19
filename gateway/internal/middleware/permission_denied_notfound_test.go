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
		// nlb — must equal services/nlb/internal/repo/kacho/pg/load_balancer_repo.go.
		{"nlb load balancer", "lb_network_load_balancer", id, "NetworkLoadBalancer " + id + " not found"},

		// Non-vpc/nlb object types are NOT remapped — the fallback stays the bare
		// "<type> not found" so other services' contracts are unaffected (e.g.
		// registry-repository Newman asserts exactly "repository not found").
		{"registry repository fallback", "repository", id, "repository not found"},
		{"unmapped type fallback", "some_other_type", "", "some_other_type not found"},
		// Empty resource type → neutral fallback.
		{"empty type", "", "", "not found"},
		// Mapped type but no concrete id (wildcard/empty scope) → neutral fallback
		// rather than a malformed "Subnet  not found".
		{"mapped type wildcard id", "vpc_subnet", "*", "vpc_subnet not found"},
		{"mapped type empty id", "vpc_subnet", "", "vpc_subnet not found"},
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
