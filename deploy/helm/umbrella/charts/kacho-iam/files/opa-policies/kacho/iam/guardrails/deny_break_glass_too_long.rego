# KAC-127 Phase 3 (acceptance §4.6 R4, design §4.1 #4)
# Deny: break-glass grant duration > 2h (7200s).
#
# Rationale: emergency_admin grants bypass tenant boundaries and are the
# highest-impact privilege in the system. Limiting to 2h forces operators
# to re-justify (and re-trigger 2-person approval, Phase 7) for sustained
# emergencies, preventing forgotten/silently-renewed god-mode access.
#
# 2h hardcoded — change requires a model_v3 / policy_v2 deploy + Decision Log
# update. Phase 7 may parameterize per-org with strict caps.

package kacho.iam.guardrails

import rego.v1

deny contains msg if {
	input.action == "cluster.break_glass.grant"
	input.duration_seconds > 7200
	msg := "break-glass grant cannot exceed 2 hours"
}
