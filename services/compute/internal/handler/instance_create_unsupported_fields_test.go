// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// Шесть полей CreateInstanceRequest, которые compute принимал и выбрасывал: они
// живы в контракте, но во всём непроверочном дереве services/compute нет ни одного
// читателя. «Принято-и-проигнорировано» — не исход (api-conventions.md); поведение
// приведено к тому, что docs/engineering/architecture/07-known-divergences.md уже обещал по
// filesystem_specs, и распространено на остальные пять.
//
// Форма отказа — контракт: код INVALID_ARGUMENT, сообщение ОБОБЩЁННОЕ
// («invalid argument»), имя поля — в google.rpc.BadRequest.field_violations[].field.
// Заперто на уровне наблюдаемого, а не «код 3 и ладно».

// violationFields собирает имена полей из BadRequest-деталей статуса.
func violationFields(t *testing.T, err error) []string {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "error must carry a gRPC status: %v", err)
	var out []string
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, fv := range br.GetFieldViolations() {
			out = append(out, fv.GetField())
		}
	}
	return out
}

// unsupportedCreateFieldCases — по одному мутатору на каждое из шести полей.
// Тело в остальном ВАЛИДНО (см. validCreateReq), поэтому единственная причина
// отказа — само поле.
var unsupportedCreateFieldCases = []struct {
	field string
	set   func(*computev1.CreateInstanceRequest)
}{
	{"network_settings", func(r *computev1.CreateInstanceRequest) {
		r.NetworkSettings = &computev1.NetworkSettings{Type: computev1.NetworkSettings_SOFTWARE_ACCELERATED}
	}},
	{"filesystem_specs", func(r *computev1.CreateInstanceRequest) {
		r.FilesystemSpecs = []*computev1.AttachedFilesystemSpec{
			{Mode: computev1.AttachedFilesystemSpec_READ_WRITE, DeviceName: "fs0", FilesystemId: "fs-1"},
		}
	}},
	{"local_disk_specs", func(r *computev1.CreateInstanceRequest) {
		r.LocalDiskSpecs = []*computev1.AttachedLocalDiskSpec{{Size: 107374182400}}
	}},
	{"maintenance_policy", func(r *computev1.CreateInstanceRequest) {
		r.MaintenancePolicy = computev1.MaintenancePolicy_MIGRATE
	}},
	{"maintenance_grace_period", func(r *computev1.CreateInstanceRequest) {
		r.MaintenanceGracePeriod = durationpb.New(60_000_000_000) // 60s
	}},
	{"serial_port_settings", func(r *computev1.CreateInstanceRequest) {
		r.SerialPortSettings = &computev1.SerialPortSettings{
			SshAuthorization: computev1.SerialPortSettings_OS_LOGIN,
		}
	}},
	// Четыре поля ВНУТРИ `network_interface_specs[]`, найденные тем же обходом.
	// Родитель ЧИТАЕТСЯ (из него берутся subnetId и securityGroupIds), поэтому
	// прежний обход по верхнему уровню их не видел: отображение proto → domain
	// (`nicSpecsFromProto`) переносит ровно два поля, остальные молча теряются.
	//
	// Два из четырёх предикат тоже пропустил, и по названной причине: он ищет
	// читателя ПО ИМЕНИ, а `NicId` и `Index` читаются у соседнего сообщения в том же
	// файле (spec attach-RPC), поэтому поля выглядели закрытыми. Найдены глазами по
	// списку «закрыто именем-омонимом».
	{"network_interface_specs.primary_v4_address_spec", func(r *computev1.CreateInstanceRequest) {
		nicSpec(r).PrimaryV4AddressSpec = &computev1.PrimaryAddressSpec{Address: "10.0.0.7"}
	}},
	{"network_interface_specs.primary_v6_address_spec", func(r *computev1.CreateInstanceRequest) {
		nicSpec(r).PrimaryV6AddressSpec = &computev1.PrimaryAddressSpec{Address: "fd00::7"}
	}},
	{"network_interface_specs.nic_id", func(r *computev1.CreateInstanceRequest) {
		nicSpec(r).NicId = "nic-b3n7k1x9q2m5t8"
	}},
	{"network_interface_specs.index", func(r *computev1.CreateInstanceRequest) {
		nicSpec(r).Index = "3"
	}},
}

// nicSpec — первый (и единственный в validCreateReq) элемент network_interface_specs.
// Мутаторы обязаны СОСТАВЛЯТЬСЯ: два теста таблицы применяют их все подряд к одному
// запросу, поэтому элемент правится на месте, а не пересоздаётся.
func nicSpec(r *computev1.CreateInstanceRequest) *computev1.NetworkInterfaceSpec {
	if len(r.NetworkInterfaceSpecs) == 0 {
		r.NetworkInterfaceSpecs = []*computev1.NetworkInterfaceSpec{{SubnetId: "sub-a"}}
	}
	return r.NetworkInterfaceSpecs[0]
}

// TestInstanceHandler_Create_RejectsUnsupportedField — каждое из шести полей по
// отдельности: sync INVALID_ARGUMENT, имя поля в деталях, Operation НЕ создана
// (отказ синхронный, а не async op.error).
func TestInstanceHandler_Create_RejectsUnsupportedField(t *testing.T) {
	for _, tc := range unsupportedCreateFieldCases {
		t.Run(tc.field, func(t *testing.T) {
			h, ops := newInstanceHandlerForValidation(t)
			req := validCreateReq()
			tc.set(req)

			op, err := h.Create(context.Background(), req)
			require.Nil(t, op, "rejected Create must not return an Operation")
			require.Equal(t, codes.InvalidArgument, status.Code(err),
				"%s must be rejected synchronously with InvalidArgument", tc.field)
			require.Contains(t, violationFields(t, err), tc.field,
				"field name must be reported in BadRequest.field_violations")

			all, _, lerr := ops.List(context.Background(), operations.ListFilter{})
			require.NoError(t, lerr)
			require.Empty(t, all, "rejection must happen before any Operation is created")
		})
	}
}

// TestInstanceHandler_Create_UnsupportedFieldMessageStaysGeneric — имя поля живёт в
// деталях, НЕ в тексте. Замок против дрейфа к «говорящему» message: текст —
// стабильный обобщённый контракт, машиночитаемое — details.
func TestInstanceHandler_Create_UnsupportedFieldMessageStaysGeneric(t *testing.T) {
	for _, tc := range unsupportedCreateFieldCases {
		t.Run(tc.field, func(t *testing.T) {
			h, _ := newInstanceHandlerForValidation(t)
			req := validCreateReq()
			tc.set(req)

			_, err := h.Create(context.Background(), req)
			msg := status.Convert(err).Message()
			require.Equal(t, "invalid argument", msg,
				"message must stay generic; the field name belongs in the details")
			require.NotContains(t, strings.ToLower(msg), strings.ToLower(tc.field))
		})
	}
}

// TestInstanceHandler_Create_UnsupportedFieldBeatsOtherValidation — отказ идёт
// ПЕРВЫМ стейтментом: тело, невалидное и по другой причине (нет instanceKind),
// всё равно отвечает про непринимаемое поле, а не про instance_kind.
func TestInstanceHandler_Create_UnsupportedFieldBeatsOtherValidation(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	req.InstanceKind = computev1.InstanceKind_INSTANCE_KIND_UNSPECIFIED // отдельный, более поздний отказ
	req.FilesystemSpecs = []*computev1.AttachedFilesystemSpec{{FilesystemId: "fs-1"}}

	_, err := h.Create(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	fields := violationFields(t, err)
	require.Contains(t, fields, "filesystem_specs")
	require.NotContains(t, fields, "instance_kind",
		"unsupported-field rejection must run before the rest of Create validation")
}

// TestInstanceHandler_Create_ReportsEveryUnsupportedField — клиент, приславший все
// шесть, узнаёт про все за один заход, а не по одному на запрос.
func TestInstanceHandler_Create_ReportsEveryUnsupportedField(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	for _, tc := range unsupportedCreateFieldCases {
		tc.set(req)
	}

	_, err := h.Create(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	fields := violationFields(t, err)
	for _, tc := range unsupportedCreateFieldCases {
		require.Contains(t, fields, tc.field)
	}
	require.Len(t, fields, len(unsupportedCreateFieldCases))
}

// TestRejectUnsupportedCreateFields_RESTPayloads — те же шесть полей, но в том виде,
// в каком их шлёт чёрный ящик: camelCase-JSON через grpc-gateway. Замок проверяет,
// что тела newman-кейсов INST-RD-CR-VAL-UNSUPPORTED-* вообще разбираются в
// CreateInstanceRequest и доходят до отказа с ожидаемым именем поля. Без него
// опечатка в JSON-ключе дала бы 400 на разборе тела — кейс «падает», но по совсем
// другой причине, и это видно только на живом стенде.
func TestRejectUnsupportedCreateFields_RESTPayloads(t *testing.T) {
	payloads := map[string]string{
		"network_settings":         `{"networkSettings":{"type":"SOFTWARE_ACCELERATED"}}`,
		"filesystem_specs":         `{"filesystemSpecs":[{"mode":"READ_WRITE","deviceName":"fs0","filesystemId":"fs-1"}]}`,
		"local_disk_specs":         `{"localDiskSpecs":[{"size":"107374182400"}]}`,
		"maintenance_policy":       `{"maintenancePolicy":"MIGRATE"}`,
		"maintenance_grace_period": `{"maintenanceGracePeriod":"60s"}`,
		"serial_port_settings":     `{"serialPortSettings":{"sshAuthorization":"OS_LOGIN"}}`,
	}
	for field, body := range payloads {
		t.Run(field, func(t *testing.T) {
			req := &computev1.CreateInstanceRequest{}
			require.NoError(t, protojson.Unmarshal([]byte(body), req),
				"REST payload must decode into CreateInstanceRequest")

			err := RejectUnsupportedCreateFields(req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Equal(t, []string{field}, violationFields(t, err))
		})
	}
}

// TestInstanceHandler_Create_ZeroValuedLegacyBlockPasses — граница: proto3 не
// отличает «не прислал» от «прислал нули» на non-optional enum и на пустом
// repeated. Отказ обязан цепляться за ЗАДАННОЕ значение, иначе он сломает каждый
// валидный Create.
func TestInstanceHandler_Create_ZeroValuedLegacyBlockPasses(t *testing.T) {
	h, ops := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	req.MaintenancePolicy = computev1.MaintenancePolicy_MAINTENANCE_POLICY_UNSPECIFIED
	req.FilesystemSpecs = []*computev1.AttachedFilesystemSpec{}
	req.LocalDiskSpecs = []*computev1.AttachedLocalDiskSpec{}

	op, err := h.Create(context.Background(), req)
	require.NoError(t, err, "zero-valued legacy block is indistinguishable from absent and must pass")
	require.NotEmpty(t, op.Id)
	done := portmock.AwaitOpDone(t, ops, op.Id)
	require.Nil(t, done.Error)
}
