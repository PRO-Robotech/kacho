// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// Поля, которые compute принимал и выбрасывал, — второй заход. Первый закрыл
// шесть легаси-полей Create (см. instance_create_unsupported_fields_test.go);
// здесь закрыты остальные, найденные механическим обходом дескрипторов:
//
//   - Create.ssh_public_keys — ключи не персистятся нигде и никому не выдаются;
//     подсистемы их доставки в гостя (metadata-сервис / guest-agent) в
//     control-plane не существует. Мало того, поле УДОВЛЕТВОРЯЛО страж
//     достижимости: «машина будет запущена и недостижима» снималось списком
//     ключей, который никуда не доедет, — то есть страж отпускал ровно тот
//     случай, ради которого заведён.
//   - Update.ssh_public_keys — то же, плюс метка «вступит в силу при следующей
//     загрузке»: продукт не просто игнорировал параметр, он подтверждал его приём.
//   - Update.metadata / .network_settings / .maintenance_policy /
//     .maintenance_grace_period / .serial_port_settings — известный остаток того
//     же класса: known-set маски их не содержит, поэтому ЯВНОЕ упоминание в маске
//     уже давало 400, но при ПУСТОЙ маске (full-object PATCH) тело снова
//     принималось и выбрасывалось.
//   - GetSerialPortOutput.port — ответ синтетический и от порта не зависит.
//
// Форма отказа — та же, что у шести: INVALID_ARGUMENT, обобщённый текст, имя поля
// в google.rpc.BadRequest.field_violations[].field.

// TestInstanceHandler_Create_RejectsSSHPublicKeys — sync-отказ, имя поля в
// деталях, Operation НЕ создана.
func TestInstanceHandler_Create_RejectsSSHPublicKeys(t *testing.T) {
	h, ops := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	req.SshPublicKeys = []string{"ssh-ed25519 AAAA user@h"}

	op, err := h.Create(context.Background(), req)
	require.Nil(t, op, "rejected Create must not return an Operation")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, violationFields(t, err), "ssh_public_keys")
	require.Equal(t, "invalid argument", status.Convert(err).Message(),
		"message stays generic; the field name belongs in the details")

	all, _, lerr := ops.List(context.Background(), operations.ListFilter{})
	require.NoError(t, lerr)
	require.Empty(t, all, "rejection must happen before any Operation is created")
}

// TestInstanceHandler_Create_UnreachableGuardNoLongerLiftedBySSH — следствие,
// ради которого это важно: страж достижимости больше нельзя снять списком
// ключей. VM без внешнего адреса и без признания недостижимости отвергается.
func TestInstanceHandler_Create_UnreachableGuardNoLongerLiftedBySSH(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	req.SshPublicKeys = nil
	req.AssignExternalAddress = false
	req.AcknowledgeUnreachable = false

	_, err := h.Create(context.Background(), req)
	require.Equal(t, codes.FailedPrecondition, status.Code(err),
		"a VM with no external address and no acknowledgement must not be created")
	require.Contains(t, status.Convert(err).Message(), "unreachable")
}

// updateDroppedFieldCases — по одному мутатору на каждое непринимаемое поле
// Update. Тело в остальном валидно, маска ПУСТАЯ (full-object PATCH) — именно та
// ветка, в которой поля раньше принимались и выбрасывались.
var updateDroppedFieldCases = []struct {
	field string
	set   func(*computev1.UpdateInstanceRequest)
}{
	{"ssh_public_keys", func(r *computev1.UpdateInstanceRequest) {
		r.SshPublicKeys = []string{"ssh-ed25519 AAAA user@h"}
	}},
	// Поля `metadata` здесь БОЛЬШЕ НЕТ, и это не пропуск: оно снято с контракта
	// целиком (номер и имя зарезервированы). Кейс, ставивший его, стал
	// неконструируемым by construction — сообщение такого поля не несёт, — а
	// значит проба про «непринимаемое поле» о нём высказаться не может.
	{"network_settings", func(r *computev1.UpdateInstanceRequest) {
		r.NetworkSettings = &computev1.NetworkSettings{Type: computev1.NetworkSettings_SOFTWARE_ACCELERATED}
	}},
	{"maintenance_policy", func(r *computev1.UpdateInstanceRequest) {
		r.MaintenancePolicy = computev1.MaintenancePolicy_MIGRATE
	}},
	{"maintenance_grace_period", func(r *computev1.UpdateInstanceRequest) {
		r.MaintenanceGracePeriod = durationpb.New(60_000_000_000)
	}},
	{"serial_port_settings", func(r *computev1.UpdateInstanceRequest) {
		r.SerialPortSettings = &computev1.SerialPortSettings{SshAuthorization: computev1.SerialPortSettings_OS_LOGIN}
	}},
}

// seedInstanceForUpdate создаёт инстанс и возвращает его id (Update читает
// существующий ресурс раньше, чем дошёл бы до тела).
func seedInstanceForUpdate(t *testing.T, h *InstanceHandler, ops *portmock.OpsRepo) string {
	t.Helper()
	op, err := h.Create(context.Background(), validCreateReq())
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, ops, op.Id)
	require.Nil(t, done.Error, "seed Create must succeed: %v", done.Error)

	var md computev1.CreateInstanceMetadata
	require.NoError(t, done.Metadata.UnmarshalTo(&md))
	require.NotEmpty(t, md.GetInstanceId())
	return md.GetInstanceId()
}

// TestInstanceHandler_Update_RejectsDroppedField — пустая маска, одно
// непринимаемое поле в теле: sync INVALID_ARGUMENT с именем поля.
func TestInstanceHandler_Update_RejectsDroppedField(t *testing.T) {
	for _, tc := range updateDroppedFieldCases {
		t.Run(tc.field, func(t *testing.T) {
			h, ops := newInstanceHandlerForValidation(t)
			id := seedInstanceForUpdate(t, h, ops)

			req := &computev1.UpdateInstanceRequest{InstanceId: id, Name: "vm-renamed"}
			tc.set(req)

			op, err := h.Update(context.Background(), req)
			require.Nil(t, op, "rejected Update must not return an Operation")
			require.Equal(t, codes.InvalidArgument, status.Code(err),
				"%s must be rejected synchronously", tc.field)
			require.Contains(t, violationFields(t, err), tc.field)
			require.Equal(t, "invalid argument", status.Convert(err).Message())
		})
	}
}

// TestInstanceHandler_Update_RejectsDroppedFieldEvenInMask — та же форма отказа,
// когда поле названо в маске явно. Раньше явная маска давала generic «unknown
// field» (known-set его не содержит), а пустая — молчаливое выбрасывание: один и
// тот же параметр отвечал по-разному в зависимости от маски.
func TestInstanceHandler_Update_RejectsDroppedFieldEvenInMask(t *testing.T) {
	for _, tc := range updateDroppedFieldCases {
		t.Run(tc.field, func(t *testing.T) {
			h, ops := newInstanceHandlerForValidation(t)
			id := seedInstanceForUpdate(t, h, ops)

			req := &computev1.UpdateInstanceRequest{
				InstanceId: id,
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{tc.field}},
			}
			tc.set(req)

			_, err := h.Update(context.Background(), req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Contains(t, violationFields(t, err), tc.field,
				"the field name must be reported the same way as with an empty mask")
		})
	}
}

// TestInstanceHandler_Update_AcceptsSupportedFields — контрольный случай той же
// формы: обычная правка проходит. Без него гейт ловил бы «Update что-нибудь
// отвергает», а не «отвергает именно непринимаемое».
func TestInstanceHandler_Update_AcceptsSupportedFields(t *testing.T) {
	h, ops := newInstanceHandlerForValidation(t)
	id := seedInstanceForUpdate(t, h, ops)

	op, err := h.Update(context.Background(), &computev1.UpdateInstanceRequest{
		InstanceId:  id,
		Name:        "vm-renamed",
		Description: "d",
	})
	require.NoError(t, err)
	require.NotNil(t, op)
}

// TestInstanceHandler_GetSerialPortOutput_RejectsPort — ответ синтетический и от
// порта не зависит; принимать номер порта значит обещать выбор, которого нет.
func TestInstanceHandler_GetSerialPortOutput_RejectsPort(t *testing.T) {
	h, ops := newInstanceHandlerForValidation(t)
	id := seedInstanceForUpdate(t, h, ops)

	_, err := h.GetSerialPortOutput(context.Background(), &computev1.GetInstanceSerialPortOutputRequest{
		InstanceId: id,
		Port:       2,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, violationFields(t, err), "port")
}

// TestInstanceHandler_GetSerialPortOutput_UnsetPortPasses — контрольный случай:
// не присланный порт проходит (proto3 не отличает «не прислано» от нуля, поэтому
// зацепка — заданное значение).
func TestInstanceHandler_GetSerialPortOutput_UnsetPortPasses(t *testing.T) {
	h, ops := newInstanceHandlerForValidation(t)
	id := seedInstanceForUpdate(t, h, ops)

	resp, err := h.GetSerialPortOutput(context.Background(), &computev1.GetInstanceSerialPortOutputRequest{
		InstanceId: id,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetContents())
}
