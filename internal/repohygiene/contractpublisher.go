// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"sort"
	"strings"
)

// contractpublisher.go — вердикт о ЕДИНСТВЕННОСТИ ПУБЛИКАТОРА каждого пути
// `.proto` по всем модулям состава.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ, И ОН НЕ СБОРОЧНЫЙ
//
// Двоичное, слинковавшее ДВА Go-пакета, каждый из которых регистрирует одно и то
// же имя файла контракта, СОБИРАЕТСЯ ЧИСТО и падает при регистрации
// дескрипторов:
//
//	panic: proto: file "kaname/cloud/iam/v1/authorize_service.proto" is already registered
//
// Отказ приходит в инициализации и ТРАНЗИТИВНО — у потребителя, который ни
// одного из двух путей не называл. Значит ни `go build`, ни `go vet`, ни сверка
// заглушек с контрактом этого класса не видят: судить обязано ОБЪЯВЛЕНИЕ
// генерации, до всякой сборки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ «ЛЕЖИТ В ДВУХ ДЕРЕВЬЯХ» — НЕ ТО ЖЕ, ЧТО «ПУБЛИКУЕТСЯ ДВАЖДЫ»
//
// Копия `.proto` в чужом дереве НЕИЗБЕЖНА и законна: оператор `import`
// резолвится ФАЙЛОМ, а не модулем, — без шести входных файлов контракты службы
// не компилируются нигде. Нарушение начинается там, где второй модуль ПОРОЖДАЕТ
// по этому пути заглушки.
//
// Поэтому предикат — не «сколько деревьев несут путь», а «сколько объявлений
// генерации его накрывают». Считать деревья значило бы краснеть на законном
// входе и молчать на настоящем дубле: у входа копия есть, а порождения нет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ СУДЬЯ НЕ ДЕЛАЕТ
//
// Он не сверяет СОДЕРЖИМОЕ копий: одинаковы ли входной файл и его исходник —
// отдельный предмет со своим замером (ведомость входов службы,
// `proto/inputs.yaml`, и её гейт отпечатков). Здесь вопрос ровно один: не
// порождаются ли по одному пути два Go-пакета.

// contractPublication — что ОДИН модуль публикует заглушками.
//
// Единица — путь `.proto`, ВЫВЕДЕННЫЙ из дерева заглушек модуля, а не взятый из
// его `buf.gen.yaml`. Разница несущая: объявление говорит, что модуль СОБИРАЛСЯ
// породить, дерево заглушек — что он ПОРОДИЛ, а регистрация дескрипторов бывает
// ровно у второго. Объявление, разошедшееся со своим выходом, здесь и обнажится.
type contractPublication struct {
	Module string
	Paths  []string
}

// contractPublisherCensus — объём осмотренного.
type contractPublisherCensus struct {
	Modules   int
	FilesRead int
	Paths     int
	Duplicate int
}

func (c contractPublisherCensus) String() string {
	return fmt.Sprintf(
		"модулей прочитано %d · файлов заглушек осмотрено %d · "+
			"различных путей .proto опубликовано %d · публикуемых дважды %d",
		c.Modules, c.FilesRead, c.Paths, c.Duplicate)
}

// AuditContractPublishers — у каждого пути `.proto` РОВНО ОДИН публикатор.
//
// ОТКАЗЫ НА ПУСТОМ ВХОДЕ — ТРИ, и каждый закрывает свою слепоту:
//
//	модулей меньше двух   дубль возникает МЕЖДУ модулями, и вердикт по одному
//	                      относился бы к непрочитанному;
//	путей ноль            обход деревьев заглушек отказал;
//	модуль без путей      его дерево заглушек не прочитано — а именно такой
//	                      молчащий ноль и означает, что дубль в нём не найдут.
func AuditContractPublishers(pubs []contractPublication, filesRead int) ([]string, contractPublisherCensus) {
	census := contractPublisherCensus{Modules: len(pubs), FilesRead: filesRead}

	if len(pubs) < 2 {
		return []string{fmt.Sprintf("модулей прочитано %d: дубль публикатора возникает "+
			"МЕЖДУ модулями, и вердикт по одному дереву относился бы к непрочитанному",
			len(pubs))}, census
	}

	publishers := map[string][]string{}
	for _, pub := range pubs {
		if len(pub.Paths) == 0 {
			return []string{fmt.Sprintf("модуль %s не опубликовал НИ ОДНОГО пути .proto: "+
				"его дерево заглушек не прочитано, и «дубля нет» означало бы «в нём не "+
				"искали». Разбор дерева модуля отказал либо кэш модулей не наполнен "+
				"(`go mod download`)", pub.Module)}, census
		}
		seen := map[string]bool{}
		for _, path := range pub.Paths {
			if seen[path] {
				continue
			}
			seen[path] = true
			publishers[path] = append(publishers[path], pub.Module)
		}
	}
	census.Paths = len(publishers)

	if census.Paths == 0 {
		return []string{"путей .proto не выведено ни одного при непустых деревьях " +
			"заглушек — перевод имени заглушки в путь контракта отказал"}, census
	}

	paths := make([]string, 0, len(publishers))
	for path := range publishers {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var faults []string
	for _, path := range paths {
		mods := publishers[path]
		if len(mods) < 2 {
			continue
		}
		census.Duplicate++
		sort.Strings(mods)
		faults = append(faults, fmt.Sprintf(
			"путь %s публикуют заглушками %d модуля (%s): двоичное, слинковавшее оба, "+
				"падает при РЕГИСТРАЦИИ ДЕСКРИПТОРОВ (`already registered`) и падает "+
				"ТРАНЗИТИВНО — у потребителя, который ни одного из путей не называл. "+
				"Сборкой класс не ловится; лечится тем, что второй модуль держит путь "+
				"ВХОДОМ и по нему не порождает",
			path, len(mods), strings.Join(mods, ", ")))
	}
	return faults, census
}

// externalRootDeclarationCensus — объём осмотренного у сверки двух объявлений
// внешних корней (Go и оболочка).
type externalRootDeclarationCensus struct {
	GoRows    int
	ShellRows int
	Mentions  int
}

func (c externalRootDeclarationCensus) String() string {
	return fmt.Sprintf("записей Go %d · записей оболочки %d · упоминаний имени массива %d",
		c.GoRows, c.ShellRows, c.Mentions)
}

// AuditExternalRootDeclarations — перечень «корень → публикующий модуль»
// объявлен ДВАЖДЫ (Go и оболочка), и расхождение обязано краснеть.
//
// Вторая копия НЕИЗБЕЖНА — оболочка не импортирует пакет Go, — но обе стороны
// ОТБИРАЮТ ПОПУЛЯЦИЮ: корень, известный одной и неизвестный другой, даёт не
// отказ, а СУЖЕНИЕ. Измерено на этом самом переезде: генератор края, не знавший,
// откуда взять корень службы, выходил кодом 0 и печатал «OK: 233 entries»
// вместо 350.
//
// Пустая сторона — ОТКАЗ, а не «расхождений нет»: пустой перечень у оболочки
// неотличим от «присваивание не распознано».
func AuditExternalRootDeclarations(fromGo, fromShell map[string]string, mentions int) ([]string, externalRootDeclarationCensus) {
	census := externalRootDeclarationCensus{
		GoRows: len(fromGo), ShellRows: len(fromShell), Mentions: mentions,
	}
	if len(fromGo) == 0 {
		return []string{"перечень внешних корней в Go пуст: всякий корень считался бы " +
			"лежащим в этом дереве, и отсутствие его файлов читалось бы как пустое дерево"}, census
	}
	if len(fromShell) == 0 {
		return []string{"перечень внешних корней в оболочке не распознан (ноль записей): " +
			"пустой перечень неотличим от неудачного разбора ПРИСВАИВАНИЯ, а ноль здесь " +
			"означает, что вход генераторов собирается без чужих корней"}, census
	}

	roots := map[string]struct{}{}
	for r := range fromGo {
		roots[r] = struct{}{}
	}
	for r := range fromShell {
		roots[r] = struct{}{}
	}
	ordered := make([]string, 0, len(roots))
	for r := range roots {
		ordered = append(ordered, r)
	}
	sort.Strings(ordered)

	var faults []string
	for _, r := range ordered {
		g, inGo := fromGo[r]
		sh, inShell := fromShell[r]
		switch {
		case !inShell:
			faults = append(faults, fmt.Sprintf(
				"корень %q объявлен внешним в Go (модуль %s), а в оболочке — нет: "+
					"генераторы края собрали бы вход БЕЗ его дерева и вышли бы успехом", r, g))
		case !inGo:
			faults = append(faults, fmt.Sprintf(
				"корень %q объявлен внешним в оболочке (модуль %s), а в Go — нет: "+
					"пробы дерева искали бы его контракты в этом дереве и отказывали бы на "+
					"отсутствующем файле, называя это находкой", r, sh))
		case g != sh:
			faults = append(faults, fmt.Sprintf(
				"корень %q взят из РАЗНЫХ модулей: Go называет %s, оболочка — %s. "+
					"Две координаты одного дерева контрактов расходятся молча, и вердикт "+
					"гейта относился бы не к тому дереву, из которого собран выход", r, g, sh))
		}
	}
	return faults, census
}
