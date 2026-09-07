# KAC-127 Phase 3 (acceptance §4.6 R2, design §4.1 #2)
# Deny: ServiceAccount cannot grant a role to a User (escalation prevention)
#
# Rationale: SAs are non-interactive, often have broad infra-level grants
# (e.g., terraform-provisioner SA has account.admin). If an SA could grant
# a role to a User, a compromised SA = grant any privilege to attacker-controlled
# user. Hard block — even cluster-admin SA cannot escalate user privileges
# (must be done through a human-initiated AccessBinding mutation).

package kacho.iam.guardrails

import rego.v1

deny contains msg if {
	input.action == "access_bindings.upsert"
	input.principal.type == "service_account"
	input.target.subject_type == "user"
	msg := "service accounts may not grant roles to users"
}
