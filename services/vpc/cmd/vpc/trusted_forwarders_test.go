// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Кто вправе ГОВОРИТЬ ЗА пользователя.
//
// Личность конечного пользователя приезжает заголовками `x-kacho-principal-*`, и
// сервис принимает их не от кого попало, а от перечисленного круга отправителей.
// Сертификат внутреннего CA доказывает, ЧЕЙ это пир, и НЕ даёт права
// представляться другим — это решает список.
//
// Тесты гоняют ТУ ЖЕ пару звеньев, что уходит в оба листенера (principalExtract*),
// в том же порядке, и утверждают на НАБЛЮДАЕМОМ исходе: дошёл ли вызов до
// handler'а и КОГО модель прав увидела субъектом. «Функция вызвана» здесь ничего
// не доказывает — доказывает то, что получает вызывающий.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

const (
	sanGateway   = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"
	sanNeighbour = "spiffe://kacho.cloud/ns/kacho/sa/kacho-registry"
)

// certPeerCtx — ctx с пиром, ПРОШЕДШИМ проверку клиентского сертификата, чей SAN
// равен san. Сертификат собирается на месте: доверенная пара читает готовую
// verified-цепочку из peer-инфо, поэтому живое TLS-рукопожатие для проверки её
// решения не нужно и только добавило бы недетерминизма.
func certPeerCtx(t *testing.T, ctx context.Context, san string) context.Context {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	u, err := url.Parse(san)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "peer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		URIs:         []*url.URL{u},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return peer.NewContext(ctx, &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{leaf}},
		}},
	})
}

// victimHeaders — метаданные, которые пир пишет сам: он объявляет, что звонит от
// имени жертвы. Ровно та форма, которую край ставит законному запросу.
func victimHeaders() metadata.MD {
	return metadata.MD{
		grpcsrv.MDKeyPrincipalType:    []string{"user"},
		grpcsrv.MDKeyPrincipalID:      []string{"usr_victim"},
		grpcsrv.MDKeyPrincipalDisplay: []string{"victim@example.com"},
	}
}

// chainOutcome прогоняет РЕАЛЬНУЮ пару извлечения + AuthN-страж + интерсептор
// авторизации и сообщает наблюдаемое: дошёл ли вызов до handler'а, с какой ошибкой
// и КОГО модель увидела субъектом.
func chainOutcome(t *testing.T, ctx context.Context, forwarders grpcsrv.TrustedForwarders) (
	reached bool, err error, askedSubjects []string,
) {
	t.Helper()

	const method = "/kacho.cloud.vpc.v1.InternalNetworkService/GetNetwork"
	req := &vpcv1.GetInternalNetworkRequest{NetworkId: "enp_x"}
	info := &grpc.UnaryServerInfo{FullMethod: method}

	authzIntr := authz.NewInterceptor(authz.InterceptorOptions{
		Cache:       authz.NewCache(0),
		ServiceName: "kacho-vpc-test",
		Map:         check.PermissionMap(),
		Client: authz.CheckClientFunc(func(_ context.Context, subject, _, _ string) (bool, error) {
			askedSubjects = append(askedSubjects, subject)
			// Модель говорит «да» жертве — она действительно владелец. Весь
			// вопрос в том, ЗА КОГО спросили.
			return subject == "user:usr_victim", nil
		}),
	})

	final := func(context.Context, any) (any, error) {
		reached = true
		return nil, nil
	}

	// Порядок ровно тот, что собирает носитель контура: пара извлечения → решение
	// о доступе. Сворачиваем справа налево, чтобы первым исполнялось первое звено.
	//
	// Отдельного AuthN-стража между ними больше нет: звено решения о доступе само
	// отвергает неназванного вызывающего безусловно (в любом режиме), тогда как
	// снятый страж делал это только в боевом. Случаи ниже от этого не изменились —
	// они утверждают на исходе, а исход прежний.
	chain := grpcsrv.PrincipalExtractUnary(grpcsrv.NewTrustDomain("kacho.cloud"), forwarders)
	next := func(ctx context.Context, req any) (any, error) {
		return authzIntr.Unary()(ctx, req, info, final)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		intr, downstream := chain[i], next
		next = func(ctx context.Context, req any) (any, error) {
			return intr(ctx, req, info, downstream)
		}
	}
	_, err = next(ctx, req)
	return reached, err, askedSubjects
}

// Сосед с законным сертификатом внутреннего CA не может говорить за жертву.
//
// Такой сертификат есть у КАЖДОГО служебного пода, а публичный листенер открыт
// всему пространству имён. Если переданная им личность принимается, он читает,
// меняет и удаляет ресурсы жертвы — модель отвечает «да», потому что жертва
// действительно владелец. Наблюдаемое требование: отказ, а не тихий проход.
func TestPrincipalChain_NeighbourWithValidCertCannotSpeakForAUser(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), victimHeaders())
	ctx = certPeerCtx(t, ctx, sanNeighbour)

	reached, err, asked := chainOutcome(t, ctx, grpcsrv.NewTrustedForwarders(sanGateway))

	require.Error(t, err, "пир вне круга отправителей не вправе говорить за пользователя")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, reached, "handler не должен быть достигнут")
	assert.NotContains(t, asked, "user:usr_victim",
		"личность жертвы не должна становиться субъектом проверки прав")
}

// Симметричный контроль: законный отправитель работает по-прежнему. Без него
// предыдущий тест зеленел бы и от «сломали передачу личности вообще».
func TestPrincipalChain_ListedForwarderSpeaksForTheUser(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), victimHeaders())
	ctx = certPeerCtx(t, ctx, sanGateway)

	reached, err, asked := chainOutcome(t, ctx, grpcsrv.NewTrustedForwarders(sanGateway))

	require.NoError(t, err)
	assert.True(t, reached, "законный отправитель обязан доносить личность до модели")
	assert.Contains(t, asked, "user:usr_victim")
}

// Круг сужается ПО ФАКТИЧЕСКИМ отправителям, а не по догадке «наверное только
// шлюз»: у vpc личность конечного пользователя законно передают ещё compute
// (привязка интерфейса, резолв подсети), nlb (резолв подсети/адреса/группы) и
// Сузив список до одного шлюза, мы бы сломали их — и это проверяется исходом, а не
// чтением values. Здесь стоял ещё один отправитель — компонент, которого в дереве
// нет и репозитория под который не существует; снят решением владельца 2026-08-09,
// потому что круг пинится по ФАКТИЧЕСКИМ отправителям.
func TestPrincipalChain_EveryListedSenderIsAccepted(t *testing.T) {
	senders := []string{
		sanGateway,
		"spiffe://kacho.cloud/ns/kacho/sa/kacho-compute",
		"spiffe://kacho.cloud/ns/kacho/sa/kacho-nlb",
	}
	for _, san := range senders {
		t.Run(san, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), victimHeaders())
			ctx = certPeerCtx(t, ctx, san)

			reached, err, asked := chainOutcome(t, ctx, grpcsrv.NewTrustedForwarders(senders...))
			require.NoError(t, err)
			assert.True(t, reached)
			assert.Contains(t, asked, "user:usr_victim")
		})
	}
}

// Пустой список — это «НЕ СУЖАЕМ», а не «запрещаем»: contract corelib сверяет
// сертификат со списком только когда список непуст. Тест фиксирует именно это
// свойство, потому что ровно из-за него нужна стража старта: без неё сервис
// поднимается, доверяя всем, и выглядит исправным.
func TestPrincipalChain_EmptyListNarrowsNothing(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), victimHeaders())
	ctx = certPeerCtx(t, ctx, sanNeighbour)

	reached, err, asked := chainOutcome(t, ctx, grpcsrv.TrustedForwarders{})

	require.NoError(t, err, "пустой список принимает личность от любого проверенного пира")
	assert.True(t, reached)
	assert.Contains(t, asked, "user:usr_victim")
}

// Пир БЕЗ проверенного клиентского сертификата не проходит, даже если написал
// заголовки: доверие — свойство отправителя, а не запроса.
func TestPrincipalChain_TLSPeerWithoutVerifiedCertIsNotTrusted(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), victimHeaders())
	ctx = peer.NewContext(ctx, &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{}},
	})

	reached, err, asked := chainOutcome(t, ctx, grpcsrv.NewTrustedForwarders(sanGateway))

	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, reached)
	assert.NotContains(t, asked, "user:usr_victim")
}
