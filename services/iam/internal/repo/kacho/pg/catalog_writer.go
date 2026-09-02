// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

// catalog_writer.go — ЕДИНСТВЕННЫЙ писатель строк каталога модуля в прод-коде
// (`kacho_iam.catalog_module` / `catalog_resource` / `catalog_verb`), задача
// продукта #1034.
//
// # Почему писатель живёт РЯДОМ С ЧИТАТЕЛЕМ, а не на `kacho.Writer`
//
// Каталог — данные ПЛАТФОРМЫ, а `kacho.Writer` — писательская транзакция
// арендаторских ресурсов: аккаунты, проекты, роли, выдачи. Каталог там был бы
// методом, который ни один арендаторский use-case не вправе позвать, — и он был
// бы у всех. Читатель каталога (`catalog_repo.go`) по этой же причине живёт над
// пулом, а не за `kacho.Reader`; писатель следует за ним.
//
// Транзакцию открывает `pkg/db.Transactor` — ЕДИНСТВЕННОЕ платформенное
// объявление этого паттерна над пулом. Своя последовательность
// begin→commit→rollback была бы вторым местом об одном предмете и разошлась бы
// с первым молча.
//
// # Отказы приходят СЫРЫМИ, и это решение
//
// Приведение к статусу (`mapErr`) здесь НЕ делается: оно сворачивает
// `*pgconn.PgError` в sentinel и теряет ИМЯ НАРУШЕННОГО ОГРАНИЧЕНИЯ, а именно оно
// и есть предмет разбора для оператора установки — «порядок держит ключ» без
// имени ключа проверить нечем. Приведение принадлежит транспорту, который у
// этого глагола появится вместе со своим потребителем.
//
// # Идемпотентность выражена ОПЕРАТОРОМ, а не сравнением в коде
//
// `ON CONFLICT … DO UPDATE … WHERE <строка отличается>` меняет строку ровно
// тогда, когда объявленное состояние в ней не стоит, и `RETURNING` пуст, когда не
// меняет. «Прочитать и сравнить» дало бы то же число и окно между чтением и
// записью (запрет #10): под конкуренцией два применения увидели бы одну и ту же
// строку неизменной.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	"github.com/PRO-Robotech/kacho/services/iam/internal/apps/kacho/modulecatalog"
	"github.com/PRO-Robotech/kacho/services/iam/internal/catalog"
)

// CatalogLockKey — ключ ГЛОБАЛЬНОГО консультативного замка каталога.
//
// Один на весь каталог, а не на модуль: переселение проекций трогает роли, а роль
// одного модуля вправе называть ресурс другого. Замок по модулю сериализовал бы
// то, что и так безопасно, и пропустил бы ровно тот случай, ради которого берётся.
//
// Экспортирован ради пробы замка: она обязана состязаться за ТОТ ЖЕ ключ, а
// выписанный у неё второй литерал разошёлся бы с этим молча — и разошёлся бы
// незаметно, потому что проба на чужом ключе просто не дождалась бы блокировки и
// зеленела бы, ничего не проверив.
const CatalogLockKey = "kacho_iam.module_catalog"

// CatalogWriteRepo — исполнитель транзакций применителя каталога над пулом.
type CatalogWriteRepo struct {
	tx *coredb.Transactor
}

// NewCatalogWriteRepo собирает исполнителя транзакций поверх пула.
func NewCatalogWriteRepo(pool *pgxpool.Pool) *CatalogWriteRepo {
	return &CatalogWriteRepo{tx: coredb.NewTransactor(pool)}
}

// RunInWriteTx исполняет fn под ОДНОЙ писательской транзакцией: все шаги
// применения ложатся вместе либо не ложатся вовсе.
func (r *CatalogWriteRepo) RunInWriteTx(
	ctx context.Context,
	fn func(context.Context, modulecatalog.CatalogWriter) error,
) error {
	return r.tx.InTx(ctx, func(tx pgx.Tx) error { return fn(ctx, catalogWriter{tx: tx}) })
}

// catalogWriter — `modulecatalog.CatalogWriter` над одной транзакцией.
type catalogWriter struct{ tx pgx.Tx }

// LockCatalog берёт транзакционный консультативный замок: он снимается коммитом
// И откатом, поэтому оборванный применитель не оставляет каталог запертым.
func (w catalogWriter) LockCatalog(ctx context.Context) error {
	_, err := w.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, CatalogLockKey)
	return err
}

// ReadModule читает живые строки одного модуля.
//
// Три оператора под ОДНОЙ транзакцией — то есть один снимок: собранный из разных
// моментов, он показал бы «ресурс снят, его глаголы живы», а такого состояния в
// базе не бывает ни при каком порядке применения.
func (w catalogWriter) ReadModule(ctx context.Context, module string) (catalog.Rows, error) {
	var out catalog.Rows

	var present bool
	if err := w.tx.QueryRow(ctx,
		`SELECT true FROM kacho_iam.catalog_module WHERE module = $1 AND live`, module,
	).Scan(&present); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, fmt.Errorf("прочитать строку модуля %s: %w", module, err)
	}
	if present {
		out.Modules = append(out.Modules, module)
	}

	resRows, err := w.tx.Query(ctx,
		`SELECT module, resource FROM kacho_iam.catalog_resource WHERE module = $1 AND live
		  ORDER BY resource`, module)
	if err != nil {
		return out, fmt.Errorf("прочитать ресурсы модуля %s: %w", module, err)
	}
	out.Resources, err = pgx.CollectRows(resRows, func(row pgx.CollectableRow) (catalog.ResourceRow, error) {
		var r catalog.ResourceRow
		return r, row.Scan(&r.Module, &r.Resource)
	})
	if err != nil {
		return out, fmt.Errorf("прочитать ресурсы модуля %s: %w", module, err)
	}

	verbRows, err := w.tx.Query(ctx,
		`SELECT module, resource, verb, per_object FROM kacho_iam.catalog_verb
		  WHERE module = $1 AND live ORDER BY resource, verb`, module)
	if err != nil {
		return out, fmt.Errorf("прочитать действия модуля %s: %w", module, err)
	}
	out.Verbs, err = pgx.CollectRows(verbRows, func(row pgx.CollectableRow) (catalog.VerbRow, error) {
		var v catalog.VerbRow
		return v, row.Scan(&v.Module, &v.Resource, &v.Verb, &v.PerObject)
	})
	if err != nil {
		return out, fmt.Errorf("прочитать действия модуля %s: %w", module, err)
	}
	return out, nil
}

// UpsertModule заводит либо ОЖИВЛЯЕТ строку модуля.
//
// Оживление, а не вставка новой строки: снятая строка занимает первичный ключ, и
// на этом стоит обратимость установки — повторная установка возвращает ТУ ЖЕ
// строку, а не заводит вторую с той же парой.
func (w catalogWriter) UpsertModule(ctx context.Context, module string) (bool, error) {
	return w.changed(ctx, `
		INSERT INTO kacho_iam.catalog_module (module) VALUES ($1)
		ON CONFLICT (module) DO UPDATE
		   SET retired_at = NULL, live = true, retired_reason = NULL
		 WHERE catalog_module.live IS DISTINCT FROM true
		RETURNING 1`, module)
}

// UpsertResource заводит либо оживляет строку ресурса.
//
// `superseded_by` снимается вместе с оживлением: преемник объявлен ровно у снятой
// строки (`catalog_resource_successor_only_when_retired`), и оставить его на живой
// нельзя ни при каком порядке.
func (w catalogWriter) UpsertResource(ctx context.Context, r catalog.ResourceRow) (bool, error) {
	return w.changed(ctx, `
		INSERT INTO kacho_iam.catalog_resource (module, resource, dotted)
		VALUES ($1, $2, $1 || '.' || $2)
		ON CONFLICT (module, resource) DO UPDATE
		   SET retired_at = NULL, live = true, retired_reason = NULL, superseded_by = NULL
		 WHERE catalog_resource.live IS DISTINCT FROM true
		RETURNING 1`, r.Module, r.Resource)
}

// UpsertVerb заводит, оживляет либо приводит признак словаря.
//
// Признак входит в условие изменения намеренно: строка, лежащая с неверным
// признаком, существует и по тройке сверку прошла бы молча — а разошлись бы ровно
// те две величины, ради которых словари разделены (что ключ пропускает и что
// материализуется).
func (w catalogWriter) UpsertVerb(ctx context.Context, v catalog.VerbRow) (bool, error) {
	return w.changed(ctx, `
		INSERT INTO kacho_iam.catalog_verb (module, resource, verb, per_object)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (module, resource, verb) DO UPDATE
		   SET retired_at = NULL, live = true, retired_reason = NULL,
		       per_object = EXCLUDED.per_object
		 WHERE catalog_verb.live IS DISTINCT FROM true
		    OR catalog_verb.per_object IS DISTINCT FROM EXCLUDED.per_object
		RETURNING 1`, v.Module, v.Resource, v.Verb, v.PerObject)
}

// RetireVerb помечает строку действия снятой. Повторное снятие — ноль строк.
func (w catalogWriter) RetireVerb(ctx context.Context, v catalog.VerbRow, reason string) (bool, error) {
	return w.changed(ctx, `
		UPDATE kacho_iam.catalog_verb
		   SET retired_at = now(), live = false, retired_reason = $4
		 WHERE module = $1 AND resource = $2 AND verb = $3 AND live
		RETURNING 1`, v.Module, v.Resource, v.Verb, reason)
}

// RetireResource помечает строку ресурса снятой. Повторное снятие — ноль строк.
func (w catalogWriter) RetireResource(ctx context.Context, r catalog.ResourceRow, reason string) (bool, error) {
	return w.changed(ctx, `
		UPDATE kacho_iam.catalog_resource
		   SET retired_at = now(), live = false, retired_reason = $3
		 WHERE module = $1 AND resource = $2 AND live
		RETURNING 1`, r.Module, r.Resource, reason)
}

// ResettleTenantProjections переселяет проекции АРЕНДАТОРСКИХ ролей, теряющие
// референт вместе со снимаемыми строками.
//
// # Почему две популяции переселяются по РАЗНЫМ входам
//
// `role_rule_ref` ссылается и на ресурс, и на действие (`role_rule_ref_res_fk`,
// `role_rule_ref_verb_fk`) — значит её строку роняет и снятие ресурса, и снятие
// одного действия. `role_verb` ссылается ТОЛЬКО на ресурс
// (`role_verb_type_fk` → `catalog_resource(dotted, live)`), поэтому снятие
// действия её ключом не задевает, и переселять её на этом входе значило бы
// отбирать у арендатора право, которого база не отбирает. Снятое действие при
// живом ресурсе — предмет `deprecatedVerbs`, а не этого писателя.
//
// # Почему только `is_system = false`
//
// Роль системного яруса объявлена манифестом. Если манифест снимает ресурс,
// который его же роль называет, манифест противоречит сам себе — и это обязано
// быть отвергнуто ключом, а не улажено молчаливым отбором права у роли, которую
// применитель не объявлял.
func (w catalogWriter) ResettleTenantProjections(
	ctx context.Context,
	resources []catalog.ResourceRow,
	verbs []catalog.VerbRow,
	reason string,
) (modulecatalog.Resettled, error) {
	var out modulecatalog.Resettled

	resModules := make([]string, 0, len(resources))
	resNames := make([]string, 0, len(resources))
	for _, r := range resources {
		resModules = append(resModules, r.Module)
		resNames = append(resNames, r.Resource)
	}
	verbModules := make([]string, 0, len(verbs))
	verbResources := make([]string, 0, len(verbs))
	verbNames := make([]string, 0, len(verbs))
	for _, v := range verbs {
		verbModules = append(verbModules, v.Module)
		verbResources = append(verbResources, v.Resource)
		verbNames = append(verbNames, v.Verb)
	}

	// Один оператор на популяцию: перенос и снятие обязаны быть неделимы, иначе
	// между ними помещается состояние «право отобрано и нигде не записано».
	if err := w.tx.QueryRow(ctx, `
		WITH stale_res AS (
		  SELECT * FROM unnest($1::text[], $2::text[]) AS t(module, resource)
		), stale_verb AS (
		  SELECT * FROM unnest($3::text[], $4::text[], $5::text[]) AS t(module, resource, verb)
		), doomed AS (
		  SELECT rr.role_id, rr.module, rr.resource, rr.verb
		    FROM kacho_iam.role_rule_ref rr
		    JOIN kacho_iam.roles r ON r.id = rr.role_id
		   WHERE r.is_system = false
		     AND (EXISTS (SELECT 1 FROM stale_res s
		                   WHERE s.module = rr.module AND s.resource = rr.resource)
		       OR EXISTS (SELECT 1 FROM stale_verb s
		                   WHERE s.module = rr.module AND s.resource = rr.resource
		                     AND s.verb = rr.verb))
		), moved AS (
		  INSERT INTO kacho_iam.role_grant_orphan (role_id, object_type, verb, source, reason)
		  SELECT d.role_id, d.module || '.' || d.resource, COALESCE(d.verb, ''), 'rule_ref', $6
		    FROM doomed d
		  ON CONFLICT (role_id, object_type, verb, source) DO NOTHING
		), dropped AS (
		  DELETE FROM kacho_iam.role_rule_ref rr
		   USING doomed d
		   WHERE rr.role_id = d.role_id AND rr.module = d.module AND rr.resource = d.resource
		     AND rr.verb IS NOT DISTINCT FROM d.verb
		  RETURNING 1
		)
		SELECT count(*) FROM dropped`,
		resModules, resNames, verbModules, verbResources, verbNames, reason,
	).Scan(&out.RuleRefs); err != nil {
		return out, fmt.Errorf("переселить объявления правил: %w", err)
	}

	if err := w.tx.QueryRow(ctx, `
		WITH stale_res AS (
		  SELECT module || '.' || resource AS dotted
		    FROM unnest($1::text[], $2::text[]) AS t(module, resource)
		), doomed AS (
		  SELECT rv.role_id, rv.object_type, rv.verb
		    FROM kacho_iam.role_verb rv
		    JOIN kacho_iam.roles r ON r.id = rv.role_id
		   WHERE r.is_system = false
		     AND rv.object_type IN (SELECT dotted FROM stale_res)
		), moved AS (
		  INSERT INTO kacho_iam.role_grant_orphan (role_id, object_type, verb, source, reason)
		  SELECT d.role_id, d.object_type, d.verb, 'role_verb', $3 FROM doomed d
		  ON CONFLICT (role_id, object_type, verb, source) DO NOTHING
		), dropped AS (
		  DELETE FROM kacho_iam.role_verb rv
		   USING doomed d
		   WHERE rv.role_id = d.role_id AND rv.object_type = d.object_type AND rv.verb = d.verb
		  RETURNING 1
		)
		SELECT count(*) FROM dropped`,
		resModules, resNames, reason,
	).Scan(&out.RoleVerbs); err != nil {
		return out, fmt.Errorf("переселить выдачи глаголов: %w", err)
	}
	return out, nil
}

// changed исполняет оператор, меняющий не более одной строки, и отвечает,
// изменилась ли она. Пустой `RETURNING` — это «объявленное уже стоит», а не отказ.
func (w catalogWriter) changed(ctx context.Context, sql string, args ...any) (bool, error) {
	var one int
	err := w.tx.QueryRow(ctx, sql, args...).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Проверка соответствия портам — на этапе сборки, а не в рантайме.
var (
	_ modulecatalog.TxRunner      = (*CatalogWriteRepo)(nil)
	_ modulecatalog.CatalogWriter = catalogWriter{}
)
