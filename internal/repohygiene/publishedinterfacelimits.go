// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// publishedinterfacelimits.go — гейт на КЛАСС: величина одного интерфейса, которую
// продукт ОБЕЩАЕТ арендатору, живёт в дереве одним объявлением; документация
// называет её тем же числом; она не зависит ни от сети, ни от зоны, ни от семейства
// адресов; вывод числа на публичную страницу не выносится; и то, что наша сторона
// проверить не может, записано ОТКРЫТЫМ ДОЛГОМ, а не выдано за исполненное.
//
// # Предмет
//
// Три величины — полоса, предел одновременных соединений и темп их установления —
// адресованы тому, кто проверить их не может: арендатор читает число и планирует от
// него нагрузку, не зная ни стенда, ни того, кто несёт на нём трафик. Форма та же,
// что у гарантированного минимума полезной нагрузки (`guaranteedpayloadfloor.go`), и
// заведена она вторым экземпляром намеренно: предметы разные (у того — обещание,
// которое СВЯЗЫВАЕТ посадку через страж старта; у этих — обещание, за которое на
// нашей стороне не отвечает ничто), поэтому и утверждения о них разные.
//
// # Что именно требуется, семью утверждениями
//
//  1. ОДНО объявление на величину. Второе — второе место об одном предмете, и оно
//     разойдётся с первым на первой же правке;
//  2. объявление — целочисленный литерал. Стань оно вычисляемым выражением, сверять
//     документацию было бы не с чем, и молчать гейт права не имеет;
//  3. каждая величина названа в документации КАНОНИЧЕСКОЙ формулировкой хотя бы раз,
//     и в каждом таком месте — своим числом и никаким другим;
//  4. число величины не стоит в документации там, где этой формулировки нет: такое
//     место гейт сверить не может, и оно переживёт правку величины. Сюда же попадает
//     ИЗМЕРЕННОЕ значение, если его вздумают опубликовать, — его публикация запрещена
//     отдельно (по измеренной полосе опознаётся железо и способ передачи);
//  5. обещание названо ВСЕОБЩИМ: в том же абзаце сказано, что число одно для всех
//     сетей, всех зон и обоих семейств адресов. Без этого арендатор прочтёт число как
//     свойство СВОЕЙ сети или зоны, и первое же исключение станет законным;
//  6. ни одно место с числом не привязывает его к ЧАСТНОЙ сети, зоне, региону или
//     семейству. «В зоне X — столько-то» ломает обещание, даже когда число совпадает:
//     это форма, из которой расхождение вырастает;
//  7. ВЫВОД числа (плотность интерфейсов на узел, бюджет узла, ёмкость таблицы
//     состояний, класс карты) на публичной странице отсутствует. Вывод — инфра-данные:
//     по нему опознаётся конкретная реализация сетевой фабрики, а публичная
//     поверхность, по которой она опознаётся, — дефект дизайна.
//
// # Восьмое утверждение — про честность: долг не выдаётся за исполнение
//
// Производителя этих величин у управляющего контура НЕТ: их держит исполнитель
// датаплейна. Значит «объявить» и «исполнить» здесь — разные вещи, и разница обязана
// быть записана, иначе объявление читается как гарантия.
//
// Гейт классифицирует каждую величину ПО ДЕРЕВУ: есть ли у неё читатель в прод-коде
// сервиса. Читатель — это единственное, чем наша сторона вообще способна связать
// обещание (так сделано у минимума полезной нагрузки: страж старта не поднимает
// посадку, объявившую меньше). Классификация сверяется с РЕЕСТРОМ ДОЛГА
// (`limitsDebtRegisterPath`), и сверка двусторонняя:
//
//   - величина без читателя ОБЯЗАНА иметь запись «наша сторона не проверяет ничего» с
//     непустым предикатом снятия;
//   - величина, у которой читатель появился, обязана эту запись ПОТЕРЯТЬ: долг,
//     переживший свой предмет, — находка, а не безобидная строка.
//
// Так послабление истекает само: в тот день, когда кто-нибудь заведёт страж старта под
// полосу, гейт покраснеет на устаревшей записи долга и потребует её снять.
//
// # Чего гейт НАМЕРЕННО не проверяет, и это измерено, а не предположено
//
// Запрета литерала, равного величине, в прод-дереве сервиса здесь НЕТ — в отличие от
// гейта минимума полезной нагрузки, где он есть и уместен. Причина в самих числах:
// 1400 в дереве встречается один раз, а 1000 и 10000 — законно и часто. Замер на дереве
// этой ветки: `1000` — 21 вхождение в не-тестовом коде `services/vpc/` (предел размера
// страницы, срок ожидания фильтра прав, жёсткий предел перечисления в хранилище прав),
// `10000` — 6 (ёмкости кэшей). Запрет дал бы 27 находок, ни одна из которых не является
// вторым местом об обещании, — то есть ловил бы ФОРМУ, а не предмет, и был бы снят при
// первом же ложном срабатывании. Дрейф по копиям здесь ловится там, где он опасен: на
// стороне ДОКУМЕНТАЦИИ, которую читает арендатор (утверждения 3 и 4).
//
// # Объявленные слепые зоны
//
//   - Корпус — отслеживаемые git-элементы (`pkg/treecorpus`). Файл, которого нет в
//     индексе, невидим: вердикт обязан быть свойством коммита, а не рабочего каталога;
//   - окно формулировки — СПЛОШНОЙ непустой блок строк, содержащий формулировку
//     (абзац), и он обрывается на строке, которая начинает ДРУГОЕ обещание. Абзац, а не
//     «строка ± строка», выбран потому, что проза переносится: обещание, изложенное
//     предложением на три строки, иначе читалось бы как «формулировка без числа»;
//   - совпадение чисел величины сравнивается МНОЖЕСТВОМ, а не списком: число, названное
//     в одном абзаце дважды, — это по-прежнему одно место об одном предмете;
//   - «читатель» — упоминание имени константы в не-тестовом Go-файле `services/vpc/`
//     вне файла объявления. Упоминание в комментарии читателем не является (разбор
//     синтаксиса, а не подстрока).
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// limitsReaderScope — прод-дерево владельца величин: только здесь упоминание имени
// считается читателем. Путь от корня дерева, слэш-разделённый.
const limitsReaderScope = "services/vpc/"

// limitsDebtRegisterPath — реестр открытого долга соответствия. Инженерный документ,
// а не страница сайта: он адресован тому, кто будет закрывать долг, а не арендатору.
const limitsDebtRegisterPath = "services/vpc/docs/engineering/architecture/10-executor-conformance-debt.md"

// limitsSelfChecked / limitsNotChecked — ДВА канонических ответа реестра на вопрос
// «что делает наша сторона». Третьего нет намеренно: свободный текст здесь означал бы,
// что сверить запись с деревом нечем, и реестр стал бы прозой о самом себе.
const (
	limitsNotChecked  = "не проверяет ничего"
	limitsSelfChecked = "сверяет объявление посадки при старте"
)

// limitsDerivationLeak — вывод величины: плотность интерфейсов на узел, бюджет узла,
// ёмкость таблицы состояний, класс карты. На публичной странице ему не место — по нему
// опознаётся конкретная реализация фабрики. Проверяется ТОЛЬКО внутри окон обещания:
// там живёт соблазн объяснить число, а остальная документация вправе обсуждать что
// угодно.
var limitsDerivationLeak = regexp.MustCompile(`(?i)(интерфейс[а-яё]*\s+на\s+узел|на\s+один\s+узел|бюджет[а-яё]*\s+узла|ёмкост[а-яё]*\s+таблицы|таблиц[а-яё]*\s+состояний|плотност[а-яё]+|гигабитн[а-яё]*\s+карт|карт[а-яё]*\s+узла)`)

// limitsParticularPlacement — привязка числа к ЧАСТНОЙ сети, зоне, региону или
// семейству. Всеобщие формы («в любой зоне», «для всех сетей») сюда НЕ попадают by
// construction: между предлогом и существительным у них стоит квантор, и выражение его
// не пропускает. Это проверено законным близнецом в пробе, а не рассуждением.
var limitsParticularPlacement = regexp.MustCompile(`(?i)(в\s+зоне|в\s+регионе|в\s+сети|для\s+семейства|только\s+для\s+IPv[46])`)

// limitsUniversalPlacement — всеобщая форма той же привязки. Страхует предыдущее
// выражение: строка, объявившая величину одинаковой везде, частной привязкой не
// является, как бы она ни была написана.
var limitsUniversalPlacement = regexp.MustCompile(`(?i)(в\s+люб[а-яё]+\s+(зоне|регионе|сети)|в\s+кажд[а-яё]+\s+(зоне|регионе|сети)|для\s+всех\s+)`)

// limitsUniversality / limitsInvarianceSubjects — из чего складывается утверждение
// «число одно для всех сетей, зон и семейств». Три предмета названы порознь, чтобы
// находка могла сказать, КАКОГО из них не хватает: «формулировка неполна» отправило бы
// автора перечитывать абзац целиком.
var (
	limitsUniversality       = regexp.MustCompile(`(?i)(всех|всем|люб[а-яё]+|кажд[а-яё]+|независимо)`)
	limitsInvarianceSubjects = []struct {
		Name string
		Re   *regexp.Regexp
	}{
		// Основы слов, а не полные формы с границей слова: `\b` в RE2 — ГРАНИЦА
		// ASCII-слова, поэтому после кириллического «зон» она не срабатывает вовсе.
		// Проба на законной странице это и показала: выражение `зон\b` не нашло «зон»
		// в «для всех зон», то есть гейт краснел на верном тексте. Основа слова здесь
		// достаточна: предмет ищется в абзаце, который уже опознан как обещание.
		{"сеть", regexp.MustCompile(`(?i)сет[ьиея]`)},
		{"зона", regexp.MustCompile(`(?i)зон`)},
		{"семейство", regexp.MustCompile(`(?i)семейств`)},
	}
)

// publishedLimit — одна опубликованная величина одного интерфейса.
type publishedLimit struct {
	// Name — как называть величину в находке.
	Name string
	// Idents — константы, несущие её числа. Их больше одной там, где обещание само
	// состоит из пары (постоянный темп и всплеск): пара публикуется вместе, потому
	// что порознь ни одно из чисел не описывает поведение.
	Idents []string
	// Anchor — каноническая формулировка обещания в документации. Формулировка выбрана
	// так, чтобы из неё НЕ следовало, чем обещание достигается.
	Anchor *regexp.Regexp
	// Figure — число величины в тексте: группа 1 — цифры, группа 2 — единица.
	Figure *regexp.Regexp
	// Exclude — продолжение строки, при котором совпадение принадлежит ДРУГОЙ величине
	// (RE2 не умеет заглядывать вперёд, поэтому хвост проверяется явно). Так «столько-то
	// соединений» и «столько-то соединений в секунду» остаются разными предметами.
	Exclude *regexp.Regexp
	// Scale — единица → множитель к единице объявления. nil означает «величина без
	// единиц, множитель единица».
	Scale map[string]int
}

// limitsCatalog — периметр гейта. Перечень выписан, а не выведен: величина, которую
// продукт обещает арендатору, заводится решением, а не появлением файла, поэтому её
// внесение сюда — часть этого решения.
var limitsCatalog = []publishedLimit{
	{
		Name:   "гарантированная полоса на интерфейс",
		Idents: []string{"GuaranteedInterfaceBandwidthFloorMbps"},
		Anchor: regexp.MustCompile(`(?i)гарантированн[а-яё]*\s+полос[а-яё]*\s+на\s+интерфейс`),
		Figure: regexp.MustCompile(`(?i)([0-9][0-9\s\x{00a0}\x{202f}]*)\s*(Гбит/с|Мбит/с)`),
		Scale:  map[string]int{"гбит/с": 1000, "мбит/с": 1},
	},
	{
		Name:    "предел одновременных соединений на интерфейс",
		Idents:  []string{"InterfaceConnectionCeiling"},
		Anchor:  regexp.MustCompile(`(?i)одновременн[а-яё]*\s+соединени[а-яё]*\s+на\s+интерфейс`),
		Figure:  regexp.MustCompile(`(?i)([0-9][0-9\s\x{00a0}\x{202f}]*)\s*(соединени[а-яё]*)`),
		Exclude: regexp.MustCompile(`(?i)^\s*в\s+секунду`),
	},
	{
		Name: "темп установления соединений на интерфейс",
		Idents: []string{
			"InterfaceConnectionRateCeilingPerSecond",
			"InterfaceConnectionRateBurstCeiling",
		},
		Anchor: regexp.MustCompile(`(?i)темп[а-яё]*\s+установления\s+соединений`),
		Figure: regexp.MustCompile(`(?i)([0-9][0-9\s\x{00a0}\x{202f}]*)\s*соединени[а-яё]*\s+в\s+секунду`),
	},
}

// limitFinding — расхождение. Координата обязательна: находка без файла и строки не
// является действием.
type limitFinding struct {
	File string
	Line int
	What string
}

func (f limitFinding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.What)
	}
	return fmt.Sprintf("%s: %s", f.File, f.What)
}

// limitPerValueCensus — объём осмотренного по одной величине.
type limitPerValueCensus struct {
	Name string
	// Declarations — объявлений найдено по всем именам величины (обязано быть по
	// одному на имя).
	Declarations int
	// Values — значения из объявлений, в порядке имён. -1 означает «объявлено не
	// литералом».
	Values []int
	// Readers — упоминаний имён в не-тестовом коде сервиса вне файлов объявления.
	Readers int
	// Windows — окон документации с канонической формулировкой.
	Windows int
	// Figures — чисел величины, сверенных внутри этих окон.
	Figures int
	// RegisterEntries — записей о величине в реестре долга.
	RegisterEntries int
}

// limitsCensus — объём осмотренного. Печатается всегда: без него «ноль расхождений»
// неотличимо от «ноль прочитанного».
type limitsCensus struct {
	GoFilesParsed  int
	DocFilesRead   int
	RegisterFound  bool
	RegisterBlocks int
	PerValue       []limitPerValueCensus
	Findings       int
}

func (c limitsCensus) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "осмотрено: файлов Go %d, файлов документации %d; реестр долга %s (записей %d)",
		c.GoFilesParsed, c.DocFilesRead, limitsRegisterState(c.RegisterFound), c.RegisterBlocks)
	for _, v := range c.PerValue {
		fmt.Fprintf(&b, "\n  · %s: объявлений %d %v, читателей в прод-коде %d, "+
			"окон с формулировкой %d, сверено чисел %d, записей долга %d",
			v.Name, v.Declarations, v.Values, v.Readers, v.Windows, v.Figures, v.RegisterEntries)
	}
	fmt.Fprintf(&b, "\nРАСХОЖДЕНИЙ: %d", c.Findings)
	return b.String()
}

func limitsRegisterState(found bool) string {
	if found {
		return "прочитан"
	}
	return "НЕ НАЙДЕН"
}

// limitSighting — место в файле, где встретилось имя.
type limitSighting struct {
	File string
	Line int
}

// limitsGoScan — то, что гейт вычитал из Go-корпуса.
type limitsGoScan struct {
	// declarations — объявления по имени константы.
	declarations map[string][]limitSighting
	// values — значение первого объявления имени; -1, если оно не литерал.
	values map[string]int
	// declaredIn — файл первого объявления имени.
	declaredIn map[string]string
	// readers — упоминания имени в не-тестовом коде сервиса вне файла объявления.
	readers     map[string][]limitSighting
	filesParsed int
}

// auditPublishedInterfaceLimits — единственный судья и для дерева, и для инъекции.
// Инъекция обязана исполнять ТОТ ЖЕ код, иначе она доказывает свойство своей копии.
func auditPublishedInterfaceLimits(root string) ([]limitFinding, limitsCensus, error) {
	files, err := treecorpus.Under(root)
	if err != nil {
		return nil, limitsCensus{}, err
	}

	var findings []limitFinding
	census := limitsCensus{}

	scan, err := scanGoForLimits(root, files)
	if err != nil {
		return nil, limitsCensus{}, err
	}
	census.GoFilesParsed = scan.filesParsed

	declFindings, values := checkLimitDeclarations(scan)
	findings = append(findings, declFindings...)

	docFindings, windows, docsRead, figures, err := checkLimitDocs(root, files, values)
	if err != nil {
		return nil, limitsCensus{}, err
	}
	findings = append(findings, docFindings...)
	census.DocFilesRead = docsRead

	entries, registerFound, registerBlocks, regFindings, err := readLimitsDebtRegister(root, files)
	if err != nil {
		return nil, limitsCensus{}, err
	}
	findings = append(findings, regFindings...)
	census.RegisterFound = registerFound
	census.RegisterBlocks = registerBlocks
	findings = append(findings, checkLimitsDebtAgainstTree(scan, entries)...)

	for i, l := range limitsCatalog {
		pv := limitPerValueCensus{Name: l.Name, Windows: windows[i], Figures: figures[i]}
		for _, id := range l.Idents {
			pv.Declarations += len(scan.declarations[id])
			v, ok := scan.values[id]
			if !ok {
				v = -1
			}
			pv.Values = append(pv.Values, v)
			pv.Readers += len(scan.readers[id])
			if _, has := entries[id]; has {
				pv.RegisterEntries++
			}
		}
		census.PerValue = append(census.PerValue, pv)
	}

	census.Findings = len(findings)
	return findings, census, nil
}

// checkLimitDeclarations — утверждения 1 и 2: одно объявление на имя, и оно
// целочисленный литерал. Возвращает ожидаемые числа по каждой величине.
func checkLimitDeclarations(scan limitsGoScan) ([]limitFinding, [][]int) {
	var findings []limitFinding
	values := make([][]int, len(limitsCatalog))

	for i, l := range limitsCatalog {
		for _, id := range l.Idents {
			decls := scan.declarations[id]
			switch {
			case len(decls) == 0:
				findings = append(findings, limitFinding{
					File: "<дерево>",
					What: fmt.Sprintf("величина %s (%s) не объявлена ни в одном файле — "+
						"обещание, которое документация даёт арендатору, не закреплено ничем: "+
						"сверять его число не с чем", id, l.Name),
				})
				continue
			case len(decls) > 1:
				for _, d := range decls {
					findings = append(findings, limitFinding{
						File: d.File, Line: d.Line,
						What: fmt.Sprintf("величина %s объявлена здесь и ещё в %d месте(ах) — "+
							"два места об одном предмете расходятся на первой же правке, и "+
							"расходятся молча", id, len(decls)-1),
					})
				}
			}
			v := scan.values[id]
			if v < 0 {
				findings = append(findings, limitFinding{
					File: decls[0].File, Line: decls[0].Line,
					What: fmt.Sprintf("величина %s задана не целочисленным литералом — "+
						"предпосылка этого гейта («число задано здесь») перестала быть верной, "+
						"и сверить с ним документацию нечем. Это отказ, а не пропуск: молчащий "+
						"гейт хуже отсутствующего", id),
				})
				continue
			}
			values[i] = append(values[i], v)
		}
	}
	return findings, values
}

// scanGoForLimits — разбор Go-корпуса: объявления и читатели.
func scanGoForLimits(root string, files []string) (limitsGoScan, error) {
	scan := limitsGoScan{
		declarations: map[string][]limitSighting{},
		values:       map[string]int{},
		declaredIn:   map[string]string{},
		readers:      map[string][]limitSighting{},
	}
	wanted := map[string]bool{}
	for _, l := range limitsCatalog {
		for _, id := range l.Idents {
			wanted[id] = true
			scan.values[id] = -1
		}
	}

	fset := token.NewFileSet()
	type parsedFile struct {
		rel  string
		file *ast.File
	}
	var trees []parsedFile

	for _, abs := range files {
		if !strings.HasSuffix(abs, ".go") {
			continue
		}
		rel := limitsRel(root, abs)
		src, err := os.ReadFile(filepath.Clean(abs))
		if err != nil {
			return limitsGoScan{}, fmt.Errorf("чтение %s: %w", rel, err)
		}
		f, err := parser.ParseFile(fset, abs, src, parser.SkipObjectResolution)
		if err != nil {
			// Неразбираемый Go — отказ, а не пропуск: пропустив файл, гейт отдал бы
			// «ноль находок» на непрочитанном.
			return limitsGoScan{}, fmt.Errorf("разбор %s: %w", rel, err)
		}
		scan.filesParsed++
		trees = append(trees, parsedFile{rel: rel, file: f})

		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !wanted[name.Name] {
						continue
					}
					pos := fset.Position(name.Pos())
					scan.declarations[name.Name] = append(scan.declarations[name.Name],
						limitSighting{File: rel, Line: pos.Line})
					if _, seen := scan.declaredIn[name.Name]; seen {
						continue
					}
					scan.declaredIn[name.Name] = rel
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.INT {
							if n, err := strconv.Atoi(lit.Value); err == nil {
								scan.values[name.Name] = n
							}
						}
					}
				}
			}
		}
	}

	// Второй проход — читатели. Отдельным проходом, потому что файлы объявлений
	// известны только после первого.
	for _, p := range trees {
		if strings.HasSuffix(p.rel, "_test.go") || !strings.HasPrefix(p.rel, limitsReaderScope) {
			continue
		}
		ast.Inspect(p.file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || !wanted[id.Name] || scan.declaredIn[id.Name] == p.rel {
				return true
			}
			pos := fset.Position(id.Pos())
			scan.readers[id.Name] = append(scan.readers[id.Name],
				limitSighting{File: p.rel, Line: pos.Line})
			return true
		})
	}
	return scan, nil
}

// limitDocWindow — окно формулировки: сплошной непустой блок строк (0-based,
// включительно), содержащий формулировку одной величины.
type limitDocWindow struct {
	limit    int
	line     int
	from, to int
}

// checkLimitDocs — утверждения 3-7 по всей документации дерева.
func checkLimitDocs(root string, files []string, values [][]int) (
	findings []limitFinding, windows []int, docsRead int, figures []int, err error) {

	windows = make([]int, len(limitsCatalog))
	figures = make([]int, len(limitsCatalog))

	for _, abs := range files {
		if !strings.HasSuffix(abs, ".md") && !strings.HasSuffix(abs, ".mdx") {
			continue
		}
		rel := limitsRel(root, abs)
		raw, readErr := os.ReadFile(filepath.Clean(abs))
		if readErr != nil {
			return nil, nil, 0, nil, fmt.Errorf("чтение %s: %w", rel, readErr)
		}
		docsRead++
		lines := strings.Split(string(raw), "\n")

		fileWindows := limitWindowsIn(lines)
		anchored := map[int]bool{}
		for _, w := range fileWindows {
			windows[w.limit]++
			for j := w.from; j <= w.to; j++ {
				anchored[j] = true
			}
		}

		for _, w := range fileWindows {
			f, n := checkLimitWindow(rel, lines, w, values[w.limit])
			findings = append(findings, f...)
			figures[w.limit] += n
		}
		findings = append(findings, checkLimitFiguresOutsideWindows(rel, lines, anchored)...)
		findings = append(findings, checkLimitPlacementQualifiers(rel, lines)...)
	}

	for i, l := range limitsCatalog {
		if windows[i] == 0 {
			findings = append(findings, limitFinding{
				File: "<документация>",
				What: fmt.Sprintf("ни одна страница не даёт обещания «%s» канонической "+
					"формулировкой (%s) — величина живёт в коде и арендатору не обещана, то "+
					"есть он вынужден догадываться, а догадка у каждого своя", l.Name, l.Anchor),
			})
		}
	}
	return findings, windows, docsRead, figures, nil
}

// limitWindowsIn — окна формулировок в одном файле. Окно — сплошной непустой блок
// строк вокруг формулировки, обрывающийся на строке, которая начинает ДРУГОЕ обещание:
// иначе соседнее обещание внесло бы в окно свои числа и покрасило бы верную страницу.
func limitWindowsIn(lines []string) []limitDocWindow {
	anchorOf := make([]int, len(lines))
	for i := range anchorOf {
		anchorOf[i] = -1
	}
	for i, ln := range lines {
		for li, l := range limitsCatalog {
			if l.Anchor.MatchString(ln) {
				anchorOf[i] = li
				break
			}
		}
	}

	var out []limitDocWindow
	for i, li := range anchorOf {
		if li < 0 {
			continue
		}
		from, to := i, i
		for from-1 >= 0 && strings.TrimSpace(lines[from-1]) != "" && anchorOf[from-1] < 0 {
			from--
		}
		for to+1 < len(lines) && strings.TrimSpace(lines[to+1]) != "" && anchorOf[to+1] < 0 {
			to++
		}
		out = append(out, limitDocWindow{limit: li, line: i, from: from, to: to})
	}
	return out
}

// checkLimitWindow — утверждения 3, 5 и 7 внутри одного окна.
func checkLimitWindow(rel string, lines []string, w limitDocWindow, expected []int) ([]limitFinding, int) {
	l := limitsCatalog[w.limit]
	var findings []limitFinding

	seen := map[int]int{} // значение → строка (1-based) первого вхождения
	checked := 0
	for j := w.from; j <= w.to; j++ {
		for _, n := range l.figuresIn(lines[j]) {
			checked++
			if _, dup := seen[n]; !dup {
				seen[n] = j + 1
			}
		}
	}

	want := map[int]bool{}
	for _, v := range expected {
		want[v] = true
	}
	for n, line := range seen {
		if want[n] {
			continue
		}
		findings = append(findings, limitFinding{
			File: rel, Line: line,
			What: fmt.Sprintf("документация называет обещание «%s» величиной %d, а в дереве "+
				"объявлено %v — два места об одном предмете разошлись; верно ровно одно, и "+
				"правится оно вместе со вторым", l.Name, n, expected),
		})
	}
	for _, v := range expected {
		if _, ok := seen[v]; ok {
			continue
		}
		findings = append(findings, limitFinding{
			File: rel, Line: w.line + 1,
			What: fmt.Sprintf("формулировка обещания «%s» стоит без числа %d — величина "+
				"обязана быть названа в том же абзаце, иначе сверять её с объявлением в "+
				"дереве нечем", l.Name, v),
		})
	}

	// Утверждение 5: обещание названо всеобщим.
	window := strings.Join(lines[w.from:w.to+1], " ")
	if !limitsUniversality.MatchString(window) {
		findings = append(findings, limitFinding{
			File: rel, Line: w.line + 1,
			What: fmt.Sprintf("обещание «%s» не названо ВСЕОБЩИМ — в абзаце нет слов «для "+
				"всех»/«в любой»/«независимо», поэтому арендатор прочтёт число как свойство "+
				"своей сети или зоны, и первое же исключение станет законным", l.Name),
		})
	} else {
		for _, s := range limitsInvarianceSubjects {
			if s.Re.MatchString(window) {
				continue
			}
			findings = append(findings, limitFinding{
				File: rel, Line: w.line + 1,
				What: fmt.Sprintf("обещание «%s» не говорит, что величина одна для всех — "+
					"не назван предмет «%s». Названы обязаны быть все три (сеть, зона, "+
					"семейство адресов): умолчание о любом из них оставляет дверь для "+
					"«а у нас исключение»", l.Name, s.Name),
			})
		}
	}

	// Утверждение 7: вывод числа на публичной странице отсутствует.
	for j := w.from; j <= w.to; j++ {
		if m := limitsDerivationLeak.FindString(lines[j]); m != "" {
			findings = append(findings, limitFinding{
				File: rel, Line: j + 1,
				What: fmt.Sprintf("рядом с обещанием «%s» назван ВЫВОД величины (%q) — это "+
					"инфра-данные: по плотности, бюджету узла и ёмкости таблицы состояний "+
					"опознаётся конкретная реализация сетевой фабрики. Вывод живёт в плане, "+
					"арендатору сообщается величина и то, что она ни от чего не зависит",
					l.Name, strings.TrimSpace(m)),
			})
		}
	}
	return findings, checked
}

// checkLimitFiguresOutsideWindows — утверждение 4: число величины вне окна формулировки.
// Такое место гейт сверить не может, и оно переживёт правку величины. Сюда же попадает
// измеренное значение, если его вздумают опубликовать.
func checkLimitFiguresOutsideWindows(rel string, lines []string, anchored map[int]bool) []limitFinding {
	var findings []limitFinding
	for i, ln := range lines {
		if anchored[i] {
			continue
		}
		for _, l := range limitsCatalog {
			for _, n := range l.figuresIn(ln) {
				findings = append(findings, limitFinding{
					File: rel, Line: i + 1,
					What: fmt.Sprintf("величина %d названа там, где нет канонической "+
						"формулировки обещания «%s» — гейт такое место сверить не может, и оно "+
						"переживёт правку числа. Либо поставь рядом формулировку обещания, либо "+
						"сошлись на страницу, которая его даёт. Если это ИЗМЕРЕННОЕ значение — "+
						"публиковать его нельзя: по нему опознаются железо и способ передачи",
						n, l.Name),
				})
			}
		}
	}
	return findings
}

// checkLimitPlacementQualifiers — утверждение 6: число, привязанное к частной сети,
// зоне, региону или семейству. Ломает обещание ДАЖЕ при совпадающем числе: это форма,
// из которой расхождение вырастает на первой же правке.
func checkLimitPlacementQualifiers(rel string, lines []string) []limitFinding {
	var findings []limitFinding
	for i, ln := range lines {
		if limitsUniversalPlacement.MatchString(ln) {
			continue
		}
		q := limitsParticularPlacement.FindString(ln)
		if q == "" {
			continue
		}
		for _, l := range limitsCatalog {
			if len(l.figuresIn(ln)) == 0 && !l.Anchor.MatchString(ln) {
				continue
			}
			findings = append(findings, limitFinding{
				File: rel, Line: i + 1,
				What: fmt.Sprintf("обещание «%s» привязано здесь к ЧАСТНОМУ размещению (%q) — "+
					"величина одна для всех сетей, зон и семейств, и публиковать её «по зонам» "+
					"нельзя даже совпадающим числом: из этой формы расхождение вырастает на "+
					"первой же правке", l.Name, strings.TrimSpace(q)),
			})
		}
	}
	return findings
}

// figuresIn — числа величины в строке, приведённые к единице объявления.
func (l publishedLimit) figuresIn(line string) []int {
	idx := l.Figure.FindAllStringSubmatchIndex(line, -1)
	if len(idx) == 0 {
		return nil
	}
	var out []int
	for _, m := range idx {
		if l.Exclude != nil && l.Exclude.MatchString(line[m[1]:]) {
			continue
		}
		n, err := limitsParseNumber(line[m[2]:m[3]])
		if err != nil {
			continue
		}
		unit := ""
		if len(m) >= 6 && m[4] >= 0 {
			unit = strings.ToLower(line[m[4]:m[5]])
		}
		out = append(out, n*l.scaleOf(unit))
	}
	return out
}

// scaleOf — множитель единицы. Единица, которой нет в таблице, невозможна by
// construction (выражение допускает ровно перечисленные), но множитель по умолчанию
// назван явно: молчаливый ноль превратил бы верное число в расхождение.
func (l publishedLimit) scaleOf(unit string) int {
	if l.Scale == nil {
		return 1
	}
	if k, ok := l.Scale[unit]; ok {
		return k
	}
	return 1
}

// limitsDebtEntry — запись реестра долга.
type limitsDebtEntry struct {
	Ident     string
	Line      int
	OurSide   string
	Predicate string
}

var (
	limitsRegisterHeading  = regexp.MustCompile("^###\\s+`([A-Za-z][A-Za-z0-9_]*)`\\s*$")
	limitsRegisterOurSide  = regexp.MustCompile(`^-\s+\*\*Наша сторона:\*\*\s*(.+?)\s*$`)
	limitsRegisterPredicat = regexp.MustCompile(`^-\s+\*\*Предикат снятия:\*\*\s*(.+?)\s*$`)
)

// limitsPredicateMinRunes — предикат снятия обязан быть предложением, а не отметкой.
// Порог грубый намеренно: он отсекает «нет», «позже», «—», то есть формы, при которых
// снять долг было бы некому и не по чему, и не притворяется мерой качества текста.
const limitsPredicateMinRunes = 16

// readLimitsDebtRegister — разбор реестра. Отсутствие файла — находка: долг, который
// негде прочитать, не записан.
func readLimitsDebtRegister(root string, files []string) (
	map[string]limitsDebtEntry, bool, int, []limitFinding, error) {

	entries := map[string]limitsDebtEntry{}
	var findings []limitFinding

	var abs string
	for _, f := range files {
		if limitsRel(root, f) == limitsDebtRegisterPath {
			abs = f
			break
		}
	}
	if abs == "" {
		findings = append(findings, limitFinding{
			File: limitsDebtRegisterPath,
			What: "реестра открытого долга нет в дереве — величины, за которые на нашей " +
				"стороне не отвечает ничто, объявлены и выглядят гарантированными. Объявить " +
				"и исполнить — разные вещи, и разница обязана быть записана",
		})
		return entries, false, 0, findings, nil
	}

	raw, err := os.ReadFile(filepath.Clean(abs))
	if err != nil {
		return nil, false, 0, nil, fmt.Errorf("чтение %s: %w", limitsDebtRegisterPath, err)
	}
	lines := strings.Split(string(raw), "\n")

	cur := ""
	blocks := 0
	for i, ln := range lines {
		if m := limitsRegisterHeading.FindStringSubmatch(ln); m != nil {
			cur = m[1]
			blocks++
			if _, dup := entries[cur]; dup {
				findings = append(findings, limitFinding{
					File: limitsDebtRegisterPath, Line: i + 1,
					What: fmt.Sprintf("величина %s записана в реестре дважды — две записи об "+
						"одном долге разойдутся, и снят он будет по одной из них", cur),
				})
				continue
			}
			entries[cur] = limitsDebtEntry{Ident: cur, Line: i + 1}
			continue
		}
		if cur == "" {
			continue
		}
		e := entries[cur]
		if m := limitsRegisterOurSide.FindStringSubmatch(ln); m != nil {
			e.OurSide = m[1]
		}
		if m := limitsRegisterPredicat.FindStringSubmatch(ln); m != nil {
			e.Predicate = m[1]
		}
		entries[cur] = e
	}
	return entries, true, blocks, findings, nil
}

// checkLimitsDebtAgainstTree — восьмое утверждение, обе стороны сразу.
func checkLimitsDebtAgainstTree(scan limitsGoScan, entries map[string]limitsDebtEntry) []limitFinding {
	var findings []limitFinding
	inScope := map[string]bool{}

	for _, l := range limitsCatalog {
		for _, id := range l.Idents {
			inScope[id] = true
			readers := len(scan.readers[id])
			want := limitsNotChecked
			if readers > 0 {
				want = limitsSelfChecked
			}
			e, ok := entries[id]
			if !ok {
				findings = append(findings, limitFinding{
					File: limitsDebtRegisterPath,
					What: fmt.Sprintf("величина %s (%s) опубликована, но записи о долге у неё "+
						"нет — читателей в прод-коде %d, то есть наша сторона проверяет «%s». "+
						"Ненаписанный долг неотличим от исполненного обещания",
						id, l.Name, readers, want),
				})
				continue
			}
			if e.OurSide != want {
				findings = append(findings, limitFinding{
					File: limitsDebtRegisterPath, Line: e.Line,
					What: fmt.Sprintf("реестр говорит про %s «%s», а дерево — «%s» (читателей "+
						"в прод-коде %d). Запись, разошедшаяся с деревом, либо выдаёт долг за "+
						"исполненный, либо переживает свой предмет; допустимы ровно два "+
						"ответа: %q и %q", id, e.OurSide, want, readers,
						limitsNotChecked, limitsSelfChecked),
				})
			}
			if len([]rune(strings.TrimSpace(e.Predicate))) < limitsPredicateMinRunes {
				findings = append(findings, limitFinding{
					File: limitsDebtRegisterPath, Line: e.Line,
					What: fmt.Sprintf("у долга по %s нет предиката снятия — долг без условия "+
						"снятия снять некому: он переживёт и того, кто его завёл, и причину, "+
						"по которой он заведён", id),
				})
			}
		}
	}
	for id, e := range entries {
		if inScope[id] {
			continue
		}
		findings = append(findings, limitFinding{
			File: limitsDebtRegisterPath, Line: e.Line,
			What: fmt.Sprintf("в реестре записан долг по %s, а такой опубликованной величины "+
				"в периметре гейта нет — запись, которой больше нечего исключать, наследуется "+
				"следующей слепой зоной", id),
		})
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].What < findings[j].What
	})
	return findings
}

// limitsParseNumber — «10 000» → 10000. Пробельные разделители внутри числа снимаются:
// типографская запись не должна читаться как отсутствие числа.
func limitsParseNumber(s string) (int, error) {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return strconv.Atoi(b.String())
}

// limitsRel — путь от корня дерева, слэш-разделённый.
//
// Своё имя, а не соседское: в пакете живут одноимённые помощники с РАЗНЫМ поведением
// (один не приводит разделитель к слэшу), и разделять имя с ними значило бы завести две
// семантики под одним словом. Пять строк дешевле связать заново, чем распутывать потом.
func limitsRel(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}
