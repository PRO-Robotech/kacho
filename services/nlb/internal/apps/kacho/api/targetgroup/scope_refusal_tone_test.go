// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
)

// Пара «код + дословный текст» отказа на правку области владения (#1671).
// `TargetGroupService.Move` существует и доступен получившему отказ, поэтому
// молчание об этом глаголе стоило клиенту похода в документацию.
func TestUpdateTargetGroup_ProjectScopeRefusal_NamesTheNextStep(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "imm-proj-tone")
	repo.seedTG(tg)
	uc := NewUpdateTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

	_, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"project_id"}},
	})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t,
		"project_id is immutable after TargetGroup.Create; use TargetGroupService.Move",
		status.Convert(err).Message())
}

// Положительный контроль: законная маска проходит. Соседнее неизменяемое поле
// при этом отвергается КАНОНИЧЕСКИМ текстом без следующего шага — глагола
// переноса региона не существует, и обещать его было бы объявлением
// неисполнимой возможности.
func TestUpdateTargetGroup_MutableMaskStillPasses_RegionKeepsPlainRefusal(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "tone-control")
	repo.seedTG(tg)
	uc := NewUpdateTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

	_, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		Name:          "tone-control-v2",
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	})
	require.NoError(t, err)

	_, err = uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"region_id"}},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "region_id is immutable after TargetGroup.Create",
		status.Convert(err).Message())
}
