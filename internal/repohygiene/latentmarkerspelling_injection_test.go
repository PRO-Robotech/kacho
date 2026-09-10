// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// latentmarkerspelling_injection_test.go — доказательство, что соседний гейт
// СПОСОБЕН упасть и способен смолчать.
//
// Вход подаётся СИНТЕТИЧЕСКИМ деревом во временном каталоге: настоящее дерево
// не правится, поэтому доказательство не зависит от того, что в нём сегодня
// лежит. Обратная сторона названа честно — синтетика не говорит НИЧЕГО о том,
// производится ли пометка ещё; это утверждает сам гейт, падая на пустом обходе.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLatentSynthetic раскладывает файлы во временном каталоге и отдаёт его состав.
func writeLatentSynthetic(t *testing.T, files map[string]string) *trackedTree {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("каталог %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", full, err)
		}
	}
	return newSyntheticTree(t, dir)
}

// latentMarkerOf собирает написание пометки из частей — иначе этот файл сам стал бы
// вхождением и попал бы в перепись настоящего дерева.
func latentMarkerOf(word string) string { return "# " + word + ":latent" }

// TestLatentMarkerSpellingGateCutsBothWays — четыре прогона: контроль, инъекция
// расхождения, инъекция пустоты и законный близнец.
func TestLatentMarkerSpellingGateCutsBothWays(t *testing.T) {
	t.Parallel()

	// ─── 1. КОНТРОЛЬ: все стороны согласны → гейт молчит ────────────────────
	t.Run("контроль: одно написание на всех сторонах", func(t *testing.T) {
		t.Parallel()
		tt := writeLatentSynthetic(t, map[string]string{
			"proto/model.fga":             "type vpc_address_pool\n  relations\n    " + latentMarkerOf("kacho") + " — причина\n    define cluster: [cluster]\n",
			"services/vpc/manifest.yaml":  "          " + latentMarkerOf("kacho") + " — причина\n",
			"internal/gate_test.go":       "const latentMarker = \"" + latentMarkerOf("kacho") + "\"\n",
			"services/iam/reader_test.go": "const latentTypeMarker = \"" + latentMarkerOf("kacho") + "\"\n",
		})
		hits, filesRead, filesWith := collectLatentSpellings(t, tt)
		t.Logf("перепись: %s", latentCensus(hits, filesRead, filesWith))
		if len(hits) != 4 {
			t.Fatalf("контроль: вхождений %d, ожидалось 4 — распознаватель не видит одной из форм", len(hits))
		}
		if v := latentSpellingVerdict(hits, filesRead); v != "" {
			t.Errorf("контроль обязан молчать, а гейт сказал:\n%s", v)
		}
	})

	// ─── 2. ИНЪЕКЦИЯ РАСХОЖДЕНИЯ: переехала ОДНА сторона ────────────────────
	// Меняется РОВНО ОДИН факт против контроля — написание у одного
	// распознавателя. Остальные три файла побайтово те же.
	t.Run("инъекция: распознаватель службы переехал один", func(t *testing.T) {
		t.Parallel()
		tt := writeLatentSynthetic(t, map[string]string{
			"proto/model.fga":             "type vpc_address_pool\n  relations\n    " + latentMarkerOf("kacho") + " — причина\n    define cluster: [cluster]\n",
			"services/vpc/manifest.yaml":  "          " + latentMarkerOf("kacho") + " — причина\n",
			"internal/gate_test.go":       "const latentMarker = \"" + latentMarkerOf("kacho") + "\"\n",
			"services/iam/reader_test.go": "const latentTypeMarker = \"" + latentMarkerOf("kaname") + "\"\n",
		})
		hits, filesRead, _ := collectLatentSpellings(t, tt)
		v := latentSpellingVerdict(hits, filesRead)
		if v == "" {
			t.Fatal("односторонний переезд обязан краснеть — гейт смолчал")
		}
		// Находка обязана НАЗЫВАТЬ координату, а не только факт расхождения:
		// находка, называющая симптом, посылает читателя искать не там.
		if !strings.Contains(v, "services/iam/reader_test.go") {
			t.Errorf("находка не называет переехавшую сторону:\n%s", v)
		}
		if !strings.Contains(v, "kaname") || !strings.Contains(v, "kacho") {
			t.Errorf("находка не называет ОБА написания:\n%s", v)
		}
		t.Logf("инъекция дала находку:\n%s", v)
	})

	// ─── 3. ИНЪЕКЦИЯ ПУСТОТЫ: предмет исчез ─────────────────────────────────
	// Пометки нет ни в одном файле. Это НЕ успех: гейт, которому нечего
	// читать, обязан сказать это отдельным текстом, а не смолчать.
	t.Run("инъекция: предмет исчез — обход не пуст, а пометки нет", func(t *testing.T) {
		t.Parallel()
		tt := writeLatentSynthetic(t, map[string]string{
			"proto/model.fga": "type vpc_address_pool\n  relations\n    define cluster: [cluster]\n",
		})
		hits, filesRead, _ := collectLatentSpellings(t, tt)
		v := latentSpellingVerdict(hits, filesRead)
		if v == "" {
			t.Fatal("исчезнувший предмет обязан краснеть — иначе гейт переживёт то, что стерёг")
		}
		if !strings.Contains(v, "не найдена НИ РАЗУ") {
			t.Errorf("текст обязан отличать «предмета нет» от расхождения написаний:\n%s", v)
		}
		t.Logf("инъекция пустоты дала находку:\n%s", v)
	})

	// ─── 4. ЗАКОННЫЙ БЛИЗНЕЦ: слово есть, пометки нет ───────────────────────
	// Без него отрицание зеленело бы на чём угодно: надо доказать, что гейт
	// ловит ПОМЕТКУ, а не слово «latent».
	t.Run("законный близнец: слово latent без пометки — молчание", func(t *testing.T) {
		t.Parallel()
		tt := writeLatentSynthetic(t, map[string]string{
			"internal/gate_test.go": "const latentMarker = \"" + latentMarkerOf("kacho") + "\"\n",
			"docs/prose.md":         "Спящее отношение (latent relation) объявляется заранее.\n// latent: yes\nlatentTypeMarker\n",
		})
		hits, filesRead, filesWith := collectLatentSpellings(t, tt)
		if filesWith != 1 {
			t.Fatalf("файлов с пометкой %d, ожидался 1 — гейт ловит слово, а не пометку", filesWith)
		}
		if v := latentSpellingVerdict(hits, filesRead); v != "" {
			t.Errorf("законный близнец обязан молчать, а гейт сказал:\n%s", v)
		}
	})
}
