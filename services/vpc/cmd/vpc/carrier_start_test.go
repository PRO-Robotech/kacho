// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// carrier_start_test.go — vpc проходит ВСЕ отказы старта носителя, а не только
// те, что соседняя проба умеет повторить у себя.
//
// # Почему без неё дескриптор мог нести ЛОЖНОЕ заявление
//
// `describe_test.go` спрашивает КОНСТРУКТОР дескриптора: он судит поля по себе —
// объявлена ли ось, положительна ли величина, сходится ли форма с производителем.
// Половина отказов носителя так не проверяется: они существуют только там, где
// есть СЛУЖИМЫЙ НАБОР, снятый у самих серверов после регистрации, — метод без
// строки каталога (О2), домен без единой записи карты (О9), проводка сужателя
// против каталога в обе стороны (О3/О4), скрытие существования против предиката
// РАНТАЙМА (О5/О5б) и служимый серверный стрим против оси его срока (О11).
//
// Именно эта дыра дала ложное заявление у двух соседей: дескриптор объявлял ось
// скрытия неприменимой словами «в каталоге нет строк hide_existence» — верно про
// аннотации и ложно про исполнение, — и заявление проходило лишь потому, что
// судья читал не тот предикат, а проверить его было негде. У vpc аннотаций тоже
// ноль, а скрывает он на семи `/Get`; эта проба и есть место, где такое
// расхождение краснеет.
//
// # Что здесь регистрируется
//
// РЕАЛЬНЫЕ регистраторы обоих слушателей (`registerPublicServices` /
// `registerInternalServices`) с пустыми обработчиками: предмет — служимый НАБОР
// RPC, снятый носителем у самих серверов, а не поведение обработчиков. Подменить
// регистрацию своим списком значило бы проверять то, что написали в пробе, а не
// то, что процесс служит.
//
// Порты эфемерные: проба обязана быть детерминированной, а фиксированный номер
// сделал бы её заложницей занятости машины прогона.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	addressapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/address"
	addresspoolapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/addresspool"
	gatewayapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/gateway"
	networkapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/network"
	niapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/networkinterface"
	routetableapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/routetable"
	sgapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/securitygroup"
	subnetapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/subnet"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/services/addressref"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/services/networkinternal"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/services/nicinternal"

	cidrgroupapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/cidrgroup"
)

// emptyServices — набор обработчиков в нулевой форме.
//
// Регистраторы принимают структуру целиком, поэтому она обязана быть заполнена
// объектами нужных типов; вызывать их проба не будет — носитель гасит слушатели
// раньше первого соединения. Нулевые указатели здесь законны ровно потому, что
// предмет пробы — ИМЕНА служимых методов, которые grpc-go снимает с дескрипторов
// служб, а не тела обработчиков.
func emptyServices() *services {
	return &services{
		networkHandler:           &networkapp.Handler{},
		subnetHandler:            &subnetapp.Handler{},
		addressHandler:           &addressapp.Handler{},
		addressAllocate:          &addressapp.AllocateUseCase{},
		addressRefService:        &addressref.Service{},
		routeTableHandler:        &routetableapp.Handler{},
		securityGroupHandler:     &sgapp.Handler{},
		gatewayHandler:           &gatewayapp.Handler{},
		addressPoolHandler:       &addresspoolapp.Handler{},
		addressPoolPublic:        &addresspoolapp.PublicHandler{},
		networkInternal:          &networkinternal.Service{},
		networkInterfaceHandler:  &niapp.Handler{},
		networkInterfaceInternal: &nicinternal.Service{},
		cidrGroupHandler:         &cidrgroupapp.Handler{},
	}
}

// TestCarrierRaisesVPCWithoutAStartRefusal — исход: носитель поднимает vpc и не
// находит расхождения между объявленным и служимым.
//
// Контекст отменён ЗАРАНЕЕ: предмет пробы — отказы, которые носитель считает ДО
// первого соединения. Отменённый контекст гасит слушатели сразу после того, как
// отказы отработали, поэтому проба не держит сокета и не ждёт сети.
func TestCarrierRaisesVPCWithoutAStartRefusal(t *testing.T) {
	cfg, mtls := describeCfg(t)
	// Эфемерные порты: слушатели поднимутся и тут же погаснут.
	cfg.APIServer.Endpoint = "tcp://127.0.0.1:0"
	cfg.APIServer.InternalEndpoint = "tcp://127.0.0.1:0"

	// Журнал носителя читается, а не выбрасывается: перепись осмотренного он
	// печатает ВСЕГДА, и без неё «отказов нет» неотличимо от «ничего не осмотрено».
	var log strings.Builder
	logger := slog.New(slog.NewTextHandler(&log, nil))

	// Приёмник величин кеша вердиктов — НАСТОЯЩИЙ, а не заглушка: предмет здесь
	// не «поле заполнено», а «носитель позвал приёмник И отдал читателя того
	// кеша, который спрашивает звено». Проба, принимающая заглушку, осталась бы
	// зелёной на носителе, который приёмник не зовёт вовсе.
	var authzCacheReader func() authz.Metrics
	observeAuthzCache := func(read func() authz.Metrics) { authzCacheReader = read }

	desc, err := describe(cfg, mtls, logger, buildListFilter(cfg, nil, logger),
		bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"}), probeExistence{},
		observeAuthzCache, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор отвергнут конструктором — процесс не поднялся бы:\n%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svcs := emptyServices()
	subscribe, serr := buildSubscriptionServer(cfg, narrowtest.AllowingAll(), discardLogger())
	if serr != nil {
		t.Fatalf("сервер потока не собрался — процесс не поднялся бы: %v", serr)
	}
	opsRepo := operations.NewRepo(nil, "kacho_vpc")
	serveErr := servicehost.Serve(ctx, desc,
		func(reg grpc.ServiceRegistrar) { registerPublicServices(reg, svcs, opsRepo) },
		func(reg grpc.ServiceRegistrar) { registerInternalServices(reg, svcs, subscribe) },
	)
	if serveErr != nil && strings.Contains(serveErr.Error(), "не поднимается") {
		t.Fatalf("носитель ОТКАЗАЛ vpc в старте — на стенде процесс не поднялся бы:\n%v", serveErr)
	}
	// «сервер остановлен» — законный исход ОТМЕНЁННОГО контекста, а не отказ.
	// Носитель успел собрать оба сервера и погасить их; предмет пробы — отказы,
	// которые считаются ДО этого, и они не сработали. Прочие ошибки — настоящие.
	//
	// Различать обязательно: приняв любую ошибку за норму, проба перестала бы
	// отличать «отказов нет» от «подъём вообще не состоялся», а именно это она и
	// проверяет.
	if serveErr != nil && !strings.Contains(serveErr.Error(), "server has been stopped") {
		t.Fatalf("носитель вернул ошибку подъёма: %v", serveErr)
	}

	// Величины кеша вердиктов вышли из процесса: без этого доля попаданий не
	// наблюдается, и «кеш не попадает ни разу» снаружи неотличимо от «кеш
	// поглощает весь поток».
	if authzCacheReader == nil {
		t.Fatal("носитель не отдал читателя величин кеша положительных вердиктов — " +
			"доля попаданий не выходит из процесса")
	}
	if s := authzCacheReader().Cache; s.Hits != 0 || s.Misses != 0 {
		t.Fatalf("до первого вызова окно вердиктов не спрашивали, а счётчики не нулевые: %+v", s)
	}

	census := log.String()
	if strings.Contains(census, "методов 0") {
		t.Fatalf("отказы старта осмотрели НОЛЬ методов — вердикт получен на пустом наборе:\n%s", census)
	}
	// Скрытие существования у vpc ЕСТЬ, и перепись обязана это показывать: «ноль
	// скрывающих типов» означало бы, что судья снова читает не тот предикат.
	if strings.Contains(census, "скрывающих типов 0") {
		t.Fatalf("перепись носителя утверждает, что vpc ничего не скрывает, — предикат рантайма "+
			"говорит обратное (семь /Get скрывают по форме):\n%s", census)
	}
	t.Logf("перепись носителя: %s", strings.TrimSpace(census))
}
