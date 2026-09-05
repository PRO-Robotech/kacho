// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// target_drain_runner.go — двухфазный drain background worker.
//
// Контекст: NetworkLoadBalancer.TargetGroupService.RemoveTargets — двухфазный
// (+):
//
//   - фаза A (synchronous, в request-path RemoveTargets handler'а): атомарный
//     `UPDATE targets SET status='DRAINING', drain_started_at=now WHERE...`.
//     Клиент сразу видит target вне traffic-pool (data-plane прекращает new
//     connections), но строка ещё в БД — flow'ам разрешено доиграть.
//
//   - фаза B (этот runner): периодический tick (default 10s).
//     `DELETE FROM targets WHERE status='DRAINING' AND
//     drain_started_at < now - tg.deregistration_delay_seconds`.
//     После DELETE — INSERT в `nlb_outbox` (DISTINCT per TG)
//     событие `nlb_target_group:<tg_id> UPDATED` → trigger `nlb_outbox_notify_trg`
//     шлёт `pg_notify('nlb_outbox', seq)` → пробуждение общего сервера потока
//     (`pkg/subscription`). Прежняя редакция называла здесь «lifecycle stream к
//     iam» — такого потребителя нет: контракт снят задачей #814, а зеркало прав
//     ходит очередью `fga_register_outbox`.
//
// Архитектура (workspace CLAUDE.md «Чистая архитектура»): runner использует
// `*pgxpool.Pool` напрямую, минуя CQRS Repository — это намеренно (godzila
// pattern для админ-job'ов: drain — pure SQL operator, не use-case с
// бизнес-логикой; не пересекается с handler'ами).
//
// Failure isolation: транзиентные SQL-errors на drainOnce логируются и
// **НЕ** завершают Run (continue loop). Только `ctx.Done` exits.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// ЗДЕСЬ СТОЯЛ ОДИН ОПЕРАТОР — снятие истёкших целей и строка журнала общим
// табличным выражением. Его больше нет, и причина не в стиле.
//
// Вид `nlb_target_group` объявлен несущим ПОЛНОЕ состояние предмета, а публичная
// проекция группы строится НЕ из её строки, а из набора целей С СОСТОЯНИЕМ.
// Собрать такую нагрузку в SQL значило бы завести ВТОРУЮ проекцию ресурса — на
// другом языке, рядом с той, которой отвечает чтение, — и расходились бы они
// молча. Прежняя нагрузка этого не делала и потому была честно неполной:
// `{id, reason}`, без целей, без меток, без якоря проекта.
//
// Теперь снятие отдаёт ЧТО СНЯЛОСЬ (`DeleteExpiredDrainingTargets`), а строку
// журнала собирает эта джоба — тем же строителем, что и остальные шесть точек
// эмиссии вида.
//
// ЧТО ПРИ ЭТОМ СОХРАНЕНО, названо явно, потому что стоило миграций:
//
//   - АТОМАРНОСТЬ: снятие и эмиссия идут в ОДНОЙ writer-транзакции, как и прежде.
//     Сорвавшийся проход не оставляет ни снятых строк без события, ни события без
//     снятия;
//   - МНОГОРЕПЛИЧНОСТЬ: строки заперты самим `DELETE`, поэтому вторая реплика
//     снимает ноль строк и эмитит ноль событий; к соседям проход не ходит;
//   - ОДНА СТРОКА НА ГРУППУ: различные идентификаторы отбирает порт, а не
//     `DISTINCT` в тексте запроса.
//
// ЦЕНА, названная тоже: чтений стало N+1 — по одному на затронутую группу вместо
// нуля. Плата за это — событие, из которого подписчик узнаёт, ЧТО ИМЕННО у
// группы теперь, вместо «что-то изменилось, перечитай».

// TargetDrainRunner — фоновый worker, реализующий двухфазный drain.
// Запускается из cmd/kacho-loadbalancer/main.go параллельно с gRPC-серверами.
type TargetDrainRunner struct {
	repo     kachorepo.Repository
	logger   *slog.Logger
	interval time.Duration

	// onTickErr — test-only observation hook (nil в проде), вызывается с
	// non-ctx ошибкой tick'а. Позволяет integration-тесту детерминированно
	// дождаться реально произошедшей transient-ошибки вместо wall-clock sleep
	// (audit TEST #7, CWE-367).
	onTickErr func(error)
}

// NewTargetDrainRunner создаёт runner. `interval` — период между tick'ами
// (рекомендованный default 10s; задаётся через `cfg.Jobs.TargetDrain.Interval`).
// Если interval <= 0 — используется fallback 10s (defense-in-depth от
// мисконфига; основная защита — config.Validate).
func NewTargetDrainRunner(repo kachorepo.Repository, logger *slog.Logger, interval time.Duration) *TargetDrainRunner {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &TargetDrainRunner{
		repo:     repo,
		logger:   logger,
		interval: interval,
	}
}

// Run блокирует goroutine до отмены ctx. Каждые `r.interval` вызывает
// drainOnce(ctx); transient errors logging + continue (не exit Run).
//
// Возвращает nil после ctx.Done — это «штатное завершение» runner'а
// при SIGTERM (parallel.ExecAbstract воспринимает nil как успех task'а).
//
// РЕПЛИКИ: на-реплику — проход — ОДИН оператор: снятие истёкших целей и эмиссия события идут
// одним `DELETE … RETURNING` с общим выражением. Строки заперты самим
// оператором, поэтому вторая реплика снимает ноль строк и эмитирует ноль
// событий; к соседям проход не ходит.
func (r *TargetDrainRunner) Run(ctx context.Context) error {
	r.logger.InfoContext(ctx, "target drain runner started", "interval", r.interval)
	defer r.logger.InfoContext(ctx, "target drain runner stopped")

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	// Первый tick — сразу, не ждать interval (быстрее «убираем мусор»,
	// если процесс рестартовал когда expired targets уже накопились).
	r.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// tick — одна итерация: вызывает drainOnce и логирует результат. Транзиентные
// errors не пропускаются наружу («runner respects ctx cancel
// cleanly; transient errors do not abort the loop»).
func (r *TargetDrainRunner) tick(ctx context.Context) {
	start := time.Now()
	deleted, tgs, err := r.drainOnce(ctx)
	took := time.Since(start)

	if err != nil {
		// ctx.Err ловится отдельно: cancel в середине tick'а — штатно.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		r.logger.ErrorContext(ctx, "target drain tick failed",
			"err", err, "took", took)
		if r.onTickErr != nil {
			r.onTickErr(err)
		}
		return
	}
	r.logger.InfoContext(ctx, "target drain tick",
		"deleted", deleted, "tgs", tgs, "took_ms", took.Milliseconds())
}

// drainOnce — один проход: снятие истёкших целей + строка журнала на каждую
// затронутую группу, ОДНОЙ writer-транзакцией.
//
// Возвращает (снято строк, различных групп, ошибка).
func (r *TargetDrainRunner) drainOnce(ctx context.Context) (int64, int, error) {
	w, err := r.repo.Writer(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("drain expired targets: %w", err)
	}
	defer w.Abort()

	deleted, tgIDs, err := w.TargetGroups().DeleteExpiredDrainingTargets(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("drain expired targets: %w", err)
	}
	if deleted == 0 {
		// Снимать было нечего — объявлять тоже. Пустая транзакция закрывается без
		// фиксации: строки журнала у прохода без предмета не бывает.
		return 0, 0, nil
	}
	for _, tgID := range tgIDs {
		// Состояние читается В ТОЙ ЖЕ транзакции, ПОСЛЕ снятия: именно снятие и
		// есть предмет события, и набор целей обязан прийти уже без снятых. Запись,
		// прочитанная до, показала бы их живыми.
		state, gerr := w.TargetGroups().Get(ctx, tgID)
		if gerr != nil {
			return 0, 0, fmt.Errorf("drain expired targets: read group %s: %w", tgID, gerr)
		}
		if eerr := w.Outbox().Emit(ctx,
			kachorepo.OutboxResourceTargetGroup, tgID, string(state.ProjectID),
			kachorepo.OutboxActionUpdated, kachorepo.TargetGroupStatePayload(state),
		); eerr != nil {
			return 0, 0, fmt.Errorf("drain expired targets: emit for group %s: %w", tgID, eerr)
		}
	}
	if err := w.Commit(); err != nil {
		return 0, 0, fmt.Errorf("drain expired targets: %w", err)
	}
	return deleted, len(tgIDs), nil
}
