// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import "testing"

// The boot guard and the code that decides whether to mount the revocation
// check must read the SAME value.
//
// They did not. The guard asks `strings.TrimSpace(url) == ""`, so a value of
// spaces is "unset" to it. The composition root asks `url != ""`, so the same
// value is "set" — it mounts the check and hands the blank address to the
// cache, whose own emptiness check is untrimmed too and lets it through. Every
// request then fails the same way forever.
//
// A value of spaces is not exotic: a chart knob templated from something absent
// renders exactly that, and YAML quotes it without complaint. What made this
// worth fixing is not the odds of it happening — the deployment classes that
// run the guard are refused at boot either way — but that two places asked the
// same question with two different predicates. That is how a guard and the
// thing it guards drift apart, and it is the reason the empty-allow-list defect
// survived in four services: the check counted a raw slice while the transport
// counted entries.
//
// Normalising once, where the value is read, leaves one predicate for everyone.
func TestResolvedRevocationEndpoints_BlankIsUnset(t *testing.T) {
	for _, blank := range []string{"", " ", "   ", "\t", "\n", " \t\n "} {
		cfg := Config{HydraIntrospectionURL: blank, HydraAdminURL: blank}

		if got := cfg.ResolvedHydraIntrospectionURL(); got != "" {
			t.Errorf("introspection URL %q: got %q, want empty — a blank address is unset, "+
				"and the guard already reads it that way", blank, got)
		}
		if got := cfg.ResolvedHydraAdminURL(); got != "" {
			t.Errorf("admin URL %q: got %q, want empty", blank, got)
		}
	}
}

// Surrounding whitespace must not survive into the address either: the guard
// parses the trimmed form and passes, while the request would be built from the
// untrimmed one and fail — the same divergence, one layer down.
func TestResolvedRevocationEndpoints_TrimsSurroundingSpace(t *testing.T) {
	const want = "http://provider-admin:4445/admin/oauth2/introspect"
	cfg := Config{HydraIntrospectionURL: "  " + want + "\n", HydraAdminURL: " http://provider-admin:4445 "}

	if got := cfg.ResolvedHydraIntrospectionURL(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := cfg.ResolvedHydraAdminURL(); got != "http://provider-admin:4445" {
		t.Errorf("got %q, want trimmed", got)
	}
}
