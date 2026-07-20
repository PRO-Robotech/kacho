// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import "testing"

// nlb1a_fga_rename_test.go — regression-lock for sub-phase NLB-1a: the listauthz
// ListObjects resource_type tokens sent to iam must be the renamed `nlb_*` object
// types, so per-object List filtering resolves against the same FGA type the
// interceptor and catalog key on. A drift here silently returns an empty (or
// unfiltered) page. Traceability: NLB-1a-03/05.
func TestNLB1a05_ListObjectsResourceTypesRenamed(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"load balancer", ResourceTypeLoadBalancer, "nlb_network_load_balancer"},
		{"listener", ResourceTypeListener, "nlb_listener"},
		{"target group", ResourceTypeTargetGroup, "nlb_target_group"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Fatalf("listauthz ResourceType %q = %q; want renamed %q (NLB-1a hard-rename)",
					c.name, c.got, c.want)
			}
		})
	}
}
