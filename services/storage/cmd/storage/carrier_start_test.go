// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// carrier_start_test.go — storage проходит ВСЕ отказы старта носителя, а не
// только те, что соседняя проба умеет повторить у себя.
//
// # Почему без неё дескриптор мог нести ЛОЖНОЕ заявление
//
// `describe_test.go` спрашивает КОНСТРУКТОР дескриптора: он судит поля по себе —
// объявлена ли ось, положительна ли величина, сходится ли форма с производителем.
// Половина отказов носителя так не проверяется: они существуют только там, где
// есть СЛУЖИМЫЙ НАБОР, снятый у самих серверов после регистрации, — метод без
// строки каталога, домен без единой записи карты, служимый стрим, и сверка осей
// с каталогом.
//
// Именно эта дыра дала ложное заявление. Дескриптор объявлял ось скрытия
// неприменимой словами «отказ на чужом объекте приходит отказом в доступе, а не
// промахом владельца» — вторая половина неверна: три `/Get` (Volume, Snapshot,
// Image) скрывают существование ПО ФОРМЕ (глагол чтения плюс голос владельца в
// таблице промахов), и заявление проходило лишь потому, что судья читал не тот
// предикат, а проверить его было негде.
//
// Порты эфемерные: проба обязана быть детерминированной, а фиксированный номер
// сделал бы её заложницей занятости машины прогона. Само по себе объявление
// эфемерного порта этого не даёт — доехать до разбора конфигурации обязано ИМЯ
// ручки, и что оно доехало, утверждает страж предусловия
// (`carrierprobe.RequireKernelAssigned`), а не комментарий.

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/servicehost"

	"github.com/PRO-Robotech/kacho/internal/carrierprobe"
)

// TestCarrierRaisesStorageWithoutAStartRefusal — исход: носитель поднимает
// storage и не находит расхождения между объявленным и служимым.
//
// Контекст отменён ЗАРАНЕЕ: предмет пробы — отказы, которые носитель считает ДО
// первого соединения. Отменённый контекст гасит слушатели сразу после того, как
// отказы отработали: проба держит только сокеты на портах, назначенных ядром,
// и лишь до гашения, а сети не ждёт. Что порты эфемерны, утверждает страж
// `carrierprobe.RequireKernelAssigned`, а не этот комментарий.
func TestCarrierRaisesStorageWithoutAStartRefusal(t *testing.T) {
	cfg := bootConfig(t, map[string]string{
		"KACHO_STORAGE_GRPC_PORT": "0",
		// Имя внутреннего слушателя — `KACHO_STORAGE_INTERNAL_PORT`, без
		// `GRPC`: так оно объявлено в описателе настроек и так же рендерится
		// ConfigMap'ом. Здесь стояло `KACHO_STORAGE_INTERNAL_GRPC_PORT` — имя, не
		// читаемое ничем, — и внутренний слушатель молча оставался на 9091 (#2678).
		"KACHO_STORAGE_INTERNAL_PORT": "0",
	})

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

	desc, err := describe(cfg, logger, buildListFilter(cfg, nil, logger), probeExistence{}, observeAuthzCache, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор отвергнут конструктором — процесс не поднялся бы:\n%v", err)
	}
	// Страж предусловия — ДО носителя и по адресам ДЕСКРИПТОРА, то есть ровно
	// по тому, что получит net.Listen: ручка, не доехавшая до разбора,
	// краснит здесь текстом «условие не создано», а не соседним стендом.
	carrierprobe.RequireKernelAssigned(t, desc.Spec(), "KACHO_STORAGE_GRPC_PORT", "KACHO_STORAGE_INTERNAL_PORT")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	regs := registrarsOfBothListeners(t)
	serveErr := servicehost.Serve(ctx, desc, regs[0], regs[1])
	// Исход подъёма судит общий пакет: отказ носителя — красное, слушатель,
	// не поднявшийся на порту, — «условие не создано», прочее — красное с
	// текстом. Три исхода различимы, и различие не зависит от того, какую
	// строку вернул носитель на штатном гашении.
	carrierprobe.RequireRaised(t, "storage", serveErr)

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
	// Якорь — НАЧАЛО переписи: та же строка несёт «сужаемых методов 0», и
	// предикат по голой подстроке краснел бы на сервисе, который законно не
	// сужает ни одного метода. Здесь это не проявилось только потому, что
	// storage сужает — то есть проба была зелёной случайно.
	if strings.Contains(census, "осмотрено: методов 0") {
		t.Fatalf("отказы старта осмотрели НОЛЬ методов — вердикт получен на пустом наборе:\n%s", census)
	}
	t.Logf("перепись носителя: %s", strings.TrimSpace(census))
}

// TestCarrierOnAnOccupiedPortSaysConditionNotCreated — занятый порт есть ТРЕТЬЯ
// категория исхода, а не отказ носителя (kacho#2680).
//
// Прежде проба на занятом порту печатала «носитель вернул ошибку подъёма … bind:
// address already in use», и отличить это от настоящей находки можно было только
// прочитав текст отказа. Здесь занятость создаётся САМОЙ пробой — соседом на
// эфемерном порту, который выбрало ядро, — и порт этого соседа отдаётся ручке
// публичного слушателя: вход настоящий, от ядра, и от машины прогона не зависит.
//
// Обе половины утверждены: страж предусловия называет ручку с фиксированным
// портом, а исход подъёма на ней — «условие не создано», не красное.
func TestCarrierOnAnOccupiedPortSaysConditionNotCreated(t *testing.T) {
	occupant, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("предпосылка не создана: сосед на эфемерном порту не поднялся: %v", err)
	}
	defer occupant.Close()
	_, port, err := net.SplitHostPort(occupant.Addr().String())
	if err != nil {
		t.Fatalf("адрес соседа не разобран: %v", err)
	}

	cfg := bootConfig(t, map[string]string{
		"KACHO_STORAGE_GRPC_PORT":     port,
		"KACHO_STORAGE_INTERNAL_PORT": "0",
	})
	desc, err := describeWith(t, cfg)
	if err != nil {
		t.Fatalf("дескриптор отвергнут конструктором — процесс не поднялся бы:\n%v", err)
	}

	refusal := carrierprobe.Refusal(carrierprobe.Listeners(desc.Spec(), "KACHO_STORAGE_GRPC_PORT", "KACHO_STORAGE_INTERNAL_PORT")...)
	if refusal == nil || !strings.Contains(refusal.Error(), "KACHO_STORAGE_GRPC_PORT") {
		t.Fatalf("страж предусловия не назвал ручку с фиксированным портом %s: %v", port, refusal)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	regs := registrarsOfBothListeners(t)
	serveErr := servicehost.Serve(ctx, desc, regs[0], regs[1])
	if got := carrierprobe.Classify(serveErr); got != carrierprobe.NotCreated {
		t.Fatalf("подъём на занятом порту классифицирован как %v, а не «условие не создано»; ошибка носителя: %v",
			got, serveErr)
	}
	if v := carrierprobe.Verdict("storage", serveErr); !strings.Contains(v, "УСЛОВИЕ НЕ СОЗДАНО") {
		t.Fatalf("текст исхода не называет третью категорию:\n%s", v)
	}
}
