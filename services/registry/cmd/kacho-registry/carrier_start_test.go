// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// carrier_start_test.go — реестр проходит ВСЕ отказы старта носителя контура, а
// не только те, что проба умеет повторить у себя.
//
// # Зачем отдельная проба, если рядом лежит describe_test.go
//
// Тот повторяет три сверки (О3/О4/О5) на дереве и оттого узнаёт о расхождении
// раньше стенда. Но носитель считает ДЕСЯТЬ отказов, и половине из них нужен
// СЛУЖИМЫЙ НАБОР RPC, снятый у самих серверов после регистрации: служится метод
// без строки каталога (О2), домен не дал карте ни одной записи (О9), служится
// серверный стрим при верхней границе обработки, выведенной для запроса (О11).
// Повторить их у себя нельзя — они существуют только там, где есть оба
// собранных сервера.
//
// Поэтому здесь зовётся САМ носитель, с настоящим дескриптором и настоящими
// регистраторами. Отказ старта рантаймовый: без этой пробы одиннадцатая
// сужаемая строка каталога, новый стрим или потерянная аннотация стали бы
// известны только на поднятом стенде.
//
// Порты — эфемерные (`:0`): проба обязана быть детерминированной, а фиксированный
// номер сделал бы её заложницей того, что ещё занято на машине прогона.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	"github.com/PRO-Robotech/kacho/services/registry/internal/handler"
)

// TestCarrierRaisesRegistryWithoutAStartRefusal — исход: носитель поднимает
// реестр и не находит расхождения между объявленным и служимым.
//
// Контекст отменён ЗАРАНЕЕ: предмет пробы — отказы старта, которые носитель
// считает ДО того, как принять первое соединение. Отменённый контекст гасит
// слушатели сразу после того, как отказы отработали, поэтому проба не держит
// сокета и не ждёт сети.
func TestCarrierRaisesRegistryWithoutAStartRefusal(t *testing.T) {
	cfg := bootConfig(t, map[string]string{
		"KACHO_REGISTRY_GRPC_PORT":     "0",
		"KACHO_REGISTRY_INTERNAL_PORT": "0",
	})
	// Журнал носителя читается, а не выбрасывается: перепись осмотренного он
	// печатает ВСЕГДА, и без неё «отказов нет» неотличимо от «ничего не
	// осмотрено».
	var log strings.Builder
	// Посадка разбирается тем же местом, что и в композиционном корне: там она
	// уезжает и в профиль не-gRPC поверхностей, и в дескриптор процесса.
	mode, merr := servicecontract.ParseMode(cfg.AuthMode)
	if merr != nil {
		t.Fatalf("посадка фикстуры не разобралась: %v", merr)
	}
	desc, err := describe(cfg, mode, slog.New(slog.NewTextHandler(&log, nil)), probePorts())
	if err != nil {
		t.Fatalf("дескриптор отвергнут конструктором — процесс не поднялся бы:\n%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registryHandler := handler.NewRegistryHandler(nil, nil, 0)
	internalHandler := handler.NewInternalRegistryHandler(nil)
	opHandler := handler.NewOperationHandler(operations.NewRepo(nil, "kacho_registry"))

	serveErr := servicehost.Serve(ctx, desc,
		func(reg grpc.ServiceRegistrar) {
			registerPublic(reg, registryHandler, handler.NewQuotaHandler(nil), opHandler)
		},
		func(reg grpc.ServiceRegistrar) { registerInternal(reg, internalHandler, opHandler) },
	)
	if serveErr != nil && strings.Contains(serveErr.Error(), "не поднимается") {
		t.Fatalf("носитель ОТКАЗАЛ реестру в старте — на стенде процесс не поднялся бы:\n%v", serveErr)
	}
	// «сервер остановлен» — законный исход ОТМЕНЁННОГО контекста, а не отказ:
	// носитель успел собрать оба сервера и погасить их. Что именно вернётся —
	// nil или это сообщение — решает планировщик, поэтому проба, принимавшая
	// только nil, зеленела НЕДЕТЕРМИНИРОВАННО и краснела под -race, где порядок
	// другой. Предмет пробы — отказы, которые считаются ДО первого соединения;
	// прочие ошибки остаются настоящими.
	if serveErr != nil && !strings.Contains(serveErr.Error(), "server has been stopped") {
		t.Fatalf("носитель вернул ошибку подъёма: %v", serveErr)
	}

	// Предпосылка: отказы что-то осмотрели. Ноль осмотренных методов означал бы,
	// что «расхождений нет» получено на пустом наборе.
	census := log.String()
	if !strings.Contains(census, "start refusals passed") {
		t.Fatalf("носитель не напечатал переписи осмотренного — «отказов нет» здесь неотличимо "+
			"от «ничего не осмотрено»:\n%s", census)
	}
	if strings.Contains(census, "методов 0") {
		t.Fatalf("отказы старта осмотрели НОЛЬ методов — вердикт получен на пустом наборе:\n%s", census)
	}
	t.Logf("перепись носителя: %s", strings.TrimSpace(census))
}
