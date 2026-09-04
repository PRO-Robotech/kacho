// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

func validListener() domain.Listener {
	return domain.Listener{
		ID:             "lst-x",
		ProjectID:      "prj-x",
		LoadBalancerID: "nlb-x",
		RegionID:       "ru-central1",
		Name:           "http",
		Description:    "",
		Labels:         domain.LbLabels{},
		Protocol:       domain.ProtoTCP,
		Port:           80,
		Status:         domain.ListenerStatusCreating,
	}
}

func TestListener_Validate_HappyPath(t *testing.T) {
	t.Parallel()
	if err := validListener().Validate(); err != nil {
		t.Fatalf("happy-path: %v", err)
	}
}

func TestListener_Validate_PortBoundary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		port    domain.LbPort
		wantErr bool
	}{
		{"port=1 OK", 1, false},
		{"port=65535 OK", 65535, false},
		{"port=0 rejected (LST-008)", 0, true},
		{"port=65536 rejected", 65536, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := validListener()
			l.Port = tc.port
			err := l.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestListener_Validate_ProtocolMustBeL4(t *testing.T) {
	t.Parallel()
	l := validListener()
	l.Protocol = "HTTP"
	if err := l.Validate(); err == nil {
		t.Fatal("expected error: HTTP not allowed at L4 listener (LST-009)")
	}
}

// Equal остаётся option-aware по единственному оставшемуся option-полю листенера
// (DefaultTargetGroupID): some-vs-none и different-some должны различаться.
func TestListener_Equal_OptionFieldsAware(t *testing.T) {
	t.Parallel()
	a := validListener()
	b := validListener()
	a.DefaultTargetGroupID = option.MustNewOption[domain.ResourceID]("tgr-1")
	b.DefaultTargetGroupID = option.MustNewOption[domain.ResourceID]("tgr-1")
	if !a.Equal(b) {
		t.Fatal("equal DefaultTargetGroupID should compare equal")
	}
	b.DefaultTargetGroupID = option.MustNewOption[domain.ResourceID]("tgr-2")
	if a.Equal(b) {
		t.Fatal("differing DefaultTargetGroupID must compare unequal")
	}
	c := validListener()
	d := validListener()
	c.DefaultTargetGroupID = option.MustNewOption[domain.ResourceID]("tgr-3")
	// d has none (some vs none)
	if c.Equal(d) {
		t.Fatal("some-vs-none must compare unequal")
	}
}
