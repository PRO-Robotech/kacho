// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/platformmodules"
)

// platformmodulevocabulary.go — вердикт о словаре имён модулей платформы
// (pkg/platformmodules) против дерева.
//
// # Зачем гейт словарю, который «просто перечисляет имена»
//
// Словарь заведён затем, чтобы соответствие «служба ↔ модуль каталога»
// перестало жить пятью копиями в корпусе гейтов (#1885). Копия расходится
// молча — но и ЕДИНСТВЕННОЕ объявление расходится молча, если его никто не
// сверяет с деревом: разница лишь в том, что расхождение станет одно вместо
// пяти, и обнаружится оно у всех пяти читателей сразу.
//
// # У каждой колонки СВОЙ производитель в дереве
//
//	Service        каталог `services/<X>`
//	CatalogModule  каталог контрактов `proto/kacho/cloud/<Y>`
//	ObjectDomain   приставка типов канонической модели прав
//
// Сверка ДВУСТОРОННЯЯ: односторонняя пропускает ровно то, ради чего словарь
// заведён, — служба, которой в словаре нет, читалась бы соглашением об
// именовании, а «ноль находок» относилось бы к меньшему, чем кажется.
//
// # Пустой домен объекта — ФАКТ, а не пропуск
//
// `ObjectDomain: ""` утверждает, что модель не объявляет НИ ОДНОГО типа этого
// модуля, и проверяется так же строго, как непустой: иначе пустая строка стала
// бы способом снять проверку с колонки.

// vocabularyCensus — объём осмотренного, по колонкам.
type vocabularyCensus struct {
	Modules          int
	ServiceDirs      int
	ProtoDirs        int
	ModelTypes       int
	WithObjectDomain int
}

func (c vocabularyCensus) Summary() string {
	return fmt.Sprintf(
		"объявлено модулей %d · каталогов служб %d · каталогов контрактов %d · "+
			"типов модели %d · модулей с непустым доменом типов %d",
		c.Modules, c.ServiceDirs, c.ProtoDirs, c.ModelTypes, c.WithObjectDomain)
}

// judgePlatformVocabulary судит словарь против трёх производителей.
//
// Вход подаётся значениями, а не читается из дерева: инъекция подаёт
// синтетический тем же путём, каким гейт подаёт настоящий, поэтому
// доказательство способности падать относится к ЭТОЙ функции, а не к её копии.
//
// Находки собираются ВСЕ: названная первая заставила бы чинить их по одной.
func judgePlatformVocabulary(declared []platformmodules.Module,
	serviceDirs, protoDirs, modelTypes map[string]struct{}) ([]string, vocabularyCensus) {

	census := vocabularyCensus{
		Modules:     len(declared),
		ServiceDirs: len(serviceDirs),
		ProtoDirs:   len(protoDirs),
		ModelTypes:  len(modelTypes),
	}

	var faults []string
	byService := map[string]struct{}{}

	for _, m := range declared {
		byService[m.Service] = struct{}{}

		if _, ok := serviceDirs[m.Service]; !ok {
			faults = append(faults, "модуль "+m.Service+": каталога services/"+m.Service+
				" в дереве нет — короткое имя службы называет то, чего не существует")
		}
		if _, ok := protoDirs[m.CatalogModule]; !ok {
			faults = append(faults, "модуль "+m.Service+": каталога контрактов "+
				"proto/kacho/cloud/"+m.CatalogModule+" в дереве нет — модуль каталога "+
				"назван неверно")
		}

		switch m.ObjectDomain {
		case "":
			// Спрашиваем ОБА написания: модуль, чьих типов словарь не признаёт,
			// мог назвать их и коротким именем, и именем каталога.
			if found := typesPrefixedByAny(modelTypes, m.Service, m.CatalogModule); len(found) > 0 {
				faults = append(faults, "модуль "+m.Service+": домен типов объявлен ПУСТЫМ, а "+
					"модель объявляет типы с его приставкой ("+strings.Join(found, ", ")+
					") — пустая строка утверждает «типов нет» и стала бы способом снять "+
					"проверку с колонки")
			}
		default:
			if len(typesPrefixedByAny(modelTypes, m.ObjectDomain)) == 0 {
				faults = append(faults, "модуль "+m.Service+": домен типов объявлен как "+
					m.ObjectDomain+", а модель не объявляет ни одного типа с приставкой "+
					m.ObjectDomain+"_ — колонка называет несуществующий домен")
				break
			}
			census.WithObjectDomain++
		}
	}

	for dir := range serviceDirs {
		if _, ok := byService[dir]; !ok {
			faults = append(faults, "служба services/"+dir+" словарём не объявлена: её имена "+
				"будут прочитаны соглашением об именовании, а не словарём — добавьте запись")
		}
	}

	sort.Strings(faults)
	return faults, census
}

// typesPrefixedByAny — типы модели, чья приставка совпала с любым из написаний,
// отсортированно. Пустое написание не совпадает ни с чем: иначе оно совпало бы
// со всеми типами разом.
func typesPrefixedByAny(modelTypes map[string]struct{}, names ...string) []string {
	var out []string
	for typ := range modelTypes {
		for _, n := range names {
			if n != "" && strings.HasPrefix(typ, n+"_") {
				out = append(out, typ)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
