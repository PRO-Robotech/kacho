// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// Поиск адреса по значению НЕ отвечает на вопрос о внешнем адресе, и обязан
// сказать это вызывающему прямо — по имени поля, синхронно.
//
// Почему прямо: единственная область, которой располагает запрос, — подсеть
// (`oneof scope`), и авторизация метода читает именно её. Внешний адрес в
// подсети не размещается, поэтому вопрос «какой внешний адрес имеет значение X
// внутри подсети S» не имеет ответа НИ ПРИ КАКИХ данных. Пока отказа не было,
// вызывающий получал «не найдено» про адрес, который существует, — ложное
// утверждение об отсутствии в ответ на запрос, который контракт рекламирует.
//
// Утверждение — на уровне наблюдаемого: код, имя поля в сообщении И в
// field-violation. Возврат ложного «не найдено» (как было до фикса) роняет
// каждое из трёх.
func TestGetByValueUseCase_ExternalAddress_RefusedNamingTheField(t *testing.T) {
	kr := kachomock.NewRepository()
	seedExternalAddress(t, kr, "203.0.113.7")
	uc := NewGetByValueUseCase(kr)

	_, err := uc.Execute(context.Background(), "203.0.113.7", "", ids.NewID(ids.PrefixSubnet))

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "ожидается gRPC-статус")
	require.Equal(t, codes.InvalidArgument, st.Code(),
		"отказ синхронный и терминальный, а не «не найдено» про существующий адрес")
	require.Contains(t, st.Message(), "external_ipv4_address",
		"сообщение называет поле, из-за которого запрос отвергнут")
	require.Contains(t, fieldViolationFields(t, err), "external_ipv4_address",
		"поле названо и в details — клиент читает его машинно")
}

// Отказ не зависит от того, названа ли область: у этого RPC нет области, в
// которой внешний адрес разрешим. Без этого утверждения фикс можно было бы
// «починить» до «отвергаем только когда подсеть названа», и вызов без подсети
// снова отвечал бы ложным «не найдено» — на том пути, куда его пускает
// cluster-internal listener.
func TestGetByValueUseCase_ExternalAddress_RefusedWithoutSubnetToo(t *testing.T) {
	kr := kachomock.NewRepository()
	seedExternalAddress(t, kr, "203.0.113.8")
	uc := NewGetByValueUseCase(kr)

	_, err := uc.Execute(context.Background(), "203.0.113.8", "", "")

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "external_ipv4_address")
}

// Внутренняя ветка — та, которую метод действительно умеет, — не задета.
func TestGetByValueUseCase_InternalAddress_StillResolves(t *testing.T) {
	kr := kachomock.NewRepository()
	subnetID := ids.NewID(ids.PrefixSubnet)
	want := seedInternalAddress(t, kr, "10.0.0.5", subnetID)
	uc := NewGetByValueUseCase(kr)

	got, err := uc.Execute(context.Background(), "", "10.0.0.5", subnetID)

	require.NoError(t, err)
	require.Equal(t, want, got.ID)
}

// Тот же отказ виден на transport-слое (handler), а не только в use-case: это
// то, что получает вызывающий по сети.
func TestHandler_GetByValue_ExternalAddress_RefusedNamingTheField(t *testing.T) {
	h, _, kr, _ := minimalHandler(t, true)
	seedExternalAddress(t, kr, "203.0.113.9")

	_, err := h.GetByValue(context.Background(), &vpcv1.GetAddressByValueRequest{
		Address: &vpcv1.GetAddressByValueRequest_ExternalIpv4Address{ExternalIpv4Address: "203.0.113.9"},
		Scope:   &vpcv1.GetAddressByValueRequest_SubnetId{SubnetId: ids.NewID(ids.PrefixSubnet)},
	})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "external_ipv4_address")
}

// ---- helpers ---------------------------------------------------------------

func seedExternalAddress(t *testing.T, kr *kachomock.Repository, ip string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixAddress)
	insertAddressRecord(t, kr, &domain.Address{
		ID: id, ProjectID: "f1", Name: domain.RcNameVPC("ext"),
		ExternalIpv4: &domain.ExternalIpv4Spec{Address: ip},
	})
	return id
}

func seedInternalAddress(t *testing.T, kr *kachomock.Repository, ip, subnetID string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixAddress)
	insertAddressRecord(t, kr, &domain.Address{
		ID: id, ProjectID: "f1", Name: domain.RcNameVPC("int"),
		InternalIpv4: &domain.InternalIpv4Spec{Address: ip, SubnetID: subnetID},
	})
	return id
}

func insertAddressRecord(t *testing.T, kr *kachomock.Repository, a *domain.Address) {
	t.Helper()
	ctx := context.Background()
	w, err := kr.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Addresses().Insert(ctx, a)
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

// fieldViolationFields — имена полей из BadRequest-details статуса.
func fieldViolationFields(t *testing.T, err error) []string {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "ожидается gRPC-статус")
	var out []string
	for _, d := range st.Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			for _, fv := range br.GetFieldViolations() {
				out = append(out, fv.GetField())
			}
		}
	}
	return out
}
