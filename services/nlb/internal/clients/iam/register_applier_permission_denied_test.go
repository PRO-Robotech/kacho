// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package iam_test

// register_applier_permission_denied_test.go — a denial on authorization grounds
// from kaname is TERMINAL for the register drainer.
//
// This is the service the class was first observed in: a register queue in which no
// row had ever been delivered — every one refused on authorization grounds. The
// reason it stayed invisible is that a refusal was classified transient, and a
// transient row is deliberately kept one attempt below the poison gate, so it never
// leaves the claim query's blocking set. Partitions are keyed per resource, so the
// registration stuck at the head blocked the UNregistration queued behind it: the
// resource was deleted while its grant stayed materialized.
//
// An identical retry cannot change an authorization decision — it is a function of
// (caller, relation, object), none of which a retry alters. Poisoning is the safe
// direction: the write never happened, the partition unblocks, and the periodic
// redrive backstop replays the poisoned row.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// denyingRegisterClient refuses every call on authorization grounds.
type denyingRegisterClient struct{}

func (denyingRegisterClient) RegisterResource(
	context.Context, *iampb.RegisterResourceRequest, ...grpc.CallOption,
) (*iampb.RegisterResourceResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "relation not accepted by iam register")
}

func (denyingRegisterClient) UnregisterResource(
	context.Context, *iampb.UnregisterResourceRequest, ...grpc.CallOption,
) (*iampb.UnregisterResourceResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "relation not accepted by iam register")
}

func TestRegisterApplier_PermissionDenied_IsPermanent(t *testing.T) {
	apply := iam.NewRegisterApplier(denyingRegisterClient{})

	intent := domain.FGARegisterIntent{
		Kind:            "TargetGroup",
		ResourceID:      "tgr-aaaaaaaaaaaaaaaaa",
		ParentProjectID: "prj-prod000000000000",
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeTargetGroup, "tgr-aaaaaaaaaaaaaaaaa", "prj-prod000000000000"),
		},
	}

	for _, event := range []string{domain.FGAEventRegister, domain.FGAEventUnregister} {
		t.Run(event, func(t *testing.T) {
			err := apply(context.Background(), event, intent)
			require.Error(t, err)
			assert.Truef(t, errors.Is(err, drainer.ErrPermanent),
				"%s refused on authorization grounds must poison: an identical retry cannot succeed, and a head "+
					"that never poisons wedges its partition — this is exactly how 198 rows were never delivered", event)
		})
	}
}
