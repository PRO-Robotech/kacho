// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// prodbinarycontainerclient_injection_test.go — доказательство, что гейт соседнего
// файла СПОСОБЕН упасть и способен смолчать.
//
// Гейт, чья способность упасть не проверена, не проверен: на чистом дереве
// покрасневший и разучившийся краснеть выглядят одинаково. Поэтому судья
// (`auditProdBinaryContainerClients`) вынесен из тела теста и здесь ему подаются
// записи, СОБРАННЫЕ ИЗ ЭТОГО ЖЕ ДЕРЕВА: настоящий бинарь и настоящий путь
// зависимости, взятый у настоящего её носителя. Выдуманная строка доказала бы
// только то, что `strings.Contains` работает.
//
// Инъекция снимает РОВНО проверяемое свойство: у существующего бинаря появляется
// зависимость. Ни один другой контроль дерева при этом не нарушается, поэтому
// красное приходит от нового гейта, а не от соседа.
package repohygiene

import (
	"strings"
	"testing"
)

// realTreeSamples — настоящий бинарь и настоящая зависимость контейнерного клиента,
// взятые из этого дерева. Вход инъекции обязан быть настоящим.
func realTreeSamples(t *testing.T) (binary listedPackage, containerDep string, carrier listedPackage) {
	t.Helper()

	pkgs, err := listPackagesWithDeps(repoRoot(t))
	if err != nil {
		t.Fatalf("вход не получен, вердикта нет: %v", err)
	}

	for _, p := range pkgs {
		if p.Name == "main" && binary.ImportPath == "" {
			binary = p
		}
		// Законный носитель: библиотека проб, которой контейнеры нужны по существу.
		if p.ImportPath == "github.com/PRO-Robotech/kacho/pkg/pgtest" {
			carrier = p
			for _, d := range p.Deps {
				if strings.Contains(d, "testcontainers") {
					containerDep = d
					break
				}
			}
		}
	}

	if binary.ImportPath == "" {
		t.Fatal("в дереве не нашлось ни одного пакета main — инъекции не на чем стоять")
	}
	if containerDep == "" || carrier.ImportPath == "" {
		t.Fatal("в дереве не нашлось законного носителя контейнерной зависимости — " +
			"предпосылка пробы не выполнена")
	}
	if carrier.Name == "main" {
		t.Fatal("законный близнец оказался бинарём — он не близнец")
	}
	return binary, containerDep, carrier
}

func TestProdBinaryContainerClientGateCutsBothWays(t *testing.T) {
	binary, containerDep, carrier := realTreeSamples(t)
	t.Logf("вход инъекции взят из дерева: бинарь %s · зависимость %s · законный носитель %s",
		binary.ImportPath, containerDep, carrier.ImportPath)

	t.Run("контроль: бинарь как есть — судья молчит", func(t *testing.T) {
		findings, census := auditProdBinaryContainerClients([]listedPackage{binary})
		if len(findings) != 0 {
			t.Fatalf("судья нашёл на нетронутом бинаре: %v", findings)
		}
		if census.BinariesSeen != 1 {
			t.Fatalf("перепись обязана была осмотреть 1 бинарь, осмотрела %d", census.BinariesSeen)
		}
	})

	t.Run("инъекция: зависимость возвращена бинарю — краснеет и НАЗЫВАЕТ его", func(t *testing.T) {
		hurt := binary
		hurt.Deps = append(append([]string{}, binary.Deps...), containerDep)

		findings, census := auditProdBinaryContainerClients([]listedPackage{hurt})
		if len(findings) != 1 {
			t.Fatalf("инъекция не поймана: находок %d (перепись: %s)", len(findings), census)
		}
		if findings[0].Binary != binary.ImportPath {
			t.Fatalf("находка не назвала виновника: %q вместо %q", findings[0].Binary, binary.ImportPath)
		}
		// Диагностика — часть свойства: находка, не называющая зависимость,
		// посылает читателя искать не там.
		if !strings.Contains(findings[0].String(), containerDep) {
			t.Fatalf("находка не назвала зависимость: %q", findings[0].String())
		}
	})

	t.Run("законный близнец: та же зависимость у библиотеки проб — судья молчит", func(t *testing.T) {
		findings, census := auditProdBinaryContainerClients([]listedPackage{carrier})
		if len(findings) != 0 {
			t.Fatalf("судья покраснел на законном носителе %s: %v", carrier.ImportPath, findings)
		}
		if census.BinariesSeen != 0 {
			t.Fatalf("библиотека проб не бинарь, а перепись насчитала %d", census.BinariesSeen)
		}
	})

	t.Run("послабление собственного модуля доказано в ОБЕ стороны", func(t *testing.T) {
		own := listedPackage{
			ImportPath: "github.com/PRO-Robotech/kacho/services/x/cmd/x",
			Name:       "main",
			Deps:       []string{ownModulePrefix + "internal/dockerfilegen"},
		}
		findings, census := auditProdBinaryContainerClients([]listedPackage{own})
		if len(findings) != 0 {
			t.Fatalf("собственный пакет с токеном в имени принят за клиент среды: %v", findings)
		}
		if census.OwnMatchesSkipped != 1 {
			t.Fatalf("послабление не отчиталось: пропущено %d, ожидалось 1", census.OwnMatchesSkipped)
		}

		// Зеркало: тот же хвост пути, но ЧУЖОЙ модуль — обязан быть находкой.
		// Без него послабление означало бы «не проверяем» вместо «проверяем уже».
		foreign := listedPackage{
			ImportPath: "github.com/PRO-Robotech/kacho/services/x/cmd/x",
			Name:       "main",
			Deps:       []string{"github.com/someoneelse/internal/dockerfilegen"},
		}
		if f, _ := auditProdBinaryContainerClients([]listedPackage{foreign}); len(f) != 1 {
			t.Fatalf("послабление протекло на чужой модуль: находок %d", len(f))
		}
	})

	t.Run("пустой вход: перепись говорит ноль, а не молчит успехом", func(t *testing.T) {
		findings, census := auditProdBinaryContainerClients(nil)
		if len(findings) != 0 {
			t.Fatalf("на пустом входе появились находки: %v", findings)
		}
		// Именно это число боевой тест читает как отказ — «ноль находок» обязано
		// быть отличимо от «ноль прочитанного».
		if census.PackagesListed != 0 || census.BinariesSeen != 0 {
			t.Fatalf("перепись пустого входа не нулевая: %s", census)
		}
	})
}
