// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_chain_raised_on_the_tree_build_test.go — конвейер поднимает цепочку `own`
// на образах СБОРКИ ЗАПРОСА, а не на опубликованных пинах чужой ревизии.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (kacho#2931)
//
// Слой площадки `values.own-stand.yaml` пинит образы продукта на тегах одной
// опубликованной ревизии — так стенд `own` поднимается на своём кластере.
// Работа конвейера судит ЗАПРОС: поднятый ею стенд обязан исполнять дерево
// запроса, иначе вердикт о посадке относится к другой ревизии, а провенанс
// стенда краснеет на каждом прогоне. Поэтому рецепт подъёма накладывает поверх
// цепочки слой, переводящий образы частей этого дерева на образы сборки
// (`product-names --local-overlay`, решение — internal/localimages).
//
// Здесь держатся два звена, каждое своим утверждением:
//
//  1. НАКЛАДКА ПОКРЫВАЕТ ЦЕПОЧКУ: сложение «умолчания умбреллы + цепочка own +
//     накладка» не оставляет ни одной части этого дерева на опубликованном
//     образе, и каждую из них сборка стенда ПРОИЗВОДИТ (`SERVICES` рецепта и
//     модули консоли с Dockerfile). Пин вынесенной части остаётся пином;
//  2. РЕЦЕПТ ЕЁ НАКЛАДЫВАЕТ ПОСЛЕ ЦЕПОЧКИ: в применении helm слой накладки стоит
//     правее цепочки — иначе пины площадки перебили бы образы сборки молча.
//
// Цель рецепта не выписана: она берётся у ноги конвейера, чья последняя
// цепочка — `own` (stand_chain_is_raised_by_the_conveyor_test.go).
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/localimages"
	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// ownChainName — цепочка таблицы стендов, корень которой — боевой слой и которую
// поднимает конвейер (предикат kacho#1276, раскрытый в kacho#2931).
const ownChainName = "own"

// consoleModuleImages — образы модулей консоли, которые собирает `build-ui`:
// перечень модулей выводится из дерева тем же обходом (`ui-future/*/Dockerfile`),
// имя — формой семейства консоли, той же, что у рецепта.
func consoleModuleImages(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../ui-future/*/Dockerfile")
	if err != nil || len(files) == 0 {
		t.Fatalf("модулей консоли с Dockerfile не найдено (%v) — производителя образов консоли назвать нечем", err)
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, "kacho-ui-future-"+filepath.Base(filepath.Dir(f)))
	}
	sort.Strings(out)
	return out
}

func TestOwnChainOverlayRepointsEveryTreePartAtTheStandBuild(t *testing.T) {
	chain, ok := deployStacks(t)[ownChainName]
	if !ok {
		t.Fatalf("цепочки %q в таблице стендов нет — предмет переехал, вердикт беспредметен", ownChainName)
	}
	folded := localimages.Merge(map[string]any{}, readYAML(t, filepath.Join(umbrellaDir, "values.yaml")))
	for _, p := range chain {
		folded = localimages.Merge(folded, readYAML(t, filepath.Join(umbrellaDir, p)))
	}

	var built []string
	for _, svc := range standRecipeServices(t) {
		built = append(built, productnaming.ChartName(svc))
	}
	built = append(built, consoleModuleImages(t)...)

	overlay, census := localimages.Overlay(folded, built, "dev")
	t.Logf("осмотрено: объявлений образа %d; переписано %d; пинов вынесенных частей %d; сторонних %d; "+
		"карт без репозитория %d; собрано %d, из них цепочкой не объявлено %v",
		census.Declarations, census.TreeParts, census.External, census.Foreign, census.UnknownRepo,
		len(built), census.Unused)
	if len(census.Unbuilt) != 0 {
		t.Errorf("цепочка %s объявляет части этого дерева, которых сборка стенда не производит: %v — "+
			"на конвейере под ушёл бы в ImagePullBackOff после «helm upgrade прошёл»", ownChainName, census.Unbuilt)
	}
	if census.TreeParts == 0 || census.External == 0 {
		t.Fatalf("переписано %d, пинов вынесенных частей %d — обход не узнал цепочку, "+
			"и «покрыто» было бы сказано о непрочитанном", census.TreeParts, census.External)
	}

	final := localimages.Merge(localimages.Merge(map[string]any{}, folded), overlay)
	ours, external := 0, 0
	for _, d := range localimages.Declarations(final) {
		dir, isPart := productnaming.ServiceDir(d.Image)
		if !isPart {
			continue
		}
		where := strings.Join(d.Path, ".")
		if !productnaming.SourcesInThisTree(dir) {
			external++
			if !strings.Contains(d.Repo, "/") {
				t.Errorf("%s: пин вынесенной части стал локальным образом %q — её сборки здесь нет", where, d.Ref())
			}
			continue
		}
		ours++
		if want := d.Image + ":dev"; d.Ref() != want {
			t.Errorf("%s: после накладки часть этого дерева исполняет %q, а не образ сборки %q", where, d.Ref(), want)
		}
	}
	if ours != census.TreeParts {
		t.Errorf("частей этого дерева после сложения %d, переписано %d — накладка и цепочка говорят о разном", ours, census.TreeParts)
	}
	t.Logf("после сложения: частей этого дерева на образе сборки %d, пинов вынесенных частей %d", ours, external)
}

// ownHelmApply — применение helm в рецепте цели: от `helm upgrade` до `--wait`.
var ownHelmApply = regexp.MustCompile(`helm upgrade --install[^\n]*?--wait`)

func TestOwnChainRecipeAppliesTheOverlayAfterTheChain(t *testing.T) {
	var target string
	for _, l := range conveyorLegs(t) {
		if l.OnPullRequest && l.Judged && l.terminal() == ownChainName {
			target = l.Target
		}
	}
	if target == "" {
		t.Fatalf("ноги конвейера, поднимающей цепочку %s, нет — звено 2 судить не о чем "+
			"(предмет держит TestEveryStandChainIsRaisedByTheConveyorOrNamesWhyNot)", ownChainName)
	}
	raw, err := os.ReadFile(standMakefile)
	if err != nil {
		t.Fatalf("%s не читается: %v", standMakefile, err)
	}
	lines := flattenRecipe(parseRecipeTargets(string(raw)), target, map[string]bool{})
	flat := regexp.MustCompile(`\\\n\s*`).ReplaceAllString(strings.Join(lines, "\n"), " ")

	// Производитель накладки — читатель имён оболочки (он зовёт режим
	// `--local-overlay` инструмента, правило второй раз не толкуется).
	if !regexp.MustCompile(`product_local_overlay [^;]*> \./\$\(LOCAL_IMAGES_VALUES\)`).MatchString(flat) {
		t.Errorf("рецепт %s не производит накладку сборки в $(LOCAL_IMAGES_VALUES)", target)
	}
	applies := ownHelmApply.FindAllString(flat, -1)
	judged := 0
	for _, a := range applies {
		chainAt := strings.Index(a, "$(call STACK_ARGS,"+ownChainName+")")
		if chainAt < 0 {
			continue
		}
		judged++
		overlayAt := strings.Index(a, "-f ./$(LOCAL_IMAGES_VALUES)")
		if overlayAt < chainAt {
			t.Errorf("рецепт %s: слой накладки сборки не стоит правее цепочки %s в применении helm — "+
				"пины площадки перебили бы образы сборки молча:\n  %s", target, ownChainName, a)
		}
	}
	if judged == 0 {
		t.Fatalf("в рецепте %s нет применения helm с цепочкой %s — распознаватель не узнал рецепт (%d применений)",
			target, ownChainName, len(applies))
	}
}
