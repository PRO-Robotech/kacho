// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/instance"
)

const (
	// stuckDeleteInterval — период прохода добивателя.
	stuckDeleteInterval = 5 * time.Minute
	// stuckDeleteGrace — отсрочка: удаление моложе этого срока доделывает
	// законный исполнитель, и подхватывать его нельзя — оба снимали бы одни и те
	// же привязки наперегонки. Пяти минут хватает любому пути удаления с запасом.
	stuckDeleteGrace = 5 * time.Minute
	// stuckDeleteTimeout — верхняя граница одного прохода. Проход ходит к двум
	// соседям, и без границы он повис бы до конца жизни процесса на неотвечающем.
	stuckDeleteTimeout = 2 * time.Minute
)

// runStuckDeleteFinisher — фоновый добиватель начатых удалений ВМ.
//
// # Что он закрывает
//
// Порядок delete-саги crash-safe: строка машины живёт до последнего шага,
// привязки резолвятся у владельцев на каждом прогоне, повтор идемпотентен.
// Повторять было НЕКОМУ: разрешитель осиротевших операций по контракту рабочую
// функцию не перезапускает — он приводит статус операции в соответствие
// закоммиченной реальности, видит строку на месте и помечает операцию
// прерванной. Машина оставалась в DELETING навсегда, а её интерфейсы и тома —
// занятыми у владельцев, которые снятия не запрашивают.
//
// Симптома у этого нет ни у кого: удаление «прошло», клиент получил исход, и
// только потом обнаруживается, что том не присоединяется к другой машине, а
// адрес из ограниченного пула не возвращается.
//
// # Почему это не гейт
//
// Добиватель не участвует в пути запроса и не может уронить под: ошибка прохода
// логируется, следующий проход начинает заново. Отказ соседа не проглатывается —
// он прерывает проход, оставляя строку на месте, и разбирается повтором, когда
// сосед вернётся.
//
// РЕПЛИКИ: одиночка — проход берёт одна реплика замком прохода в базе сервиса
// (InstanceRepo.TryClaimStuckDeleteSweep); проигравший пропускает тик.
//
// Первый проход идёт сразу, до первого тика: после перезапуска застрявшее не
// должно ждать полный интервал, а перезапусков в жизни сервиса больше, чем
// интервалов между ними.
func runStuckDeleteFinisher(ctx context.Context, svc *instance.InstanceService, logger *slog.Logger) {
	log := logger.With(slog.String("component", "stuck_delete_finisher"))
	tick := time.NewTicker(stuckDeleteInterval)
	defer tick.Stop()
	finishStuckDeletesOnce(ctx, svc, log)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			finishStuckDeletesOnce(ctx, svc, log)
		}
	}
}

// finishStuckDeletesOnce — один проход.
func finishStuckDeletesOnce(ctx context.Context, svc *instance.InstanceService, log *slog.Logger) {
	passCtx, cancel := context.WithTimeout(ctx, stuckDeleteTimeout)
	defer cancel()

	finished, ran, err := svc.FinishStuckDeletes(passCtx, stuckDeleteGrace)
	// Доделанное логируется ДАЖЕ при ошибке: проход мог довести часть машин до
	// конца и встать на следующей, и молчать о сделанном нельзя.
	for _, id := range finished {
		log.Info("удаление, начатое умершим процессом, доведено до конца", "instance_id", id)
	}
	if err != nil {
		log.Warn("проход добивателя прерван; строки остались на месте и будут разобраны повтором",
			"finished_before_error", len(finished), "err", err)
		return
	}
	if !ran {
		// Проход исполняет другая реплика — это штатный исход, а не отказ.
		// Отдельная строка от «застрявших нет»: иначе «нас развели» и «работы не
		// было» выглядели бы одинаково, а различить их — ровно то, ради чего
		// развод и заведён.
		log.Debug("проход добивателя пропущен: его исполняет другая реплика")
		return
	}
	if len(finished) == 0 {
		// Ноль находок печатается тоже: молчащий добиватель неотличим от
		// неподнятого, а различить их — ровно то, ради чего он заведён.
		log.Debug("проход добивателя завершён: застрявших удалений нет")
	}
}
