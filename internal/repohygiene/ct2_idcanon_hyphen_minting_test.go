// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// TestEveryHyphenMintedPrefixIsInTheCanon — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`ct2_idcanon_hyphen_minting_injection_test.go`): здесь только вердикт.
func TestEveryHyphenMintedPrefixIsInTheCanon(t *testing.T) {
	opts := DocsIDFormOptions{Root: repoRoot(t), ModulePath: "github.com/PRO-Robotech/kacho"}

	var log strings.Builder
	findings, census, err := AuditHyphenMintedPrefixesInCanon(opts, ids.KnownHyphenPrefixes(), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премисы: прочитано то, что заведомо есть. Без них «ноль находок»
	// достигалось бы пустым обходом — и это был бы ровно тот вид зелёного,
	// который получает проверка, не смотрящая никуда.
	if census.GoFiles < 200 {
		t.Fatalf("исходников Go прочитано %d — дерево не обойдено, чеканку искать негде", census.GoFiles)
	}
	if census.MintedHyphen == 0 {
		t.Fatalf("префиксов, чеканимых дефисной формой, найдено 0 — обход не видит `NewHyphenID`, " +
			"и вердикт вынесен о пустом множестве")
	}
	if census.CanonSize == 0 {
		t.Fatalf("каталог дефисных префиксов пуст — сверять не с чем")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("продукт чеканит дефисной формой префикс, которого нет в каталоге дефисных "+
		"(чеканится %d %v, в каталоге %d):\n%s\n\n"+
		"Следствие: `validate.ResourceID` отвергает идентификатор, который платформа сама и "+
		"произвела, — сегмент до дефиса каталогу неизвестен, а legacy-ветвь берёт первые ТРИ "+
		"знака и совпасть ей не с чем. Пока сервис не зовёт проверку формата, дефект ЛАТЕНТЕН, "+
		"и это худшее его свойство: он проявится у первого, кто поступит по конвенции и "+
		"поставит malformed-id-check первым стейтментом. Исходов два: внести префикс в "+
		"`hyphenFormPrefixes` — либо снять его из дефисной чеканки, и второе есть ломающее "+
		"изменение внешне-адресуемой координаты, если хоть одна строка с таким id уже создана.",
		census.MintedHyphen, census.MintedNames, census.CanonSize, strings.Join(lines, "\n"))
}
