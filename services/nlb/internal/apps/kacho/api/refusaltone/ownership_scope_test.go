// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package refusaltone_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/listener"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/loadbalancer"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/targetgroup"
)

// refusal — одно наблюдение: что ответил Update ресурса на маску, называющую
// поле. Зависимости use-case'ов не поднимаются: проверка маски стоит первым
// стейтментом, до любого обращения к репозиторию, — поэтому nil-порты сюда не
// доезжают, а проба меряет ровно синхронный отказ.
type refusal struct {
	resource string
	field    string
	message  string
	code     codes.Code
}

func observe(t *testing.T, resource, field string, err error) refusal {
	t.Helper()
	require.Error(t, err, "%s: маска %q обязана отвергаться", resource, field)
	st := status.Convert(err)
	return refusal{resource: resource, field: field, message: st.Message(), code: st.Code()}
}

// updateWithMask прогоняет Update каждого мутируемого ресурса nlb с маской из
// одного поля и возвращает наблюдения в порядке объявления.
func updateWithMask(t *testing.T, field string) []refusal {
	t.Helper()
	ctx := context.Background()

	_, lbErr := loadbalancer.NewUpdateLoadBalancerUseCase(nil, nil, nil, nil).Execute(ctx,
		&lbv1.UpdateNetworkLoadBalancerRequest{
			NetworkLoadBalancerId: ids.NewID(ids.PrefixLoadBalancer),
			UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{field}},
		})

	_, tgErr := targetgroup.NewUpdateTargetGroupUseCase(nil, nil, nil).Execute(ctx,
		&lbv1.UpdateTargetGroupRequest{
			TargetGroupId: ids.NewID(ids.PrefixTargetGroup),
			UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{field}},
		})

	_, lsErr := listener.NewUpdateUseCase(nil, nil, nil).Run(ctx,
		&lbv1.UpdateListenerRequest{
			ListenerId: ids.NewID(ids.PrefixListener),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{field}},
		})

	return []refusal{
		observe(t, "NetworkLoadBalancer", field, lbErr),
		observe(t, "TargetGroup", field, tgErr),
		observe(t, "Listener", field, lsErr),
	}
}

// ownershipScopeShape — форма отказа о смене области владения: канонический
// зачин конвенции Kachō плюс названный следующий шаг. Хвост после имени глагола
// разрешён (у листенера переезжает РОДИТЕЛЬ, и это надо сказать), но сам глагол
// обязан быть назван.
var ownershipScopeShape = regexp.MustCompile(
	`^project_id is immutable after [A-Za-z]+\.Create; use [A-Za-z]+Service\.Move(?: .+)?$`)

// plainImmutableShape — форма отказа о поле, которое не является областью
// владения: тот же зачин и НИКАКОГО следующего шага (его у такого поля нет).
var plainImmutableShape = regexp.MustCompile(
	`^[a-z_]+ is immutable after [A-Za-z]+\.Create$`)

// TestOwnershipScopeRefusalsSpeakOneToneAndNameTheNextStep — ПАРНОЕ утверждение:
// все ресурсы nlb, формулирующие запрет на смену области владения, отвечают
// одним тоном и каждый называет следующий шаг.
//
// Проба на ОДИН отказ предикатом этой нормы не является: она закрепляет ответ, а
// не согласие ответов. Наблюдалось (#1671): балансировщик отвечал
// `project_id is immutable; use NetworkLoadBalancerService.Move`, а группа целей
// и листенер — `project_id is immutable after <R>.Create`, то есть без зачина
// конвенции у первого и без следующего шага у двух остальных. Собственные пробы
// каждого ресурса были при этом зелёными.
func TestOwnershipScopeRefusalsSpeakOneToneAndNameTheNextStep(t *testing.T) {
	observed := updateWithMask(t, "project_id")
	require.NotEmpty(t, observed,
		"перепись пуста: сравнивать нечего, вердикт беспредметен")
	t.Logf("перепись: осмотрено отказов об области владения %d", len(observed))

	// Дословный текст каждого. Изменение любой строки — изменение контракта и
	// правится осознанно, тикетом.
	want := map[string]string{
		"NetworkLoadBalancer": "project_id is immutable after NetworkLoadBalancer.Create; use NetworkLoadBalancerService.Move",
		"TargetGroup":         "project_id is immutable after TargetGroup.Create; use TargetGroupService.Move",
		"Listener":            "project_id is immutable after Listener.Create; use NetworkLoadBalancerService.Move on the parent NetworkLoadBalancer",
	}
	require.Len(t, want, len(observed),
		"перечень ожидаемых текстов разошёлся с числом осмотренных ресурсов")

	// assert, а не require: расхождение тона видно только целиком. Останов на
	// первом несогласии называл бы один ресурс из трёх и посылал чинить по
	// одному — ровно то, из-за чего расхождение и завелось.
	for _, r := range observed {
		assert.Equal(t, codes.InvalidArgument, r.code,
			"%s: код отказа", r.resource)
		assert.Equal(t, want[r.resource], r.message,
			"%s: дословный текст отказа", r.resource)
		assert.Regexp(t, ownershipScopeShape, r.message,
			"%s: отказ обязан нести зачин конвенции И назвать следующий шаг", r.resource)
	}
}

// TestPlainImmutableRefusalsCarryNoNextStep — положительный контроль к пробе
// выше. Без него «один тон» достигался бы приписыванием `; use …Move` ко ВСЕМ
// отказам об immutable-поле — включая те, у которых следующего шага нет вовсе
// (сменить протокол листенера или тип балансировщика нельзя ничем).
func TestPlainImmutableRefusalsCarryNoNextStep(t *testing.T) {
	// Поле на ресурс: у каждого своё, но все три — не область владения.
	perResource := map[string]string{
		"NetworkLoadBalancer": "type",
		"TargetGroup":         "region_id",
		"Listener":            "protocol",
	}
	seen := 0
	for resource, field := range perResource {
		for _, r := range updateWithMask(t, field) {
			if r.resource != resource {
				continue
			}
			seen++
			assert.Equal(t, codes.InvalidArgument, r.code, "%s.%s: код", resource, field)
			assert.Regexp(t, plainImmutableShape, r.message,
				"%s.%s: отказ о поле, не являющемся областью владения, следующего шага не называет",
				resource, field)
			assert.NotContains(t, r.message, "Move",
				"%s.%s: глагола переноса у этого поля нет", resource, field)
		}
	}
	require.Equal(t, len(perResource), seen,
		"перепись контроля неполна: осмотрено %d из %d", seen, len(perResource))
}
