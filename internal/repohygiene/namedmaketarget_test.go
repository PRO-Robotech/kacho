// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// namedmaketarget_test.go — команда сборки, названная читателю, обязана
// существовать.
//
// # Предмет
//
// Документ, комментарий у скрипта или шапка хука называют способ что-то
// сделать: «установка — `make install-hooks`». Читатель набирает названное и
// получает `No rule to make target`. Дальше происходит одно из двух, и оба
// плохи: он ищет цель по дереву (минуты на пустом месте) либо решает, что
// механизма нет вовсе, — и делает без него.
//
// Класс тихий по построению. Ни компилятор, ни линтер, ни `go test` не читают
// прозу; цель, которую никогда не заводили или которую переименовали, живёт в
// тексте сколько угодно долго. Наблюдалось в этом дереве дважды, и обе находки
// разные по происхождению:
//
//   - `scripts/hooks/pre-push` называл установку целью, которой не заводили
//     НИКОГДА, — и та же несуществующая команда стояла в правиле воркспейса.
//     То есть обязательный локальный прогон, специально объявленный «держится
//     гейтом, а не памятью», не держался ничем: провязать его было нечем;
//   - манифест нагрузочного прогона называл цель `load-test-address-allocate`
//     при живой `loadtest-address-allocate` — лишний дефис, переживший, судя по
//     всему, переименование.
//
// # Предикат и его границы — измерены, а не выбраны
//
// Осматриваются упоминания ВНУТРИ inline-кода (обратные кавычки): именно так
// команду НАЗЫВАЮТ читателю. Цель обязана существовать в каком-нибудь
// отслеживаемом Makefile дерева — не обязательно в ближайшем к упоминанию:
// прозе свойственно называть `make dev-up`, подразумевая `deploy/Makefile`, и
// судить о том, из какого каталога читатель запустит команду, гейт не вправе.
//
// Замер по дереву (ревизия 5df7da76, единица счёта — упоминание):
//
//	inline-код                      240 упоминаний, 2 находки, 0 ложных
//	«ближайший Makefile или корень»  237 упоминаний, 61 находка, 61 ложная
//	блоки кода ``` в .md/.mdx        +93 упоминания, +0 находок, 1 ложная
//
// Средняя строка — отвергнутый вариант предиката: он судит о том, откуда
// запускают, и даёт шестьдесят одну ложную находку на законной прозе. Нижняя —
// отвергнутое расширение охвата: блоки кода не приносят НИ ОДНОЙ находки, зато
// приносят ложную: перечисление вида make k6-{smoke,baseline,…} в дереве каталогов
// командой не является, а по форме от неё неотличимо.
// Узость здесь не осторожность, а результат: первый ложный срабат снял бы гейт
// целиком.
package repohygiene

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// makeMentionRe — вызов вида make [-C <каталог>] <цель> внутри inline-кода.
//
// Переменные окружения после цели (make test-integration SVC=vpc) ловить не
// нужно: предмет — имя цели, а оно стоит первым непустым словом после
// возможного -C <каталог>.
var makeMentionRe = regexp.MustCompile(`\bmake\s+(?:-C\s+[A-Za-z0-9_./-]+\s+)?([a-z][a-z0-9_-]*)`)

// commandStarters — чем может кончаться текст ПЕРЕД вызовом команды в одной
// строке командной оболочки.
var commandStarters = []string{"&&", "||", ";", "|", "(", "$", ">", "{"}

// mentionsInSpan — имена целей, названные внутри одного участка inline-кода.
//
// ВЫЗОВ ОБЯЗАН НАЧИНАТЬ КОМАНДУ. Без этого условия в предмет попадает
// собственная диагностика make: строка «No rule to make target» читается как
// вызов цели с именем target — и гейт краснеет на файле, который всего лишь
// цитирует сообщение об ошибке. Проверено на этом же файле: без условия он
// находил сам себя дважды. При этом составная команда остаётся видимой —
// перед вызовом стоит разделитель (cd deploy && make dev-up).
func mentionsInSpan(span string) []string {
	var out []string
	for _, loc := range makeMentionRe.FindAllStringSubmatchIndex(span, -1) {
		prefix := strings.TrimRight(span[:loc[0]], " \t")
		if prefix != "" {
			starts := false
			for _, sep := range commandStarters {
				if strings.HasSuffix(prefix, sep) {
					starts = true
					break
				}
			}
			if !starts {
				continue
			}
		}
		out = append(out, span[loc[2]:loc[3]])
	}
	return out
}

// makeRuleRe — строка объявления цели в Makefile: `test:`, `test: dep`,
// `a b:`. `:=` исключено — это присваивание переменной, а не цель.
var makeRuleRe = regexp.MustCompile(`^([A-Za-z0-9_.%/ -]+):(?:[^=]|$)`)

// inlineCodeSpans — содержимое участков в обратных кавычках, по строке.
//
// Разбором строки, а не документа: гейт читает и .md, и .go, и .yaml, и .sh —
// у них нет общего разметочного разбора, зато у всех есть одна и та же
// договорённость «команда — в обратных кавычках».
//
// СТРОКА С НЕЧЁТНЫМ ЧИСЛОМ КАВЫЧЕК УЧАСТКОВ НЕ ДАЁТ. Участок, начатый на
// предыдущей строке и закрытый на этой, оставляет открывающую кавычку там, а
// закрывающую здесь: наивное деление пополам объявило бы участком ВЕСЬ хвост
// строки — то есть обычную прозу. Найдено инъекцией в дерево на живом
// комментарии, где хвостом оказалось «…do not make a projection»: гейт прочитал
// это как цель с именем «a». Многострочный inline-код при этом не
// осматривается вовсе, и это законно: команду читателю называют в одну строку.
//
// Обратная сторона того же — этот файл. Имя несуществующей цели, поставленное в
// обратные кавычки внутри объяснения, гейт находит и обязан находить: в
// обратных кавычках стоит то, что читателю предлагают набрать. Поэтому здесь
// имена целей-примеров пишутся без кавычек, а не исключаются списком.
func inlineCodeSpans(line string) []string {
	parts := strings.Split(line, "`")
	if len(parts)%2 == 0 {
		return nil // нечётное число кавычек — участок не закрыт на этой строке
	}
	var spans []string
	for i := 1; i < len(parts); i += 2 {
		spans = append(spans, parts[i])
	}
	return spans
}

// makeTargetsOf — цели, объявленные одним Makefile.
func makeTargetsOf(body string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "\t") {
			continue // строка рецепта, а не объявления
		}
		if rest, ok := strings.CutPrefix(line, ".PHONY:"); ok {
			for _, name := range strings.Fields(rest) {
				out[name] = true
			}
			continue
		}
		m := makeRuleRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, name := range strings.Fields(m[1]) {
			if strings.HasPrefix(name, ".") {
				continue // .PHONY, .SHELLFLAGS и прочие директивы — не цели
			}
			out[name] = true
		}
	}
	return out
}

// checkNamedMakeTargets — находки одного файла и число осмотренных упоминаний.
//
// Вынесено отдельной функцией именно затем, чтобы способность гейта упасть
// доказывалась инъекцией на синтетике, не трогая дерево.
func checkNamedMakeTargets(path, raw string, known map[string]bool) (findings []string, seen int) {
	for n, line := range strings.Split(raw, "\n") {
		for _, span := range inlineCodeSpans(line) {
			for _, target := range mentionsInSpan(span) {
				seen++
				if known[target] {
					continue
				}
				findings = append(findings, path+":"+itoa(n+1)+" — названа цель сборки "+
					"make "+target+", которой нет НИ В ОДНОМ Makefile дерева. Читатель "+
					"наберёт названное и получит `No rule to make target`: дальше он либо "+
					"ищет цель по дереву, либо решает, что механизма нет, и делает без "+
					"него. Три исхода, четвёртого нет: завести цель · назвать ту, что "+
					"есть · снять упоминание вместе с обещанием")
			}
		}
	}
	return findings, seen
}

// TestNamedMakeTargetExists — по дереву.
func TestNamedMakeTargetExists(t *testing.T) {
	root := repoRoot(t)

	out, err := gitenv.Command(root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — без переписи «ноль находок» неотличимо от "+
			"«ноль прочитанного»", err)
	}
	var files []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p == "" || strings.Contains(p, "/node_modules/") {
			continue
		}
		files = append(files, p)
	}
	if len(files) == 0 {
		t.Fatal("отслеживаемых файлов ноль — обход сломан, а не дерево чисто")
	}

	// Множество целей — тоже перепись: обход, переставший находить Makefile,
	// объявил бы находкой КАЖДОЕ упоминание, а не ноль.
	known := map[string]bool{}
	makefiles := 0
	for _, f := range files {
		if base := f[strings.LastIndexByte(f, '/')+1:]; base != "Makefile" && !strings.HasSuffix(f, ".mk") {
			continue
		}
		body, err := gitShowFile(t, root, f)
		if err != nil {
			t.Errorf("%s не прочитан: %v — цели этого Makefile НЕ учтены", f, err)
			continue
		}
		makefiles++
		for name := range makeTargetsOf(body) {
			known[name] = true
		}
	}
	if makefiles == 0 || len(known) == 0 {
		t.Fatalf("Makefile найдено %d, целей %d — множество целей пусто, "+
			"вердикт был бы свойством обхода, а не дерева", makefiles, len(known))
	}

	mentions := 0
	var findings []string
	for _, f := range files {
		body, err := gitShowFile(t, root, f)
		if err != nil {
			continue // двоичное содержимое и нечитаемое — не наш предмет
		}
		fs, seen := checkNamedMakeTargets(f, body, known)
		mentions += seen
		findings = append(findings, fs...)
	}
	if mentions == 0 {
		t.Fatal("упоминаний `make <цель>` в inline-коде ноль — так не бывает, " +
			"сломан отбор участков, а не дерево чисто")
	}

	sort.Strings(findings)
	for _, msg := range findings {
		t.Error(msg)
	}
	t.Logf("осмотрено отслеживаемых файлов: %d; Makefile: %d; различных целей: %d; "+
		"упоминаний `make <цель>` в inline-коде: %d",
		len(files), makefiles, len(known), mentions)
}

// errBinaryFile — содержимое не текстовое; прозы в нём не бывает, а разбор
// байтов дал бы находку, которой никто не писал.
var errBinaryFile = errors.New("двоичное содержимое")

// gitShowFile — содержимое отслеживаемого файла из рабочего дерева.
func gitShowFile(t *testing.T, root, rel string) (string, error) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return "", errBinaryFile
	}
	return string(b), nil
}

// TestNamedMakeTargetDetectorSeesBothForms — инъекция в обе стороны.
func TestNamedMakeTargetDetectorSeesBothForms(t *testing.T) {
	// bt — обратная кавычка. Фикстуры собираются конкатенацией, а не пишутся
	// литералом: иначе гейт нашёл бы СВОИ ЖЕ фикстуры при обходе дерева и
	// покраснел бы на несуществующих целях, которые здесь заведены нарочно.
	const bt = "`"

	known := map[string]bool{"test": true, "dev-up": true, "test-integration": true}

	cases := []struct {
		name    string
		body    string
		wantHit bool
	}{
		{
			name:    "существующая цель в обратных кавычках — молчит",
			body:    "Прогон: " + bt + "make test" + bt + ".",
			wantHit: false,
		},
		{
			name:    "несуществующая цель в обратных кавычках — находка",
			body:    "Установка: " + bt + "make install-hooks" + bt + ".",
			wantHit: true,
		},
		{
			name:    "цель с переменной — читается имя цели, молчит",
			body:    "Одному сервису — " + bt + "make test-integration SVC=vpc" + bt + ".",
			wantHit: false,
		},
		{
			name:    "составная команда — цель всё равно видна, находка",
			body:    "Поднять: " + bt + "cd deploy && make dev-upp" + bt + ".",
			wantHit: true,
		},
		{
			name:    "с -C и существующей целью — молчит",
			body:    "Стенд: " + bt + "make -C deploy dev-up" + bt + ".",
			wantHit: false,
		},
		{
			name:    "с -C и несуществующей целью — находка",
			body:    "Стенд: " + bt + "make -C deploy dev-upgrade-all" + bt + ".",
			wantHit: true,
		},
		{
			name:    "английская проза без обратных кавычек — молчит",
			body:    "This does not make the tree green, and will make a mess.",
			wantHit: false,
		},
		{
			name:    "несуществующая цель БЕЗ обратных кавычек — молчит намеренно",
			body:    "надо бы завести make install-hooks когда-нибудь",
			wantHit: false,
		},
		{
			name:    "две цели в одной строке, вторая несуществующая — находка",
			body:    "Сначала " + bt + "make test" + bt + ", затем " + bt + "make relase" + bt + ".",
			wantHit: true,
		},
		{
			// Живой комментарий из дерева: участок открыт строкой выше и закрыт
			// здесь. Хвост строки — проза, и «do not make a projection» читалось
			// бы как цель с именем «a».
			name:    "хвост многострочного участка — проза, а не команда, молчит",
			body:    "// version is newer AND nothing changed. " + bt + "labels =\n// $5::jsonb" + bt + " is equality — whitespace does not make a projection",
			wantHit: false,
		},
		{
			// Собственная диагностика make. Без правила «вызов начинает команду»
			// это читалось бы как цель с именем target, и гейт краснел бы на
			// файле, объясняющем его же находку.
			name:    "цитата сообщения make об ошибке — молчит",
			body:    "Читатель получит " + bt + "No rule to make target" + bt + ".",
			wantHit: false,
		},
		{
			name:    "вызов после точки с запятой — находка",
			body:    "Одной строкой: " + bt + "cd deploy; make nosuchtarget" + bt + ".",
			wantHit: true,
		},
		{
			name:    "участок открыт и не закрыт вовсе — молчит",
			body:    "Смотри " + bt + "make install-hooks и дальше по тексту",
			wantHit: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, seen := checkNamedMakeTargets("синтетика.md", tc.body, known)
			if got := len(findings) > 0; got != tc.wantHit {
				t.Fatalf("ожидалась находка=%v, получено %v (осмотрено упоминаний %d): %v",
					tc.wantHit, got, seen, findings)
			}
		})
	}
}

// TestNamedMakeTargetParserReadsRules — разбор объявлений Makefile: без него
// множество известных целей молча оскудеет, и гейт начнёт краснеть на законном.
func TestNamedMakeTargetParserReadsRules(t *testing.T) {
	body := strings.Join([]string{
		"SHELL := bash",
		".PHONY: test test-unit help",
		"test: test-unit test-integration",
		"\t@echo не цель: make рецепт-строка",
		"docs-sites:",
		"\tpython3 build.go",
		"SVC_PATH = services/$(SVC)",
	}, "\n")

	got := makeTargetsOf(body)
	for _, want := range []string{"test", "test-unit", "help", "docs-sites"} {
		if !got[want] {
			t.Errorf("цель %q не распознана — гейт объявит её упоминание находкой", want)
		}
	}
	for _, notTarget := range []string{"SHELL", "SVC_PATH", ".PHONY", "рецепт-строка"} {
		if got[notTarget] {
			t.Errorf("%q принято за цель — множество известных шире дерева, "+
				"настоящая находка пройдёт незамеченной", notTarget)
		}
	}
}
