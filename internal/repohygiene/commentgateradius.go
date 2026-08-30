// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// commentgateradius.go — анализатор «прохибиционный список комментариев
// объявляет свой РАДИУС».
//
// # Предмет
//
// Инструмент, судящий текст комментариев по набору запрещённых образцов, легко
// принять за держателя общеплатформенного запрета — и его отсутствие у соседа
// принять за разрешение. Разница между «этот предмет узок» и «этот предмет
// общий, просто до соседа не доехало» в дереве ничем не видна: оба выглядят как
// один каталог с одним тестом.
//
// Цена различия измерена, а не предположена: список одного сервиса, поднятый на
// всё дерево как есть, дал бы почти три тысячи находок, из которых большинство —
// нормативные ссылки, которых корпус ТРЕБУЕТ. Разбор и числа —
// docs/architecture/comment-hygiene-radius.md.
//
// # Что анализатор опознаёт
//
// Прохибиционный список — файл, у которого сходятся ЧЕТЫРЕ признака сразу:
//
//	РАЗБИРАЕТ КОММЕНТАРИИ  просит у разбора текст комментариев;
//	ОБХОДИТ ДЕРЕВО         идёт по каталогу, а не по одному названному файлу;
//	ЧИТАЕТ ГРУППЫ          перебирает комментарии разобранного файла;
//	НЕСЁТ НАБОР ОБРАЗЦОВ   объявляет не меньше трёх образцов сравнения.
//
// Порознь ни один не достаточен: разбор комментариев есть у всякого, кто читает
// шапку функции; обход дерева — у половины гейтов; набор образцов — у любого
// разборщика. Вместе они означают «этот файл судит чужие комментарии по списку».
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **образец, собранный из переменной** (`regexp.MustCompile(pat)`) — довод
//     тот же, что и у соседних анализаторов дерева: литерал виден, вычисление
//     нет. Ошибка идёт в сторону МОЛЧАНИЯ, и это здесь названо, потому что
//     направление неудобное: список, собранный вычислением, анализатор пропустит.
//  2. **список из двух образцов** — порог назван ниже константой и выбран так,
//     чтобы одиночная проверка формы («имя обязано отвечать образцу») под него не
//     подпадала. Список из двух образцов в дереве не встречался ни разу.
//  3. **инструмент на другом языке** — предмет анализатора Go, и только он.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// minProhibitionPatterns — сколько образцов делает набор набором.
//
// Два — это ещё пара проверок формы (положительная и отрицательная), три —
// уже перечень запретов. Порог выбран по дереву: наборов из двух образцов,
// сходящихся с прочими тремя признаками, в нём нет.
const minProhibitionPatterns = 3

// radiusMarker — слово, которым объявление радиуса опознаётся машинно.
//
// Маркер, а не свободная проза: «сказано вслух» без опознаваемой формы
// проверить нельзя, а правило, обещающее непроверяемое, само есть тот класс,
// который корпус ловит.
const radiusMarker = "РАДИУС:"

// CommentProhibitionPattern — один образец набора вместе с координатой.
type CommentProhibitionPattern struct {
	Line   int
	Source string
}

// CommentProhibitionList — найденный набор запретов одного файла.
type CommentProhibitionList struct {
	// File — путь от корня дерева.
	File string
	// Patterns — образцы набора.
	Patterns []CommentProhibitionPattern
	// Declaration — текст объявления радиуса из шапки файла; пуст, если
	// объявления нет.
	Declaration string
}

// ProhibitionCensus — объём осмотренного одним файлом.
//
// Три величины, а не одна: файл разобран · образцов встречено · признаков
// сошлось. Одно число «файлов прочитано» скрыло бы ровно тот случай, ради
// которого перепись и печатается, — разбор, переставший видеть предмет.
type ProhibitionCensus struct {
	// Literals — строковых литералов образцов встречено (в том числе в файлах,
	// не ставших набором).
	Literals int
	// SignalsMet — сколько из четырёх признаков сошлось на этом файле.
	SignalsMet int
}

// ScanCommentProhibitionList разбирает один файл Go и возвращает набор
// запретов, если все четыре признака сошлись. Иначе — nil и перепись.
func ScanCommentProhibitionList(rel string, src []byte) (*CommentProhibitionList, ProhibitionCensus, error) {
	var census ProhibitionCensus

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, census, err
	}

	var (
		parsesComments bool
		walksTree      bool
		readsGroups    bool
		patterns       []CommentProhibitionPattern
	)

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			pkg, ok := node.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch pkg.Name + "." + node.Sel.Name {
			case "parser.ParseComments":
				parsesComments = true
			case "filepath.Walk", "filepath.WalkDir",
				"treecorpus.Under", "treecorpus.UnderWithSuffix", "treecorpus.Glob":
				walksTree = true
			case "ast.CommentGroup", "ast.CommentMap", "ast.NewCommentMap":
				readsGroups = true
			}
			if node.Sel.Name == "Comments" {
				readsGroups = true
			}
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "regexp" {
				return true
			}
			if sel.Sel.Name != "MustCompile" && sel.Sel.Name != "Compile" {
				return true
			}
			if len(node.Args) != 1 {
				return true
			}
			lit, ok := node.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				// Образец, собранный вычислением. Считаем встреченным — иначе
				// перепись завысила бы долю разобранного.
				census.Literals++
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				census.Literals++
				return true
			}
			census.Literals++
			patterns = append(patterns, CommentProhibitionPattern{
				Line:   fset.Position(lit.Pos()).Line,
				Source: value,
			})
		}
		return true
	})

	for _, met := range []bool{parsesComments, walksTree, readsGroups, len(patterns) >= minProhibitionPatterns} {
		if met {
			census.SignalsMet++
		}
	}
	if census.SignalsMet < 4 {
		return nil, census, nil
	}

	return &CommentProhibitionList{
		File:        rel,
		Patterns:    patterns,
		Declaration: radiusDeclaration(fset, file),
	}, census, nil
}

// radiusDeclaration достаёт объявление радиуса из ШАПКИ файла — комментариев,
// стоящих до ключевого слова пакета.
//
// Именно до него, а не где угодно: объявление обязано читаться первым, вместе с
// назначением файла. Строка, спрятанная в середине, объявлением не является —
// её не прочтёт тот, ради кого она написана.
func radiusDeclaration(fset *token.FileSet, file *ast.File) string {
	pkgLine := fset.Position(file.Package).Line
	var head []string
	for _, group := range file.Comments {
		if fset.Position(group.End()).Line >= pkgLine {
			break
		}
		head = append(head, group.Text())
	}
	joined := strings.Join(head, "\n")
	at := strings.Index(joined, radiusMarker)
	if at < 0 {
		return ""
	}
	return strings.TrimSpace(joined[at:])
}

// DeclarationNamesAPath отвечает, называет ли объявление радиуса координату,
// которая в дереве СУЩЕСТВУЕТ.
//
// Требование не косметическое: объявление, называющее общеплатформенный
// инструмент, есть ссылка, а ссылка стареет. Мёртвая координата в объявлении
// радиуса хуже её отсутствия — она утверждает, что общий предмет где-то держат,
// и указывает не туда. Проверка существования делает объявление
// САМОИСТЕКАЮЩИМ: переехал инструмент — объявление стало находкой.
//
// exists принимает путь от корня дерева и отвечает, есть ли такой файл или
// каталог.
func DeclarationNamesAPath(declaration string, exists func(rel string) bool) (string, bool) {
	for _, field := range strings.FieldsFunc(declaration, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', ',', ';', '(', ')', '«', '»', '"', '`':
			return true
		}
		return false
	}) {
		candidate := strings.Trim(field, ".:—–-")
		if !strings.Contains(candidate, "/") {
			continue
		}
		if exists(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// MandatedCommentForm — форма комментария, которую корпус ТРЕБУЕТ.
//
// Запрещать её — значит запрещать исполнение правила, и такой набор поднимается
// не на дерево, а на пересмотр. Каждая запись называет правило, из которого
// взята: форма без источника была бы вкусом, а не нормой.
//
// Корпус лежит здесь, а не в файле гейта, потому что им пользуются ДВОЕ — гейт и
// его инъекция. Вторая копия разошлась бы с первой молча и разошлась бы ровно
// там, где расхождение не видно: обе отвечают «совпало» на совпадающем входе.
type MandatedCommentForm struct {
	// Name — как форма называется в находке.
	Name string
	// Text — сам комментарий, как он выглядит в исходнике.
	Text string
	// Rule — правило, которое эту форму требует.
	Rule string
}

// MandatedCommentForms — корпус нормативных форм.
var MandatedCommentForms = []MandatedCommentForm{
	{
		Name: "ссылка на непреложный запрет по номеру",
		Text: "// Внутренние методы не публикуются на внешнем слушателе (ban #6).",
		Rule: "security.md, три невыхолащиваемых места: комментарий у гейта обязан " +
			"называть то, что запрещает, иначе его снимут как непонятную проверку",
	},
	{
		Name: "ссылка на раздел правила",
		Text: "// Когерентность размещения задана data-integrity.md " + "\u00a7" + "Placement-coherence.",
		Rule: "тот же пункт security.md: комментарий обязан называть предмет прямо",
	},
	{
		Name: "обычная инженерная лексика",
		Text: "// Двухфазная фиксация: сначала prepare phase, затем commit.",
		Rule: "запрет, срабатывающий на честной прозе, учит писать в обход образца, " +
			"а не избегать предмета — довод, по которому образец verbatim из этого же " +
			"набора уже снимали",
	},
	{
		Name: "диагностика гейта о собственной ведомости",
		Text: "// Запись, которой больше нечего исключать, — находка, а не тишина.",
		Rule: "testing.md, гейт на класс п.5: послабление обязано истекать само, " +
			"и сказать об этом гейт может только словами",
	},
}

// ControlViolationForms — НАСТОЯЩИЕ нарушения. Контроль прогонщика в обратную
// сторону: молчание на нормативных формах имеет смысл только тогда, когда
// прогонщик вообще способен что-нибудь найти.
//
// Собраны из частей по той же причине, по которой из частей собраны примеры у
// соседнего гейта отложенной работы: написанные целиком, они сделали бы ЭТОТ
// файл нарушителем чужого запрета, и тот был бы прав.
var ControlViolationForms = []string{
	"// " + "TODO" + " дочинить после релиза",
	"// Форма ссылки взята из " + "KAC" + "-266",
}

// ProbeAgainstMandatedForms возвращает описания совпадений набора с корпусом
// нормативных форм и отдельно — образцы, которые не компилируются.
func ProbeAgainstMandatedForms(l *CommentProhibitionList, forms []MandatedCommentForm) (collisions, broken []string) {
	for _, p := range l.Patterns {
		re, err := regexp.Compile(p.Source)
		if err != nil {
			broken = append(broken, fmt.Sprintf("строка %d, образец %q не компилируется: %v",
				p.Line, p.Source, err))
			continue
		}
		for _, m := range forms {
			if re.MatchString(m.Text) {
				collisions = append(collisions, fmt.Sprintf(
					"строка %d, образец %q запрещает форму «%s» (%s)",
					p.Line, p.Source, m.Name, m.Rule))
			}
		}
	}
	sort.Strings(collisions)
	sort.Strings(broken)
	return collisions, broken
}

// CountControlHits — сколько раз образцы наборов нашли настоящее нарушение.
func CountControlHits(lists []*CommentProhibitionList, control []string) int {
	hits := 0
	for _, l := range lists {
		for _, p := range l.Patterns {
			re, err := regexp.Compile(p.Source)
			if err != nil {
				continue
			}
			for _, v := range control {
				if re.MatchString(v) {
					hits++
				}
			}
		}
	}
	return hits
}

// JudgeServiceLocalList — вердикт по ОДНОМУ служебному набору.
//
// Пустая строка означает «замечаний нет». Требование адресовано только тем
// наборам, которые запрещают нормативную форму: набору, запрещающему одни лишь
// настоящие нарушения, объявлять нечего — его радиус не спорен.
func JudgeServiceLocalList(l *CommentProhibitionList, forms []MandatedCommentForm, exists func(rel string) bool) (finding string, collisions int) {
	cols, broken := ProbeAgainstMandatedForms(l, forms)
	if len(broken) > 0 {
		return fmt.Sprintf("%s — образцы набора не компилируются:\n     %s",
			l.File, strings.Join(broken, "\n     ")), len(cols)
	}
	if len(cols) == 0 {
		return "", 0
	}
	if l.Declaration == "" {
		return fmt.Sprintf(
			"%s — набор запрещает форму, которой корпус ТРЕБУЕТ, и не объявляет своего "+
				"радиуса.\n     %s\n     Снятие: поставить в ШАПКУ файла (до слова package) "+
				"строку с маркером %q, назвав в ней (а) что инструмент читает ОДИН сервис "+
				"и (б) координату инструмента, держащего общий предмет по всему дереву.",
			l.File, strings.Join(cols, "\n     "), radiusMarker), len(cols)
	}
	if _, ok := DeclarationNamesAPath(l.Declaration, exists); !ok {
		return fmt.Sprintf(
			"%s — объявление радиуса есть, но не называет НИ ОДНОЙ живой координаты общего "+
				"инструмента.\n     Объявление: %q\n     Мёртвая координата хуже её "+
				"отсутствия: она утверждает, что общий предмет где-то держат, и указывает "+
				"не туда.",
			l.File, FirstLine(l.Declaration)), len(cols)
	}
	return "", len(cols)
}

// FirstLine — первая строка текста: находка обязана показывать то, что нашла, не
// вываливая абзац целиком.
func FirstLine(s string) string {
	if at := strings.IndexByte(s, '\n'); at >= 0 {
		return strings.TrimSpace(s[:at])
	}
	return strings.TrimSpace(s)
}
