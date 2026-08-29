// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// sharedHelperRel — общий слой генератора коллекций newman.
const sharedHelperRel = "tests/newman/kacholib/gen_shared.py"

// sharedHelperCensus — объём осмотренного. Печатается ВСЕГДА: без него «находок
// нет» неотличимо от «ничего не прочитано», а обе половины (функции и константы)
// названы порознь, потому что одно суммарное число скрыло бы ровно тот случай,
// ради которого распознаватель расширяли.
type sharedHelperCensus struct {
	generators   int
	sharedFuncs  int
	sharedConsts int
	forks        int
	// crossKind — форки, у которых ФОРМА объявления у набора и в общем слое
	// РАЗНАЯ: функция общего слоя затенена присваиванием набора либо наоборот.
	// Считается отдельно намеренно: одно суммарное число скрыло бы ровно тот
	// случай, ради которого распознаватель расширяли, — расширение обязано
	// менять осмотренное, и это число печатается.
	crossKind int
}

// auditSharedHelperForks — судящая функция гейта.
//
// Выделена, чтобы инъекция гоняла ЕЁ, а не свою копию: проба, повторяющая логику
// гейта, доказывала бы свойство копии.
//
// Возвращает находки в порядке имён генераторов и перепись осмотренного.
func auditSharedHelperForks(sharedSrc string, generators map[string]string) ([]string, sharedHelperCensus) {
	sharedFuncs, sharedConsts := declaredNames(sharedSrc)
	cen := sharedHelperCensus{generators: len(generators), sharedFuncs: len(sharedFuncs), sharedConsts: len(sharedConsts)}

	rels := make([]string, 0, len(generators))
	for rel := range generators {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var findings []string
	for _, rel := range rels {
		ownFuncs, ownConsts := declaredNames(generators[rel])
		var clash []string
		for name := range ownFuncs {
			switch {
			case sharedFuncs[name]:
				clash = append(clash, name)
			case sharedConsts[name]:
				// ПЕРЕКРЁСТНОЕ ЗАТЕНЕНИЕ. Форма объявления разная, а имя одно —
				// и Python разрешает имя по ПОСЛЕДНЕМУ связыванию в модуле,
				// поэтому импорт из общего слоя оказывается перекрыт, а импорт
				// при этом остаётся на месте и выглядит действующим.
				clash = append(clash, name+" (функция набора затеняет константу общего слоя)")
				cen.crossKind++
			}
		}
		// Константа — та же форма форка, что и функция, и в этом дереве её
		// переносят вместе с функциями, которые её читают. Распознаватель,
		// знающий только `def`, оставил бы одиннадцать переехавших констант ВНЕ
		// наблюдения: не находкой и не молчанием, а невидимостью.
		for name := range ownConsts {
			switch {
			case sharedConsts[name]:
				clash = append(clash, name+" (константа)")
			case sharedFuncs[name]:
				// Та же ось с другой стороны, и она НЕ теоретическая: связывание
				// общего помощника с умолчаниями набора (`имя = partial(…)`)
				// записывается именно так. Пока распознаватель сверял форму с
				// формой, такое связывание было для него невидимо — не находкой
				// и не молчанием, а невидимостью.
				clash = append(clash, name+" (присваивание набора затеняет функцию общего слоя)")
				cen.crossKind++
			}
		}
		if len(clash) == 0 {
			continue
		}
		sort.Strings(clash)
		cen.forks += len(clash)
		findings = append(findings, fmt.Sprintf("%s — своё определение вместо общего: %s", rel, strings.Join(clash, ", ")))
	}
	return findings, cen
}

// Помощник генератора newman, вынесенный в общий слой, объявляется в дереве ОДИН
// раз.
//
// ПРЕДМЕТ. Генераторов newman в дереве девять — по одному на набор (семь
// сервисов, край и его тонкая обёртка), — и вспомогательный слой у них общий:
// сериализация литерала JavaScript, разбор порождаемого скрипта, признаки шага,
// утверждения об исходе операции. Пока каждый генератор нёс СВОЮ копию, правка
// помощника стоила восьми правок, а «поправил у себя» было неотличимо от
// «поправил везде».
//
// ЦЕНА ИЗМЕРЕНА, А НЕ ПРЕДПОЛОЖЕНА (задача #1367). Разбор AST по восьми копиям:
// функций с именем в пяти и более копиях — 41, из них побайтово совпадающих во
// ВСЕХ копиях — 25, разошедшихся — 16. Среди разошедшихся полоса видимости
// `retry_until_authorized` (пять различных версий на шесть копий) и ожидание
// операции `poll_operation_until_done` (шесть на шесть) — то есть ровно те
// помощники, которых нормативные правила требуют от каждого набора.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ДОГОВОРЁННОСТЬ. Копия заводится не злым умыслом, а
// копированием соседнего генератора при заведении набора — тем самым действием,
// которым набор и заводят. Договорённость этого не переживает: восемь копий и
// накопились так.
//
// ЧЕГО ГЕЙТ НЕ СУДИТ. Он не требует, чтобы общим стал КАЖДЫЙ помощник: полосы,
// разошедшиеся по существу, сводятся решением, а не переносом, и живут своей
// задачей. Предмет здесь — перечень УЖЕ вынесенного: раз имя объявлено в общем
// слое, второе его объявление есть форк, а не полоса набора.
func TestNewmanSharedHelperIsDeclaredOnce(t *testing.T) {
	root := repoRoot(t)

	// Состав — из индекса git, а не обходом диска: под корнем лежат рабочие
	// копии агентов и отчёты прогонов, и обход по диску сделал бы вердикт
	// свойством чужого каталога — в обе стороны: красный на файле, которого в
	// коммите нет, и молчание в свежем checkout.
	tt := newTrackedTree(t, root)

	if !tt.files[sharedHelperRel] {
		t.Fatalf("предпосылка гейта не выполняется: общего слоя %s в индексе git нет.\n"+
			"Гейт стережёт ПЕРЕЧЕНЬ вынесенного и без него судил бы пустоту — это отказ,\n"+
			"а не молчаливый успех: «ноль находок» обязано быть отличимо от «ноль прочитанного».",
			sharedHelperRel)
	}
	sharedSrc, err := os.ReadFile(filepath.Join(root, sharedHelperRel)) // #nosec G304 -- путь из индекса git этого модуля
	if err != nil {
		t.Fatalf("чтение %s: %v", sharedHelperRel, err)
	}

	generators := map[string]string{}
	for rel := range tt.files {
		if filepath.Base(rel) != "gen.py" || !strings.Contains(rel, "tests/newman") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса git этого модуля
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		generators[rel] = string(b)
	}
	if len(generators) == 0 {
		t.Fatalf("предпосылка гейта не выполняется: генераторов newman в индексе НОЛЬ —\n" +
			"либо файл переименован, либо обход смотрит не туда; чинить надо гейт,\n" +
			"а не молча выходить успехом.")
	}

	findings, cen := auditSharedHelperForks(string(sharedSrc), generators)

	if cen.sharedFuncs == 0 || cen.sharedConsts == 0 {
		t.Fatalf("предпосылка гейта не выполняется: в %s найдено функций %d, констант %d.\n"+
			"Общий слой несёт и то и другое, поэтому ноль в любой половине означает,\n"+
			"что распознаватель не знает формы записи, а не что предмета нет.",
			sharedHelperRel, cen.sharedFuncs, cen.sharedConsts)
	}

	t.Logf("осмотрено генераторов newman: %d; в общем слое функций %d, констант %d; "+
		"собственных определений тех же имён: %d, из них с РАЗНОЙ формой объявления: %d",
		cen.generators, cen.sharedFuncs, cen.sharedConsts, cen.forks, cen.crossKind)

	if len(findings) > 0 {
		t.Fatalf("помощник, вынесенный в %s, объявлен вторично в генераторе набора.\n"+
			"Форк помощника не ломает сборку и не роняет прогон — он расходится МОЛЧА,\n"+
			"и расхождение видно только тому, кто сравнит копии. Чинится импортом из\n"+
			"общего слоя, а не переносом правки:\n  %s",
			sharedHelperRel, strings.Join(findings, "\n  "))
	}
}

// declaredNames — имена, ОБЪЯВЛЕННЫЕ модулем: функции и константы, порознь.
//
// Судит объявление в начале строки (`def имя(` и `ИМЯ = …`), а не упоминание:
// имя помощника встречается в вызовах, в текстах сообщений и в объяснениях
// рядом, и распознаватель по подстроке краснел бы на собственном комментарии.
// Вложенные определения (с отступом) объявлениями модуля не считаются — они
// принадлежат своей функции и форком не являются.
//
// Форма константы выведена из дерева, а не из вкуса: все одиннадцать
// переехавших констант записаны верхнеуровневым присваиванием одному имени.
// Сравнение `==` под неё не подпадает.
var (
	funcDeclRe  = regexp.MustCompile(`(?m)^def ([A-Za-z_][A-Za-z0-9_]*)\(`)
	constDeclRe = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*) = [^=]`)
)

func declaredNames(src string) (funcs, consts map[string]bool) {
	funcs, consts = map[string]bool{}, map[string]bool{}
	for _, m := range funcDeclRe.FindAllStringSubmatch(src, -1) {
		funcs[m[1]] = true
	}
	for _, m := range constDeclRe.FindAllStringSubmatch(src, -1) {
		consts[m[1]] = true
	}
	return funcs, consts
}
