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

// redriveInterval — как часто отравленные строки register-outbox возвращаются в
// доставляемое состояние. 5 минут — паритет с compute/vpc/nlb.
const redriveInterval = 5 * time.Minute

// startRedriveBackstop поднимает периодический RedrivePoisoned поверх
// kacho_storage.fga_register_outbox.
//
// # Зачем он обязателен, а не «на будущее»
//
// Отравленная строка ИСКЛЮЧЕНА из блокирующего набора claim-запроса, поэтому её
// партиция разблокируется сразу — именно это не даёт одному отвергнутому intent'у
// заглушить все последующие intent'ы того же ресурса. Но сама строка после этого
// не доставляется НИКОГДА, если её никто не переигрывает, а недоставленная
// регистрация означает, что в kacho-iam нет mirror-строки ресурса: нет
// owner/parent-tuple, реконсайлер такой объект вообще не перечисляет (все три
// его выборки читают resource_mirror), и материализовать ему v_* не из чего.
// Ресурс становится невидим для authz до ручной правки БД.
//
// Периодический redrive превращает отравление в ОГРАНИЧЕННУЮ ПАУЗУ вместо
// безвозвратной потери: причина, которая была временной (не досеянный грант
// fga_writer на свежем стенде), отработает на следующем круге; причина, которая
// действительно постоянна (отношение вне принимаемого набора), будет отравляться
// снова — и это видно счётчиком отравлений, а не тишиной.
//
// Только redrive: BackfillFromState/GCOrphans переигрывают corelib-фиксированный
// payload, который декодер storage не прочитает, поэтому reconciler строится
// NewRedriveOnly — без доменных адаптеров, которых здесь не будет.
func startRedriveBackstop(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	rc, err := reconciler.NewRedriveOnly(pool, reconciler.Config{
		PartitionColumn: reconciler.RegisterOutboxPartition,
		Table:           fgaRegisterOutboxTable,
		Channel:         fgaRegisterOutboxChannel,
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
						"table", fgaRegisterOutboxTable, "err", rerr)
				case n > 0:
					// Не INFO-шум: ненулевой счётчик означает, что доставка
					// действительно отказывала, и повтор может отказать снова.
					logger.Warn("re-drove poisoned register-intents",
						"table", fgaRegisterOutboxTable, "count", n)
				}
			}
		}
	}()
	logger.Info("register-outbox redrive backstop started",
		"table", fgaRegisterOutboxTable, "interval", redriveInterval)
	return nil
}
