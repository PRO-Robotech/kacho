// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
)

// `Target` служит и запросом, и ответом. Состояние слива, добавленное в ответ,
// на путях СОЗДАНИЯ цели читателя не имеет — принять его молча значило бы
// пообещать возможность добавить цель уже сливающейся, которой нет. Поэтому там
// оно отвергается синхронно, с именем поля.
//
// На снятии те же поля игнорируются: сопоставление идёт по идентичности, и
// естественный поток «прочитал группу → передал цель на снятие» обязан
// продолжать работать. Асимметрия проверяется обоими тестами ниже, чтобы она
// оставалась решением, а не случайностью.

func TestCreate_TargetStatus_IsOutputOnly(t *testing.T) {
	repo := newFakeRepo()
	uc := mkUC(repo, newFakeOpsRepo())
	req := mkCreateReq("prj-acme", "ru-central1", "out-only-status")
	req.Targets = []*lbv1.Target{{
		Identity: &lbv1.Target_InstanceId{InstanceId: "epd-i1"},
		Weight:   100,
		Status:   lbv1.Target_DRAINING,
	}}
	_, err := uc.Execute(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, fieldViolationsText(err), "targets[0].status")
}

func TestCreate_TargetDrainStartedAt_IsOutputOnly(t *testing.T) {
	repo := newFakeRepo()
	uc := mkUC(repo, newFakeOpsRepo())
	req := mkCreateReq("prj-acme", "ru-central1", "out-only-drain-at")
	req.Targets = []*lbv1.Target{{
		Identity:       &lbv1.Target_InstanceId{InstanceId: "epd-i1"},
		Weight:         100,
		DrainStartedAt: timestamppb.Now(),
	}}
	_, err := uc.Execute(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, fieldViolationsText(err), "targets[0].drain_started_at")
}

func TestAdd_TargetStatus_IsOutputOnly(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "add-out-only")
	repo.seedTG(tg)
	uc := mkAddUC(repo, newFakeOpsRepo())

	_, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_InstanceId{InstanceId: "epd-i1"}, Weight: 100},
			{Identity: &lbv1.Target_NicId{NicId: "enp-nic1"}, Weight: 100, Status: lbv1.Target_ACTIVE},
		},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, fieldViolationsText(err), "targets[1].status",
		"отказ обязан назвать ту цель набора, из-за которой он произошёл")
}

// Парная половина: без output-only полей тот же запрос проходит — иначе
// отрицательные тесты выше зеленели бы и при полностью сломанном пути.
func TestAdd_WithoutOutputOnlyFields_Accepted(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "add-clean")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := mkAddUC(repo, opsRepo)

	op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_InstanceId{InstanceId: "epd-i1"}, Weight: 100},
		},
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nilf(t, final.Error, "op error: %v", final.Error)
}

// Снятие цели round-trip'ом собственного ответа сервиса обязано работать:
// состояние там не читается и наказывать за него нельзя.
func TestRemove_TargetStatus_IsIgnoredNotRefused(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "rm-roundtrip")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	addUC := mkAddUC(repo, opsRepo)
	addOp, err := addUC.Execute(context.Background(), &lbv1.AddTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets: []*lbv1.Target{
			{Identity: &lbv1.Target_InstanceId{InstanceId: "epd-i1"}, Weight: 100},
		},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, addOp.ID).Error)

	rmUC := NewRemoveTargetsUseCase(repo, opsRepo, nil)
	op, err := rmUC.Execute(context.Background(), &lbv1.RemoveTargetsRequest{
		TargetGroupId: string(tg.ID),
		Targets: []*lbv1.Target{
			{
				Identity:       &lbv1.Target_InstanceId{InstanceId: "epd-i1"},
				Weight:         100,
				Status:         lbv1.Target_ACTIVE,
				DrainStartedAt: timestamppb.Now(),
			},
		},
	})
	require.NoError(t, err, "снятие по собственному ответу сервиса обязано проходить")
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nilf(t, final.Error, "op error: %v", final.Error)
}
