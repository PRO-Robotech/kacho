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
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestUpdate_Targets_ForbiddenInBodyWithoutMask — прикрытие маской было
// ЧАСТИЧНЫМ, и потому обманчивым.
//
// ПРЕДМЕТ (api-conventions.md, «Принято-и-проигнорировано — ЗАПРЕЩЕНО»).
// Контракт поля прямо говорит «Targets list replacement is rejected on this
// RPC», а отвергалось только ЯВНОЕ упоминание в маске. Пустая маска —
// full-object PATCH по конвенции, и тогда тот же список целей принимался и молча
// выбрасывался: вызывающий получал 200, а состав группы не менялся. Один и тот
// же параметр отвечал по-разному в зависимости от маски, и в тихой ветке —
// успехом. Тот же класс закрыт у compute для полей Update.
//
// Текст отказа — уже существующий фиксированный: он часть контракта и называет
// настоящий канал правки.
//
// Пара обязательна: без положительного контроля (правка без целей проходит)
// отрицание зеленело бы и на use-case, отвергающем любое обновление.
func TestUpdate_Targets_ForbiddenInBodyWithoutMask(t *testing.T) {
	const fixedText = "targets must be modified via AddTargets / RemoveTargets"

	body := []*lbv1.Target{{
		Identity: &lbv1.Target_InstanceId{InstanceId: "epd37vjqr6hpy1pctgjx"},
	}}

	t.Run("пустая маска, цели в теле — отказ фиксированным текстом", func(t *testing.T) {
		repo := newFakeRepo()
		tg := makeTG("prj-acme", "tg-body-nomask")
		repo.seedTG(tg)
		uc := NewUpdateTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

		_, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
			TargetGroupId: string(tg.ID),
			Targets:       body,
		})

		require.Error(t, err, "full-object PATCH обязан отвергать цели, а не выбрасывать их")
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, status.Convert(err).Message(), fixedText)
	})

	t.Run("маска без targets, цели в теле — тот же отказ", func(t *testing.T) {
		repo := newFakeRepo()
		tg := newFakeRepoTG(t, repo, "tg-body-othermask")
		uc := NewUpdateTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

		_, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
			TargetGroupId: string(tg.ID),
			UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"name"}},
			Name:          "tg-renamed",
			Targets:       body,
		})

		require.Error(t, err, "маска, не называющая targets, не делает их применёнными")
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, status.Convert(err).Message(), fixedText)
	})

	t.Run("та же правка без целей проходит (положительный контроль)", func(t *testing.T) {
		repo := newFakeRepo()
		tg := newFakeRepoTG(t, repo, "tg-body-clean")
		opsRepo := newFakeOpsRepo()
		uc := NewUpdateTargetGroupUseCase(repo, opsRepo, nil)

		op, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
			TargetGroupId: string(tg.ID),
			UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"name"}},
			Name:          "tg-renamed",
		})

		require.NoError(t, err, "правка без целей обязана проходить")
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
	})
}

// newFakeRepoTG — засеянная группа с указанным именем.
func newFakeRepoTG(t *testing.T, repo *fakeRepo, name string) *kachorepo.TargetGroupRecord {
	t.Helper()
	tg := makeTG("prj-acme", name)
	repo.seedTG(tg)
	return tg
}
