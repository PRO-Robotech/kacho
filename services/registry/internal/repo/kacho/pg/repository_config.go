// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/db/pgfault"
	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// repository_config.go — Postgres-adapter (handwritten pgx) config-overlay Repository
// (таблица repository_configs, RG-1). Реализует CQRS-порты
// registry.RepositoryConfigReader/RepositoryConfigWriter. Все инварианты — DB-level
// (PRIMARY KEY(registry_id,name), visibility CHECK, single-statement re-key/visibility
// CAS, FK ON DELETE CASCADE); adapter лишь маппит SQLSTATE→sentinel (ban #10).
//
// Каждый writer оборачивает DML в tx: (1) ACTIVE-guard — SELECT registries.status FOR
// UPDATE (DELETING → FailedPrecondition "registry is being deleted", A24; отсутствует →
// "registry not found"); (2) overlay-DML; (3) эмиссия переданных FGA register/unregister
// intent'ов в registry_outbox В ТОЙ ЖЕ tx (transactional-outbox: adopt-owner/public-grant
// governance атомарна с overlay-DML, at-least-once; iam-недоступность НЕ откатывает
// мутацию — X03). Guard-SELECT FOR UPDATE берёт row-lock реестра → сериализуется с
// Registry MarkDeleting (тот же lock), закрывая гонку «мутируем overlay в DELETING-реестре».

// configColumns — канонический порядок SELECT/RETURNING overlay-строки.
const configColumns = `registry_id, name, description, labels, visibility, created_at, lifecycle`

// RepositoryConfigRepo — реализация registry.RepositoryConfigRepo поверх pgxpool.
type RepositoryConfigRepo struct {
	pool *pgxpool.Pool
}

// NewRepositoryConfigRepo создаёт RepositoryConfigRepo поверх pgxpool.
func NewRepositoryConfigRepo(pool *pgxpool.Pool) *RepositoryConfigRepo {
	return &RepositoryConfigRepo{pool: pool}
}

// ready — pool обязан быть подан composition root'ом (иначе Unavailable, не паника).
func (r *RepositoryConfigRepo) ready() error {
	if r.pool == nil {
		return regerrors.ErrUnavailable
	}
	return nil
}

// GetConfig возвращает overlay-строку по натуральному ключу (registry_id, name).
// pgx.ErrNoRows → ErrNotFound "repository not found" (existence-hiding — в handler).
func (r *RepositoryConfigRepo) GetConfig(ctx context.Context, registryID, name string) (*domain.RepositoryConfig, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT %s FROM %s.repository_configs WHERE registry_id = $1 AND name = $2`,
		configColumns, schema)
	cfg, err := scanConfig(r.pool.QueryRow(ctx, q, registryID, name))
	if err != nil {
		return nil, mapConfigErr(err)
	}
	return cfg, nil
}

// ListConfigs возвращает overlay-строки реестра (created_at, name) ASC. Use-case
// объединяет их с projection (zot) в overlay ⊔ projection union (A20).
func (r *RepositoryConfigRepo) ListConfigs(ctx context.Context, registryID string) ([]*domain.RepositoryConfig, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT %s FROM %s.repository_configs WHERE registry_id = $1
		ORDER BY created_at ASC, name ASC`, configColumns, schema)
	rows, err := r.pool.Query(ctx, q, registryID)
	if err != nil {
		return nil, mapConfigErr(err)
	}
	defer rows.Close()

	var out []*domain.RepositoryConfig
	for rows.Next() {
		cfg, serr := scanConfig(rows)
		if serr != nil {
			return nil, mapConfigErr(serr)
		}
		out = append(out, cfg)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, mapConfigErr(rerr)
	}
	return out, nil
}

// ListConfigsExcludingNames возвращает ОКНО overlay-строк реестра в том же порядке
// (created_at, name) ASC, исключая перечисленные имена — offset/limit.
//
// Отбор ведёт БД по индексу (registry_id, created_at, name), а не память сервиса:
// перечисление репозиториев прежде тянуло ВЕСЬ каталог наложения вместе с метками на
// КАЖДОЙ странице и фильтровало его у себя, поэтому обход реестра стоил O(N²/page_size)
// прочитанных строк, а при page_size=1 страница читала весь реестр целиком. limit<=0 →
// пусто (вырожденное окно не превращаем в «прочитать всё»).
func (r *RepositoryConfigRepo) ListConfigsExcludingNames(ctx context.Context, registryID string, excluded []string, offset, limit int) ([]*domain.RepositoryConfig, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}
	if offset < 0 {
		offset = 0
	}
	if excluded == nil {
		excluded = []string{}
	}
	// Анти-джойн через unnest, а НЕ `name <> ALL($2)`: форма с массивом сравнивает
	// каждую строку с каждым элементом (O(строк×имён)) — это заменило бы память
	// сервиса процессорным временем базы на том же большом реестре. unnest даёт
	// планировщику хеш-анти-джойн и NULL-безопасен (в списке имён NULL быть не может).
	q := fmt.Sprintf(`SELECT %s FROM %s.repository_configs c
		WHERE c.registry_id = $1
		  AND NOT EXISTS (SELECT 1 FROM unnest($2::text[]) AS x(n) WHERE x.n = c.name)
		ORDER BY c.created_at ASC, c.name ASC OFFSET $3 LIMIT $4`, configColumns, schema)
	rows, err := r.pool.Query(ctx, q, registryID, excluded, offset, limit)
	if err != nil {
		return nil, mapConfigErr(err)
	}
	defer rows.Close()
	return scanConfigRows(rows)
}

// ConfigsByNames возвращает overlay-строки реестра ТОЛЬКО для перечисленных имён
// (окно проекции движка). Пустой список — не запрос «все», а пустой ответ: иначе
// вырожденный случай тихо вернул бы полный скан, ради устранения которого метод и
// заведён.
func (r *RepositoryConfigRepo) ConfigsByNames(ctx context.Context, registryID string, names []string) ([]*domain.RepositoryConfig, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	q := fmt.Sprintf(`SELECT %s FROM %s.repository_configs
		WHERE registry_id = $1 AND name = ANY($2)
		ORDER BY created_at ASC, name ASC`, configColumns, schema)
	rows, err := r.pool.Query(ctx, q, registryID, names)
	if err != nil {
		return nil, mapConfigErr(err)
	}
	defer rows.Close()
	return scanConfigRows(rows)
}

// scanConfigRows вычитывает набор overlay-строк (общий хвост списочных запросов).
func scanConfigRows(rows pgx.Rows) ([]*domain.RepositoryConfig, error) {
	var out []*domain.RepositoryConfig
	for rows.Next() {
		cfg, serr := scanConfig(rows)
		if serr != nil {
			return nil, mapConfigErr(serr)
		}
		out = append(out, cfg)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, mapConfigErr(rerr)
	}
	return out, nil
}

// InsertConfig вставляет overlay-строку под ACTIVE-guard + эмитит intents одной tx
// (Create durable; adopt-additive поверх проекции — overlay ⟂ projection; ephemeral
// rename auto-promote A23). PRIMARY KEY(registry_id,name)-конфликт → 23505 →
// ErrAlreadyExists ("repository already exists"). Реестр DELETING → FailedPrecondition
// (A24); отсутствует → FailedPrecondition "registry not found" (guard-parity с FK 23503).
func (r *RepositoryConfigRepo) InsertConfig(ctx context.Context, cfg *domain.RepositoryConfig, intents ...registry.OutboxIntent) (*domain.RepositoryConfig, []registry.OutboxIntent, error) {
	if err := r.ready(); err != nil {
		return nil, nil, err
	}
	labels, err := marshalLabels(cfg.Labels)
	if err != nil {
		return nil, nil, regerrors.ErrInternal
	}
	return runConfigTx(ctx, r.pool, cfg.RegistryID, intents, func(tx pgx.Tx) (*domain.RepositoryConfig, error) {
		q := fmt.Sprintf(`
			INSERT INTO %s.repository_configs (registry_id, name, description, labels, visibility, lifecycle)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6)
			RETURNING %s`, schema, configColumns)
		return scanConfig(tx.QueryRow(ctx, q,
			cfg.RegistryID, cfg.Name, cfg.Description, labels, cfg.Visibility.String(), cfg.Lifecycle.String()))
	})
}

// UpdateConfig применяет mutable-поля (Apply*-флаги) одним UPDATE ... RETURNING под
// ACTIVE-guard + эмиссию intents одной tx. visibility-flip сериализуется row-lock'ом
// (детерминированный терминал, B09). 0 rows (строки нет) → ErrNotFound.
//
// REG-1 F7 AUTO-PROMOTE: любой overlay-set ставит lifecycle='DURABLE' (REG-1-23) —
// explicit UpdateRepository = durable intent; EPHEMERAL overlay поднимается до DURABLE
// (наблюдаемо через enum). Понижение DURABLE→EPHEMERAL через API не выразимо (REG-1-24).
// lifecycle всегда в SET → UPDATE выполняется даже при отсутствии других mutable-полей
// (тот же row-lock; конкурентный promote идемпотентен — REG-1-25).
func (r *RepositoryConfigRepo) UpdateConfig(ctx context.Context, spec registry.RepositoryConfigUpdate, intents ...registry.OutboxIntent) (*domain.RepositoryConfig, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	// lifecycle='DURABLE' — auto-promote-инвариант overlay-set (первым, литерал без арга).
	sets := []string{"lifecycle = 'DURABLE'"}
	args := []any{spec.RegistryID, spec.Name}
	idx := 3
	if spec.ApplyDescription {
		sets = append(sets, fmt.Sprintf("description = $%d", idx))
		args = append(args, spec.Description)
		idx++
	}
	if spec.ApplyLabels {
		labels, err := marshalLabels(spec.Labels)
		if err != nil {
			return nil, regerrors.ErrInternal
		}
		sets = append(sets, fmt.Sprintf("labels = $%d::jsonb", idx))
		args = append(args, labels)
		idx++
	}
	if spec.ApplyVisibility {
		sets = append(sets, fmt.Sprintf("visibility = $%d", idx))
		args = append(args, spec.Visibility.String())
	}

	// Синхронной доставки на этом пути нет — проштампованные намерения не нужны.
	out, _, uerr := runConfigTx(ctx, r.pool, spec.RegistryID, intents, func(tx pgx.Tx) (*domain.RepositoryConfig, error) {
		q := fmt.Sprintf(`
			UPDATE %s.repository_configs SET %s
			WHERE registry_id = $1 AND name = $2
			RETURNING %s`, schema, strings.Join(sets, ", "), configColumns)
		return scanConfig(tx.QueryRow(ctx, q, args...))
	})
	return out, uerr
}

// RekeyConfig — durable rename: одностейтментный перенос name-колонки существующей
// overlay-строки под ACTIVE-guard + intents одной tx. Занятое целевое имя → PRIMARY KEY
// 23505 → ErrAlreadyExists (A16/A17/A18); исходной строки нет → 0 rows → ErrNotFound.
// Ephemeral auto-promote (нет overlay-строки) — НЕ этот путь: он через InsertConfig под
// new_name (D-5/A23).
func (r *RepositoryConfigRepo) RekeyConfig(ctx context.Context, registryID, oldName, newName string, intents ...registry.OutboxIntent) (*domain.RepositoryConfig, []registry.OutboxIntent, error) {
	if err := r.ready(); err != nil {
		return nil, nil, err
	}
	return runConfigTx(ctx, r.pool, registryID, intents, func(tx pgx.Tx) (*domain.RepositoryConfig, error) {
		// Rename = overlay-set → auto-promote lifecycle='DURABLE' (REG-1-23 parity).
		q := fmt.Sprintf(`
			UPDATE %s.repository_configs SET name = $3, lifecycle = 'DURABLE'
			WHERE registry_id = $1 AND name = $2
			RETURNING %s`, schema, configColumns)
		return scanConfig(tx.QueryRow(ctx, q, registryID, oldName, newName))
	})
}

// DeleteConfig снимает overlay-строку (DELETE ... RETURNING name) под ACTIVE-guard +
// intents одной tx. 0 rows (строки нет / уже снята) → ErrNotFound — конкурентный/
// повторный Delete не даёт дубля.
func (r *RepositoryConfigRepo) DeleteConfig(ctx context.Context, registryID, name string, intents ...registry.OutboxIntent) error {
	if err := r.ready(); err != nil {
		return err
	}
	_, _, err := runConfigTx(ctx, r.pool, registryID, intents, func(tx pgx.Tx) (*domain.RepositoryConfig, error) {
		var deleted string
		q := fmt.Sprintf(`DELETE FROM %s.repository_configs
			WHERE registry_id = $1 AND name = $2 RETURNING name`, schema)
		if serr := tx.QueryRow(ctx, q, registryID, name).Scan(&deleted); serr != nil {
			return nil, serr
		}
		return &domain.RepositoryConfig{RegistryID: registryID, Name: deleted}, nil
	})
	return err
}

// ---- tx-orchestration ----

// runConfigTx открывает writer-tx, применяет ACTIVE-guard реестра, исполняет DML-callback
// (single-statement INSERT/UPDATE/DELETE ... RETURNING), эмитит FGA intent'ы в
// registry_outbox В ТОЙ ЖЕ tx и коммитит. DML/guard/scan-ошибка маппится mapConfigErr;
// осиротевший rollback — defer. Пустой набор intent'ов → чистый guard+DML.
func runConfigTx(ctx context.Context, pool *pgxpool.Pool, registryID string, intents []registry.OutboxIntent, dml func(pgx.Tx) (*domain.RepositoryConfig, error)) (*domain.RepositoryConfig, []registry.OutboxIntent, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, nil, mapConfigErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if gerr := guardRegistryActive(ctx, tx, registryID); gerr != nil {
		return nil, nil, gerr
	}
	out, derr := dml(tx)
	if derr != nil {
		return nil, nil, mapConfigErr(derr)
	}
	// Проштампованные намерения возвращаются вызывающему: синхронная доставка
	// обязана нести ТУ ЖЕ версию, что легла в очередь, иначе гашение повторной
	// доставки у владельца прав зависит от того, кто выиграл гонку.
	stampedIntents := make([]registry.OutboxIntent, 0, len(intents))
	for _, oi := range intents {
		stamped, eerr := emitFGAIntent(ctx, tx, oi.Event, oi.Intent)
		if eerr != nil {
			return nil, nil, eerr
		}
		stampedIntents = append(stampedIntents, registry.OutboxIntent{Event: oi.Event, Intent: stamped})
	}
	if cerr := tx.Commit(ctx); cerr != nil {
		return nil, nil, mapConfigErr(cerr)
	}
	return out, stampedIntents, nil
}

// guardRegistryActive — ACTIVE-guard overlay-мутации (A24): SELECT registries.status FOR
// UPDATE в текущей tx. Реестр отсутствует → FailedPrecondition "registry not found"
// (guard-parity с FK 23503); DELETING → FailedPrecondition "registry is being deleted"
// (терминальный реестр не принимает новую repo-конфигурацию). FOR UPDATE берёт row-lock
// реестра → сериализуется с Registry MarkDeleting (гонка «мутируем overlay в DELETING»).
func guardRegistryActive(ctx context.Context, tx pgx.Tx, registryID string) error {
	var status string
	q := fmt.Sprintf(`SELECT status FROM %s.registries WHERE id = $1 FOR UPDATE`, schema)
	if err := tx.QueryRow(ctx, q, registryID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: registry not found", regerrors.ErrFailedPrecondition)
		}
		return mapConfigErr(err)
	}
	if status == "DELETING" {
		return fmt.Errorf("%w: registry is being deleted", regerrors.ErrFailedPrecondition)
	}
	return nil
}

// ---- helpers ----

// scanConfig читает overlay-строку из pgx.Row/pgx.Rows в domain.RepositoryConfig.
func scanConfig(row pgx.Row) (*domain.RepositoryConfig, error) {
	var (
		cfg          domain.RepositoryConfig
		labelsRaw    []byte
		visRaw       string
		lifecycleRaw string
	)
	if err := row.Scan(&cfg.RegistryID, &cfg.Name, &cfg.Description, &labelsRaw, &visRaw, &cfg.CreatedAt, &lifecycleRaw); err != nil {
		return nil, err
	}
	labels, err := unmarshalLabels(labelsRaw)
	if err != nil {
		return nil, err
	}
	cfg.Labels = labels
	cfg.Visibility = domain.VisibilityFromString(visRaw)
	cfg.Lifecycle = domain.LifecycleFromString(lifecycleRaw)
	return &cfg, nil
}

// mapConfigErr транслирует pgx/SQLSTATE в sentinel kacho-registry с ТОЧНЫМ
// контракт-текстом overlay Repository (api-conventions.md error-format). Сырой pgx
// наружу не течёт (некатегоризированное → фикс. INTERNAL, security.md hardening #1).
//
//	pgx.ErrNoRows → ErrNotFound            "repository not found"
//	23505 PK/UNIQUE → ErrAlreadyExists     "repository already exists"
//	23503 FK        → ErrFailedPrecondition "registry not found"
//	23514 CHECK     → ErrInvalidArg        "invalid repository config"
//	иначе           → ErrInternal (+ внутренний лог SQLSTATE)
func mapConfigErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, regerrors.ErrFailedPrecondition) || errors.Is(err, regerrors.ErrInvalidArg) ||
		errors.Is(err, regerrors.ErrAlreadyExists) || errors.Is(err, regerrors.ErrNotFound) ||
		errors.Is(err, regerrors.ErrUnavailable) || errors.Is(err, regerrors.ErrInternal) {
		return err // уже sentinel (guard/emitFGAIntent) — не переоборачиваем
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: repository not found", regerrors.ErrNotFound)
	}
	f := pgfault.Classify(err)
	if f.FromDatabase() {
		switch f.Class {
		case pgfault.Unique: // PRIMARY KEY(registry_id, name)
			return fmt.Errorf("%w: repository already exists", regerrors.ErrAlreadyExists)
		case pgfault.ForeignKey: // registry_id → registries(id)
			return fmt.Errorf("%w: registry not found", regerrors.ErrFailedPrecondition)
		case pgfault.Check: // visibility / labels-object
			return fmt.Errorf("%w: invalid repository config", regerrors.ErrInvalidArg)
		}
		slog.Default().Error("registry repo: unclassified repository_configs error", f.LogAttrs()...)
		return regerrors.ErrInternal
	}
	slog.Default().Error("registry repo: unclassified repository_configs error", "err", err.Error())
	return regerrors.ErrInternal
}

var _ registry.RepositoryConfigRepo = (*RepositoryConfigRepo)(nil)
