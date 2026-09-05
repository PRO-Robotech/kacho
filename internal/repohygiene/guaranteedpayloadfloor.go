// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// guaranteedpayloadfloor.go — гейт на КЛАСС: величина, которую продукт ОБЕЩАЕТ
// арендатору, живёт в дереве одним объявлением, её читает прод-код, и число,
// которым её называет документация, сверяется с этим объявлением механически.
//
// # Предмет
//
// Обещание — это утверждение о продукте, адресованное тому, кто не может его
// проверить: арендатор читает нижнюю границу полезной нагрузки кадра в
// документации и рассчитывает на неё, не зная ни стенда, на который пришёл, ни
// его исполнителя датаплейна. Поэтому у обещания две стороны, и обе обязаны
// говорить одно:
//
//   - КОД — величина, которой страж старта не пускает посадку, объявившую
//     меньше обещанного (`domain.GuaranteedPayloadFloorBytes`);
//   - ДОКУМЕНТАЦИЯ — число, которое читает арендатор.
//
// Это два места об одном предмете. Прозой такая пара расходится МОЛЧА: правку
// вносят в одно место, второе с этого дня лжёт, и ни сборка, ни слияние об этом
// не скажут. Ровно тот класс, который корпус ловит в коде, — только цена здесь
// выше: расхождение читает не следующий контрибьютор, а арендатор, строящий на
// обещании свою сеть.
//
// # Что именно требуется, четырьмя утверждениями
//
//  1. ОДНО объявление величины в дереве. Второе объявление — второе место об
//     одном предмете, и оно разойдётся с первым на первой же правке;
//  2. у величины есть ЧИТАТЕЛЬ в прод-коде. Объявленная и никем не читаемая
//     величина — украшение: обещание при ней держится ничем, а выглядит
//     закреплённым;
//  3. в прод-дереве сервиса нет ЛИТЕРАЛА, равного величине. Литерал — это и есть
//     «три места об одном числе», от которых обещание рассыпается по копиям;
//  4. каждое место документации, называющее обещание канонической формулировкой,
//     называет ЕГО ЧИСЛО и никакого другого; и наоборот — число обещания не
//     стоит в документации там, где рядом нет формулировки, по которой гейт
//     умеет его найти.
//
// Четвёртое утверждение двустороннее намеренно. Без первой половины разошлась бы
// документация, без второй — число уехало бы в страницу, которую гейт не
// осматривает, и «ноль расхождений» означало бы «ноль прочитанного».
//
// # Почему формулировка, а не «любое число рядом со словом байт»
//
// Предикат «всякое N байт в документации равно обещанию» пришлось отвергнуть: в
// дереве есть законные байтовые величины другого предмета (длина имени, длина
// значения метки), и такой гейт краснел бы на них — то есть ловил бы форму, а не
// существо, и был бы снят при первом ложном срабатывании. Поэтому осматриваются
// окна вокруг КАНОНИЧЕСКОЙ ФОРМУЛИРОВКИ обещания, а законные байтовые величины
// другого предмета гейт обязан пропускать — это проверено инъекцией законного
// близнеца, а не рассуждением.
//
// # Предпосылка и её самопроверка
//
// Вывод «величина задана здесь» верен, пока объявление — целочисленный литерал.
// Стань оно вычисляемым выражением, разбор не дал бы числа, и гейт не смог бы
// сверить документацию — молчать он при этом права не имеет, поэтому такое
// объявление он называет находкой.
//
// # Объявленные слепые зоны
//
//   - Корпус — отслеживаемые git-элементы (`pkg/treecorpus`). Файл, которого
//     нет в индексе, невидим: вердикт обязан быть свойством коммита, а не рабочего
//     каталога;
//   - формулировка обещания и его число обязаны стоять в ОДНОМ окне (строка
//     формулировки ± строка). Число, унесённое в соседний абзац, читается как
//     «формулировка без числа» — находка, а не тишина;
//   - запрет литерала действует в прод-дереве сервиса-владельца
//     (payloadFloorLiteralScope). Проба вправе писать число явно: она для того и
//     существует, чтобы утверждать конкретную величину.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// payloadFloorIdent — имя величины. Гейт ищет ОБЪЯВЛЕНИЕ и ССЫЛКИ по разбору
// синтаксиса, а не подстрокой: строковый литерал с этим именем (в тексте самого
// гейта, например) объявлением не является и читателем тоже.
const payloadFloorIdent = "GuaranteedPayloadFloorBytes"

// payloadFloorLiteralScope — прод-дерево владельца величины, где литерал,
// равный ей, запрещён. Путь от корня дерева, слэш-разделённый.
const payloadFloorLiteralScope = "services/vpc/"

// payloadFloorAnchor — каноническая формулировка обещания в документации.
//
// Именно «минимум полезной нагрузки», а не «MTU минус накладные расходы»: по
// величине накладных расходов опознаётся конкретная инкапсуляция, то есть
// конкретная реализация сетевой фабрики. Формулировка, из которой это следует,
// на публичной поверхности — дефект дизайна, поэтому гейт закрепляет ту
// формулировку, которая ничего о фабрике не сообщает.
var payloadFloorAnchor = regexp.MustCompile(`(?i)минимум[а-яё]*\s+полезной\s+нагрузки`)

// payloadFloorBytesNumber — число, названное байтами. Пробелы внутри числа
// допускаются (типографская запись «1 400 байт»), иначе гейт читал бы такую
// запись как отсутствие числа.
var payloadFloorBytesNumber = regexp.MustCompile(`([0-9][0-9\s\x{00a0}\x{202f}]*)\s*байт`)

// payloadFloorFinding — расхождение. Координата обязательна: находка без файла и
// строки не является действием.
type payloadFloorFinding struct {
	File string
	Line int
	What string
}

func (f payloadFloorFinding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.What)
	}
	return fmt.Sprintf("%s: %s", f.File, f.What)
}

// payloadFloorCensus — объём осмотренного. Печатается всегда: без него «ноль
// расхождений» неотличимо от «ноль прочитанного».
type payloadFloorCensus struct {
	GoFilesParsed int
	DocFilesRead  int
	// Declarations — объявлений величины найдено (обязано быть ровно одно).
	Declarations int
	// Value — величина из объявления (0, если объявления нет или оно не литерал).
	Value int
	// Readers — ссылок на величину из прод-кода вне файла объявления.
	Readers int
	// LiteralsInScope — литералов, равных величине, в прод-дереве владельца
	// (обязано быть ноль).
	LiteralsInScope int
	// AnchoredWindows — окон документации с канонической формулировкой.
	AnchoredWindows int
	// NumbersChecked — байтовых чисел, сверенных внутри этих окон.
	NumbersChecked int
	Findings       int
}

func (c payloadFloorCensus) String() string {
	return fmt.Sprintf(
		"осмотрено: файлов Go %d, файлов документации %d; объявлений величины %d (значение %d), "+
			"читателей в прод-коде %d, литералов в %s %d, окон с формулировкой обещания %d, "+
			"сверено чисел %d; РАСХОЖДЕНИЙ: %d",
		c.GoFilesParsed, c.DocFilesRead, c.Declarations, c.Value, c.Readers,
		payloadFloorLiteralScope, c.LiteralsInScope, c.AnchoredWindows, c.NumbersChecked, c.Findings)
}

// goSighting — место в Go-файле, где встретилось имя или число.
type goSighting struct {
	File string
	Line int
}

// goScan — то, что гейт вычитал из Go-корпуса.
type goScan struct {
	// declarations — объявления величины (файл, строка).
	declarations []goSighting
	// declaredValue — значение из ПЕРВОГО объявления; -1, если оно не литерал.
	declaredValue int
	// declaredIn — файл объявления (rel), чтобы не считать его собственным
	// читателем и не ловить его же литерал.
	declaredIn string
	// readers — ссылки на имя из прод-кода вне файла объявления.
	readers []goSighting
	// intLiterals — целочисленные литералы прод-дерева владельца: значение → места.
	intLiterals map[int][]goSighting
	filesParsed int
}

// auditGuaranteedPayloadFloor — единственный судья и для дерева, и для инъекции.
// Инъекция обязана исполнять ТОТ ЖЕ код, иначе она доказывает свойство своей
// копии.
func auditGuaranteedPayloadFloor(root string) ([]payloadFloorFinding, payloadFloorCensus, error) {
	files, err := treecorpus.Under(root)
	if err != nil {
		return nil, payloadFloorCensus{}, err
	}

	var findings []payloadFloorFinding
	census := payloadFloorCensus{}

	scan, err := scanGoForPayloadFloor(root, files)
	if err != nil {
		return nil, payloadFloorCensus{}, err
	}
	census.GoFilesParsed = scan.filesParsed
	census.Declarations = len(scan.declarations)
	census.Readers = len(scan.readers)

	switch {
	case len(scan.declarations) == 0:
		findings = append(findings, payloadFloorFinding{
			File: "<дерево>",
			What: fmt.Sprintf("величина %s не объявлена ни в одном файле — обещание, которое "+
				"документация даёт арендатору, не закреплено ничем: сверять её число не с чем, "+
				"и страж старта не может отказать посадке, объявившей меньше", payloadFloorIdent),
		})
	case len(scan.declarations) > 1:
		for _, d := range scan.declarations {
			findings = append(findings, payloadFloorFinding{
				File: d.File, Line: d.Line,
				What: fmt.Sprintf("величина %s объявлена здесь и ещё в %d месте(ах) — два места "+
					"об одном предмете расходятся на первой же правке, и расходятся молча",
					payloadFloorIdent, len(scan.declarations)-1),
			})
		}
	}

	if len(scan.declarations) == 1 && scan.declaredValue < 0 {
		findings = append(findings, payloadFloorFinding{
			File: scan.declarations[0].File, Line: scan.declarations[0].Line,
			What: fmt.Sprintf("величина %s задана не целочисленным литералом — предпосылка "+
				"этого гейта («число задано здесь») перестала быть верной, и сверить с ним "+
				"документацию нечем. Это отказ, а не пропуск: молчащий гейт хуже отсутствующего",
				payloadFloorIdent),
		})
	}

	value := scan.declaredValue
	census.Value = value

	if len(scan.declarations) == 1 && len(scan.readers) == 0 {
		findings = append(findings, payloadFloorFinding{
			File: scan.declarations[0].File, Line: scan.declarations[0].Line,
			What: fmt.Sprintf("величину %s не читает ни один файл прод-кода — объявление без "+
				"читателя не держит обещание, а лишь выглядит закреплённым: посадка вправе "+
				"объявить меньше обещанного, и ничто её не остановит", payloadFloorIdent),
		})
	}

	if value > 0 {
		for _, s := range scan.intLiterals[value] {
			census.LiteralsInScope++
			findings = append(findings, payloadFloorFinding{
				File: s.File, Line: s.Line,
				What: fmt.Sprintf("литерал %d в прод-коде — это величина обещания, записанная "+
					"вторым местом; сошлись на %s, иначе копии разойдутся на первой правке и "+
					"разойдутся там, где расхождение не видно", value, payloadFloorIdent),
			})
		}
	}

	docFindings, docCensus, err := checkPayloadFloorDocs(root, files, value)
	if err != nil {
		return nil, payloadFloorCensus{}, err
	}
	findings = append(findings, docFindings...)
	census.DocFilesRead = docCensus.DocFilesRead
	census.AnchoredWindows = docCensus.AnchoredWindows
	census.NumbersChecked = docCensus.NumbersChecked

	census.Findings = len(findings)
	return findings, census, nil
}

// scanGoForPayloadFloor — разбор Go-корпуса: объявления, читатели, литералы.
func scanGoForPayloadFloor(root string, files []string) (goScan, error) {
	scan := goScan{declaredValue: -1, intLiterals: map[int][]goSighting{}}
	fset := token.NewFileSet()

	// Первый проход — объявления: файл объявления нужен, чтобы не счесть его
	// собственным читателем и не поймать его же литерал.
	type parsed struct {
		rel  string
		file *ast.File
	}
	var trees []parsed
	for _, abs := range files {
		if !strings.HasSuffix(abs, ".go") {
			continue
		}
		rel := payloadFloorRel(root, abs)
		src, err := os.ReadFile(filepath.Clean(abs))
		if err != nil {
			return goScan{}, fmt.Errorf("чтение %s: %w", rel, err)
		}
		f, err := parser.ParseFile(fset, abs, src, parser.SkipObjectResolution)
		if err != nil {
			// Неразбираемый Go — отказ, а не пропуск: пропустив файл, гейт отдал бы
			// «ноль находок» на непрочитанном.
			return goScan{}, fmt.Errorf("разбор %s: %w", rel, err)
		}
		scan.filesParsed++
		trees = append(trees, parsed{rel: rel, file: f})

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
					if name.Name != payloadFloorIdent {
						continue
					}
					pos := fset.Position(name.Pos())
					scan.declarations = append(scan.declarations, goSighting{File: rel, Line: pos.Line})
					if scan.declaredIn == "" {
						scan.declaredIn = rel
						if i < len(vs.Values) {
							if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.INT {
								if n, err := strconv.Atoi(lit.Value); err == nil {
									scan.declaredValue = n
								}
							}
						}
					}
				}
			}
		}
	}

	// Второй проход — читатели и литералы. Отдельным проходом, потому что файл
	// объявления известен только после первого.
	for _, p := range trees {
		isTest := strings.HasSuffix(p.rel, "_test.go")
		inScope := strings.HasPrefix(p.rel, payloadFloorLiteralScope)
		ast.Inspect(p.file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Ident:
				if node.Name == payloadFloorIdent && !isTest && p.rel != scan.declaredIn {
					pos := fset.Position(node.Pos())
					scan.readers = append(scan.readers, goSighting{File: p.rel, Line: pos.Line})
				}
			case *ast.BasicLit:
				if node.Kind != token.INT || isTest || !inScope || p.rel == scan.declaredIn {
					return true
				}
				if v, err := strconv.Atoi(node.Value); err == nil {
					pos := fset.Position(node.Pos())
					scan.intLiterals[v] = append(scan.intLiterals[v], goSighting{File: p.rel, Line: pos.Line})
				}
			}
			return true
		})
	}
	return scan, nil
}

// checkPayloadFloorDocs — документация: окна с канонической формулировкой обещания
// обязаны называть его число, и число обещания не вправе стоять там, где такого
// окна нет.
func checkPayloadFloorDocs(root string, files []string, value int) ([]payloadFloorFinding, payloadFloorCensus, error) {
	var findings []payloadFloorFinding
	census := payloadFloorCensus{}

	for _, abs := range files {
		if !strings.HasSuffix(abs, ".md") && !strings.HasSuffix(abs, ".mdx") {
			continue
		}
		rel := payloadFloorRel(root, abs)
		raw, err := os.ReadFile(filepath.Clean(abs))
		if err != nil {
			return nil, payloadFloorCensus{}, fmt.Errorf("чтение %s: %w", rel, err)
		}
		census.DocFilesRead++
		lines := strings.Split(string(raw), "\n")

		// Окна формулировки: строка с формулировкой ± строка, не переходя через
		// пустую строку (границу абзаца).
		anchored := map[int]bool{} // номера строк (1-based), попавшие в какое-либо окно
		for i, ln := range lines {
			if !payloadFloorAnchor.MatchString(ln) {
				continue
			}
			census.AnchoredWindows++
			from, to := i, i
			if i > 0 && strings.TrimSpace(lines[i-1]) != "" {
				from = i - 1
			}
			if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
				to = i + 1
			}
			var found bool
			for j := from; j <= to; j++ {
				anchored[j+1] = true
				for _, m := range payloadFloorBytesNumber.FindAllStringSubmatch(lines[j], -1) {
					n, err := payloadFloorParseNumber(m[1])
					if err != nil {
						continue
					}
					census.NumbersChecked++
					if n == value {
						found = true
						continue
					}
					findings = append(findings, payloadFloorFinding{
						File: rel, Line: j + 1,
						What: fmt.Sprintf("документация называет обещание величиной %d байт, "+
							"а в дереве объявлено %d — два места об одном предмете разошлись; "+
							"верно ровно одно, и правится оно вместе со вторым", n, value),
					})
				}
			}
			if !found {
				findings = append(findings, payloadFloorFinding{
					File: rel, Line: i + 1,
					What: fmt.Sprintf("формулировка обещания стоит без его числа — величина "+
						"обязана быть названа в том же окне (строка ± строка), иначе сверять "+
						"её с объявлением в дереве (%d байт) нечем", value),
				})
			}
		}

		// Обратная сторона: число обещания, названное байтами ВНЕ окна
		// формулировки. Такое место гейту невидимо по существу — оно разойдётся с
		// объявлением молча.
		if value <= 0 {
			continue
		}
		for i, ln := range lines {
			if anchored[i+1] {
				continue
			}
			for _, m := range payloadFloorBytesNumber.FindAllStringSubmatch(ln, -1) {
				n, err := payloadFloorParseNumber(m[1])
				if err != nil || n != value {
					continue
				}
				findings = append(findings, payloadFloorFinding{
					File: rel, Line: i + 1,
					What: fmt.Sprintf("число обещания (%d байт) названо там, где нет его "+
						"канонической формулировки — гейт такое место сверить не может, и оно "+
						"переживёт правку величины. Поставь рядом формулировку обещания либо "+
						"сошлись на страницу, которая его даёт", value),
				})
			}
		}
	}

	if census.AnchoredWindows == 0 {
		findings = append(findings, payloadFloorFinding{
			File: "<документация>",
			What: fmt.Sprintf("ни одна страница не даёт обещания канонической формулировкой "+
				"(%s) — величина живёт в коде и арендатору не обещана, то есть он вынужден "+
				"догадываться, а догадка у каждого своя", payloadFloorAnchor),
		})
	}

	census.Findings = len(findings)
	return findings, census, nil
}

// payloadFloorParseNumber — «1 400» → 1400. Пробельные разделители внутри числа
// снимаются: типографская запись не должна читаться как отсутствие числа.
func payloadFloorParseNumber(s string) (int, error) {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return strconv.Atoi(b.String())
}

// payloadFloorRel — путь от корня дерева, слэш-разделённый.
//
// Своё имя, а не соседское: в пакете уже живёт одноимённый помощник с ДРУГИМ
// поведением (он не приводит разделитель к слэшу), и разделять имя с ним значило
// бы завести две семантики под одним словом.
func payloadFloorRel(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}
