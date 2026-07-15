# KAC-127 Phase 3 (acceptance §4.6 R5, design §4.1 #5)
# Deny: cross-tenant resource access (User from account A touching resource in account B).
#
# Rationale: belt-and-suspenders on top of OpenFGA's tuple-based isolation.
# Even if a misconfigured AccessBinding mistakenly granted an `editor`-style
# role to a cross-tenant user, OPA blocks the request at gateway-time.
# cluster_admin (permanent OR emergency) is exempt — they have legitimate
# need for cross-tenant access (see ClusterAdminGrant / BreakGlass workflows).
#
# Helper `principal_in_resource_account(principal, resource)` matches
# principal.account_id against resource.account_id. Both fields must be
# populated by api-gateway during Principal extraction (Phase 2).

package kacho.iam.guardrails

import rego.v1

deny contains msg if {
	input.principal.type == "user"
	not principal_in_resource_account(input.principal, input.target.resource)
	not input.principal.cluster_admin
	msg := sprintf("cross-tenant access blocked: %v -> %v", [
		input.principal.account_id,
		input.target.resource.account_id,
	])
}

# principal_in_resource_account(principal, resource)
# Equality on the account_id boundary. Resource without account_id (cluster-
# scoped resources like AddressPool) is allowed for any principal — the
# higher-level FGA Check already enforced cluster-admin requirement for those.
principal_in_resource_account(principal, resource) if {
	resource.account_id == ""
}

principal_in_resource_account(principal, resource) if {
	resource.account_id != ""
	principal.account_id == resource.account_id
}
