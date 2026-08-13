// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
)

// maskViolations — описания FieldViolation'ов из BadRequest-детали ошибки.
// corevalidate.UpdateMask отдаёт сообщение "invalid argument", а имя отвергнутого
// поля кладёт В ДЕТАЛЬ, поэтому утверждать про неизвестное поле по st.Message()
// нельзя: такая проба зеленела бы на любом отказе.
func maskViolations(t *testing.T, err error) []string {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "ошибка обязана быть gRPC-статусом")
	var out []string
	for _, d := range st.Details() {
		br, isBR := d.(*errdetails.BadRequest)
		if !isBR {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			out = append(out, v.GetField()+": "+v.GetDescription())
		}
	}
	return out
}

// seedGateway создаёт шлюз через use-case create и возвращает его id — Update'у
// нужен существующий предмет, иначе положительный контроль не отличит «маска
// принята» от «предмета нет».
// Пробы этого файла заякорены на ветвь «только исход» НАМЕРЕННО, и это не
// безразличный выбор фикстуры. Их предмет — маска обновления, форма ответа и
// поток CRUD, то есть свойства, от вида шлюза не зависящие. Ветвь трансляции
// потребовала бы пул, аренду и привязку адреса; научить этому in-memory дублёра
// значило бы написать поддельный учёт пула, а поддельный учёт оказался бы
// СНИСХОДИТЕЛЬНЕЕ настоящего ровно там, где проверка и нужна. Ветвь трансляции
// проверяется против настоящего Postgres — services/vpc/internal/repo/
// gateway_external_address_integration_test.go.
func seedGateway(t *testing.T) (*Handler, *repomock.OpsRepo, string) {
	t.Helper()
	h, or, _ := minimalHandler(t, true)
	createOp, err := h.Create(narrowtest.Caller(), &vpcv1.CreateGatewayRequest{
		ProjectId: "f1", Name: "mask-probe",
		Gateway: &vpcv1.CreateGatewayRequest_EgressOnlyGatewaySpec{
			EgressOnlyGatewaySpec: &vpcv1.EgressOnlyGatewaySpec{},
		},
		SubnetId: seedSubnetID,
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, createOp.Id)
	require.Nil(t, saved.Error)

	resp, err := h.List(narrowtest.Caller(), &vpcv1.ListGatewaysRequest{ProjectId: "f1"})
	require.NoError(t, err)
	require.Len(t, resp.Gateways, 1)
	return h, or, resp.Gateways[0].Id
}

func mask(paths ...string) *fieldmaskpb.FieldMask {
	return &fieldmaskpb.FieldMask{Paths: paths}
}

// seedSubnetID — якорь размещения для проб этого файла. Он обязан быть
// well-formed id подсети: формат СВОЕГО id use-case проверяет первым стейтментом,
// и мусорная строка отвергалась бы раньше, чем дело дошло бы до маски.
// Существование подсети проверяет оператор вставки (integration-уровень), а
// in-memory mock ссылку не резолвит — здесь предмет проверки маска, а не якорь.
var seedSubnetID = ids.NewID(ids.PrefixSubnet)

// TestGatewayUpdateMaskNamesContractFieldsNotDBColumns — известный набор полей
// маски обязан состоять из имён ПОЛЕЙ КОНТРАКТА, а не из имён столбцов БД.
//
// До починки набор содержал `gateway_type` — имя столбца
// (services/vpc/internal/migrations/0001_initial.sql), которого нет ни в одном
// сообщении proto. Следствие было двойным, и обе половины проверяются здесь:
// имя столбца ПРИНИМАЛОСЬ и меняло вид шлюза, а законное имя поля контракта
// отвергалось как «unknown field». Положительный контроль (`name` проходит)
// обязателен: без него отрицания зеленели бы на наборе, отвергающем всё.
func TestGatewayUpdateMaskNamesContractFieldsNotDBColumns(t *testing.T) {
	t.Run("положительный контроль: законное имя поля контракта принято", func(t *testing.T) {
		h, or, gwID := seedGateway(t)
		op, err := h.Update(narrowtest.Caller(), &vpcv1.UpdateGatewayRequest{
			GatewayId:  gwID,
			Name:       "mask-probe-renamed",
			UpdateMask: mask("name"),
		})
		require.NoError(t, err, "mask=[name] — законная маска, отказа быть не должно")
		saved := repomock.AwaitOpDone(t, or, op.Id)
		require.Nil(t, saved.Error, "законная маска обязана доехать до записи")
	})

	t.Run("имя столбца БД отвергается", func(t *testing.T) {
		h, _, gwID := seedGateway(t)
		_, err := h.Update(narrowtest.Caller(), &vpcv1.UpdateGatewayRequest{
			GatewayId:  gwID,
			UpdateMask: mask("gateway_type"),
		})
		require.Error(t, err, "`gateway_type` — столбец БД, полем контракта он не является")
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, maskViolations(t, err),
			"update_mask: unknown field in update_mask: gateway_type")
	})

	// Имена перечислены ЛИТЕРАЛАМИ, а не взяты из констант прод-кода: предмет
	// пробы — контракт, который видит вызывающий. Читая тот же источник, что и
	// проверяемый код, проба закрепила бы его согласие с собой.
	t.Run("вид шлюза и привязка названы контрактными именами и НЕИЗМЕНЯЕМЫ", func(t *testing.T) {
		for _, field := range []string{"nat_gateway", "egress_only_gateway", "subnet_id"} {
			h, _, gwID := seedGateway(t)
			_, err := h.Update(narrowtest.Caller(), &vpcv1.UpdateGatewayRequest{
				GatewayId:  gwID,
				UpdateMask: mask(field),
			})
			require.Error(t, err, field)
			assert.Equal(t, codes.InvalidArgument, status.Code(err), field)
			assert.Equal(t, field+" is immutable after Gateway.Create",
				status.Convert(err).Message(),
				"вид шлюза и якорь выбираются на Create; отказ обязан нести конвенционный тон, а не generic unknown-field")
		}
	})
}

// TestGatewayCreateRequiresKindAndAnchor — обе обязательных величины Create
// отвергаются ИМЕНЕМ ПОЛЯ, и рядом стоит положительный контроль. Без него
// отрицания зеленели бы на реализации, отвергающей любой Create.
func TestGatewayCreateRequiresKindAndAnchor(t *testing.T) {
	t.Run("положительный контроль: вид и якорь названы — Create принят", func(t *testing.T) {
		h, or, _ := minimalHandler(t, true)
		op, err := h.Create(narrowtest.Caller(), &vpcv1.CreateGatewayRequest{
			ProjectId: "f1", Name: "gw-ok",
			Gateway:  &vpcv1.CreateGatewayRequest_EgressOnlyGatewaySpec{EgressOnlyGatewaySpec: &vpcv1.EgressOnlyGatewaySpec{}},
			SubnetId: seedSubnetID,
		})
		require.NoError(t, err)
		saved := repomock.AwaitOpDone(t, or, op.Id)
		require.Nil(t, saved.Error)
	})

	t.Run("вид шлюза не назван — отказ по имени поля", func(t *testing.T) {
		h, _, _ := minimalHandler(t, true)
		_, err := h.Create(narrowtest.Caller(), &vpcv1.CreateGatewayRequest{
			ProjectId: "f1", Name: "gw-no-kind",
			SubnetId: seedSubnetID,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Equal(t, "gateway: required", status.Convert(err).Message())
	})

	t.Run("якорь размещения не назван — отказ по имени поля", func(t *testing.T) {
		h, _, _ := minimalHandler(t, true)
		_, err := h.Create(narrowtest.Caller(), &vpcv1.CreateGatewayRequest{
			ProjectId: "f1", Name: "gw-no-anchor",
			Gateway: &vpcv1.CreateGatewayRequest_EgressOnlyGatewaySpec{EgressOnlyGatewaySpec: &vpcv1.EgressOnlyGatewaySpec{}},
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Equal(t, "subnet_id: required", status.Convert(err).Message())
	})

	t.Run("якорь размещения — мусор: терминальный отказ формата, а не полоса существования", func(t *testing.T) {
		h, _, _ := minimalHandler(t, true)
		_, err := h.Create(narrowtest.Caller(), &vpcv1.CreateGatewayRequest{
			ProjectId: "f1", Name: "gw-bad-anchor",
			Gateway:  &vpcv1.CreateGatewayRequest_EgressOnlyGatewaySpec{EgressOnlyGatewaySpec: &vpcv1.EgressOnlyGatewaySpec{}},
			SubnetId: "not-an-id",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Contains(t, status.Convert(err).Message(), "invalid subnet id 'not-an-id'")
	})
}
