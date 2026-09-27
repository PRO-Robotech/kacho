// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// carrier_start_test.go — compute проходит ВСЕ отказы старта носителя, а не
// только те, что соседняя проба умеет повторить у себя.
//
// # Почему без неё дескриптор мог нести ЛОЖНОЕ заявление
//
// `describe_test.go` спрашивает КОНСТРУКТОР дескриптора: он судит поля по себе —
// объявлена ли ось, положительна ли величина, сходится ли форма с производителем.
// Половина отказов носителя так не проверяется: они существуют только там, где
// есть СЛУЖИМЫЙ НАБОР, снятый у самих серверов после регистрации, — метод без
// строки каталога, домен без единой записи карты, служимый стрим, и сверка осей
// с каталогом в обе стороны.
//
// Именно эта дыра дала ложное заявление у двух соседей подряд: дескриптор
// объявлял ось скрытия неприменимой, ссылаясь на отсутствие аннотаций, тогда как
// рантайм скрывает и ПО ФОРМЕ, — и заявление проходило лишь потому, что судья
// читал не тот предикат, а проверить его было негде.
//
// Порты эфемерные: проба обязана быть детерминированной, а фиксированный номер
// сделал бы её заложницей занятости машины прогона.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/outbox/bootgate"
	"github.com/PRO-Robotech/corelib/servicehost"

	"github.com/PRO-Robotech/kacho/internal/carrierprobe"
)

// TestCarrierRaisesComputeWithoutAStartRefusal — исход: носитель поднимает
// compute и не находит расхождения между объявленным и служимым.
//
// Контекст отменён ЗАРАНЕЕ: предмет пробы — отказы, которые носитель считает ДО
// первого соединения. Отменённый контекст гасит слушатели сразу после того, как
// отказы отработали: проба держит только сокеты на портах, назначенных ядром,
// и лишь до гашения, а сети не ждёт. Что порты эфемерны, утверждает страж
// `carrierprobe.RequireKernelAssigned`, а не этот комментарий.
func TestCarrierRaisesComputeWithoutAStartRefusal(t *testing.T) {
	desc, err := describeWith(t, describeCfg())
	if err != nil {
		t.Fatalf("дескриптор отвергнут конструктором — процесс не поднялся бы:\n%v", err)
	}
	// Страж предусловия — ДО носителя и по адресам ДЕСКРИПТОРА, то есть ровно
	// по тому, что получит net.Listen: ручка, не доехавшая до разбора,
	// краснит здесь текстом «условие не создано», а не соседним стендом.
	carrierprobe.RequireKernelAssigned(t, desc.Spec(), "config.GrpcPort", "config.InternalGrpcPort")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	regs := registrarsOfBothListeners()
	serveErr := servicehost.Serve(ctx, desc, regs[0], regs[1])
	// Исход подъёма судит общий пакет: отказ носителя — красное, слушатель,
	// не поднявшийся на порту, — «условие не создано», прочее — красное с
	// текстом. Три исхода различимы, и различие не зависит от того, какую
	// строку вернул носитель на штатном гашении.
	carrierprobe.RequireRaised(t, "compute", serveErr)
}

// TestCarrierCensusIsNotEmptyForCompute — перепись осмотренного не пуста.
//
// Отдельная проба, а не строка в предыдущей: «отказов нет» и «ничего не
// осмотрено» обязаны быть различимы, и различие это утверждается, а не
// подразумевается. Дескриптор здесь тот же, а логгер — свой, чтобы читать
// перепись именно этого подъёма.
func TestCarrierCensusIsNotEmptyForCompute(t *testing.T) {
	var log strings.Builder
	logger := slog.New(slog.NewTextHandler(&log, nil))

	cfg := describeCfg()
	// Приёмник величин кеша вердиктов — НАСТОЯЩИЙ, а не заглушка: предмет здесь
	// не «поле заполнено», а «носитель позвал приёмник И отдал читателя того
	// кеша, который спрашивает звено». Проба, принимающая заглушку, осталась бы
	// зелёной на носителе, который приёмник не зовёт вовсе.
	var authzCacheReader func() authz.Metrics
	observeAuthzCache := func(read func() authz.Metrics) { authzCacheReader = read }

	desc, err := describe(cfg, logger, buildListFilter(cfg, nil, logger),
		bootgate.New(bootgate.Config{RequireIAM: cfg.RequireIAM, Service: "kacho-compute"}), probeExistence{},
		observeAuthzCache, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	carrierprobe.RequireKernelAssigned(t, desc.Spec(), "config.GrpcPort", "config.InternalGrpcPort")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	regs := registrarsOfBothListeners()
	// Исход подъёма здесь СУДИТСЯ, а не выбрасывается: перепись, снятая с
	// подъёма, который не состоялся, была бы переписью ничего.
	carrierprobe.RequireRaised(t, "compute", servicehost.Serve(ctx, desc, regs[0], regs[1]))

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
	// Предикат читает ПОЛЕ переписи, а не подстроку: носитель печатает
	// «осмотрено: методов N (…), …, сужаемых методов M, …», и поиск «методов 0»
	// совпадал с ХВОСТОМ строки — с числом сужаемых. Пока сужаемые у сервиса
	// были, проба зеленела; их стало ноль (поток журнала снят) — и она
	// покраснела на 38 осмотренных методах, объявив пустым непустое.
	if strings.Contains(census, "осмотрено: методов 0") {
		t.Fatalf("отказы старта осмотрели НОЛЬ методов — вердикт получен на пустом наборе:\n%s", census)
	}
	if !strings.Contains(census, "census") {
		t.Fatalf("носитель не напечатал переписи осмотренного — «ноль находок» неотличимо от "+
			"«ноль прочитанного»:\n%s", census)
	}
	t.Logf("перепись носителя: %s", strings.TrimSpace(census))
}
