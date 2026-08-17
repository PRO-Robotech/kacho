// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// free_ip_runner.go — background reconciler «застрявших» LoadBalancer'ов (durable
// handle). Устраняет утечку anycast-VIP при сбое create/delete-саги.
//
// Контекст. VIP консолидирован на LoadBalancer (anycast active-active): VIP
// аллоцируется per-family из vpc REGIONAL-подсети — внешний side-effect,
// единственный dual-write edge create/delete LB. Если процесс падает (или
// peer-vpc недоступен) между аллокацией VIP и финализацией строки, остаётся
// «сирота»:
//
//   - create-path: durable-handle строка в status='CREATING' с уже
//     персистнутыми address_id_v4/v6 (INSERT 'CREATING' ДО alloc, отдельный
//     commit CAS-attach сразу после alloc), но без перехода в терминальный статус;
//   - delete-path: строка в status='DELETING' (release VIP упал → Delete оставил
//     её на retry).
//
// В обоих случаях VIP остаётся аллоцированным в vpc, а строка-handle висит в
// нетерминальном статусе. Reconciler периодически сканирует такие строки старше
// age-порога (свежий in-flight не трогаем — легитимный worker дорабатывает) и
// детерминированно по address_id снимает аренду VIP КАЖДОГО семейства РАЗДЕЛЬНО
// ОДНИМ глаголом владельца (#439), затем финализирует/удаляет handle. Ветку
// «удалить адрес модуля» (RELEASED — аренда уходит в пул) либо «оставить адрес
// арендатора» (DETACHED — ссылка снята, адрес жив) выбирает vpc по СВОЕЙ
// колонке `address_references.owned`; реконсайлер её не выводит и по
// vip_origin не ветвится — эта колонка остаётся здесь только для наблюдаемости.
//
// Идемпотентность: повторный проход безопасен — владелец отвечает
// ALREADY_RELEASED/ALREADY_DETACHED. «Не найдено» на этой полосе — ОТКАЗ, а не
// успех: глагол его не производит, поэтому получить его можно только говоря не
// с тем глаголом (гейт `leaserefusalassuccess`). Multi-replica-safety: claim строки — FOR UPDATE SKIP
// LOCKED, release+DELETE по строке выполняет ровно одна реплика. Узкий auto-only
// known-gap (крах в окне «alloc-ответ ↔ persist» — в строке НЕТ ни адреса, ни его
// ключа) — handle удаляется без release. Строка, у которой адрес ЕСТЬ, а ключ
// аренды потерян, — другое состояние: она не захватывается и переписывается
// (`censusLostLeases`, #467). Подробности — docs/architecture/15-free-ip-runner.md.
//
// Архитектура (как TargetDrainRunner): admin-job поверх *pgxpool.Pool, минуя
// CQRS Repository (pure SQL reconcile). Failure isolation: транзиентные ошибки
// (vpc Unavailable / SQL) логируются и НЕ завершают Run — строка остаётся и
// переедет на следующем тике. Только ctx.Done() выходит.
//
// Hard-cut / mixed-version: новый runner сканирует ТОЛЬКО load_balancers; старая
// версия (если ещё жива в кластере) сканирует listeners. Каждый Address
// освобождается ровно одним реклеймером (release идемпотентен по address_id).
// Pre-cut listener-VIP действующих листенеров этот runner НЕ трогает.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// freeIPMaxPerTick — верхняя граница строк, реконсилируемых за один тик (защита
// от unbounded-loop при большом backlog; остаток доедет на следующем тике).
const freeIPMaxPerTick = 100

// selectStuckSQL — claim одной застрявшей LB-строки старше age-порога под
// FOR UPDATE SKIP LOCKED (exactly-once между репликами). make_interval(secs=>$1)
// — age в секундах; ORDER BY updated_at — старейшие первыми (partial index
// load_balancers_reconcile_idx).
//
// Строки с ПОТЕРЯННЫМ ключом аренды (адрес выдан, `address_id` пуст) выборкой не
// клеймятся — `leaseCoherent` ниже; перепись по ним — `censusLostLeaseSQL`.
const selectStuckSQL = `
SELECT id, project_id, region_id,
       address_id_v4, address_id_v6, vip_origin_v4, vip_origin_v6, status
  FROM kacho_nlb.load_balancers
 WHERE status IN ('DELETING','CREATING')
   AND updated_at < now() - make_interval(secs => $1::double precision)
   AND ` + leaseCoherent + `
 ORDER BY updated_at ASC
 LIMIT 1
 FOR UPDATE SKIP LOCKED`

// leaseCoherent — «у каждого семейства адрес и ключ аренды либо оба есть, либо
// обоих нет». Тот же предикат, что закрепляет схема (миграция 0035,
// `(адрес = ”) = (ключ = ”)`), выраженный здесь как условие ОТБОРА.
const leaseCoherent = `((address_v4 = '') = (address_id_v4 = '')
                        AND (address_v6 = '') = (address_id_v6 = ''))`

// censusLostLeaseSQL — перепись строк, у которых адрес выдан, а ключ аренды
// потерян. Освободить их нечем, сносить нельзя, и они не клеймятся — значит
// обязаны быть ЗАМЕТНЫ, иначе «реконсайлер ничего не находит» станет неотличимо
// от «реконсайлеру нечего находить». Обоснование — у `censusLostLeases`.
const censusLostLeaseSQL = `
SELECT id, status
  FROM kacho_nlb.load_balancers
 WHERE status IN ('DELETING','CREATING')
   AND NOT ` + leaseCoherent + `
 ORDER BY updated_at ASC
 LIMIT $1`

// FreeIPRunner — фоновый reconciler застрявших LoadBalancer'ов (durable handle).
type FreeIPRunner struct {
	pool         *pgxpool.Pool
	addrs        vpcclient.InternalAddressClient // release VIP (FreeIP / ClearReference)
	logger       *slog.Logger
	interval     time.Duration
	ageThreshold time.Duration

	// onPoison — опциональный observer, вызываемый когда reconciler изолирует
	// ядовитую (permanent-release-failure) строку (nil в дефолте). Прод-wiring
	// инкрементит poison-метрику (mirror corelib drainer.WithPoisonObserver).
	onPoison func(lbID string)
}

// FreeIPOption конфигурирует FreeIPRunner (functional-options).
type FreeIPOption func(*FreeIPRunner)

// WithPoisonObserver регистрирует callback, вызываемый КАЖДЫЙ раз когда
// reconciler изолирует ядовитую строку (permanent VIP-release failure). Прод —
// инкремент poison-метрики; тест — детерминированная фиксация факта изоляции.
// Зеркалит corelib drainer.WithPoisonObserver.
func WithPoisonObserver(fn func(lbID string)) FreeIPOption {
	return func(r *FreeIPRunner) { r.onPoison = fn }
}

// NewFreeIPRunner создаёт reconciler. interval — период тиков; ageThreshold —
// минимальный возраст строки (по updated_at), чтобы не трогать свежий in-flight.
// Невалидные (<=0) значения подменяются безопасными дефолтами. addrs допускается
// nil (vpc не сконфигурирован) — тогда reconciler no-op (без release нельзя
// безопасно удалять handle, иначе утечка).
func NewFreeIPRunner(pool *pgxpool.Pool, addrs vpcclient.InternalAddressClient, logger *slog.Logger, interval, ageThreshold time.Duration, opts ...FreeIPOption) *FreeIPRunner {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if ageThreshold <= 0 {
		ageThreshold = 5 * time.Minute
	}
	r := &FreeIPRunner{
		pool:         pool,
		addrs:        addrs,
		logger:       logger,
		interval:     interval,
		ageThreshold: ageThreshold,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run блокирует goroutine до отмены ctx. Каждые r.interval — tick reconcile;
// транзиентные ошибки логируются и НЕ завершают Run (continue). Возврат nil
// после ctx.Done — штатное завершение.
func (r *FreeIPRunner) Run(ctx context.Context) error {
	r.logger.InfoContext(ctx, "free_ip_runner started",
		"interval", r.interval, "age_threshold", r.ageThreshold, "vpc_configured", r.addrs != nil)
	defer r.logger.InfoContext(ctx, "free_ip_runner stopped")

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

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

// tick — одна итерация: reconcileOnce + лог. Транзиентные ошибки не пропускаются
// наружу (loop respects ctx cancel; transient errors do not abort).
func (r *FreeIPRunner) tick(ctx context.Context) {
	start := time.Now()
	n, err := r.reconcileOnce(ctx)
	took := time.Since(start)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		r.logger.ErrorContext(ctx, "free_ip_runner tick failed",
			"err", err, "reconciled", n, "took", took)
		return
	}
	if n > 0 {
		r.logger.InfoContext(ctx, "free_ip_runner tick",
			"reconciled", n, "took_ms", took.Milliseconds())
	}
}

// reconcileOutcome — исход одной итерации reconcileOne.
type reconcileOutcome int

const (
	outcomeIdle       reconcileOutcome = iota // нет claimable-строки старше age-порога
	outcomeReconciled                         // VIP освобождён + handle финализирован/удалён
	outcomePoisoned                           // permanent-release failure → строка изолирована (updated_at bumped)
)

// reconcileOnce реконсилит застрявшие строки до исчерпания (или до
// freeIPMaxPerTick). addrs==nil → no-op. Возвращает число РЕКОНСИЛИРОВАННЫХ
// (успешно освобождённых) строк; первая транзиентная ошибка прерывает тик.
// Ядовитая строка (permanent-release failure) изолируется (bump updated_at) и
// НЕ прерывает тик — иначе одна строка head-of-line-блокирует всю очередь
// (selectStuckSQL ORDER BY updated_at ASC переизбирал бы её первой каждый тик).
func (r *FreeIPRunner) reconcileOnce(ctx context.Context) (int, error) {
	if r.addrs == nil {
		return 0, nil
	}
	r.censusLostLeases(ctx)
	reconciled := 0
	for i := 0; i < freeIPMaxPerTick; i++ {
		outcome, err := r.reconcileOne(ctx)
		if err != nil {
			return reconciled, err
		}
		switch outcome {
		case outcomeReconciled:
			reconciled++
		case outcomePoisoned:
			// Строка изолирована (updated_at bumped, выпала из age-порога) —
			// продолжаем свип к следующему кандидату, не прерывая тик.
		case outcomeIdle:
			return reconciled, nil
		}
	}
	return reconciled, nil
}

// censusLostLeases — перепись строк с потерянным ключом аренды (адрес выдан,
// `address_id` пуст), выполняемая раз в тик ДО свипа.
//
// Такая строка не клеймится (`selectStuckSQL`), и это не «пропуск», а
// единственный неразрушающий исход из трёх возможных:
//
//   - освободить — НЕЧЕМ: ключа аренды в строке нет;
//   - снести строку — НЕЛЬЗЯ: она единственная зацепка к висящей аренде
//     (выборка идёт по `load_balancers` в DELETING/CREATING, а обратного поиска
//     «что принадлежит этому балансировщику» у vpc нет). Ровно поэтому путь
//     удаления такую строку СОХРАНЯЕТ (#467, `api/loadbalancer/delete.go`) —
//     снеся её здесь, реконсайлер обесценил бы тот отказ;
//   - изолировать отметкой времени — НЕВОЗМОЖНО: строку запрещает ограничение
//     миграции 0035, а Postgres перепроверяет CHECK на ЛЮБОМ обновлении строки,
//     поэтому отвергается даже `SET updated_at = now()`. Отсюда и запрет
//     клеймить: строка, которую нельзя ни разобрать, ни отодвинуть,
//     переизбиралась бы первой каждый тик (`ORDER BY updated_at ASC`) и
//     head-of-line-блокировала бы ВСЮ очередь.
//
// Остаётся сделать её ЗАМЕТНОЙ: перепись пишет WARN и дёргает poison-обсервер
// (в проде — метрика). Чинит такую строку оператор: вернуть ключ аренды либо
// освободить адрес у vpc вручную и снять строку. Ошибка самой переписи не
// прерывает тик — она про наблюдаемость, а не про работу.
func (r *FreeIPRunner) censusLostLeases(ctx context.Context) {
	rows, err := r.pool.Query(ctx, censusLostLeaseSQL, freeIPMaxPerTick)
	if err != nil {
		r.logger.WarnContext(ctx, "free_ip_runner: lost-lease census failed", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			r.logger.WarnContext(ctx, "free_ip_runner: lost-lease census scan failed", "err", err)
			return
		}
		r.logger.WarnContext(ctx,
			"free_ip_runner: load balancer holds an address whose lease id is empty — "+
				"the lease cannot be returned and the row is kept as its only handle; needs operator repair",
			"load_balancer_id", id, "status", status)
		if r.onPoison != nil {
			r.onPoison(id)
		}
	}
	if err := rows.Err(); err != nil {
		r.logger.WarnContext(ctx, "free_ip_runner: lost-lease census failed", "err", err)
	}
}

// stuckLB — claimed LB-handle (durable handle reconcile-вход).
type stuckLB struct {
	id          string
	projectID   string
	regionID    string
	addressIDV4 string
	addressIDV6 string
	originV4    string
	originV6    string
	status      string
}

// reconcileOne — claim+release+finalize одной строки в одной TX. FOR UPDATE SKIP
// LOCKED держит row-lock на время release (network) → ровно одна реплика
// освобождает VIP и удаляет handle. Транзиентная release-ошибка (vpc Unavailable)
// → rollback, строка остаётся → retry на следующем тике (идемпотентно).
// Permanent release-ошибка (ErrInvalidArg/ErrFailedPrecondition) → строка
// изолируется (handleReleaseErr: bump updated_at), чтобы не head-of-line-
// блокировать очередь; см. handleReleaseErr. Возвращает reconcileOutcome.
//
// Row-lock hold window (round-6 audit finding 1; magnitude уточнён round-9):
// releaseFamily вызывает ClearReference/FreeIP (v4+v6, каждое семейство up to
// two-step ClearReference→FreeIP) НА raw process-lifetime ctx этого reconciler'а
// (без deadline). Каждый gRPC-вызов клиента несёт свой собственный
// context.WithTimeout(DefaultInternalAddressCallTimeout=5s) внутри
// (vpc.internalAddressClient.withCallTimeout), НО FreeIP дополнительно дожидается
// Delete-Operation через waitOperation, чей poll-цикл ограничен ОТДЕЛЬНЫМ
// vpcOpPollTimeout=15s на raw ctx (не withCallTimeout) — см. waitOperation в
// internal_address_client.go. Поэтому зависший/медленный vpc-peer больше не
// парковал бы эту горутину (и tx + row-lock) навсегда, но worst-case hold шире
// одного per-call timeout: ≈ 2×(ClearReference 5s + FreeIP[Delete 5s + poll 15s])
// ≈ 50s (v4+v6), затем releaseFamily возвращает DeadlineExceeded→
// domain.ErrUnavailable, tx rollback'ится, строка остаётся на retry следующим
// тиком. Полный вынос release-вызовов за границы транзакции — отдельный (более
// рискованный) рефактор, не предпринят без детерминированного теста на его
// race-профиль.
func (r *FreeIPRunner) reconcileOne(ctx context.Context) (reconcileOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return outcomeIdle, fmt.Errorf("begin reconcile tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var lb stuckLB
	err = tx.QueryRow(ctx, selectStuckSQL, r.ageThreshold.Seconds()).Scan(
		&lb.id, &lb.projectID, &lb.regionID,
		&lb.addressIDV4, &lb.addressIDV6, &lb.originV4, &lb.originV6, &lb.status)
	if errors.Is(err, pgx.ErrNoRows) {
		return outcomeIdle, nil
	}
	if err != nil {
		return outcomeIdle, fmt.Errorf("claim stuck load balancer: %w", err)
	}

	// Per-family release VIP по address_id (детерминированно, раздельно v4/v6).
	// Владелец называет исход полем; ЛЮБОЙ отказ означает, что аренда не снята.
	// Транзиентная ошибка (peer недоступен) прерывает тик (retry); permanent
	// изолирует ядовитую строку, не блокируя очередь (см. handleReleaseErr).
	if err := r.releaseFamily(ctx, lb.projectID, lb.id, lb.addressIDV4); err != nil {
		return r.handleReleaseErr(ctx, tx, &committed, lb, "v4", lb.addressIDV4, lb.originV4, err)
	}
	if err := r.releaseFamily(ctx, lb.projectID, lb.id, lb.addressIDV6); err != nil {
		return r.handleReleaseErr(ctx, tx, &committed, lb, "v6", lb.addressIDV6, lb.originV6, err)
	}
	if lb.addressIDV4 == "" && lb.addressIDV6 == "" {
		r.logger.WarnContext(ctx,
			"free_ip_runner: stuck load balancer has no address_id — deleting handle without VIP release (auto-only residual gap)",
			"load_balancer_id", lb.id, "status", lb.status)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM kacho_nlb.load_balancers WHERE id = $1`, lb.id); err != nil {
		return outcomeIdle, fmt.Errorf("delete stuck load balancer %s: %w", lb.id, err)
	}

	// DELETING — finalize то, что сделал бы успешный Delete (DELETED + fga-unregister).
	// CREATING-сирота никогда не достиг терминального статуса и не анонсировался
	// (CREATED/fga-register не эмитились) → ничего не эмитим.
	if lb.status == string(domain.LBStatusDeleting) {
		if err := emitReconcileFinalize(ctx, tx, lb.id, lb.projectID); err != nil {
			return outcomeIdle, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return outcomeIdle, fmt.Errorf("commit reconcile %s: %w", lb.id, err)
	}
	committed = true
	r.logger.InfoContext(ctx, "free_ip_runner reconciled stuck load balancer",
		"load_balancer_id", lb.id, "status", lb.status,
		"address_id_v4", lb.addressIDV4, "address_id_v6", lb.addressIDV6)
	return outcomeReconciled, nil
}

// handleReleaseErr классифицирует ошибку release VIP и решает судьбу тика:
//
//   - ТРАНЗИЕНТНАЯ (domain.ErrUnavailable — peer недоступен; ctx-отмена при
//     shutdown): rollback, вернуть ошибку. Строка остаётся с нетронутым
//     updated_at → быстрый retry следующим тиком (self-heal при восстановлении
//     vpc-peer'а). tick() глотает non-ctx ошибку (логирует), ctx-ошибку — тихо.
//
//   - ПЕРМАНЕНТНАЯ (domain.ErrInvalidArg / domain.ErrFailedPrecondition — напр.
//     stray referrer не даёт удалить owned-Address, malformed/invalid address_id):
//     ядовитая строка. НЕ прерываем тик — иначе одна строка head-of-line-
//     блокирует всю очередь (selectStuckSQL ORDER BY updated_at ASC переизбирал
//     бы её первой каждый тик → unbounded VIP-leak за одной строкой). Изолируем:
//     bump updated_at=now() в ЭТОЙ же tx и commit → строка тонет в хвост очереди
//     и выпадает из age-порога на ~ageThreshold (back-off) → следующий SELECT
//     берёт другого кандидата. VIP остаётся аллоцированным (наблюдаемо через
//     onPoison-обсервер/Warn-лог), но утечка больше не растёт неограниченно.
func (r *FreeIPRunner) handleReleaseErr(
	ctx context.Context, tx pgx.Tx, committed *bool, lb stuckLB, family, addressID, origin string, releaseErr error,
) (reconcileOutcome, error) {
	if isTransientReleaseErr(releaseErr) {
		return outcomeIdle, fmt.Errorf("release vip %s %s (origin=%s): %w", family, addressID, origin, releaseErr)
	}

	// Permanent → изолировать ядовитую строку (bump updated_at, commit).
	if _, err := tx.Exec(ctx,
		`UPDATE kacho_nlb.load_balancers SET updated_at = now() WHERE id = $1`, lb.id); err != nil {
		return outcomeIdle, fmt.Errorf("isolate poison load balancer %s: %w", lb.id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return outcomeIdle, fmt.Errorf("commit poison isolation %s: %w", lb.id, err)
	}
	*committed = true
	r.logger.WarnContext(ctx,
		"free_ip_runner: permanent VIP release failure — isolating poison load balancer (updated_at bumped, tick continues)",
		"load_balancer_id", lb.id, "status", lb.status,
		"family", family, "address_id", addressID, "vip_origin", origin, "err", releaseErr)
	if r.onPoison != nil {
		r.onPoison(lb.id)
	}
	return outcomePoisoned, nil
}

// isTransientReleaseErr — ошибка release, при которой строку НЕ изолируем
// (self-heal на следующем тике): peer недоступен (domain.ErrUnavailable —
// покрывает и per-call DeadlineExceeded, замапленный клиентом в ErrUnavailable)
// либо отмена ctx (shutdown). Всё прочее (ErrInvalidArg/ErrFailedPrecondition) —
// permanent → poison.
func isTransientReleaseErr(err error) bool {
	return errors.Is(err, domain.ErrUnavailable) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// releaseFamily снимает аренду VIP одного семейства ОДНИМ глаголом владельца:
// пустой address_id → no-op; исход читается из поля ответа, а не выводится из
// кода ошибки.
//
// Прежде это была пара пообъектных вызовов, у которой ответ «не найдено»
// означал успех. Для реконсайлера это было особенно дорого: он ходит под
// системной личностью, и потеря принципала на этом пути давала ровно тот же
// ответ — то есть «аренда освобождена» отвечалось там, где не было сделано
// ничего, после чего строка потребителя сносилась вместе с последней
// координатой аренды.
//
// Пустой address_id здесь означает ровно «этого семейства у балансировщика
// нет»: строку, где адрес ВЫДАН, а ключ аренды потерян, выборка НЕ КЛЕЙМИТ
// вовсе (`selectStuckSQL` + `censusLostLeases`, #467), поэтому такой вход сюда
// не доходит и различать его здесь нечем и незачем.
func (r *FreeIPRunner) releaseFamily(ctx context.Context, projectID, lbID, addressID string) error {
	if addressID == "" {
		return nil
	}
	// System-reconcile детачнут от tenant-request — идём под system-principal,
	// чтобы вызов к vpc нёс identity (иначе authz_no_principal).
	ctx = operations.WithPrincipal(ctx, operations.SystemPrincipal())
	_, err := r.addrs.ReleaseLease(ctx, vpcclient.ReleaseLeaseRequest{
		ProjectID: projectID,
		AddressID: addressID,
		Owner:     vpcclient.AddressOwner{Kind: vpcclient.OwnerKindLoadBalancer, ID: lbID},
	})
	return err
}

// emitReconcileFinalize эмитит в текущей TX outbox DELETED (nlb_load_balancer) +
// fga-unregister (project-hierarchy) — то же, что финальный шаг успешного Delete.
// Все INSERT'ы — в той же TX, что и DELETE строки (атомарно).
func emitReconcileFinalize(ctx context.Context, tx pgx.Tx, lbID, projectID string) error {
	lbPayload, err := json.Marshal(map[string]any{
		"id":         lbID,
		"project_id": projectID,
		"reason":     "free_ip_runner_reconcile",
	})
	if err != nil {
		return fmt.Errorf("marshal lb DELETED payload: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, project_id, action, payload)
		VALUES ('nlb_load_balancer', $1, $2, 'DELETED', $3::jsonb)
	`, lbID, projectID, lbPayload); err != nil {
		return fmt.Errorf("emit lb DELETED: %w", err)
	}

	// fga-unregister project-hierarchy (как loadbalancer.Delete). source_version
	// штампуется из DB-clock внутри этой writer-TX (last-source-state-wins).
	intent := domain.FGARegisterIntent{
		Kind:       "NetworkLoadBalancer",
		ResourceID: lbID,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeLoadBalancer, lbID, projectID),
		},
	}
	payload, err := intent.Marshal()
	if err != nil {
		return fmt.Errorf("marshal fga unregister intent: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO kacho_nlb.fga_register_outbox (event_type, payload, resource_kind, resource_id)
		VALUES ('fga.unregister', jsonb_set($1::jsonb, '{source_version}', to_jsonb(now())), 'NetworkLoadBalancer', $2)
	`, payload, lbID); err != nil {
		return fmt.Errorf("emit fga unregister: %w", err)
	}
	return nil
}
