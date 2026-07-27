// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package tenant

import (
	"testing"
)

// TestIsAnonymous — anonymous = ни Admin, ни ProjectIDs. На этот предикат
// опираются production-mode AuthN-guard и admin-gate в `internal/handler`;
// авторизация как таковая живёт в permission-модели (per-RPC Check), не здесь.
func TestIsAnonymous(t *testing.T) {
	cases := []struct {
		name string
		tc   TenantCtx
		want bool
	}{
		{"empty", TenantCtx{}, true},
		{"project", TenantCtx{ProjectIDs: map[string]struct{}{"f1": {}}}, false},
		{"admin", TenantCtx{Admin: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.tc.IsAnonymous(); got != c.want {
				t.Fatalf("IsAnonymous=%v, want %v (case %s)", got, c.want, c.name)
			}
		})
	}
}
