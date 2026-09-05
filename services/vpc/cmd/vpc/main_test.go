// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Тесты wiring composition-root: проверяют enable/disable/no-conn контракт
// per-object FGA-фильтра (`buildListFilter` поверх AuthorizeService.BatchCheck), а
// статический guard ниже держит каждый vpc→iam dial протянутым через client-cert mTLS.

import (
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
)

// TestLROWorker_ReadyAfterBootWiring — composition root проводит package-level
// default-registry LRO-worker'а (ConfigureDefault + Start) ДО приема трафика:
// readiness-чекер lro-worker зеленый без единой мутации. Без этой проводки под в
// k8s залипал бы NotReady (нет трафика → нет Run → dispatcher лениво не стартует →
// NotReady навсегда — boot-deadlock).
func TestLROWorker_ReadyAfterBootWiring(t *testing.T) {
	require.False(t, operations.Ready(), "до boot-wiring default-registry dispatcher не запущен")
	require.NoError(t, startLROWorker(operations.NewMemRecorder(), discardLogger()))
	require.True(t, operations.Ready(),
		"после boot-wiring operations.Ready()=true — readiness lro-worker зеленый до трафика")
}

// dialLoopback — возвращает closed-loop grpc-conn (живой сервер не нужен:
// buildListFilter только конструирует FGAFilter поверх conn, но не вызывает его).
func dialLoopback(t *testing.T) *grpc.ClientConn {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Выключенный фильтр БОЛЬШЕ НЕ ЗНАЧИТ сквозной проход: сужатель собирается всегда и
// отказывает, пока ему не с кем говорить. Прежняя редакция возвращала здесь nil, и
// use-case'ы трактовали его как «сужение выключено, страницу отдать» — то есть
// посадка без модели показывала каждому участнику проекта каждую его строку.
func TestBuildListFilter_Disabled_StillNarrowsByRefusing(t *testing.T) {
	var cfg config.Config
	cfg.AuthZ.ListFilter.Enabled = false
	f := buildListFilter(cfg, dialLoopback(t), discardLogger())
	require.NotNil(t, f, "сужатель собирается всегда — отсутствие модели не отменяет вопроса")

	_, err := listnarrow.IDs(narrowtest.Caller(), f, "vpc_subnet", "vpc.subnets.list", []string{"sub_a"})
	require.Error(t, err, "спросить негде — значит отказ, а не «да»")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, f.Narrows(), "и это состояние обязано быть ЧИТАЕМЫМ снаружи")
}

func TestBuildListFilter_EnabledNilConn_StillNarrowsByRefusing(t *testing.T) {
	var cfg config.Config
	cfg.AuthZ.ListFilter.Enabled = true
	f := buildListFilter(cfg, nil, discardLogger())
	require.NotNil(t, f)

	_, err := listnarrow.IDs(narrowtest.Caller(), f, "vpc_subnet", "vpc.subnets.list", []string{"sub_a"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// Отказ на отсутствующей модели прав НЕ СНИМАЕТСЯ настройкой.
//
// Здесь стояла проба аварийного пропуска: она выставляла ручку и утверждала, что
// страница уходит несуженной, а срабатывание считается. Ручка снята (её имя — в
// `retired_knobs_test.go`): её предмет — «посадка без модели» — недостижим, потому
// что на выключенном фильтре и на нерезолвимом адресе процесс не поднимается
// вовсе. Проба перевёрнута и утверждает то, что осталось верным: пропуска нет ни
// при какой настройке, и счётчик аварийных проходов остаётся нулевым.
//
// Прочитанный ноль — часть утверждения: он отличает «пропуска не было» от
// «счётчика нет вовсе» (счётчик живёт в общем сужателе и продолжает существовать
// для тех, у кого такой режим есть).
func TestBuildListFilter_NoRightsModel_RefusesAndNoKnobWaivesIt(t *testing.T) {
	var cfg config.Config
	cfg.AuthZ.ListFilter.Enabled = false
	f := buildListFilter(cfg, nil, discardLogger())
	require.NotNil(t, f)
	require.Zero(t, f.Counts().Breakglass, "прочитанный ноль отличает «не было» от «счётчика нет»")

	got, err := listnarrow.IDs(narrowtest.Caller(), f, "vpc_subnet", "vpc.subnets.list", []string{"sub_a"})
	require.Error(t, err, "спросить негде — значит отказ; настройки, дающей проход, больше нет")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, got, "отказ не отдаёт страницу")
	assert.Zero(t, f.Counts().Breakglass,
		"аварийных проходов не было и быть не может: у vpc такого режима нет")
}

// Парный положительный контроль к трём отрицаниям выше: на ЗАКОННОЙ посадке
// (фильтр включён, соединение есть) сужатель действительно сужает. Без него все
// три отрицания зеленели бы на сужателе, который отказывает всегда, — то есть на
// сломанном чтении.
func TestBuildListFilter_EnabledWithConn_Narrows(t *testing.T) {
	var cfg config.Config
	cfg.AuthZ.ListFilter.Enabled = true
	f := buildListFilter(cfg, dialLoopback(t), discardLogger())
	require.NotNil(t, f, "enabled + conn → FGA per-object filter")
	assert.True(t, f.Narrows(),
		"сужение обязано быть ЧИТАЕМЫМ снаружи и включённым: иначе отрицания выше не отличают "+
			"«отказал, потому что не с кем говорить» от «отказывает всегда»")
}

// TestSECI_CompletenessGuard_EveryIAMDialThreadsClientCreds — статический
// completeness-gate. Composition root ОБЯЗАН протянуть per-edge client-cert mTLS
// creds в КАЖДЫЙ vpc→iam dial: public ProjectService.Get conn (`iamConn`) и internal
// InternalIAMService.Check conn (`authzConn`, общий с list-filter). Если любой
// read/authz iam-dial оставить server-auth-only/plaintext, то, когда kaname
// требует и проверяет client-cert, handshake падает — guard запрещает эту регрессию,
// проверяя, что оба dial'а консультируются с mTLS-хелперами.
//
// Все четыре peer-dial'а идут через общий хелпер `dialPeer`, которому per-edge
// creds-функция передается значением (`mtlsCfg.IAM*ClientCreds`, без вызова на
// call-site); сам вызов `credsFn()` — внутри `dialPeer`. Guard проверяет и то, что
// creds-хелперы протянуты в каждый edge, и то, что `dialPeer` их действительно
// вызывает (иначе creds были бы «протянуты, но не предъявлены»).
func TestSECI_CompletenessGuard_EveryIAMDialThreadsClientCreds(t *testing.T) {
	src, err := os.ReadFile("main.go")
	require.NoError(t, err)
	main := string(src)

	for _, want := range []string{
		// Ребро ProjectService.Get (iamConn) консультируется с IAM-project mTLS-хелпером.
		"mtlsCfg.IAMProjectMTLS.Enable",
		"mtlsCfg.IAMProjectClientCreds",
		// Ребро Check + list-filter (authzConn) консультируется с IAM-authz mTLS-хелпером.
		"mtlsCfg.IAMAuthzMTLS.Enable",
		"mtlsCfg.IAMAuthzClientCreds",
		// dialPeer действительно предъявляет переданную creds-функцию (вызывает её).
		"creds, err := credsFn()",
	} {
		require.Contains(t, main, want,
			"composition root must thread client-cert mTLS into every vpc→iam read/authz dial; missing %q", want)
	}

	// Ребро register-drainer остается на mTLS через свой хелпер — без регрессии.
	require.Contains(t, main, "IAMRegisterClientCreds()",
		"register-drainer edge must keep its mTLS helper")

	// Защита от старого server-auth-only bool-пути на read/authz dial'ах: ни iamConn,
	// ни authzConn не должны дилить только с `TLS: ...IAM.TLS.Enable` /
	// `TLS: ...IAMTLS.Enable`, когда соответствующее mTLS-ребро включено. Проверяем,
	// что creds-хелпер текстуально предшествует bool-TLS-пути iamConn edge'а.
	require.Less(t,
		strings.Index(main, "mtlsCfg.IAMProjectClientCreds"),
		strings.LastIndex(main, "iamPeer.TLS.Enable"),
		"IAMProjectClientCreds mTLS branch must guard the iamConn insecure/server-auth fallback")
}
