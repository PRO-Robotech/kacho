// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"sort"
	"strings"
)

// foundationtreepathliteral.go — прод-код ФУНДАМЕНТА не называет координату
// дерева, которого у него после разъезда не будет.
//
// # Предмет, и почему его не видит ни один существующий гейт
//
// Гейт границы (`foundationboundary.go`) судит РЁБРА — то есть объявления
// импорта. Путь, лежащий в строковом ЗНАЧЕНИИ, ребра не образует: он не
// компилируется, ни на что не ссылается и в графе импортов отсутствует
// by construction. Значит фундамент может адресовать чужое дерево, а все три оси
// границы останутся зелёными — что и было: три координаты, и ни одна не
// краснила ничего.
//
// # Что именно запрещено
//
// Строковый литерал прод-файла переезжающего набора, начинающийся с корня,
// которого в отдельном репозитории фундамента не будет:
//
//	корни платформы и службы   gateway/ services/ terraform/ proto/
//	оснастка дерева            internal/ deploy/ tools/ ui-future/
//	САМ `pkg/`                 пакеты переезжают из `pkg/X` в `X`
//
// Последняя строка не педантизм: координата собственного пакета в форме дерева
// ломается ровно так же, как чужая, и найдена она была тем же обходом. Разница
// лишь в том, что чужая указывает в никуда, а своя — в место, которого нет.
//
// Словарь корней НЕ выписывается: он выводится из `foundationRoots`, то есть из
// той же карты, которой объявлены классы. Второй экземпляр разошёлся бы с первым
// молча — как разошлись бы «имя домена» и «имя каталога», будь они выписаны
// порознь.
//
// # Три исхода координаты, и все три наблюдались
//
//	мёртвая координата в чужое дерево   снимается вместе с предметом
//	живая, но не в том дереве           переезжает туда, где её читатели
//	координата СВОЕГО пакета            записывается путём ИМПОРТА, а не дерева
//
// Путь импорта годится потому, что живёт в одном пространстве имён с самим
// импортом и правится ТЕМ ЖЕ заходом; путь дерева не правится ничем.
//
// # Чего гейт НЕ утверждает
//
// Он не судит комментарии и не судит пробы. Комментарий — не значение, его
// ложность ловится проверкой свежести документации; проба фундамента, читающая
// дерево платформы, — отдельный класс со своим предметом (её место, а не её
// строка), и смешивать их значило бы получить один гейт с двумя предметами.

// treePathLiteral — одно наблюдение: литерал прод-файла фундамента, адресующий
// корень, которого у фундамента не будет.
type treePathLiteral struct {
	File  string
	Value string
	Root  string
}

// treePathLiteralCensus — объём осмотренного. Печатается ВСЕГДА.
type treePathLiteralCensus struct {
	ProdFiles int // прод-файлов переезжающего набора прочитано
	Literals  int // строковых литералов осмотрено
	Roots     int // корней в словаре запрета
	Packages  int // пакетов переезжающего набора
}

func (c treePathLiteralCensus) String() string {
	return fmt.Sprintf("перепись: пакетов переезда %d · прод-файлов прочитано %d · "+
		"литералов осмотрено %d · корней в словаре %d",
		c.Packages, c.ProdFiles, c.Literals, c.Roots)
}

// forbiddenTreeRootsForFoundation — корни, которых у отдельного репозитория
// фундамента не будет.
//
// Выводится из `foundationRoots` (там объявлен класс каждого верхнего корня) плюс
// `pkg/`: сам этот каталог тоже исчезает — пакеты переезжают из `pkg/X` в `X`.
func forbiddenTreeRootsForFoundation() []string {
	seen := map[string]struct{}{"pkg": {}}
	for _, r := range foundationRoots {
		// Берётся ПЕРВЫЙ сегмент: `services/iam` и `services` — один корень
		// дерева, и запрещать надо корень, а не каждую его запись.
		root := r.Prefix
		if i := strings.IndexByte(root, '/'); i >= 0 {
			root = root[:i]
		}
		seen[root] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// literalNamesForbiddenRoot — литерал адресует запрещённый корень.
//
// Требуется приставка «корень + слэш», а не просто совпадение имени: слово
// `services` само по себе координатой не является, и запрет по нему краснел бы на
// прозе, поехавшей в сообщение об ошибке.
func literalNamesForbiddenRoot(value string, roots []string) (string, bool) {
	for _, r := range roots {
		if strings.HasPrefix(value, r+"/") {
			return r, true
		}
	}
	return "", false
}

// judgeFoundationTreePathLiterals — вердикт.
//
// Пустой обход — НАХОДКА: «ноль координат» обязано быть отличимо от «ноль
// прочитанного».
func judgeFoundationTreePathLiterals(found []treePathLiteral, census treePathLiteralCensus) []string {
	var faults []string

	if census.Packages == 0 || census.ProdFiles == 0 {
		faults = append(faults, fmt.Sprintf("обход пуст: пакетов %d, прод-файлов %d — "+
			"вердикт беспредметен, и «ноль координат» здесь означает «ноль прочитанного»",
			census.Packages, census.ProdFiles))
		return faults
	}
	if census.Literals == 0 {
		faults = append(faults, "во всём прод-коде переезжающего набора не осмотрено ни одного "+
			"строкового литерала — разбор перестал их видеть, и зелёное было бы вакуумным")
		return faults
	}
	if census.Roots == 0 {
		faults = append(faults, "словарь запрещённых корней пуст — гейт не запрещает ничего")
		return faults
	}

	ordered := append([]treePathLiteral(nil), found...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].File != ordered[j].File {
			return ordered[i].File < ordered[j].File
		}
		return ordered[i].Value < ordered[j].Value
	})
	for _, f := range ordered {
		faults = append(faults, fmt.Sprintf("%s: литерал %q адресует корень %q, которого у "+
			"отдельного репозитория фундамента не будет. Путь лежит в ЗНАЧЕНИИ, поэтому ни граф "+
			"импортов, ни сборка о нём не скажут. Исходов три: снять вместе с предметом · "+
			"перенести туда, где живут читатели · записать путём ИМПОРТА, если координата своя",
			f.File, f.Value, f.Root))
	}
	return faults
}
