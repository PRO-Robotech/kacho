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
		`SELECT module, resource, object_type FROM kacho_iam.catalog_resource
		  WHERE module = $1 AND live
		  ORDER BY resource`, module)
	if err != nil {
		return out, fmt.Errorf("прочитать ресурсы модуля %s: %w", module, err)
	}
	out.Resources, err = pgx.CollectRows(resRows, func(row pgx.CollectableRow) (catalog.ResourceRow, error) {
		var r catalog.ResourceRow
		return r, row.Scan(&r.Module, &r.Resource, &r.ObjectType)
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
//
// `object_type` входит в условие изменения наравне со снятостью: строка, лежащая
// с ЧУЖИМ именем типа, живёт и по паре (модуль, ресурс) сверку прошла бы молча —
// а расходилась бы при этом ровно та величина, ради которой колонка заведена
// (какое отношение `v_*` адресует ресурс). Правка манифеста, меняющая
// `objectType`, обязана доезжать до строки; иначе она была бы принята и
// проигнорирована.
func (w catalogWriter) UpsertResource(ctx context.Context, r catalog.ResourceRow) (bool, error) {
	return w.changed(ctx, `
		INSERT INTO kacho_iam.catalog_resource (module, resource, dotted, object_type)
		VALUES ($1, $2, $1 || '.' || $2, $3)
		ON CONFLICT (module, resource) DO UPDATE
		   SET retired_at = NULL, live = true, retired_reason = NULL, superseded_by = NULL,
		       object_type = EXCLUDED.object_type
		 WHERE catalog_resource.live IS DISTINCT FROM true
		    OR catalog_resource.object_type IS DISTINCT FROM EXCLUDED.object_type
		RETURNING 1`, r.Module, r.Resource, r.ObjectType)
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
//
// # Почему СНЯТИЕ производит переселяемое, а не отбирает его второй раз
//
// # Строка, оставшаяся без единого живого типа, СНИМАЕТСЯ целиком
//
// Пустой массив запрещён ограничением `role_rule_selectors_types_nonempty`
// (миграция `0026`): селектор, которому нечего выбирать, строкой быть не вправе.
// Оба исхода — «вырезать элементы» и «снять строку» — не изобретены здесь: их
// ВЫПОЛНИЛ ЧЕЛОВЕК двумя отдельными шагами миграции `0074`, ровно по этому
// признаку. Применитель делает то же самое глаголом, а не рукой.
//
// # Почему оператор отдаёт ТРИ величины, а не одну
//
// Этот писатель — НЕ автор строки: он её только снимает. Автор один
// (`roleWriter.ReplaceRoleVerbs` / `ReplaceRuleRefs`), и форму строки знает он.
// Остаток, который такое разделение оставляло, ровно один: ключ строки был
// ПОВТОРЁН — сначала в отборе (`doomed`), потом в предикате снятия
// (`USING … WHERE`). Ключ изменят, предикат разойдётся с отбором, и разойдётся
// МОЛЧА, — поэтому оператор отдавал обе величины, а писатель их сверял.
//
// Повтора больше нет: снятие само СТАЛО отбором. `DELETE … RETURNING` — и
// единственное место, где сказано, что уходит, и производитель того, что
// переселяется; вставка в сироты читает его выход. Расхождение отбора со снятием
// перестало быть представимым — не «не производится ни одним входом», а
// невыразимо by construction, — и вместе с предметом снята сверка кардинальности
// (`resettleExactly`) и её проба. Оставлять отрицание, чей вход больше не
// производится, значило бы держать вечно молчащую проверку, неотличимую от
// исправной (`testing.md` §«Гейт на класс», п. 9).
//
// # Цена повтора была измерена, а не предположена (задача продукта #1959)
//
// Повтор ключа стоил не строки кода, а ИСПОЛНИМОСТИ операции. Ключ проекции
// правила несёт `verb`, допускающий NULL, поэтому предикат снятия сравнивал его
// через `IS NOT DISTINCT FROM`, а этот оператор не хешируется и не мержится:
// планировщик сводил соединение к паре `(module, resource)` и отправлял
// `role_id`/`verb` в фильтр. На тысяче ролей это давало merge join с
// `Rows Removed by Join Filter: 99 980 000` — то есть сто миллионов пар ради
// двадцати тысяч снятий, квадратично по числу ролей.
//
// Замер на PostgreSQL 16.15 (shared_buffers 128MB, work_mem 4MB), 1000 ролей,
// 20 000 строк в популяции, `EXPLAIN (ANALYZE, BUFFERS)`:
//
//	повтор ключа, свежая статистика	17 054 мс	merge join, 99 980 000 пар
//	повтор ключа, ПОСЛЕ `ANALYZE`	   824 мс	hash join, 80 000 пар
//	снятие-производитель		   834 мс	соединения нет вовсе
//
// Средняя строка объясняет, почему дефект дожил до порога незамеченным: та же
// SQL на той же машине быстрее в двадцать раз, если статистика собрана. Разбор,
// снятый на собранной статистике, называл дорогим не то звено; применитель на
// свежей установке встречает первый план, а не второй. Соединения в новой форме
// нет вовсе — значит выбирать планировщику нечего, и разброса нет.
//
// Что осталось дорогим и почему это не чинится здесь: 96 % нового оператора —
// вставка в сироты (420 мс) и пооперационный триггер ключа на `roles` (385 мс).
// Ключ снимает `FOR KEY SHARE` со строки роли, и это единственная гонко-безопасная
// конструкция: заменив его проверкой по множеству, мы отдали бы блокировку и
// пустили гонку «сирота записана — роль удалена» (запрет #10). Замер потолка:
// та же вставка без ключа — 414 мс против 818 мс, то есть весь возможный выигрыш
// вдвое, ценой инварианта. Не берём.
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
	// Порядок внутри оператора задан ПОТОКОМ ДАННЫХ, а не порядком записи веток:
	// `moved` читает выход `dropped`, поэтому вставка не может опередить снятие.
	if err := w.tx.QueryRow(ctx, `
		WITH stale_res AS (
		  SELECT * FROM unnest($1::text[], $2::text[]) AS t(module, resource)
		), stale_verb AS (
		  SELECT * FROM unnest($3::text[], $4::text[], $5::text[]) AS t(module, resource, verb)
		), dropped AS (
		  DELETE FROM kacho_iam.role_rule_ref rr
		   WHERE EXISTS (SELECT 1 FROM kacho_iam.roles r
		                  WHERE r.id = rr.role_id AND r.is_system = false)
		     AND (EXISTS (SELECT 1 FROM stale_res s
		                   WHERE s.module = rr.module AND s.resource = rr.resource)
		       OR EXISTS (SELECT 1 FROM stale_verb s
		                   WHERE s.module = rr.module AND s.resource = rr.resource
		                     AND s.verb = rr.verb))
		  RETURNING rr.role_id, rr.module, rr.resource, rr.verb
		), moved AS (
		  INSERT INTO kacho_iam.role_grant_orphan (role_id, object_type, verb, source, reason)
		  SELECT d.role_id, d.module || '.' || d.resource, COALESCE(d.verb, ''), 'rule_ref', $6
		    FROM dropped d
		  ON CONFLICT (role_id, object_type, verb, source) DO NOTHING
		)
		SELECT (SELECT count(*) FROM dropped)`,
		resModules, resNames, verbModules, verbResources, verbNames, reason,
	).Scan(&out.RuleRefs); err != nil {
		return out, fmt.Errorf("переселить объявления правил: %w", err)
	}

	if err := w.tx.QueryRow(ctx, `
		WITH stale_res AS (
		  SELECT module || '.' || resource AS dotted
		    FROM unnest($1::text[], $2::text[]) AS t(module, resource)
		), dropped AS (
		  DELETE FROM kacho_iam.role_verb rv
		   WHERE EXISTS (SELECT 1 FROM kacho_iam.roles r
		                  WHERE r.id = rv.role_id AND r.is_system = false)
		     AND rv.object_type IN (SELECT dotted FROM stale_res)
		  RETURNING rv.role_id, rv.object_type, rv.verb
		), moved AS (
		  INSERT INTO kacho_iam.role_grant_orphan (role_id, object_type, verb, source, reason)
		  SELECT d.role_id, d.object_type, d.verb, 'role_verb', $3 FROM dropped d
		  ON CONFLICT (role_id, object_type, verb, source) DO NOTHING
		)
		SELECT (SELECT count(*) FROM dropped)`,
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

// PruneRetiredSelectorTypes приводит ТРЕТЬЮ проекцию правила к каталожному факту:
// вырезает из `role_rule_selectors.object_types` арендаторских ролей элементы, не
// называющие ЖИВОЙ строки каталога (задача продукта #1942).
//
// # Почему фильтр «оставить живое», а не «вырезать снятое»
//
// Вход триггера `role_rule_selectors_types_live` судит КАЖДЫЙ элемент массива, а
// не изменённые. Вырежи мы лишь снятое ЭТИМ применением — строка с ранее
// повисшим элементом была бы отвергнута триггером, и применитель отказал бы по
// причине, которой в манифесте нет и которую оператору нечем починить. Фильтр
// «оставить живое» делает правку приемлемой для триггера by construction.
//
// # Почему трогаются только пересекающиеся строки
//
// Предмет вырезания есть СНЯТИЕ, а не таблица целиком. Отбери мы строки по одному
// признаку «есть неживой элемент» — всякий подъём службы правил бы селекторы всех
// арендаторов, и цена применения перестала бы зависеть от размера снятого.
//
// # Почему только `is_system = false`
//
// Тот же довод, что у переселения: роль системного яруса объявлена манифестом, и
// манифест, снимающий ресурс, который его же роль называет, противоречит сам
// себе — это обязано быть отвергнуто ключом, а не улажено молчаливой правкой
// роли, которую применитель не объявлял.
//
// # Почему оператор отдаёт ДВЕ величины
//
// «Тронута одна строка» не говорит, вырезан из неё один элемент или пять, а
// «вырезано пять» не говорит, у одной роли или у пяти; а строка, СНЯТАЯ целиком,
// есть событие иного рода, чем строка укороченная, и сумма их не различает. Все
// три приходят из ОДНОГО оператора, то есть из одного снимка.
func (w catalogWriter) PruneRetiredSelectorTypes(
	ctx context.Context,
	resources []catalog.ResourceRow,
) (modulecatalog.Pruned, error) {
	var out modulecatalog.Pruned
	if len(resources) == 0 {
		return out, nil
	}
	dotted := make([]string, 0, len(resources))
	for _, r := range resources {
		dotted = append(dotted, r.Module+"."+r.Resource)
	}

	if err := w.tx.QueryRow(ctx, `
		WITH touched AS (
		  SELECT s.role_id, s.rule_fp, s.object_types AS was,
		         (SELECT coalesce(array_agg(t ORDER BY t), ARRAY[]::text[])
		            FROM unnest(s.object_types) AS t
		           WHERE EXISTS (SELECT 1 FROM kacho_iam.catalog_resource cr
		                          WHERE cr.dotted = t AND cr.live)) AS alive
		    FROM kacho_iam.role_rule_selectors s
		    JOIN kacho_iam.roles r ON r.id = s.role_id
		   WHERE r.is_system = false
		     AND s.object_types && $1::text[]
		), changed AS (
		  SELECT * FROM touched WHERE alive IS DISTINCT FROM was
		), emptied AS (
		  DELETE FROM kacho_iam.role_rule_selectors s
		   USING changed c
		   WHERE s.role_id = c.role_id AND s.rule_fp = c.rule_fp
		     AND cardinality(c.alive) = 0
		  RETURNING 1
		), stripped AS (
		  UPDATE kacho_iam.role_rule_selectors s
		     SET object_types = c.alive
		    FROM changed c
		   WHERE s.role_id = c.role_id AND s.rule_fp = c.rule_fp
		     AND cardinality(c.alive) > 0
		  RETURNING 1
		)
		SELECT (SELECT count(*) FROM stripped),
		       (SELECT count(*) FROM emptied),
		       (SELECT coalesce(sum(cardinality(was) - cardinality(alive)), 0) FROM changed)`,
		dotted,
	).Scan(&out.Rows, &out.Dropped, &out.Elements); err != nil {
		return out, fmt.Errorf("вырезать снятые типы из селекторов: %w", err)
	}
	return out, nil
}
