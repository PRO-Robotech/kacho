// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/singlepass"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// orphanSweepDefaultGrace — отсрочка по умолчанию: объект, чьё последнее событие
// моложе этого срока, предикат не называет.
//
// Отсрочка нужна не «на всякий случай». Постановка на учёт и появление признака
// существования пишутся ОДНОЙ транзакцией, но подметальщик — сторонний читатель со
// своим снимком, и вне транзакции автора он вправе увидеть промежуточное состояние
// соседа. Пятнадцати минут хватает с запасом любому пути записи и на порядки меньше
// того, сколько мусор уже пролежал.
const orphanSweepDefaultGrace = 15 * time.Minute

// orphanSweepBatch — сколько объектов называется за один проход. Не «производительность»:
// исторический мусор мерится сотнями, и вываливать его в очередь одним куском значит
// заставить дренаж разгребать многосотенный бэклог вместо своей обычной работы.
// Подметальщик повторяется, поэтому остаток разберётся следующими проходами.
const orphanSweepBatch = 100

// orphanSweepLock — имя замка прохода подметальщика. Домен в имени обязателен:
// пространство ключей общее на всю базу.
const orphanSweepLock = "kacho.registry.orphan-sweep"

// TryClaimOrphanSweep берёт проход подметальщика на одну реплику.
//
// # Что это чинит
//
// Предикат детерминирован (`ORDER BY … LIMIT` по журналу намерений), поэтому без
// развода N реплик называют ТЕ ЖЕ объекты и эмитируют по каждому своё намерение о
// снятии. Применение намерения идемпотентно, поэтому права не портятся, — но
// очередь получает N строк вместо одной, дренаж разгребает N-кратный бэклог вместо
// своей обычной работы, и предел партии, заведённый ровно затем, чтобы этого не
// было, молча умножается на число реплик.
//
// Замок прохода, а не клейм строки: подметальщик ходит раз в час, ограничен своей
// партией и не обязан ускоряться от числа реплик — «исполняет один» есть ровно то,
// что уже написано у предела партии.
func (r *RegistryRepo) TryClaimOrphanSweep(ctx context.Context) (singlepass.Release, bool, error) {
	if err := r.ready(); err != nil {
		return nil, false, err
	}
	return singlepass.TryAcquire(ctx, r.pool, orphanSweepLock)
}

// SweepOrphanedRepositories находит объекты репозиториев, ЧЕЙ ВЛАДЕЛЕЦ О НИХ НЕ ЗНАЕТ,
// и эмитирует на каждый намерение о снятии. Возвращает список названных объектов в том
// виде, в каком они адресуются в хранилище прав (`<реестр>/<имя>`).
//
// # Предикат
//
// Объект называется, когда выполняются ВСЕ условия:
//
//  1. последнее событие о нём в журнале намерений — постановка на учёт (то есть
//     объект был заявлен и с тех пор не снимался);
//  2. ни один из ДВУХ источников существования владельца о нём не знает — нет ни
//     строки признака (заявлен первым push'ем), ни строки наложения (заявлен через
//     control-plane). Это тот же предикат, которым data-plane выбирает глагол записи;
//  3. последнее событие старше отсрочки.
//
// Читается ТОЛЬКО база владельца. Иначе нельзя: зеркало в iam не может ни подтвердить,
// ни опровергнуть существование ресурса — оно и есть то, что сверяется, — а спросить
// владельца со стороны iam запрещено топологией (iam лист; ребро назад замкнуло бы граф).
//
// # Почему снятие эмитируется в очередь, а не применяется напрямую
//
// Намерение едет обычным путём: at-least-once, порядок по голове партиции, идемпотентное
// применение, отравление при терминальном отказе. Прямая правка хранилища прав в обход
// этого пути не имела бы ни одного из четырёх свойств и разошлась бы с зеркалом.
//
// # Сходимость
//
// После эмиссии последним событием объекта становится снятие, поэтому условие (1)
// перестаёт выполняться и следующий проход его не назовёт. Подметальщик, который называл
// бы одно и то же каждый цикл, был бы источником нагрузки, а не порядка.
//
// grace ≤ 0 → берётся orphanSweepDefaultGrace; ноль передаётся только пробами, которым
// нужен детерминизм без ожидания.
func (r *RegistryRepo) SweepOrphanedRepositories(ctx context.Context, grace time.Duration) ([]string, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if grace < 0 {
		grace = orphanSweepDefaultGrace
	}

	named, err := r.namedOrphanRepositories(ctx, grace)
	if err != nil {
		return nil, err
	}
	for _, objectID := range named {
		registryID, repo, ok := splitRepoObjectID(objectID)
		if !ok {
			// Объект, который не разбирается на реестр и имя, снимать нечем: намерение
			// строится ровно из этих двух частей. Пропускаем, не выдумывая координату.
			continue
		}
		if err := r.UnregisterRepository(ctx, domain.UnregisterIntentForRepo(registryID, repo)); err != nil {
			// Часть уже названного могла быть снята — это безопасно (повтор снятия
			// идемпотентен), поэтому возвращаем и ошибку, и то, что успели назвать.
			return named, fmt.Errorf("withdraw orphaned repository %s: %w", objectID, err)
		}
	}
	return named, nil
}

// namedOrphanRepositories — сам предикат, отдельным запросом.
//
// `resource_kind` сужен до 'Repository' НАМЕРЕННО: под тем же `resource_id` едут
// намерения о публичном доступе ('RepositoryPublicGrant'), и они говорят о другом
// отношении на том же объекте. Смешай их — и последним событием объекта оказалось бы
// снятие публичности, а сам объект остался бы неназванным.
//
// Сравнение идёт СКЛЕЙКОЙ (`registry_id || '/' || repo`), а не разбором `resource_id`:
// имя репозитория само содержит косые черты, поэтому разбор был бы неоднозначен, а
// склейка воспроизводит ровно тот идентификатор, которым объект адресуется.
func (r *RegistryRepo) namedOrphanRepositories(ctx context.Context, grace time.Duration) ([]string, error) {
	q := fmt.Sprintf(`
		WITH latest AS (
		    SELECT resource_id,
		           (array_agg(event_type ORDER BY id DESC))[1] AS last_event,
		           max(created_at)                             AS last_at
		      FROM %s.registry_outbox
		     WHERE resource_kind = $1
		     GROUP BY resource_id
		)
		SELECT l.resource_id
		  FROM latest l
		 WHERE l.last_event = $2
		   AND l.last_at < now() - $3::interval
		   AND NOT EXISTS (
		         SELECT 1 FROM %s.registry_repository_registration g
		          WHERE g.registry_id || '/' || g.repo = l.resource_id)
		   AND NOT EXISTS (
		         SELECT 1 FROM %s.repository_configs c
		          WHERE c.registry_id || '/' || c.name = l.resource_id)
		 ORDER BY l.last_at
		 LIMIT %d`, schema, schema, schema, orphanSweepBatch)

	rows, err := r.pool.Query(ctx, q,
		domain.RegisterIntentKindRepository, domain.FGAEventRegister, grace.String())
	if err != nil {
		return nil, wrapPgErr(err, "registry_outbox", "orphan-sweep")
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var objectID string
		if err := rows.Scan(&objectID); err != nil {
			return nil, wrapPgErr(err, "registry_outbox", "orphan-sweep")
		}
		out = append(out, objectID)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapPgErr(err, "registry_outbox", "orphan-sweep")
	}
	return out, nil
}

// splitRepoObjectID разбирает `<реестр>/<имя>` по ПЕРВОЙ косой черте: идентификатор
// реестра её не содержит by construction, а имя репозитория содержать может. Обратная
// склейка поэтому возвращает исходную строку побайтово.
func splitRepoObjectID(objectID string) (registryID, repo string, ok bool) {
	registryID, repo, ok = strings.Cut(objectID, "/")
	if !ok || registryID == "" || repo == "" {
		return "", "", false
	}
	return registryID, repo, true
}
