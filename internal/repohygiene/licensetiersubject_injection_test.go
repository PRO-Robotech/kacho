// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// licensetiersubject_injection_test.go — доказательство способности оси
// четвёртой карты лицензий упасть и смолчать.
//
// Ось ловит МЁРТВОЕ ИМЯ: уровень, в который не разрешается ни один путь дерева.
// Вакуумной она становится проще всего — достаточно перестать различать «нуль
// путей» и «путей не считали», и всякий снятый уровень пройдёт молча. Поэтому
// каждая проба меняет РОВНО ОДИН факт против своего законного близнеца, а
// близнец отличается от неё только тем, есть ли под уровнем путь.
//
// Инъекция гоняет ТО ЖЕ суждение, которое исполняется на дереве
// (judgeLicenseTierSubjects), на синтетическом входе: уровни передаются
// параметром именно ради этого — доказательство на копии суждения доказывало бы
// не то, что исполняется.
package repohygiene

import (
	"strings"
	"testing"
)

// injTiers — синтетическая карта: два уровня с приставкой плюс умолчание. Форма
// повторяет живую (умолчание последним, приставки со слэшем), чтобы разница
// между пробой и деревом сводилась к содержимому, а не к устройству входа.
var injTiers = []licenseTier{
	{Prefix: "pkg/", Name: "фундамент-и", SPDX: licenseApache, OwnLicenseFile: true},
	{Prefix: "services/gone/", Name: "вынесенный-и", SPDX: licenseAGPL, OwnLicenseFile: true},
	{Prefix: "", Name: "монорепо-и", SPDX: licenseBUSL, OwnLicenseFile: true},
}

// injTierPaths — перепись путей по уровням синтетической карты. Считается тем же
// разрешением, что и на дереве, но по СВОЕЙ карте: licenseTierFor читает
// глобальную таблицу, а инъекции нужна своя.
func injTierPaths(tiers []licenseTier, paths []string) map[string]int {
	out := map[string]int{}
	for _, rel := range paths {
		best := -1
		for i, t := range tiers {
			if t.Prefix != "" && !strings.HasPrefix(rel, t.Prefix) {
				continue
			}
			if best < 0 || len(t.Prefix) > len(tiers[best].Prefix) {
				best = i
			}
		}
		if best >= 0 {
			out[tiers[best].Name]++
		}
	}
	return out
}

// ── сторона (а): внесённый факт краснеет и называет приставку И уровень ──────

// Несущий случай перехода: каталог вынесенного продукта уехал, а его уровень в
// карте остался. Против законного близнеца ниже мир отличается РОВНО ОДНИМ
// фактом — наличием файла под `services/gone/`.
func TestLicenseTierSubjectGate_RedsWhenATierHasNoPathInTheTree(t *testing.T) {
	t.Parallel()
	paths := []string{"pkg/ids/id.go", "gateway/main.go"} // под services/gone/ — ничего

	faults, census := judgeLicenseTierSubjects(injTiers, injTierPaths(injTiers, paths))

	if len(faults) != 1 {
		t.Fatalf("уровень без предмета не найден: находок %d (%v); перепись: %s",
			len(faults), faults, census)
	}
	// Находка обязана называть ОБЕ координаты: по имени уровня запись ищут в
	// карте, по приставке — предмет в дереве. Одной из двух недостаточно.
	if !strings.Contains(faults[0], "вынесенный-и") || !strings.Contains(faults[0], "services/gone/") {
		t.Fatalf("находка не называет ни уровень, ни приставку: %q", faults[0])
	}
	if census.Judged != 2 || census.Live != 1 {
		t.Fatalf("перепись не разделяет судимое и живое: судимых %d, живых %d", census.Judged, census.Live)
	}
}

// ── сторона (б): ЗАКОННЫЙ БЛИЗНЕЦ той же формы молчит ────────────────────────

// Тот же вход, отличающийся ровно одним фактом: под `services/gone/` появился
// файл. Без этой пробы отрицание выше зеленело бы на суждении, которое краснеет
// на всём подряд.
func TestLicenseTierSubjectGate_SilentWhenEveryTierHasAPath(t *testing.T) {
	t.Parallel()
	paths := []string{"pkg/ids/id.go", "gateway/main.go", "services/gone/cmd/main.go"}

	faults, census := judgeLicenseTierSubjects(injTiers, injTierPaths(injTiers, paths))

	if len(faults) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v", faults)
	}
	if census.Judged != 2 || census.Live != 2 {
		t.Fatalf("перепись не подтверждает, что смотреть было на что: судимых %d, живых %d",
			census.Judged, census.Live)
	}
}

// ── ось умолчания: пустая приставка из суждения исключена BY CONSTRUCTION ────

// Уровень умолчания совпадает со всяким путём и мёртвым именем быть не может.
// Проба закрепляет это как РЕШЕНИЕ, а не как побочный эффект: без неё первый же
// рефакторинг цикла втянул бы умолчание в ось, и гейт краснел бы на пустом
// дереве дважды — один раз предпосылкой, второй раз мнимой мёртвой записью.
func TestLicenseTierSubjectGate_DefaultTierIsNeverJudged(t *testing.T) {
	t.Parallel()
	// Путей нет ни под одной приставкой, но умолчанию досталось два файла.
	paths := []string{"README.md", "Makefile"}

	faults, census := judgeLicenseTierSubjects(injTiers, injTierPaths(injTiers, paths))

	if census.PathsPerTier["монорепо-и"] != 2 {
		t.Fatalf("умолчание не получило путей: %d", census.PathsPerTier["монорепо-и"])
	}
	if census.Judged != 2 {
		t.Fatalf("судимых уровней %d — умолчание втянуто в ось", census.Judged)
	}
	for _, f := range faults {
		if strings.Contains(f, "монорепо-и") {
			t.Fatalf("умолчание объявлено мёртвым именем: %q", f)
		}
	}
	if len(faults) != 2 {
		t.Fatalf("оба уровня с приставкой обязаны быть находками: %v", faults)
	}
}

// ── предпосылка: пустой обход — ОТКАЗ, а не пустой успех ─────────────────────

func TestLicenseTierSubjectGate_EmptyWalkIsARefusal(t *testing.T) {
	t.Parallel()
	faults, census := judgeLicenseTierSubjects(injTiers, map[string]int{})
	if len(faults) != 1 || !strings.Contains(faults[0], "обход пуст") {
		t.Fatalf("нулевой обход прошёл как чистота: %v", faults)
	}
	if census.Paths != 0 {
		t.Fatalf("перепись насчитала пути там, где их нет: %d", census.Paths)
	}

	empty, _ := judgeLicenseTierSubjects(nil, map[string]int{"x": 1})
	if len(empty) != 1 || !strings.Contains(empty[0], "карта лицензий пуста") {
		t.Fatalf("пустая карта прошла как чистота: %v", empty)
	}

	// Карта, у которой остался ОДИН уровень умолчания: судить нечем, и это тоже
	// отказ. Иначе снятие последнего уровня с приставкой сделало бы ось
	// беспредметной — тихо и навсегда.
	only := []licenseTier{{Prefix: "", Name: "монорепо-и", SPDX: licenseBUSL}}
	solo, _ := judgeLicenseTierSubjects(only, map[string]int{"монорепо-и": 3})
	if len(solo) != 1 || !strings.Contains(solo[0], "судимых осью уровней ноль") {
		t.Fatalf("карта из одного умолчания прошла как чистота: %v", solo)
	}
}

// ── перепись: «ноль находок» отличимо от «ноль прочитанного» ─────────────────

func TestLicenseTierSubjectGate_CensusCountsEveryTierSeparately(t *testing.T) {
	t.Parallel()
	paths := []string{"pkg/a.go", "pkg/b.go", "services/gone/c.go", "README.md"}
	_, census := judgeLicenseTierSubjects(injTiers, injTierPaths(injTiers, paths))

	want := map[string]int{"фундамент-и": 2, "вынесенный-и": 1, "монорепо-и": 1}
	for name, n := range want {
		if census.PathsPerTier[name] != n {
			t.Fatalf("перепись уровня %q: %d, ожидалось %d", name, census.PathsPerTier[name], n)
		}
	}
	if census.Paths != 4 || census.Tiers != 3 {
		t.Fatalf("суммарная перепись разошлась: путей %d, уровней %d", census.Paths, census.Tiers)
	}
	// Одно суммарное число скрыло бы мёртвый уровень: проверяем, что строка
	// переписи и в самом деле несёт разбивку, а не только сумму.
	if s := census.String(); !strings.Contains(s, "вынесенный-и") {
		t.Fatalf("перепись не печатает уровни порознь: %q", s)
	}
}

// ── тот же опыт на ЖИВОЙ карте: снятие каталога обязано краснеть ─────────────

// Пробы выше гоняют суждение по синтетической карте — так проверяется механизм.
// Эта гоняет его по ЖИВОЙ licenseTiers и воспроизводит настоящий переход:
// перечень путей дерева без каталога вынесенного продукта. Без неё доказано
// было бы, что ось работает вообще, но не что она работает на той карте,
// которая исполняется.
//
// Пара одно-фактная: близнец — тот же перечень с каталогом на месте.
func TestLicenseTierSubjectGate_LiveMapRedsWhenTheCarvedOutTreeGoes(t *testing.T) {
	t.Parallel()

	const carved = "services/iam/"
	full := []string{
		"pkg/ids/id.go",
		"proto/kacho/cloud/vpc/v1/network.proto",
		"proto/google/api/annotations.proto",
		carved + "cmd/iam/main.go",
		"gateway/main.go",
	}
	var without []string
	for _, p := range full {
		if !strings.HasPrefix(p, carved) {
			without = append(without, p)
		}
	}

	// БЛИЗНЕЦ: каталог на месте — молчание.
	if faults, census := judgeLicenseTierSubjects(licenseTiers, licenseTierPathCensus(full)); len(faults) != 0 {
		t.Fatalf("живая карта краснеет на полном дереве: %v; перепись: %s", faults, census)
	}

	// ВНЕСЁННЫЙ ФАКТ: каталога нет — находка, называющая приставку.
	faults, census := judgeLicenseTierSubjects(licenseTiers, licenseTierPathCensus(without))
	if len(faults) != 1 {
		t.Fatalf("снятие каталога прошло молча: находок %d (%v); перепись: %s",
			len(faults), faults, census)
	}
	if !strings.Contains(faults[0], carved) {
		t.Fatalf("находка не называет снятую приставку: %q", faults[0])
	}
	if census.PathsPerTier["вынесенный продукт"] != 0 {
		t.Fatalf("перепись не показывает нулевой уровень: %d", census.PathsPerTier["вынесенный продукт"])
	}
}
