// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package pg — Postgres-adapter (handwritten pgx) для таблицы registries
// kacho-registry. Реализует CQRS-порты registry.RegistryReader/RegistryWriter.
//
// Мутации атомарны на DB-уровне (INSERT/UPDATE ... RETURNING, CAS-переход
// ACTIVE→DELETING через UPDATE ... WHERE, DELETE ... RETURNING) и пишут owner-tuple
// register/unregister intent в registry_outbox в ТОЙ ЖЕ writer-tx — transactional
// outbox, без software check-then-act и без dual-write.
package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/filter"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// schema — квалификатор таблиц kacho-registry (не полагаемся на search_path).
const schema = "kacho_registry"

// registryColumns — канонический порядок SELECT/RETURNING строки реестра.
const registryColumns = `id, project_id, name, description, labels, status, created_at, default_visibility, region_id, placement_type`

// RegistryRepo — реализация registry.RegistryRepo поверх pgxpool.
type RegistryRepo struct {
	pool *pgxpool.Pool
}

// NewRegistryRepo создаёт RegistryRepo поверх pgxpool.
func NewRegistryRepo(pool *pgxpool.Pool) *RegistryRepo { return &RegistryRepo{pool: pool} }

// ready — pool обязан быть подан composition root'ом (иначе Unavailable, не паника).
func (r *RegistryRepo) ready() error {
	if r.pool == nil {
		return regerrors.ErrUnavailable
	}
	return nil
}

// Get возвращает реестр по id. pgx.ErrNoRows → ErrNotFound через wrapPgErr.
func (r *RegistryRepo) Get(ctx context.Context, id string) (*domain.Registry, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT %s FROM %s.registries WHERE id = $1`, registryColumns, schema)
	reg, err := scanRegistry(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, wrapPgErr(err, "Registry", id)
	}
	return reg, nil
}

// RegistryProjectID — узкий lookup owning-project реестра по id (data-plane
// register-on-first-push: интент репо должен нести ParentProjectID для containment
// scope в iam-mirror). pgx.ErrNoRows → ErrNotFound через wrapPgErr.
func (r *RegistryRepo) RegistryProjectID(ctx context.Context, id string) (string, error) {
	if err := r.ready(); err != nil {
		return "", err
	}
	var projectID string
	q := fmt.Sprintf(`SELECT project_id FROM %s.registries WHERE id = $1`, schema)
	if err := r.pool.QueryRow(ctx, q, id).Scan(&projectID); err != nil {
		return "", wrapPgErr(err, "Registry", id)
	}
	return projectID, nil
}

// List возвращает реестры project'а cursor-пагинацией (created_at,id) ASC.
// filter — whitelist `name=` (corelib filter.Parse; garbage → InvalidArgument);
// garbage page_token → InvalidArgument. Запрашивает pageSize+1 для next-cursor.
func (r *RegistryRepo) List(ctx context.Context, q registry.ListQuery) ([]*domain.Registry, string, error) {
	if err := r.ready(); err != nil {
		return nil, "", err
	}

	conds := []string{}
	args := []any{}
	idx := 1
	if q.ProjectID != "" {
		conds = append(conds, fmt.Sprintf("project_id = $%d", idx))
		args = append(args, q.ProjectID)
		idx++
	}

	ast, err := filter.Parse(q.Filter, []string{"name"})
	if err != nil {
		return nil, "", invalidFilterErr(err)
	}
	if ast != nil {
		conds = append(conds, fmt.Sprintf("name = $%d", idx))
		args = append(args, ast.Value)
		idx++
	}

	if q.PageToken != "" {
		cur, derr := decodePageToken(q.PageToken)
		if derr != nil {
			return nil, "", invalidPageTokenErr(derr)
		}
		conds = append(conds, fmt.Sprintf("(created_at, id) > ($%d, $%d)", idx, idx+1))
		args = append(args, cur.CreatedAt, cur.ID)
		idx += 2
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	sql := fmt.Sprintf(
		`SELECT %s FROM %s.registries %s ORDER BY created_at ASC, id ASC LIMIT $%d`,
		registryColumns, schema, where, idx,
	)
	args = append(args, pageSize+1)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", wrapPgErr(err, "Registry", "")
	}
	defer rows.Close()

	var out []*domain.Registry
	for rows.Next() {
		reg, serr := scanRegistry(rows)
		if serr != nil {
			return nil, "", wrapPgErr(serr, "Registry", "")
		}
		out = append(out, reg)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, "", wrapPgErr(rerr, "Registry", "")
	}

	var next string
	if int64(len(out)) > pageSize {
		last := out[pageSize-1]
		next = encodePageToken(last.CreatedAt, last.ID)
		out = out[:pageSize]
	}
	return out, next, nil
}

// Insert создаёт реестр + register-intent в registry_outbox ОДНОЙ writer-tx.
// partial UNIQUE(project_id,name)WHERE status<>'DELETING' → 23505 → ErrAlreadyExists.
func (r *RegistryRepo) Insert(ctx context.Context, reg *domain.Registry, intent domain.RegisterIntent) (*domain.Registry, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	labels, err := marshalLabels(reg.Labels)
	if err != nil {
		return nil, regerrors.ErrInternal
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, wrapPgErr(err, "Registry", reg.ID)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := fmt.Sprintf(`
		INSERT INTO %s.registries (id, project_id, name, description, labels, status, region_id, placement_type)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		RETURNING %s`, schema, registryColumns)
	created, err := scanRegistry(tx.QueryRow(ctx, q,
		reg.ID, reg.ProjectID, reg.Name, reg.Description, labels, statusString(reg.Status),
		reg.RegionID, placementTypeString(reg.PlacementType)))
	if err != nil {
		return nil, wrapPgErr(err, "Registry", reg.ID)
	}

	if err := emitFGAIntent(ctx, tx, domain.FGAEventRegister, intent); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapPgErr(err, "Registry", reg.ID)
	}
	return created, nil
}

// Update применяет mutable-поля по Apply*-флагам одним UPDATE ... RETURNING;
// mirror register-intent (обновлённые labels) строится callback'ом из RETURNING
// строки и эмитится в той же tx. 0 rows (нет ACTIVE-реестра) → ErrNotFound.
func (r *RegistryRepo) Update(ctx context.Context, spec registry.UpdateSpec, mirror func(*domain.Registry) domain.RegisterIntent) (*domain.Registry, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}

	sets := []string{}
	args := []any{spec.RegistryID}
	idx := 2
	if spec.ApplyName {
		// Смена имени: partial-UNIQUE(project_id,name) WHERE status<>'DELETING' →
		// конфликт даёт 23505 → wrapPgErr → ErrAlreadyExists.
		sets = append(sets, fmt.Sprintf("name = $%d", idx))
		args = append(args, spec.Name)
		idx++
	}
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
	if spec.ApplyDefaultVisibility {
		sets = append(sets, fmt.Sprintf("default_visibility = $%d", idx))
		args = append(args, spec.DefaultVisibility.String())
		// default_visibility — последнее применяемое поле; idx дальше не читается.
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, wrapPgErr(err, "Registry", spec.RegistryID)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var updated *domain.Registry
	if len(sets) == 0 {
		// Пустой набор применяемых полей (mask без mutable-полей) — возвращаем
		// текущую ACTIVE-строку; mirror по-прежнему re-register'ит labels. FOR UPDATE
		// берёт тот же row-lock, что и SET-ветка (UPDATE ... WHERE), поэтому outbox-
		// INSERT этой ветки сериализуется с конкурентными реальными UPDATE того же
		// реестра — source_version (clock_timestamp() на INSERT'е) остаётся
		// commit-order-monotonic и mirror не откатывает label-scope к устаревшему
		// снапшоту (MVCC-reader без FOR UPDATE не блокируется на writer-row-lock и мог бы
		// получить больший маркер при stale labels). Ветка ныне недостижима через use-case, но FOR UPDATE закрывает
		// её как foot-gun для любого прямого/будущего caller'а с пустым Apply-набором.
		q := fmt.Sprintf(`SELECT %s FROM %s.registries WHERE id = $1 AND status = 'ACTIVE' FOR UPDATE`,
			registryColumns, schema)
		updated, err = scanRegistry(tx.QueryRow(ctx, q, spec.RegistryID))
	} else {
		q := fmt.Sprintf(`
			UPDATE %s.registries SET %s
			WHERE id = $1 AND status = 'ACTIVE'
			RETURNING %s`, schema, strings.Join(sets, ", "), registryColumns)
		updated, err = scanRegistry(tx.QueryRow(ctx, q, args...))
	}
	if err != nil {
		return nil, wrapPgErr(err, "Registry", spec.RegistryID)
	}

	if err := emitFGAIntent(ctx, tx, domain.FGAEventRegister, mirror(updated)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, wrapPgErr(err, "Registry", spec.RegistryID)
	}
	return updated, nil
}

// MarkDeleting — атомарный forward-only CAS в DELETING: ACTIVE→DELETING (или
// идемпотентно DELETING→DELETING, чтобы retry/крэш-рекавери довели удаление до
// конца). 0 rows только когда строки нет (уже удалена) → ErrNotFound. revert в
// ACTIVE невозможен (нет пути DELETING→ACTIVE).
func (r *RegistryRepo) MarkDeleting(ctx context.Context, id string) (*domain.Registry, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`
		UPDATE %s.registries SET status = 'DELETING'
		WHERE id = $1 AND status IN ('ACTIVE', 'DELETING')
		RETURNING %s`, schema, registryColumns)
	reg, err := scanRegistry(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, wrapPgErr(err, "Registry", id)
	}
	return reg, nil
}

// Delete физически удаляет строку реестра + unregister-intent'ы (самого реестра И каждого
// зарегистрированного под ним репозитория) в той же writer-tx. Намерения эмитятся ТОЛЬКО
// когда строка реально удалена (DELETE RETURNING 1 row) — конкурентный/повторный Delete
// видит 0 rows → ErrNotFound без второго destructive-дубля.
//
// ПОЧЕМУ ДЕТИ ПЕРЕЧИСЛЯЮТСЯ ЗДЕСЬ, А НЕ ОСТАЮТСЯ КАСКАДУ. Признак существования
// репозитория висит на реестре через FK `ON DELETE CASCADE` (миграция 0014). Каскад
// снимает признак — и НИЧЕГО не эмитирует: та же миграция объявляет, что признак и
// намерение не могут разъехаться, и ровно в этом направлении FK её и опровергал. Со
// стороны это неотличимо от исправной работы: ресурса нет, ошибок нет, очередь пуста, —
// а объект репозитория остаётся в хранилище прав со всем, что на нём было. Замер на
// стенде 2026-08-04: 479 регистраций против 60 снятий при нуле живых репозиториев.
//
// Перечисление идёт ДО DELETE — после него строк уже нет by construction. Обе операции в
// одной транзакции, поэтому «реестр удалён» и «за детей отчитались» либо оба верны, либо
// оба нет.
func (r *RegistryRepo) Delete(ctx context.Context, id string, intent domain.RegisterIntent) error {
	if err := r.ready(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return wrapPgErr(err, "Registry", id)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	children, err := childRepoRegistrations(ctx, tx, id)
	if err != nil {
		return err
	}

	var deletedID string
	q := fmt.Sprintf(`DELETE FROM %s.registries WHERE id = $1 RETURNING id`, schema)
	if err := tx.QueryRow(ctx, q, id).Scan(&deletedID); err != nil {
		return wrapPgErr(err, "Registry", id)
	}

	if err := emitFGAIntent(ctx, tx, domain.FGAEventUnregister, intent); err != nil {
		return err
	}
	for _, child := range children {
		if err := emitFGAIntent(ctx, tx, domain.FGAEventUnregister,
			domain.UnregisterIntentForRepo(id, child)); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return wrapPgErr(err, "Registry", id)
	}
	return nil
}

// RepositoryDeclared сообщает, существует ли репозиторий как РЕСУРС: есть строка
// наложения (repository_configs — заявлен через control-plane) ЛИБО строка регистрации
// (registry_repository_registration — заявлен первым push'ем, миграция 0014). Оба
// источника durable и принадлежат сервису.
//
// Содержимое движка (теги) в предикат НЕ входит: тег — это содержимое, а не ресурс.
// Заявленный и ещё пустой репозиторий существует; репозиторий, чьи теги удалили, но
// наложение осталось, — тоже (он и снимается только DeleteRepository). Именно этим
// предикатом data-plane выбирает глагол записи: существует ⇒ «изменить ЭТОТ
// репозиторий», не существует ⇒ «создать репозиторий в реестре».
//
// Один запрос двумя EXISTS — обе таблицы в схеме сервиса, лишнего round-trip нет.
func (r *RegistryRepo) RepositoryDeclared(ctx context.Context, registryID, repo string) (bool, error) {
	if err := r.ready(); err != nil {
		return false, err
	}
	q := fmt.Sprintf(`SELECT
		  EXISTS (SELECT 1 FROM %s.repository_configs WHERE registry_id = $1 AND name = $2)
		  OR
		  EXISTS (SELECT 1 FROM %s.registry_repository_registration WHERE registry_id = $1 AND repo = $2)`,
		schema, schema)
	var declared bool
	if err := r.pool.QueryRow(ctx, q, registryID, repo).Scan(&declared); err != nil {
		return false, wrapPgErr(err, "Repository", registryID+"/"+repo)
	}
	return declared, nil
}

// RegisterRepository эмитит register-intent (parent+owner tuple) нового repo в
// registry_outbox И записывает durable-признак его существования
// (registry_repository_registration) — обе записи в ОДНОЙ tx (см. emitFGAIntent).
// Register-drainer применяет intent через fga-proxy идемпотентно (повторный push того
// же repo даёт дубль-intent, iam дедуплицирует → AlreadyApplied).
func (r *RegistryRepo) RegisterRepository(ctx context.Context, intent domain.RegisterIntent) error {
	return r.emitRepoIntent(ctx, domain.FGAEventRegister, intent)
}

// UnregisterRepository эмитит unregister-intent repo (снятие parent-tuple) в
// registry_outbox И снимает durable-признак существования — обе записи в ОДНОЙ tx
// (см. emitFGAIntent): ресурса больше нет, поэтому следующая запись под этим именем
// снова гейтится правом «создавать репозитории в реестре», а не правом на объект,
// у которого уже нет ни одного tuple.
func (r *RegistryRepo) UnregisterRepository(ctx context.Context, intent domain.RegisterIntent) error {
	return r.emitRepoIntent(ctx, domain.FGAEventUnregister, intent)
}

// emitRepoIntent — durable-emit repo-intent одиночной tx (у repo нет ресурсной DML,
// с которой надо было бы атомарить). Пустой набор tuple → no-op.
//
// В отличие от registry-scoped мутаций (Insert/Update/Delete сериализуются row-lock'ом
// на registries — воркер, закоммитивший позже, обязательно выполнил outbox-INSERT позже
// и получил больший source_version), у repo-объекта НЕТ registries-строки для
// row-lock (source of truth репо = zot). Без явной сериализации две конкурентные
// register/unregister ОДНОГО repo-объекта могли бы закоммититься в порядке, расходящемся
// с их source_version → iam-mirror last-source-state-wins выбрал бы не финально-
// закоммиченное состояние (dangling authz-объект / непуллимый свежий repo). Поэтому берём
// per-repo pg_advisory_xact_lock(hashtext(resource_id)) ПЕРЕД outbox-INSERT: concurrent
// intent'ы одного repo-объекта сериализуются (второй ждёт commit первого → получает
// больший маркер), а разные repo-объекты друг друга не блокируют. Lock — xact-scoped,
// снимается на commit/rollback.
func (r *RegistryRepo) emitRepoIntent(ctx context.Context, eventType string, intent domain.RegisterIntent) error {
	if err := r.ready(); err != nil {
		return err
	}
	if len(intent.Tuples) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return wrapPgErr(err, "registry_outbox", intent.ResourceID)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, intent.ResourceID); err != nil {
		return wrapPgErr(err, "registry_outbox", intent.ResourceID)
	}
	if err := emitFGAIntent(ctx, tx, eventType, intent); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return wrapPgErr(err, "registry_outbox", intent.ResourceID)
	}
	return nil
}

// ---- helpers ----

// scanRegistry читает строку реестра из pgx.Row/pgx.Rows в domain.Registry.
func scanRegistry(row pgx.Row) (*domain.Registry, error) {
	var (
		reg           domain.Registry
		labelsRaw     []byte
		statusRaw     string
		defaultVisRaw string
		placementRaw  string
	)
	if err := row.Scan(&reg.ID, &reg.ProjectID, &reg.Name, &reg.Description, &labelsRaw, &statusRaw, &reg.CreatedAt, &defaultVisRaw, &reg.RegionID, &placementRaw); err != nil {
		return nil, err
	}
	labels, err := unmarshalLabels(labelsRaw)
	if err != nil {
		return nil, err
	}
	reg.Labels = labels
	reg.Status = statusFromString(statusRaw)
	reg.DefaultVisibility = domain.VisibilityFromString(defaultVisRaw)
	reg.PlacementType = placementTypeFromString(placementRaw)
	return &reg, nil
}

// emitFGAIntent пишет register/unregister intent в registry_outbox в текущей tx.
// source_version штампуется BEFORE INSERT триггером как clock_timestamp() (миграция
// 0011, ранее — BIGSERIAL id строки) — commit-order-monotonic per-object маркер:
// воркер, закоммитивший позже, получил больший маркер (его INSERT выполнился позже под
// row-lock сериализацией, а clock_timestamp() читает часы именно на INSERT'е, в отличие
// от now()==transaction_timestamp), поэтому last-source-state-wins в iam-mirror
// корректен. Шкала — время, а не sequence, потому что тот же маркер обязан быть сравним
// с версией, которую синхронный registrar штампует после commit'а (у него нет
// outbox-id), и потому что iam хранит его как timestamptz. Значение SourceVersion,
// приехавшее из Go, триггер ПЕРЕЗАПИСЫВАЕТ. Пустой набор tuple → no-op.
// childRepoRegistrations перечисляет репозитории, зарегистрированные под реестром, внутри
// уже открытой транзакции удаления. Порядок задан, чтобы намерения ложились в очередь
// детерминированно (у очереди дренаж по голове партиции — воспроизводимый порядок дешевле
// разбирать).
//
// Читается ТОЛЬКО таблица признака: наложение (repository_configs) описывает заявленную
// конфигурацию и снимается своим путём, а предмет здесь — существование объекта прав.
func childRepoRegistrations(ctx context.Context, tx pgx.Tx, registryID string) ([]string, error) {
	q := fmt.Sprintf(
		`SELECT repo FROM %s.registry_repository_registration WHERE registry_id = $1 ORDER BY repo`,
		schema)
	rows, err := tx.Query(ctx, q, registryID)
	if err != nil {
		return nil, wrapPgErr(err, "Registry", registryID)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			return nil, wrapPgErr(err, "Registry", registryID)
		}
		out = append(out, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapPgErr(err, "Registry", registryID)
	}
	return out, nil
}

func emitFGAIntent(ctx context.Context, tx pgx.Tx, eventType string, intent domain.RegisterIntent) error {
	if len(intent.Tuples) == 0 {
		return nil
	}
	payload, err := intent.Marshal()
	if err != nil {
		return regerrors.ErrInternal
	}
	q := fmt.Sprintf(`
		INSERT INTO %s.registry_outbox (event_type, payload, resource_kind, resource_id)
		VALUES ($1, $2::jsonb, $3, $4)`, schema)
	if _, err := tx.Exec(ctx, q, eventType, string(payload), intent.Kind, intent.ResourceID); err != nil {
		return wrapPgErr(err, "registry_outbox", intent.ResourceID)
	}
	return applyRepoRegistration(ctx, tx, eventType, intent)
}

// applyRepoRegistration поддерживает durable-признак существования репозитория
// (registry_repository_registration, миграция 0014) В ТОЙ ЖЕ tx, что и эмиссия его
// интента: register → строка появляется, unregister → строка исчезает. Атомарность
// не «соблюдается», а обеспечена by construction — откат tx убирает и строку очереди,
// и признак, поэтому «намерение эмитировано» и «ресурс существует» разъехаться не могут.
//
// Сидит в emitFGAIntent, а НЕ в emitRepoIntent, потому что интент репозитория эмитируют
// ОБА писателя: data-plane (emitRepoIntent, push/удаление последнего тега) и control-plane
// (runConfigTx: CreateRepository/RenameRepository/DeleteRepository). Признак, поддержанный
// только на первом пути, пережил бы DeleteRepository — наложение снято, признак остался,
// значит следующая запись под этим именем гейтилась бы правом на объект, у которого больше
// нет ни одного tuple: имя оказалось бы заперто для всех, включая владельца реестра.
//
// Разбор INSERT/DELETE — идемпотентен (ON CONFLICT DO NOTHING / DELETE без совпадения),
// поэтому повторный register уже существующего репозитория (re-push, adopt поверх
// проекции) остаётся no-op'ом, а повторный unregister не падает.
func applyRepoRegistration(ctx context.Context, tx pgx.Tx, eventType string, intent domain.RegisterIntent) error {
	registryID, repo, ok := repoRegistrationKey(intent)
	if !ok {
		return nil
	}
	var q string
	switch eventType {
	case domain.FGAEventRegister:
		q = fmt.Sprintf(`INSERT INTO %s.registry_repository_registration (registry_id, repo)
			VALUES ($1, $2) ON CONFLICT (registry_id, repo) DO NOTHING`, schema)
	case domain.FGAEventUnregister:
		q = fmt.Sprintf(`DELETE FROM %s.registry_repository_registration
			WHERE registry_id = $1 AND repo = $2`, schema)
	default:
		return nil
	}
	if _, err := tx.Exec(ctx, q, registryID, repo); err != nil {
		return wrapPgErr(err, "Repository", intent.ResourceID)
	}
	return nil
}

// repoRegistrationKey извлекает (registry_id, repo) из интента, который объявляет или
// снимает репозиторий как РЕСУРС, и сообщает ok=false для всех прочих интентов.
//
// Дискриминатор — наличие СТРУКТУРНОЙ привязки репозитория к реестру (parent-tuple
// registry_registry:<reg> → registry_repository:<reg>/<repo>): именно она вводит объект
// в иерархию и снимается вместе с ним. Ключ берётся из САМОГО tuple, а не из
// RegisterIntent.Kind/ResourceID: те объявлены полями «для observability, не участвуют
// в apply» (domain.RegisterIntent) — сделать их несущими значило бы оставить рядом
// комментарий, противоречащий коду. По этому же признаку мимо проходят интенты
// public-grant (их единственный tuple — "user:* v_get", привязку не трогает) и интенты
// самого реестра (их объект — registry_registry).
func repoRegistrationKey(intent domain.RegisterIntent) (registryID, repo string, ok bool) {
	const objectPrefix = domain.FGAObjectTypeRepository + ":"
	for _, t := range intent.Tuples {
		if t.Relation != domain.FGARelationParent {
			continue
		}
		objectID, found := strings.CutPrefix(t.Object, objectPrefix)
		if !found {
			continue
		}
		// object-id репозитория — "<registryID>/<repo>"; repo сам может быть
		// multi-segment ("team/app"), поэтому режем по ПЕРВОМУ разделителю.
		registryID, repo, found = strings.Cut(objectID, "/")
		if !found || registryID == "" || repo == "" {
			continue
		}
		return registryID, repo, true
	}
	return "", "", false
}

// marshalLabels сериализует карту labels в JSON-строку (jsonb-колонка через
// `$N::jsonb`). nil/пустая карта → "{}".
func marshalLabels(labels map[string]string) (string, error) {
	if len(labels) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalLabels разбирает jsonb-колонку labels в карту (пусто → nil).
func unmarshalLabels(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}

// statusString / statusFromString — маппинг domain-enum ↔ TEXT-колонка status.
func statusString(s domain.RegistryStatus) string {
	if s == domain.RegistryStatusDeleting {
		return "DELETING"
	}
	return "ACTIVE"
}

func statusFromString(s string) domain.RegistryStatus {
	if s == "DELETING" {
		return domain.RegistryStatusDeleting
	}
	return domain.RegistryStatusActive
}

// placementTypeString / placementTypeFromString — маппинг domain-enum ↔ TEXT-колонка
// placement_type (REG-1 F4). Registry — always-REGIONAL: любой не-REGIONAL (включая
// UNSPECIFIED) на записи схлопывается в 'REGIONAL' (DB-CHECK гарантирует домен).
func placementTypeString(p domain.PlacementType) string {
	_ = p // registry — always REGIONAL (const carve-out); значение не варьируется
	return "REGIONAL"
}

func placementTypeFromString(s string) domain.PlacementType {
	if s == "REGIONAL" {
		return domain.PlacementTypeRegional
	}
	return domain.PlacementTypeUnspecified
}

// invalidFilterErr оборачивает ошибку парсинга filter в domain-sentinel
// ErrInvalidArg (repo НЕ формирует gRPC-статус — единый маппинг sentinel→gRPC в
// serviceerr; CLAUDE.md dependency rule). serviceerr.ToStatus срежет префикс →
// клиент видит стабильное "invalid filter: <причина>" с кодом INVALID_ARGUMENT.
func invalidFilterErr(err error) error {
	return fmt.Errorf("%w: invalid filter: %v", regerrors.ErrInvalidArg, err)
}

var _ registry.RegistryRepo = (*RegistryRepo)(nil)
var _ registry.RepoRegistrar = (*RegistryRepo)(nil)
