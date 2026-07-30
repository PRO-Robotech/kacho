// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// The boot guard for the per-object filter and the RPCs that depend on it must not
// be able to disagree.
//
// Marking an RPC scope-filtered says "this RPC has no per-RPC Check; narrowing
// happens on the data". That is only true while the narrowing actually exists, so the
// production boot guard refuses to start on a configuration where it does not. The
// hazard is that the guard and the RPCs drift apart: the guard keeps passing while
// some scope-filtered RPC is served by nothing, or the guard is narrowed to fewer
// knobs than the RPCs need.
//
// So the linkage is asserted mechanically and in BOTH directions, on the object the
// two share — the filter the composition root builds:
//
//   - every configuration the guard REJECTS must leave the stream incapable;
//   - the configuration the guard ACCEPTS must leave it capable.
//
// A census assertion comes first, because "no scope-filtered RPC found" and "nothing
// was examined" must not look the same.

import (
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/PRO-Robotech/kacho/services/compute/internal/check"
	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

// scopeFilteredMethods — the surface this guard exists for, read from the map rather
// than restated here, so a new scope-filtered RPC is covered the moment it is added.
func scopeFilteredMethods() []string {
	var out []string
	for method, entry := range check.PermissionMap() {
		if entry.ScopeFiltered {
			out = append(out, method)
		}
	}
	return out
}

// TestScopeFilteredCensusIsNotEmpty — the guard's premise. If nothing in the map is
// scope-filtered, everything below asserts nothing, and it must say so out loud
// rather than pass quietly.
func TestScopeFilteredCensusIsNotEmpty(t *testing.T) {
	methods := scopeFilteredMethods()
	if len(methods) == 0 {
		t.Fatal("no scope-filtered RPC found in the permission map — every assertion in this file " +
			"would be vacuous; if that is now true by design, retire this gate deliberately")
	}
	// The outbox stream is the case that has no per-RPC Check to fall back on at all,
	// so its presence here is what places it under the production filter guard.
	const watch = "/kacho.cloud.compute.v1.InternalWatchService/Watch"
	found := false
	for _, m := range methods {
		if m == watch {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s must be scope-filtered: it is the RPC whose narrowing lives entirely in the "+
			"data path, so nothing else would put it under the boot guard. census: %v", watch, methods)
	}
}

// filterCapable reports what the composition root would hand the outbox stream for a
// given configuration: capable means the stream may open.
func filterCapable(t *testing.T, cfg config.Config) bool {
	t.Helper()
	var conn *grpc.ClientConn
	if cfg.AuthZIAMGRPCAddr != "" {
		// grpc.NewClient does not dial eagerly, so this costs nothing and no peer is
		// contacted; it stands in for the real authz connection.
		c, err := grpc.NewClient(cfg.AuthZIAMGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("stand-in authz conn: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		conn = c
	}
	vis := watchVisibility(buildListFilter(cfg, conn, discardLogger()))
	return vis != nil && vis.Narrows()
}

// TestBootGuardVerdictMatchesStreamCapability — the two directions.
func TestBootGuardVerdictMatchesStreamCapability(t *testing.T) {
	accepted := func() config.Config {
		cfg := allEdgesSecured()
		cfg.ListFilterFailOpen = false
		return cfg
	}

	t.Run("configuration the guard accepts leaves the stream capable", func(t *testing.T) {
		cfg := accepted()
		if err := requireListFilter(cfg); err != nil {
			t.Fatalf("premise broken: this configuration is meant to pass the guard, got %v", err)
		}
		if !filterCapable(t, cfg) {
			t.Fatal("the guard passed a configuration under which the stream cannot open — the guard " +
				"would then vouch for narrowing that is not there")
		}
	})

	// Each knob the guard names, injected one at a time. Rejecting must coincide with
	// the stream being unable to open; otherwise the guard is the only thing standing
	// between a served RPC and an unnarrowed answer, and it is not enough.
	for _, tc := range []struct {
		name    string
		breakIt func(*config.Config)
	}{
		{"master switch off", func(c *config.Config) { c.ListFilterEnabled = false }},
		{"authorize endpoint unset", func(c *config.Config) { c.AuthZIAMGRPCAddr = "" }},
		{"soft-pass on error", func(c *config.Config) { c.ListFilterFailOpen = true }},
	} {
		t.Run("configuration the guard rejects leaves the stream incapable: "+tc.name, func(t *testing.T) {
			cfg := accepted()
			tc.breakIt(&cfg)

			if err := requireListFilter(cfg); err == nil {
				t.Fatalf("the guard must refuse to start on %q: a scope-filtered RPC has no per-RPC "+
					"Check underneath, so an absent or non-narrowing filter is the whole authorisation", tc.name)
			}
			if filterCapable(t, cfg) {
				t.Fatalf("the guard rejects %q, yet the stream would still open — the refusal and the "+
					"capability disagree, and on a non-production stand only the capability applies", tc.name)
			}
		})
	}
}
