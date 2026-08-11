// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Fail-closed posture of `authorizeDestination` — the decision that keeps a
// caller from re-parenting its TargetGroup into somebody else's project.
//
// It used to answer "authorized" when the decision could not be made: no Check
// client wired, or a caller that cannot be named as an FGA subject. An absent
// decider is not a permission (`security.md` §"Пустой субъект отсекается
// БЕЗУСЛОВНО").
//
// Each case asserts the OUTCOME — the refusal AND that the TargetGroup stayed in
// its source project. Positive control: TestMove_AllowsAuthorizedDestination
// (move_test.go) — a named, authorized caller still moves the TG.

func ctxSystemPrincipal() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "system", ID: "bootstrap"})
}

func ctxAnonymousPrincipal() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: domain.AnonymousPrincipalID})
}

func TestMoveTargetGroup_NoCheckClient_Refused(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-src", "movable")
	repo.seedTG(tg)
	uc := NewMoveTargetGroupUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, nil, nil)

	_, err := uc.Execute(ctxWithUser("usr_caller"), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-victim",
	})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, domain.ProjectID("prj-src"), repo.tgs[string(tg.ID)].ProjectID,
		"the TG must not be re-parented when the destination decision cannot be made")
}

func TestMoveTargetGroup_UnnameableCaller_Refused(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"no principal": context.Background(),
		"system type":  ctxSystemPrincipal(),
		"anonymous id": ctxAnonymousPrincipal(),
	} {
		t.Run(name, func(t *testing.T) {
			repo := newFakeRepo()
			tg := makeTG("prj-src", "movable")
			repo.seedTG(tg)
			chk := &fakeCheckClient{allowed: true} // would say yes if asked
			uc := NewMoveTargetGroupUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, chk, nil)

			_, err := uc.Execute(ctx, &lbv1.MoveTargetGroupRequest{
				TargetGroupId:        string(tg.ID),
				DestinationProjectId: "prj-victim",
			})
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			require.Equal(t, domain.ProjectID("prj-src"), repo.tgs[string(tg.ID)].ProjectID,
				"the TG must not be re-parented for a caller we cannot name")
			require.Zero(t, chk.calls,
				"an unnameable caller must not be asked about under an invented subject")
		})
	}
}
