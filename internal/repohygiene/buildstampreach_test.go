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
// ВЕДОМОСТЬ ИСТЕКЛА — И ЭТО ЦЕЛЬ, А НЕ ПОЛОМКА
//
// Ведомость ПУСТА (#2527): штамп ставят все объявившие. Пустая ведомость держателя
// не ослабляет — механизм остаётся, и он двусторонний: запись, чей двоичный файл
// штамп ПОЛУЧИЛ, — находка («исключению нечего исключать»), запись про
// несуществующий двоичный файл — тоже. Значит следующий, кто заведёт образ со
// штампом и забудет подстановку, узнает об этом от прогона, а не от чьей-то
// памяти, и вернуть запись «на время» молча не получится.
//
// Числа «сколько ставят» здесь намеренно нет: у числа в прозе нет владельца, и
// эта шапка состарилась бы ровно так же, как состарился абзац, из-за которого
// держатель заведён. Величину печатает перепись прогона.
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
// Он НЕ судит, откуда взято значение: `-X main.buildVersion=v1.2.3` подстановкой
// является и до двоичного файла доедет — просто скажет не про это дерево. Ось
// «значение взято ИМЕННО из аргумента, которым клеймится образ» требует ЗНАТЬ
// имена аргументов, а здесь они намеренно не выписаны: держатель обязан
// опознавать и службу с её `buildRevision`, и платформу с `buildCommit`, и
// следующее семейство, чьих имён ещё нет. Эту ось держит гейт службы доступа
// `services/iam/internal/supplyhygiene`, где имена аргументов — часть предмета.
//
// Он НЕ доказывает, что собранный образ и вправду несёт величину: это требовало
// бы демона сборки, а проба, упирающаяся в демон, даёт «не выполнилось», а не
// вердикт. Здесь судится ОБЪЯВЛЕНИЕ — файл сборки, — и этого довольно, чтобы
// поймать оба тихих способа сломать штамп: отсутствие `-X` и невидимый аргумент.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЯТАЯ ОСЬ: ВИДИМОСТЬ АРГУМЕНТА В СТУПЕНИ (#2527)
//
// Область видимости `ARG` — ступень. Строка сборки, берущая значение из
// аргумента, объявленного в ДРУГОЙ ступени (обычно в конечной, где он нужен
// клейму), получает ПУСТУЮ строку — и штамп молча становится «не проставлено».
//
// Ось заведена не умозрительно: до неё держатель на таком файле сборки печатал
// «ставит» и МОЛЧАЛ. То есть первые четыре оси зеленели ровно на том состоянии,
// ради которого весь держатель написан, — и заметить это можно было только
// собрав образ.
//
// Находка этой оси ведомостью НЕ прощается. Запись ведомости означает «образ
// штампа ещё не ставит» — известный долг с предметом; образ, назвавший `-X` от
// невидимого аргумента, долгом не является: он выглядит проставленным и отвечает
// пустой строкой, то есть хуже нештампованного, у которого хотя бы умолчание
// читается как «dev».
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

	"github.com/PRO-Robotech/corelib/treecorpus"
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
	// ArgsUnseen — аргументы сборки, из которых берётся значение `-X`, но
	// которые в ЭТОЙ ступени не объявлены. Область видимости аргумента —
	// ступень: объявленный в другой, здесь он даёт ПУСТУЮ подстановку, и штамп
	// молча становится «не проставлено». Поле отдельно от `Stamped` намеренно —
	// см. врезку о пятой оси в шапке файла.
	ArgsUnseen []string
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
	// StageArgs — объявлений аргумента, ВИДИМЫХ в своей ступени. Печатается,
	// чтобы «ноль невидимых» было отличимо от «ступени не разбирались вовсе».
	StageArgs int
}

// BuildStampDebtEntry — запись ведомости: двоичный файл, штампа НЕ получающий.
type BuildStampDebtEntry struct {
	// Dir — каталог композиционного корня, от корня дерева.
	Dir string
	// Reason — причина и предмет. Пустая причина запрещена: запись без причины
	// не отличима от забытой.
	Reason string
}

// BuildStampDebt — ведомость двоичных файлов, до которых подстановка НЕ доходит.
//
// ПУСТА, и это ЦЕЛЬ, а не недосмотр: пять записей, заведённых вместе с
// держателем, сняты по одной вместе со своими образами (#2527). Держатель на
// пустой ведомости зелен и предметом не обделён — он по-прежнему обходит дерево
// и роняет прогон на всяком объявившем, до кого подстановка не дошла.
//
// Заводя запись, назови ПРИЧИНУ и ПРЕДМЕТ: запись без причины неотличима от
// забытой, и держатель на такой краснеет. Запись, чей образ штамп получил, —
// тоже находка: исключению нечего исключать.
var BuildStampDebt = []BuildStampDebtEntry{}

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
// Сверяется ХВОСТ пути, а не полный: сборка идёт либо из корня СВОЕГО модуля
// (так собиралась служба доступа, пока её модуль лежал в этом дереве), либо из
// корня дерева (`./services/vpc/cmd/vpc`) — так собираются все шесть служб
// платформы. Требовать полный путь значило бы объявить один из двух законных
// способов ненаблюдаемым; форма сохранена и вернётся в работу вместе со вторым
// модулем.
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

// stampArgRefRe — ссылка на аргумент сборки в значении `-X`: `$ИМЯ` либо
// `${ИМЯ}`. Разбирается ЗНАЧЕНИЕ, а не вся строка: `$GOOS` и прочие переменные
// той же команды к штампу отношения не имеют, и требовать их объявления
// аргументом значило бы краснеть на исправной сборке.
var stampArgRefRe = regexp.MustCompile(`^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)

// stampArgOf — имя аргумента, из которого берётся значение символа в этой
// инструкции. Пустая строка означает «значение не ссылается на аргумент» —
// это ЗАКОННО (литерал доедет до двоичного файла) и здесь не судится; см.
// «чего держатель не закрывает» в шапке.
func stampArgOf(instruction, symbol string) string {
	marker := "-X main." + symbol + "="
	i := strings.Index(instruction, marker)
	if i < 0 {
		return ""
	}
	value := instruction[i+len(marker):]
	if j := strings.IndexAny(value, " \t\""); j >= 0 {
		value = value[:j]
	}
	m := stampArgRefRe.FindStringSubmatch(value)
	if m == nil {
		return ""
	}
	return m[1]
}

// argNameOf — имя, объявленное инструкцией `ARG`.
func argNameOf(instruction string) string {
	name := strings.TrimSpace(strings.TrimPrefix(instruction, "ARG "))
	name, _, _ = strings.Cut(name, "=")
	return strings.TrimSpace(name)
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
		// stageArgs — аргументы, объявленные В ТЕКУЩЕЙ ступени и ДО текущей
		// строки. Сбрасывается на каждом `FROM`: область видимости аргумента —
		// ступень. Объявления до первого `FROM` сюда НЕ попадают намеренно —
		// глобальный аргумент виден строкам `FROM`, но внутри ступени требует
		// повторного объявления, и молчаливо считать его видимым значило бы
		// прощать ровно тот промах, ради которого эта ось заведена.
		stageArgs := map[string]bool{}
		inStage := false
		for _, instruction := range dockerInstructionsOf(string(files[path])) {
			census.Instructions++
			if strings.HasPrefix(instruction, "FROM ") {
				stageArgs = map[string]bool{}
				inStage = true
				continue
			}
			if strings.HasPrefix(instruction, "ARG ") {
				if inStage {
					stageArgs[argNameOf(instruction)] = true
					census.StageArgs++
				}
				continue
			}
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
						continue
					}
					// Ссылка на аргумент разбирается ТОЛЬКО у подставляемого
					// символа: у неподставленного значения нет, и находка про
					// невидимый аргумент увела бы читателя от настоящей причины.
					arg := stampArgOf(instruction, s)
					if arg == "" || stageArgs[arg] {
						continue
					}
					if !hasString(b.ArgsUnseen, arg) {
						b.ArgsUnseen = append(b.ArgsUnseen, arg)
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
		sort.Strings(b.ArgsUnseen)
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
		"из них сборок %d · объявлений аргумента в своей ступени %d", census.GoFiles,
		census.Dockerfiles, census.Instructions, census.BuildCommands, census.StageArgs)
	t.Logf("ОБЪЯВЛЯЮТ штамп %d · СТАВЯТ %d · не ставят %d — это и есть та величина, "+
		"которая прежде стояла числом в прозе deploy/Makefile",
		census.Declaring, census.Stamped, census.Unstamped)
	for _, b := range binaries {
		state := "НЕ ставит"
		if b.Stamped {
			state = "ставит"
		}
		if len(b.ArgsUnseen) > 0 {
			state += fmt.Sprintf(" · аргумент вне ступени %v", b.ArgsUnseen)
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
		// ПЯТАЯ ОСЬ, и ведомость её НЕ прощает: запись ведомости говорит «этот
		// образ штампа ещё не ставит» — известный долг с предметом. Образ,
		// назвавший `-X` от невидимого аргумента, долгом не является: он
		// выглядит проставленным и отвечает пустой строкой, то есть хуже
		// нештампованного, у которого хотя бы умолчание читается как «dev».
		for _, arg := range b.ArgsUnseen {
			t.Errorf("%s: сборка (%v) берёт значение из аргумента %s, не объявленного "+
				"в ТОЙ ЖЕ ступени — область видимости аргумента ступенью и ограничена, "+
				"поэтому подстановка даст ПУСТУЮ строку, а витрина ответит «не "+
				"проставлено», ничем это не показав. Объявите `ARG %s` в ступени сборки, "+
				"непосредственно перед `go build`", b.Dir, b.BuiltBy, arg, arg)
		}
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
