// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
)

// `HealthCheck` служит и запросом, и ответом, поэтому выходное производное поле
// `effective_port` доезжает до вызывающего в теле ЗАПРОСА. Контракт объявляет
// его выходным (`health_check.proto`), а величина выводится сервером: порт
// пробы, если задан, иначе порт группы.
//
// Принять такое поле молча значит пообещать, что порт пробы можно назначить
// мимо самой пробы, — возможности, которой нет: путь записи величину не читает
// вовсе, а ответ пересчитывает её сам. Поэтому она отвергается синхронно и с
// именем поля — тем же исходом, что уже выбран для соседей той же природы
// (`targets[i].status`, `targets[i].drain_started_at`, см.
// `target_output_only_fields_test.go`).
//
// ГРАНИЦА, ВЗЯТАЯ НАМЕРЕННО. Отказ живёт в конвертере, поэтому срабатывает
// везде, где тело `health_check` вообще ЧИТАЕТСЯ: на создании всегда, на правке
// — при пустой маске, при `health_check` целиком и при любом
// `health_check.<под-поле>`. Маска, не называющая `health_check` вовсе, под
// отказ НЕ подпадает, и это не пропуск: по конвенции обновления маска и есть
// заявление о том, что применять, — там не читается ни одно поле проверки, а не
// одно только это. Полосу самой маски закрывает известный набор под-полей:
// `health_check.effective_port` отвергается как неизвестный путь маски ещё до
// конвертера.

// effectivePortReq — минимально-законный запрос создания с назначенным
// выходным полем. Отдельный конструктор, чтобы величина стояла ровно в одном
// месте и её нельзя было потерять правкой соседнего теста.
func effectivePortReq(name string, effectivePort int32) *lbv1.CreateTargetGroupRequest {
	req := mkCreateReq("prj-acme", "ru-central1", name)
	req.HealthCheck.EffectivePort = effectivePort
	return req
}

func TestCreate_HealthCheckEffectivePort_IsOutputOnly(t *testing.T) {
	uc := mkUC(newFakeRepo(), newFakeOpsRepo())

	_, err := uc.Execute(context.Background(), effectivePortReq("hc-eff-port", 9999))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, fieldViolationsText(err), "health_check.effective_port",
		"отказ обязан назвать поле, из-за которого он произошёл")
}

// Совпадение присланной величины с выводимой отказа НЕ отменяет: принимать
// «угадавшего» значило бы сделать исход функцией того, угадал ли вызывающий, и
// увести поле обратно в разряд принимаемых на следующей же смене вывода.
// Тот же выбор уже сделан для производного режима балансировщика
// (`placement_test.go`, «type set rejected (even consistent with placement)»).
func TestCreate_HealthCheckEffectivePort_RejectedEvenWhenItMatchesTheDerivedValue(t *testing.T) {
	uc := mkUC(newFakeRepo(), newFakeOpsRepo())

	// 8080 — ровно то, что вывел бы сервер: порт пробы HTTP в mkCreateReq.
	_, err := uc.Execute(context.Background(), effectivePortReq("hc-eff-match", 8080))
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, fieldViolationsText(err), "health_check.effective_port")
}

// Парная половина: без выходного поля тот же запрос проходит — иначе
// отрицания выше зеленели бы и на полностью сломанном пути создания.
func TestCreate_HealthCheckWithoutEffectivePort_Accepted(t *testing.T) {
	opsRepo := newFakeOpsRepo()
	uc := mkUC(newFakeRepo(), opsRepo)

	op, err := uc.Execute(context.Background(), effectivePortReq("hc-eff-clean", 0))
	require.NoErrorf(t, err, "details=%s", fieldViolationsText(err))
	require.Nilf(t, awaitOpDone(t, opsRepo, op.ID).Error, "операция обязана завершиться успехом")
}

func TestUpdate_HealthCheckEffectivePort_IsOutputOnly(t *testing.T) {
	repo := newFakeRepo()
	tg := makeHTTPProbeTG(t, repo, "hc-eff-upd")
	uc := NewUpdateTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

	_, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"health_check"}},
		HealthCheck: &lbv1.HealthCheck{
			Interval:           durationpb.New(2 * time.Second),
			Timeout:            durationpb.New(1 * time.Second),
			UnhealthyThreshold: 2,
			HealthyThreshold:   2,
			Options: &lbv1.HealthCheck_Http{
				Http: &lbv1.HealthCheck_HttpOptions{Port: 8080, Path: "/healthz"},
			},
			EffectivePort: 9999,
		},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, fieldViolationsText(err), "health_check.effective_port")
}

// Точечная маска читает тело проверки так же, как маска целиком, — значит и
// отказ обязан быть тем же. Без этого случая отказ закрывал бы одну форму
// правки из двух.
func TestUpdate_HealthCheckDottedMask_EffectivePortInBody_IsOutputOnly(t *testing.T) {
	repo := newFakeRepo()
	tg := makeHTTPProbeTG(t, repo, "hc-eff-upd-dotted")
	uc := NewUpdateTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

	_, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"health_check.interval"}},
		HealthCheck: &lbv1.HealthCheck{
			Interval:      durationpb.New(2 * time.Second),
			EffectivePort: 9999,
		},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, fieldViolationsText(err), "health_check.effective_port")
}

// Парная половина для правки: та же точечная маска без выходного поля проходит.
func TestUpdate_HealthCheckDottedMask_WithoutEffectivePort_Accepted(t *testing.T) {
	repo := newFakeRepo()
	tg := makeHTTPProbeTG(t, repo, "hc-eff-upd-clean")
	opsRepo := newFakeOpsRepo()
	uc := NewUpdateTargetGroupUseCase(repo, opsRepo, nil)

	op, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"health_check.interval"}},
		HealthCheck:   &lbv1.HealthCheck{Interval: durationpb.New(2 * time.Second)},
	})
	require.NoErrorf(t, err, "details=%s", fieldViolationsText(err))
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
}
