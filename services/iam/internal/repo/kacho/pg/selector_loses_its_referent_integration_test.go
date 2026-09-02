// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// selector_loses_its_referent_integration_test.go — ТРЕТЬЯ поверхность проекции
// правила теряет референт, когда строку каталога снимает ГЛАГОЛ (задача продукта
// #1942).
//
// # Предмет: у половины референта нет производителя входа
//
// Референт третьей проекции — триггер `role_rule_selectors_types_live` — держится
// НА ВХОДЕ (`BEFORE INSERT OR UPDATE ON kacho_iam.role_rule_selectors`). Он судит
// запись селектора и НЕ судит снятие строки каталога; обратной половины у него
// нет и быть не может тем же способом — ключ на элемент массива невыразим, и это
// сказано в шапке самой миграции `20260902174500`.
//
// Две другие проекции референт имеют КЛЮЧОМ (`role_rule_ref_res_fk`,
// `role_verb_type_fk`), поэтому снятие ресурса их роняет — и потому применитель
// обязан переселять их тем же оператором (`ResettleTenantProjections`). Третью
// не роняет ничто, и она остаётся называть снятый тип МОЛЧА.
//
// # Почему это стало дефектом только теперь
//
// Пока строки каталога снимала ТОЛЬКО применённая миграция, остаток закрывал
// человек: `0074` вычищала селекторы двумя отдельными шагами руками. С
// применителем каталога (#1034) снятие стало ГЛАГОЛОМ, у которого автора-человека
// рядом нет, и остаток стал дефектом.
//
// # Чего эта проба НЕ делает — и это сказано прямо
//
// Она НЕ решает, каким исходу быть. Три мыслимых исхода (вырезать элемент из
// массива · переселить строку в сироты · отвергнуть применение при живой ссылке)
// разобраны и отвергнуты в теле #1942, четвёртого никто не принимал, а решение
// принадлежит той задаче и её приёмке, не этой пробе.
//
// Она ЗАВОДИТ ПРОИЗВОДИТЕЛЯ ВХОДА, которого сегодня нет ни у одной проверки, и
// закрепляет СЕГОДНЯШНИЙ исход как известный — ровно затем, чтобы его смена была
// видна. Пока решения нет, дыра машинно наблюдаема числом; в день, когда решение
// сядет, эта проба покраснеет и назовёт себя.

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// selectorsNamingDeadTypes — перепись: сколько строк селекторов называют тип, у
// которого в каталоге нет ЖИВОЙ строки.
//
// Возвращает и число строк, и их перечень: одно число не говорит, ЧТО именно
// повисло, а перечень без числа не отличает «ничего не нашли» от «ничего не
// прочли».
func selectorsNamingDeadTypes(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (int, []string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT s.role_id, s.rule_fp, t AS dotted
		  FROM kacho_iam.role_rule_selectors s
		  CROSS JOIN LATERAL unnest(s.object_types) AS t
		 WHERE NOT EXISTS (
		         SELECT 1 FROM kacho_iam.catalog_resource cr
		          WHERE cr.dotted = t AND cr.live)
		 ORDER BY s.role_id, s.rule_fp, t`)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var role, fp, dotted string
		require.NoError(t, rows.Scan(&role, &fp, &dotted))
		out = append(out, role+"/"+fp+" → "+dotted)
	}
	require.NoError(t, rows.Err())
	return len(out), out
}

// selectorObjectTypesRead — объём осмотренного: сколько ЭЛЕМЕНТОВ массивов
// прочитано переписью выше. Без него «повисших ноль» неотличимо от «прочитано
// ноль», а таблица селекторов на свежей базе непуста только благодаря досеву.
func selectorObjectTypesRead(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM kacho_iam.role_rule_selectors s
		 CROSS JOIN LATERAL unnest(s.object_types) AS t`).Scan(&n))
	return n
}

// TestSelectorKeepsNamingAResourceTheApplierRetired — ХАРАКТЕРИЗУЮЩИЙ ЗАМОК.
//
// Снятие строки каталога ГЛАГОЛОМ проходит при живом селекторе, её называющем;
// селектор остаётся и продолжает называть снятый тип; узнаёт об этом СЛЕДУЮЩИЙ,
// кто тронет селектор, — отказом, к его правке отношения не имеющим.
func TestSelectorKeepsNamingAResourceTheApplierRetired(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: требует Postgres")
	}
	ctx, pool := catalogPool(t)
	applier := applierOver(t, pool)

	// ── ПРЕДПОСЫЛКА, а не допущение: на намигрированной базе повисших нет ──────
	read0 := selectorObjectTypesRead(t, ctx, pool)
	dangling0, list0 := selectorsNamingDeadTypes(t, ctx, pool)
	t.Logf("перепись ДО: элементов object_types прочитано %d · называют снятый тип %d %v",
		read0, dangling0, list0)
	require.Positivef(t, read0, "прочитано ноль элементов object_types — перепись беспредметна, "+
		"и её «повисших ноль» получено даром")
	require.Zerof(t, dangling0, "на намигрированной базе селекторы уже называют снятые типы %v — "+
		"это НЕ предмет этой пробы, и всё, что она измерит дальше, будет смешано с чужим", list0)

	// ── ВХОД: ресурс заведён глаголом и назван живым селектором ────────────────
	const dotted = applierProbeModule + ".orphaned"
	rep, err := applier.Apply(ctx, probeManifest(
		probeResource("orphaned", "get"),
		probeResource("kept", "get"),
	))
	require.NoError(t, err)
	require.True(t, rep.Changed(), "заведение ресурсов обязано изменить каталог: %s", rep)

	role := catalogRole(t, ctx, pool, "sel1942")
	require.NoError(t, writeSelector(ctx, pool, role, "fp-1942", []string{dotted}),
		"ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: живой тип обязан записаться, иначе снимать будет нечего")

	// ── ГЛАГОЛ: тот же модуль без снятого ресурса ─────────────────────────────
	gone, err := applier.Apply(ctx, probeManifest(probeResource("kept", "get")))
	require.NoError(t, err, "снятие строки каталога при живом селекторе, её называющем, "+
		"сегодня НЕ отвергается — если оно отверглось, исход решён и замок пора переписать")
	require.Positivef(t, gone.RetiredResources,
		"применитель ресурсов не снял (%s) — вход не произведён, и всё ниже вакуумно", gone)

	var live bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT live FROM kacho_iam.catalog_resource WHERE dotted = $1`, dotted).Scan(&live))
	require.Falsef(t, live, "строка каталога %s жива после снятия", dotted)

	// ── ИСХОД, закреплённый как известный ─────────────────────────────────────
	read1 := selectorObjectTypesRead(t, ctx, pool)
	dangling1, list1 := selectorsNamingDeadTypes(t, ctx, pool)
	t.Logf("перепись ПОСЛЕ: элементов object_types прочитано %d · называют снятый тип %d %v",
		read1, dangling1, list1)

	require.Equalf(t, 1, dangling1,
		"СЕГОДНЯШНИЙ ИСХОД, закреплённый как известный: снятие строки каталога глаголом "+
			"оставляет селектор её называть. Стало %d повисших вместо 1 — либо решение #1942 "+
			"село (тогда этот замок обязан быть переписан под него ОДНИМ изменением с ним), "+
			"либо повисло чужое: %v", dangling1, list1)

	// Цена, названная наблюдаемо: следующий, кто тронет ЭТОТ селектор, получает
	// отказ про тип, которого он не выбирал.
	err = writeSelector(ctx, pool, role, "fp-1942-next", []string{dotted})
	require.Error(t, err, "повторная запись того же типа обязана быть отвергнута триггером — "+
		"иначе референт не держится и на входе тоже")
	require.Containsf(t, err.Error(), dotted,
		"отказ обязан назвать ЭЛЕМЕНТ: автор правила ни одного элемента подстановочной "+
			"строки сам не выбирал, и отказ «в массиве что-то не то» послал бы его "+
			"перечитывать массив, которого он не писал: %v", err)
	require.Truef(t, strings.Contains(err.Error(), "23514") ||
		strings.Contains(err.Error(), "not a live platform resource"),
		"отказ пришёл не от референта третьей поверхности: %v", err)
}

// TestRetiringAResourceNoSelectorNamesLeavesTheCensusAlone — ПАРНЫЙ
// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ.
//
// Без него «повисших стало 1» было бы неотличимо от переписи, которая считает
// повисшим ВСЯКИЙ снятый ресурс — то есть от предиката, срабатывающего на
// снятии как таковом, а не на потерянном референте.
func TestRetiringAResourceNoSelectorNamesLeavesTheCensusAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: требует Postgres")
	}
	ctx, pool := catalogPool(t)
	applier := applierOver(t, pool)

	read0 := selectorObjectTypesRead(t, ctx, pool)
	dangling0, _ := selectorsNamingDeadTypes(t, ctx, pool)
	require.Positive(t, read0, "прочитано ноль элементов object_types — перепись беспредметна")
	require.Zero(t, dangling0, "предпосылка нарушена: повисшие есть до пробы")

	rep, err := applier.Apply(ctx, probeManifest(
		probeResource("unnamed", "get"),
		probeResource("kept", "get"),
	))
	require.NoError(t, err)
	require.True(t, rep.Changed(), "заведение обязано изменить каталог: %s", rep)

	// Селектор пишется на СОСЕДНИЙ ресурс: он остаётся живым, поэтому снятие
	// `unnamed` его референта не касается. Одна ось различия с пробой выше.
	role := catalogRole(t, ctx, pool, "sel1942pos")
	require.NoError(t, writeSelector(ctx, pool, role, "fp-1942-pos",
		[]string{applierProbeModule + ".kept"}))

	gone, err := applier.Apply(ctx, probeManifest(probeResource("kept", "get")))
	require.NoError(t, err)
	require.Positivef(t, gone.RetiredResources, "ресурс не снят (%s) — контроль вакуумен", gone)

	read1 := selectorObjectTypesRead(t, ctx, pool)
	dangling1, list1 := selectorsNamingDeadTypes(t, ctx, pool)
	t.Logf("перепись: элементов прочитано %d → %d · повисших %d → %d %v",
		read0, read1, dangling0, dangling1, list1)
	require.Positive(t, read1, "после снятия перепись читает ноль элементов — она ослепла")
	require.Zerof(t, dangling1,
		"снятие ресурса, которого НЕ называет ни один селектор, дало %d повисших %v — "+
			"перепись срабатывает на снятии как таковом, и число из соседней пробы "+
			"не значит того, что она утверждает", dangling1, list1)
}
