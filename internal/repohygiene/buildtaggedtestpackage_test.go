// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildtaggedtestpackage_test.go — пакет, чьи пробы стоят под признаком сборки,
// обязан СОБИРАТЬСЯ под этим признаком.
//
// # Предмет
//
// Признак сборки (`//go:build <тег>`) выводит файл из обычной сборки. Пока тег
// никто не передаёт, содержимое такого файла не компилируется НИ РАЗУ: ни
// `go build ./...`, ни `go vet ./...`, ни `go test ./... -short` его не читают.
// Ошибка в нём — обращение к методу, которого нет, — живёт в дереве сколько
// угодно, и все обычные проверки при этом зелёные.
//
// Цена не в самом файле, а в ПАКЕТЕ: тестовый бинарь собирается целиком, поэтому
// одна несобираемая строка под тегом отменяет прогон ВСЕХ проб пакета — включая
// те, что тега не несут.
//
// # Где наблюдалось
//
// `services/compute/internal/repo` (задача #489): проба владения узлом звала
// `r.BindingOf`, метода с таким именем в дереве не было никогда. Пакет не
// собирался под своим тегом с момента заведения файла (#284), то есть под
// `-tags=integration` не исполнялась ни одна из 74 проб пакета. Обычная сборка
// и короткий прогон всё это время были зелёными — они этот файл не читали.
//
// # Почему предикат — сборка, а не текст
//
// Отсутствующий идентификатор не отличим от существующего никаким разбором
// синтаксиса: нужен вывод типов, то есть настоящий компилятор. Поэтому гейт
// зовёт `go vet` с тем самым тегом — ту же команду, что стоит в признаке задачи.
// Это дороже текстового предиката и это осознанная плата: текстовый предикат
// этот класс не ловит вовсе.
//
// # Чего гейт НЕ утверждает
//
// Он утверждает СОБИРАЕМОСТЬ, а не ИСПОЛНЕНИЕ. «Пакет собирается под тегом» и
// «пробы под тегом кто-то запускает» — разные свойства; второе держит
// `make test-integration`, передающий тег (см. корневой Makefile).
package repohygiene

import (
	"bufio"
	"fmt"
	"go/build/constraint"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// buildTagFinding — пакет, не собравшийся под своим признаком сборки.
type buildTagFinding struct {
	Pkg    string // rel-путь пакета
	Tag    string // признак, под которым не собрался
	Output string // что сказал компилятор — находка без этого не действие
}

func (f buildTagFinding) String() string {
	return fmt.Sprintf("%s не собирается под -tags=%s:\n%s", f.Pkg, f.Tag, f.Output)
}

// buildTagCensus — объём осмотренного. Без него «ноль находок» неотличимо от
// «ноль прочитанного», а у гейта, зовущего внешнюю команду, есть и третий исход:
// команда не отработала вовсе.
type buildTagCensus struct {
	TestFilesScanned  int
	FilesWithBuildTag int
	DistinctTags      []string
	PackagesChecked   int
	VetRuns           int
}

func (c buildTagCensus) String() string {
	return fmt.Sprintf(
		"перепись: файлов проб прочитано %d · из них с признаком сборки %d · "+
			"различных признаков %d (%s) · пакетов проверено %d · вызовов сборки %d",
		c.TestFilesScanned, c.FilesWithBuildTag, len(c.DistinctTags),
		strings.Join(c.DistinctTags, ", "), c.PackagesChecked, c.VetRuns)
}

// toolchainTags — признаки, которые выставляет САМ инструмент, а не человек.
//
// Их нельзя передавать через `-tags`: `GOOS`/`GOARCH` задаются переменными
// окружения, `cgo`/`race`/`msan` — флагами, `ignore` означает «никогда», а
// `go1.NN` вычисляется по версии. Перечень нужен, чтобы гейт не изобрёл вызов
// вида `-tags=linux`, который проверял бы не то и мог бы упасть на ровном месте.
//
// Перечень не выписывает ВСЕ значения `GOOS`/`GOARCH`: он ловит форму по
// префиксу `go1.` и по членству в наборе ниже, а незнакомое имя считает
// пользовательским — это верное умолчание, потому что пользовательский признак
// и есть предмет гейта.
var toolchainTags = map[string]bool{
	"ignore": true, "cgo": true, "race": true, "msan": true, "asan": true,
	"gc": true, "gccgo": true, "unix": true, "purego": true, "math_big_pure_go": true,
	"linux": true, "darwin": true, "windows": true, "freebsd": true, "openbsd": true,
	"netbsd": true, "dragonfly": true, "solaris": true, "aix": true, "plan9": true,
	"js": true, "wasip1": true, "android": true, "ios": true,
	"amd64": true, "arm64": true, "arm": true, "386": true, "ppc64": true,
	"ppc64le": true, "mips": true, "mips64": true, "riscv64": true, "s390x": true,
	"wasm": true, "loong64": true,
}

func isToolchainTag(tag string) bool {
	return toolchainTags[tag] || strings.HasPrefix(tag, "go1.")
}

// enablingTags — признаки, передача которых ВКЛЮЧАЕТ этот файл.
//
// Не «все идентификаторы выражения», и разница здесь — не придирка. `//go:build
// integration || !short` несёт два идентификатора, но `short` не включает файл
// ни при каком раскладе: он его ИСКЛЮЧАЕТ. Предикат «пакет обязан собираться под
// каждым упомянутым идентификатором» потребовал бы сборки под `-tags=short` —
// вызова, которого в дереве не делает никто, — и получил бы находку на законном
// коде. Такой гейт ловит форму, а не существо, и снимается первым же ложным
// срабатом.
//
// Поэтому признак берётся, только если выражение становится ИСТИННЫМ, когда
// передан он один. Это ровно то условие, при котором файл впервые попадает в
// сборку, а значит и то, при котором его содержимое впервые компилируется.
//
// Проверено на дереве в обе стороны: `integration` (compute, deploy) берётся,
// `short` (vpc) — нет; см. `TestBuildTagSelectionTakesOnlyEnablingTags`.
func enablingTags(e constraint.Expr) []string {
	mentioned := map[string]bool{}
	collectTags(e, mentioned)

	var out []string
	for tag := range mentioned {
		if isToolchainTag(tag) {
			continue
		}
		only := tag
		if e.Eval(func(t string) bool { return t == only }) {
			out = append(out, tag)
		}
	}
	sort.Strings(out)
	return out
}

// collectTags — все идентификаторы, упомянутые в выражении.
//
// Разбор идёт по дереву выражения, а не по тексту строки: текстовый предикат
// «слово после go:build» увидел бы у `a || !b` только один идентификатор.
func collectTags(e constraint.Expr, into map[string]bool) {
	switch x := e.(type) {
	case *constraint.TagExpr:
		into[x.Tag] = true
	case *constraint.NotExpr:
		collectTags(x.X, into)
	case *constraint.AndExpr:
		collectTags(x.X, into)
		collectTags(x.Y, into)
	case *constraint.OrExpr:
		collectTags(x.X, into)
		collectTags(x.Y, into)
	}
}

// buildTagLine — строка `//go:build …` файла, если она есть.
//
// Читаются только строки ДО первого `package`: `//go:build` ниже объявления
// пакета инструментом не читается, и гейт, принявший такую строку за признак,
// звал бы сборку с несуществующим тегом.
func buildTagLine(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 — путь приходит из индекса git этого дерева
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "package ") {
			return "", nil
		}
		if constraint.IsGoBuild(line) {
			return line, nil
		}
	}
	return "", sc.Err()
}

// auditBuildTaggedTestPackages — судья. Обходит индекс git, собирает признаки
// сборки у файлов проб и требует, чтобы каждый пакет собирался под каждым своим
// признаком.
//
// Вынесен из тела теста, чтобы ТОТ ЖЕ судья судил синтетическое дерево пробы
// инъекции: гейт, чья способность упасть проверена другим кодом, не проверена.
func auditBuildTaggedTestPackages(root string) ([]buildTagFinding, buildTagCensus, error) {
	var census buildTagCensus

	files, err := treecorpus.UnderWithSuffix(root, "_test.go")
	if err != nil {
		return nil, census, fmt.Errorf("перечень файлов проб: %w", err)
	}
	census.TestFilesScanned = len(files)

	// пакет -> набор признаков
	byPkg := map[string]map[string]bool{}
	allTags := map[string]bool{}

	for _, abs := range files {
		// `treecorpus.Under` отдаёт АБСОЛЮТНЫЕ пути (так объявлено в её godoc);
		// пакет же именуется относительно корня, потому что именно в этой форме
		// он уезжает операндом в `go vet`.
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, census, fmt.Errorf("путь %s относительно корня: %w", abs, err)
		}
		rel = filepath.ToSlash(rel)

		line, err := buildTagLine(abs)
		if err != nil {
			return nil, census, fmt.Errorf("чтение %s: %w", rel, err)
		}
		if line == "" {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return nil, census, fmt.Errorf("разбор признака сборки в %s: %w", rel, err)
		}
		kept := enablingTags(expr)
		if len(kept) == 0 {
			continue
		}
		census.FilesWithBuildTag++

		pkg := filepath.ToSlash(filepath.Dir(rel))
		if byPkg[pkg] == nil {
			byPkg[pkg] = map[string]bool{}
		}
		for _, tag := range kept {
			byPkg[pkg][tag] = true
			allTags[tag] = true
		}
	}

	census.DistinctTags = buildTagSortedKeys(allTags)
	census.PackagesChecked = len(byPkg)

	var findings []buildTagFinding
	for _, pkg := range buildTagSortedKeys(buildTagPkgSet(byPkg)) {
		for _, tag := range buildTagSortedKeys(byPkg[pkg]) {
			census.VetRuns++
			out, err := vetWithTag(root, pkg, tag)
			if err != nil {
				findings = append(findings, buildTagFinding{Pkg: pkg, Tag: tag, Output: out})
			}
		}
	}
	return findings, census, nil
}

// vetWithTag — та же команда, что стоит в признаке задачи #489.
//
// `go vet` выбран вместо `go build`, потому что он ЧИТАЕТ файлы проб: `go build`
// их не компилирует вовсе и на этом дефекте молчит — то есть был бы проверкой с
// формой, но без содержания.
func vetWithTag(root, pkg, tag string) (string, error) {
	cmd := exec.Command("go", "vet", "-tags="+tag, "./"+pkg+"/") // #nosec G204 — операнды из индекса git
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func buildTagSortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func buildTagPkgSet(m map[string]map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// TestBuildTaggedTestPackagesCompileUnderTheirOwnTag — гейт класса.
//
// ПУСТОЙ перечень признаков — законный исход, а не поломка: дерево без признаков
// сборки нечему ломать. Гейт печатает перепись и проходит. Отказом является
// другое — «ноль прочитанных файлов»: тогда молчание означает, что судья не
// работал, а вовсе не что дерево чисто.
func TestBuildTaggedTestPackagesCompileUnderTheirOwnTag(t *testing.T) {
	root := repoRoot(t)

	findings, census, err := auditBuildTaggedTestPackages(root)
	if err != nil {
		t.Fatalf("обход дерева сорвался — вердикта нет: %v", err)
	}
	t.Log(census.String())

	if census.TestFilesScanned == 0 {
		t.Fatal("прочитано ноль файлов проб — предпосылка гейта не выполнена. " +
			"«Ноль находок» здесь означало бы «ноль прочитанного», а это не вердикт")
	}

	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "пакетов, не собирающихся под своим признаком сборки: %d\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "%s\n\n", f)
	}
	b.WriteString("Пока пакет не собирается под своим признаком, НИ ОДНА его проба под этим\n")
	b.WriteString("признаком не исполняется — включая пробы, признака не несущие. Обычная\n")
	b.WriteString("сборка при этом зелёная: она эти файлы не читает.\n")
	b.WriteString(census.String())
	t.Fatal(b.String())
}
