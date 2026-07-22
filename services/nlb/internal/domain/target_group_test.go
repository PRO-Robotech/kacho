// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

func validTG() domain.TargetGroup {
	return domain.TargetGroup{
		ID:                  "tgr-x",
		ProjectID:           "prj-x",
		RegionID:            "ru-central1",
		Name:                "backend-web",
		Description:         "",
		Labels:              domain.LabelsFromMap(map[string]string{"tier": "web"}),
		Targets:             nil,
		HealthCheck:         validHC(),
		DeregistrationDelay: domain.LbDuration(300 * time.Second),
		SlowStart:           domain.LbDuration(0),
		Status:              domain.TargetGroupStatusActive,
		Port:                8080,
	}
}

// TestTargetGroup_Validate_Port — NLB-1-35 (F6-co-req): TargetGroup.port is a
// required backend port, range 1..65535. 0 (unset) and >65535 are rejected.
func TestTargetGroup_Validate_Port(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		value   domain.LbPort
		wantErr bool
	}{
		{"1 OK (lower bound)", 1, false},
		{"8080 OK", 8080, false},
		{"65535 OK (upper bound)", 65535, false},
		{"0 rejected (required/unset)", 0, true},
		{"-1 rejected", -1, true},
		{"65536 rejected (over max)", 65536, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tg := validTG()
			tg.Port = tc.value
			err := tg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Port=%d: err=%v wantErr=%v", tc.value, err, tc.wantErr)
			}
		})
	}
}

func TestTargetGroup_Validate_HappyPath(t *testing.T) {
	t.Parallel()
	if err := validTG().Validate(); err != nil {
		t.Fatalf("happy-path: %v", err)
	}
}

func TestTargetGroup_Validate_DeregistrationDelay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		value   domain.LbDuration
		wantErr bool
	}{
		{"0 OK (lower bound)", domain.LbDuration(0), false},
		{"300 OK", domain.LbDuration(300 * time.Second), false},
		{"3600 OK (upper bound)", domain.LbDuration(3600 * time.Second), false},
		{"-1 rejected (TGR-007)", domain.LbDuration(-1 * time.Second), true},
		{"3601 rejected", domain.LbDuration(3601 * time.Second), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tg := validTG()
			tg.DeregistrationDelay = tc.value
			err := tg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestTargetGroup_Validate_SlowStart(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		value   domain.LbDuration
		wantErr bool
	}{
		{"0 OK (lower bound)", domain.LbDuration(0), false},
		{"900 OK (upper bound)", domain.LbDuration(900 * time.Second), false},
		{"-1 rejected (TGR-008)", domain.LbDuration(-1 * time.Second), true},
		{"901 rejected", domain.LbDuration(901 * time.Second), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tg := validTG()
			tg.SlowStart = tc.value
			err := tg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestTargetGroup_Validate_TargetsCardinality(t *testing.T) {
	t.Parallel()
	t.Run("100 targets OK (upper bound)", func(t *testing.T) {
		t.Parallel()
		tg := validTG()
		tg.Targets = make([]domain.Target, 100)
		for i := range tg.Targets {
			tg.Targets[i] = domain.Target{
				ExternalIP: &domain.TargetExternalIP{Address: "203.0.113.50"},
				Weight:     100,
			}
		}
		if err := tg.Validate(); err != nil {
			t.Fatalf("100 targets: %v", err)
		}
	})
	t.Run("101 targets rejected", func(t *testing.T) {
		t.Parallel()
		tg := validTG()
		tg.Targets = make([]domain.Target, 101)
		for i := range tg.Targets {
			tg.Targets[i] = domain.Target{
				ExternalIP: &domain.TargetExternalIP{Address: "203.0.113.50"},
				Weight:     100,
			}
		}
		if err := tg.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTargetGroup_Validate_PropagatesTargetError(t *testing.T) {
	t.Parallel()
	tg := validTG()
	tg.Targets = []domain.Target{
		// no identity → exactly-one-of error
		{Weight: 100},
	}
	if err := tg.Validate(); err == nil {
		t.Fatal("expected error from invalid target")
	}
}

func TestTargetGroup_Validate_PropagatesHealthCheckError(t *testing.T) {
	t.Parallel()
	tg := validTG()
	tg.HealthCheck.TCP = nil
	tg.HealthCheck.HTTP = nil
	if err := tg.Validate(); err == nil {
		t.Fatal("expected error from invalid HC")
	}
}

func TestTargetGroup_Validate_NameRequired(t *testing.T) {
	t.Parallel()
	tg := validTG()
	tg.Name = ""
	if err := tg.Validate(); err == nil {
		t.Fatal("expected error: empty name")
	}
}
