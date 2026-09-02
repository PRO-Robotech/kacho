// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestAcceptanceDoesNotDenyALiveManifestKey — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`manifestkeydenial_injection_test.go`): здесь только вердикт.
//
// Приёмка пинит ревизию, поэтому её утверждение — замер, а не ложь, и правится
// оно НЕ переписыванием чужого круга: круг обязан остаться тем, что читал
// рецензент. Обязателен МАРКЕР СОСТОЯНИЯ — своя ревизия и свой предикат рядом с
// пережившим предмет утверждением.
func TestAcceptanceDoesNotDenyALiveManifestKey(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditManifestKeyDenial(DefaultManifestKeyDenialOptions(repoRoot(t)), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом, и вердикт был бы беспредметен.
	if census.ManifestFiles < 5 {
		t.Fatalf("файлов манифеста %d — объявлять ключи неоткуда, вердикт беспредметен",
			census.ManifestFiles)
	}
	if census.LiveKeys < 20 {
		t.Fatalf("живых ключей %d — словарь ключей не построен", census.LiveKeys)
	}
	if census.DocFiles < 10 {
		t.Fatalf("приёмок %d — обход пуст, судить не о чем", census.DocFiles)
	}
	if census.DocLines < 1000 {
		t.Fatalf("строк приёмок %d — прочитано слишком мало, чтобы вердикт что-то значил",
			census.DocLines)
	}

	// Строк-утверждений НОЛЬ — законное состояние и цель: гейт не вправе падать
	// на достижении собственной цели. Способность распознать утверждение
	// проверяется инъекцией, а не популяцией настоящего дерева.
	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("приёмка утверждает о ключе манифеста то, что дерево опровергает "+
		"(строк-утверждений %d, блоков состояния %d):\n%s\n\n"+
		"Утверждение приёмки — ЗАМЕР на её ревизии, а не ложь, поэтому текст круга "+
		"не переписывают: рецензент читал именно его, и правка сделала бы вердикт "+
		"непрослеживаемым. Рядом дописывается МАРКЕР СОСТОЯНИЯ — блок-цитата, несущая "+
		"слово СОСТОЯНИЕ, ключ в обратных кавычках В ТОЙ ЖЕ СТРОКЕ и ревизию, на которой "+
		"снят новый замер. Обратная сторона держится тем же гейтом: маркер, чей ключ "+
		"манифест больше не несёт, — тоже находка.",
		census.ClaimLines, census.MarkerBlocks, strings.Join(lines, "\n"))
}
