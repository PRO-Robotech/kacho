// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// contractpublisher_injection_test.go — доказательство падучести двух судей
// contractpublisher.go. Каждая проба снимает РОВНО ОДНО свойство фикстуры и
// требует РОВНО ОДНУ находку; рядом с каждым отрицанием стоит положительный
// близнец, отличающийся ровно тем же одним фактом.
//
// Фикстура синтетическая: настоящее дерево судят пробы contractpublisher_test.go,
// и повторять их здесь значило бы проверять дерево вместо механизма.

func publisherFixture() []contractPublication {
	return []contractPublication{
		{Module: "github.com/PRO-Robotech/kacho", Paths: []string{
			"kacho/cloud/vpc/v1/network.proto",
			"kacho/cloud/compute/v1/instance.proto",
		}},
		{Module: "github.com/PRO-Robotech/kaname", Paths: []string{
			"kaname/cloud/iam/v1/account.proto",
		}},
		{Module: "github.com/PRO-Robotech/corelib", Paths: []string{
			"corelib/operation/operation.proto",
		}},
	}
}

func TestPublisherJudgeIsSilentWhenEveryPathHasOneHome(t *testing.T) {
	t.Parallel()

	faults, census := AuditContractPublishers(publisherFixture(), 264)

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на единственных публикаторах (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if census.Modules != 3 || census.Paths != 4 || census.FilesRead != 264 || census.Duplicate != 0 {
		t.Fatalf("перепись контроля не сошлась: модулей %d, путей %d, файлов %d, дублей %d",
			census.Modules, census.Paths, census.FilesRead, census.Duplicate)
	}
}

// TestPublisherJudgeCatchesTheSamePathPublishedTwice — ИМЕННО тот дефект, ради
// которого судья заведён: второй публикатор одного пути.
//
// Фикстура воспроизводит настоящий случай — путь контракта службы, оставленный в
// дереве платформы после переезда. Сообщение настоящей паники процитировано в
// шапке судьи; здесь проверяется, что находка называет ОБА модуля.
func TestPublisherJudgeCatchesTheSamePathPublishedTwice(t *testing.T) {
	t.Parallel()
	pubs := publisherFixture()
	pubs[0].Paths = append(pubs[0].Paths, "kaname/cloud/iam/v1/account.proto")

	faults, census := AuditContractPublishers(pubs, 265)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	for _, want := range []string{
		"kaname/cloud/iam/v1/account.proto",
		"github.com/PRO-Robotech/kacho",
		"github.com/PRO-Robotech/kaname",
		"already registered",
	} {
		if !strings.Contains(faults[0], want) {
			t.Fatalf("находка не называет %q: %s", want, faults[0])
		}
	}
	if census.Duplicate != 1 {
		t.Fatalf("перепись обязана назвать один дубль, а не %d", census.Duplicate)
	}
}

// TestPublisherJudgeIgnoresARepeatedPathInsideOneModule — законный близнец
// предыдущей: тот же путь дважды, но у ОДНОГО модуля. Это не дубль публикатора —
// три заглушки одного контракта (`x.pb.go`, `x_grpc.pb.go`, `x.pb.gw.go`) дают
// один путь по три раза, и краснеть на этом значило бы краснеть на каждом файле
// дерева.
func TestPublisherJudgeIgnoresARepeatedPathInsideOneModule(t *testing.T) {
	t.Parallel()
	pubs := publisherFixture()
	pubs[1].Paths = append(pubs[1].Paths,
		"kaname/cloud/iam/v1/account.proto", "kaname/cloud/iam/v1/account.proto")

	faults, census := AuditContractPublishers(pubs, 264)

	if len(faults) != 0 {
		t.Fatalf("повтор пути ВНУТРИ модуля не есть дубль публикатора: %v", faults)
	}
	if census.Paths != 4 {
		t.Fatalf("путей обязано остаться 4 (повтор дедуплицирован), а не %d", census.Paths)
	}
}

func TestPublisherJudgeRefusesASingleModuleInsteadOfReportingNoFindings(t *testing.T) {
	t.Parallel()
	pubs := publisherFixture()[:1]

	faults, _ := AuditContractPublishers(pubs, 165)

	if len(faults) != 1 || !strings.Contains(faults[0], "МЕЖДУ модулями") {
		t.Fatalf("один модуль обязан быть ОТКАЗОМ, а не «дублей нет»: %v", faults)
	}
}

// TestPublisherJudgeRefusesAModuleThatPublishedNothing — молчащий ноль у одного
// из трёх деревьев. Отличается от предыдущей ровно одним фактом: модулей три, а
// путей у одного из них ноль.
func TestPublisherJudgeRefusesAModuleThatPublishedNothing(t *testing.T) {
	t.Parallel()
	pubs := publisherFixture()
	pubs[1].Paths = nil

	faults, _ := AuditContractPublishers(pubs, 177)

	if len(faults) != 1 {
		t.Fatalf("ожидался ровно один отказ, получено %d: %v", len(faults), faults)
	}
	if !strings.Contains(faults[0], "github.com/PRO-Robotech/kaname") ||
		!strings.Contains(faults[0], "в нём не искали") {
		t.Fatalf("отказ не называет ни модуль, ни причину: %s", faults[0])
	}
}

// ---------------------- сверка двух объявлений ----------------------

func rootDeclarationFixture() (map[string]string, map[string]string) {
	fromGo := map[string]string{"kaname": "github.com/PRO-Robotech/kaname"}
	fromShell := map[string]string{"kaname": "github.com/PRO-Robotech/kaname"}
	return fromGo, fromShell
}

func TestRootDeclarationJudgeIsSilentWhenBothSidesAgree(t *testing.T) {
	t.Parallel()
	fromGo, fromShell := rootDeclarationFixture()

	faults, census := AuditExternalRootDeclarations(fromGo, fromShell, 8)

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на сошедшихся объявлениях: %v", faults)
	}
	if census.GoRows != 1 || census.ShellRows != 1 || census.Mentions != 8 {
		t.Fatalf("перепись контроля не сошлась: %s", census.String())
	}
}

// TestRootDeclarationJudgeCatchesARootKnownOnlyToGo — сужение популяции у
// генераторов края: они собрали бы вход БЕЗ дерева службы и вышли бы успехом.
// Измеренная цена такого молчания — «OK: 233 entries» вместо 350.
func TestRootDeclarationJudgeCatchesARootKnownOnlyToGo(t *testing.T) {
	t.Parallel()
	fromGo, _ := rootDeclarationFixture()

	faults, _ := AuditExternalRootDeclarations(fromGo, map[string]string{"other": "example.com/other"}, 8)

	if len(faults) != 2 {
		t.Fatalf("ожидались две находки (корень только в Go и корень только в оболочке), "+
			"получено %d: %v", len(faults), faults)
	}
	joined := strings.Join(faults, "\n")
	if !strings.Contains(joined, "в оболочке — нет") || !strings.Contains(joined, "в Go — нет") {
		t.Fatalf("находки не называют обе стороны расхождения: %s", joined)
	}
}

// TestRootDeclarationJudgeCatchesTheSameRootTakenFromDifferentModules —
// законный близнец: корень известен обеим сторонам, но модуль назван разный.
func TestRootDeclarationJudgeCatchesTheSameRootTakenFromDifferentModules(t *testing.T) {
	t.Parallel()
	fromGo, fromShell := rootDeclarationFixture()
	fromShell["kaname"] = "github.com/PRO-Robotech/kaname-old"

	faults, _ := AuditExternalRootDeclarations(fromGo, fromShell, 8)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(faults), faults)
	}
	if !strings.Contains(faults[0], "РАЗНЫХ модулей") ||
		!strings.Contains(faults[0], "kaname-old") {
		t.Fatalf("находка не называет расхождение координат: %s", faults[0])
	}
}

func TestRootDeclarationJudgeRefusesAnEmptyShellSide(t *testing.T) {
	t.Parallel()
	fromGo, _ := rootDeclarationFixture()

	faults, _ := AuditExternalRootDeclarations(fromGo, nil, 8)

	if len(faults) != 1 || !strings.Contains(faults[0], "не распознан") {
		t.Fatalf("пустая сторона оболочки обязана быть ОТКАЗОМ: %v", faults)
	}
}

func TestRootDeclarationJudgeRefusesAnEmptyGoSide(t *testing.T) {
	t.Parallel()
	_, fromShell := rootDeclarationFixture()

	faults, _ := AuditExternalRootDeclarations(nil, fromShell, 8)

	if len(faults) != 1 || !strings.Contains(faults[0], "перечень внешних корней в Go пуст") {
		t.Fatalf("пустая сторона Go обязана быть ОТКАЗОМ: %v", faults)
	}
}
