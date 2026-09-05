// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// providersurfacedeployment_test.go — административную дорогу к поставщику
// НЕ прокладывает рабочая нагрузка, порождённая чартом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Рядом стоит гейт, требующий, чтобы с поставщиком говорили только файлы
// ведомости. Он обходит `git ls-files -- '*.go'`, поэтому о дереве вне Go не
// утверждает НИЧЕГО — и «ноль находок» у него означает «ноль прочитанного» для
// каждого шаблона чарта.
//
// В эту слепую зону и попало задание, регистрировавшее доверие внешним
// издателям У ПОСТАВЩИКА: оно `curl -X POST`-ило его административный путь из
// пода, то есть было полноценным потребителем поверхности — невидимым для
// ведомости by construction, потому что написано не на Go.
//
// Предмет у задания исчез: перечень доверенных издателей стал НАШЕЙ таблицей
// (#1124), её читает наша проверка утверждения на пути запроса, а запись у
// поставщика на решение о доступе не влияет. Само задание при этом не работало
// и по прежней роли — оно POSTило шаблон ключа-заглушки, который поставщик
// отвергает.
//
// Гейт заведён не ради удаления (удалить можно и без него), а ради того, чтобы
// «следующий увидит и решит, что так и надо» перестало быть возможным.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦЫ ПРЕДИКАТА, каждая с причиной
//
//  1. Только пути, начинающиеся с `/admin/`. `/oauth2/token` — стандартная
//     выдача токена, её законно называют профили, кейсы и документация; включив
//     её, гейт получил бы популяцию ложных находок и был бы отключён первым же.
//  2. Только ИСПОЛНЯЕМАЯ часть. Комментарий, объясняющий, ЗАЧЕМ соседний
//     контейнер знает адрес, законен и обязан молчать — такой в дереве есть, и
//     он служит законным близнецом (см. самопроверку).
//  3. Только шаблоны чартов. Файл значений объявляет АДРЕС для потребителя на
//     Go, а тот уже связан ведомостью; шаблон же порождает рабочую нагрузку,
//     у которой ведомости нет.
//
// Словарь поверхностей берётся ИЗ ОБЩЕГО ИСТОЧНИКА (ProviderSurfaces),
// а не копируется: две копии одного предиката расходятся молча, и разошлись бы
// именно здесь — соседний гейт судит Go, этот шаблоны, а спор между ними
// решался бы в пользу того, кто громче упал.

// helmComment — блок `{{/* … */}}`, которым комментируют шаблоны helm.
var helmComment = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)

// deploymentExecutablePart — исполняемая часть шаблона: блоки `{{/* … */}}` и
// строки, начинающиеся с `#`, вырезаны. Гейт обязан читать то, что ИСПОЛНЯЕТСЯ:
// иначе объяснение, зачем адрес нужен, читалось бы как обращение к нему.
func deploymentExecutablePart(body string) string {
	body = helmComment.ReplaceAllString(body, "")
	var out []string
	for _, raw := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		out = append(out, raw)
	}
	return strings.Join(out, "\n")
}

// adminSurfaces — административные пути общего словаря. Выводятся, а не
// выписываются: словарь пополнится — гейт узнает об этом сам.
func adminSurfaces() []ProviderSurface {
	var out []ProviderSurface
	for _, s := range ProviderSurfaces {
		if strings.HasPrefix(s.Path, "/admin/") {
			out = append(out, s)
		}
	}
	return out
}

// chartTemplates — шаблоны чартов, спрошенные У ИНДЕКСА. Обход диска судил бы
// произведённые файлы и чужие рабочие каталоги.
func chartTemplateBodies(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "deploy/helm").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"означало бы «ноль прочитанного»", err)
	}
	files := map[string]string{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || !strings.Contains(rel, "/templates/") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		files[rel] = string(raw)
	}
	return files
}

// deploymentProviderLedger — шаблоны, которым говорить с поставщиком РАЗРЕШЕНО.
//
// Пуст намеренно, и пустота здесь — ЦЕЛЬ, а не поломка: рабочая нагрузка,
// порождённая чартом, административной дороги к поставщику не прокладывает.
// Проба на пустой ведомости обязана ПРОХОДИТЬ — падающая подталкивала бы
// держать запись ради зелёного.
var deploymentProviderLedger = map[string][]string{}

func TestChartTemplatesDoNotDialTheProviderAdminSurface(t *testing.T) {
	templates := chartTemplateBodies(t)
	surfaces := adminSurfaces()

	if len(templates) == 0 || len(surfaces) == 0 {
		t.Fatalf("обход ничего не прочитал: шаблонов=%d, административных поверхностей=%d — "+
			"предикат перестал узнавать дерево, а не дерево стало чистым",
			len(templates), len(surfaces))
	}

	names := make([]string, 0, len(templates))
	for rel := range templates {
		names = append(names, rel)
	}
	sort.Strings(names)

	findings, allowed := 0, 0
	for _, rel := range names {
		exec := deploymentExecutablePart(templates[rel])
		for _, s := range surfaces {
			if !strings.Contains(exec, s.Path) {
				continue
			}
			if permitted, ok := deploymentProviderLedger[rel]; ok {
				if containsSurfacePath(permitted, s.Path) {
					allowed++
					continue
				}
			}
			findings++
			t.Errorf("%s обращается к административной поверхности поставщика %s (%s). "+
				"Рабочая нагрузка, порождённая чартом, административной дороги к поставщику "+
				"не прокладывает: у неё нет ведомости, которой связан прод-код на Go, поэтому "+
				"такой вызов не осматривает никто. Если решение у поставщика больше не "+
				"спрашивают — сними задание вместе с его ручками; если оно нужно — заведи "+
				"запись в deploymentProviderLedger с причиной",
				rel, s.Path, s.What)
		}
	}

	paths := make([]string, 0, len(surfaces))
	for _, s := range surfaces {
		paths = append(paths, s.Path)
	}
	t.Logf("перепись: шаблонов чартов осмотрено — %d; административных поверхностей в словаре — %d (%s); "+
		"записей ведомости — %d; разрешённых обращений — %d; находок — %d",
		len(templates), len(surfaces), strings.Join(paths, " "), len(deploymentProviderLedger), allowed, findings)
}

// Запись ведомости обязана иметь предмет.
func TestDeploymentProviderLedger_StillHasASubject(t *testing.T) {
	templates := chartTemplateBodies(t)
	for rel, permitted := range deploymentProviderLedger {
		body, ok := templates[rel]
		if !ok {
			t.Errorf("записи %q больше нечего разрешать — такого шаблона в дереве нет. "+
				"Удали её: разрешение, пережившее свой предмет, разрешает то, чего нет", rel)
			continue
		}
		exec := deploymentExecutablePart(body)
		for _, p := range permitted {
			if !strings.Contains(exec, p) {
				t.Errorf("запись %q разрешает %s, но шаблон к нему не обращается — "+
					"разрешение потеряло предмет", rel, p)
			}
		}
	}
	t.Logf("записей ведомости — %d", len(deploymentProviderLedger))
}

func containsSurfacePath(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
