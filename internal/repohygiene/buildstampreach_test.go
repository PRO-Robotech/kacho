// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildstampreach_test.go — сборка ПРОСТАВЛЯЕТ штамп каждому двоичному файлу,
// который его объявляет; число «сколько ставят» принадлежит ЭТОМУ держателю, а
// не прозе (#2521).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Двоичный файл объявляет `buildVersion` и второй символ и кормит ими ряд
// `*_build_info`. Само по себе это не гарантирует ничего: компоновщик
// подставляет значение по ПОЛНОМУ имени символа и молчит, когда такого символа
// нет, а сборка, не назвавшая `-ldflags` вовсе, оставляет умолчание. Витрина в
// обоих случаях отвечает — и отвечает неправду.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ДЕРЖАТЕЛЬ, ЕСЛИ ПРО ЭТО УЖЕ БЫЛ АБЗАЦ
//
// Абзац и был предметом находки. В `deploy/Makefile` стояло «бинари объявляют
// buildVersion/buildCommit (ЧЕТЫРЕ сервиса) … ни одна сборка дерева этого не
// делает» — верно в день записи и ложно через месяц: образ службы доступа штамп
// проставил. Число в прозе не имеет владельца: оно стареет молча и утаскивает за
// собой вывод, сделанный из него, — «штамповать нечем», хотя образец уже лежит
// рядом.
//
// Поэтому величина переехала СЮДА. Перепись печатает обе стороны — «объявляют N ·
// ставят M», — а расхождение с ведомостью роняет прогон.
//
// ─────────────────────────────────────────────────────────────────────────────
// ВЕДОМОСТЬ ИСТЕКАЕТ САМА, И ЭТО НЕСУЩЕЕ СВОЙСТВО
//
// Сегодня штамп ставит ОДИН образ из шести объявляющих. Остальные пять названы
// ведомостью с причиной и предметом: запись, чей двоичный файл штамп ПОЛУЧИЛ, —
// находка («исключению нечего исключать»), запись про несуществующий двоичный
// файл — тоже. Значит следующий, кто научит образ штамповать, узнает об этой
// ведомости от прогона, а не от чьей-то памяти.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАЗБИРАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ
//
// Слово `-ldflags` стоит в комментариях — в шапках композиционных корней, в
// шапке этого файла и в объявлении самой сборки. Поиск по подстроке над сырым
// файлом нашёл бы СВОЁ СОБСТВЕННОЕ объяснение и остался бы зелёным при снятой
// подстановке. Поэтому строки комментариев отбрасываются ДО сшивки продолжений:
// команда сборки разнесена по нескольким физическим строкам, и построчный разбор
// увидел бы половину.
//
// Символы ищутся разбором синтаксического дерева, а не образцом: имя
// `buildVersion` встречается и в прозе, и в строковых литералах фикстур.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ ДЕРЖАТЕЛЬ НЕ ЗАКРЫВАЕТ — сказано прямо
//
// Он не судит, ВИДЕН ли аргумент сборки в той ступени, где идёт сборка (аргумент
// из чужой ступени даёт молчаливую пустую строку). Эту ось держит гейт службы
// доступа `services/iam/internal/supplyhygiene` — он же и единственный, кому
// сегодня есть что проверять. Здесь предмет другой: ДОХОДИТ ли подстановка до
// каждого объявившего.
//
// Способность упасть и смолчать доказана инъекцией — buildstampreach_injection_test.go.
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
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// buildStampPrimarySymbol — символ, по которому двоичный файл опознаётся как
// НЕСУЩИЙ штамп. Второй символ у семейств разный (`buildCommit` у платформы,
// `buildRevision` у службы доступа), поэтому он выводится, а не выписывается:
// перечень из двух имён разошёлся бы с деревом на первом же новом семействе.
const buildStampPrimarySymbol = "buildVersion"

// buildStampSecondarySymbols — законные имена ВТОРОГО символа. Перечень закрыт
// намеренно: символ с третьим именем обязан покраснеть здесь, а не остаться вне
// наблюдения.
var buildStampSecondarySymbols = []string{"buildCommit", "buildRevision"}

// StampedBinary — двоичный файл, объявивший штамп.
type StampedBinary struct {
	// Dir — каталог композиционного корня, от корня дерева.
	Dir string
	// Symbols — объявленные символы штампа в порядке объявления.
	Symbols []string
	// BuiltBy — файлы сборки, чья ИСПОЛНЯЕМАЯ строка собирает этот пакет.
	BuiltBy []string
	// Stamped — хотя бы одна такая строка подставляет ВСЕ объявленные символы.
	Stamped bool
}

// BuildStampReachCensus — объём осмотренного. Печатается ВСЕГДА: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type BuildStampReachCensus struct {
	GoFiles       int
	Dockerfiles   int
	Instructions  int
	BuildCommands int
	Declaring     int
	Stamped       int
	Unstamped     int
}

// BuildStampDebtEntry — запись ведомости: двоичный файл, штампа НЕ получающий.
type BuildStampDebtEntry struct {
	// Dir — каталог композиционного корня, от корня дерева.
	Dir string
	// Reason — причина и предмет. Пустая причина запрещена: запись без причины
	// не отличима от забытой.
	Reason string
}

// BuildStampDebt — ведомость двоичных файлов, до которых подстановка НЕ
// доходит. Пять записей на 2026-09-10; ведомость обязана ОПУСТЕТЬ, и пустая
// ведомость — цель, а не поломка.
//
// Предмет всех пяти один и назван задачей: метрика `*_build_info` сообщает
// version="dev" commit="unknown", то есть отвечает неправду. Образец, по
// которому это чинится, лежит рядом — `services/iam/Dockerfile`.
var BuildStampDebt = []BuildStampDebtEntry{
	{"gateway/cmd/api-gateway", "#2527 — образ края штамп не проставляет"},
	{"services/compute/cmd/compute", "#2527 — образ compute штамп не проставляет"},
	{"services/geo/cmd/kacho-geo", "#2527 — образ geo штамп не проставляет"},
	{"services/nlb/cmd/kacho-loadbalancer", "#2527 — образ nlb штамп не проставляет"},
	{"services/vpc/cmd/vpc", "#2527 — образ vpc штамп не проставляет"},
}

// dockerInstructionsOf — исполняемая часть файла сборки, сшитая по продолжениям
// строки. Комментарии отбрасываются ЦЕЛИКОМ и ДО сшивки: иначе продолжение,
// начинающееся с решётки, унесло бы с собой хвост команды.
func dockerInstructionsOf(raw string) []string {
	var out []string
	var cur strings.Builder
	cont := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		body := strings.TrimSpace(strings.TrimSuffix(trimmed, "\\"))
		if cont {
			cur.WriteString(" ")
		}
		cur.WriteString(body)
		cont = strings.HasSuffix(trimmed, "\\")
		if cont {
			continue
		}
		out = append(out, cur.String())
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// buildsPackage — собирает ли инструкция пакет с этим последним сегментом.
//
// Сверяется ХВОСТ пути, а не полный: у службы доступа сборка идёт из своего
// модуля (`./cmd/kaname`), у платформы — из корня дерева
// (`./services/vpc/cmd/vpc`). Требовать полный путь значило бы объявить один из
// двух законных способов ненаблюдаемым.
func buildsPackage(instruction, base string) bool {
	if !strings.Contains(instruction, "go build") {
		return false
	}
	re := regexp.MustCompile(`(^|\s)\.(?:/[A-Za-z0-9_.-]+)*/cmd/` +
		regexp.QuoteMeta(base) + `(\s|$)`)
	return re.MatchString(instruction)
}

// injectsSymbol — подставляет ли инструкция ЭТОТ символ компоновщиком.
func injectsSymbol(instruction, symbol string) bool {
	return strings.Contains(instruction, "-ldflags") &&
		strings.Contains(instruction, "-X main."+symbol+"=")
}

// AuditBuildStampReach разбирает ПРОИЗВОЛЬНЫЙ корпус (путь → содержимое):
// настоящее дерево и синтетический мир инъекции проходят одну функцию, поэтому
// доказанное на втором верно для первого.
func AuditBuildStampReach(files map[string][]byte) ([]StampedBinary, BuildStampReachCensus, error) {
	var census BuildStampReachCensus
	if len(files) == 0 {
		return nil, census, fmt.Errorf(
			"на вход подано ноль файлов — обход дерева не состоялся, и молчание " +
				"держателя ничего не утверждает")
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Полоса 1: кто ОБЪЯВЛЯЕТ штамп.
	byDir := map[string]*StampedBinary{}
	for _, path := range paths {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, files[path], 0)
		if perr != nil {
			// Неразбираемый исходник — предмет компилятора, не этого разбора.
			continue
		}
		census.GoFiles++
		if file.Name == nil || file.Name.Name != "main" {
			continue
		}
		symbols := stampSymbolsOf(file)
		if len(symbols) == 0 {
			continue
		}
		dir := pathDir(path)
		b, seen := byDir[dir]
		if !seen {
			b = &StampedBinary{Dir: dir}
			byDir[dir] = b
		}
		for _, s := range symbols {
			if !hasString(b.Symbols, s) {
				b.Symbols = append(b.Symbols, s)
			}
		}
	}

	// Полоса 2: что СОБИРАЕТ каждого из них и подставляет ли.
	for _, path := range paths {
		if filepath.Base(path) != "Dockerfile" {
			continue
		}
		census.Dockerfiles++
		for _, instruction := range dockerInstructionsOf(string(files[path])) {
			census.Instructions++
			if !strings.Contains(instruction, "go build") {
				continue
			}
			census.BuildCommands++
			for _, b := range byDir {
				if !buildsPackage(instruction, filepath.Base(b.Dir)) {
					continue
				}
				if !hasString(b.BuiltBy, path) {
					b.BuiltBy = append(b.BuiltBy, path)
				}
				all := len(b.Symbols) > 0
				for _, s := range b.Symbols {
					if !injectsSymbol(instruction, s) {
						all = false
					}
				}
				if all {
					b.Stamped = true
				}
			}
		}
	}

	out := make([]StampedBinary, 0, len(byDir))
	for _, b := range byDir {
		sort.Strings(b.BuiltBy)
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })

	census.Declaring = len(out)
	for _, b := range out {
		if b.Stamped {
			census.Stamped++
			continue
		}
		census.Unstamped++
	}
	return out, census, nil
}

// stampSymbolsOf — символы штампа, ОБЪЯВЛЕННЫЕ переменной уровня пакета.
// Первичный обязателен: файл, назвавший только второй символ, штампа не несёт.
func stampSymbolsOf(file *ast.File) []string {
	declared := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				declared[name.Name] = true
			}
		}
	}
	if !declared[buildStampPrimarySymbol] {
		return nil
	}
	out := []string{buildStampPrimarySymbol}
	for _, s := range buildStampSecondarySymbols {
		if declared[s] {
			out = append(out, s)
		}
	}
	return out
}

// pathDir — каталог пути в форме со слешами.
func pathDir(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return "."
}

// hasString — есть ли строка в срезе. Имя своё: в пакете уже живёт `contains`
// с другим предметом, и переиспользование чужого имени сделало бы вердикт
// функцией порядка файлов.
func hasString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// buildStampCorpus — отслеживаемые файлы дерева, спрошенные У ИНДЕКСА git.
//
// Довод не стилистический: под каталогами служб на всякой машине, где поднимали
// стенд, лежат распаковки чартов и рабочие копии полос, и обход диска посчитал
// бы чужие файлы сборки наравне с деревом.
func buildStampCorpus(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева: %v — «ноль находок» здесь означало бы «ноль прочитанного»", err)
	}
	corpus := map[string][]byte{}
	for _, abs := range files {
		if !strings.HasSuffix(abs, ".go") && filepath.Base(abs) != "Dockerfile" {
			continue
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			t.Fatalf("путь %s: %v", abs, relErr)
		}
		body, readErr := os.ReadFile(abs) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		corpus[filepath.ToSlash(rel)] = body
	}
	return corpus
}

// TestBuildStampReachesEveryBinaryThatDeclaresIt — подстановка доходит до
// каждого объявившего штамп, либо он назван ведомостью с причиной.
func TestBuildStampReachesEveryBinaryThatDeclaresIt(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	binaries, census, err := AuditBuildStampReach(buildStampCorpus(t, root))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("перепись: исходников разобрано %d · файлов сборки %d · инструкций %d · "+
		"из них сборок %d", census.GoFiles, census.Dockerfiles, census.Instructions,
		census.BuildCommands)
	t.Logf("ОБЪЯВЛЯЮТ штамп %d · СТАВЯТ %d · не ставят %d — это и есть та величина, "+
		"которая прежде стояла числом в прозе deploy/Makefile",
		census.Declaring, census.Stamped, census.Unstamped)
	for _, b := range binaries {
		state := "НЕ ставит"
		if b.Stamped {
			state = "ставит"
		}
		t.Logf("  %-40s символы %v · собирает %v · %s", b.Dir, b.Symbols, b.BuiltBy, state)
	}

	if census.Dockerfiles == 0 || census.Declaring == 0 {
		t.Fatalf("файлов сборки %d, объявляющих штамп %d — обход не состоялся, и вердикт "+
			"беспредметен", census.Dockerfiles, census.Declaring)
	}

	forgiven := map[string]string{}
	for _, e := range BuildStampDebt {
		if e.Reason == "" {
			t.Errorf("%s: запись ведомости без причины — не отличима от забытой", e.Dir)
		}
		if _, dup := forgiven[e.Dir]; dup {
			t.Errorf("%s: запись ведомости задвоена", e.Dir)
		}
		forgiven[e.Dir] = e.Reason
	}

	known := map[string]bool{}
	for _, b := range binaries {
		known[b.Dir] = true
		switch {
		case b.Stamped && forgiven[b.Dir] != "":
			t.Errorf("%s: штамп ПОСТАВЛЕН, а ведомость его ещё прощает (%q) — "+
				"исключению нечего исключать, снимите запись", b.Dir, forgiven[b.Dir])
		case !b.Stamped && forgiven[b.Dir] == "":
			if len(b.BuiltBy) == 0 {
				t.Errorf("%s: объявляет символы %v, а файла сборки, который собирает этот "+
					"пакет, в дереве НЕТ — витрина ответит умолчанием, и производителя у "+
					"величины не существует вовсе", b.Dir, b.Symbols)
				continue
			}
			t.Errorf("%s: объявляет символы %v, а сборка (%v) их не подставляет — "+
				"ряд *_build_info ответит version=\"dev\" и это будет неправдой. "+
				"Образец подстановки — services/iam/Dockerfile; либо назовите двоичный "+
				"файл ведомостью BuildStampDebt с причиной и предметом",
				b.Dir, b.Symbols, b.BuiltBy)
		}
	}
	for dir := range forgiven {
		if !known[dir] {
			t.Errorf("%s: ведомость прощает двоичный файл, который штампа не объявляет "+
				"(либо переехал) — запись потеряла предмет", dir)
		}
	}
}
