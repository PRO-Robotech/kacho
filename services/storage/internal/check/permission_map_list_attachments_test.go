// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/check"
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
)

const listAttachmentsMethod = "/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments"

// principalCtx — ctx с principal'ом, который кладёт principal-extract-интерсептор.
func principalCtx(typ, id string) context.Context {
	return operations.WithPrincipal(context.Background(), operations.Principal{
		Type: typ, ID: id, DisplayName: "test",
	})
}

// newTestInterceptor — authz-интерсептор поверх реальной PermissionMap с подменным
// CheckClient'ом; счётчик показывает, задавался ли вопрос модели вообще.
func newTestInterceptor(t *testing.T, allow bool) (*authz.Interceptor, *int) {
	t.Helper()
	calls := 0
	cli := authz.CheckClientFunc(func(context.Context, string, string, string) (bool, error) {
		calls++
		return allow, nil
	})
	intr := authz.NewInterceptor(authz.InterceptorOptions{
		Cache:       authz.NewCache(0),
		ServiceName: "kacho-storage-test",
		Map:         check.PermissionMap(),
		Client:      cli,
	})
	return intr, &calls
}

// TestPermissionMap_InternalVolume_ListAttachments_ScopeFiltered — привязки томов
// авторизуются на уровне ДАННЫХ, а не единичным per-RPC Check'ом.
//
// Инстансы называет вызывающий, а ответ касается томов, у каждого из которых свой
// владелец: одного объекта, про который можно спросить заранее, здесь нет. Прежний
// вопрос — `viewer` на синглтоне `cluster:cluster_kacho_root` — относился к
// ГЛОБАЛЬНОМУ СПРАВОЧНИКУ (регионы, зоны, типы дисков), и bootstrap кластера
// намеренно пишет `cluster:<root>#viewer@user:*`, чтобы справочник читал любой
// аутентифицированный субъект. Значит проверка пропускала всех.
//
// Locked здесь: cluster-уровневый relation НЕ восстановлен (иначе гейт снова стал бы
// формальностью) и метод НЕ помечен Public (это не exempt — авторизация не исчезла,
// она переехала на данные).
func TestPermissionMap_InternalVolume_ListAttachments_ScopeFiltered(t *testing.T) {
	e, ok := check.PermissionMap()[listAttachmentsMethod]
	require.True(t, ok, "%s must be present in PermissionMap", listAttachmentsMethod)
	require.True(t, e.ScopeFiltered,
		"видимость решается per-object в use-case, не единичным Check'ом")
	require.False(t, e.Public,
		"не exempt: авторизация переехала на данные, а не исчезла")
	require.Empty(t, e.Relation,
		"cluster-scoped relation здесь пропускал каждого аутентифицированного субъекта "+
			"(`cluster:<root>#viewer@user:*` пишет bootstrap) — не восстанавливать")
	require.Nil(t, e.Extract,
		"объекта, про который можно спросить заранее, у этого RPC нет")
	require.Equal(t, "storage.volumes.listAttachments", e.Permission,
		"строка каталога сохраняется — метод не исчезает из аудита")
}

// TestInterceptor_ListAttachments_NoSingleObjectCheck — behaviour-level: интерсептор
// НЕ задаёт единичный вопрос модели за этот RPC и пропускает его к handler'у,
// который сузит ответ per-object. `calls==0` ловит регрессию, при которой сюда
// вернули бы cluster-scoped Check (он снова пропускал бы всех).
func TestInterceptor_ListAttachments_NoSingleObjectCheck(t *testing.T) {
	intr, calls := newTestInterceptor(t, false) // deny — если Check вызовут, до handler'а не дойдём
	uIntr := intr.Unary()

	handlerCalled := false
	handler := func(context.Context, any) (any, error) {
		handlerCalled = true
		return &storagev1.ListAttachmentsResponse{}, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: listAttachmentsMethod}
	req := &storagev1.ListAttachmentsRequest{InstanceIds: []string{"ins-x"}}

	_, err := uIntr(principalCtx("user", "usr_alice"), req, info, handler)
	require.NoError(t, err)
	require.True(t, handlerCalled, "handler обязан отработать — он и есть точка авторизации")
	require.Equal(t, 0, *calls, "единичный Check за этот RPC не задаётся: спрашивать нечего")
}

// TestScopeFilteredRPCs_AreBackedByTheProductionBootGuard — марка ScopeFiltered
// снимает per-RPC Check, поэтому единственной защитой такого RPC остаётся
// per-object фильтр. Значит фильтр не может быть просто конфигурационной ручкой,
// которую выключили и не заметили: пока в карте есть хотя бы одна такая запись,
// production boot-guard ОБЯЗАН отказывать в старте при выключенном фильтре.
//
// Тест связывает два артефакта наблюдаемо: карту (какие RPC остались без Check'а) и
// config.Validate (что именно отвергается на старте). Уберут guard — покраснеет
// здесь, а не только в config-тестах, где связь с картой не видна.
func TestScopeFilteredRPCs_AreBackedByTheProductionBootGuard(t *testing.T) {
	var scopeFiltered []string
	for fullMethod, e := range check.PermissionMap() {
		if e.ScopeFiltered {
			scopeFiltered = append(scopeFiltered, fullMethod)
		}
	}
	require.NotEmpty(t, scopeFiltered,
		"этот тест имеет смысл, только пока такие RPC есть; иначе удалить вместе с ними")

	prod := config.Config{
		AuthMode:          "production",
		DBSSLMode:         "require",
		AuthZIAMGRPCAddr:  "kacho-iam-internal:9091",
		ListFilterEnabled: false, // ослаблено ровно одно измерение
		AuthZTrustedForwarderSANs: []string{
			"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway",
			"spiffe://kacho.cloud/ns/kacho/sa/kacho-compute",
		},
		GeoClientMTLS:      grpcclient.TLSClient{Enable: true},
		IAMClientMTLS:      grpcclient.TLSClient{Enable: true},
		PublicServerMTLS:   grpcsrv.TLSServer{Enable: true},
		InternalServerMTLS: grpcsrv.TLSServer{Enable: true},
	}
	err := prod.Validate()
	require.Errorf(t, err,
		"production с выключенным list-фильтром обязан отказать в старте: %v остались без "+
			"per-RPC Check и полагаются на фильтр целиком", scopeFiltered)
	require.Contains(t, err.Error(), "LIST_FILTER_ENABLED", "отказ обязан назвать ручку")

	// Обратная сторона: с включённым фильтром та же посадка стартует — гейт
	// отвергает именно это измерение, а не «всё подряд».
	prod.ListFilterEnabled = true
	require.NoError(t, prod.Validate())
}

// TestInterceptor_ObjectScopedInternalRPCStillChecked — контроль на то, что снятие
// Check'а не расползлось: соседний internal RPC того же сервиса (GetInternal, у
// него объект в запросе ЕСТЬ) по-прежнему проходит per-RPC Check и отвергается,
// когда модель отвечает «нет».
func TestInterceptor_ObjectScopedInternalRPCStillChecked(t *testing.T) {
	intr, calls := newTestInterceptor(t, false)
	uIntr := intr.Unary()

	handlerCalled := false
	handler := func(context.Context, any) (any, error) {
		handlerCalled = true
		return &storagev1.VolumeInternal{}, nil
	}
	info := &grpc.UnaryServerInfo{
		FullMethod: "/kacho.cloud.storage.v1.InternalVolumeService/GetInternal",
	}
	req := &storagev1.GetInternalVolumeRequest{VolumeId: "vol00000000000000001"}

	_, err := uIntr(principalCtx("user", "usr_alice"), req, info, handler)
	require.Error(t, err, "модель ответила «нет» — вызов не должен дойти до handler'а")
	require.False(t, handlerCalled)
	require.Equal(t, 1, *calls, "объектный internal RPC обязан спрашивать модель")
}
