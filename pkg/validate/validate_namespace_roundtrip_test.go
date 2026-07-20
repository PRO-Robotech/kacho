// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package validate

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// TestResourceID_NamespaceGeneratorRoundTrip_REG_1_31 locks generator↔router
// coherence for the redesigned Namespace resource (id-prefix `ns-`, F1):
// every id the REG-1 generator (ids.NewHyphenID) emits MUST pass the format-check
// (validate.ResourceID) that the handler runs as its first statement — otherwise a
// freshly created namespace would fail its own GetNamespace with "invalid namespace
// id". This is the regression that the reg/rop drift class produced (constant added
// but router disagreed). Malformed input still rejects with the flat contract tone.
func TestResourceID_NamespaceGeneratorRoundTrip_REG_1_31(t *testing.T) {
	// Round-trip: 1000 generated ids all classify as well-formed namespace ids.
	for i := 0; i < 1000; i++ {
		id := ids.NewHyphenID(ids.PrefixNamespace)
		if err := ResourceID("namespace", ids.PrefixNamespace, id); err != nil {
			t.Fatalf("ResourceID(namespace, _, %q) = %v, want nil (generator output must be router-valid)", id, err)
		}
		if !strings.HasPrefix(id, "ns-") {
			t.Fatalf("generated namespace id %q must carry the ns- hyphen prefix", id)
		}
	}

	// Malformed namespace id → InvalidArgument with the "invalid <res> id" tone
	// (REG-1-31: first-statement reject). Note ResourceID is family-agnostic
	// (`_ = expectedPrefix`): it rejects ids whose prefix is in NEITHER the hyphen
	// canon nor the legacy concat canon. (`reg-xxx` still passes via the legacy
	// `reg` fallback — that is caught later by repo.Get→NotFound, not by format.)
	for _, bad := range []string{"NS!!!", "namespace", "n-abc", "xyz123"} {
		err := ResourceID("namespace", ids.PrefixNamespace, bad)
		if err == nil {
			t.Fatalf("ResourceID(namespace, _, %q) = nil, want InvalidArgument", bad)
		}
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Fatalf("ResourceID(namespace, _, %q) code = %v, want InvalidArgument", bad, got)
		}
		if msg := status.Convert(err).Message(); !strings.Contains(msg, "invalid namespace id") {
			t.Errorf("ResourceID(namespace, _, %q) message = %q, want contains %q", bad, msg, "invalid namespace id")
		}
	}
}
