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
// сделал бы её заложницей занятости машины прогона.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/servicehost"
)

// TestCarrierRaisesStorageWithoutAStartRefusal — исход: носитель поднимает
// storage и не находит расхождения между объявленным и служимым.
//
// Контекст отменён ЗАРАНЕЕ: предмет пробы — отказы, которые носитель считает ДО
// первого соединения. Отменённый контекст гасит слушатели сразу после того, как
// отказы отработали, поэтому проба не держит сокета и не ждёт сети.
func TestCarrierRaisesStorageWithoutAStartRefusal(t *testing.T) {
	cfg := bootConfig(t, map[string]string{
		"KACHO_STORAGE_GRPC_PORT":          "0",
		"KACHO_STORAGE_INTERNAL_GRPC_PORT": "0",
	})

	// Журнал носителя читается, а не выбрасывается: перепись осмотренного он
	// печатает ВСЕГДА, и без неё «отказов нет» неотличимо от «ничего не осмотрено».
	var log strings.Builder
	logger := slog.New(slog.NewTextHandler(&log, nil))

	desc, err := describe(cfg, logger, buildListFilter(cfg, nil, logger), probeExistence{})
	if err != nil {
		t.Fatalf("дескриптор отвергнут конструктором — процесс не поднялся бы:\n%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	regs := registrarsOfBothListeners()
	serveErr := servicehost.Serve(ctx, desc, regs[0], regs[1])
	if serveErr != nil && strings.Contains(serveErr.Error(), "не поднимается") {
		t.Fatalf("носитель ОТКАЗАЛ storage в старте — на стенде процесс не поднялся бы:\n%v", serveErr)
	}
	if serveErr != nil {
		t.Fatalf("носитель вернул ошибку подъёма: %v", serveErr)
	}

	census := log.String()
	if strings.Contains(census, "методов 0") {
		t.Fatalf("отказы старта осмотрели НОЛЬ методов — вердикт получен на пустом наборе:\n%s", census)
	}
	t.Logf("перепись носителя: %s", strings.TrimSpace(census))
}
