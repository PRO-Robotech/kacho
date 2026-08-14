// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build integration

package repo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Учёт числа ресурсов арендатора у kacho-compute: списание, возврат, три исхода.
//
// ПОЧЕМУ ПРОБЫ ХОДЯТ ЧЕРЕЗ РЕАЛЬНЫЕ ТАБЛИЦЫ, А НЕ ЗОВУТ ФУНКЦИЮ НАПРЯМУЮ.
// Списание держится триггером, то есть свойством СВЯЗКИ «таблица ресурса →
// функция учёта». Проба, зовущая функцию сама, закрепляет её ответ и молчит о
// том, повешена ли она хоть на что-нибудь: она осталась бы зелёной на дереве,
// где триггер не создан вовсе. Поэтому вход подаётся вставкой в ту же таблицу,
// в которую пишет продукт.

// quotaSeed заводит строку учёта — то, что в продукте делает материализация.
//
// Заводится ЯВНО, а не подразумевается: «строки нет» — отдельный исход (KQ002),
// и проба, полагающаяся на её самозарождение, не отличила бы его от исчерпания.
func quotaSeed(t *testing.T, pool *pgxpool.Pool, projectID, kind string, limit int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO project_resource_quotas
		     (carrier_type, carrier_id, kind, used, limit_value,
		      source_scope, source_scope_id, limit_revision, account_id)
		 VALUES ('project', $1, $2, 0, $3, 'DEFAULT', '', 1, $4)
		 ON CONFLICT (carrier_type, carrier_id, kind) DO UPDATE
		     SET limit_value = EXCLUDED.limit_value`,
		projectID, kind, limit, "acc-"+projectID)
	require.NoError(t, err)
}

// quotaUsed читает потребление. Отдельно от списания: «прошло» и «записано» —
// разные утверждения, и совпадать они обязаны, а не подразумеваться.
func quotaUsed(t *testing.T, pool *pgxpool.Pool, projectID, kind string) int64 {
	t.Helper()
	var used int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT used FROM project_resource_quotas
		  WHERE carrier_type = 'project' AND carrier_id = $1 AND kind = $2`,
		projectID, kind).Scan(&used))
	return used
}

// plgID / gakID — идентификаторы нужной ФОРМЫ: схема проверяет их регулярным
// выражением (крокфордов base32), поэтому фикстура обязана быть НЕ
// снисходительнее продукта. Цифры входят в алфавит, буквы `i l o u` — нет.
func plgID(n int) string { return fmt.Sprintf("plg-%017d", n) }
func gakID(n int) string { return fmt.Sprintf("gak-%017d", n) }

// TestQuota_ChargeAndRefundAreDoneByTheTrigger — списание и возврат случаются от
// САМОЙ вставки и САМОГО удаления, без участия вызывающего.
//
// Несущее свойство всей работы: если оно держится, то верно для каждого
// писателя — будущего пути, каскада, реконсайлера, административного SQL.
func TestQuota_ChargeAndRefundAreDoneByTheTrigger(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	const projectID = "prj-quota-trigger"
	quotaSeed(t, pool, projectID, "compute.placementGroup", 10)

	_, err := pool.Exec(ctx,
		`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
		 VALUES ($1, $2, 'pg-one', 'SPREAD', 'ZONAL', 'ru-central1-a')`,
		plgID(1), projectID)
	require.NoError(t, err)

	require.EqualValues(t, 1, quotaUsed(t, pool, projectID, "compute.placementGroup"),
		"вставка не списала места: триггер не повешен либо не сработал")

	_, err = pool.Exec(ctx, `DELETE FROM placement_groups WHERE id = $1`, plgID(1))
	require.NoError(t, err)

	require.EqualValues(t, 0, quotaUsed(t, pool, projectID, "compute.placementGroup"),
		"удаление не вернуло места: возврат обязан быть свойством того же триггера")
}

// TestQuota_ConcurrentCreatesRespectTheLimit — при пределе N и 2N одновременных
// вставок проходит РОВНО N.
//
// # Почему проба обязана быть конкурентной
//
// Последовательная проверка «при исчерпанном пределе следующее списание
// отвергается» зеленеет и на коде, где предел читается отдельным запросом, а
// списывается вторым. Между ними помещается чужая запись: два создателя увидят
// одно и то же свободное место и оба пройдут — потолок выродится в частоту,
// умноженную на число параллельных пар.
//
// Проба без конкуренции этого не ловит вовсе, поэтому она НЕ ПРИНИМАЕТСЯ как
// проверка этого свойства. Наследована от прецедента, снятого этой же работой:
// вопрос остаётся, механизм под ним сменился.
func TestQuota_ConcurrentCreatesRespectTheLimit(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	const (
		projectID = "prj-quota-race"
		limit     = 5
		attempts  = 10 // 2N: половина обязана не пройти
	)
	quotaSeed(t, pool, projectID, "compute.placementGroup", limit)

	var wg sync.WaitGroup
	results := make([]error, attempts)
	start := make(chan struct{})

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // все стартуют разом, иначе это не гонка

			tx, err := pool.Begin(ctx)
			if err != nil {
				results[idx] = err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()

			if _, err := tx.Exec(ctx,
				`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
				 VALUES ($1, $2, $3, 'SPREAD', 'ZONAL', 'ru-central1-a')`,
				plgID(idx+100), projectID, fmt.Sprintf("pg-%d", idx)); err != nil {
				results[idx] = classifyQuotaErr(err)
				return
			}
			results[idx] = tx.Commit(ctx)
		}(i)
	}
	close(start)
	wg.Wait()

	var passed, refused int
	for _, err := range results {
		switch {
		case err == nil:
			passed++
		case errors.Is(err, ErrQuotaExceeded):
			refused++
		default:
			// Отказ хранилища, не отображённый в понятный, — отдельный дефект:
			// арендатор увидел бы «что-то сломалось» вместо «место кончилось».
			require.Failf(t, "непонятный отказ", "списание отказало не пределом: %v", err)
		}
	}

	require.Equal(t, limit, passed,
		"прошло не ровно столько, сколько разрешено: потолок не держит под конкуренцией")
	require.Equal(t, attempts-limit, refused,
		"остальные обязаны получить именно исчерпание предела")
	require.EqualValues(t, limit, quotaUsed(t, pool, projectID, "compute.placementGroup"),
		"счётчик обязан совпадать с числом прошедших: иначе успех означает не то, что записано")
}

// TestQuota_ProjectWithoutCeilingIsRefused — проект, которому потолок не назван
// НИ НА ОДНОЙ области, получает отказ, ОТЛИЧИМЫЙ от исчерпания.
//
// # Перевёрнутая проба, а не новая
//
// Предшественница называлась `TestQuota_ProjectWithoutLimitIsNotBlocked` и
// утверждала обратное: «проект без назначенного предела не должен отвергаться».
// Её вопрос — «что происходит, когда предела нет» — законен и сохранён; сменился
// ОТВЕТ (V2-3, «не сказано» = отказ). Имя изменено вместе с ответом: прежнее
// утверждало исход, которого больше нет, и, оставленное, лгало бы в списке проб.
//
// # Почему прежний ответ был неверен
//
// Пропуск при отсутствии строки неотличим от отсутствия квот вовсе. Измерено на
// том же прецеденте: строку предела не писал ни один путь прод-кода (ноль
// производителей при контроле два в пробах), поэтому списание ВСЕГДА получало
// ноль строк, трактовало это как «предел не назначен» и пропускало. Механизм был
// провязан, покрыт пробой гонки и не отказал ни разу за всю свою жизнь.
func TestQuota_ProjectWithoutCeilingIsRefused(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	// Строки учёта нет вовсе — умышленно: это и есть предмет пробы.
	_, err := pool.Exec(ctx,
		`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
		 VALUES ($1, 'prj-no-ceiling', 'pg-nc', 'SPREAD', 'ZONAL', 'ru-central1-a')`,
		plgID(2))
	require.Error(t, err, "проект без названного потолка обязан получить отказ, а не пройти")
	require.ErrorIs(t, classifyQuotaErr(err), ErrQuotaNotProvisioned)

	// И он ОТЛИЧИМ от исчерпания — иначе администратор, читающий «место
	// кончилось», пойдёт искать, что понизить, там, где ничего не назначено.
	require.NotErrorIs(t, classifyQuotaErr(err), ErrQuotaExceeded)

	// Положительный контроль: тот же путь при названном потолке проходит.
	// Без него отрицание зеленело бы на дереве, где вставка сломана вообще.
	quotaSeed(t, pool, "prj-with-ceiling", "compute.placementGroup", 1)
	_, err = pool.Exec(ctx,
		`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
		 VALUES ($1, 'prj-with-ceiling', 'pg-wc', 'SPREAD', 'ZONAL', 'ru-central1-a')`,
		plgID(3))
	require.NoError(t, err)

	// А второй раз — уже исчерпание, и это ДРУГОЙ признак.
	_, err = pool.Exec(ctx,
		`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
		 VALUES ($1, 'prj-with-ceiling', 'pg-wc2', 'SPREAD', 'ZONAL', 'ru-central1-a')`,
		plgID(4))
	require.ErrorIs(t, classifyQuotaErr(err), ErrQuotaExceeded)
}

// TestQuota_LoweringBelowUsageIsExpressible — понижение предела ниже текущего
// потребления ПРОХОДИТ.
//
// Это и есть причина, по которой потолок живёт в предикате списывающего
// оператора, а не в `CHECK (used <= limit_value)`: ограничение схемы запрещало бы
// состояние «занято больше, чем разрешено» и тем самым запрещало бы САМО
// понижение — административное действие становилось бы заложником того, кого оно
// ограничивает. Прецедент, на котором это измерено, снят этой же работой.
func TestQuota_LoweringBelowUsageIsExpressible(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	const projectID = "prj-quota-lower"
	quotaSeed(t, pool, projectID, "compute.placementGroup", 3)

	for i := 0; i < 3; i++ {
		_, err := pool.Exec(ctx,
			`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
			 VALUES ($1, $2, $3, 'SPREAD', 'ZONAL', 'ru-central1-a')`,
			plgID(200+i), projectID, fmt.Sprintf("pg-low-%d", i))
		require.NoError(t, err)
	}

	// Понижение НИЖЕ потребления — штатное административное действие.
	_, err := pool.Exec(ctx,
		`UPDATE project_resource_quotas SET limit_value = 1
		  WHERE carrier_type = 'project' AND carrier_id = $1 AND kind = 'compute.placementGroup'`,
		projectID)
	require.NoError(t, err, "понижение предела ниже потребления обязано быть выразимо")

	// Новые нельзя…
	_, err = pool.Exec(ctx,
		`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
		 VALUES ($1, $2, 'pg-after-lower', 'SPREAD', 'ZONAL', 'ru-central1-a')`,
		plgID(210), projectID)
	require.ErrorIs(t, classifyQuotaErr(err), ErrQuotaExceeded)

	// …старые живут…
	var alive int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM placement_groups WHERE project_id = $1`, projectID).Scan(&alive))
	require.Equal(t, 3, alive, "понижение не вправе сносить уже созданное")

	// …и удаление работает, возвращая место.
	_, err = pool.Exec(ctx, `DELETE FROM placement_groups WHERE id = $1`, plgID(200))
	require.NoError(t, err)
	require.EqualValues(t, 2, quotaUsed(t, pool, projectID, "compute.placementGroup"))
}

// TestQuota_KindsAreCountedSeparately — виды не смешиваются: исчерпание одного не
// закрывает другой.
//
// Механизм параметризован видом, а не написан под один; без этой пробы «одна
// таблица на три вида» зеленела бы и на реализации, считающей всё в одну строку.
func TestQuota_KindsAreCountedSeparately(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	const projectID = "prj-quota-kinds"
	quotaSeed(t, pool, projectID, "compute.placementGroup", 1)
	quotaSeed(t, pool, projectID, "compute.guestAccessKey", 2)

	_, err := pool.Exec(ctx,
		`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
		 VALUES ($1, $2, 'pg-k', 'SPREAD', 'ZONAL', 'ru-central1-a')`,
		plgID(300), projectID)
	require.NoError(t, err)

	// Группа размещения исчерпана…
	_, err = pool.Exec(ctx,
		`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
		 VALUES ($1, $2, 'pg-k2', 'SPREAD', 'ZONAL', 'ru-central1-a')`,
		plgID(301), projectID)
	require.ErrorIs(t, classifyQuotaErr(err), ErrQuotaExceeded)

	// …а ключ входа — нет: у него свой счёт.
	_, err = pool.Exec(ctx,
		`INSERT INTO guest_access_keys (id, project_id, name, public_key, fingerprint)
		 VALUES ($1, $2, 'key-k', 'ssh-ed25519 AAAA', $3)`,
		gakID(302), projectID, "fp-"+projectID)
	require.NoError(t, err, "исчерпание одного вида не вправе закрывать другой")
	require.EqualValues(t, 1, quotaUsed(t, pool, projectID, "compute.guestAccessKey"))
}
