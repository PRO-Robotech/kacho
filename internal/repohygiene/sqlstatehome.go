// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sqlstatehome.go — разбор двух сторон одного предмета: где принимается решение
// о классе отказа хранилища и чем отвечает ветка по умолчанию.
//
// # Почему разбор, а не поиск по образцу
//
// Токен `"23505"` встречается в этом дереве **преимущественно в прозе**: замер на
// ревизии заведения — файлов с токеном 85, из них решение принимают 18, а
// остальные 67 называют код в комментарии, документируя маршрут. Поиск по
// подстроке считал бы объяснение за исполнение и краснел бы на собственной шапке;
// разбор синтаксического дерева видит строковый литерал и не видит комментария
// **by construction**.
//
// # Три стороны, и они РАЗНЫЕ
//
//  1. `ScanSQLStateLiterals` — где код целостности превращается в решение. Дом
//     этого решения обязан быть один; всё остальное — либо перевод на дом, либо
//     названное отступление.
//  2. `ScanStatusMappers` — чем отвечает ветка по умолчанию отображения
//     «род отказа → код gRPC». Она обязана давать ФИКСИРОВАННЫЙ текст, а не эхо
//     ошибки хранилища (`security.md` §Hardening-инварианты, п.1).
//  3. `ScanTextFaultDecisions` — где род отказа решается по СЛОВАМ сервера, а не
//     по коду. Третья сторона заведена задачей #1455; до неё этот класс стоял
//     здесь же в перечне невидимого, и запись честно называла его «другим
//     классом» — но названная слепая зона остаётся слепой.
//
// Стороны разведены намеренно: первая говорит о МЕСТЕ решения, вторая — о ТЕКСТЕ
// его исхода, третья — о ПРИЗНАКЕ, по которому решение принято. Проверка,
// склеившая их в одну, отвечала бы одним ответом на три вопроса и молчала бы,
// когда сломан ровно один.
//
// # Почему третья сторона не покрывается первой
//
// Первая судит решение по КОДУ и о решении по ТЕКСТУ не утверждает ничего:
// литерал `"SQLSTATE 23505"` коду `"23505"` НЕ равен — это строка из
// восемнадцати знаков, которой в перечне кодов нет. Гейт места на ней молчит
// by construction, и молчал ровно столько, сколько существовал.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **Код, собранный из частей** (`"235" + "05"`, `fmt.Sprintf`) — литералом не
//     является, и восстанавливать значение вычислением разбор не берётся.
//  2. **Код, пришедший переменной** (прочитанный из настройки, из таблицы) —
//     разбор судит выражение по месту, а не по потоку значений.
//  3. **Слова сервера, собранные из частей** — вокабуляр сверяется с ЛИТЕРАЛОМ;
//     фраза, склеенная из переменных, литералом не является. Та же граница, что
//     у первого пункта, и по той же причине.
//
// Прежде здесь стояла и четвёртая запись — «своя классификация по тексту
// сообщения СУБД»; её предмет закрыт третьей стороной разбора, и запись снята
// вместе с ним.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// IntegritySQLStates — коды, которые корпус правил называет ОДНИМ правилом
// (`data-integrity.md` §«Within-service инварианты», таблица SQLSTATE→gRPC), плюс
// смежные, которые сервисы дерева уже разбирают наравне с ними.
//
// Перечень закрыт и не покрывает ни собственных кодов продукта (полоса учёта
// величин), ни кодов доступности сервера: у тех другой предмет и другой владелец,
// и требовать для них того же дома значило бы расширить запрет за границу
// правила, которым он обоснован.
var IntegritySQLStates = map[string]string{
	"23000": "integrity_constraint_violation",
	"23502": "not_null_violation",
	"23503": "foreign_key_violation",
	"23505": "unique_violation",
	"23514": "check_violation",
	"23P01": "exclusion_violation",
	"22P02": "invalid_text_representation",
	"40001": "serialization_failure",
	"40P01": "deadlock_detected",
}

// SQLStateSite — координата исполняемого вхождения кода целостности.
type SQLStateSite struct {
	File string
	Line int
	// Code — сам код, как он записан в исходнике.
	Code string
	// Func — функция, в теле которой стоит вхождение. Пусто для вхождения вне
	// функции (объявление константы уровня пакета). Гейт судит по функции, а не
	// по файлу: у одного файла бывает и переведённая часть, и та, что законно
	// осталась своей.
	Func string
}

// SQLStateCensus — объём осмотренного одним файлом.
type SQLStateCensus struct {
	// Literals — строковых литералов осмотрено всего. Печатается, чтобы «кодов
	// не найдено» было отличимо от «литералов не читалось».
	Literals int
	// Funcs — объявлений функций прочитано.
	Funcs int
}

// ScanSQLStateLiterals находит исполняемые вхождения кодов целостности в одном
// файле.
func ScanSQLStateLiterals(path string, src []byte) (sites []SQLStateSite, census SQLStateCensus, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return nil, SQLStateCensus{}, perr
	}

	// Карта «строка → имя функции»: вхождение приписывается той функции, в чьих
	// границах оно стоит. Строится по объявлениям, а не догадкой по отступу.
	type span struct {
		from, to int
		name     string
	}
	var spans []span
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		census.Funcs++
		spans = append(spans, span{
			from: fset.Position(fn.Pos()).Line,
			to:   fset.Position(fn.End()).Line,
			name: fn.Name.Name,
		})
	}
	funcAt := func(line int) string {
		best := ""
		bestWidth := 1 << 30
		for _, s := range spans {
			if line >= s.from && line <= s.to && s.to-s.from < bestWidth {
				best, bestWidth = s.name, s.to-s.from
			}
		}
		return best
	}

	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		census.Literals++
		val := strings.Trim(lit.Value, "\"`")
		if _, isCode := IntegritySQLStates[val]; !isCode {
			return true
		}
		line := fset.Position(lit.Pos()).Line
		sites = append(sites, SQLStateSite{File: path, Line: line, Code: val, Func: funcAt(line)})
		return true
	})

	sort.Slice(sites, func(i, j int) bool { return sites[i].Line < sites[j].Line })
	return sites, census, nil
}

// StatusMapper — функция, отображающая ОШИБКУ в код gRPC.
//
// Опознаётся ДВУМЯ признаками сразу, и оба несущие:
//
//  1. принимает значение типа `error` — то есть её предмет и есть чужая ошибка.
//     Без этого признака в перечень попадали бы обычные обработчики, чей
//     терминальный возврат подставляет свои же значения (номер элемента, имя
//     поля, идентификатор из запроса), — и все находки оказывались бы ложными:
//     проверено на дереве, шесть из шести;
//  2. производит БОЛЬШЕ ОДНОГО кода gRPC — одна ветка есть отказ конкретного
//     места, а не таблица родов.
//
// Опознание по ИМЕНИ (`MapRepoErr`, `ToStatus`, `mapErr`) отвергнуто: имя задаёт
// автор, и восьмое отображение он назовёт иначе.
type StatusMapper struct {
	File string
	Line int
	Func string
	// Codes — сколько РАЗНЫХ кодов gRPC функция производит.
	Codes int
	// TailLine — строка терминального возврата (последнего `return` тела).
	TailLine int
	// TailFixed — терминальный возврат НЕ выносит наружу принятую ошибку.
	TailFixed bool
	// TailText — что именно там стоит, для текста находки.
	TailText string
	// TailIsStatus — терминальный возврат вообще является `status.Error(...)`.
	// Функция, чей хвост — не статус (делегирование соседу, возврат ошибки как
	// есть), о ветке по умолчанию ничего не утверждает, и обвинять её не в чем.
	TailIsStatus bool
}

// StatusMapperCensus — объём осмотренного одним файлом.
type StatusMapperCensus struct {
	// Funcs — объявлений функций прочитано.
	Funcs int
	// ErrTaking — из них принимающих `error`. Печатается отдельно: это
	// знаменатель предмета, и его обвал означает сломанный разбор сигнатур, а не
	// чистое дерево.
	ErrTaking int
	// StatusCall — вызовов status.Error/Errorf осмотрено.
	StatusCall int
}

// ScanStatusMappers находит отображения «ошибка → код gRPC» и разбирает их
// терминальный возврат.
func ScanStatusMappers(path string, src []byte) (mappers []StatusMapper, census StatusMapperCensus, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return nil, StatusMapperCensus{}, perr
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		census.Funcs++

		errNames := errorParams(fn)
		if len(errNames) == 0 {
			continue
		}
		census.ErrTaking++

		seen := map[string]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if code, _, ok := statusErrorCall(n); ok {
				census.StatusCall++
				seen[code] = true
			}
			return true
		})
		if len(seen) < 2 {
			continue
		}

		m := StatusMapper{
			File:  path,
			Line:  fset.Position(fn.Pos()).Line,
			Func:  fn.Name.Name,
			Codes: len(seen),
		}
		// Терминальный возврат — последний оператор тела, если это `return`.
		// Именно он достигается, когда ни одна ветка рода не сработала.
		if n := len(fn.Body.List); n > 0 {
			if ret, ok := fn.Body.List[n-1].(*ast.ReturnStmt); ok && len(ret.Results) > 0 {
				m.TailLine = fset.Position(ret.Pos()).Line
				last := ret.Results[len(ret.Results)-1]
				if call, msg, ok := statusErrorCall(last); ok {
					_ = call
					m.TailIsStatus = true
					m.TailFixed, m.TailText = fixedMessage(last, msg, errNames)
				}
			}
		}
		mappers = append(mappers, m)
	}

	sort.Slice(mappers, func(i, j int) bool { return mappers[i].Line < mappers[j].Line })
	return mappers, census, nil
}

// errorParams — имена параметров функции, имеющих тип `error`.
//
// Разрешается СИНТАКСИЧЕСКИ, по сигнатуре, а не выводом типов: `error` — имя
// встроенного типа, и переопределяют его в этом дереве ноль раз. Цена решения
// названа: значение ошибки, полученное внутри тела через `:=`, в этот набор не
// попадает, и подстановка такого значения останется невидимой. Это слепая зона,
// а не «редкий случай», — но у ОТОБРАЖЕНИЯ чужой ошибки предмет приходит
// параметром by construction, иначе отображать было бы нечего.
func errorParams(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	if fn.Type == nil || fn.Type.Params == nil {
		return out
	}
	for _, field := range fn.Type.Params.List {
		id, ok := field.Type.(*ast.Ident)
		if !ok || id.Name != "error" {
			continue
		}
		for _, name := range field.Names {
			out[name.Name] = true
		}
	}
	return out
}

// statusErrorCall — вызов `status.Error`/`status.Errorf` с первым аргументом
// `codes.<X>`; возвращает имя кода и выражение сообщения (для `Errorf` с
// подстановками — nil, разбор аргументов делает вызывающий).
//
// Приведение к паре «пакет.функция» здесь не делается намеренно: пакет
// `google.golang.org/grpc/status` в этом дереве не переименовывают, а разбор
// импортов ради него удлинил бы проверку без выигрыша. Цена названа: чужой
// одноимённый помощник `status.Error` был бы засчитан — и это дало бы ЛИШНЮЮ
// находку, а не пропуск, то есть ошибку в сторону осторожности.
func statusErrorCall(n ast.Node) (code string, msg ast.Expr, ok bool) {
	call, isCall := n.(*ast.CallExpr)
	if !isCall || len(call.Args) < 2 {
		return "", nil, false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return "", nil, false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	if !isIdent || pkg.Name != "status" {
		return "", nil, false
	}
	if sel.Sel.Name != "Error" && sel.Sel.Name != "Errorf" {
		return "", nil, false
	}
	codeSel, isCodeSel := call.Args[0].(*ast.SelectorExpr)
	if !isCodeSel {
		return "", nil, false
	}
	codePkg, isCodePkg := codeSel.X.(*ast.Ident)
	if !isCodePkg || codePkg.Name != "codes" {
		return "", nil, false
	}
	return codeSel.Sel.Name, call.Args[1], true
}

// fixedMessage — выносит ли терминальный возврат наружу ПРИНЯТУЮ ошибку.
//
// Производным считается ровно то, через что слова СУБД попадают в текст:
//
//	err, e                  — сама принятая ошибка (имена берутся из сигнатуры)
//	err.Error()             — её текст
//	pgErr.Message, .Detail  — слова СУБД из разобранного отказа
//
// Подстановка СВОИХ значений (имя поля, номер элемента, идентификатор из
// запроса, ярлык оператора) производной НЕ считается — и это не послабление, а
// предмет: правило запрещает эхо ОШИБКИ, а не форматирование как таковое.
// Прежняя редакция считала производной всякую подстановку и дала шесть находок
// из шести ложных.
func fixedMessage(tail ast.Expr, msg ast.Expr, errNames map[string]bool) (bool, string) {
	call, ok := tail.(*ast.CallExpr)
	if !ok {
		return true, "не вызов"
	}
	// Аргументы после кода: сообщение и подстановки.
	for _, arg := range call.Args[1:] {
		if leak, what := carriesError(arg, errNames); leak {
			return false, what
		}
	}
	// Само сообщение: литерал либо имя.
	if msg != nil {
		switch e := msg.(type) {
		case *ast.BasicLit:
			return true, strings.Trim(e.Value, "\"`")
		case *ast.Ident:
			return true, e.Name
		}
	}
	return true, "фиксированный текст с подстановкой своих значений"
}

// carriesError — выносит ли выражение наружу принятую ошибку либо слова СУБД.
func carriesError(e ast.Expr, errNames map[string]bool) (bool, string) {
	switch x := e.(type) {
	case *ast.Ident:
		if errNames[x.Name] {
			return true, "принятую ошибку (" + x.Name + ")"
		}
	case *ast.SelectorExpr:
		// Слова СУБД из разобранного отказа.
		if x.Sel.Name == "Message" || x.Sel.Name == "Detail" {
			if inner, ok := x.X.(*ast.Ident); ok {
				return true, "слова СУБД (" + inner.Name + "." + x.Sel.Name + ")"
			}
		}
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" && len(x.Args) == 0 {
			if inner, ok := sel.X.(*ast.Ident); ok {
				return true, "текст ошибки (" + inner.Name + ".Error())"
			}
			return true, "текст ошибки (.Error())"
		}
		// Форматирование внутри аргумента: заглядываем внутрь.
		for _, arg := range x.Args {
			if leak, what := carriesError(arg, errNames); leak {
				return true, what
			}
		}
	case *ast.BinaryExpr:
		if leak, what := carriesError(x.X, errNames); leak {
			return true, what
		}
		if leak, what := carriesError(x.Y, errNames); leak {
			return true, what
		}
	}
	return false, ""
}

// ─────────────── сторона третья: ПРИЗНАК, по которому принято решение ──────────

// DatabaseFaultPhrases — слова, которыми сервер СООБЩАЕТ о нарушенном
// инварианте, и фрагмент, которым драйвер рендерит код в текст.
//
// Перечень — вокабуляр находки, а не контракт: он существует затем, чтобы
// узнать решение, принятое по словам, и ни одно из этих значений продукт
// производить не должен.
//
// # Почему решение по этим словам неверно всегда, а не «пока»
//
// Текст сообщения сервера зависит от `lc_messages`: на сервере с русской
// локалью «duplicate key value» не встречается ВОВСЕ. Он зависит и от выпуска
// сервера, который формулировки правит. Предикат по подстроке при этом не может
// покраснеть — он молча перестаёт совпадать, и ветка, обязанная сработать, не
// срабатывает никогда. Со стороны это выглядит как «отказ классифицирован как
// внутренний», а не как поломка классификации.
//
// Фрагмент `SQLSTATE` — тот же класс с другой стороны: код в нём настоящий, но
// добывается он разбором ФОРМАТИРОВАНИЯ ошибки драйвером, а не полем
// `PgError.Code`. Формат вывода драйвер менять вправе, поле — нет.
//
// # Одной фразы здесь НЕТ намеренно — у неё другой владелец
//
// `violates check constraint` в перечень не входит, и вернуть её сюда значило бы
// завести второе место об одном предмете — ровно тот класс, против которого этот
// файл и написан. Фразой владеет `TestCheckViolationNeverSpeaksTheDBTone`
// (задача #718), и его правило СТРОЖЕ здешнего: он запрещает такой литерал в
// не-тестовом Go вообще, а не только в решении. Проверено опытом: решение по
// этой фразе он роняет, называя файл и строку.
//
// Цена решения названа: его диагноз говорит о «тексте для вызывающего», тогда
// как пойманное — решение по тексту. Формулировка уводит на соседний вопрос, но
// место называет верно, и посадку такое место не проходит.
var DatabaseFaultPhrases = map[string]string{
	"duplicate key value":             "23505, слова сервера",
	"violates unique constraint":      "23505, слова сервера",
	"violates foreign key constraint": "23503, слова сервера",
	"violates not-null constraint":    "23502, слова сервера",
	"violates exclusion constraint":   "23P01, слова сервера",
	"conflicting key value":           "23P01, слова сервера",
	"deadlock detected":               "40P01, слова сервера",
	"could not serialize access":      "40001, слова сервера",
	"no rows in result set":           "отсутствие строки, слова драйвера",
	"sqlstate":                        "код, добытый из форматирования драйвером",
}

// TextFaultDecision — координата решения о роде отказа, принятого по тексту.
type TextFaultDecision struct {
	File string
	Line int
	// Func — функция, в теле которой стоит решение. Гейт судит по функции, а не
	// по файлу: у одного файла бывает и переведённая часть, и та, что законно
	// осталась своей.
	Func string
	// Needle — литерал, по которому принято решение.
	Needle string
	// Why — чем этот литерал опознан (для текста находки).
	Why string
	// How — каким сравнением принято решение.
	How string
}

// TextFaultCensus — объём осмотренного одним файлом.
type TextFaultCensus struct {
	// Funcs — объявлений функций прочитано.
	Funcs int
	// Literals — строковых литералов осмотрено. Печатается, чтобы «слов сервера
	// не найдено» было отличимо от «литералов не читалось».
	Literals int
	// Comparisons — сравнений строк осмотрено (вызовы `strings.*` и `==`/`!=` со
	// строковым литералом). Это ЗНАМЕНАТЕЛЬ предмета: ноль сравнений при тысяче
	// файлов означает сломанный разбор, а не чистое дерево.
	Comparisons int
}

// stringSearchFuncs — функции пакета `strings`, которыми принимают решение по
// содержанию текста.
//
// `Split`, `TrimPrefix` и прочие преобразователи сюда НЕ входят намеренно: они
// текст режут, а не судят, и решение на их исходе принимает уже другое место —
// которое этот же разбор и увидит.
var stringSearchFuncs = map[string]bool{
	"Contains":    true,
	"ContainsAny": true,
	"HasPrefix":   true,
	"HasSuffix":   true,
	"Index":       true,
	"LastIndex":   true,
	"EqualFold":   true,
	"Compare":     true,
}

// ScanTextFaultDecisions находит решения о роде отказа хранилища, принятые по
// СЛОВАМ сервера, в одном файле.
//
// # Что здесь считается находкой
//
// Сравнение строк, чей ОБРАЗЕЦ принадлежит вокабуляру отказа сервера. Судится
// образец, а не то, что обыскивают: провенанс обыскиваемого значения разбор по
// месту не восстанавливает, а вокабуляр однозначен — ни одну из этих фраз
// продукт не производит, и искать её можно ровно в одном случае.
//
// Комментарий, объясняющий маршрут теми же словами, находкой НЕ является и не
// может ею стать: разбор видит строковый литерал и не видит комментария **by
// construction**. Проверка по подстроке краснела бы здесь на собственной шапке —
// в ней эти фразы стоят все до одной.
func ScanTextFaultDecisions(path string, src []byte) (found []TextFaultDecision, census TextFaultCensus, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return nil, TextFaultCensus{}, perr
	}

	type span struct {
		from, to int
		name     string
	}
	var spans []span
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		census.Funcs++
		spans = append(spans, span{
			from: fset.Position(fn.Pos()).Line,
			to:   fset.Position(fn.End()).Line,
			name: fn.Name.Name,
		})
	}
	funcAt := func(line int) string {
		best := ""
		bestWidth := 1 << 30
		for _, s := range spans {
			if line >= s.from && line <= s.to && s.to-s.from < bestWidth {
				best, bestWidth = s.name, s.to-s.from
			}
		}
		return best
	}

	// phraseOf — принадлежит ли литерал вокабуляру отказа. Сверка регистро-
	// независима: тот же образец пишут и строчными, и как в сообщении сервера.
	phraseOf := func(lit *ast.BasicLit) (needle, why string, ok bool) {
		if lit.Kind != token.STRING {
			return "", "", false
		}
		val := strings.Trim(lit.Value, "\"`")
		low := strings.ToLower(val)
		for phrase, what := range DatabaseFaultPhrases {
			if strings.Contains(low, phrase) {
				return val, what, true
			}
		}
		return "", "", false
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				census.Literals++
			}
		case *ast.CallExpr:
			sel, isSel := x.Fun.(*ast.SelectorExpr)
			if !isSel {
				return true
			}
			pkg, isPkg := sel.X.(*ast.Ident)
			if !isPkg || pkg.Name != "strings" || !stringSearchFuncs[sel.Sel.Name] {
				return true
			}
			// Аргумент-литерал делает вызов СРАВНЕНИЕМ по образцу; вызов, чей
			// образец приходит переменной, знаменателем не считается — судить о
			// нём разбор всё равно не берётся.
			for _, arg := range x.Args {
				lit, isLit := arg.(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					continue
				}
				census.Comparisons++
				needle, why, ok := phraseOf(lit)
				if !ok {
					continue
				}
				line := fset.Position(lit.Pos()).Line
				found = append(found, TextFaultDecision{
					File: path, Line: line, Func: funcAt(line),
					Needle: needle, Why: why, How: "strings." + sel.Sel.Name,
				})
			}
		case *ast.BinaryExpr:
			if x.Op != token.EQL && x.Op != token.NEQ {
				return true
			}
			for _, side := range []ast.Expr{x.X, x.Y} {
				lit, isLit := side.(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					continue
				}
				census.Comparisons++
				needle, why, ok := phraseOf(lit)
				if !ok {
					continue
				}
				line := fset.Position(lit.Pos()).Line
				found = append(found, TextFaultDecision{
					File: path, Line: line, Func: funcAt(line),
					Needle: needle, Why: why, How: x.Op.String(),
				})
			}
		}
		return true
	})

	sort.Slice(found, func(i, j int) bool { return found[i].Line < found[j].Line })
	return found, census, nil
}
