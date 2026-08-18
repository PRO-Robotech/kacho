// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// resourcenameform_test.go — гейт единственности формы имени ресурса и
// единственности производства имени по умолчанию (задача #715).
//
// Предмет. Форма имени объявлялась в дереве ЧЕТЫРЕЖДЫ, и четыре объявления
// разошлись по трём осям: заглавные принимал один, подчёркивание два, пустую
// строку три. Один контракт исполнялся по-разному, и наблюдалось это
// арендатором: вторая сеть с пустым именем в проекте отвергалась, вторая
// подсеть — нет. Расхождение накопилось молча, потому что каждое отдельное
// объявление выглядело защитимым: «как общий, но здесь ещё подчёркивание».
//
// Почему гейт, а не разовая правка. Свести четыре регулярки в одну — работа на
// один коммит; удержать единственность — работа на всё время жизни дерева.
// Следующее «как общий, но…» напишут не из злого умысла, а потому что в чужом
// сервисе так уже сделано. Гейт краснеет в момент появления второго объявления,
// а не тогда, когда кто-то снова пройдёт по дереву руками.
//
// То же и для УМОЛЧАНИЯ. Правило «пустое имя не доживает до записи», рассыпанное
// по сервисам как `if name == "" { name = … }`, разойдётся ровно так же, как
// разошлись регулярки, — только тише: две формы умолчания в двух сервисах не
// видны ни в одном диффе.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: строка формы стоит и в
// комментариях, объясняющих эту же защиту (в том числе в шапке этого файла), и
// текстовый поиск принял бы объяснение за само объявление — ровно тот класс,
// который гейт и ловит.
//
// ЧЕГО ГЕЙТ НЕ ВИДИТ — названо здесь, чтобы «зелено» не читалось шире, чем есть:
//
//   - регулярку имени, написанную ИНАЧЕ (другой порядок альтернатив, другой
//     предел длины). Байт-идентичную копию канона он ловит, эквивалентную по
//     смыслу — нет: эквивалентность регулярных выражений здесь не решается;
//   - производство умолчания под ДРУГИМ именем, написанное с нуля. Ловится копия
//     объявления, а не независимая выдумка;
//   - регулярки ДРУГИХ референтов, и это намеренно. Идентификатор роли
//     (`roles/vpc.admin`), путь OCI-репозитория и DNS-имя хоста именем ресурса
//     не являются, формой имени не судятся и под гейт не подпадают. Гейт,
//     краснеющий на них, был бы снят первым же ложным срабатыванием.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// canonNameFormConst — имя константы, несущей единственную форму имени ресурса.
const canonNameFormConst = "Form"

// canonNameFormPkgDir — каталог, которому принадлежит форма. Она горизонтальна
// (её читают все семь сервисов), поэтому живёт в общем фундаменте, а не в первом
// сервисе, где понадобилась. Пакет намеренно БЕЗ транспорта: ту же форму обязан
// читать слой домена, которому grpc запрещён (architecture.md), а в Go импорт
// пакетный — потянув валидаторы ради одной строки, домен потянул бы и grpc.
const canonNameFormPkgDir = "pkg/validate/nameform"

// canonDerivationPkgDir — каталог производства умолчания. Он ДРУГОЙ: умолчание
// возвращает готовый ответ вызывающему и живёт рядом с валидаторами, а не рядом
// с формой.
const canonDerivationPkgDir = "pkg/validate"

// canonDerivationFunc — единственное производство имени по умолчанию.
const canonDerivationFunc = "defaultNameForID"

// canonSubstitutionFunc — единственная точка подстановки умолчания, через
// которую сервис обязан звать производство.
const canonSubstitutionFunc = "NameOrDefault"

type nameFormFinding struct {
	file   string
	line   int
	detail string
}

// TestResourceNameFormIsDeclaredOnce — форма имени объявлена ровно один раз, в
// общем фундаменте, и её байт-идентичной копии в дереве нет.
func TestResourceNameFormIsDeclaredOnce(t *testing.T) {
	tt := newTrackedTree(t, repoRoot(t))
	canonDecls, copies, scanned := scanNameFormDecls(t, tt)
	assertNameFormSingle(t, canonDecls, copies, scanned)
}

// scanNameFormDecls — обход дерева по корню. Вынесен, чтобы инъекция могла
// натравить гейт на синтетическое дерево: проверка, которую нельзя навести на
// подложенный дефект, о своей способности упасть не свидетельствует.
func scanNameFormDecls(t *testing.T, tt *trackedTree) (canonDecls, copies []nameFormFinding, scanned int) {
	t.Helper()
	root := tt.root
	files := goFilesForNameFormGate(tt)
	if len(files) == 0 {
		t.Fatalf("осмотрено 0 файлов Go — гейту нечего рассматривать; " +
			"молчаливый зелёный здесь означал бы «проверено»")
	}

	canonPattern := readCanonPattern(t, root)

	for _, f := range files {
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, f.abs, nil, 0)
		if err != nil {
			t.Fatalf("%s: разбор: %v", f.rel, err)
		}
		scanned++
		inCanonPkg := strings.HasPrefix(filepath.ToSlash(f.rel), canonNameFormPkgDir+"/")

		ast.Inspect(af, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, v := range vs.Values {
				lit, isLit := v.(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil || val != canonPattern {
					continue
				}
				pos := fset.Position(lit.Pos())
				find := nameFormFinding{file: f.rel, line: pos.Line,
					detail: "литерал формы имени"}
				if inCanonPkg {
					canonDecls = append(canonDecls, find)
				} else {
					copies = append(copies, find)
				}
			}
			return true
		})
	}
	return canonDecls, copies, scanned
}

func assertNameFormSingle(t *testing.T, canonDecls, copies []nameFormFinding, scanned int) {
	t.Helper()
	t.Logf("осмотрено файлов Go: %d; объявлений канона в %s: %d; копий вне него: %d",
		scanned, canonNameFormPkgDir, len(canonDecls), len(copies))

	if len(canonDecls) != 1 {
		for _, f := range canonDecls {
			t.Errorf("%s:%d: %s", f.file, f.line, f.detail)
		}
		t.Fatalf("форма имени обязана быть объявлена в %s РОВНО ОДИН раз, найдено %d. "+
			"Второе объявление — это второе правило: они разойдутся молча, как разошлись "+
			"четыре прежних валидатора (#715).", canonNameFormPkgDir, len(canonDecls))
	}
	for _, f := range copies {
		t.Errorf("%s:%d: байт-идентичная копия формы имени вне %s. "+
			"Зови validate.%s — копия перестанет совпадать с каноном в тот день, "+
			"когда канон поправят, и никто этого не заметит.",
			f.file, f.line, canonNameFormPkgDir, canonNameFormConst)
	}
}

// TestDefaultNameDerivationIsDeclaredOnce — производство имени по умолчанию и
// точка его подстановки объявлены ровно по одному разу на всё дерево.
func TestDefaultNameDerivationIsDeclaredOnce(t *testing.T) {
	tt := newTrackedTree(t, repoRoot(t))
	decls, scanned := scanDerivationDecls(t, tt)
	assertDerivationSingle(t, decls, scanned)
}

func scanDerivationDecls(t *testing.T, tt *trackedTree) (map[string][]nameFormFinding, int) {
	t.Helper()
	files := goFilesForNameFormGate(tt)
	if len(files) == 0 {
		t.Fatalf("осмотрено 0 файлов Go — гейту нечего рассматривать")
	}

	decls := map[string][]nameFormFinding{}
	scanned := 0
	for _, f := range files {
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, f.abs, nil, 0)
		if err != nil {
			t.Fatalf("%s: разбор: %v", f.rel, err)
		}
		scanned++
		for _, d := range af.Decls { //nolint:gocritic // обход объявлений файла
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Name == nil {
				continue
			}
			name := fd.Name.Name
			if name != canonDerivationFunc && name != canonSubstitutionFunc {
				continue
			}
			pos := fset.Position(fd.Pos())
			decls[name] = append(decls[name], nameFormFinding{
				file: f.rel, line: pos.Line, detail: "func " + name})
		}
	}
	return decls, scanned
}

func assertDerivationSingle(t *testing.T, decls map[string][]nameFormFinding, scanned int) {
	t.Helper()
	t.Logf("осмотрено файлов Go: %d; объявлений %s: %d; объявлений %s: %d",
		scanned, canonDerivationFunc, len(decls[canonDerivationFunc]),
		canonSubstitutionFunc, len(decls[canonSubstitutionFunc]))

	for _, fn := range []string{canonDerivationFunc, canonSubstitutionFunc} {
		found := decls[fn]
		if len(found) == 1 {
			rel := filepath.ToSlash(found[0].file)
			if !strings.HasPrefix(rel, canonDerivationPkgDir+"/") {
				t.Errorf("%s:%d: %s объявлена вне %s. Производство имени горизонтально — "+
					"его зовут все сервисы, значит место ему в общем фундаменте.",
					found[0].file, found[0].line, fn, canonDerivationPkgDir)
			}
			continue
		}
		for _, f := range found {
			t.Errorf("%s:%d: %s", f.file, f.line, f.detail)
		}
		t.Errorf("%s обязана быть объявлена РОВНО ОДИН раз на дерево, найдено %d. "+
			"Две формы умолчания в двух сервисах не видны ни в одном диффе — "+
			"поэтому единственность держится гейтом, а не вниманием (#715).",
			fn, len(found))
	}
}

type nameFormFile struct {
	abs string
	rel string
}

// goFilesForNameFormGate — файлы Go, подлежащие осмотру.
//
// Состав берётся у ДЕРЕВА (индекс git), а не обходом диска. Под корнем лежат
// каталоги, которых в репозитории нет — рабочие копии агентов, отчёты прогонов,
// сборочные и сгенерированные каталоги, — и прочитав их, гейт сделал бы свой
// вердикт свойством ЧУЖОГО рабочего каталога, а не коммита. Ошибка при этом
// работает в обе стороны: красное на файле, которого в репозитории нет, и
// молчание в свежем checkout там, где гейт обязан говорить. Поймано мета-гейтом
// TestTreeWalkersAskTheIndex — эта проверка сама была его находкой.
//
// Сгенерированные стабы исключены: их пишет buf, а не человек.
func goFilesForNameFormGate(tt *trackedTree) []nameFormFile {
	var out []nameFormFile
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		if strings.HasPrefix(rel, "pkg/api/") {
			continue
		}
		out = append(out, nameFormFile{abs: filepath.Join(tt.root, filepath.FromSlash(rel)), rel: rel})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out
}

func readCanonPattern(t *testing.T, root string) string {
	t.Helper()
	pat, err := findCanonPattern(root)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return pat
}

// findCanonPattern читает саму форму из константы канона.
//
// Читается ИЗ ДЕРЕВА, а не выписывается сюда литералом: выписанная копия и есть
// то, что гейт запрещает, и она разошлась бы с каноном первой.
//
// Возвращает ошибку, а не роняет пробу, чтобы инъекция могла проверить ОТКАЗ
// как исход: гейт, не нашедший канона, обязан отказаться судить, а не выйти
// успехом с «копий не найдено».
func findCanonPattern(root string) (string, error) {
	dir := filepath.Join(root, filepath.FromSlash(canonNameFormPkgDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("чтение %s: %w", canonNameFormPkgDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		af, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			return "", fmt.Errorf("%s/%s: разбор: %w", canonNameFormPkgDir, e.Name(), perr)
		}
		var found string
		ast.Inspect(af, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, nm := range vs.Names {
				if nm.Name != canonNameFormConst || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, uerr := strconv.Unquote(lit.Value); uerr == nil {
						found = v
					}
				}
			}
			return true
		})
		if found != "" {
			return found, nil
		}
	}
	return "", fmt.Errorf("константа %s не найдена в %s — предпосылка гейта не выполнена: "+
		"он судит копии по канону, а канона нет", canonNameFormConst, canonNameFormPkgDir)
}
