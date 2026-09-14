// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Разбор дерева для гейта «отказ на снятие несёт полосу».
//
// Живёт в НЕ-тестовом файле, потому что его зовут двое: сам гейт и инъекция,
// подающая синтетическое дерево. Судья отделён от сборщика фактов по той же
// причине: инъекция обязана проверять, что он краснеет ПО СУЩЕСТВУ и называет
// координату, а не что он вообще умеет краснеть.

// deletionRefusalFamilies — образцы прозы, по которым ищутся КАНДИДАТЫ в отказы
// на снятие.
//
// ПРЕДЕЛ НАЗВАН ПРЯМО: это распознаватель по ТЕКСТУ, и он знает ровно те формы,
// которые в дереве есть на день заведения. Отказ, написанный незнакомой формой,
// он не найдёт — и не покраснеет, и не позеленеет, а промолчит (testing.md
// §Гейт на класс, п.7). Поэтому перепись печатает объём осмотренного: расширение
// образца обязано менять число кандидатов, иначе расширение холостое.
//
// Две формы дописаны ПОСЛЕ инъекции, а не по догадке: широкий невод по тем же
// файлам дал вдвое больше попаданий, и адъюдикация остатка нашла два живых
// отказа без полосы. Первая форма — тот же счётчик детей, но с форматным
// указателем вместо цифры (`has %d target(s)`); прежний образец знал только
// цифру и потому молчал на КАЖДОМ отказе, собранном форматной строкой.
// Вторая — защита от снятия, написанная не через «protected».
//
// Структурная половина гейта этого предела не имеет: мост, пара «полоса +
// sentinel предусловия» и наличие производителя у каждой полосы проверяются по
// узлам разбора, а не по словам.
var deletionRefusalFamilies = regexp.MustCompile(
	`(?i)deletion[ _]protection enabled|protected from deletion` +
		`|\bis not empty\b|\bstill contains\b` +
		`|\bis in use\b|\bin use by\b|\bis referenced by\b|already referenced\b` +
		`|\bstill attached to\b|\bis attached to\b|\bstill holds\b` +
		`|has \d*\s*[a-z]+\(s\)|has %[a-z] [a-z]+\(s\)|\bdependent resources\b` +
		`|\bcannot be deleted\b`)

// laneBearing — признаки выражения, которое ПРИКЛЕИВАЕТ полосу.
//
// Проверяется по напечатанному выражению вызова, а не по подстроке файла:
// «refusal» встречается в этом дереве и в комментариях, и в именах проб, и гейт
// по тексту зеленел бы на прозе о самом себе.
var laneBearing = []string{"refusal.", "DeletionRefusal"}

// DeletionRefusalSite — кандидат в отказы на снятие.
type DeletionRefusalSite struct {
	// File — путь, укороченный до первого значимого сегмента дерева.
	File string
	// Line — строка литерала.
	Line int
	// Text — сам литерал.
	Text string
	// Laned — литерал стоит внутри выражения, приклеивающего полосу.
	Laned bool
}

// DeletionRefusalFacts — что найдено в прод-коде.
type DeletionRefusalFacts struct {
	// Sites — кандидаты, найденные образцом прозы.
	Sites []DeletionRefusalSite
	// LaneNamedBy — какие полосы вообще названы прод-кодом, и где впервые.
	LaneNamedBy map[string]string
	// ServicesNamingLane — службы, чей прод-код называет полосу.
	ServicesNamingLane map[string]bool
	// LanesByService — какие ИМЕННО полосы называет каждая служба. Нужно
	// странице арендатора: она обязана называть то, что служба производит, и
	// молчать о том, чего у неё нет.
	LanesByService map[string]map[string]bool
	// ServicesAttaching — службы, чей прод-код зовёт refusal.Attach.
	ServicesAttaching map[string]bool
	// WrapOnForeignSentinel — вызовы refusal.Wrap поверх чего-то, что не является
	// sentinel'ом предусловия: полоса уехала бы с чужим кодом.
	WrapOnForeignSentinel []string
	// Parsed — сколько файлов разобрано.
	Parsed int
}

// ScanDeletionRefusalLanes разбирает прод-файлы Go и собирает факты.
//
// Разбор идёт по AST, а не поиском подстроки: имена полос и слово «refusal»
// стоят в комментариях этого дерева десятками — в том числе в шапке ЭТОГО
// файла, — и предикат по тексту зеленел бы на прозе о самом себе.
//
// serviceOf отвечает, какой службе принадлежит файл; пустая строка означает
// «файл не принадлежит ни одной службе» и в счёт служб не идёт.
func ScanDeletionRefusalLanes(files []string, serviceOf func(string) string, shorten func(string) string) (DeletionRefusalFacts, []string) {
	facts := DeletionRefusalFacts{
		LaneNamedBy:        map[string]string{},
		ServicesNamingLane: map[string]bool{},
		LanesByService:     map[string]map[string]bool{},
		ServicesAttaching:  map[string]bool{},
	}
	var parseErrs []string

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".pb.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0) // без комментариев — намеренно
		if err != nil {
			// Файл, который не разбирается, — не «ноль находок»: о нём надо
			// сказать, иначе перепись объявит осмотренным то, чего не читала.
			parseErrs = append(parseErrs, shorten(path)+": "+err.Error())
			continue
		}
		facts.Parsed++
		svc := serviceOf(path)
		rel := shorten(path)

		// Диапазоны выражений, приклеивающих полосу, и композитных литералов с
		// полем Lane. Литерал внутри такого диапазона считается помеченным.
		var laneRanges [][2]token.Pos

		// Типы, чей `Unwrap` называет полосу, и диапазоны всех методов каждого типа.
		//
		// Нужно типизированным отказам: у них текст живёт в `Error()`, а полоса —
		// в `Unwrap()`, и позиционной вложенности между ними нет by construction.
		// Требовать полосу у вызывающего значило бы назвать её ВТОРОЙ раз: вид
		// отказа уже назван самим типом, и два места разошлись бы молча.
		//
		// Правило узкое НАМЕРЕННО — ровно `Unwrap`, а не любой метод типа. Широкое
		// («у типа где-то названа полоса») отбеливало бы соседние литералы того же
		// получателя: в этом дереве у одного писателя живут и снятие ресурса, и
		// привязка, и второй уехал бы из-под наблюдения молча. Проверено замером:
		// широкое правило признало помеченными на четыре литерала больше, и все
		// четыре были отказами на ПРИВЯЗКУ.
		laneByReceiver := map[string]bool{}
		methodsOf := map[string][][2]token.Pos{}

		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
				continue
			}
			recv := laneReceiverTypeName(fd.Recv.List[0].Type)
			if recv == "" {
				continue
			}
			methodsOf[recv] = append(methodsOf[recv], [2]token.Pos{fd.Pos(), fd.End()})
			if fd.Name.Name != "Unwrap" {
				continue
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "refusal" {
					switch sel.Sel.Name {
					case "Protected", "HoldsChildren", "ReferredTo", "ReferredUnnamed":
						laneByReceiver[recv] = true
					}
				}
				return true
			})
		}
		for recv := range laneByReceiver {
			laneRanges = append(laneRanges, methodsOf[recv]...)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.SelectorExpr:
				if x, ok := v.X.(*ast.Ident); ok && x.Name == "refusal" {
					switch v.Sel.Name {
					case "Protected", "HoldsChildren", "ReferredTo", "ReferredUnnamed":
						if _, seen := facts.LaneNamedBy[v.Sel.Name]; !seen {
							facts.LaneNamedBy[v.Sel.Name] = rel
						}
						if svc != "" {
							facts.ServicesNamingLane[svc] = true
							if facts.LanesByService[svc] == nil {
								facts.LanesByService[svc] = map[string]bool{}
							}
							facts.LanesByService[svc][v.Sel.Name] = true
						}
					case "Attach":
						if svc != "" {
							facts.ServicesAttaching[svc] = true
						}
					}
				}
			case *ast.CallExpr:
				fn := laneExprText(fset, v.Fun)
				for _, m := range laneBearing {
					if strings.Contains(fn, m) {
						laneRanges = append(laneRanges, [2]token.Pos{v.Pos(), v.End()})
						break
					}
				}
				// Полоса на sentinel'е НЕ предусловия уедет с чужим кодом —
				// предел, названный в godoc refusal.Attach, держится здесь.
				if fn == "refusal.Wrap" && len(v.Args) == 3 {
					if bad := laneWrappedSentinel(fset, v.Args[2]); bad != "" {
						facts.WrapOnForeignSentinel = append(facts.WrapOnForeignSentinel,
							rel+": refusal.Wrap поверх "+bad+" — полоса отказа на снятие обязана "+
								"обёртывать sentinel предусловия, иначе признак уедет с чужим кодом")
					}
				}
			case *ast.CompositeLit:
				for _, el := range v.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Lane" {
						laneRanges = append(laneRanges, [2]token.Pos{v.Pos(), v.End()})
					}
				}
			}
			return true
		})

		ast.Inspect(file, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				return true
			}
			s, uerr := strconv.Unquote(bl.Value)
			if uerr != nil || len(s) > 200 || !deletionRefusalFamilies.MatchString(s) {
				return true
			}
			laned := false
			for _, r := range laneRanges {
				if bl.Pos() >= r[0] && bl.End() <= r[1] {
					laned = true
					break
				}
			}
			facts.Sites = append(facts.Sites, DeletionRefusalSite{
				File:  rel,
				Line:  fset.Position(bl.Pos()).Line,
				Text:  s,
				Laned: laned,
			})
			return true
		})
	}
	sort.Slice(facts.Sites, func(i, j int) bool {
		if facts.Sites[i].File != facts.Sites[j].File {
			return facts.Sites[i].File < facts.Sites[j].File
		}
		return facts.Sites[i].Line < facts.Sites[j].Line
	})
	sort.Strings(facts.WrapOnForeignSentinel)
	return facts, parseErrs
}

// laneWrappedSentinel отвечает, чем НЕ является третий аргумент refusal.Wrap:
// пустая строка — всё в порядке. Ищется имя sentinel'а предусловия в поддереве
// выражения; отсутствие — находка. Имя своё, а не общее: `wrappedSentinel` в
// этом пакете занята пробой полос резолва и отвечает на другой вопрос.
func laneWrappedSentinel(fset *token.FileSet, arg ast.Expr) string {
	found := false
	ast.Inspect(arg, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if namesPrecondition(v.Name) {
				found = true
			}
		case *ast.SelectorExpr:
			if namesPrecondition(v.Sel.Name) {
				found = true
			}
		}
		return !found
	})
	if found {
		return ""
	}
	return laneExprText(fset, arg)
}

// namesPrecondition — имя называет sentinel предусловия либо конструктор отказа
// предусловия (`ErrFailedPrecondition`, `failedPrecondition`, `failPrecondition`).
//
// Сверка идёт по ИМЕНИ, а не по типу: разрешение типов потребовало бы загрузки
// пакетов, а гейт обязан работать на синтетическом дереве инъекции, которое не
// собирается. Цена названа: имя, называющее предусловие иначе, гейт не узнает —
// и тогда законный вызов станет находкой, то есть ошибётся ГРОМКО, а не молча.
func namesPrecondition(name string) bool {
	return strings.Contains(strings.ToLower(name), "failedprecondition") ||
		strings.Contains(strings.ToLower(name), "failprecondition")
}

// laneReceiverTypeName — имя типа получателя метода, без указателя. Имя своё:
// `receiverTypeName` в этом пакете занята пробой другого предмета.
func laneReceiverTypeName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// laneExprText печатает выражение — для сверки формы вызова и для координаты в
// тексте находки. Имя своё, а не общее: `exprText` в этом пакете занята и
// печатает КООРДИНАТУ, а не текст, поэтому подмена была бы молчаливой.
func laneExprText(fset *token.FileSet, e ast.Expr) string {
	var sb strings.Builder
	if err := printer.Fprint(&sb, fset, e); err != nil {
		return ""
	}
	return sb.String()
}

// deletionRefusalExemption — запись ведомости: кандидат, который отказом на
// СНЯТИЕ не является.
type DeletionRefusalExemption struct {
	File string
	Text string
	Why  string
}

// DeletionRefusalExemptions — кандидаты образца, чей предмет ДРУГОЙ.
//
// Образец ищет ссылочную занятость вообще, а полоса — только отказ на СНЯТИЕ.
// Отказ на ПРИВЯЗКУ («слот занят», «CAS проиграл») говорит о том же факте другой
// стороной: вызывающий не снимает ресурс, а пытается его взять, и действие у него
// иное. Дать таким отказам полосу снятия значило бы объявить полосу там, где её
// предмета нет.
//
// Ведомость ИСТЕКАЕТ САМА: запись, чей литерал в дереве больше не найден, —
// находка, а не молчание. Иначе послабление пережило бы свой предмет и унесло
// с собой следующую слепую зону.
var DeletionRefusalExemptions = []DeletionRefusalExemption{
	{
		File: "services/storage/internal/repo/pg/volume_repo.go",
		Text: "%w: Volume %s is in use",
		Why:  "отказ на ПРИВЯЗКУ тома к машине (disambiguateAttach), а не на снятие тома",
	},
	{
		File: "services/vpc/internal/apps/kacho/services/nicinternal/service.go",
		Text: "NetworkInterface is in use",
		Why:  "отказ на ПРИВЯЗКУ интерфейса к машине, а не на снятие интерфейса",
	},
	{
		File: "services/vpc/internal/repo/kacho/pg/address.go",
		Text: "%w: address already referenced by another resource",
		Why:  "проигранный CAS привязки адреса, а не отказ на снятие адреса",
	},
	{
		File: "services/vpc/internal/repo/kacho/pg/route_table.go",
		Text: "%w: %s: Gateway %s is attached to another network",
		Why:  "проверка ссылки на статический маршрут при правке таблицы, а не снятие шлюза",
	},
	{
		File: "services/compute/internal/ports/portmock/portmock.go",
		Text: "NetworkInterface is in use",
		Why:  "дублёр отказа на ПРИВЯЗКУ интерфейса — повторяет настоящий, у которого полосы снятия нет",
	},
	{
		File: "services/compute/internal/ports/portmock/portmock.go",
		Text: "Volume is in use",
		Why:  "дублёр отказа на ПРИВЯЗКУ тома — повторяет настоящий, у которого полосы снятия нет",
	},
}

// DeletionRefusalFindings — суждение о собранных фактах, отдельно от их сбора.
//
// Возвращает находки и число записей ведомости, которым в дереве нечего
// исключать.
// Ведомость передаётся ПАРАМЕТРОМ, а не читается из пакета: инъекция подаёт
// судье синтетическое дерево со своей ведомостью и проверяет, что он краснеет по
// существу. Судья, читающий ведомость дерева на синтетике, объявил бы все её
// записи потерявшими предмет — и инъекция утверждала бы не то, что проверяет.
func DeletionRefusalFindings(facts DeletionRefusalFacts, services []string, exemptions []DeletionRefusalExemption) (findings []string, staleExemptions []string) {
	exempt := map[string]string{}
	used := map[string]bool{}
	for _, e := range exemptions {
		exempt[e.File+"\x00"+e.Text] = e.Why
	}

	for _, s := range facts.Sites {
		key := s.File + "\x00" + s.Text
		if _, ok := exempt[key]; ok {
			used[key] = true
			continue
		}
		if !s.Laned {
			findings = append(findings, s.File+":"+strconv.Itoa(s.Line)+
				" — отказ на снятие без полосы: "+strconv.Quote(s.Text)+
				"; клиенту остаётся разбирать прозу")
		}
	}
	for _, e := range exemptions {
		if !used[e.File+"\x00"+e.Text] {
			staleExemptions = append(staleExemptions, e.File+" "+strconv.Quote(e.Text)+
				" — записи нечего исключать: литерала в дереве нет, послабление пережило свой предмет")
		}
	}

	// Полоса без производителя — мёртвая запись словаря.
	for _, lane := range []string{"Protected", "HoldsChildren", "ReferredTo", "ReferredUnnamed"} {
		if facts.LaneNamedBy[lane] == "" {
			findings = append(findings, "полоса refusal."+lane+
				" не названа НИ ОДНИМ производителем прод-кода — запись словаря мертва")
		}
	}

	// Служба, называющая полосу и не приклеивающая её: признак чеканится и
	// теряется по дороге, а отказ приходит клиенту без него.
	for _, svc := range services {
		if facts.ServicesNamingLane[svc] && !facts.ServicesAttaching[svc] {
			findings = append(findings, svc+
				" — прод-код называет полосу отказа, но нигде не зовёт refusal.Attach: "+
				"признак чеканится и теряется в классификации, клиент его не увидит")
		}
	}

	findings = append(findings, facts.WrapOnForeignSentinel...)
	sort.Strings(findings)
	sort.Strings(staleExemptions)
	return findings, staleExemptions
}

// ─── страница арендатора обязана называть то, что служба ПРОИЗВОДИТ ──────────

// laneDocClaim — как полоса называется на странице арендатора.
//
// Ключ — имя полосы в коде, значение — то, что страница обязана написать. Пара
// объявлена ЗДЕСЬ, рядом со словарём, а не в проверке: два места об одном
// предмете разошлись бы молча, и разошлось бы то, которое не считали.
var laneDocClaim = map[string][2]string{
	"Protected":       {"`DELETION_PROTECTED`", ""},
	"HoldsChildren":   {"`REFERENCE_IN_USE`", "`children`"},
	"ReferredTo":      {"`REFERENCE_IN_USE`", "`referrers`"},
	"ReferredUnnamed": {"`REFERENCE_IN_USE`", "`unnamed`"},
}

// DeletionRefusalDocFindings сверяет страницу арендатора с тем, что служба
// производит, В ОБЕ СТОРОНЫ.
//
// Полоса без строки на странице — арендатор не узнает признака, ради которого
// всё и делалось. Строка без полосы — обещание, которого продукт не исполняет
// ни при каком входе: та самая «неисполнимая возможность», только в документе.
//
// Судит НАЛИЧИЕ УПОМИНАНИЯ, а не формулировку: тон страницы принадлежит автору,
// и гейт, требующий дословности, краснел бы на законной правке прозы. Предел
// назван прямо — гейт не проверяет, что написанное ВЕРНО по существу; это
// суждение о прозе, у которого машинного предиката нет.
func DeletionRefusalDocFindings(lanesByService map[string]map[string]bool, pageOf map[string]string) []string {
	var findings []string
	for svc, page := range pageOf {
		lanes := lanesByService[svc]
		for lane, want := range laneDocClaim {
			// ЧЕМ полоса опознаётся на странице: у защиты от удаления — своим
			// токеном, у трёх полос ссылки — ВИДОМ ссылки. Токен у них общий, и
			// судить по нему значило бы засчитать соседку за эту.
			mark := want[1]
			if mark == "" {
				mark = want[0]
			}
			said := strings.Contains(page, mark)
			switch {
			case lanes[lane] && !said:
				findings = append(findings, svc+
					" — производит полосу refusal."+lane+", а страница арендатора о ней молчит ("+
					mark+" на странице нет): признак доезжает до клиента необъяснённым")
			case !lanes[lane] && said:
				findings = append(findings, svc+
					" — страница арендатора обещает полосу refusal."+lane+" ("+mark+
					"), а производителя у неё нет: обещание неисполнимо ни при каком входе")
			}
		}
	}
	sort.Strings(findings)
	return findings
}
