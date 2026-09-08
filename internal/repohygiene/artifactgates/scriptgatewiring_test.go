// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// scriptgatewiring_test.go — гейт-скрипт из tools/ обязан ИСПОЛНЯТЬСЯ конвейером.
//
// ПРЕДМЕТ. `tools/<имя>/` — дом инструментов уровня репозитория: каждый здешний
// скрипт выносит вердикт о ДЕРЕВЕ (не о сервисе), поэтому ему нужен ровно один
// вызывающий — конвейер. Скрипт, лежащий здесь и не вызванный ниоткуда, работает
// украшением: файл есть, бит исполнения стоит, шапка объясняет, что он меряет, —
// и не меряет ничего никогда.
//
// Как нашлось (перепись 2026-07-30): в четырёх деревьях гейтов 47 единиц, из них
// достижимы из конвейера 45. Два скрипта в tools/ не звал НИКТО — ни workflow, ни
// Makefile, ни другой гейт; единственным упоминанием запуска была строка помощи в
// самом файле. При этом один из них прямо пишет в шапке, что «стало 0» проверяемо
// только вместе с предикатом, которым мерили, — то есть существует ради
// повторяемого замера, которого не происходило.
//
// ПОЧЕМУ СВОЙСТВО, А НЕ ДВЕ ПРАВКИ. Следующий такой инструмент, добавленный по
// образцу соседа, унаследует ту же немоту, и заметить это будет снова некому:
// немой гейт ничем не отличается от вызванного, пока не спросишь конвейер.
// Класс уже случался здесь дважды — стражи рендера (renderguard_test.go) и набор
// манифест-проверок deploy/tests/helm, около двух тысяч строк, не звавшихся ни
// разу.
//
// ДОСТИЖИМОСТЬ СЧИТАЕТСЯ ТРАНЗИТИВНО через Makefile — тем же обходом, что у
// renderguard_test.go (reachableFromWorkflows/anyMentions): вызов из рецепта цели,
// которую зовёт workflow, считается вызовом.
package artifactgates

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// toolsDir — дом инструментов уровня репозитория.
const toolsDir = "tools"

// gateGlobs — где ещё живут гейты уровня дерева, кроме tools/.
//
// Прежняя редакция смотрела ТОЛЬКО в tools/ — 2 файла из 43 гейт-скриптов
// дерева, то есть 5% предмета. Остальные 41 лежат в deploy/scripts (по
// конвенции имени — assert-*/remeasure-*) и в .github/scripts, и правило
// «гейт обязан исполняться» относится к ним ровно так же: измерено — из них
// один (`deploy/scripts/assert-alt-fixtures-are-another.py`) не запускался НИ
// ОТКУДА, кроме собственной самопроверки, при том что комментарий в
// `services/nlb/tests/newman/cases/listener.py:789` утверждал, что этот гейт
// «keeps the pair distinct AND its value produced».
//
// В deploy/scripts предикат — ИМЯ, а не место, и это не отступление от
// «место, а не имя»: каталог смешанный (там же живут прогонщики newman и
// генераторы), а `assert-*`/`remeasure-*` — заведённая в нём конвенция, по
// которой собран и состав самопроверок в run-gate-self-tests.sh.
var gateGlobs = []string{
	"deploy/scripts/assert-*",
	"deploy/scripts/remeasure-*",
	".github/scripts/*",
}

// selfTestOnlyRunner — прогонщик самопроверок НЕ считается вызывающим.
//
// Он находит гейты сам и запускает КАЖДЫЙ как `<гейт> --self-test`, то есть
// против синтетических фикстур, а не против дерева. Засчитать его за вызов
// значило бы объявить гейт исполняемым ровно в том случае, который здесь и
// ловится: доказательство «умею краснеть» есть, а измерения дерева нет.
//
// Предпосылка проверяется отдельно (TestSelfTestRunnerStillOnlySelfTests):
// если прогонщик когда-нибудь начнёт запускать гейты и по-настоящему, это
// исключение станет ложью, и о нём нужно узнать.
const selfTestOnlyRunner = "deploy/scripts/run-gate-self-tests.sh"

// declaredDebtGates — гейты, чей вызов из конвейера объявлен ОТДЕЛЬНО и с числом.
//
// Запись здесь означает: гейт вызывается, но его находки — уже существующий долг
// продукта, а не регресс этой правки; порог зафиксирован в самом шаге конвейера и
// может только сужаться. Это НЕ разрешение не вызывать: такой гейт обязан быть
// достижим наравне с остальными, и обход это проверяет.
//
// Список существует только чтобы объяснить читателю, почему у гейта в конвейере
// стоит порог, а не голый «ноль». Пустой список — норма.
var declaredDebtGates = map[string]string{
	"tools/unreadfieldaudit/unread_field_audit.py": "поля публичного запроса без читателя в прод-коде: " +
		"порогов в шаге конвейера пять (без читателя · не осмотрено · слабая атрибуция · " +
		"полоса «РПЦ-не-реализован» · каталог называет несуществующее поле), каждый " +
		"сужается и растёт только вместе со сменой основы обхода в том же коммите",
}

// isGateScriptExt — языки, на которых в дереве пишут скрипты-гейты.
//
// `.js` здесь не впрок: гейт assert-generated-scripts-parse.js подпадал под
// gateGlobs по ИМЕНИ и всё равно выпадал из обхода — по расширению, — поэтому
// вопрос «а зовёт ли его кто-нибудь» об этом гейте не задавался ВООБЩЕ. Ровно
// та же слепота была у прогонщика самопроверок (deploy/scripts/run-gate-self-tests.sh),
// и закрывать её надо в обоих местах: два обхода, одна пропущенная сторона.
//
// Go сюда НЕ входит, и это решение, а не пропуск: 26 файлов на Go в домах гейтов —
// это пакеты tools/, у каждого есть companion `_test.go`, то есть они доказываются
// обычным `go test`, а не вызовом скрипта из конвейера. Предикат «скрипт» и предикат
// «пакет с тестом» — разные, и смешивать их значило бы требовать от Go-гейта вызова,
// которого его форма не предполагает.
func isGateScriptExt(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".py", ".sh", ".js":
		return true
	}
	return false
}

// findToolScriptGates — все скрипты-гейты в tools/<имя>/.
//
// Предикат — МЕСТО, а не имя и не содержимое: `tools/` заведён под инструменты
// уровня репозитория, и другого назначения у файла здесь нет. Ловить по имени
// («*audit*», «verify-*») значило бы завести слепую зону под первый же скрипт,
// названный иначе.
func findToolScriptGates(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	base := filepath.Join(root, toolsDir)
	err := filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !isGateScriptExt(p) {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s сорвался: %v — предпосылка гейта не выполняется", toolsDir, err)
	}
	// Плюс гейты вне tools/ — по конвенции имени своего каталога.
	tt := newTrackedTree(t, root)
	for rel := range tt.files {
		if !isGateScriptExt(rel) {
			continue
		}
		if rel == selfTestOnlyRunner {
			continue // он не гейт дерева, а прогонщик самопроверок
		}
		for _, g := range gateGlobs {
			if ok, _ := filepath.Match(g, rel); ok {
				out = append(out, rel)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// reachableIncludingScripts — команды конвейера ПЛЮС тела скриптов, которые
// конвейер запускает, транзитивно.
//
// Без этого шага гейт, вызванный из другого — уже вызванного — гейта, читался
// бы как немой: обход достижимости идёт по workflow'ам и Makefile'ам и внутрь
// шелла не заходит. Реальные пары в этом дереве:
// `assert-production-posture.sh` → `assert-admin-hop-transport.sh` →
// `remeasure-provider-listener-tls.sh`. Ложное срабатывание на них выключило бы
// гейт при первой же встрече — поэтому расширение обязательно, а не удобно.
//
// Единственное исключение — selfTestOnlyRunner, см. его комментарий.
func reachableIncludingScripts(t *testing.T, root string) []string {
	t.Helper()
	texts := reachableFromWorkflows(t, root)
	tt := newTrackedTree(t, root)

	var scripts []string
	for rel := range tt.files {
		ext := strings.ToLower(filepath.Ext(rel))
		if (ext == ".py" || ext == ".sh") && rel != selfTestOnlyRunner {
			scripts = append(scripts, rel)
		}
	}
	sort.Strings(scripts)

	pulled := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, s := range scripts {
			if pulled[s] || !anyMentions(texts, s) {
				continue
			}
			body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(s)))
			if err != nil {
				t.Fatalf("чтение достижимого скрипта %s: %v", s, err)
			}
			pulled[s] = true
			texts = append(texts, executableLines(string(body)))
			changed = true
		}
	}
	return texts
}

// executableLines — тело скрипта БЕЗ комментариев.
//
// Без этого шага «вызывающим» становится любой упомянувший: комментарий в
// `services/nlb/tests/newman/cases/listener.py:789` называет
// `deploy/scripts/assert-alt-fixtures-are-another.py` и утверждает, что тот
// «keeps the pair distinct» — а гейт не запускался ни разу нигде, кроме
// собственной самопроверки. Ровно тот случай, ради которого этот файл написан:
// объяснение защиты неотличимо от самой защиты, если читать текст, а не код.
// Тот же приём и по той же причине — в `run-gate-self-tests.sh`.
func executableLines(body string) string {
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#!") {
			continue
		}
		if i := strings.Index(line, " # "); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestSelfTestRunnerStillOnlySelfTests — предпосылка исключения выше.
//
// Исключение обосновано ФАКТОМ: прогонщик запускает найденное только с
// `--self-test`. Факт может измениться, и тогда исключение станет ложным
// утверждением о дереве — ровно тот класс, который эти гейты и ловят. Поэтому
// он проверяется, а не предполагается.
func TestSelfTestRunnerStillOnlySelfTests(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(selfTestOnlyRunner)))
	if err != nil {
		t.Fatalf("%s не читается: %v — исключение объявлено для того, чего нет", selfTestOnlyRunner, err)
	}
	if !strings.Contains(string(body), "--self-test") {
		t.Errorf("%s больше не запускает гейты с --self-test: исключение «этот вызывающий не "+
			"считается» перестало быть верным. Либо сними исключение, либо назови новую причину.",
			selfTestOnlyRunner)
	}
}

// TestEveryToolScriptGateIsInvoked — ни один гейт-скрипт tools/ не остаётся немым.
func TestEveryToolScriptGateIsInvoked(t *testing.T) {
	root := repoRoot(t)

	gates := findToolScriptGates(t, root)
	// Перепись — ОТДЕЛЬНОЕ утверждение. Обход, переставший доходить до скриптов
	// (каталог переименовали, расширение сменили), выходит ЗЕЛЁНЫМ на пустом
	// множестве — ровно тот класс, который здесь искореняют.
	if len(gates) == 0 {
		t.Fatalf("в %s/ не найдено ни одного скрипта-гейта — обход сломан, а не дерево чисто", toolsDir)
	}
	t.Logf("осмотрено гейтов-скриптов: %d (%s/ и %s)", len(gates), toolsDir, strings.Join(gateGlobs, ", "))

	reachable := reachableIncludingScripts(t, root)
	if len(reachable) == 0 {
		t.Fatal("из конвейера не извлечено ни одной команды — обход достижимости сломан")
	}

	for _, g := range gates {
		if anyMentions(reachable, g) {
			continue
		}
		note := ""
		if why, ok := declaredDebtGates[g]; ok {
			note = "\n     У гейта объявлен порог (" + why + "), но объявление порога НЕ ЗАМЕНЯЕТ вызова: " +
				"пока его никто не зовёт, порог тоже ничего не держит."
		}
		t.Errorf("%s не вызывается НИ ОТКУДА: ни один workflow и ни одна достижимая из "+
			"workflow цель Makefile его не запускает. Гейт, которого никто не звал, — "+
			"украшение: он не может ни покраснеть, ни отчитаться.%s\n"+
			"     Починка — ровно два исхода: (1) позвать его из конвейера шагом с вердиктом "+
			"по содержимому; (2) удалить вместе с его предметом, если мерить больше нечего. "+
			"Оставить в дереве невызванным — не исход.", g, note)
	}
}

// TestDeclaredDebtGatesStillHaveSubject — запись о пороге не переживает свой предмет.
//
// Порог объявляется под КОНКРЕТНЫЙ существующий долг. Если гейта уже нет в дереве,
// запись описывает то, чего не существует, и следующий читатель примет её за
// действующее разрешение.
func TestDeclaredDebtGatesStillHaveSubject(t *testing.T) {
	root := repoRoot(t)
	for g := range declaredDebtGates {
		if _, err := os.Stat(filepath.Join(root, g)); err != nil {
			t.Errorf("в declaredDebtGates объявлен порог для %s, а такого гейта в дереве нет — "+
				"запись без предмета, удали её", g)
		}
	}
}
