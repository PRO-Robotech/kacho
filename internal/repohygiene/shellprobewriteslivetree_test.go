// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// shellprobewriteslivetree_test.go — shell-проба не пишет в дерево, из которого запущена.
//
// # Предмет
//
// Половина этого класса, написанная на Go, стоит рядом
// (`probewriteslivetree_test.go`, задача #696) и в собственной шапке называет
// свою слепую зону: shell-суиты она не читает вовсе. Между тем три из четырёх
// экземпляров класса, найденных при её заведении, жили именно в shell — проба
// правила ЖИВОЙ файл дерева и возвращала его последней строкой тела, а
// прерывание до этой строки не доходит.
//
// # Почему предикат не текстовый — измерено, а не выбрано по вкусу
//
// Текстовый поиск по форме `… > "$FILE"` пробовался при закрытии #696 и отвергнут
// числом: правку через встроенный python (`python3 - "$file" <<PY … open(path,
// "w")`) он не видит, потому что цель записи там уезжает АРГУМЕНТОМ, а сама
// запись живёт в чужом языке. Из трёх экземпляров он нашёл бы ОДИН и объявил бы
// остальные два чистыми — то есть напечатал бы «ноль находок», прочитав треть
// предмета.
//
// Поэтому здесь разбирается shell: слово, кавычки, документ-вставка, присваивание,
// параметр функции. Происхождение прослеживается по ЗНАЧЕНИЮ, ровно как в Go-половине.
//
// # Норма
//
// `multi-agent-flow.md` §«НЕПРИКОСНОВЕННОСТЬ ЧУЖОГО СОСТОЯНИЯ»: проба изолирует
// то, что правит. Копия дерева в `mktemp -d`, свой каталог, своё окружение —
// исход; уборка в конце тела и даже `trap` исходом НЕ являются: `SIGKILL` не
// доставляется, а `trap EXIT` не выполняется при снятии процесса девятым сигналом.
//
// # Предикат
//
// Корпус — отслеживаемые `*.sh`, которые дерево само называет пробами: лежащие
// под `tests/`/`test/`/`e2e/` либо названные `*-test.sh`/`*_test.sh`/`test-*.sh`.
// Ограничение не косметическое: инструменты дерева (генерация пинов образов,
// подкачка подчартов, установка хуков) пишут в живое дерево ПО СВОЕМУ
// НАЗНАЧЕНИЮ, и запрет был бы запретом на них — та же граница, по которой
// Go-половина берёт `*_test.go`, а не весь Go-код. Число названо в переписи:
// сколько скриптов в дереве всего и сколько из них попало в корпус.
//
// Производитель живого корня НЕ выписан списком имён (в дереве их два десятка:
// `DEPLOY_ROOT`, `REPO_ROOT`, `UMBRELLA`, `SCRIPT_DIR`, `NEWMAN_DIR`, `ROOT`, …),
// а выведен из ИСПОЛНЯЕМОЙ формы: значение, полученное от пути САМОГО скрипта
// (`$0`, `${BASH_SOURCE[0]}`) либо от `git rev-parse --show-toplevel`, и всё, что
// от него произведено — склейкой, `dirname`, `cd … && pwd`, `readlink -f`,
// `realpath`.
//
// Находка = вызов записи, чья ЦЕЛЬ происходит от живого корня. Цель берётся по
// смыслу команды, а не «любой живой аргумент»: у `cp`/`mv` пишется ПОСЛЕДНИЙ
// аргумент, и живой первый — это законное чтение живого дерева. Ровно так
// устроен возврат в починенных суитах (`cp "$DEPLOY_ROOT/$rel" "$WORK/$rel"`), и
// предикат «любой аргумент живой» покрасил бы их — то есть первый же ложный
// срабат отключил бы гейт целиком.
//
// # Чего предикат НЕ ловит — названо числом, а не умолчано
//
//   - **восхождение от рабочего каталога процесса** (`$(cd ../.. && pwd)` без
//     опоры на `$0`/`${BASH_SOURCE}`). В корпусе такой производитель ровно один
//     (предикат: `git grep -n 'cd \.\./' -- '*.sh' | grep '\$(cd'`), и записи
//     от него не ведётся вовсе — значение уходит только в чтение;
//   - **`git` без `-C`**: подкоманда исполняется в текущем каталоге, живого
//     СЛОВА в вызове нет, и вести происхождение не от чего. Распознаётся форма с
//     явным корнем (`git -C "$ROOT" add`) — единственная, встречающаяся в дереве;
//   - **`dd of=…`, `ed`/`ex`, `patch`** — не распознаются; экземпляров в корпусе
//     нет;
//   - **нагрузка не на python** (awk/perl/ruby внутри документа-вставки).
//     Владельцев документов-вставок в корпусе два — `cat` и `python3`; третьего
//     языка там нет, и ветка под него была бы кодом, который нечем проверить;
//   - **нагрузка пишет в ОДИН аргумент, а живым является ДРУГОЙ** — гейт
//     покраснеет: цель ВНУТРИ чужого языка не разбирается. В корпусе такого нет:
//     живая цель во всём корпусе одна, и она у перенаправления, а не у нагрузки;
//   - **смена рабочего каталога** (`cd "$ROOT"`) с последующей записью по
//     ОТНОСИТЕЛЬНОМУ пути: живого значения в такой записи нет вовсе;
//   - **каталоги** (`mkdir`/`rmdir`) — обоснование стоит у таблицы команд:
//     git не отслеживает каталоги, поэтому оставленный каталог не виден ни одному
//     читателю корпуса.
//
// Границы куплены точностью сознательно: гейт с ложным срабатыванием снимают
// первым, и тогда не остаётся ничего. Цена измерена — см. сообщение коммита:
// предикат «любой живой аргумент — цель» вместо «цель по смыслу команды» даёт на
// чистом стволе восемь находок, и все восемь ложные, ровно на ПОЧИНЕННЫХ суитах.
//
// # Раскладка
//
// Здесь — происхождение значения, распознавание записи и сам гейт. Разбор
// shell-исходника (слово, кавычки, документ-вставка, границы функций) живёт в
// `shellsource_test.go`, разрешение пути цели — в `shellpathresolve_test.go`,
// доказательство способности краснеть и молчать — в
// `shellprobewriteslivetree_injection_test.go`. Разнесено по предметам, а не по
// объёму: тронуть один, не читая остальных, иначе нельзя.
package repohygiene

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/gitenv"
)

// ─────────────────────────────────────────────────────────────────────────────
// Находка и перепись.
// ─────────────────────────────────────────────────────────────────────────────

// shellLiveWriteFinding — место, где живой корень доезжает до записи.
type shellLiveWriteFinding struct {
	File   string // путь относительно корня дерева
	Line   int
	Func   string // функция, в теле которой стоит вызов ("" — верхний уровень)
	What   string // чем пишет
	Target string // во что пишет, как записано в исходнике
}

// shellLiveWriteCensus — объём осмотренного. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного», поэтому счётчики печатаются всегда и на нуле корпуса
// гейт падает.
type shellLiveWriteCensus struct {
	Scripts   int // скриптов корпуса разобрано
	Lines     int // строк в них
	Commands  int // простых команд осмотрено
	Funcs     int // определений функций
	Producers int // присваиваний, породивших живой корень
	Payloads  int // встроенных полезных нагрузок (документов-вставок) осмотрено
	Writers   int // из них таких, которые ПИШУТ
	Writes    int // мест записи осмотрено
	Desync    int // скриптов, на которых лексер потерял синхронизацию (не прочитаны)
	Live      int // из них с целью, производной от живого корня
	Declared  int // из них по пути, который дерево само объявило своей свалкой
	Tainted   int // остаток — находки

	// DesyncedFiles — их имена. Число без имён нечинибельно: непрочитанный
	// скрипт надо назвать, иначе «Desync 1» означает «где-то что-то».
	DesyncedFiles []string
}

// ─────────────────────────────────────────────────────────────────────────────
// Что считается записью.
// ─────────────────────────────────────────────────────────────────────────────

// shellWriteLastArg — команды, у которых пишется ПОСЛЕДНИЙ аргумент, а первые
// читаются. Различение обязательно: починенные суиты возвращают файл копии из
// живого дерева (`cp "$DEPLOY_ROOT/$rel" "$WORK/$rel"`), и предикат «любой
// аргумент живой» покрасил бы именно их.
var shellWriteLastArg = map[string]bool{
	"cp": true, "mv": true, "install": true, "rsync": true, "ln": true,
}

// shellWriteAllArgs — команды, у которых пишется КАЖДЫЙ файловый аргумент.
//
// `mkdir` и `rmdir` сюда НЕ входят, и это не упущение: git не отслеживает
// каталоги, поэтому оставленный прерванным прогоном каталог не виден ни
// `git status`, ни `git ls-files`, ни одному гейту, берущему состав корпуса у
// индекса. Вред этого класса определяется тем, что увидит СЛЕДУЮЩИЙ читатель, —
// а он не увидит ничего. Замер: без этой границы гейт давал на чистом стволе две
// находки, обе на `mkdir -p` каталога для собственных артефактов пробы, и обе
// ложные. Записи ВНУТРЬ такого каталога распознаются отдельно и своим порядком.
var shellWriteAllArgs = map[string]bool{
	"rm": true, "unlink": true, "touch": true,
	"truncate": true, "chmod": true, "chown": true, "chgrp": true, "shred": true,
	"tee": true,
}

// shellInPlaceEditors — редакторы «на месте»: пишут в файл, названный аргументом,
// когда включён соответствующий флаг.
var shellInPlaceEditors = map[string]bool{"sed": true, "perl": true, "ruby": true}

// shellEmbeddedRunners — интерпретаторы, которым нагрузка приходит документом-вставкой
// либо строкой `-c`, а цель записи — АРГУМЕНТОМ. Ровно эта форма и не ловится
// текстовым предикатом: записи `open(path, "w")` в самом shell нет.
var shellEmbeddedRunners = map[string]bool{
	"python": true, "python3": true, "py": true,
}

// shellPayloadWrites — признаки того, что встроенная нагрузка ПИШЕТ. Читающая
// нагрузка (разбор рендера, проверка схемы) записью не является, и запрет на неё
// был бы запретом на половину проб дерева.
var shellPayloadWrites = []*regexp.Regexp{
	regexp.MustCompile(`open\s*\(\s*[^,)]+,\s*['"][rbx+]*[wa]`),
	regexp.MustCompile(`\.write_text\s*\(`),
	regexp.MustCompile(`\.write_bytes\s*\(`),
	regexp.MustCompile(`os\.(remove|unlink|rename|replace|truncate|makedirs|mkdir|rmdir|symlink)\s*\(`),
	regexp.MustCompile(`shutil\.(copy|copy2|copyfile|copytree|move|rmtree)\s*\(`),
	regexp.MustCompile(`Path\s*\([^)]*\)\s*\.\s*(write_text|write_bytes|unlink|touch|rename|replace|mkdir)\s*\(`),
}

// shellGitMutators — подкоманды git, меняющие состояние репозитория. Чтения
// (`ls-files`, `rev-parse`, `status`, `diff`, `log`, `show`) сюда НЕ входят
// намеренно: пробы дерева читают живой репозиторий постоянно.
var shellGitMutators = map[string]bool{
	"add": true, "rm": true, "mv": true, "commit": true, "commit-tree": true,
	"checkout": true, "switch": true, "restore": true, "reset": true,
	"stash": true, "clean": true, "update-index": true, "update-ref": true,
	"config": true, "init": true, "apply": true, "worktree": true,
	"branch": true, "tag": true, "merge": true, "rebase": true, "am": true,
	"cherry-pick": true, "gc": true, "prune": true, "sparse-checkout": true,
	"notes": true, "symbolic-ref": true, "push": true, "hash-object": true,
	"write-tree": true, "filter-branch": true,
}

// shellPathPreserving — подстановки, ВОЗВРАЩАЮЩИЕ тот же путь в другой форме.
// Всё прочее происхождения не передаёт: вывод произвольной программы — не путь,
// произведённый от корня. `basename` и `git rev-parse --show-prefix`
// происхождение СНИМАЮТ (остаётся отрезок без корня) — как `filepath.Base` в
// Go-половине.
var shellPathPreserving = map[string]bool{
	"cd": true, "pwd": true, "dirname": true, "readlink": true, "realpath": true,
}

// ─────────────────────────────────────────────────────────────────────────────
// Происхождение значения.
// ─────────────────────────────────────────────────────────────────────────────

// shellEnv — состояние прослеживания в пределах одного скрипта.
type shellEnv struct {
	live     map[string]bool         // переменные, чьё значение происходит от живого корня
	vals     map[string]string       // и их РАЗРЕШЁННОЕ значение, если оно вычислимо статически
	selfPath string                  // путь самого скрипта в дереве: опора `$0`/`${BASH_SOURCE}`
	params   map[string]map[int]bool // функция → живые позиционные параметры (0 = «все», форма "$@")
	curFn    string
	out      map[string]map[int]bool // куда живое уехало на этом проходе
}

var shVarRef = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*|[0-9]|@|\*)`)

// shellLiveText — происходит ли значение выражения от живого корня.
//
// Разбор идёт по СОДЕРЖИМОМУ раскрываемой части: ссылка на живую переменную,
// путь самого скрипта (`$0`, `${BASH_SOURCE[0]}`) либо подстановка, сохраняющая
// путь. Вывод произвольной команды живым не считается — иначе `out="$(bash "$0")"`
// (исход прогона гейта) объявлялся бы путём в живом дереве, а таких мест в
// пробах много.
func (e *shellEnv) shellLiveText(s string) bool {
	rest := s
	for {
		idx := strings.Index(rest, "$(")
		bt := strings.Index(rest, "`")
		switch {
		case idx >= 0 && (bt < 0 || idx < bt):
			head := rest[:idx]
			if e.plainLive(head) {
				return true
			}
			inner, after, ok := shellCutSubst(rest[idx+2:], '(', ')')
			if !ok {
				return e.plainLive(rest)
			}
			if e.substLive(inner) {
				return true
			}
			rest = after
		case bt >= 0:
			head := rest[:bt]
			if e.plainLive(head) {
				return true
			}
			end := strings.IndexByte(rest[bt+1:], '`')
			if end < 0 {
				return e.plainLive(rest)
			}
			if e.substLive(rest[bt+1 : bt+1+end]) {
				return true
			}
			rest = rest[bt+1+end+1:]
		default:
			return e.plainLive(rest)
		}
	}
}

// plainLive — ссылки на переменные вне подстановок.
func (e *shellEnv) plainLive(s string) bool {
	for _, m := range shVarRef.FindAllStringSubmatch(s, -1) {
		name := m[1]
		switch {
		case name == "0":
			return true // путь самого скрипта — исходный производитель
		case name == "BASH_SOURCE":
			return true
		case name == "@" || name == "*":
			if idx := e.params[e.curFn]; idx != nil {
				for _, v := range idx {
					if v {
						return true
					}
				}
			}
		case len(name) == 1 && name[0] >= '1' && name[0] <= '9':
			if idx := e.params[e.curFn]; idx != nil {
				if idx[0] || idx[int(name[0]-'0')] {
					return true
				}
			}
		default:
			if e.live[name] {
				return true
			}
		}
	}
	return false
}

// substLive — подстановка `$( … )`: живой её делает только команда, СОХРАНЯЮЩАЯ
// путь, либо прямой опрос корня у git.
func (e *shellEnv) substLive(inner string) bool {
	if strings.Contains(inner, "rev-parse") && strings.Contains(inner, "--show-toplevel") {
		return true
	}
	cmds, _ := shellParse(inner)
	for _, c := range cmds {
		if len(c.words) == 0 {
			continue
		}
		name := shellBase(c.words[0].lit)
		if !shellPathPreserving[name] {
			continue
		}
		for _, w := range c.words[1:] {
			if e.shellLiveText(w.exp) {
				return true
			}
		}
	}
	return false
}

// shellCutSubst — вырезать содержимое подстановки с учётом вложенности.
func shellCutSubst(s string, open, close byte) (inner, after string, ok bool) {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[:i], s[i+1:], true
			}
		}
	}
	return s, "", false
}

func shellBase(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// ─────────────────────────────────────────────────────────────────────────────
// Предикат целиком.
// ─────────────────────────────────────────────────────────────────────────────

// auditShellProbeWritesToLiveTree — вход это исходники проб (путь → текст),
// чтобы инъекция гоняла ТУ ЖЕ функцию, что и гейт по дереву.
// Второй вход — объявленная деревом свалка: путь, который дерево игнорирует и не
// отслеживает. Он передаётся ФУНКЦИЕЙ, а не вычисляется внутри, чтобы инъекция
// гоняла ту же логику находки на синтетических исходниках, которых на диске нет.
func auditShellProbeWritesToLiveTree(
	sources map[string]string,
	declaredSink func(rel string) bool,
) ([]shellLiveWriteFinding, shellLiveWriteCensus) {
	var (
		findings []shellLiveWriteFinding
		census   shellLiveWriteCensus
		desynced []string
	)
	rels := make([]string, 0, len(sources))
	for rel := range sources {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		src := sources[rel]
		census.Scripts++
		census.Lines += strings.Count(src, "\n") + 1
		cmds, funcs, desync := shellParseChecked(src)
		census.Funcs += funcs
		if desync {
			census.Desync++
			desynced = append(desynced, rel)
		}

		// Параметры функций считаются до неподвижной точки: помощник зовёт
		// помощника (`run_with_injection "$LIVE"` → `inject_at "$@"`), и живой
		// корень уезжает через две границы. Три прохода покрывают наблюдаемую в
		// дереве глубину; счётчики и находки берутся с последнего.
		params := map[string]map[int]bool{}
		var last shellLiveWriteCensus
		var lastFindings []shellLiveWriteFinding
		for pass := range 3 {
			e := &shellEnv{
				live:     map[string]bool{},
				vals:     map[string]string{},
				selfPath: rel,
				params:   params,
				out:      map[string]map[int]bool{},
			}
			lastFindings, last = shellWalk(rel, cmds, e, declaredSink)
			for callee, idx := range e.out {
				if params[callee] == nil {
					params[callee] = map[int]bool{}
				}
				for i, v := range idx {
					if v {
						params[callee][i] = true
					}
				}
			}
			_ = pass
		}
		findings = append(findings, lastFindings...)
		census.Commands += last.Commands
		census.Producers += last.Producers
		census.Payloads += last.Payloads
		census.Writers += last.Writers
		census.Writes += last.Writes
		census.Live += last.Live
		census.Declared += last.Declared
		census.Tainted += last.Tainted
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	census.DesyncedFiles = desynced
	return findings, census
}

// shellWalk — один проход по командам скрипта в порядке исходника.
func shellWalk(
	rel string, cmds []shCmd, e *shellEnv, declaredSink func(string) bool,
) ([]shellLiveWriteFinding, shellLiveWriteCensus) {
	var (
		findings []shellLiveWriteFinding
		census   shellLiveWriteCensus
	)
	for _, c := range cmds {
		census.Commands++
		e.curFn = c.fn

		// Присваивания. Живое значение делает переменную живой, любое другое —
		// СНИМАЕТ метку: именно так копия дерева перебивает одноимённую
		// переменную (`UMBRELLA="$WORK/helm/umbrella"`), и без снятия починенная
		// суита осталась бы находкой.
		rest := c.words
		for len(rest) > 0 {
			w := rest[0]
			name, val, ok := shellAssignment(w)
			if !ok {
				break
			}
			if name == "local" || name == "export" || name == "declare" ||
				name == "readonly" || name == "typeset" {
				rest = rest[1:]
				continue
			}
			live := e.shellLiveText(val)
			resolved := e.shellResolve(val) // считается ДО присваивания: `X="$X/sub"`
			if live {
				census.Producers++
			}
			if strings.HasSuffix(name, "+") { // ARR+=(…) / VAR+=…
				n := strings.TrimSuffix(name, "+")
				e.live[n] = e.live[n] || live
				e.vals[n] = ""
			} else {
				e.live[name] = live
				e.vals[name] = resolved
			}
			rest = rest[1:]
		}
		// `local file="$1" bak` — присваивания идут ПОСЛЕ ключевого слова.
		if len(c.words) > 0 {
			switch c.words[0].lit {
			case "local", "export", "declare", "readonly", "typeset":
				for _, w := range c.words[1:] {
					name, val, ok := shellAssignment(w)
					if !ok {
						continue
					}
					live := e.shellLiveText(val)
					resolved := e.shellResolve(val)
					if live {
						census.Producers++
					}
					e.live[strings.TrimSuffix(name, "+")] = live
					e.vals[strings.TrimSuffix(name, "+")] = resolved
				}
			}
		}

		// Перенаправление вывода — самая частая форма правки файла из shell.
		for _, r := range c.redirs {
			census.Writes++
			if f, ok := e.shellJudge(rel, c, r, "перенаправление вывода", declaredSink, &census); ok {
				findings = append(findings, f)
			}
		}

		// Служебные слова перед именем команды. Без их снятия `elif !
		// run_with_injection "$LIVE" …` читается как команда `elif`, и вся
		// цепочка помощников остаётся невидимой — ровно та слепая зона, из-за
		// которой первая редакция предиката находила 2 экземпляра из 3.
		for len(rest) > 0 && shellCommandPrefix[rest[0].lit] {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			continue
		}
		cmdName := shellBase(rest[0].lit)
		args := rest[1:]

		if c.heredoc != "" {
			census.Payloads++
		}

		targets, what := shellWriteTargets(cmdName, args, c.heredoc, &census)
		for _, tw := range targets {
			census.Writes++
			if f, ok := e.shellJudge(rel, c, tw, what, declaredSink, &census); ok {
				findings = append(findings, f)
			}
		}

		// Команда, спрятанная в подстановке (`X="$(помощник "$LIVE")"`), — такая
		// же команда. Без спуска внутрь вызов помощника из подстановки не
		// участвует ни в передаче живого значения, ни в распознавании записи:
		// в одной из трёх исторических суит цепочка помощников вызывалась именно
		// так, и её вторая половина оставалась невидимой.
		for _, w := range c.words {
			findings, census = e.shellDescend(rel, c, w, declaredSink, findings, census)
		}
		for _, w := range c.redirs {
			findings, census = e.shellDescend(rel, c, w, declaredSink, findings, census)
		}
		e.curFn = c.fn

		// Передача живого значения в функцию того же скрипта — один шаг наружу;
		// неподвижная точка снаружи доводит его до конца цепочки помощников.
		for i, a := range args {
			if !e.shellLiveText(a.exp) {
				continue
			}
			if e.out[cmdName] == nil {
				e.out[cmdName] = map[int]bool{}
			}
			if strings.Contains(a.exp, "$@") || strings.Contains(a.exp, "$*") {
				e.out[cmdName][0] = true // «все параметры» — форма "$@"
			}
			e.out[cmdName][i+1] = true
		}
	}
	return findings, census
}

// shellJudge — приговор одному месту записи.
//
// Порядок вопросов важен и не переставляется: сперва происхождение (иначе судить
// нечего), потом разрешение пути. Неразрешимая цель с живым происхождением
// остаётся находкой — гейт не вправе оправдывать то, чего не смог прочитать.
func (e *shellEnv) shellJudge(
	rel string, c shCmd, target shWord, what string,
	declaredSink func(string) bool, census *shellLiveWriteCensus,
) (shellLiveWriteFinding, bool) {
	if !e.shellLiveText(target.exp) {
		return shellLiveWriteFinding{}, false
	}
	census.Live++
	if p := shellCleanRel(e.shellResolve(target.exp)); p != "" && declaredSink != nil && declaredSink(p) {
		census.Declared++
		return shellLiveWriteFinding{}, false
	}
	census.Tainted++
	return shellLiveWriteFinding{
		File: rel, Line: c.line, Func: c.fn, What: what, Target: target.raw,
	}, true
}

// shellDescend — спуск в подстановки слова. Номер строки берётся у ВНЕШНЕЙ
// команды: разбор подстановки начинает счёт заново, и собственный её номер
// указывал бы не туда.
func (e *shellEnv) shellDescend(
	rel string, outer shCmd, w shWord, declaredSink func(string) bool,
	findings []shellLiveWriteFinding, census shellLiveWriteCensus,
) ([]shellLiveWriteFinding, shellLiveWriteCensus) {
	for _, inner := range shellSubstBodies(w.exp) {
		cmds, funcs := shellParse(inner)
		for i := range cmds {
			cmds[i].line = outer.line
			if cmds[i].fn == "" {
				cmds[i].fn = outer.fn
			}
		}
		f, c := shellWalk(rel, cmds, e, declaredSink)
		findings = append(findings, f...)
		census.Funcs += funcs
		census.Commands += c.Commands
		census.Producers += c.Producers
		census.Payloads += c.Payloads
		census.Writers += c.Writers
		census.Writes += c.Writes
		census.Live += c.Live
		census.Declared += c.Declared
		census.Tainted += c.Tainted
	}
	return findings, census
}

// shellSubstBodies — тела подстановок `$( … )` слова, с учётом вложенности.
func shellSubstBodies(exp string) []string {
	var out []string
	rest := exp
	for {
		i := strings.Index(rest, "$(")
		if i < 0 {
			return out
		}
		inner, after, ok := shellCutSubst(rest[i+2:], '(', ')')
		if !ok {
			return out
		}
		if !strings.HasPrefix(inner, "(") { // `$(( … ))` — арифметика, не команда
			out = append(out, inner)
		}
		rest = after
	}
}

// shellAssignment — слово вида `NAME=…` / `NAME+=…` в позиции присваивания.
func shellAssignment(w shWord) (name, val string, ok bool) {
	eq := strings.IndexByte(w.raw, '=')
	if eq <= 0 {
		return "", "", false
	}
	head := w.raw[:eq]
	if strings.HasSuffix(head, "+") {
		head = head[:len(head)-1]
	}
	if head == "" {
		return "", "", false
	}
	for i := 0; i < len(head); i++ {
		if !isShNameByte(head[i]) {
			return "", "", false
		}
	}
	if head[0] >= '0' && head[0] <= '9' {
		return "", "", false
	}
	// Значение берётся из раскрываемой части: `X='$LIVE'` подстановки не делает.
	ve := strings.IndexByte(w.exp, '=')
	if ve < 0 {
		return w.raw[:eq], "", true
	}
	return w.raw[:eq], w.exp[ve+1:], true
}

// shellWriteTargets — какие слова команды являются ЦЕЛЬЮ записи.
func shellWriteTargets(cmd string, args []shWord, heredoc string, census *shellLiveWriteCensus) ([]shWord, string) {
	files := shellFileArgs(args)
	switch {
	case shellWriteLastArg[cmd]:
		if len(files) < 2 {
			return nil, ""
		}
		return files[len(files)-1:], cmd
	case shellWriteAllArgs[cmd]:
		return files, cmd
	case shellInPlaceEditors[cmd]:
		if !shellHasInPlaceFlag(args) {
			return nil, ""
		}
		// У `sed -i 's/…/…/' ФАЙЛ` первый неопционный аргумент — сам сценарий.
		if !shellHasScriptFlag(args) && len(files) > 1 {
			files = files[1:]
		}
		return files, cmd + " -i"
	case shellEmbeddedRunners[cmd]:
		payload := heredoc
		if s, ok := shellDashCScript(args); ok {
			payload += "\n" + s
		}
		if payload == "" {
			return nil, ""
		}
		if !shellPayloadWritesTo(payload) {
			return nil, ""
		}
		census.Writers++
		return shellRunnerFileArgs(args), cmd + " (встроенная нагрузка пишет)"
	case cmd == "git":
		sub, ok := shellGitSubcommand(args)
		if !ok {
			return nil, ""
		}
		return args, "git " + sub
	}
	return nil, ""
}

// shellCommandPrefix — слова, стоящие ПЕРЕД именем команды: ключевые слова
// оболочки и обёртки, не меняющие того, что исполняется.
var shellCommandPrefix = map[string]bool{
	"if": true, "elif": true, "while": true, "until": true, "then": true,
	"else": true, "do": true, "!": true, "time": true, "exec": true,
	"command": true, "builtin": true, "nohup": true,
}

// shellFileArgs — аргументы, не являющиеся флагами.
func shellFileArgs(args []shWord) []shWord {
	var out []shWord
	for _, a := range args {
		if strings.HasPrefix(a.raw, "-") && a.lit != "" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func shellHasInPlaceFlag(args []shWord) bool {
	for _, a := range args {
		if a.lit == "" || !strings.HasPrefix(a.lit, "-") {
			continue
		}
		if a.lit == "--in-place" || strings.HasPrefix(a.lit, "--in-place=") {
			return true
		}
		if strings.HasPrefix(a.lit, "-") && !strings.HasPrefix(a.lit, "--") &&
			strings.Contains(a.lit, "i") {
			return true
		}
	}
	return false
}

func shellHasScriptFlag(args []shWord) bool {
	for _, a := range args {
		if a.lit == "-e" || a.lit == "-f" || strings.HasPrefix(a.lit, "--expression") ||
			strings.HasPrefix(a.lit, "--file") {
			return true
		}
	}
	return false
}

// shellDashCScript — сценарий, переданный интерпретатору строкой `-c`.
func shellDashCScript(args []shWord) (string, bool) {
	for i, a := range args {
		if a.lit == "-c" && i+1 < len(args) {
			return args[i+1].raw, true
		}
	}
	return "", false
}

// shellRunnerFileArgs — аргументы интерпретатора, которые может назвать нагрузка.
// Сам сценарий (`-c …`) и маркер стандартного ввода (`-`) файлами не являются.
func shellRunnerFileArgs(args []shWord) []shWord {
	var out []shWord
	skip := -1
	for i, a := range args {
		if i == skip {
			continue
		}
		if a.lit == "-c" {
			skip = i + 1
			continue
		}
		if a.lit == "-" || (strings.HasPrefix(a.raw, "-") && a.lit != "") {
			continue
		}
		out = append(out, a)
	}
	return out
}

func shellPayloadWritesTo(payload string) bool {
	code := shellStripPythonComments(payload)
	for _, re := range shellPayloadWrites {
		if re.MatchString(code) {
			return true
		}
	}
	return false
}

// shellStripPythonComments — комментарий в нагрузке исполняемым не является.
// Без этого пояснение «open(path, "w") здесь запрещён» само стало бы находкой.
func shellStripPythonComments(s string) string {
	var b strings.Builder
	for _, ln := range strings.Split(s, "\n") {
		inS, inD := false, false
		cut := -1
		for i := 0; i < len(ln); i++ {
			switch ln[i] {
			case '\'':
				if !inD {
					inS = !inS
				}
			case '"':
				if !inS {
					inD = !inD
				}
			case '#':
				if !inS && !inD {
					cut = i
				}
			}
			if cut >= 0 {
				break
			}
		}
		if cut >= 0 {
			ln = ln[:cut]
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return b.String()
}

func shellGitSubcommand(args []shWord) (string, bool) {
	for _, a := range args {
		if a.lit == "" {
			continue
		}
		if shellGitMutators[a.lit] {
			return a.lit, true
		}
	}
	return "", false
}

// ─────────────────────────────────────────────────────────────────────────────
// Корпус и гейт.
// ─────────────────────────────────────────────────────────────────────────────

// isShellProbePath — дерево само называет пробой то, что лежит под `tests/`,
// `test/`, `e2e/` либо названо `*-test.sh`/`*_test.sh`/`test-*.sh`. Признак взят
// у дерева, а не выписан списком файлов: список разошёлся бы с деревом молча.
func isShellProbePath(rel string) bool {
	if !strings.HasSuffix(rel, ".sh") {
		return false
	}
	segs := strings.Split(rel, "/")
	for _, s := range segs[:len(segs)-1] {
		switch s {
		case "tests", "test", "e2e":
			return true
		}
	}
	base := segs[len(segs)-1]
	if strings.HasSuffix(base, "-test.sh") || strings.HasSuffix(base, "_test.sh") ||
		strings.HasPrefix(base, "test-") || strings.HasPrefix(base, "test_") {
		return true
	}
	return isShellRoleProbe(base)
}

// isShellRoleProbe — проба, узнаваемая по РОЛИ, а не по месту.
//
// # Почему одного места недостаточно (замерено, а не предположено)
//
// Отбор по каталогу `tests/` брал 73 скрипта из 119 и оставлял снаружи ещё 21,
// которые пробами являются по существу: семь утверждений о посадке стенда, шесть
// проверок фильтра списков, гейт над гейтами, два вносителя дефектов, страж
// рендера и судья дрейфа. Двое из них вносят дефект НАМЕРЕННО — то есть ровно то
// действие, ради которого #724 и заведена, — и ни один не читался.
//
// Это тот же промах, из-за которого отвергнут текстовый предикат: он находил
// треть предмета и печатал «ноль находок». Здесь предикат находил бы 78 % и
// печатал то же самое — доля другая, класс тот же.
//
// # Чем этот отбор плох и почему он всё-таки взят
//
// Отбор по имени меряет СОГЛАШЕНИЕ ОБ ИМЕНОВАНИИ, а не роль: проба, названная
// иначе, останется непрочитанной. Признать это дешевле, чем спрятать, поэтому
// перепись гейта печатает и знаменатель — сколько скриптов в дереве всего.
// Расхождение видно числом, а не догадкой; сузить его — работа предиката, а не
// умолчания.
func isShellRoleProbe(base string) bool {
	for _, pre := range []string{"assert-", "check-", "audit-", "verify-", "inject-"} {
		if strings.HasPrefix(base, pre) {
			return true
		}
	}
	return strings.Contains(base, "-inject") || strings.Contains(base, "self-test") ||
		strings.HasSuffix(base, "-guard.sh") || base == "judge.sh"
}

// TestShellProbesDoNotWriteIntoTheTreeTheyRunFrom — гейт по дереву.
func TestShellProbesDoNotWriteIntoTheTreeTheyRunFrom(t *testing.T) {
	root := repoRoot(t)
	sources, allScripts, matched := shellProbeSources(t, root)

	if len(sources) == 0 {
		t.Fatal("обход не нашёл ни одной shell-пробы — гейт беспредметен: либо состав " +
			"дерева взять не удалось, либо соглашение об именовании проб сменилось. " +
			"В обоих случаях зелёный вердикт ниже был бы получен даром.")
	}

	sink := shellDeclaredSink(t, root)
	findings, census := auditShellProbeWritesToLiveTree(sources, sink)

	// Предпосылки предиката. Без производителя живого корня прослеживать нечего,
	// без единого места записи — не о чем судить, без единой встроенной нагрузки
	// нераспознанным оказался бы ровно тот вид, ради которого гейт и написан.
	// Молчание тогда означало бы поломку разбора, а не чистоту дерева.
	if census.Producers == 0 {
		t.Error("в корпусе не найдено ни одного производителя живого корня — источник, " +
			"от которого предикат ведёт происхождение, исчез, и «ноль находок» ниже " +
			"неотличимо от «ноль прочитанного»")
	}
	if census.Writes == 0 {
		t.Error("в корпусе не найдено ни одного места записи — распознавание записи сломано")
	}
	if census.Desync > 0 {
		t.Errorf("лексер потерял синхронизацию на %d скрипте(ах): %s.\n"+
			"Это НЕ находка и НЕ чистота: непрочитанный скрипт не идёт ни в числитель, "+
			"ни молча в знаменатель. Незакрытая цитата (либо форма цитирования, которую "+
			"разбор ещё не знает) уводит лексер до конца файла, и дальше он читает код "+
			"как строку. Наблюдалось на живой пробе: 467 строк разобрались в ДВЕ команды, "+
			"и «ноль находок» по ней было получено даром",
			census.Desync, strings.Join(census.DesyncedFiles, ", "))
	}
	if census.Payloads == 0 {
		t.Error("в корпусе не найдено ни одной встроенной нагрузки (документа-вставки). " +
			"Читается это двояко, и оба чтения требуют действия: либо сломан разбор " +
			"документов-вставки — а именно он отличает этот предикат от текстового, " +
			"нашедшего бы треть предмета, — либо пробы дерева перестали пользоваться " +
			"вставками вовсе, и тогда предпосылка снимается ОСОЗНАННО, вместе с формой. " +
			"Тихо пройти нельзя: «ноль находок» ниже опирается на этот разбор")
	}

	for _, f := range findings {
		where := f.Func
		if where == "" {
			where = "верхний уровень"
		}
		t.Errorf("проба %s:%d (%s) пишет через %s в %s:\n"+
			"  цель производна от корня ЖИВОГО дерева — прерывание прогона "+
			"(снятие по времени, нехватка места, SIGKILL) оставит правку в рабочей копии, "+
			"а возврата у прерванного прогона нет: `trap EXIT` девятым сигналом не выполняется.\n\n"+
			"Исход один: инъекция идёт в КОПИЮ (`WORK=$(mktemp -d); cp -r \"$ROOT/.\" \"$WORK/\"`), "+
			"и гейт зовётся из копии. Возврат в конце тела исходом НЕ является.",
			f.File, f.Line, where, f.What, f.Target)
	}

	if matched != len(sources) {
		t.Errorf("проб в корпусе %d, а прочитано %d: %d файлов не открылись. "+
			"Непрочитанный файл не идёт ни в числитель, ни молча в знаменатель — "+
			"иначе «ноль находок» означало бы «ноль просмотренного»",
			matched, len(sources), matched-len(sources))
	}

	t.Logf("перепись: скриптов в дереве %d, из них проб разобрано %d (строк %d), "+
		"команд осмотрено %d, функций %d, производителей живого корня %d, "+
		"встроенных нагрузок %d (из них пишущих %d), мест записи %d, "+
		"из них по живому корню %d, из тех по объявленной свалке %d, находок %d; "+
		"скриптов с потерей синхронизации %d",
		allScripts, census.Scripts, census.Lines, census.Commands, census.Funcs,
		census.Producers, census.Payloads, census.Writers, census.Writes,
		census.Live, census.Declared, census.Tainted, census.Desync)
}

// shellDeclaredSink — путь, который дерево САМО объявило свалкой: он не
// отслеживается И покрыт правилом игнорирования. Оба условия обязательны:
// отслеживаемый файл под правилом игнорирования всё равно принадлежит корпусу,
// а неотслеживаемый без правила — просто мусор, который прогон и оставляет.
//
// Это не список исключений, а факт дерева: исключение истекает само, как только
// объявление снимут.
func shellDeclaredSink(t *testing.T, root string) func(string) bool {
	t.Helper()
	tracked := map[string]bool{}
	for _, rel := range trackedPaths(t, root) {
		tracked[rel] = true
	}
	cache := map[string]bool{}
	return func(rel string) bool {
		if tracked[rel] {
			return false
		}
		if v, ok := cache[rel]; ok {
			return v
		}
		// Спрашивается ДВЕ формы, и вторая обязательна. Правило игнорирования
		// вида `helm/umbrella/tmpcharts-*/` объявляет КАТАЛОГ, и `check-ignore`
		// на пути без косой черты честно отвечает «нет»: он не знает, каталог
		// это или файл. Без второго вопроса объявленная деревом свалка читалась
		// бы находкой — и её пришлось бы гасить списком исключений, то есть
		// заменить факт дерева памятью автора.
		//
		// Форма с косой чертой НЕ размывает предикат: на отслеживаемом файле
		// (`…/values.prod.yaml/`) и на необъявленном каталоге (`helm/umbrella/`)
		// она отвечает «нет» — проверено обеими сторонами в пробе.
		v := gitenv.Command(root, "check-ignore", "-q", "--", rel).Run() == nil ||
			gitenv.Command(root, "check-ignore", "-q", "--", rel+"/").Run() == nil
		cache[rel] = v
		return v
	}
}

// shellProbeSources — исходники проб из состава дерева (индекс git), а не с
// диска: посторонний каталог рядом с репозиторием иначе влиял бы на вердикт.
// Второе возвращаемое — сколько скриптов в дереве всего, третье — сколько попало
// в корпус. Доля корпуса и потери на чтении обязаны быть НАЗВАНЫ, а не
// подразумеваться: без них «ноль находок» неотличимо от «ноль прочитанного».
func shellProbeSources(t *testing.T, root string) (map[string]string, int, int) {
	t.Helper()
	out := map[string]string{}
	all, matched := 0, 0
	osRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("корень %s не открыт: %v — состав проб взять неоткуда, и «ноль находок» "+
			"было бы утверждением ни о чём", root, err)
	}
	defer func() { _ = osRoot.Close() }()
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasSuffix(rel, ".sh") {
			continue
		}
		all++
		if !isShellProbePath(rel) {
			continue
		}
		matched++
		body, ok := readTracked(osRoot, rel)
		if !ok {
			continue
		}
		out[rel] = body
	}
	return out, all, matched
}

// shellFindingsBrief — компактный вид находок для сообщений инъекции.
func shellFindingsBrief(fs []shellLiveWriteFinding) string {
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "\n  %s:%d %s → %s", f.File, f.Line, f.What, f.Target)
	}
	return b.String()
}
