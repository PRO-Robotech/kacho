// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
)

const (
	// registerOutboxTable / registerOutboxChannel — та же таблица и канал, что
	// дренирует register-drainer (имя историческое: registry_outbox — это
	// register-outbox).
	registerOutboxTable   = "kacho_registry.registry_outbox"
	registerOutboxChannel = "kacho_registry_outbox"

	// redriveInterval — как часто отравленные строки возвращаются в доставляемое
	// состояние. 5 минут — паритет с compute/vpc/nlb/storage.
	redriveInterval = 5 * time.Minute
)

// startRedriveBackstop поднимает периодический RedrivePoisoned поверх
// register-outbox'а registry.
//
// # Зачем он обязателен, а не «на будущее»
//
// Отравленная строка ИСКЛЮЧЕНА из блокирующего набора claim-запроса, поэтому её
// партиция разблокируется сразу — именно это не даёт одному отвергнутому intent'у
// заглушить все последующие intent'ы того же объекта. Но сама строка после этого
// не доставляется НИКОГДА, если её никто не переигрывает, а недоставленная
// регистрация означает, что в kaname нет mirror-строки: нет owner/project-tuple,
// реконсайлер такой объект не перечисляет (все его выборки читают resource_mirror),
// и материализовать ему v_* не из чего. Реестр становится невидим в
// authz-фильтрованном List до ручной правки БД — ровно то, чего этот дренаж и
// должен был не допустить.
//
// Периодический redrive превращает отравление в ОГРАНИЧЕННУЮ ПАУЗУ вместо
// безвозвратной потери. Постоянная причина отравится снова — и будет видна как
// повторяющееся отравление, а не как тишина.
//
// Возврат отравленных — единственный проход backstop'а. Здесь стояла оговорка,
// почему НЕ запускаются два прохода сверки с состоянием: они переигрывали
// corelib-фиксированный payload, который декодер registry не прочитает. Оговорка
// пережила свой предмет — оба прохода сняты из corelib (#760), их предикаты
// были недостижимы by construction.
//
// РЕПЛИКИ: на-реплику — проход — один условный оператор возврата отравленных строк.
// Строки заперты самим оператором, повтор идемпотентен, к соседям проход
// не ходит; вторая реплика приводит к тому же состоянию.
func startRedriveBackstop(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	rc, err := reconciler.NewRedriveOnly(pool, reconciler.Config{
		PartitionColumn: reconciler.RegisterOutboxPartition,
		Table:           registerOutboxTable,
		Channel:         registerOutboxChannel,
	}, logger)
	if err != nil {
		return err
	}
	go func() {
		tick := time.NewTicker(redriveInterval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				n, rerr := rc.RedrivePoisoned(ctx)
				switch {
				case rerr != nil:
					logger.Warn("redrive of poisoned register-intents failed",
						"table", registerOutboxTable, "err", rerr)
				case n > 0:
					// Не INFO-шум: ненулевой счётчик означает, что доставка
					// действительно отказывала, и повтор может отказать снова.
					logger.Warn("re-drove poisoned register-intents",
						"table", registerOutboxTable, "count", n)
				}
			}
		}
	}()
	logger.Info("register-outbox redrive backstop started",
		"table", registerOutboxTable, "interval", redriveInterval)
	return nil
}
