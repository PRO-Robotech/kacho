// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package grpcsrv_test

// principal_display_wire_test.go — отображаемое имя принципала пересекает
// провод НЕИСКАЖЁННЫМ при любом алфавите (#873).
//
// Продукт русскоязычный, поле имени — свободный ввод, поэтому кириллица в нём
// не ошибка пользователя, а ожидаемый ввод. Транспорт gRPC допускает в значении
// обычного metadata-ключа только печатаемый ASCII, поэтому имя обязано ехать
// закодированным, а извлечение — возвращать исходную строку.
//
// Проба утверждает ИСХОД на проводе (вызов состоялся, сервер увидел ту же
// строку), а не наличие кодека в коде: кодек, не провязанный в производителя,
// оставил бы её красной.

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// callHealthPropagating дозванивается по bufconn, положив принципала на
// исходящий ctx ТЕМ ЖЕ производителем, которым пользуется прод-код.
func callHealthPropagating(t *testing.T, dialer func(context.Context, string) (net.Conn, error), p operations.Principal) error {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(dialer), mustClientCreds(t, grpcclient.TLSClient{Enable: false}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = auth.PropagateOutgoing(operations.WithPrincipal(ctx, p))

	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	return err
}

// --- кириллическое имя доезжает целым, а не роняет вызов.
func TestIssue873_CyrillicDisplayNameSurvivesTheWire(t *testing.T) {
	const cyrillic = "Демо Пользователь"

	cap := &captured{}
	dialer := serveBufconnNet(t, cap, mustServerCreds(t, grpcsrv.TLSServer{Enable: false}))

	err := callHealthPropagating(t, dialer, operations.Principal{
		Type: "user", ID: "usr-demo", DisplayName: cyrillic,
	})
	require.NoError(t, err, "кириллическое отображаемое имя не имеет права ронять вызов")

	_, _, p, _ := cap.get()
	require.Equal(t, cyrillic, p.DisplayName,
		"имя обязано доехать неискажённым, а не просто перестать ронять вызов")
	require.Equal(t, "usr-demo", p.ID)
}

// --- положительный контроль: латиница едет как сегодня, БАЙТ В БАЙТ.
//
// Без него зелень предыдущей пробы была бы совместима с кодеком, который
// экранирует всё подряд: старый читатель на другом конце выкатки увидел бы
// экранированную строку в поле контракта. Здесь утверждается, что для обычного
// печатаемого ASCII кодирование — тождество.
func TestIssue873_ASCIIDisplayNameUnchangedOnTheWire(t *testing.T) {
	const ascii = "Demo User"

	cap := &captured{}
	dialer := serveBufconnNet(t, cap, mustServerCreds(t, grpcsrv.TLSServer{Enable: false}))

	// то, что кладёт производитель на провод
	outCtx := auth.PropagateOutgoing(operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr-demo", DisplayName: ascii}))
	md, ok := metadata.FromOutgoingContext(outCtx)
	require.True(t, ok)
	require.Equal(t, []string{ascii}, md.Get(grpcsrv.MDKeyPrincipalDisplay),
		"печатаемый ASCII обязан ехать байт в байт — иначе выкатка портит поле контракта")

	err := callHealthPropagating(t, dialer, operations.Principal{
		Type: "user", ID: "usr-demo", DisplayName: ascii,
	})
	require.NoError(t, err)
	_, _, p, _ := cap.get()
	require.Equal(t, ascii, p.DisplayName)
}

// --- контроль, что проба меряет НАСТОЯЩЕЕ ограничение транспорта, а не заглушку.
//
// Сырая кириллица, положенная в metadata мимо производителя, обязана быть
// отвергнута транспортом. Если однажды gRPC это разрешит, проба выше станет
// вакуумной — и покраснеет ЗДЕСЬ, назвав причину.
func TestIssue873_RawNonASCIIStillRejectedByTransport(t *testing.T) {
	cap := &captured{}
	dialer := serveBufconnNet(t, cap, mustServerCreds(t, grpcsrv.TLSServer{Enable: false}))

	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(dialer), mustClientCreds(t, grpcclient.TLSClient{Enable: false}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		grpcsrv.MDKeyPrincipalType, "user",
		grpcsrv.MDKeyPrincipalID, "usr-demo",
		grpcsrv.MDKeyPrincipalDisplay, "Демо Пользователь",
	))
	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	require.Error(t, err, "транспорт обязан отвергать непечатаемый ASCII в обычном ключе")
}
