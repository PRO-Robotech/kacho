// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authz

// hide_existence.go — the NOT_FOUND text used when a read is refused on an object
// the caller may not know about.
//
// When the check reports that the object exists but is not the caller's, the
// interceptor answers NOT_FOUND instead of PERMISSION_DENIED, so that "not yours"
// cannot be told from "not there". That protection is only as good as the TEXT:
// if the refusal reads differently from the owning service's genuine miss, the
// caller separates the two cases by reading the message, and the whole point of
// answering NOT_FOUND is lost.
//
// Each format is therefore copied VERBATIM from the owning service's repo-layer
// NotFound — the wire text a real miss produces (every service's error mapper
// strips only its sentinel prefix and passes the message through). The single %s
// is the caller-supplied id, echoed back: the caller already knows it.
//
// The same table exists on the api-gateway, which refuses the same class of read
// one hop earlier; both must agree with the owning service, and the drift guard
// in hide_existence_parity_test.go fails when they stop agreeing.

import "fmt"

// hideExistenceNotFoundFormats maps an authorization object type to the Kachō
// contract-tone NotFound of the service that owns it.
//
// Sources (repo layer of the owning service):
//
//	iam       services/iam/internal/repo/kacho/pg/{account,project,user_pool,group,service_account,access_binding}_repo.go
//	compute   services/compute/internal/repo/{disk,image,instance,snapshot}_repo.go
//	vpc       services/vpc/internal/repo/kacho/pg/*.go (+ repo/helpers/sg.go)
//	nlb       services/nlb/internal/repo/kacho/pg/{load_balancer,listener,target_group}_repo.go
//	registry  services/registry/internal/repo/kacho/pg/registry.go
//
// A new object-scoped resource adds its line here with the text taken from its
// repo layer — never invented, or the refusal stops matching the miss.
var hideExistenceNotFoundFormats = map[string]string{
	// iam
	"account":             "Account %s not found",
	"project":             "Project %s not found",
	"iam_user":            "User %s not found",
	"iam_group":           "Group %s not found",
	"iam_service_account": "ServiceAccount %s not found",
	"iam_access_binding":  "AccessBinding %s not found",
	// compute
	"compute_disk":     "Disk %s not found",
	"compute_image":    "Image %s not found",
	"compute_instance": "Instance %s not found",
	"compute_snapshot": "Snapshot %s not found",
	// vpc
	"vpc_network":     "Network %s not found",
	"vpc_subnet":      "Subnet %s not found",
	"vpc_address":     "Address %s not found",
	"vpc_route_table": "Route table %s not found",
	// NOTE: the vpc security-group text carries a debug rendering of the id
	// ("SecurityGroup.Id(value=%s)") rather than the plain contract tone. It is
	// reproduced verbatim ON PURPOSE — byte-identity with the backend is what
	// closes the oracle — and may only change together with
	// services/vpc/internal/repo/helpers/sg.go.
	"vpc_security_group":    "Security group SecurityGroup.Id(value=%s) not found",
	"vpc_gateway":           "Gateway %s not found",
	"vpc_network_interface": "Network interface %s not found",
	// nlb
	"nlb_network_load_balancer": "NetworkLoadBalancer %s not found",
	"nlb_listener":              "Listener %s not found",
	"nlb_target_group":          "TargetGroup %s not found",
	// registry
	"registry_registry": "Registry %s not found",
}

// hideExistenceMessage is the message returned when a read is refused on an
// object the caller may not know exists.
//
// For a known object type with a concrete id it is the owning service's own miss,
// byte for byte. Otherwise it is the neutral "not found".
//
// The fallback is honestly NOT byte-identical to a real miss — a real miss names
// the resource and echoes the id. It is the least-informative remainder for the
// two cases where identity is impossible: an object type with no owning-service
// text to copy, and an absent/wildcard id (there is nothing to echo, and
// "<Resource>  not found" would be its own distinguishable, malformed string). It
// deliberately does NOT echo the authorization object type: that token appears
// nowhere on the public surface, so emitting it would both leak the internal
// dictionary and — being unlike any backend text — be the very tell this function
// exists to remove.
func hideExistenceMessage(objectType, objectID string) string {
	if f, ok := hideExistenceNotFoundFormats[objectType]; ok && objectID != "" && objectID != "*" {
		return fmt.Sprintf(f, objectID)
	}
	return "not found"
}
