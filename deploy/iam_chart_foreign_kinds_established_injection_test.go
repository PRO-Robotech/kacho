// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iam_chart_foreign_kinds_established_injection_test.go — доказательство того,
// что сверка «что чарт везёт» с «что владелец подъёма заводит» СПОСОБНА упасть,
// и что она молчит на законном близнеце.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЛЕВЕР ИНЪЕКЦИИ — ОДИН ФАКТ, И ОН НАСТОЯЩИЙ
//
// Подменяется РОВНО перечень владельца подъёма: `KANAME_CHART_BOOTS_FOREIGN_KINDS`.
// Чарт при этом настоящий, рендер настоящий, скрипт настоящий — то есть путь
// проверяется ВЕСЬ (рендер → классификатор → перечень → сверка), а не одна
// функция.
//
// Подстановка в скрипте объявлена как `${VAR-умолчание}`, а НЕ `${VAR:-…}`:
// иначе объявленный пустым перечень молча вернул бы умолчание, инъекция была бы
// недейственной, и «способность падать» доказывалась бы не тем входом. Это
// свойство здесь и закрепляется — сквозной случай И1 упал бы, будь в скрипте
// `:-`.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ ФАЙЛ НЕ ДЕЛАЕТ
//
// Он не поднимает кластера и потому НЕ утверждает, что определение по названному
// адресу существует и правда заводит вид. Это решает живой прогон полосы
// кластера, и только он.
package deploy_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// foreignKindsRosterEnv — ручка, которой подменяется перечень владельца подъёма.
const foreignKindsRosterEnv = "KANAME_CHART_BOOTS_FOREIGN_KINDS"

// failsUnder — исполняет обращение в СВОЕЙ горутине и отвечает, упало ли оно.
//
// Отдельная горутина здесь несущая, а не стилистическая: отказ утверждения —
// это `runtime.Goexit`, а НЕ паника, и `recover` его не ловит. В общей горутине
// такой отказ унёс бы саму пробу, и доказательство «разборщик умеет
// отвергнуть» превратилось бы в падение доказывающего.
func failsUnder(call func(t *testing.T)) bool {
	sub := &testing.T{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		call(sub)
	}()
	<-done
	return sub.Failed()
}

// unestablishedAgainst — чужие виды рендера, которых нет в перечне. Та же
// арифметика, что у гейта; вынесена, чтобы инъекция подавала вход настоящему
// пути, а не переписывала его своей копией.
func unestablishedAgainst(rendered string, roster []string) []string {
	established := map[string]bool{}
	for _, k := range roster {
		established[k] = true
	}
	var out []string
	for _, api := range renderedAPIVersions(rendered) {
		if builtinAPIGroups[apiGroupOf(api)] || established[api] {
			continue
		}
		out = append(out, api)
	}
	sort.Strings(out)
	return out
}

// TestIAMChartForeignKinds_FailsWhenTheBootOwnerEstablishesNothing — И1 СКВОЗНАЯ.
//
// Настоящий скрипт с ПУСТЫМ перечнем. Находка обязана назвать ИМЕННО чужой вид,
// а не «что-то не сошлось»: находка, называющая симптом, посылает читателя
// искать не там, и на неё тратят прогон.
func TestIAMChartForeignKinds_FailsWhenTheBootOwnerEstablishesNothing(t *testing.T) {
	rendered := renderDeliveredIAMChart(t)

	empty := bootOwnerForeignKinds(t, iamBootOwnerScript, foreignKindsRosterEnv+"=")
	require.Emptyf(t, empty,
		"перечень объявлен пустым, а владелец вернул %v — подстановка недейственна: "+
			"в скрипте `${VAR:-…}` вместо `${VAR-…}`, и инъекция ничего не подменила", empty)

	missing := unestablishedAgainst(rendered, empty)
	require.NotEmpty(t, missing,
		"владелец не заводит НИ ОДНОГО вида, а сверка находок не дала — "+
			"значит она не читает чужие виды рендера вовсе")
	require.Containsf(t, missing, "monitoring.coreos.com/v1",
		"находка не назвала чужой вид поимённо, а перечислила %v", missing)
}

// TestIAMChartForeignKinds_SilentOnTheLegalTwin — И2 ЗАКОННЫЙ БЛИЗНЕЦ.
//
// Тот же путь, ничего не подменено. Без него отрицание выше зеленело бы на
// сверке, которая краснеет всегда.
func TestIAMChartForeignKinds_SilentOnTheLegalTwin(t *testing.T) {
	rendered := renderDeliveredIAMChart(t)

	roster := bootOwnerForeignKinds(t, iamBootOwnerScript)
	require.NotEmpty(t, roster, "перечень дерева пуст — тогда близнец не законный, а вырожденный")
	require.Empty(t, unestablishedAgainst(rendered, roster),
		"на нетронутом дереве сверка обязана молчать")
}

// TestIAMChartForeignKinds_StaleRosterEntryIsAFinding — И3 САМОИСТЕЧЕНИЕ.
//
// Запись, которой больше нечего заводить, — находка. Без этой стороны перечень
// копил бы записи про виды, снятые с поставки, и унаследовал бы следующую
// слепую зону.
func TestIAMChartForeignKinds_StaleRosterEntryIsAFinding(t *testing.T) {
	rendered := renderDeliveredIAMChart(t)

	const ghost = "gone.example.invalid/v1"
	roster := bootOwnerForeignKinds(t, iamBootOwnerScript,
		foreignKindsRosterEnv+"="+ghost+"§ghosts.gone.example.invalid§https://example.invalid/ghost.yaml")

	shipped := map[string]bool{}
	for _, api := range renderedAPIVersions(rendered) {
		shipped[api] = true
	}
	var stale []string
	for _, k := range roster {
		if !shipped[k] {
			stale = append(stale, k)
		}
	}
	require.Containsf(t, stale, ghost,
		"вид, которого чарт не везёт, не признан устаревшей записью: %v", stale)
}

// TestIAMChartForeignKinds_ClassifierSeparatesBothSides — И4 КЛАССИФИКАТОР.
//
// Синтетика намеренно: настоящий чарт несёт ровно один чужой вид, а предмет
// здесь — обе стороны разделения. Классификатор, относящий к чужим ВСЁ, дал бы
// на дереве тот же зелёный.
func TestIAMChartForeignKinds_ClassifierSeparatesBothSides(t *testing.T) {
	builtin := []string{"v1", "apps/v1", "batch/v1", "networking.k8s.io/v1", "rbac.authorization.k8s.io/v1"}
	foreign := []string{"monitoring.coreos.com/v1", "cert-manager.io/v1", "gateway.networking.example/v1"}

	for _, api := range builtin {
		require.Truef(t, builtinAPIGroups[apiGroupOf(api)],
			"встроенный вид %q признан чужим — гейт потребовал бы заводить то, что кластер служит сам", api)
	}
	for _, api := range foreign {
		require.Falsef(t, builtinAPIGroups[apiGroupOf(api)],
			"чужой вид %q признан встроенным — он ушёл бы из-под наблюдения МОЛЧА", api)
	}
}

// TestIAMChartForeignKinds_NestedAPIVersionIsNotAnObject — И5 РАЗБОРЩИК РЕНДЕРА.
//
// `apiVersion` встречается и ВНУТРИ тел — в ссылках владельца, в шаблонах пода.
// Счёт по подстроке считал бы вложенное объектом и мог бы объявить чужой вид
// там, где его никто не ставит.
func TestIAMChartForeignKinds_NestedAPIVersionIsNotAnObject(t *testing.T) {
	doc := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: kaname\n" +
		"  ownerReferences:\n    - apiVersion: nested.example.invalid/v1\n      kind: Ghost\n"
	got := renderedAPIVersions(doc)
	require.Equal(t, []string{"apps/v1"}, got,
		"разборщик посчитал вложенный apiVersion объектом")

	two := doc + "\n---\napiVersion: monitoring.coreos.com/v1\nkind: PrometheusRule\n"
	require.Equal(t, []string{"apps/v1", "monitoring.coreos.com/v1"}, renderedAPIVersions(two),
		"разборщик потерял второй документ")
}

// TestIAMChartForeignKinds_MalformedRosterEntryIsRejected — И6 НЕГОДНАЯ ЗАПИСЬ.
//
// Запись без третьего поля владелец пропустил бы МОЛЧА, и вид остался бы
// незаведённым при внешне полном перечне. Проверяется тем же ключом, которым
// перечень читает гейт.
func TestIAMChartForeignKinds_MalformedRosterEntryIsRejected(t *testing.T) {
	failed := failsUnder(func(sub *testing.T) {
		bootOwnerForeignKinds(sub, iamBootOwnerScript,
			foreignKindsRosterEnv+"=monitoring.coreos.com/v1§только-два-поля")
	})
	require.True(t, failed,
		"запись без третьего поля принята — владелец пропустил бы её молча, "+
			"и вид остался бы незаведённым при внешне полном перечне")
}

// TestIAMChartForeignKinds_BootOwnerRefusalIsNotAnEmptyRoster — И7 ТРЕТЬЯ КАТЕГОРИЯ.
//
// «Владелец не умеет отвечать» обязано быть отличимо от «владелец ничего не
// заводит»: иначе снятый ключ читался бы как пустой перечень, и гейт краснел бы
// НЕ ТЕМ текстом — либо, что хуже, зеленел бы, если перечень и правда пуст.
func TestIAMChartForeignKinds_BootOwnerRefusalIsNotAnEmptyRoster(t *testing.T) {
	dir := t.TempDir()
	deaf := filepath.Join(dir, "deaf.sh")
	require.NoError(t, os.WriteFile(deaf,
		[]byte("#!/usr/bin/env bash\necho 'неизвестный ключ '\"$1\"\nexit 2\n"), 0o600))

	failed := failsUnder(func(sub *testing.T) { bootOwnerForeignKinds(sub, deaf) })
	require.True(t, failed,
		"отказ владельца прочитан как пустой перечень — «спросить не удалось» "+
			"засчитано за ответ")
}

// TestIAMChartForeignKinds_CensusCountsWhatItRead — И8 ПЕРЕПИСЬ НЕ ВАКУУМНА.
//
// «Ноль находок» обязано быть отличимо от «ноль прочитанного»: пустой рендер
// роняет гейт, а не даёт ему зелёное на пустом обходе.
func TestIAMChartForeignKinds_CensusCountsWhatItRead(t *testing.T) {
	require.Empty(t, renderedAPIVersions(""),
		"пустой рендер дал объекты — тогда пустой обход был бы неотличим от полного")
	require.Empty(t, renderedAPIVersions("# только комментарий\n"),
		"документ без apiVersion/kind посчитан объектом")

	rendered := strings.Join([]string{
		"apiVersion: v1", "kind: Service", "---", "apiVersion: v1", "kind: ConfigMap",
	}, "\n")
	require.Len(t, renderedAPIVersions(rendered), 2, "перепись потеряла объект")
}
