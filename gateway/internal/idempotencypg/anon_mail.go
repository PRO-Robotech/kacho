// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package idempotencypg

import (
	"context"
	"fmt"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// anon_mail.go — уборка предметов хранения ограничителя анонимной почты края
// (замысел issue-2917, З26). Таблицы заводит миграция этого хранилища, решения
// пишет звено `middleware/anonmail` на своём пуле, а уборка идёт ЗДЕСЬ, на пуле
// однократности: других транзакций на пуле ограничителя нет (З8 (3)).

// anonMailSubject — предмет хранения: таблица и чем она снимается либо довод,
// почему не снимается. Перечень закрыт: таблица миграции без строки здесь —
// красный пробы TestAnonMailStorageSubjectsAreClosed.
type anonMailSubject struct {
	table string
	// reap — оператор уборки одной партии ($1 — размер партии, $2 — срок
	// хранения в секундах, где он нужен); пусто — у предмета довод.
	reap   string
	reason string
	// retention — нужен ли оператору срок хранения моментов (З26).
	retention bool
}

// anonMailSubjects — предметы хранения ограничителя в порядке уборки.
var anonMailSubjects = []anonMailSubject{
	{
		table: "anon_mail_passes",
		// Срок — функция З26 над таблицей границ края, а не литерал.
		reap: `DELETE FROM kacho_gateway.anon_mail_passes WHERE ctid IN (
    SELECT ctid FROM kacho_gateway.anon_mail_passes
     WHERE at <= now() - make_interval(secs => $2::double precision) LIMIT $1)`,
		retention: true,
	},
	{
		table: "pow_spent",
		// После срока вызова доказательство отвергает уже проверка срока.
		reap: `DELETE FROM kacho_gateway.pow_spent WHERE ctid IN (
    SELECT ctid FROM kacho_gateway.pow_spent WHERE expires_at <= now() LIMIT $1)`,
	},
	{
		table:  "anon_mail_bucket",
		reason: "одна строка на установку (строка-якорь ведра общего потока), уборке не подлежит",
	},
}

// AnonMailPurgeBatch — строк за один оператор уборки: партия ограничена ради
// блокировок, а не ради темпа (та же причина, что у ReapBatch).
const AnonMailPurgeBatch = 1000

// AnonMailSweep — исход одной уборки предметов ограничителя.
type AnonMailSweep struct {
	Removed int64
	// Drained — каждая таблица догнана (последняя партия неполная).
	Drained bool
}

// PurgeAnonMail уносит моменты пропуска старше срока хранения З26 и пометки
// вызовов после их срока — партиями, пока хвост не кончится либо не выйдет срок
// вызывающего или сторож числа партий.
func (s *Store) PurgeAnonMail(ctx context.Context) (AnonMailSweep, error) {
	out := AnonMailSweep{Drained: true}
	retention := config.AnonMailPassRetention().Seconds()
	for _, subj := range anonMailSubjects {
		if subj.reap == "" {
			continue
		}
		drained := false
		for i := 0; i < s.cfg.ReapMaxBatches && ctx.Err() == nil; i++ {
			args := []any{AnonMailPurgeBatch}
			if subj.retention {
				args = append(args, retention)
			}
			tag, err := s.pool.Exec(ctx, subj.reap, args...)
			if err != nil {
				return out, fmt.Errorf("anon mail store: purge %s: %w", subj.table, err)
			}
			out.Removed += tag.RowsAffected()
			if tag.RowsAffected() < AnonMailPurgeBatch {
				drained = true
				break
			}
		}
		out.Drained = out.Drained && drained
	}
	return out, nil
}

// purgeAnonMailOnce — заход уборки ограничителя вместе с отчётом: отказ и «не
// догнала» звучат, молча проходит только «догнала».
func (s *Store) purgeAnonMailOnce() {
	if s.baseCtx.Err() != nil {
		return
	}
	budget := min(s.cfg.ReapInterval, reapBudgetCap)
	ctx, cancel := context.WithTimeout(s.baseCtx, budget)
	defer cancel()
	sw, err := s.PurgeAnonMail(ctx)
	switch {
	case err != nil:
		s.cfg.Logger.WarnContext(ctx, "anon mail store: purge failed", "err", err, "removed", sw.Removed)
	case !sw.Drained:
		s.cfg.Logger.WarnContext(ctx, "anon mail store: purge did not keep up with the write rate",
			"removed", sw.Removed, "interval", s.cfg.ReapInterval, "batch", AnonMailPurgeBatch)
	case sw.Removed > 0:
		s.cfg.Logger.InfoContext(ctx, "anon mail store: expired rows removed", "removed", sw.Removed,
			"retention", config.AnonMailPassRetention())
	}
}

// ConnString — адрес базы хранилища. Читатель — корень края: пул ограничителя
// строится по ТОМУ ЖЕ адресу и только после этого хранилища (схема уже
// накатана его построением).
func (s *Store) ConnString() string { return s.pool.Config().ConnString() }
