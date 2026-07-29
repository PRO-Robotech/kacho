// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// stepup_validation.go — a declared authentication floor that no layer applies
// must stop the process.
//
// The permission catalog states, per RPC, how strongly a caller must have
// authenticated. Operators read that statement, the identity service mirrors it,
// and the cluster-internal arm decides on the value this process forwards. If
// this process applies none of it, every one of those readings is wrong and
// nothing about the running system says so — the control is present, wired,
// documented, and has not refused once.
//
// That is the shape security.md §8-9 names: a soft pass that cannot tell
// misconfiguration from failure. The remedy there is the remedy here — the
// enforcement is stated explicitly by every profile that stands the service up,
// and a production-class environment refuses to start when the statement and the
// process disagree.

import (
	"fmt"
	"strings"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// countDeclaredACRFloors counts the catalog rows that demand a positive assurance
// level. It ranks through the shared table rather than comparing strings, so a
// value the platform does not recognise counts as no demand here exactly as it
// does at the point of decision — a guard that counted it as a demand would
// refuse to start over a row that would never have refused a request.
func countDeclaredACRFloors(catalog *middleware.PermissionCatalog) int {
	if catalog == nil {
		return -1
	}
	n := 0
	for _, fqn := range catalog.FQNs() {
		e, ok := catalog.Lookup(fqn)
		if !ok {
			continue
		}
		if grpcsrv.ACRRank(e.RequiredACRMin) > 0 {
			n++
		}
	}
	return n
}

// StepUpConfig — what the composition root observed about the floor.
type StepUpConfig struct {
	// DeclaredFloors — how many catalog rows demand a positive assurance level.
	// NEGATIVE means the catalog could not be read: an unknown, which must never
	// read as zero (see validateProductionStepUpConfig).
	DeclaredFloors int
	// Enforced — whether the arm that applies the floor is actually mounted on
	// the authN layer every request passes through.
	Enforced bool
}

// stepUpEnforceKnob — the setting an operator turns to bring a stand up. Named in
// the refusal on purpose: a refusal that does not say what to set is a refusal
// nobody can act on, and this text is runtime diagnostics, not a public artefact.
const stepUpEnforceKnob = "KACHO_API_GATEWAY_AUTHN_ENFORCE_STEP_UP"

// validateProductionStepUpConfig refuses a production-class start when the
// catalog declares an authentication floor and nothing applies it.
//
// The subject of the guard is the CONTRADICTION, not the floor: a catalog that
// declares none has nothing to leave unapplied, and turning that into a start-up
// failure would make the guard fire on the absence of its own subject. An
// UNREADABLE catalog is a different matter — the floors it declares are unknown,
// and an unknown is not zero.
//
// dev / local / test pass through, matching the neighbouring guards: those are
// in-process fixtures, not stands.
func validateProductionStepUpConfig(env string, cfg StepUpConfig) error {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		return nil
	}

	if cfg.DeclaredFloors < 0 {
		return fmt.Errorf(
			"step-up config invalid in %q env: the permission catalog could not be read, so "+
				"whether any RPC demands a raised authentication level is unknown — an unread "+
				"catalog is not an empty one (refuse to start; set %s once the catalog loads)",
			env, stepUpEnforceKnob,
		)
	}
	if cfg.DeclaredFloors == 0 || cfg.Enforced {
		return nil
	}
	return fmt.Errorf(
		"step-up config invalid in %q env: %d catalog entr(ies) demand a raised authentication "+
			"level and no layer applies it, so the demand holds nowhere while every reader of the "+
			"catalog believes it does — set %s=true (refuse to start)",
		env, cfg.DeclaredFloors, stepUpEnforceKnob,
	)
}
