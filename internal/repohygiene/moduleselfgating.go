// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// moduleselfgating.go — ПРЕДИКАТ сверки объявления «модуль гейтит отношение сам»
// с прод-кодом самого модуля (задача продукта #2375).
//
// # Что здесь и чего здесь нет
//
// Здесь — чистая функция сравнения двух множеств и перепись по осям. Обход
// дерева, разбор Go и вердикт живут в пробе рядом: предикат обязан судиться
// инъекцией на синтетике, а синтетику нельзя подать тому, кто сам ходит по
// диску.
//
// # Почему сверка ДВУСТОРОННЯЯ
//
// Односторонняя («объявленное читается») пропустила бы отношение, которое модуль
// начал гейтить и не объявил: читатель объявления (служба доступа) не увидел бы
// у него читателя и назвал бы его мёртвым — то есть ЛОЖНАЯ находка у соседнего
// продукта. Обратная односторонняя («читаемое объявлено») пропустила бы
// объявление, потерявшее предмет: модуль перестал гейтить, а запись осталась и
// продолжает засчитывать читателя, которого нет, — то есть МОЛЧАНИЕ там, где
// отношение стало мёртвым.
//
// Оба исхода наблюдаемы только с этой стороны шва: после разреза службы её
// модели у платформы нет, а её читателю нечем проверить чужой код.
package repohygiene

import (
	"fmt"
	"sort"
)

// selfGatingFinding — одна находка сверки.
type selfGatingFinding struct {
	Module   string
	Relation string
	// Declared — объявление говорит, что модуль гейтит это отношение.
	Declared bool
	// Observed — литерал отношения встречается в его не-тестовом прод-коде.
	Observed bool
}

func (f selfGatingFinding) String() string {
	switch {
	case f.Declared && !f.Observed:
		return fmt.Sprintf(
			"%s: объявлено, что модуль гейтит %q своим кодом, а литерала в его не-тестовом "+
				"Go нет — объявление пережило предмет, и читатель засчитывает читателя, "+
				"которого не существует", f.Module, f.Relation)
	case !f.Declared && f.Observed:
		return fmt.Sprintf(
			"%s: прод-код модуля читает %q, а объявление об этом молчит — читатель "+
				"объявления назовёт отношение мёртвым, то есть получит ЛОЖНУЮ находку "+
				"о чужом продукте", f.Module, f.Relation)
	default:
		return fmt.Sprintf("%s: %q — вырожденная запись сверки", f.Module, f.Relation)
	}
}

// selfGatingCensus — объём осмотренного по осям.
type selfGatingCensus struct {
	// ModulesDeclared — модулей в объявлении.
	ModulesDeclared int
	// ModulesWalked — модулей, чей прод-код обойдён.
	ModulesWalked int
	// FilesParsed — прод-файлов Go разобрано.
	FilesParsed int
	// PairsDeclared / PairsObserved — пар (модуль, отношение) с каждой стороны.
	PairsDeclared int
	PairsObserved int
}

func (c selfGatingCensus) String() string {
	return fmt.Sprintf(
		"объявлено модулей %d (пар %d) · обойдено модулей %d, прод-файлов %d (пар %d)",
		c.ModulesDeclared, c.PairsDeclared, c.ModulesWalked, c.FilesParsed, c.PairsObserved)
}

// auditModuleSelfGating — сверка двух множеств пар (модуль, отношение).
//
// declared и observed ключуются коротким именем модуля. Модули, о которых
// молчат ОБЕ стороны, находкой не являются и в перепись пар не попадают: их не
// существует ни для одного читателя.
func auditModuleSelfGating(
	declared, observed map[string]map[string]bool,
) ([]selfGatingFinding, selfGatingCensus) {
	c := selfGatingCensus{ModulesDeclared: len(declared), ModulesWalked: len(observed)}

	modules := map[string]bool{}
	for m, rels := range declared {
		modules[m] = true
		c.PairsDeclared += len(rels)
	}
	for m, rels := range observed {
		modules[m] = true
		c.PairsObserved += len(rels)
	}

	names := make([]string, 0, len(modules))
	for m := range modules {
		names = append(names, m)
	}
	sort.Strings(names)

	var found []selfGatingFinding
	for _, m := range names {
		rels := map[string]bool{}
		for r := range declared[m] {
			rels[r] = true
		}
		for r := range observed[m] {
			rels[r] = true
		}
		relNames := make([]string, 0, len(rels))
		for r := range rels {
			relNames = append(relNames, r)
		}
		sort.Strings(relNames)
		for _, r := range relNames {
			d, o := declared[m][r], observed[m][r]
			if d == o {
				continue
			}
			found = append(found, selfGatingFinding{Module: m, Relation: r, Declared: d, Observed: o})
		}
	}
	return found, c
}
