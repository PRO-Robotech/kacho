// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
)

// Отказ на правку области владения утверждается ПАРОЙ — код и ДОСЛОВНЫЙ текст
// (#1671). Один код тут не годится by construction: расхождение тона живёт
// внутри `INVALID_ARGUMENT`, и проба, проверяющая только код, остаётся зелёной
// при любом тексте — так расхождение и прожило до сих пор.
//
// Сверка `Equal` на полном сообщении, а не `Contains`: включение проходит и на
// тексте, к которому что-то ДОПИСАЛИ, поэтому смену тона оно тоже не различает.
func TestUpdateLoadBalancer_ProjectScopeRefusal_NamesTheNextStep(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	uc := NewUpdateLoadBalancerUseCase(repo, newFakeOpsRepo(), &fakeZoneClient{}, slog.Default())

	_, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"project_id"}},
	})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t,
		"project_id is immutable after NetworkLoadBalancer.Create; use NetworkLoadBalancerService.Move",
		status.Convert(err).Message())
}

// Положительный контроль к предыдущей. Без него отрицание зеленело бы на
// use-case, отвергающем ЛЮБУЮ маску: «отвергнуто» неотличимо от «сломано всё».
func TestUpdateLoadBalancer_MutableMaskStillPasses(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	opsRepo := newFakeOpsRepo()
	uc := NewUpdateLoadBalancerUseCase(repo, opsRepo, &fakeZoneClient{}, slog.Default())

	op, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		Name:                  "edge-v2",
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	})

	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
}
