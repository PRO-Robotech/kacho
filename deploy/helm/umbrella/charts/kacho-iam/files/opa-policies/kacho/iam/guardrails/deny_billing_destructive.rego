# KAC-127 Phase 3 (acceptance §4.6 R1, design §4.1 #1)
# Deny: destructive ops on billing-* projects unless cluster-admin
#
# Rego v1 (P3-D10). This file is the dev-stand fallback copy of the policy;
# production binary embeds the same content via `//go:embed` from
# kacho-iam/policies/. The umbrella ships this so dev kind-stand can boot
# without kacho-iam bundle server (initial smoke before Phase 3 code is merged).

package kacho.iam.guardrails

import rego.v1

# Block destructive operations (projects.delete / accounts.delete) on billing-
# prefixed projects unless the principal carries the cluster_admin flag.
# Billing projects are special: their deletion cascades to cost-accounting
# ledger entries (Phase 9) and ALL associated invoices — irreversible.
deny contains msg if {
	input.action in {"projects.delete", "accounts.delete"}
	startswith(input.resource.id, "prj_billing_")
	not input.principal.cluster_admin
	msg := sprintf("destructive op on billing project requires cluster-admin (got %v)", [input.principal.id])
}
