// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subjectchangegapdetection.go — анализатор «ЖУРНАЛ СМЕНЫ СУБЪЕКТА ОБНАРУЖИВАЕТ
// ПРОПУСК, и обнаруживает его НА ОБЕИХ СТОРОНАХ» (задача #1712).
//
// # Предмет — РАЗРЫВ, невидимый ни с одной стороны по отдельности
//
// Журнал читается окном `id > since AND id <= settled`. Снятая строка в такое
// окно просто не попадает: курсор переезжает через неё по последней прочитанной
// позиции, и «строк не было» становится НЕОТЛИЧИМО от «строки убрали». Полоса при
// этом fail-open by design — пропущенная строка означает непогашенный кэш
// вердиктов края, то есть неприменённый отзыв доступа, молча.
//
// Закрывается это ТРЕМЯ звеньями, и ни одно не работает без двух других:
//
//	ПОЛ         чтение спрашивает нижнюю удержанную границу ПЕРЕД выдачей;
//	ОТКАЗ       курсор ниже пола получает явный отказ с возобновимой позицией;
//	ОТВЕТ       читатель этот отказ РАЗБИРАЕТ и на него отвечает.
//
// Каждое звено по отдельности защитимо и выглядит законченным. Отсутствие любого
// возвращает молчаливую потерю целиком:
//
//   - нет пола — отказу неоткуда взяться, сколько бы его ни объявляли;
//   - нет отказа — пол вычислен и выброшен, то есть проверка есть, а сказать о
//     её исходе нечем;
//   - нет ответа — отказ уезжает в общую ветвь читателя, а та советует повторить
//     С ТОЙ ЖЕ ПОЗИЦИИ; на утраченной позиции этот повтор не пройдёт НИКОГДА, и
//     петля отзыва встаёт навсегда.
//
// Именно поэтому гейт один, а утверждений четыре: они об одном шве, и разъехаться
// им нельзя. Проверять звенья порознь значило бы получить три зелёных гейта над
// неработающим механизмом.
//
// # Почему разбор, а не поиск по слову
//
// Признак полосы стоит в этом дереве в КОММЕНТАРИЯХ чаще, чем в объявлениях: его
// называет контракт, называет разбор в приёмке, называет проза самого этого
// файла. Предикат по подстроке краснел бы на собственном объяснении и считал бы
// комментарий порождённого кода за второе объявление. Поэтому судятся УЗЛЫ:
// строковый литерал — для объявления токена и для окна чтения, узел вызова — для
// производителя и разборщика.
//
// # Обе формы вызова считаются, и это не мелочь
//
// Разборщик зовётся и голым именем (внутри своего пакета), и через пакет (у
// владельца журнала). Распознаватель, знавший бы одну форму, объявил бы вторую
// ОТСУТСТВУЮЩЕЙ — не нарушением, а невидимостью. Поэтому имя берётся последним
// идентификатором вызываемого выражения, каким бы оно ни было.
//
// # ПРЕДПОСЫЛКИ РАЗБОРА — заявлены, потому что это факты о дереве
//
//  1. запрос чтения журнала собирается СТРОКОВЫМИ ЛИТЕРАЛАМИ в теле своей
//     функции (в том числе склейкой) — имя таблицы, вынесенное в константу,
//     этому разбору невидимо;
//  2. окно узнаётся по паре сравнений позиции в одном запросе: строго больше
//     курсора и не больше границы;
//  3. пол берётся ВЫЗОВОМ в теле той же функции, а не в соседней, и там же
//     стоит вызов, НАПОЛНЯЮЩИЙ его свежим наблюдением.
//
// Каждая может измениться, поэтому гейт печатает объём КАЖДОЙ полосы и падает на
// пустом обходе: ноль прочитанных файлов и ноль найденных окон означают слепоту
// разбора, а не благополучие дерева.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/subjectchange"
)

// positionLostToken — машинный признак полосы «позиция утрачена».
//
// Берётся У ПРОДУКТА, а не переписывается сюда литералом. Своя копия сделала бы
// гейт вторым местом об одном предмете: он объявил бы находкой всякое второе
// написание признака — и первым таким написанием оказался бы он сам. Разойдясь с
// продуктом, он вдобавок молча перестал бы находить настоящий признак.
//
// Тот же ход, каким закрыт класс «гейт, поставленный ради сверки с СУЩЕСТВОМ,
// сверяется с ТЕКСТОМ»: общий источник вместо двух копий и сверки между ними.
// Побочно это добавляет ось, которой у литерала быть не могло, — снятие экспорта
// роняет СБОРКУ, а не только гейт.
const positionLostToken = subjectchange.ReasonPositionLost

const (
	// subjectChangeJournalTable — имя журнала, как оно стоит в запросе.
	subjectChangeJournalTable = "subject_change_outbox"

	// floorSelector — чем БЕРУТ нижнюю границу.
	floorSelector = "Floor"

	// positionLostProducer / positionLostParser — две стороны шва.
	positionLostProducer = "PositionLost"
	positionLostParser   = "AsPositionLost"

	// observedFloor — внутренний ключ переписи: наблюдение, наполняющее границу.
	observedFloor = "<наблюдение>"
)

// floorObservers — ДВЕ законные формы наполнить нижнюю границу свежим числом.
//
// `Advance` спрашивает максимум и минимум одним запросом, поэтому там, где оно
// идёт на каждый вызов, узкий вопрос был бы round-trip за уже известным числом.
// `RefreshEarliest` спрашивает только минимум и заведён для полосы, где полного
// наблюдения на каждой партии не происходит. Обе формы живут в дереве, и гейт,
// знающий одну, объявил бы вторую отсутствующей.
var floorObservers = []string{"Advance", "RefreshEarliest"}

// SubjectChangeGapDetectionOptions — посадка анализатора.
type SubjectChangeGapDetectionOptions struct {
	// Root — корень обхода.
	Root string
	// ReaderPackage — путь пакета читателя относительно корня. Разбор отказа
	// обязан жить ЗДЕСЬ (свойство держится одной реализацией), а производство —
	// вне его (журнал принадлежит владельцу прав, а не читателю).
	ReaderPackage string
}

// SubjectChangeGapDetectionCensus — объём осмотренного по КАЖДОЙ полосе.
//
// Величин несколько, а не одна, ровно потому, что ноль в каждой означает своё:
// ноль файлов — обход не состоялся; ноль окон — предмет исчез либо разбор ослеп;
// ноль производителей при живых окнах — шов разошёлся.
type SubjectChangeGapDetectionCensus struct {
	GoFiles          int
	JournalLiterals  int
	Windows          []string
	WindowsAskFloor  []string
	Producers        []string
	Parsers          []string
	TokenDeclaration []string
}

// SubjectChangeGapDetectionFinding — одно нарушение.
type SubjectChangeGapDetectionFinding struct{ What string }

// AuditSubjectChangeGapDetection обходит непробное дерево и судит четыре
// утверждения об одном шве.
func AuditSubjectChangeGapDetection(
	opts SubjectChangeGapDetectionOptions, log io.Writer,
) ([]SubjectChangeGapDetectionFinding, SubjectChangeGapDetectionCensus, error) {
	var (
		findings []SubjectChangeGapDetectionFinding
		census   SubjectChangeGapDetectionCensus
	)
	readerPkg := filepath.ToSlash(strings.TrimSuffix(opts.ReaderPackage, "/"))

	err := filepath.WalkDir(opts.Root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		census.GoFiles++

		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("разбор %s: %w", path, perr)
		}
		rel, relErr := filepath.Rel(opts.Root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		inReader := readerPkg != "" && strings.HasPrefix(rel, readerPkg+"/")

		at := func(p token.Pos) string {
			return fmt.Sprintf("%s:%d", rel, fset.Position(p).Line)
		}

		// ── Полоса 1: объявление токена ─────────────────────────────────────
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if s == positionLostToken {
				census.TokenDeclaration = append(census.TokenDeclaration, at(lit.Pos()))
			}
			if strings.Contains(s, subjectChangeJournalTable) {
				census.JournalLiterals++
			}
			return true
		})

		// ── Полосы 2-3: окна чтения и вызовы шва ────────────────────────────
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var (
				sql    strings.Builder
				asks   = map[string]bool{}
				calls  []string
				callAt = map[string]token.Pos{}
			)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BasicLit:
					if node.Kind == token.STRING {
						if s, uerr := strconv.Unquote(node.Value); uerr == nil {
							sql.WriteString(" ")
							sql.WriteString(s)
						}
					}
				case *ast.CallExpr:
					name := calledName(node.Fun)
					if name == "" {
						return true
					}
					switch name {
					case floorSelector:
						asks[floorSelector] = true
					case positionLostProducer, positionLostParser:
						calls = append(calls, name)
						if _, seen := callAt[name]; !seen {
							callAt[name] = node.Pos()
						}
					}
					// Наблюдение, НАПОЛНЯЮЩЕЕ нижнюю границу. Форм две, и обе
					// законны: полное наблюдение спрашивает максимум и минимум
					// ОДНИМ запросом, узкий вопрос — только минимум. Знать одну
					// значило бы объявить вторую отсутствующей — не нарушением,
					// а невидимостью (`testing.md` §«Гейт на класс», п. 7).
					for _, observer := range floorObservers {
						if name == observer {
							asks[observedFloor] = true
						}
					}
				}
				return true
			})

			for _, name := range calls {
				site := at(callAt[name])
				switch {
				case name == positionLostProducer && !inReader:
					census.Producers = append(census.Producers, site)
				case name == positionLostParser && inReader:
					census.Parsers = append(census.Parsers, site)
				}
			}

			if !readsJournalByWindow(sql.String()) {
				continue
			}
			where := at(fn.Pos()) + " " + fn.Name.Name
			census.Windows = append(census.Windows, where)
			if asks[floorSelector] && asks[observedFloor] {
				census.WindowsAskFloor = append(census.WindowsAskFloor, where)
				continue
			}
			findings = append(findings, SubjectChangeGapDetectionFinding{
				What: where + " — журнал смены субъекта читается ОКНОМ по позиции, а нижняя " +
					"удержанная граница не спрашивается (нужен вызов " + floorSelector +
					" и наблюдение, его наполняющее: " + strings.Join(floorObservers, " либо ") +
					"). Снятая строка в такое " +
					"окно не попадает вовсе: курсор переезжает через неё, и «строк не было» " +
					"становится неотличимо от «строки убрали» — то есть отзыв доступа не " +
					"применён, и об этом не узнает никто",
			})
		}
		return nil
	})
	if err != nil {
		return nil, census, err
	}

	// ── Предпосылки разбора: пустой обход есть слепота, а не благополучие ────
	if census.GoFiles == 0 {
		return nil, census, fmt.Errorf(
			"обход не прочитал ни одного файла Go под %s — вердикт был бы беспредметен", opts.Root)
	}
	if len(census.Windows) == 0 {
		return nil, census, fmt.Errorf(
			"ни одного чтения журнала %s окном по позиции не найдено при %d прочитанных файлах "+
				"и %d литералах, называющих журнал: либо предмет гейта снят целиком, либо разбор "+
				"ослеп (запрос собран не литералами в теле функции). Молчание здесь означало бы "+
				"«проверять нечего», а не «нарушений нет»",
			subjectChangeJournalTable, census.GoFiles, census.JournalLiterals)
	}

	// ── Полоса 4: шов замкнут с ОБЕИХ сторон ────────────────────────────────
	if len(census.Producers) == 0 {
		findings = append(findings, SubjectChangeGapDetectionFinding{
			What: "отказ «позиция утрачена» НЕ ПРОИЗВОДИТСЯ ни одним владельцем (вызовов " +
				positionLostProducer + " вне пакета читателя — ноль). Пол при этом может " +
				"спрашиваться исправно: проверка есть, а сказать о её исходе нечем, и " +
				"вызывающий получает пустую страницу вместо отказа",
		})
	}
	if len(census.Parsers) == 0 {
		findings = append(findings, SubjectChangeGapDetectionFinding{
			What: "отказ «позиция утрачена» НЕ РАЗБИРАЕТСЯ читателем (вызовов " +
				positionLostParser + " в пакете читателя — ноль). Такой отказ уедет в общую " +
				"ветвь, а та советует повторить С ТОЙ ЖЕ ПОЗИЦИИ: на утраченной позиции повтор " +
				"не пройдёт никогда, и петля отзыва встанет навсегда — молча для клиента, у " +
				"которого кэш вердиктов отвечает по снятым правам",
		})
	}
	switch len(census.TokenDeclaration) {
	case 1:
	case 0:
		findings = append(findings, SubjectChangeGapDetectionFinding{
			What: "признак полосы " + positionLostToken + " не объявлен ни одним строковым " +
				"литералом непробного дерева — производитель и разборщик ключуются на разное",
		})
	default:
		findings = append(findings, SubjectChangeGapDetectionFinding{
			What: fmt.Sprintf("признак полосы %s объявлен %d раз (%s) — два написания одного "+
				"признака расходятся МОЛЧА: обе строки непусты, обе выглядят полосой, а "+
				"совпасть перестают навсегда. Объявление обязано быть одно",
				positionLostToken, len(census.TokenDeclaration),
				strings.Join(census.TokenDeclaration, ", ")),
		})
	}

	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: файлов Go %d, литералов, называющих журнал %d; "+
			"окон чтения %d, из них спрашивают пол %d; производителей отказа %d, "+
			"разборщиков у читателя %d; объявлений признака полосы %d\n",
			census.GoFiles, census.JournalLiterals,
			len(census.Windows), len(census.WindowsAskFloor),
			len(census.Producers), len(census.Parsers), len(census.TokenDeclaration))
	}
	return findings, census, nil
}

// calledName — имя вызываемого: последний идентификатор выражения.
//
// Обе формы обращения законны и обе обязаны считаться: голое имя внутри своего
// пакета и обращение через пакет — снаружи. Распознаватель, знающий одну,
// объявляет вторую отсутствующей.
func calledName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.IndexExpr:
		return calledName(f.X)
	case *ast.IndexListExpr:
		return calledName(f.X)
	case *ast.ParenExpr:
		return calledName(f.X)
	}
	return ""
}

// readsJournalByWindow — читает ли этот запрос журнал ОКНОМ по позиции.
//
// Окно узнаётся ПАРОЙ сравнений: строго больше курсора и не больше границы.
// Одного упоминания журнала мало и намеренно: снятие строк, счёт строк и вставка
// называют ту же таблицу, а пропуска не создают — гейт, краснеющий на них, ловил
// бы форму, а не существо, и первый же ложный срабат его отключил бы.
func readsJournalByWindow(sql string) bool {
	flat := strings.Join(strings.Fields(sql), " ")
	if !strings.Contains(flat, subjectChangeJournalTable) {
		return false
	}
	upper := strings.ToUpper(flat)
	if !strings.Contains(upper, "SELECT") || !strings.Contains(upper, "WHERE") {
		return false
	}
	return windowComparison(flat, ">") && windowComparison(flat, "<=")
}

// windowComparison — стоит ли в запросе сравнение позиции указанным знаком.
//
// Позиция названа колонкой `id`. Знак берётся ЦЕЛИКОМ вместе с пробелами вокруг,
// поэтому `id >= $1` под образец `id > ` не подпадает: полуокно «от курсора
// включительно» окном не является — оно отдаёт уже прочитанную строку заново.
func windowComparison(flat, op string) bool {
	needle := "id " + op + " "
	for i := 0; i < len(flat); {
		j := strings.Index(flat[i:], needle)
		if j < 0 {
			return false
		}
		at := i + j
		// Слева от `id` обязан быть разделитель, а не хвост другого имени
		// (`subject_id`, `role_id`): иначе чужая колонка прошла бы за позицию.
		if at == 0 || !isSQLIdentByte(flat[at-1]) {
			return true
		}
		i = at + 1
	}
	return false
}

func isSQLIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
