// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// stepup_validation_test.go — a declared authentication floor that nothing
// applies must stop the process, not run quietly.
//
// The catalog states, per RPC, how strongly the caller must have authenticated.
// That statement is read by operators, mirrored into the identity service and
// documented as enforced. If the process it runs in applies none of it, the
// statement is false everywhere it is read, and nothing about the running system
// says so. Refusing to start is the only outcome that cannot be missed.

import (
	"strings"
	"testing"
)

func TestStepUpGuard_ProductionRefusesWhenNothingAppliesTheFloor(t *testing.T) {
	err := validateProductionStepUpConfig("production", StepUpConfig{
		DeclaredFloors: 22,
		Enforced:       false,
	})
	if err == nil {
		t.Fatal("a production-class env must refuse to start when the catalog declares a floor " +
			"and no layer applies it")
	}
	// The operator has to be able to act on this without reading the source.
	for _, want := range []string{"KACHO_API_GATEWAY_AUTHN_ENFORCE_STEP_UP", "refuse to start"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal must name %q so the stand can be brought up; got: %v", want, err)
		}
	}
}

func TestStepUpGuard_ProductionAcceptsWhenTheFloorIsApplied(t *testing.T) {
	if err := validateProductionStepUpConfig("production", StepUpConfig{
		DeclaredFloors: 22,
		Enforced:       true,
	}); err != nil {
		t.Fatalf("an applied floor must start: %v", err)
	}
}

// The guard's subject is the CONTRADICTION between a declaration and its
// application. A catalog that declares no floor has nothing to contradict, so it
// must not be turned into a start-up failure — that would make the guard fire on
// its own absence of subject.
func TestStepUpGuard_NoDeclaredFloorIsNotAContradiction(t *testing.T) {
	if err := validateProductionStepUpConfig("production", StepUpConfig{
		DeclaredFloors: 0,
		Enforced:       false,
	}); err != nil {
		t.Fatalf("nothing is declared, so nothing is unapplied: %v", err)
	}
}

// A catalog that never loaded is not "no floors": it is an unknown, and the
// unknown must not read as permission.
func TestStepUpGuard_UnreadableCatalogRefuses(t *testing.T) {
	err := validateProductionStepUpConfig("production", StepUpConfig{
		DeclaredFloors: -1,
		Enforced:       true,
	})
	if err == nil {
		t.Fatal("an unread catalog must refuse: the floors it declares are unknown, and " +
			"unknown is not zero")
	}
}

// In-process fixtures and the local unit environment keep the pass-through the
// neighbouring guards use, so the same table decides for all of them.
func TestStepUpGuard_NonProductionEnvsPass(t *testing.T) {
	for _, env := range []string{"dev", "local", "test", "DEV", " test "} {
		if err := validateProductionStepUpConfig(env, StepUpConfig{
			DeclaredFloors: 22,
			Enforced:       false,
		}); err != nil {
			t.Errorf("env %q must not be gated: %v", env, err)
		}
	}
}
