// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

// kindmodel_test.go — у каждого опубликованного вида есть тип в канонической
// модели прав, и этот тип несёт отношение видимости `v_get`.
//
// # Предмет
//
// Строка журнала доставляется подписчику только после вопроса модели «вправе
// ли он видеть этот объект» — тип объекта для вопроса берётся из
// `Mapping.Kinds`. Тип, которого модель не объявляет, или тип без `v_get`
// делают вопрос невыполнимым: сужатель отказывает на каждой строке вида, и
// поток этого вида молчит вечно, оставаясь «зелёным».
//
// Прежде на этом месте стояла проба основания ИСКЛЮЧЕНИЯ пула адресов из
// словаря (#1494). Исключение снято (NTF-3, Р2: пул опубликован видом уровня
// кластера), и проба снята вместе с ним; её положительная сторона — «тип пула
// в модели есть» — стала частным случаем этой пробы.
//
// Тип пула помечен в модели спящим (`# kacho:latent`): объектов пула у
// владельца прав сегодня не регистрирует никто. Проба это печатает, а не
// прощает и не судит — пометка говорит о производителе кортежей, а не о
// существовании типа.
//
// # Самоистечение
//
// Проба привязана к словарю видов, а не к перечню: вид, добавленный в словарь,
// попадает под неё сам; модель, снявшая тип, краснит её с именем вида.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// canonicalModelRel — каноническая модель прав, названная путём ОТНОСИТЕЛЬНО
// `proto/`: контракты службы доступа приезжают модулем
// `github.com/PRO-Robotech/kaname` (kacho#2616, исход C); координату разрешает
// `internal/contractsource`.
const canonicalModelRel = "kaname/cloud/iam/v1/fga_model.fga"

// repoRootFromPackage — корень дерева относительно каталога этого пакета.
const repoRootFromPackage = serviceRoot + "/../.."

// latentTypeMarker — пометка спящего типа в модели (написание держит
// `internal/repohygiene/latentmarkerspelling_test.go`).
const latentTypeMarker = "# kacho:latent"

// modelTypeBlock — тело объявления `type <name>` модели до следующего `type`.
func modelTypeBlock(model, name string) (string, bool) {
	head := "type " + name
	idx := strings.Index(model, "\n"+head+"\n")
	if idx < 0 {
		return "", false
	}
	block := model[idx+1:]
	if next := strings.Index(block[len(head):], "\ntype "); next >= 0 {
		block = block[:len(head)+next]
	}
	return block, true
}

// TestEveryPublishedKindHasAVisibleTypeInTheAuthzModel — тип объекта каждого
// вида словаря объявлен канонической моделью и несёт `v_get`.
func TestEveryPublishedKindHasAVisibleTypeInTheAuthzModel(t *testing.T) {
	path, err := contractsource.Path(repoRootFromPackage, canonicalModelRel)
	if err != nil {
		t.Fatalf("каноническая модель не разрешается: %v — без неё проба судила бы о "+
			"признаке, которого не измеряла", err)
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("каноническая модель не прочитана (%s): %v", path, err)
	}
	model := string(raw)

	kinds := Journal(false).Mapping.Kinds
	if len(kinds) == 0 {
		t.Fatal("словарь видов пуст — судить нечего")
	}
	words := make([]string, 0, len(kinds))
	for w := range kinds {
		words = append(words, w)
	}
	sort.Strings(words)

	var latent []string
	for _, w := range words {
		objectType := kinds[w].ObjectType
		block, ok := modelTypeBlock(model, objectType)
		if !ok {
			t.Errorf("вид %s едет типом %q, которого каноническая модель НЕ объявляет: "+
				"вопрос видимости о нём невыполним, и поток вида молчал бы вечно", w, objectType)
			continue
		}
		if !strings.Contains(block, "define v_get") {
			t.Errorf("вид %s: тип %q объявлен без отношения `v_get` — сужать строки вида не по чему",
				w, objectType)
		}
		if strings.Contains(block, latentTypeMarker) {
			latent = append(latent, objectType)
		}
	}
	t.Logf("перепись: видов %d, модель прочитана (%d байт); помечены спящими: %v",
		len(words), len(raw), latent)
}
