// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleconfirmprobe_test.go — необратимое действие за подтверждением обязано
// быть закреплено пробой, а непокрытое место — НАЗВАНО.
//
// # Предмет
//
// `Popconfirm` — единственный из видов консоли, за кнопкой которого стоит
// необратимый шаг: отзыв ключа, отзыв токена, снятие тега образа, удаление роли,
// снятие администратора кластера. Пока общий стенд-заменитель рисовал его пустым
// `<div>`, ни текст вопроса, ни то, что нажатие уходит нужным глаголом, не были
// наблюдаемы ВООБЩЕ — писать пробу было не на чем, и отсутствие проб было
// необнаружимо.
//
// Заменитель приведён к форме настоящего (#586), и отсутствие стало обычным
// пробелом. Этот гейт не даёт пробелу вырасти молча: новое подтверждение,
// появившееся без пробы и без записи, роняет прогон.
//
// # Почему перечень, а не «просто требовать пробу везде»
//
// Требовать пробу на все девять мест сразу значило бы либо написать девять проб
// в одном изменении, либо оставить гейт красным — то есть выключить его. Поэтому
// покрытые места держатся предикатом, а непокрытые перечислены ПОИМЁННО, с
// причиной, и перечень САМОИСТЕКАЕТ: запись, у которой предмет исчез или
// покрылся пробой, — находка. Пустой перечень — цель, а не поломка: гейт на нём
// проходит.
//
// # Чего гейт НЕ утверждает
//
// Он утверждает, что рядом с местом есть проба, ОТКРЫВАЮЩАЯ подтверждение, — а
// не что она проверяет правильный глагол. Второе проверить синтаксически нельзя;
// это работа обзора. Гейт закрывает то, что закрывается механически: слот занят
// и в нём что-то есть.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// confirmMarker — признак того, что проба ОТКРЫВАЕТ подтверждение и читает его.
//
// Роль `tooltip` — то, что даёт всплывающая часть настоящего antd; проба, её не
// спрашивающая, подтверждения не открывала, а значит про необратимый шаг не
// утверждает ничего.
const confirmMarker = `"tooltip"`

// confirmRoster — места с подтверждением, пробы на которые ЕЩЁ НЕТ.
//
// Каждая запись — не разрешение, а долг с адресом. Запись, у которой в файле
// больше нет подтверждения ЛИБО рядом появилась проба, роняет гейт: послабление
// обязано истекать само, иначе оно переживёт свой предмет.
var confirmRoster = map[string]string{
	"ui-future/shared/src/pages/system/TokenIssuancePage.tsx":                                 "выдача токена; пробы у страницы нет вовсе",
	"ui-future/shared/src/pages/system/ClusterAdminsPage.tsx":                                 "снятие администратора кластера; пробы у страницы нет вовсе",
	"ui-future/iam/src/registerExtensions.tsx":                                                "два подтверждения на вкладках субъекта; проба есть, подтверждение не открывает",
	"ui-future/iam/src/pages/iam/GroupsPage/GroupsPage.tsx":                                   "удаление группы и удаление участника; проба есть, подтверждение не открывает",
	"ui-future/iam/src/pages/iam/RolesPage/RolesPage.tsx":                                     "удаление роли; пробы у страницы нет вовсе",
	"ui-future/registry/src/components/organisms/RepositoryTagsPanel/RepositoryTagsPanel.tsx": "снятие тега образа; пробы у панели нет вовсе",
}

type confirmFinding struct {
	File string
	Why  string
}

func (f confirmFinding) String() string { return fmt.Sprintf("%s — %s", f.File, f.Why) }

type confirmCensus struct {
	SourcesScanned int
	WithConfirm    int
	Covered        int
	Rostered       int
}

func (c confirmCensus) String() string {
	return fmt.Sprintf(
		"перепись: исходников консоли прочитано %d · из них с подтверждением %d · "+
			"покрыто пробой %d · в перечне непокрытых %d",
		c.SourcesScanned, c.WithConfirm, c.Covered, c.Rostered)
}

// probeFor — путь пробы, соседней с исходником компонента.
func probeFor(rel string) string {
	return strings.TrimSuffix(rel, ".tsx") + ".test.tsx"
}

// auditConsoleConfirmProbes — судья. Вынесен из тела теста, чтобы ТОТ ЖЕ судья
// судил синтетическое дерево пробы инъекции.
// Перечень непокрытых приходит ПАРАМЕТРОМ, а не читается из пакета: иначе пробу
// инъекции пришлось бы судить чужим перечнем, ключи которого к её синтетическому
// дереву отношения не имеют, — и «находка» означала бы несовпадение путей, а не
// свойство.
func auditConsoleConfirmProbes(root string, roster map[string]string) ([]confirmFinding, confirmCensus, error) {
	var census confirmCensus

	dir := filepath.Join(root, "ui-future")
	if _, err := os.Stat(dir); err != nil {
		return nil, census, fmt.Errorf("каталог консоли: %w", err)
	}
	files, err := treecorpus.UnderWithSuffix(dir, ".tsx")
	if err != nil {
		return nil, census, fmt.Errorf("перечень исходников консоли: %w", err)
	}

	seen := map[string]bool{}
	var findings []confirmFinding

	for _, abs := range files {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, census, err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, ".test.tsx") {
			continue
		}
		census.SourcesScanned++

		body, err := os.ReadFile(abs) // #nosec G304 — путь из индекса git этого дерева
		if err != nil {
			return nil, census, fmt.Errorf("чтение %s: %w", rel, err)
		}
		if !strings.Contains(string(body), "<Popconfirm") {
			continue
		}
		census.WithConfirm++
		seen[rel] = true

		covered := false
		if probe, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(probeFor(rel)))); err == nil {
			covered = strings.Contains(string(probe), confirmMarker)
		}

		_, rostered := roster[rel]
		switch {
		case covered && rostered:
			findings = append(findings, confirmFinding{rel,
				"проба открывает подтверждение, но место всё ещё в перечне непокрытых — " +
					"запись пережила свой предмет, снимите её"})
		case covered:
			census.Covered++
		case rostered:
			census.Rostered++
		default:
			findings = append(findings, confirmFinding{rel,
				"необратимое действие за подтверждением, пробы рядом нет и в перечне " +
					"непокрытых место не названо — либо проба, либо запись с причиной"})
		}
	}

	// Обратная сторона самоистечения: запись, чьего предмета в дереве больше нет.
	for rel, why := range roster {
		if !seen[rel] {
			findings = append(findings, confirmFinding{rel,
				"в перечне непокрытых, но подтверждения в файле НЕТ (" + why +
					") — исключать больше нечего, снимите запись"})
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].File < findings[j].File })
	return findings, census, nil
}

// TestConsoleConfirmationsAreProbedOrNamed — гейт класса.
func TestConsoleConfirmationsAreProbedOrNamed(t *testing.T) {
	root := repoRoot(t)

	findings, census, err := auditConsoleConfirmProbes(root, confirmRoster)
	if err != nil {
		t.Fatalf("обход дерева сорвался — вердикта нет: %v", err)
	}
	t.Log(census.String())

	if census.SourcesScanned == 0 {
		t.Fatal("прочитано ноль исходников консоли — предпосылка гейта не выполнена. " +
			"«Ноль находок» здесь означало бы «ноль прочитанного»")
	}

	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "мест с подтверждением, требующих решения: %d\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "%s\n", f)
	}
	b.WriteString("\nЗа кнопкой подтверждения стоит необратимый шаг. Исходов два: написать\n")
	b.WriteString("пробу, открывающую подтверждение и читающую его вопрос, либо назвать место\n")
	b.WriteString("в перечне непокрытых с причиной. Пустой перечень — цель, а не поломка.\n")
	b.WriteString(census.String())
	t.Fatal(b.String())
}
