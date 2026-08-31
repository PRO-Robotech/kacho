// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionfloorafterpage.go — анализатор «ПОЛ БЕРЁТСЯ НЕ РАНЬШЕ СТРАНИЦЫ»
// (задача #1764).
//
// # Предмет — ПОРЯДОК, а не наличие
//
// Журнал читается окном `позиция > курсор AND позиция <= граница`. Нижняя
// удержанная граница («пол») спрашивается отдельным запросом, и её задача одна:
// превратить снятый уборкой участок в ЯВНЫЙ отказ вместо тишины. Работает она
// ровно тогда, когда наблюдение взято НЕ РАНЬШЕ страницы.
//
// Спрошенный раньше пол описывает журнал, которого к моменту выборки уже нет:
// уборка фиксируется между двумя запросами, страница приходит без снятых строк,
// пол о них ещё не знает, отказа не будет — и вызывающий получает страницу с
// дырой как ПОЛНУЮ. Проверка присутствует, выглядит исполненной и решает при
// этом не она, а расписание: software check-then-act (ban #10).
//
// Порядок «после» делает вывод ДОКАЗУЕМЫМ. Пусть страница снята в снимке S1,
// пол — в S2 >= S1. Пропала строка из окна ⟹ она снята до S1 ⟹ в S1 нижняя
// строка уже выше неё; нижняя строка непустого журнала монотонна (уборка
// снимает префикс, номера растут) ⟹ и в S2 пол выше курсора ⟹ отказ.
//
// # Чем этот анализатор отличается от соседнего
//
// `subjectchangegapdetection.go` судит ОДИН журнал по имени его таблицы и
// требует, чтобы окно и пол стояли в одной функции. Форма общего сервера
// подписки другая: имя таблицы приходит из объявления владельца (в запросе стоит
// подстановка), а выборка вынесена в помощника — то есть окна в теле дренажа нет
// ВОВСЕ, и тот анализатор его не видит by construction.
//
// Поэтому здесь судится РАСЩЕПЛЁННАЯ форма: страницу читает помощник СВОЕГО
// пакета, а пол спрашивает вызывающий. Слитная форма (окно литералом в том же
// теле) принимается тем же предикатом — иначе перенос выборки внутрь функции
// молча выводил бы её из-под наблюдения.
//
// # ПРЕДПОСЫЛКИ РАЗБОРА — заявлены, потому что это факты о дереве
//
//  1. запрос окна собирается СТРОКОВЫМИ ЛИТЕРАЛАМИ в теле своей функции (в том
//     числе склейкой); имя таблицы при этом безразлично — окно узнаётся ПАРОЙ
//     сравнений позиции с позиционными параметрами;
//  2. помощник, читающий страницу, живёт в ТОМ ЖЕ пакете, что и спрашивающий пол:
//     обращение к нему идёт по имени, а не через импорт;
//  3. пол берут вызовом `Floor` либо `ObserveFloor` у наблюдателя границы; чтобы
//     одноимённый вызов чужого пакета (`math.Floor`) не считался полом, файл
//     обязан принадлежать пакету подписки либо импортировать его;
//  4. порядок операторов в теле функции отвечает порядку исполнения — по нему и
//     судится, взят ли пол не раньше страницы.
//
// Каждая может измениться, поэтому анализатор печатает объём КАЖДОЙ полосы и
// падает на пустом обходе: ноль прочитанных файлов, ноль найденных выборок и ноль
// рассмотренных функций означают слепоту разбора, а не благополучие дерева.
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
)

const (
	// subscriptionPackagePath — хвост пути пакета, владеющего наблюдателем
	// границы. Им же отсекается одноимённый вызов чужого пакета.
	subscriptionPackagePath = "pkg/subscription"
	// subscriptionPackageName — имя пакета для файлов самого наблюдателя: сам
	// себя он не импортирует.
	subscriptionPackageName = "subscription"
)

// subscriptionFloorSelectors — чем БЕРУТ нижнюю границу.
//
// Форм две, и обе законны: `Floor` отвечает по последнему наблюдению (годится
// там, где наблюдатель СВОЙ и между вопросом и ответом стоит одна горутина),
// `ObserveFloor` — из собственного наблюдения (нужен там, где наблюдатель ОБЩИЙ).
// Знать одну значило бы объявить вторую отсутствующей — не нарушением, а
// невидимостью.
var subscriptionFloorSelectors = []string{"Floor", "ObserveFloor"}

// SubscriptionFloorAfterPageOptions — посадка анализатора.
type SubscriptionFloorAfterPageOptions struct{ Root string }

// SubscriptionFloorAfterPageCensus — объём осмотренного по КАЖДОЙ полосе.
//
// Величин несколько, а не одна, ровно потому, что ноль в каждой означает своё:
// ноль файлов — обход не состоялся; ноль выборок — предмет исчез либо разбор
// ослеп; ноль рассмотренных функций при живых выборках — пол перестали
// спрашивать вовсе.
type SubscriptionFloorAfterPageCensus struct {
	GoFiles     int
	OwnedFiles  int
	PageReaders []string
	FloorAsks   []string
	Judged      []string
	OrderOK     []string
}

// SubscriptionFloorAfterPageFinding — одно нарушение.
type SubscriptionFloorAfterPageFinding struct{ What string }

// subscriptionScanned — что анализатор запомнил об одном теле функции.
type subscriptionScanned struct {
	where    string
	dir      string
	name     string
	windowAt token.Pos
	floorAt  token.Pos
	floorSel string
	calls    []subscriptionCall
}

type subscriptionCall struct {
	name string
	at   token.Pos
}

// AuditSubscriptionFloorAfterPage обходит непробное дерево и требует одного: там,
// где функция и читает страницу журнала, и спрашивает пол, пол стоит НЕ РАНЬШЕ.
func AuditSubscriptionFloorAfterPage(
	opts SubscriptionFloorAfterPageOptions, log io.Writer,
) ([]SubscriptionFloorAfterPageFinding, SubscriptionFloorAfterPageCensus, error) {
	var (
		findings []SubscriptionFloorAfterPageFinding
		census   SubscriptionFloorAfterPageCensus
		scanned  []subscriptionScanned
	)
	// Множество позиций ОДНО на весь обход: сравнивать позиции разных файлов
	// иначе было бы нельзя, а внутри одного файла порядок и есть предмет.
	fset := token.NewFileSet()
	// readers[каталог][имя] — помощники, читающие страницу окном.
	readers := map[string]map[string]string{}

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

		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("разбор %s: %w", path, perr)
		}
		if !ownsSubscriptionFloor(file) {
			return nil
		}
		census.OwnedFiles++

		rel, relErr := filepath.Rel(opts.Root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			rec := subscriptionScanned{
				where: fmt.Sprintf("%s:%d %s", rel, fset.Position(fn.Pos()).Line, fn.Name.Name),
				dir:   dir,
				name:  fn.Name.Name,
			}
			var sql strings.Builder
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BasicLit:
					if node.Kind != token.STRING {
						return true
					}
					s, uerr := strconv.Unquote(node.Value)
					if uerr != nil {
						return true
					}
					sql.WriteString(" ")
					sql.WriteString(s)
					// Позиция САМОГО окна: по ней судится порядок в слитной форме.
					if readsAJournalWindow(s) && !rec.windowAt.IsValid() {
						rec.windowAt = node.Pos()
					}
				case *ast.CallExpr:
					name := calledName(node.Fun)
					if name == "" {
						return true
					}
					rec.calls = append(rec.calls, subscriptionCall{name: name, at: node.Pos()})
					for _, sel := range subscriptionFloorSelectors {
						if name != sel {
							continue
						}
						if !rec.floorAt.IsValid() || node.Pos() < rec.floorAt {
							rec.floorAt, rec.floorSel = node.Pos(), sel
						}
					}
				}
				return true
			})
			// Окно, собранное СКЛЕЙКОЙ: пары сравнений в одном литерале может не
			// быть, а в собранном запросе она есть.
			if !rec.windowAt.IsValid() && readsAJournalWindow(sql.String()) {
				rec.windowAt = fn.Pos()
			}
			if rec.windowAt.IsValid() {
				if readers[dir] == nil {
					readers[dir] = map[string]string{}
				}
				readers[dir][fn.Name.Name] = rec.where
				census.PageReaders = append(census.PageReaders, rec.where)
			}
			if rec.floorAt.IsValid() {
				census.FloorAsks = append(census.FloorAsks, rec.where)
			}
			if rec.windowAt.IsValid() || rec.floorAt.IsValid() {
				scanned = append(scanned, rec)
			}
		}
		return nil
	})
	if err != nil {
		return nil, census, err
	}

	for _, rec := range scanned {
		if !rec.floorAt.IsValid() {
			continue
		}
		// СОБЫТИЕ СТРАНИЦЫ — самое раннее из двух законных форм: окно литералом
		// в этом же теле либо обращение к помощнику СВОЕГО пакета, который его
		// несёт. Знать одну форму значило бы позволить снять гейт переносом
		// выборки: она уехала бы из-под наблюдения, не изменив поведения.
		pageAt, pageWhat := rec.windowAt, "выборка страницы"
		for _, c := range rec.calls {
			where, isReader := readers[rec.dir][c.name]
			if !isReader || c.name == rec.name {
				continue
			}
			if !pageAt.IsValid() || c.at < pageAt {
				pageAt, pageWhat = c.at, "вызов "+c.name+" ("+where+")"
			}
		}
		if !pageAt.IsValid() {
			continue
		}
		census.Judged = append(census.Judged, rec.where)
		if rec.floorAt < pageAt {
			findings = append(findings, SubscriptionFloorAfterPageFinding{
				What: rec.where + " — пол берётся РАНЬШЕ страницы: " + rec.floorSel +
					" в строке " + strconv.Itoa(fset.Position(rec.floorAt).Line) + ", " + pageWhat +
					" в строке " + strconv.Itoa(fset.Position(pageAt).Line) + ". Между двумя " +
					"запросами уборка вправе зафиксироваться: страница придёт без снятых строк, " +
					"пол о них ещё не знает, отказа не будет — и вызывающий получит страницу с " +
					"дырой как полную. Пол обязан браться НЕ РАНЬШЕ страницы, иначе доказывает " +
					"он только расписание",
			})
			continue
		}
		census.OrderOK = append(census.OrderOK, rec.where)
	}

	// ── Предпосылки разбора: пустой обход есть слепота, а не благополучие ────
	if census.GoFiles == 0 {
		return nil, census, fmt.Errorf(
			"обход не прочитал ни одного файла Go под %s — вердикт был бы беспредметен", opts.Root)
	}
	if census.OwnedFiles == 0 {
		return nil, census, fmt.Errorf(
			"ни одного файла пакета %q либо импортирующего %q не найдено при %d прочитанных: "+
				"либо предмет снят целиком, либо разбор ослеп",
			subscriptionPackageName, subscriptionPackagePath, census.GoFiles)
	}
	if len(census.PageReaders) == 0 {
		return nil, census, fmt.Errorf(
			"ни одной выборки журнала окном по позиции не найдено при %d файлах подписки: "+
				"либо чтение окном снято, либо запрос собран не литералами в теле функции. "+
				"Молчание здесь означало бы «проверять нечего», а не «нарушений нет»",
			census.OwnedFiles)
	}
	if len(census.Judged) == 0 {
		return nil, census, fmt.Errorf(
			"ни одна функция не спрашивает пол рядом с выборкой при %d выборках и %d вопросах о "+
				"поле: утверждение о ПОРЯДКЕ потеряло предмет — снимайте его вместе с полом, а не "+
				"оставляйте зелёным",
			len(census.PageReaders), len(census.FloorAsks))
	}

	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: файлов Go %d, из них подписки %d; выборок окном %d, "+
			"вопросов о поле %d; рассмотрено функций %d, порядок верен у %d\n",
			census.GoFiles, census.OwnedFiles, len(census.PageReaders),
			len(census.FloorAsks), len(census.Judged), len(census.OrderOK))
	}
	return findings, census, nil
}

// ownsSubscriptionFloor — вправе ли этот файл вообще спрашивать пол.
//
// Отсекает одноимённый вызов чужого пакета (`math.Floor`): распознаватель судит
// по последнему идентификатору выражения и различить их иначе не может.
func ownsSubscriptionFloor(file *ast.File) bool {
	if file.Name != nil && file.Name.Name == subscriptionPackageName {
		return true
	}
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if p == subscriptionPackagePath || strings.HasSuffix(p, "/"+subscriptionPackagePath) {
			return true
		}
	}
	return false
}

// readsAJournalWindow — читает ли этот запрос журнал ОКНОМ по позиции.
//
// Окно узнаётся ПАРОЙ сравнений позиции с позиционными параметрами: строго
// больше курсора и не больше границы. Имя таблицы намеренно НЕ проверяется —
// у общего сервера оно приходит подстановкой из объявления владельца, и предикат
// по имени ослеп бы именно на нём. Одного упоминания выборки мало и тоже
// намеренно: снятие строк и счёт строк называют ту же таблицу, а пропуска не
// создают.
func readsAJournalWindow(sql string) bool {
	flat := strings.Join(strings.Fields(sql), " ")
	upper := strings.ToUpper(flat)
	if !strings.Contains(upper, "SELECT") || !strings.Contains(upper, "FROM") ||
		!strings.Contains(upper, "WHERE") {
		return false
	}
	// Знак берётся ЦЕЛИКОМ вместе с пробелами: `>= $1` под образец `> $` не
	// подпадает — полуокно «от курсора включительно» окном не является, оно
	// отдаёт уже прочитанную строку заново.
	return strings.Contains(flat, "> $") && strings.Contains(flat, "<= $")
}
