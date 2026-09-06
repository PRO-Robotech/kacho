// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenclaimforeignbrand.go — разбор ИМЁН КЛЕЙМ выпущенного токена: чем
// удостоверение называет своего издателя предъявителю и всякому, кто токен
// разбирает.
//
// # Предмет
//
// Служба управления доступом — самостоятельный продукт, ставящийся в чужом
// облаке. Норма разделения (решение владельца, `kacho#2076`): продукт наследует
// КОД, но не ИМЯ; своё — всё, чем он себя называет. Имя клейма читается
// оператором чужого облака БЕЗ нашего исходного кода — достаточно раскодировать
// токен, — поэтому приставка имени клейма есть идентичность, а не код.
//
// # Что здесь считается ИМЕНЕМ КЛЕЙМА, а что просто похожей строкой
//
// Приставку имени платформы носят ещё три словаря — метрики
// (`kacho_vpc_outbox_backlog_depth`), схемы и таблицы (`kacho_iam`), типы
// ресурсов модуля инфраструктуры (`kacho_vpc_network`). Все три законны и
// остаются платформе: их читает не предъявитель токена, а оператор ПЛАТФОРМЫ.
// Значит распознаватель обязан отличать клеймо от однофамильца, и отличает он
// его ПОЗИЦИЕЙ, а не перечнем имён: перечень стареет молча, а позиция —
// свойство кода.
//
// Клеймо стоит ровно в четырёх позициях, и все четыре читаются разбором:
//
//	claims := map[string]any{"kacho_user_id": …}     ← ключ состава
//	pt, _ := claims["kacho_principal_type"].(string) ← чтение по имени
//	case "kacho_mfa_at":                             ← разбор по имени
//	verifiedClaim(vt, "kacho_principal_type")        ← имя передано вызовом
//
// Пятая форма — сама ПРИСТАВКА, отданная предикату
// (`strings.HasPrefix(k, "kacho_")`). Она опаснее прочих: переносит целый
// словарь разом и при смене имён клейм молча перестаёт совпадать — читатель не
// отказывает, он просто ничего не находит.
//
// # Ось Б: имя, у которого в дереве есть двойник в СВОЁМ словаре
//
// Разбор позиций читает Go. Клеймо живёт и вне Go — в посевных наборах, в
// перечне принимаемых клейм профиля развёртывания, в собранной коллекции проб,
// на клиентской странице. Там позиции нет, и судить по ней нечего.
//
// Поэтому вторая ось судит ПАРУ: если в дереве есть клеймо своего словаря
// `<своё>_X`, то токен `<чужое>_X` в любом отслеживаемом тексте — находка.
// Словарь пар не выписан, а ВЫВЕДЕН из первой оси, поэтому он не стареет: снято
// клеймо — снято и правило о нём. Однофамильцы под неё не подпадают
// by construction: у метрики `kacho_vpc_build_info` двойника в словаре клейм
// нет и быть не может.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. имя клейма, собранное из частей либо взятое переменной;
//  2. имя клейма, стоящее в чужом языке и НЕ имеющее двойника в своём
//     словаре, — то есть клеймо, которое чеканит кто-то, кроме нас;
//  3. приставка, записанная не литералом (переменной, полем настройки).
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// claimNameShape — форма имени клейма: словарь, подчёркивание, тело из строчных
// букв, цифр и подчёркиваний. Судится ЦЕЛИКОМ, поэтому строка запроса, внутри
// которой имя лишь встречается, формой не является.
var claimNameShape = regexp.MustCompile(`^([a-z]+)_([a-z0-9_]+)$`)

// ClaimNameForm — в какой позиции стоит имя.
type ClaimNameForm string

const (
	// ClaimFormKey — ключ состава: место, где клеймо ЧЕКАНЯТ.
	ClaimFormKey ClaimNameForm = "ключ состава"
	// ClaimFormRead — чтение по имени: `claims["…"]`.
	ClaimFormRead ClaimNameForm = "чтение по имени"
	// ClaimFormCase — разбор по имени: `case "…":`.
	ClaimFormCase ClaimNameForm = "разбор по имени"
	// ClaimFormArg — имя, переданное вызовом.
	ClaimFormArg ClaimNameForm = "имя в вызове"
	// ClaimFormConst — имя, объявленное константой: единственный дом имени,
	// которым пользуются оба конца.
	ClaimFormConst ClaimNameForm = "объявление константы"
	// ClaimFormPrefix — сама приставка, отданная предикату.
	ClaimFormPrefix ClaimNameForm = "приставка в предикате"
)

// ClaimNameUse — одно употребление имени клейма.
type ClaimNameUse struct {
	File string
	Line int
	// Func — функция, внутри которой стоит имя: по номеру строки читатель
	// отказа не поймёт, что именно называет клеймо.
	Func string
	// Namespace — словарь имени: первый сегмент до подчёркивания.
	Namespace string
	// Name — имя целиком.
	Name string
	Form ClaimNameForm
}

// ClaimNameCensus — объём осмотренного.
type ClaimNameCensus struct {
	// Literals — строковых литералов прочитано.
	Literals int
	// Shaped — из них имеющих форму имени клейма.
	Shaped int
	// Positions — из них стоящих в позиции клейма.
	Positions int
	// ByForm — позиций по формам.
	ByForm map[ClaimNameForm]int
}

// prefixPredicates — вызовы, чей строковый довод есть ПРИСТАВКА словаря, а не
// имя. Перечень закрыт: предикат, заведённый и сюда не внесённый, останется вне
// наблюдения — и это видно по счётчику формы, а не молчит.
var prefixPredicates = map[string]bool{
	"HasPrefix":   true,
	"TrimPrefix":  true,
	"CutPrefix":   true,
	"TrimSuffix":  false,
	"HasSuffix":   false,
	"CutSuffix":   false,
	"EqualFold":   false,
	"SplitPrefix": false,
}

// ScanTokenClaimNames разбирает один файл Go и собирает имена клейм по позициям.
//
// namespaces — словари, чьи имена считаются именами клейм. Имя вне них формой не
// обладает: словарь и есть то, чем клеймо отличается от однофамильца.
func ScanTokenClaimNames(path string, src []byte, namespaces map[string]bool) (
	[]ClaimNameUse, ClaimNameCensus, error,
) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, ClaimNameCensus{}, err
	}
	census := ClaimNameCensus{ByForm: map[ClaimNameForm]int{}}
	var out []ClaimNameUse

	// Позиции собираются ДО обхода литералов: одно и то же имя стоит в одной
	// позиции, и решать о нём надо по узлу-родителю, а не по самому литералу.
	positions := map[*ast.BasicLit]ClaimNameForm{}
	prefixArgs := map[*ast.BasicLit]bool{}

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			mt, ok := node.Type.(*ast.MapType)
			if !ok {
				return true
			}
			if id, ok := mt.Key.(*ast.Ident); !ok || id.Name != "string" {
				return true
			}
			for _, elt := range node.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if lit, ok := kv.Key.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					positions[lit] = ClaimFormKey
				}
			}
		case *ast.IndexExpr:
			if lit, ok := node.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				positions[lit] = ClaimFormRead
			}
		case *ast.CaseClause:
			for _, e := range node.List {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					positions[lit] = ClaimFormCase
				}
			}
		case *ast.ValueSpec:
			for _, v := range node.Values {
				if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					positions[lit] = ClaimFormConst
				}
			}
		case *ast.CallExpr:
			callee := ""
			switch fun := node.Fun.(type) {
			case *ast.SelectorExpr:
				callee = fun.Sel.Name
			case *ast.Ident:
				callee = fun.Name
			}
			for _, a := range node.Args {
				lit, ok := a.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if prefixPredicates[callee] {
					prefixArgs[lit] = true
					continue
				}
				if _, taken := positions[lit]; !taken {
					positions[lit] = ClaimFormArg
				}
			}
		}
		return true
	})

	// Функция, внутри которой стоит литерал, — по диапазону позиций объявления.
	type fnSpan struct {
		from, to token.Pos
		name     string
	}
	var spans []fnSpan
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			spans = append(spans, fnSpan{fn.Pos(), fn.End(), functionQualifiedName(fn)})
		}
	}
	enclosing := func(p token.Pos) string {
		for _, s := range spans {
			if p >= s.from && p < s.to {
				return s.name
			}
		}
		return "уровень пакета"
	}

	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		census.Literals++
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		line := fset.Position(lit.Pos()).Line

		// Приставка словаря, отданная предикату: `"kacho_"`.
		if prefixArgs[lit] {
			ns, isPrefix := strings.CutSuffix(v, "_")
			if isPrefix && ns != "" && namespaces[ns] {
				census.Shaped++
				census.Positions++
				census.ByForm[ClaimFormPrefix]++
				out = append(out, ClaimNameUse{
					File: path, Line: line, Func: enclosing(lit.Pos()),
					Namespace: ns, Name: v, Form: ClaimFormPrefix,
				})
			}
			return true
		}

		m := claimNameShape.FindStringSubmatch(v)
		if m == nil || !namespaces[m[1]] {
			return true
		}
		census.Shaped++
		form, stands := positions[lit]
		if !stands {
			return true
		}
		census.Positions++
		census.ByForm[form]++
		out = append(out, ClaimNameUse{
			File: path, Line: line, Func: enclosing(lit.Pos()),
			Namespace: m[1], Name: v, Form: form,
		})
		return true
	})

	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, census, nil
}

// ClaimFileScan — что разбор нашёл в одном файле.
type ClaimFileScan struct {
	// Assembled — ключи СОСТАВОВ: место, где клеймо чеканят. Это семя словаря,
	// и оно единственное, о чём разбор знает без вывода.
	Assembled []string
	// Uses — все позиции имени клейма в этом файле.
	Uses []ClaimNameUse
}

// ClaimVocabulary — словарь клейм, ВЫВЕДЕННЫЙ из дерева.
type ClaimVocabulary struct {
	// Names — имя клейма → первое его употребление.
	Names map[string]ClaimNameUse
	// Minted — из них те, что стоят ключом состава: их продукт чеканит сам.
	Minted map[string]bool
	// Files — файлы, признанные несущими клеймо.
	Files map[string]bool
	// Rounds — за сколько кругов словарь перестал расти.
	Rounds int
}

// DeriveClaimVocabulary выводит словарь клейм как СВЯЗНУЮ КОМПОНЕНТУ, посеянную
// местом чеканки.
//
// # Почему вывод, а не перечень
//
// Приставку словаря носят ещё три вокабуляра — метрики, схемы и типы ресурсов
// модуля инфраструктуры. Отличить клеймо от однофамильца ни формой имени, ни
// позицией нельзя: и метрика бывает ключом отображения, и схема бывает доводом
// вызова. Выписанный перечень имён отличил бы — и устарел бы молча, а
// устаревает он ровно там, где заводят новое клеймо.
//
// Связная компонента отличает их ПО СУЩЕСТВУ: клеймо есть то, что связано с
// местом чеканки цепочкой совместного употребления. Метрика в эту цепочку не
// входит by construction — файл, называющий метрику, не называет ни одного
// клейма, поэтому в область не попадает вовсе.
//
// # Правило роста — два шага, повторяемые до неподвижности
//
//  1. файл, назвавший имя из словаря, входит в область;
//  2. имя, стоящее в позиции клейма внутри файла области, входит в словарь.
//
// Так словарь добирает то, чего у чеканки нет: имя, объявленное константой в
// своём файле, и имя, которое только ЧИТАЮТ, ни разу не чеканя.
func DeriveClaimVocabulary(files map[string]ClaimFileScan) ClaimVocabulary {
	v := ClaimVocabulary{
		Names:  map[string]ClaimNameUse{},
		Minted: map[string]bool{},
		Files:  map[string]bool{},
	}
	// Обход — по УПОРЯДОЧЕННЫМ путям: неподвижная точка от порядка не зависит,
	// а число кругов зависит, и печатается оно переписью. Перепись, гуляющая
	// от прогона к прогону, читается как признак дефекта, которого нет.
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// Семя: ключи составов.
	for _, path := range paths {
		fs := files[path]
		if len(fs.Assembled) == 0 {
			continue
		}
		v.Files[path] = true
		for _, k := range fs.Assembled {
			v.Minted[k] = true
			if _, seen := v.Names[k]; !seen {
				v.Names[k] = ClaimNameUse{File: path, Name: k, Form: ClaimFormKey}
			}
		}
	}
	for {
		v.Rounds++
		grew := false
		for _, path := range paths {
			fs := files[path]
			if !v.Files[path] {
				// Шаг 1: файл входит в область, назвав известное имя.
				for _, u := range fs.Uses {
					if u.Form == ClaimFormPrefix {
						continue
					}
					if _, known := v.Names[u.Name]; known {
						v.Files[path] = true
						grew = true
						break
					}
				}
			}
			if !v.Files[path] {
				continue
			}
			// Шаг 2: имена файла области входят в словарь.
			for _, u := range fs.Uses {
				if u.Form == ClaimFormPrefix {
					continue
				}
				known, seen := v.Names[u.Name]
				if !seen {
					v.Names[u.Name] = u
					grew = true
					continue
				}
				// Семя знает имя, но не строку: ключи составов приходят
				// перечнем. Первое употребление с координатой её и даёт —
				// иначе отказ называет файл без строки.
				if known.Line == 0 && u.Line != 0 {
					v.Names[u.Name] = u
				}
			}
		}
		if !grew || v.Rounds > len(files)+2 {
			return v
		}
	}
}

// ForeignTwin — имя того же клейма в другом словаре.
//
// Пустая строка означает, что имя чужому словарю не принадлежит и двойника у
// него нет.
func ForeignTwin(name, from, to string) string {
	body, ok := strings.CutPrefix(name, from+"_")
	if !ok || body == "" {
		return ""
	}
	return to + "_" + body
}

// ClaimTwinHit — совпадение имени клейма из ЧУЖОГО словаря в тексте.
type ClaimTwinHit struct {
	// Twin — найденное имя.
	Twin string
	// Of — имя того же клейма в словаре, из которого двойник выведен.
	Of string
	// Line — строка, считая с единицы.
	Line int
}

// FindClaimTwins ищет в тексте двойники имён клейм.
//
// Читает ЛЮБОЙ отслеживаемый текст, а не только Go: клеймо живёт в посевных
// наборах, в перечне принимаемых клейм профиля развёртывания, в собранной
// коллекции проб и на клиентской странице. Позиции там нет, и судить по ней
// нечего, — поэтому судится пара имён, выведенная из места чеканки.
func FindClaimTwins(text string, twins map[string]string) []ClaimTwinHit {
	var out []ClaimTwinHit
	for twin, of := range twins {
		for idx := 0; ; {
			at := strings.Index(text[idx:], twin)
			if at < 0 {
				break
			}
			abs := idx + at
			idx = abs + len(twin)
			if !claimTokenBoundary(text, abs, len(twin)) {
				continue
			}
			out = append(out, ClaimTwinHit{
				Twin: twin, Of: of, Line: 1 + strings.Count(text[:abs], "\n"),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Twin < out[j].Twin
	})
	return out
}

// claimTokenBoundary — совпадение стоит ОТДЕЛЬНЫМ токеном, а не куском чужого
// имени. Без этой проверки `kaname_account` находился бы внутри
// `kaname_account_id`, и находка называла бы имя, которого в тексте нет.
func claimTokenBoundary(text string, at, length int) bool {
	isWord := func(b byte) bool {
		return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9')
	}
	if at > 0 && isWord(text[at-1]) {
		return false
	}
	if end := at + length; end < len(text) && isWord(text[end]) {
		return false
	}
	return true
}

// isMembershipSet — отображение есть МНОЖЕСТВО, а не состав.
//
// У множества значения нет: `bool` и пустой `struct{}` — две записи одного
// приёма, где величина стоит ключом, а значение лишь помечает членство. Состав
// клейм отображает имя в величину, которую несёт токен, поэтому знаком членства
// его значение не бывает.
//
// Перечень закрыт ДВУМЯ записями намеренно: третья форма множества, заведённая и
// сюда не внесённая, останется семенем — то есть ошибётся в сторону лишнего
// имени в словаре, которое видно перечнем, а не в сторону молчания.
func isMembershipSet(mt *ast.MapType) bool {
	switch v := mt.Value.(type) {
	case *ast.Ident:
		return v.Name == "bool"
	case *ast.StructType:
		return v.Fields == nil || len(v.Fields.List) == 0
	}
	return false
}

// ScanClaimMint собирает ключи СОСТАВОВ — семя словаря клейм.
//
// # Чем состав отличается от таблицы перевода
//
// Приставку словаря носят и таблицы перевода — например, тип ресурса модуля
// инфраструктуры в имя ресурса платформы. Такая таблица тоже есть отображение
// со строковым ключом и тоже несёт три и больше ключей приставки, поэтому по
// одному лишь числу ключей она неотличима от чеканки — и, будучи принятой за
// семя, посеяла бы словарь клейм именами, которые клеймами не являются. Дальше
// гейт потребовал бы переименовать то, что переименовывать нельзя, и был бы снят
// первым же, кто на него наткнулся.
//
// Различает ЗНАЧЕНИЕ: состав клейм несёт величины, вычисляемые на выпуске —
// идентификатор, отметку времени, признак, — тогда как таблица перевода
// отображает постоянное в постоянное. Поэтому семенем считается литерал, у
// которого ХОТЯ БЫ ОДНО значение не есть строковый литерал.
//
// # МНОЖЕСТВО — не состав, и его значение не есть величина
//
// Предсказанное выше НАСТУПИЛО, и наступило через 21 минуту после посадки гейта:
// ведомость `map[string]bool{"kacho_quota_admit": true, …}` — закрытый перечень
// функций схемы общего фундамента — прошла проверкой на «вычисляемое значение»,
// потому что `true` есть узел-имя, а не строковый литерал. Семь имён схемы
// вошли в словарь клейм, и гейт потребовал переименовать применённую миграцию.
//
// Различает то же ЗНАЧЕНИЕ, только спрошенное точнее: у множества значения нет
// вовсе. `map[string]bool` и `map[string]struct{}` — две записи одного приёма:
// величина здесь ключ, а значение — постоянный знак членства. Состав же клейм
// отображает имя в ВЕЛИЧИНУ, которую несёт токен, поэтому его значение
// разнородно (`any`) либо содержательно, но никогда не есть знак членства.
//
// Замерено по дереву, а не выведено: мест, признаваемых составом, шесть — пять
// со значением `any`, все в месте чеканки, и одно со значением `bool`, и это
// ведомость. Единица счёта — составной литерал в не-тестовом отслеживаемом Go.
//
// # Чего это НЕ видит — названо, а не спрятано
//
// Таблица перевода, чьи значения вычисляются и НЕ суть знак членства, семенем
// станет; её имена войдут в словарь, и это будет видно по перечню выведенных
// имён, а не молча. Обратно: состав клейм, записанный множеством, семенем НЕ
// станет — но клеймом величина, у которой значения нет, и не бывает.
func ScanClaimMint(path string, src []byte, namespaces map[string]bool, minKeys int) (
	[]string, error,
) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		mt, ok := cl.Type.(*ast.MapType)
		if !ok {
			return true
		}
		if id, ok := mt.Key.(*ast.Ident); !ok || id.Name != "string" {
			return true
		}
		if isMembershipSet(mt) {
			return true
		}
		var (
			keys     = map[string]bool{}
			computed bool
		)
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if lit, ok := kv.Value.(*ast.BasicLit); !ok || lit.Kind != token.STRING {
				computed = true
			}
			lit, ok := kv.Key.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			m := claimNameShape.FindStringSubmatch(name)
			if m == nil || !namespaces[m[1]] {
				continue
			}
			keys[name] = true
		}
		if !computed || len(keys) < minKeys {
			return true
		}
		for k := range keys {
			out = append(out, k)
		}
		return true
	})
	sort.Strings(out)
	return out, nil
}
