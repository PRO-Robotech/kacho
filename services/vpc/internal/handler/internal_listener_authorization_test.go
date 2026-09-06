// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

// Кто на самом деле решает на :9091.
//
// Авторизация выражена в модели прав и энфорсится per-RPC интерсептором
// (check.PermissionMap → InternalIAMService.Check), fail-closed. AuthN-интерсептор
// перед ним отвечает ровно на один вопрос — предъявлен ли принципал — и собственного
// решения о доступе не принимает.
//
// Тесты ниже прогоняют РЕАЛЬНУЮ цепочку — извлечение личности (доверенная пара
// grpcsrv) → AuthN → authz, в том же порядке, что cmd/vpc/main.go собирает
// internalUnary, — по матрице форм вызывающего и фиксируют исход. Матрица — не
// украшение: она и есть доказательство, что снятие самодельного admin-гейта ничего
// не расширило. Единственный источник «да» — модель.
//
// Личность приходит ТУДА, откуда приходит на проводе: в метаданные запроса, и её
// подхватывает первое звено цепочки. Раньше тест клал принципала прямо в ctx через
// operations.WithPrincipal, то есть заявленное первое звено не исполнял вовсе — и
// не заметил бы, если бы извлечение принимало личность от кого попало. Пир здесь
// намеренно ЗАКОННЫЙ отправитель: предмет этого файла — кто принимает решение о
// доступе; кому вообще разрешено передавать личность, проверяется отдельно
// (cmd/vpc/trusted_forwarders_test.go).

// forwarderPeerCtx — ctx с пиром, прошедшим проверку клиентского сертификата, чей
// SAN входит в круг доверенных отправителей этого теста.
func forwarderPeerCtx(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	return grpcsrv.WithCertIdentityIn(
		peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{}}}), grpcsrv.NewTrustDomain("kacho.cloud"),
		testForwarderSAN, true)
}

const testForwarderSAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

// chainOutcome — прогон доверенной пары извлечения, AuthN-интерсептора и следом
// authz-интерсептора; сообщает, добрался ли вызов до handler'а.
func chainOutcome(
	t *testing.T,
	productionMode bool,
	md metadata.MD,
	principal *operations.Principal,
	fullMethod string,
	req any,
	modelSays bool,
) (reached bool, err error) {
	t.Helper()

	md = md.Copy()
	if principal != nil {
		md.Set(grpcsrv.MDKeyPrincipalType, principal.Type)
		md.Set(grpcsrv.MDKeyPrincipalID, principal.ID)
		md.Set(grpcsrv.MDKeyPrincipalDisplay, principal.DisplayName)
	}
	ctx := forwarderPeerCtx(t, metadata.NewIncomingContext(context.Background(), md))

	authzIntr := authz.NewInterceptor(authz.InterceptorOptions{
		Cache:       authz.NewCache(0),
		ServiceName: "kacho-vpc-test",
		Map:         check.PermissionMap(),
		Client: authz.CheckClientFunc(func(context.Context, string, string, string) (bool, error) {
			return modelSays, nil
		}),
	})

	handler := func(context.Context, any) (any, error) {
		reached = true
		return nil, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: fullMethod}

	// Порядок ровно тот, что собирает носитель контура: извлечение личности →
	// решение о доступе. Первое звено пары (cert-identity) здесь уже отработало —
	// forwarderPeerCtx кладёт его результат, — поэтому исполняем второе.
	//
	// Отдельного AuthN-стража в цепочке БОЛЬШЕ НЕТ, и это не ослабление: звено
	// решения о доступе само отвергает неназванного вызывающего БЕЗУСЛОВНО —
	// в любом режиме, а не только в боевом, как делал снятый страж. Аргумент
	// productionMode остаётся у helper'а: случаи ниже читаются как «в боевой
	// посадке», и обнулять эту разметку вместе со звеном было бы потерей смысла
	// самих случаев.
	inner := authzIntr.Unary()
	trustIntr := grpcsrv.UnaryTrustedPrincipalExtract(
		grpcsrv.WithTrustedForwarders(grpcsrv.NewTrustedForwarders(testForwarderSAN)))

	_, err = trustIntr(ctx, req, info, func(ctx context.Context, req any) (any, error) {
		return inner(ctx, req, info, handler)
	})
	return reached, err
}

func userPrincipal(id string) *operations.Principal {
	return &operations.Principal{Type: "user", ID: id}
}

// Формы metadata, которые встречаются на проводе и которые вызывающий может
// сочинить сам. Ни одну из них ни один компонент платформы не форвардит:
// cross-service вызовы несут только x-kacho-principal-* (pkg/auth.PropagateOutgoing),
// поэтому «project-claim» и «admin-claim» — это то, что придумал сам звонящий.
var (
	mdBare        = metadata.MD{}
	mdProjectOnly = metadata.MD{"x-kacho-project-id": []string{"prj_f1"}}
	mdAdminOnly   = metadata.MD{"x-kacho-admin": []string{"true"}}
	mdProjectPlus = metadata.MD{
		"x-kacho-project-id": []string{"prj_f1"},
		"x-kacho-admin":      []string{"true"},
	}
)

// Ни одна форма клиентской metadata не открывает cluster-scoped admin-RPC, когда
// модель говорит «нет». Именно это и должен был бы защищать самодельный гейт — и
// именно это уже защищает модель, для каждой формы одинаково.
func TestInternalListener_ClusterScopedAdminRPC_DeniedWheneverModelDenies(t *testing.T) {
	const method = "/kacho.cloud.vpc.v1.InternalAddressPoolService/Create"
	req := &vpcv1.CreateAddressPoolRequest{}

	for name, md := range map[string]metadata.MD{
		"bare":                 mdBare,
		"self-claimed project": mdProjectOnly,
		"self-claimed admin":   mdAdminOnly,
		"both self-claimed":    mdProjectPlus,
	} {
		t.Run(name, func(t *testing.T) {
			reached, err := chainOutcome(t, true, md, userPrincipal("usr_mallory"), method, req, false)
			require.Error(t, err, "модель сказала «нет» — вызов обязан быть отвергнут")
			assert.False(t, reached, "handler не должен быть достигнут")
		})
	}
}

// Симметрично: когда модель говорит «да», ни одна форма клиентской metadata не
// мешает — в том числе её отсутствие. Это ловит противоположную регрессию: гейт,
// который «на всякий случай» режет легитимного вызывающего.
func TestInternalListener_ClusterScopedAdminRPC_AllowedWheneverModelAllows(t *testing.T) {
	const method = "/kacho.cloud.vpc.v1.InternalAddressPoolService/Create"
	req := &vpcv1.CreateAddressPoolRequest{}

	for name, md := range map[string]metadata.MD{
		"bare":                 mdBare,
		"self-claimed project": mdProjectOnly,
		"self-claimed admin":   mdAdminOnly,
		"both self-claimed":    mdProjectPlus,
	} {
		t.Run(name, func(t *testing.T) {
			reached, err := chainOutcome(t, true, md, userPrincipal("usr_admin"), method, req, true)
			require.NoError(t, err)
			assert.True(t, reached, "модель сказала «да» — вызов обязан дойти до handler'а")
		})
	}
}

// Object-scoped internal RPC (IPAM-примитив на конкретном Address) — то же самое:
// решает модель, per-object. Это ребро compute/nlb → vpc, и оно не должно зависеть
// от того, сочинил ли звонящий tenant-заголовки.
func TestInternalListener_ObjectScopedRPC_FollowsModelForEveryMetadataShape(t *testing.T) {
	const method = "/kacho.cloud.vpc.v1.InternalAddressService/AllocateInternalIP"
	req := &vpcv1.AllocateInternalIPRequest{AddressId: "adr_alpha"}

	for name, md := range map[string]metadata.MD{
		"bare":                 mdBare,
		"self-claimed project": mdProjectOnly,
		"self-claimed admin":   mdAdminOnly,
		"both self-claimed":    mdProjectPlus,
	} {
		t.Run(name+"/deny", func(t *testing.T) {
			reached, err := chainOutcome(t, true, md, userPrincipal("usr_mallory"), method, req, false)
			require.Error(t, err)
			assert.False(t, reached)
		})
		t.Run(name+"/allow", func(t *testing.T) {
			reached, err := chainOutcome(t, true, md, userPrincipal("usr_owner"), method, req, true)
			require.NoError(t, err)
			assert.True(t, reached)
		})
	}
}

// Настоящий anonymous (без принципала) на :9091 отвергается в production до всякой
// модели — и сочинённые tenant-заголовки этого не меняют. Заголовок не является
// аутентификацией.
func TestInternalListener_NoPrincipalRejectedInProduction(t *testing.T) {
	const method = "/kacho.cloud.vpc.v1.InternalAddressPoolService/Create"
	req := &vpcv1.CreateAddressPoolRequest{}

	for name, md := range map[string]metadata.MD{
		"bare":               mdBare,
		"self-claimed admin": mdAdminOnly,
	} {
		t.Run(name, func(t *testing.T) {
			reached, err := chainOutcome(t, true, md, nil, method, req, true)
			require.Error(t, err, "без принципала production-mode обязан отвергнуть")
			assert.False(t, reached)
		})
	}
}

// Клиентский plaintext-заголовок не меняет исход НИГДЕ — в том числе на
// internal-листенере. До снятия гейта `x-kacho-admin: true` поднимал «cluster-admin»
// на :9091: любой peer, дотянувшийся до порта, объявлял себя администратором сам, а
// гейт, который такой заголовок «проверяет», — это опция, которую атакующий включает
// себе сам, а не контроль. Одновременно тот же гейт отвергал вызывающего, которому
// модель говорит «да», если тот прислал `x-kacho-project-id` без admin-заголовка.
//
// Лочим по исходу: для одного и того же принципала и одного и того же ответа модели
// ЛЮБАЯ форма сочинённой metadata обязана давать один и тот же результат.
func TestInternalListener_ClientMetadataNeverChangesTheOutcome(t *testing.T) {
	const method = "/kacho.cloud.vpc.v1.InternalNetworkService/GetNetwork"
	req := &vpcv1.GetInternalNetworkRequest{NetworkId: "enp_x"}

	for _, modelSays := range []bool{true, false} {
		baseReached, baseErr := chainOutcome(t, true, mdBare, userPrincipal("usr_x"), method, req, modelSays)
		for name, md := range map[string]metadata.MD{
			"self-claimed project": mdProjectOnly,
			"self-claimed admin":   mdAdminOnly,
			"both self-claimed":    mdProjectPlus,
		} {
			t.Run(name, func(t *testing.T) {
				reached, err := chainOutcome(t, true, md, userPrincipal("usr_x"), method, req, modelSays)
				assert.Equal(t, baseReached, reached,
					"привилегия выдаётся моделью, а не объявляется вызывающим о себе (model=%v)", modelSays)
				assert.Equal(t, status.Code(baseErr), status.Code(err),
					"исход обязан совпадать с вызовом без сочинённых заголовков (model=%v)", modelSays)
			})
		}
	}
}
