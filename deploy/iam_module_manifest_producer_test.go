// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_producer_test.go — у ДОСТАВКИ манифестов есть
// ПРОИЗВОДИТЕЛЬ, и имя ConfigMap у него и у чарта — ОДНО объявление (задача
// #1901).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Путь доставки был заведён целиком (#1875): чарт монтирует именованный
// ConfigMap, процесс читает смонтированный каталог, страж старта отказывает на
// сорванной доставке. Наполнить этот ConfigMap в дереве было НЕЧЕМ — ни цели
// сборки, ни шага подъёма стенда, ни объявления внешнего конвейера. Поэтому
// опору на манифесты (`manifests.required: true`) нельзя было объявить ни в
// одном профиле: под поднимется с пустым каталогом, а процесс откажет.
//
// Соседняя проверка (iam_module_manifest_delivery_test.go) судит СОГЛАСИЕ ДВУХ
// ПОЛОВИН доставки — каталог пода и каталог процесса. Здесь предмет третий:
// ЕСТЬ ЛИ ЧЕМ ЭТОТ КАТАЛОГ НАПОЛНИТЬ и совпадает ли имя объекта у того, кто его
// кладёт, с именем у того, кто его монтирует.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО ЗДЕСЬ ПРОВЕРЯЕТСЯ
//
//	(1) каждый стенд, ОБЪЯВИВШИЙ доставку, получает от производителя годный
//	    объект: имя — то самое, ключи — по одному на манифест дерева, тела —
//	    побайтово те же;
//	(2) хотя бы один стенд доставку объявляет — иначе проверка беспредметна и
//	    зеленела бы на дереве, где производителя нет вовсе;
//	(3) собранный объект РАЗБИРАЕТСЯ обратно в те же байты: производитель
//	    печатает YAML, и «собрал» обязано означать «применимо», а не «похоже»;
//	(4) подъём стенда ЗОВЁТ производителя — иначе объект существует только в
//	    воображении цели сборки, а под монтирует пустоту.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ИМЯ ЧИТАЕТСЯ ИЗ ПРОФИЛЯ, А НЕ СВЕРЯЕТСЯ С КОПИЕЙ
//
// Производитель берёт имя из ТЕХ ЖЕ файлов значений, которые получает helm.
// Копия имени в производителе разошлась бы с профилем молча: под смонтировал бы
// один объект, производитель положил бы другой, каталог доставки приехал бы
// пустым — и снаружи это неотличимо от «модулей нет». Что имя действительно
// ВЫВОДИТСЯ, а не выписано, доказывает инъекция в соседнем файле: синтетическая
// цепочка с другим именем обязана дать другое имя.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/tools/modulemanifests"
)

// producerCensus — объём осмотренного этой проверкой.
type producerCensus struct {
	Stacks          int
	StacksDeclaring int
	Manifests       int
	Bytes           int
}

// treeManifests — манифесты дерева: каталог службы → тело. Перечень ВЫВОДИТСЯ
// обходом, а не выписывается: выписанный разошёлся бы с деревом ровно тогда,
// когда заводят новый модуль.
func treeManifests(t *testing.T) map[string][]byte {
	t.Helper()
	dir := filepath.Join(repoRoot, "services")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог служб %s не прочитан: %v — непрочитанное есть НАХОДКА, "+
			"а не «служб нет»", dir, err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name(), "manifest.yaml")
		// #nosec G304 -- путь собран из константы repoRoot и имени каталога,
		// прочитанного обходом дерева.
		body, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("%s: манифест не прочитан: %v", p, err)
		}
		out[e.Name()] = body
	}
	if len(out) == 0 {
		t.Fatalf("в %s нет ни одного манифеста — вердикт беспредметен: «ноль находок» "+
			"обязано быть отличимо от «ноль прочитанного»", dir)
	}
	return out
}

// chainPaths — профили стенда путями от каталога deploy.
func chainPaths(chain []string) []string {
	out := make([]string, 0, len(chain))
	for _, p := range chain {
		out = append(out, filepath.Join(umbrellaDir, p))
	}
	return out
}

// declaredNameInChain — имя ConfigMap, ОБЪЯВЛЕННОЕ цепочкой, прочитанное этой
// проверкой САМОСТОЯТЕЛЬНО. Сверяется с тем, что вернул производитель: копия
// имени внутри производителя иначе прошла бы незамеченной.
func declaredNameInChain(t *testing.T, chain []string) string {
	t.Helper()
	name := ""
	for _, p := range chainPaths(chain) {
		// #nosec G304 -- путь собран из константы umbrellaDir и цепочки,
		// прочитанной из stacks.txt.
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("профиль %s не прочитан: %v", p, err)
		}
		var tree map[string]any
		if err := yaml.Unmarshal(raw, &tree); err != nil {
			t.Fatalf("профиль %s не разобран: %v", p, err)
		}
		if v, ok := nestedString(tree, "kacho-iam", "manifests", "configMapName"); ok {
			name = v
		}
	}
	return name
}

// TestModuleManifestConfigMapHasAProducer — производитель есть, и он даёт годный
// объект каждому стенду, объявившему доставку.
func TestModuleManifestConfigMapHasAProducer(t *testing.T) {
	tree := treeManifests(t)
	stacks := deployStacks(t)
	census := producerCensus{Stacks: len(stacks)}

	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, stack := range names {
		chain := stacks[stack]
		want := declaredNameInChain(t, chain)
		delivery, err := modulemanifests.Collect(repoRoot, chainPaths(chain))
		if want == "" {
			// Стенд доставку не объявил — законный исход, а не отказ.
			if err == nil {
				t.Errorf("стенд %s доставку не объявляет, а производитель собрал объект %q — "+
					"тогда «стенд не опирается на манифесты» неотличимо от «опирается»",
					stack, delivery.Name)
			}
			continue
		}
		census.StacksDeclaring++
		if err != nil {
			t.Errorf("стенд %s объявил доставку (%s), а производитель не собрал объект: %v "+
				"(%s)", stack, want, err, delivery.Census.Summary())
			continue
		}
		if delivery.Name != want {
			t.Errorf("стенд %s: производитель кладёт ConfigMap %q, а под монтирует %q — "+
				"каталог доставки приедет пустым, и снаружи это неотличимо от «модулей нет»",
				stack, delivery.Name, want)
		}
		assertKeysMatchTree(t, stack, delivery, tree)
		assertRenderRoundTrips(t, stack, delivery, tree)
		if census.Manifests == 0 {
			census.Manifests = delivery.Census.Manifests
			census.Bytes = delivery.Census.Bytes
		}
	}

	t.Logf("осмотрено: стендов %d · объявляют доставку %d · манифестов %d · байт %d",
		census.Stacks, census.StacksDeclaring, census.Manifests, census.Bytes)
	if census.Stacks == 0 {
		t.Fatal("стендов не прочитано ни одного — вердикт беспредметен")
	}
	if census.StacksDeclaring == 0 {
		t.Fatal("доставку не объявляет НИ ОДИН стенд — производителя некому звать, " +
			"и проверка зеленела бы на дереве, где его нет вовсе (kacho#1901)")
	}
}

// assertKeysMatchTree — ключи объекта и манифесты дерева соответствуют ОДИН К
// ОДНОМУ, и тела побайтово те же.
//
// Обе стороны, а не одна: перечень, покрывающий дерево не полностью, доставил бы
// часть модулей молча — а это ровно тот класс, ради которого доставка заводилась.
func assertKeysMatchTree(t *testing.T, stack string, d modulemanifests.Delivery, tree map[string][]byte) {
	t.Helper()
	seen := map[string]bool{}
	for _, s := range d.Sources {
		seen[s.Dir] = true
		body, ok := tree[s.Dir]
		if !ok {
			t.Errorf("стенд %s: производитель кладёт ключ %q, которому в дереве нет манифеста",
				stack, s.Key())
			continue
		}
		if string(s.Body) != string(body) {
			t.Errorf("стенд %s: тело ключа %q разошлось с %s — доставляется не то, что в дереве",
				stack, s.Key(), s.Path)
		}
	}
	for dir := range tree {
		if !seen[dir] {
			t.Errorf("стенд %s: манифест services/%s/manifest.yaml до службы не доезжает — "+
				"производитель его не кладёт, и молчит об этом", stack, dir)
		}
	}
}

// assertRenderRoundTrips — «собрал» обязано означать «применимо»: напечатанный
// объект разбирается обратно в те же ключи и те же байты.
func assertRenderRoundTrips(t *testing.T, stack string, d modulemanifests.Delivery, tree map[string][]byte) {
	t.Helper()
	out, err := modulemanifests.Render(d)
	if err != nil {
		t.Errorf("стенд %s: объект не собран: %v", stack, err)
		return
	}
	var back struct {
		APIVersion string                `yaml:"apiVersion"`
		Kind       string                `yaml:"kind"`
		Metadata   struct{ Name string } `yaml:"metadata"`
		Data       map[string]string     `yaml:"data"`
	}
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Errorf("стенд %s: напечатанный объект не разбирается обратно: %v — «собрал» "+
			"обязано означать «применимо», а не «похоже»", stack, err)
		return
	}
	if back.APIVersion != "v1" || back.Kind != "ConfigMap" || back.Metadata.Name != d.Name {
		t.Errorf("стенд %s: объект не является ConfigMap %q: apiVersion=%q kind=%q name=%q",
			stack, d.Name, back.APIVersion, back.Kind, back.Metadata.Name)
	}
	if len(back.Data) != len(tree) {
		t.Errorf("стенд %s: ключей в объекте %d при манифестах дерева %d",
			stack, len(back.Data), len(tree))
	}
	for dir, body := range tree {
		got, ok := back.Data[dir+".manifest.yaml"]
		if !ok {
			t.Errorf("стенд %s: в напечатанном объекте нет ключа %q", stack, dir+".manifest.yaml")
			continue
		}
		if got != string(body) {
			t.Errorf("стенд %s: ключ %q после печати и разбора разошёлся с деревом — "+
				"доставится не то, что в дереве", stack, dir+".manifest.yaml")
		}
	}
}

// TestStandBringUpCallsTheManifestProducer — производитель ЗОВЁТСЯ подъёмом
// стенда.
//
// Без этого звена производитель существует и не исполняется: под монтирует
// объект, которого никто не создал, каталог доставки приезжает пустым, а служба
// отказывается стартовать — то есть цель сборки есть, а стенд не поднимается.
//
// Судится ИСПОЛНЯЕМАЯ часть: строки-комментарии снимаются до поиска, иначе
// проверка зачла бы за исполнение собственное объяснение — об этом шаге в том же
// файле написана проза, и она называет цель дословно.
func TestStandBringUpCallsTheManifestProducer(t *testing.T) {
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан: %v — непрочитанное есть НАХОДКА", err)
	}
	findings, seen := auditStandBringUpCalls(string(raw))
	t.Logf("осмотрено: байт Makefile %d · после снятия комментариев %d · целей подъёма %d",
		len(raw), seen.BytesJudged, seen.Targets)
	if seen.BytesJudged == 0 {
		t.Fatal("после снятия комментариев в deploy/Makefile не осталось ни строки — " +
			"вердикт беспредметен")
	}
	if seen.Targets == 0 {
		t.Fatalf("рецептов целей подъёма стенда не найдено ни одного — предпосылка " +
			"проверки исчезла, а не Makefile стал чистым")
	}
	for _, f := range findings {
		t.Errorf("доставка манифестов: %s", f)
	}
}

// manifestProducerTarget — цель сборки, кладущая ConfigMap на стенд.
const manifestProducerTarget = "module-manifests-configmap"

// standBringUpTargets — цели, поднимающие стенд. Обе, а не только `dev-up`:
// `stack-up` поднимает ЛЮБОЙ стенд из таблицы, включая объявившие доставку, и
// без вызова производителя такой стенд не поднимется.
var standBringUpTargets = []string{"dev-up", "stack-up"}

// bringUpCensus — объём осмотренного.
type bringUpCensus struct {
	BytesJudged int
	Targets     int
}

// auditStandBringUpCalls — находки по рецептам подъёма стенда. Функция ЧИСТАЯ:
// инъекция подаёт ей изменённый текст, не трогая дерева.
func auditStandBringUpCalls(makefile string) ([]string, bringUpCensus) {
	recipes := makefileExecutablePart(makefile)
	census := bringUpCensus{BytesJudged: len(strings.TrimSpace(recipes))}

	var findings []string
	for _, target := range standBringUpTargets {
		body := makefileRecipe(recipes, target+":")
		if body == "" {
			continue
		}
		census.Targets++
		if !strings.Contains(body, manifestProducerTarget) {
			findings = append(findings, fmt.Sprintf(
				"цель %s не зовёт %s — стенд поднимется с пустым каталогом доставки, "+
					"и служба откажется стартовать (kacho#1901)", target, manifestProducerTarget))
		}
	}
	sort.Strings(findings)
	return findings, census
}

// makefileExecutablePart — Makefile без строк-комментариев.
func makefileExecutablePart(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// makefileRecipe — тело цели: строки, начинающиеся с табуляции, до первой
// строки, которая рецепт не продолжает.
func makefileRecipe(makefile, target string) string {
	lines := strings.Split(makefile, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, target) {
			continue
		}
		var body []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			if !strings.HasPrefix(next, "\t") {
				break
			}
			body = append(body, next)
		}
		return strings.Join(body, "\n")
	}
	return ""
}
