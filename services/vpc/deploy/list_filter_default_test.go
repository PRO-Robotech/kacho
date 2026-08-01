// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_filter_default_test.go — фильтр видимости включён ПО УМОЛЧАНИЮ, и ни один
// профиль его не выключает.
//
// ЧТО ОХРАНЯЕТСЯ. `authz.listFilter.enabled` решает, сужается ли прочитанная
// страница публичного List до объектов, на которые у вызывающего есть грант.
// Выключенный фильтр означает, что любой участник проекта видит КАЖДУЮ его
// строку, а помеченные ScopeFiltered RPC (за ними per-RPC Check не задаётся
// вовсе) остаются вообще без object-scope авторизации.
//
// ПОЧЕМУ ГЕЙТ ПОНАДОБИЛСЯ. Значение по умолчанию было `false`, и оно повторялось
// ДВАЖДЫ: в values.yaml и ещё раз в шаблоне как `default false`. Правка одного
// места ничего бы не изменила. Соседи по тому же классу — nlb и storage —
// объявляют `true`; vpc был единственным исключением, и никакая проба этого не
// читала. Профили prod и dev значение переопределяли, поэтому дефект был не
// виден ровно до первой посадки, которая этого не сделала, — а таких профилей в
// umbrella большинство.
//
// ПОЧЕМУ ЧИТАЮТСЯ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР. Приём взят у gateway
// (deploy/token_shape_test.go): контракт — это то, что профиль ОБЪЯВЛЯЕТ; чтение
// объявления не требует ни helm, ни зависимостей чарта, поэтому такая проба не
// может пропуститься. Отсутствие ключа — падение, а не пропуск.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	vpcValues     = "values.yaml"
	vpcConfigMap  = "templates/configmap.yaml"
	umbrellaGlob  = "../../../deploy/helm/umbrella/values*.yaml"
	minUmbrellaNo = 2
)

// listFilterEnabledDecl ловит объявление ручки в values-файле: `enabled: <v>`
// внутри блока listFilter. Читается ИМЕННО объявление — строка вида
// `enabled: false` под `listFilter:`.
var listFilterEnabledDecl = regexp.MustCompile(`(?m)^(\s*)listFilter:\s*$`)

// declaredListFilterEnabled возвращает объявленное значение ручки и признак того,
// что объявление вообще найдено. Ненайденное объявление — НЕ «значение по
// умолчанию»: это отдельный исход, и вызывающий обязан его различать.
func declaredListFilterEnabled(t *testing.T, path string) (value string, found bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("не прочитан %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	for i, ln := range lines {
		m := listFilterEnabledDecl.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		indent := len(m[1])
		// Ищем `enabled:` строго глубже отступа блока и до конца блока.
		for _, in := range lines[i+1:] {
			trimmed := strings.TrimSpace(in)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			curIndent := len(in) - len(strings.TrimLeft(in, " "))
			if curIndent <= indent {
				break // блок кончился
			}
			if strings.HasPrefix(trimmed, "enabled:") {
				v := strings.TrimSpace(strings.TrimPrefix(trimmed, "enabled:"))
				// Хвостовой комментарий — не часть значения.
				if i := strings.Index(v, "#"); i >= 0 {
					v = strings.TrimSpace(v[:i])
				}
				return v, true
			}
		}
	}
	return "", false
}

// Базовый профиль чарта ОБЯЗАН объявлять фильтр включённым.
//
// Молчание базового профиля значит, что он отрендерится, — поэтому требуется
// именно положительное объявление, а не отсутствие `false`.
func TestChartDefault_ListFilterEnabled(t *testing.T) {
	got, found := declaredListFilterEnabled(t, vpcValues)
	if !found {
		t.Fatalf("%s не объявляет authz.listFilter.enabled — гейт смотрит не туда, "+
			"его вердикт беспредметен", vpcValues)
	}
	if got != "true" {
		t.Fatalf("%s объявляет authz.listFilter.enabled: %s, обязано быть true — "+
			"выключенный по умолчанию фильтр отдаёт каждому участнику проекта все его строки, "+
			"а ScopeFiltered RPC оставляет вообще без object-scope авторизации "+
			"(nlb и storage объявляют true; vpc был единственным исключением)", vpcValues, got)
	}
}

// Шаблон не вправе восстанавливать небезопасное умолчание.
//
// В configmap.yaml стояло `default false`, то есть значение по умолчанию было
// объявлено ДВАЖДЫ, и удаление ключа из values.yaml не помогло бы: рендер всё
// равно дал бы выключенный фильтр. Пока обе записи не сходятся, «поправили
// дефолт» — утверждение о ровно половине предмета.
func TestChartTemplate_DoesNotReassertUnsafeDefault(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(vpcConfigMap))
	if err != nil {
		t.Fatalf("не прочитан %s: %v", vpcConfigMap, err)
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		if !strings.Contains(ln, "listFilter.enabled") {
			continue
		}
		if strings.Contains(ln, "default false") {
			t.Fatalf("%s восстанавливает небезопасное умолчание: %q — "+
				"правка values.yaml на этой строке не имеет силы", vpcConfigMap, strings.TrimSpace(ln))
		}
		return
	}
	t.Fatalf("%s не рендерит listFilter.enabled вовсе — ручка не доезжает до процесса", vpcConfigMap)
}

// Ни один профиль umbrella не выключает фильтр.
//
// ПЕРЕПИСЬ отдельным утверждением: если glob не нашёл профилей, «нарушений нет»
// означало бы «ничего не прочитано». Эти два исхода обязаны быть различимы.
func TestUmbrellaProfiles_NeverDisableTheListFilter(t *testing.T) {
	profiles, err := filepath.Glob(umbrellaGlob)
	if err != nil {
		t.Fatalf("glob %s: %v", umbrellaGlob, err)
	}
	if len(profiles) < minUmbrellaNo {
		t.Fatalf("найдено %d профилей umbrella по %s (ожидалось ≥%d) — гейт смотрит не туда",
			len(profiles), umbrellaGlob, minUmbrellaNo)
	}

	var examined, declaring int
	for _, p := range profiles {
		examined++
		got, found := declaredListFilterEnabled(t, p)
		if !found {
			continue // профиль наследует базовый чарт — а тот проверен выше
		}
		declaring++
		if got != "true" {
			t.Errorf("%s объявляет listFilter.enabled: %s — профиль снимает сужение страницы", p, got)
		}
	}
	t.Logf("перепись: осмотрено профилей %d, объявляют ручку %d, базовый чарт %s",
		examined, declaring, vpcValues)
}
