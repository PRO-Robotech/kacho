// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/platformmodules"
)

// platformmodulevocabulary_injection_test.go — доказательство способности гейта
// TestPlatformModuleVocabularyMatchesTheTree падать И молчать.
//
// # Прогонов больше двух, и это не педантизм
//
// У гейта ЧЕТЫРЕ независимых утверждения (короткое имя · модуль каталога ·
// непустой домен типов · пустой домен типов) плюс обратная сторона (служба
// дерева без записи). Инъекция обязана ронять ТОЛЬКО проверяемое: красное,
// пришедшее от соседнего утверждения, доказывало бы работу соседа, а новое
// могло бы оказаться вакуумным, не показав этого ничем. Поэтому каждая инъекция
// снимает ОДНО свойство у записи, чьи остальные целы, и проверяется, что находка
// ровно одна и пришла от нужной ветви.
//
// Законный близнец подан контролем: у балансировщика ДВА написания из трёх
// различны (`nlb` / `loadbalancer` / `nlb_*`) — и это законно, гейт обязан
// молчать. Без него сверка «служба == модуль каталога» выглядела бы разумной.

func vocabularyFixture() ([]platformmodules.Module,
	map[string]struct{}, map[string]struct{}, map[string]struct{}) {

	declared := []platformmodules.Module{
		{Service: "vpc", CatalogModule: "vpc", ObjectDomain: "vpc"},
		// законный близнец: три написания расходятся, и это норма
		{Service: "nlb", CatalogModule: "loadbalancer", ObjectDomain: "nlb"},
		// законный близнец: модуль без собственных типов объекта
		{Service: "geo", CatalogModule: "geo", ObjectDomain: ""},
	}
	serviceDirs := map[string]struct{}{"vpc": {}, "nlb": {}, "geo": {}}
	protoDirs := map[string]struct{}{"vpc": {}, "loadbalancer": {}, "geo": {}}
	modelTypes := map[string]struct{}{"vpc_network": {}, "nlb_listener": {}}
	return declared, serviceDirs, protoDirs, modelTypes
}

func TestVocabularyJudgeIsSilentOnADeclarationThatMatchesTheTree(t *testing.T) {
	t.Parallel()
	declared, serviceDirs, protoDirs, modelTypes := vocabularyFixture()

	faults, census := judgePlatformVocabulary(declared, serviceDirs, protoDirs, modelTypes)
	t.Logf("контроль: %s", census.Summary())

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на согласованном словаре (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	// Законные близнецы названы ЧИСЛОМ: три модуля осмотрены, домен типов
	// непуст у двух — то есть запись geo дошла до разбора и находкой не стала.
	if census.Modules != 3 || census.WithObjectDomain != 2 {
		t.Fatalf("перепись контроля не сошлась: модулей %d, с доменом типов %d, "+
			"ожидалось 3 и 2", census.Modules, census.WithObjectDomain)
	}
}

func TestVocabularyJudgeCatchesAServiceNameWithNoDirectory(t *testing.T) {
	t.Parallel()
	declared, serviceDirs, protoDirs, modelTypes := vocabularyFixture()
	declared[0].Service = "vpcs" // каталога services/vpcs нет

	faults, _ := judgePlatformVocabulary(declared, serviceDirs, protoDirs, modelTypes)

	// Находок ДВЕ и обе от одной инъекции: запись называет несуществующий
	// каталог, а каталог `vpc` остался без записи. Это и есть двусторонность.
	if len(faults) != 2 {
		t.Fatalf("ожидалось две находки одной инъекции, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	joined := strings.Join(faults, "\n")
	if !strings.Contains(joined, "services/vpcs") || !strings.Contains(joined, "services/vpc ") {
		t.Fatalf("находки не называют обе стороны: %s", joined)
	}
}

// TestVocabularyJudgeReadsTheExternalSourcesLedgerBothWays — ведомость вынесенных
// модулей самоистекает в обе стороны, и обе оси подаются ЯВНО.
//
// Без первой половины гейт краснел бы на верно исполненном разрезе и толкал снять
// запись из словаря — то есть сломать опубликованный фундамент, который эту
// запись читает на пути запроса. Без второй половины запись не истекала бы
// никогда: вернувшаяся служба осталась бы прикрытой.
func TestVocabularyJudgeReadsTheExternalSourcesLedgerBothWays(t *testing.T) {
	t.Parallel()

	// ОСЬ 1 — законный близнец разреза: каталога службы нет, вынос ОБЪЯВЛЕН.
	declared, serviceDirs, protoDirs, modelTypes := vocabularyFixture()
	gone := declared[0].Service
	delete(serviceDirs, gone)
	faults, census := judgePlatformVocabularyWithExternal(declared, serviceDirs, protoDirs,
		modelTypes, map[string]string{gone: "PRO-Robotech/kaname"})
	if len(faults) != 0 {
		t.Fatalf("объявленный вынос дал находки (%d) — гейт краснеет на верной работе:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if census.SourcesElsewhere != 1 {
		t.Fatalf("перепись не назвала вынесенный модуль числом: получено %d, ожидалось 1",
			census.SourcesElsewhere)
	}

	// ОСЬ 2 — тот же мир, но вынос НЕ объявлен: находка о несуществующем каталоге.
	faults, _ = judgePlatformVocabularyWithExternal(declared, serviceDirs, protoDirs,
		modelTypes, map[string]string{})
	if len(faults) != 1 || !strings.Contains(faults[0], "services/"+gone) {
		t.Fatalf("необъявленный вынос не назван координатой: %v", faults)
	}

	// ОСЬ 3 — обратная: вынос объявлен, а каталог службы В ДЕРЕВЕ ЕСТЬ.
	declared, serviceDirs, protoDirs, modelTypes = vocabularyFixture()
	back := declared[0].Service
	faults, _ = judgePlatformVocabularyWithExternal(declared, serviceDirs, protoDirs,
		modelTypes, map[string]string{back: "PRO-Robotech/kaname"})
	if len(faults) != 1 || !strings.Contains(faults[0], "пережило свой предмет") {
		t.Fatalf("вернувшаяся служба не названа находкой: %v", faults)
	}
}

func TestVocabularyJudgeCatchesACatalogModuleWithNoProtoDirectory(t *testing.T) {
	t.Parallel()
	declared, serviceDirs, protoDirs, modelTypes := vocabularyFixture()
	declared[1].CatalogModule = "nlb" // наивная сверка «служба == модуль каталога»

	faults, _ := judgePlatformVocabulary(declared, serviceDirs, protoDirs, modelTypes)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "proto/kacho/cloud/nlb") {
		t.Fatalf("находка пришла не от той ветви: %s", faults[0])
	}
}

func TestVocabularyJudgeCatchesAnObjectDomainTheModelDoesNotDeclare(t *testing.T) {
	t.Parallel()
	declared, serviceDirs, protoDirs, modelTypes := vocabularyFixture()
	declared[1].ObjectDomain = "loadbalancer" // типы модели зовутся `nlb_*`

	faults, census := judgePlatformVocabulary(declared, serviceDirs, protoDirs, modelTypes)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "loadbalancer_") {
		t.Fatalf("находка пришла не от той ветви: %s", faults[0])
	}
	if census.WithObjectDomain != 1 {
		t.Fatalf("перепись зачла негодную колонку в осмотренные: с доменом типов %d",
			census.WithObjectDomain)
	}
}

// TestVocabularyJudgeCatchesAnEmptyObjectDomainThatIsNotTrue — пустая строка не
// должна становиться способом снять проверку с колонки.
func TestVocabularyJudgeCatchesAnEmptyObjectDomainThatIsNotTrue(t *testing.T) {
	t.Parallel()
	declared, serviceDirs, protoDirs, modelTypes := vocabularyFixture()
	modelTypes["geo_region"] = struct{}{} // у geo появился свой тип

	faults, _ := judgePlatformVocabulary(declared, serviceDirs, protoDirs, modelTypes)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "geo_region") {
		t.Fatalf("находка не называет тип, опровергающий пустую колонку: %s", faults[0])
	}
}

func TestVocabularyJudgeCatchesAServiceOfTheTreeWithNoEntry(t *testing.T) {
	t.Parallel()
	declared, serviceDirs, protoDirs, modelTypes := vocabularyFixture()
	serviceDirs["storage"] = struct{}{} // служба заведена, записи нет

	faults, _ := judgePlatformVocabulary(declared, serviceDirs, protoDirs, modelTypes)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "services/storage") {
		t.Fatalf("находка пришла не от той ветви: %s", faults[0])
	}
}
