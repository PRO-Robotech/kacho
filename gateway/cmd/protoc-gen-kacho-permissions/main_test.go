// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"testing"

	authzv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/iam/authz/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// buildOpts builds a synthetic MethodOptions carrying the given authz
// annotations. Empty strings / nil scope are treated as "unset".
func buildOpts(t *testing.T, permission, relation, acrMin, scopeObj, scopeField string) *descriptorpb.MethodOptions {
	t.Helper()
	opts := &descriptorpb.MethodOptions{}
	if permission != "" {
		proto.SetExtension(opts, authzv1.E_Permission, permission)
	}
	if relation != "" {
		proto.SetExtension(opts, authzv1.E_RequiredRelation, relation)
	}
	if acrMin != "" {
		proto.SetExtension(opts, authzv1.E_RequiredAcrMin, acrMin)
	}
	if scopeObj != "" || scopeField != "" {
		proto.SetExtension(opts, authzv1.E_ScopeExtractor, &authzv1.ScopeExtractor{
			ObjectType:       scopeObj,
			FromRequestField: scopeField,
		})
	}
	return opts
}

func TestExtractEntry_FullyAnnotated(t *testing.T) {
	opts := buildOpts(t,
		"vpc.networks.create",
		"editor",
		"2",
		"project",
		"project_id",
	)
	entry, warn := extractEntry("kacho.cloud.vpc.v1.NetworkService/Create", opts)
	if warn != "" {
		t.Fatalf("unexpected warning: %s", warn)
	}
	if entry.FQN != "kacho.cloud.vpc.v1.NetworkService/Create" {
		t.Errorf("FQN mismatch: %s", entry.FQN)
	}
	if entry.Permission != "vpc.networks.create" {
		t.Errorf("Permission mismatch: %s", entry.Permission)
	}
	if entry.RequiredRelation != "editor" {
		t.Errorf("RequiredRelation mismatch: %s", entry.RequiredRelation)
	}
	if entry.ScopeExtractor.ObjectType != "project" {
		t.Errorf("scope.object_type mismatch: %s", entry.ScopeExtractor.ObjectType)
	}
	if entry.ScopeExtractor.FromRequestField != "project_id" {
		t.Errorf("scope.from_request_field mismatch: %s", entry.ScopeExtractor.FromRequestField)
	}
	if entry.RequiredAcrMin != "2" {
		t.Errorf("RequiredAcrMin mismatch: %s", entry.RequiredAcrMin)
	}
	if entry.HideExistence {
		t.Errorf("HideExistence must default to false when the option is unset")
	}
}

// TestExtractEntry_HideExistence pins that the generator maps the
// (kacho.iam.authz.v1.hide_existence) option into the catalog entry — the wiring
// that lets a verb-bearing mutation (registry Update/Delete) opt into gateway
// hide-existence on deny (opaque NotFound, no deny_reasons echo — security.md #6).
func TestExtractEntry_HideExistence(t *testing.T) {
	opts := buildOpts(t,
		"registry.registries.update",
		"v_update",
		"2",
		"registry_registry",
		"registry_id",
	)
	proto.SetExtension(opts, authzv1.E_HideExistence, true)

	entry, warn := extractEntry("kacho.cloud.registry.v1.RegistryService/Update", opts)
	if warn != "" {
		t.Fatalf("unexpected warning: %s", warn)
	}
	if !entry.HideExistence {
		t.Errorf("HideExistence must be true when (kacho.iam.authz.v1.hide_existence) = true")
	}
}

func TestExtractEntry_DefaultAcrMin(t *testing.T) {
	opts := buildOpts(t,
		"vpc.networks.create",
		"editor",
		"", // missing — default to "2"
		"project",
		"project_id",
	)
	entry, warn := extractEntry("x.Y/Z", opts)
	if warn != "" {
		t.Fatalf("unexpected warning: %s", warn)
	}
	if entry.RequiredAcrMin != DefaultRequiredAcrMin {
		t.Errorf("expected default acr_min %q, got %q", DefaultRequiredAcrMin, entry.RequiredAcrMin)
	}
}

func TestExtractEntry_MissingPermission(t *testing.T) {
	opts := buildOpts(t, "", "editor", "2", "project", "project_id")
	entry, warn := extractEntry("x.Y/Z", opts)
	if warn == "" {
		t.Fatal("expected warning, got none")
	}
	if entry.FQN != "x.Y/Z" {
		t.Errorf("entry should still be emitted with FQN; got %q", entry.FQN)
	}
}

func TestExtractEntry_MissingRelation(t *testing.T) {
	opts := buildOpts(t, "vpc.networks.create", "", "2", "project", "project_id")
	_, warn := extractEntry("x.Y/Z", opts)
	if warn == "" {
		t.Fatal("expected warning for missing required_relation")
	}
}

func TestExtractEntry_MissingScopeFields(t *testing.T) {
	cases := []struct {
		name, obj, field string
	}{
		{"no_object_type", "", "project_id"},
		{"no_from_request_field", "project", ""},
		{"none", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := buildOpts(t, "vpc.networks.create", "editor", "2", c.obj, c.field)
			_, warn := extractEntry("x.Y/Z", opts)
			if warn == "" {
				t.Fatalf("expected warning for case %s", c.name)
			}
		})
	}
}

func TestExtractEntry_ExemptSentinel(t *testing.T) {
	// Exempt RPC — only `permission` is required, others may be empty.
	opts := buildOpts(t, ExemptSentinel, "", "", "", "")
	entry, warn := extractEntry("op.OperationService/Get", opts)
	if warn != "" {
		t.Fatalf("exempt RPC should not warn, got: %s", warn)
	}
	if entry.Permission != ExemptSentinel {
		t.Errorf("expected exempt permission, got %q", entry.Permission)
	}
}

// TestExtractEntry_ExemptUnannotated_NoAcrDefault — SEC-ACR-15 / R3/B-1: the
// generator default "2" is injected ONLY for NON-exempt RPCs. An exempt RPC with
// no explicit required_acr_min gets an EMPTY acr (the exempt short-circuit
// early-returns BEFORE default-injection). This is the load-bearing fact behind
// the V3 fail-safe carve-out: a future exempt RPC is covered by neither the
// step-up backstop (fail-open on empty) nor authz-completeness (row exists, FGA
// scope-Check is skipped for <exempt>) — so it relies on authN + in-handler
// ReBAC + deliberate FGA-exempt posture, and adding one is high-scrutiny.
func TestExtractEntry_ExemptUnannotated_NoAcrDefault(t *testing.T) {
	opts := buildOpts(t, ExemptSentinel, "", "", "", "")
	entry, _ := extractEntry("kacho.cloud.iam.v1.AccessBindingService/Create", opts)
	if entry.RequiredAcrMin != "" {
		t.Errorf("exempt un-annotated RPC must keep EMPTY acr (no default injection), got %q", entry.RequiredAcrMin)
	}

	// Contrast: an EXPLICIT acr on an exempt RPC is preserved (orthogonal fields —
	// this is exactly the AccessBindingService/Create net-strengthening: exempt +
	// acr="2").
	optsExplicit := buildOpts(t, ExemptSentinel, "", "2", "", "")
	entry2, warn := extractEntry("kacho.cloud.iam.v1.AccessBindingService/Create", optsExplicit)
	if warn != "" {
		t.Fatalf("exempt RPC with explicit acr should not warn, got: %s", warn)
	}
	if entry2.Permission != ExemptSentinel {
		t.Errorf("permission must stay exempt (orthogonal to acr), got %q", entry2.Permission)
	}
	if entry2.RequiredAcrMin != "2" {
		t.Errorf("explicit acr=2 on an exempt RPC must be preserved (net-strengthening), got %q", entry2.RequiredAcrMin)
	}
}

func TestExtractEntry_NilOptions(t *testing.T) {
	// No annotations at all → row emitted, warning emitted.
	entry, warn := extractEntry("x.Y/Z", nil)
	if warn == "" {
		t.Fatal("expected warning for nil options")
	}
	if entry.FQN != "x.Y/Z" {
		t.Errorf("FQN missing from emitted row: %q", entry.FQN)
	}
}
