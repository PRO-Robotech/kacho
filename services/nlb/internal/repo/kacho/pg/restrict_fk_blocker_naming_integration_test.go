// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// ПРЕДМЕТ ЭТОГО ФАЙЛА.
//
// Ссылочный инвариант «родителя нельзя удалить, пока на него ссылается ребёнок»
// живёт в схеме — FK ... ON DELETE RESTRICT. Решение об отказе принимает БД, и
// только она: предпроверка в use-case'е читает СВОЙ снапшот в ОТДЕЛЬНОЙ
// читающей транзакции, а строку удаляет worker позже, поэтому ссылка,
// закоммиченная в это окно, предпроверкой не видна by construction.
//
// Отсюда требование к ТЕКСТУ: раз отказ приходит от БД, то и контрактный текст,
// НАЗЫВАЮЩИЙ блокирующие строки, обязан приходить оттуда же. Иначе у одного
// факта два клиентских текста, и тот из них, который держится под гонкой,
// блокирующих не называет — вызывающему нечего чинить, а утверждение приёмки,
// написанное на текст предпроверки, на этом пути не выполняется.
//
// Пробы ниже утверждают ИСХОД пути БД (перечень блокирующих в сообщении), а не
// факт вызова предпроверки: предпроверку они не зовут вовсе.

// --- контрактные формы (часть API, меняются осознанно) ----------------------

var (
	// tgBlockedByListenersRe — отказ удаления группы целей, на которую ссылаются
	// слушатели. Форма ОДНА на оба производителя (предпроверка use-case'а и
	// отказ БД) и несёт перечень идентификаторов.
	tgBlockedByListenersRe = regexp.MustCompile(
		`^target group is referenced by listeners: \[[^\]]+\]$`)

	// tgBlockedByTargetsRe — отказ удаления группы целей с живыми целями.
	tgBlockedByTargetsRe = regexp.MustCompile(
		`TargetGroup has \d+ target\(s\); remove them first via RemoveTargets`)

	// tgMoveBlockedRe — отказ ПЕРЕНОСА группы целей. Форма отличается от отказа
	// удаления намеренно (предмет другой: перенацелить, а не удалить) и
	// закреплена приёмкой; поэтому путь БД обязан нести именно её, а не тон
	// удаления и не безымянное «есть зависимые».
	tgMoveBlockedRe = regexp.MustCompile(
		`^target group is referenced by \d+ listener\(s\); repoint them before moving$`)
)

// seedTGWithLiveTarget — группа целей с одной ЖИВОЙ (не дренирующейся) целью.
func seedTGWithLiveTarget(t testing.TB, repo kacho.Repository, projectID string) *domain.TargetGroup {
	t.Helper()
	ctx := context.Background()
	tg := newTG(projectID, "")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
		n, err := w.TargetGroups().AddTargets(ctx, string(tg.ID), []domain.Target{{
			InstanceID: option.MustNewOption(domain.InstanceID("epd0LIVETGT1")),
			Weight:     100,
		}})
		require.NoError(t, err)
		require.Equal(t, 1, n)
	})
	return tg
}

// TestRestrictFK_TGDeleteBlockedByListener_NamesBlockingListeners — отказ БД
// обязан НАЗВАТЬ слушателей, которые держат группу целей.
//
// Путь БД берётся напрямую (writer-репозиторий, без use-case'а): именно он
// остаётся единственным, когда предпроверка промахнулась по гонке.
func TestRestrictFK_TGDeleteBlockedByListener_NamesBlockingListeners(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	_, tg, lst := seedLBTGWiredListener(t, repo, "prj01FKNAME000000001")

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	derr := w.TargetGroups().Delete(ctx, string(tg.ID))
	require.Error(t, derr, "удаление обязано быть отвергнуто FK RESTRICT")
	require.True(t, errors.Is(derr, kacho.ErrFailedPrecondition), "получено %v", derr)
	assert.NotContains(t, derr.Error(), "SQLSTATE", "сырой текст драйвера наружу не течёт")

	msg := contractText(derr)
	assert.Regexp(t, tgBlockedByListenersRe, msg,
		"отказ БД обязан нести ту же контрактную форму, что и предпроверка")
	assert.Contains(t, msg, string(lst.ID),
		"текст обязан НАЗВАТЬ блокирующего слушателя — иначе вызывающему нечего чинить")
}

// TestRestrictFK_TGDeleteBlockedByTargets_UsesTargetContractTone — отказ БД по
// ссылке цели на группу обязан нести ту же контрактную форму, что предпроверка
// («TargetGroup has N target(s); …»), а не безымянное «есть зависимые».
func TestRestrictFK_TGDeleteBlockedByTargets_UsesTargetContractTone(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	tg := seedTGWithLiveTarget(t, repo, "prj01FKTGT0000000001")

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	derr := w.TargetGroups().Delete(ctx, string(tg.ID))
	require.Error(t, derr, "удаление обязано быть отвергнуто FK RESTRICT цели")
	require.True(t, errors.Is(derr, kacho.ErrFailedPrecondition), "получено %v", derr)
	assert.NotContains(t, derr.Error(), "SQLSTATE")
	assert.Regexp(t, tgBlockedByTargetsRe, contractText(derr),
		"у отказа БД и предпроверки обязан быть ОДИН контрактный текст")
}

// TestRestrictFK_TGMoveBlockedByListener_UsesMoveContractTone — отказ ПЕРЕНОСА
// группы целей, на которую ссылается слушатель, приходит со стороны БД (guard
// `NOT EXISTS` и композитный FK при переписывании ключа) и обязан нести ту же
// контрактную форму, что предпроверка use-case'а, — со СЧЁТОМ слушателей.
func TestRestrictFK_TGMoveBlockedByListener_UsesMoveContractTone(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	_, tg, _ := seedLBTGWiredListener(t, repo, "prj01MOVEFK0000000001")

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	_, merr := w.TargetGroups().MoveProject(ctx, string(tg.ID), "prj01MOVEFKDST000001")
	require.Error(t, merr, "перенос обязан быть отвергнут — на группу ссылается слушатель")
	require.True(t, errors.Is(merr, kacho.ErrFailedPrecondition), "получено %v", merr)
	assert.NotContains(t, merr.Error(), "SQLSTATE")
	assert.Regexp(t, tgMoveBlockedRe, contractText(merr),
		"у отказа БД и предпроверки переноса обязан быть ОДИН контрактный текст")
}

// TestRestrictFK_TGDeleteVsListenerWire_Race — гонка «удаление группы» против
// «создание ссылки на неё». Выигрывает РОВНО ОДИН исход, и это решает БД:
//   - выиграло удаление ⇒ вставка слушателя отвергнута (нет референта);
//   - выиграла вставка  ⇒ удаление отвергнуто, и текст называет слушателя.
//
// Проба ставится с ОБЕИХ сторон: без положительного контроля («в каком-то
// прогоне удаление всё же проходит») отрицание зеленело бы и на дереве, где
// удаление сломано и не проходит никогда.
func TestRestrictFK_TGDeleteVsListenerWire_Race(t *testing.T) {
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	repo := kachopg.New(pool, nil)
	ctx := context.Background()

	const rounds = 12
	deleteWon, wireWon := 0, 0

	// Идентичности этой пробы собираются в рантайме, поэтому перечень фикстуры,
	// снятый с дерева литералами, их не видит — он забрал бы форматную строку.
	// Проба называет их сама: посев идёт тем же оператором продукта.
	raceProjects := make([]string, 0, rounds)
	for i := 0; i < rounds; i++ {
		raceProjects = append(raceProjects, fmt.Sprintf("prj01RACE%011d", i))
	}
	seedQuotasForProjects(t, dsn, raceProjects)

	for i := 0; i < rounds; i++ {
		project := raceProjects[i]
		lb := newLB(project, "")
		tg := newTG(project, "")
		commitWriter(t, repo, func(w kacho.RepositoryWriter) {
			_, e := w.LoadBalancers().Insert(ctx, lb)
			require.NoError(t, e)
			_, e = w.TargetGroups().Insert(ctx, tg)
			require.NoError(t, e)
		})

		lst := newListener(lb.ID, project, "", 443)
		lst.DefaultTargetGroupID = option.MustNewOption(tg.ID)

		var (
			wg              sync.WaitGroup
			delErr, wireErr error
			start           = make(chan struct{})
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			w, e := repo.Writer(ctx)
			if e != nil {
				delErr = e
				return
			}
			defer w.Abort()
			if e := w.TargetGroups().Delete(ctx, string(tg.ID)); e != nil {
				delErr = e
				return
			}
			delErr = w.Commit()
		}()
		go func() {
			defer wg.Done()
			<-start
			w, e := repo.Writer(ctx)
			if e != nil {
				wireErr = e
				return
			}
			defer w.Abort()
			if _, e := w.Listeners().Insert(ctx, lst); e != nil {
				wireErr = e
				return
			}
			wireErr = w.Commit()
		}()
		close(start)
		wg.Wait()

		switch {
		case delErr == nil && wireErr == nil:
			t.Fatalf("раунд %d: оба исхода прошли — ссылка пережила своего референта "+
				"(удаление %v, вставка %v)", i, delErr, wireErr)
		case delErr != nil && wireErr != nil:
			t.Fatalf("раунд %d: не прошёл НИ ОДИН исход — отказ не по предмету "+
				"(удаление %v, вставка %v)", i, delErr, wireErr)
		case delErr == nil:
			deleteWon++
			require.True(t, errors.Is(wireErr, kacho.ErrFailedPrecondition),
				"раунд %d: проигравшая вставка обязана получить FailedPrecondition, получено %v", i, wireErr)
		default:
			wireWon++
			require.True(t, errors.Is(delErr, kacho.ErrFailedPrecondition),
				"раунд %d: проигравшее удаление обязано получить FailedPrecondition, получено %v", i, delErr)
			assert.Regexp(t, tgBlockedByListenersRe, contractText(delErr),
				"раунд %d: отказ гонки обязан нести контрактную форму", i)
			assert.Contains(t, contractText(delErr), string(lst.ID),
				"раунд %d: отказ обязан назвать выигравшего слушателя", i)
		}

		// Состояние сошлось: либо группы нет и слушателя нет, либо есть оба.
		var tgAlive, lstAlive bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM kacho_nlb.target_groups WHERE id=$1),
			        EXISTS(SELECT 1 FROM kacho_nlb.listeners WHERE id=$2)`,
			string(tg.ID), string(lst.ID)).Scan(&tgAlive, &lstAlive))
		assert.False(t, lstAlive && !tgAlive,
			"раунд %d: слушатель остался без группы целей — ссылочная целостность нарушена", i)
	}

	t.Logf("осмотрено раундов: %d · удаление выиграло %d · вставка выиграла %d",
		rounds, deleteWon, wireWon)
	require.Positive(t, deleteWon+wireWon, "ни один раунд не дал исхода — проба ничего не утверждает")
}

// TestGate_EveryRestrictFKHasBlockerNamingContract — ГЕЙТ КЛАССА.
//
// Предпосылка гейта — сама схема: он перечисляет в мигрированной базе КАЖДЫЙ
// внешний ключ с ON DELETE RESTRICT и требует, чтобы у ссылки был объявленный
// контракт перечисления блокирующих строк (`kachopg.RestrictFKContracts`).
// Новый FK RESTRICT, заведённый миграцией без такого контракта, роняет гейт:
// послабления, которое надо помнить, здесь нет — предмет берётся из дерева.
//
// Гейт печатает объём осмотренного, чтобы «ноль находок» было отличимо от
// «ноль прочитанного».
func TestGate_EveryRestrictFKHasBlockerNamingContract(t *testing.T) {
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT c.conname,
		       child.relname  AS child_table,
		       parent.relname AS parent_table
		  FROM pg_constraint c
		  JOIN pg_class child  ON child.oid  = c.conrelid
		  JOIN pg_class parent ON parent.oid = c.confrelid
		  JOIN pg_namespace n   ON n.oid     = c.connamespace
		 WHERE c.contype = 'f'
		   AND c.confdeltype = 'r'
		   AND n.nspname = 'kacho_nlb'
		 ORDER BY c.conname`)
	require.NoError(t, err)
	defer rows.Close()

	type fk struct{ name, child, parent string }
	var found []fk
	for rows.Next() {
		var f fk
		require.NoError(t, rows.Scan(&f.name, &f.child, &f.parent))
		found = append(found, f)
	}
	require.NoError(t, rows.Err())

	// Предпосылка гейта: если ссылок с RESTRICT в схеме не осталось, гейту
	// нечего рассматривать, и молчание — не «всё хорошо».
	require.NotEmpty(t, found,
		"в схеме kacho_nlb не найдено ни одного FK ... ON DELETE RESTRICT — "+
			"предпосылка гейта не выполняется, его молчание ничего не значит")

	// Контроль в другую сторону, встроенный в гейт: в схеме обязан быть внешний
	// ключ ТОЙ ЖЕ формы (ребёнок → родитель), но НЕ RESTRICT — сегодня это
	// каскад состояния анонсов на балансировщик. Он текста отказа не должен и
	// в перечень выше попасть не может. Без этого контроля выборка «взяла всё
	// подряд» была бы неотличима от «различает по confdeltype».
	var nonRestrict []string
	crows, err := pool.Query(ctx, `
		SELECT c.conname
		  FROM pg_constraint c
		  JOIN pg_namespace n ON n.oid = c.connamespace
		 WHERE c.contype = 'f'
		   AND c.confdeltype <> 'r'
		   AND n.nspname = 'kacho_nlb'
		 ORDER BY c.conname`)
	require.NoError(t, err)
	for crows.Next() {
		var name string
		require.NoError(t, crows.Scan(&name))
		nonRestrict = append(nonRestrict, name)
	}
	crows.Close()
	require.NoError(t, crows.Err())
	require.NotEmpty(t, nonRestrict,
		"в схеме нет ни одного FK, НЕ являющегося RESTRICT — различающая "+
			"способность выборки не подтверждена, гейт мог бы требовать текст от всех")
	for _, f := range found {
		require.NotContains(t, nonRestrict, f.name,
			"ключ %s попал в оба множества — выборка не различает confdeltype", f.name)
	}

	var missing []string
	for _, f := range found {
		if _, ok := kachopg.RestrictFKContracts[f.name]; !ok {
			missing = append(missing, fmt.Sprintf(
				"%s (%s → %s): нет контракта перечисления блокирующих строк",
				f.name, f.child, f.parent))
		}
	}
	sort.Strings(missing)

	// Зеркальная сторона: объявленный контракт, которому в схеме больше ничего не
	// соответствует, — тоже находка (иначе он переживёт свой предмет).
	inSchema := make(map[string]struct{}, len(found))
	for _, f := range found {
		inSchema[f.name] = struct{}{}
	}
	var orphaned []string
	for name := range kachopg.RestrictFKContracts {
		if _, ok := inSchema[name]; !ok {
			orphaned = append(orphaned, name+": контракт объявлен, а такого FK в схеме нет")
		}
	}
	sort.Strings(orphaned)

	t.Logf("осмотрено FK в kacho_nlb: RESTRICT %d · не-RESTRICT %d (контроль) · объявлено контрактов %d",
		len(found), len(nonRestrict), len(kachopg.RestrictFKContracts))
	assert.Empty(t, missing, "ссылки без контракта перечисления:\n%s", strings.Join(missing, "\n"))
	assert.Empty(t, orphaned, "контракты без предмета в схеме:\n%s", strings.Join(orphaned, "\n"))
}

// contractText — часть сообщения после сентинела (`...: `), то есть ровно тот
// текст, который уезжает клиенту.
func contractText(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i >= 0 {
		return msg[i+2:]
	}
	return msg
}
