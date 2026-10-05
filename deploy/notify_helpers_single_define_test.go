// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notify_helpers_single_define_test.go — ПОМОЩНИКИ ФЛАГА И ПЕРЕЧНЯ — ПО ОДНОМУ
// ТЕЛУ, В ЧАРТЕ notify (NTF-1, полоса D2; замысел З28 «Помощники — по одному
// телу», CX1-113).
//
// Именованные шаблоны общие на весь релиз: одноимённый `define` в двух чартах
// ошибки не даёт, побеждает один, и победитель зависит от набора включённых
// подчартов. Поэтому каждое имя помощника определено в `deploy/helm/**` ровно
// один раз, и место — чарт notify (`_flag.tpl`, `_sources.tpl`).
//
// Вход помощников безопасен для nil: рендер чарта notify без узла
// `global.kacho.notifications` — отказ с именем ручки
// `global.kacho.notifications.enabled`, а не падение `dig` с текстом
// «interface conversion».

package deploy_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ntfHelperHomes — имя помощника → файл чарта notify, где лежит его тело.
var ntfHelperHomes = map[string]string{
	"kacho.notifications.enabledFor": "helm/notify/templates/_flag.tpl",
	"kacho.notifications.sources":    "helm/notify/templates/_sources.tpl",
}

// ntfDefineSites — `define "<имя>"` по текстовым файлам каталога root:
// имя → пути. Второй возврат — число осмотренных файлов.
func ntfDefineSites(t *testing.T, root string) (map[string][]string, int) {
	t.Helper()
	re := regexp.MustCompile(`\{\{-?\s*define\s+"([^"]+)"`)
	sites := map[string][]string{}
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".tpl", ".yaml", ".yml", ".txt":
		default:
			return nil
		}
		body, rerr := os.ReadFile(p) // #nosec G304 -- путь из обхода дерева
		if rerr != nil {
			return rerr
		}
		files++
		for _, m := range re.FindAllStringSubmatch(string(body), -1) {
			sites[m[1]] = append(sites[m[1]], p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: обход %s: %v", root, err)
	}
	if files == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s ноль файлов шаблонов — обход пуст", root)
	}
	return sites, files
}

// TestNotifyHelpersAreDefinedOnceInTheNotifyChart — каждое имя помощника
// определено в `deploy/helm/**` ровно один раз, в своём файле чарта notify.
func TestNotifyHelpersAreDefinedOnceInTheNotifyChart(t *testing.T) {
	sites, files := ntfDefineSites(t, "helm")
	names := make([]string, 0, len(ntfHelperHomes))
	for n := range ntfHelperHomes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		got := sites[n]
		home := ntfHelperHomes[n]
		if len(got) != 1 || got[0] != home {
			t.Errorf("КРАСНЫЙ: define %q в deploy/helm/** — %d раз (%s), ожидался ровно один в %s (CX1-113)",
				n, len(got), strings.Join(got, ", "), home)
			continue
		}
		t.Logf("define %q — 1, %s", n, home)
	}
	t.Logf("осмотрено файлов шаблонов %d; имён помощников %d", files, len(names))
}

// TestNotifyChartWithoutNotificationsNodeRefusesByKnobName — нога без зонтика
// без узла `global.kacho.notifications` — отказ рендера с именем ручки флага,
// а не падение разыменования (близнец — нога как есть рендерится).
func TestNotifyChartWithoutNotificationsNodeRefusesByKnobName(t *testing.T) {
	requireHelmNTF(t)
	files := standaloneLeg()
	if out, err := renderNotify(t, notifyChartDir, files); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: нога без зонтика как есть не рендерится (%v):\n%s", err, lastLines(out, 6))
	}
	out, err := renderNotify(t, notifyChartDir, files, "global.kacho.notifications=null")
	if rerr := ntfRefusalNames(out, err, ntfFlagKey); rerr != nil {
		t.Errorf("КРАСНЫЙ: нога без зонтика без узла global.kacho.notifications: %v (CX1-113, З28)", rerr)
		return
	}
	t.Logf("нога без зонтика без узла флага → отказ с именем %s", ntfFlagKey)
}
