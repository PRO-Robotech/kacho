// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// probewriteslivetree_test.go — проба не пишет в дерево, из которого запущена.
//
// # Предмет
//
// Проба-инъекция обязана доказывать, что гейт умеет краснеть. Дешёвый способ —
// подложить дефект туда, где гейт его увидит, то есть В ЖИВУЮ рабочую копию, и
// убрать в конце: `t.Cleanup` у Go, последняя строка тела у shell. Уборка не
// переживает прерывания — снятие по времени, нехватку памяти, нехватку места,
// `SIGKILL`, — а резервная копия, из которой файл можно было бы вернуть, обычно
// лежит во временном каталоге и исчезает вместе с прогоном.
//
// # Почему это дороже, чем «грязное дерево»
//
// Состав корпуса гейты этого репозитория берут у git-индекса (`pkg/treecorpus`)
// — именно затем, чтобы вердикт был свойством КОММИТА, а не рабочего каталога.
// Фантомная запись в индексе означает, что гейт судит корпус, которого нет ни на
// диске, ни в `HEAD`: «ноль находок» становится неотличимо от «ноль прочитанного»,
// а красное — от настоящей находки. Порча тихая и делает лживыми ровно те
// проверки, ради которых состав и берётся у индекса.
//
// Наблюдалось за одну смену дважды (#696): три записи `ui-future/**` остались
// staged в индексе соседней сессии после снятия прогона, и правка шаблона
// развёртывания пережила снятую shell-суиту — её нашли `git status`-ом в чистой
// до того копии, иначе она уехала бы в коммит.
//
// # Норма
//
// `multi-agent-flow.md` §«НЕПРИКОСНОВЕННОСТЬ ЧУЖОГО СОСТОЯНИЯ»: проба, заводящая
// репозиторий или пишущая в индекс, изолирует его — своё окружение либо свой
// временный каталог. Никакой записи по пути, производному от корня живого
// репозитория.
//
// # Предикат
//
// Корпус — отслеживаемые `*_test.go` ВСЕГО дерева, состав берётся у git-индекса.
// Не-тестовые `.go` в корпус не входят намеренно: инструменты дерева (генератор
// пинов образов, подкачка подчартов) пишут в него ПО СВОЕМУ НАЗНАЧЕНИЮ, и запрет
// был бы запретом на них.
//
// Находка = вызов записи (файловая система либо изменяющая git-команда), чей
// путь/каталог ПРОИСХОДИТ от значения, полученного у производителя живого корня.
//
// Производители НЕ выписаны списком имён, а ВЫВЕДЕНЫ из исходников: функция
// считается производителем живого корня, если её тело либо спрашивает рабочий
// каталог процесса (`os.Getwd`) и восходит до `go.mod`, либо отталкивается от
// файла САМОГО исходника (`runtime.Caller`) и поднимается по каталогам
// (`filepath.Dir`). Список имён разошёлся бы с деревом молча — их в дереве
// четыре десятка и они называются по-разному (`repoRoot`, `moduleRoot`,
// `monorepoRoot`, `repoRootFromTest`, …).
//
// Вторая форма добавлена по рецензии #696: соседний гейт ЭТОГО ЖЕ пакета
// (`treewalkindex_test.go`, `walkRootProducers`) знал обе, а этот — одну, и два
// определения одного предмета расходились молча. Цена расширения измерена, а не
// предположена: производителей стало 41 вместо 20, находок по дереву — 0 и до,
// и после.
//
// Происхождение прослеживается по значению, а не по имени переменной: прямое
// присваивание, `filepath.Join(<живое>, …)`, срез, собранный вокруг живого
// значения, и один уровень передачи в функцию того же пакета (`mustWrite(t,
// root, …)` — самая частая форма помощника).
//
// Разбор идёт по AST: синтетические исходники соседних инъекций лежат в
// строковых константах, и текстовый поиск нашёл бы в них ровно ту форму, ради
// запрета которой они написаны.
//
// # Чего предикат НЕ ловит — названо, а не умолчано
//
// Shell-суиты этот гейт не читает — их держит ОТДЕЛЬНЫЙ гейт того же пакета,
// `TestShellProbesDoNotWriteIntoTheTreeTheyRunFrom`
// (`shellprobewriteslivetree_test.go`, #724): корпус, вокабуляр записи и
// разрешение пути у shell свои, и один предикат на два языка был бы хуже обоих.
// Текстовый предикат по shell проверялся и отвергнут ЧИСЛОМ — из трёх
// экземпляров класса он нашёл бы ОДИН и объявил бы остальные два чистыми; замер
// закреплён пробой `TestShellProbeWriteGateSeesWhatATextualPredicateCannot` на
// настоящих прежних редакциях суит, а не на пересказе.
//
// Ещё четыре границы, купленные точностью сознательно (первое же ложное
// срабатывание отключило бы гейт целиком):
//
//   - помощник, ВОЗВРАЩАЮЩИЙ путь под переданным ему корнем, происхождения не
//     передаёт: результат произвольного вызова живым не считается;
//   - передача живого значения прослеживается на один шаг в функции того же
//     каталога, дальше — нет;
//   - смена рабочего каталога процесса (`t.Chdir`) с последующей записью по
//     ОТНОСИТЕЛЬНОМУ пути: живого значения в такой записи нет вовсе, и вести от
//     него нечего. Живых экземпляров на момент правки нет — `Chdir` встречается
//     в корпусе проб в одном файле (`git grep -l 'Chdir(' -- '*_test.go'`,
//     2 вызова), и он не пишет ничего (тот же предикат по `fsWriters` даёт 0);
//   - подкоманда git, уехавшая в переменную-ЗАМЫКАНИЕ (а не в срез, собранный
//     в той же функции): срез разбирается, замыкание — нет.
//
// Последние две названы рецензией #696 и НЕ закрыты: закрывать форму, у которой
// нет ни одного живого экземпляра, значит писать ветку, которую нечем проверить
// на дереве. Обе стоят здесь именно затем, чтобы «предикат ловит запись» не
// читалось шире, чем есть.
package repohygiene

import (
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

// liveWriteFinding — место, где живой корень доезжает до записи.
type liveWriteFinding struct {
	File string // путь относительно корня дерева
	Line int
	Func string
	What string // что именно записывает
	Why  string
}

// fsWriters — вызовы стандартной библиотеки, изменяющие файловую систему.
// Первый аргумент у всех — путь.
var fsWriters = map[string]bool{
	"WriteFile": true, "Create": true, "Remove": true, "RemoveAll": true,
	"MkdirAll": true, "Mkdir": true, "Rename": true, "Symlink": true,
	"OpenFile": true, "Chmod": true, "Truncate": true, "Link": true,
	"Chown": true, "Chtimes": true,
}

// gitMutators — подкоманды git, меняющие состояние репозитория (индекс, ссылки,
// настройки, рабочее дерево). Чтения (`ls-files`, `rev-parse`, `cat-file`,
// `status`, `diff`, `log`, `show`) сюда НЕ входят намеренно: гейты этого дерева
// читают живой репозиторий постоянно, и запрет на чтение был бы запретом на них.
var gitMutators = map[string]bool{
	"add": true, "rm": true, "mv": true, "commit": true, "commit-tree": true,
	"checkout": true, "switch": true, "restore": true, "reset": true,
	"stash": true, "clean": true, "update-index": true, "update-ref": true,
	"config": true, "init": true, "apply": true, "worktree": true,
	"branch": true, "tag": true, "merge": true, "rebase": true, "am": true,
	"cherry-pick": true, "gc": true, "prune": true, "sparse-checkout": true,
	"notes": true, "symbolic-ref": true, "push": true, "fetch": true,
	"remote": true, "hash-object": true, "write-tree": true, "filter-branch": true,
}

// liveWriteCensus — объём осмотренного. «Ноль находок» обязано быть отличимо от
// «ноль прочитанного», поэтому счётчики печатаются всегда.
type liveWriteCensus struct {
	Files     int // файлов проб разобрано
	Funcs     int // функций осмотрено
	Producers int // функций, производящих живой корень
	Writes    int // мест записи осмотрено (файловая система + изменяющий git)
	Tainted   int // из них с путём, производным от живого корня
}

// auditProbeWritesToLiveTree — весь предикат. Вход — исходники проб (путь → текст),
// чтобы инъекция гоняла ТУ ЖЕ функцию, что и гейт по дереву.
func auditProbeWritesToLiveTree(sources map[string]string) ([]liveWriteFinding, liveWriteCensus) {
	var (
		findings []liveWriteFinding
		census   liveWriteCensus
	)
	fset := token.NewFileSet()

	// Разбор один раз на файл: производители выводятся из тех же деревьев,
	// по которым потом ищутся находки.
	type parsed struct {
		rel  string
		file *ast.File
	}
	byDir := map[string][]parsed{}
	rels := make([]string, 0, len(sources))
	for rel := range sources {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		f, err := parser.ParseFile(fset, rel, sources[rel], parser.SkipObjectResolution)
		if err != nil {
			// Неразбираемый файл — не «ноль находок», а непрочитанный файл.
			// Он не идёт ни в числитель, ни молча в знаменатель.
			continue
		}
		census.Files++
		dir := filepath.Dir(rel)
		byDir[dir] = append(byDir[dir], parsed{rel: rel, file: f})
	}

	for _, files := range byDir {
		producers := map[string]bool{}
		for _, p := range files {
			for _, d := range p.file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if isLiveRootProducer(fn) {
					producers[fn.Name.Name] = true
				}
			}
		}
		census.Producers += len(producers)

		// Параметры, через которые живой корень уезжает в помощника. Считается
		// до фиксированной точки: помощник может звать помощника.
		liveParams := map[string]map[int]bool{}
		for range 3 {
			changed := false
			for _, p := range files {
				for _, d := range p.file.Decls {
					fn, ok := d.(*ast.FuncDecl)
					if !ok || fn.Body == nil {
						continue
					}
					sc := newLiveScope(producers, liveParams, fn)
					for callee, idx := range sc.escapes() {
						if liveParams[callee] == nil {
							liveParams[callee] = map[int]bool{}
						}
						for i := range idx {
							if !liveParams[callee][i] {
								liveParams[callee][i] = true
								changed = true
							}
						}
					}
				}
			}
			if !changed {
				break
			}
		}

		for _, p := range files {
			for _, d := range p.file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				census.Funcs++
				sc := newLiveScope(producers, liveParams, fn)
				for _, w := range sc.writes {
					census.Writes++
					if !w.live {
						continue
					}
					census.Tainted++
					findings = append(findings, liveWriteFinding{
						File: p.rel,
						Line: fset.Position(w.pos).Line,
						Func: fn.Name.Name,
						What: w.what,
						Why:  w.why,
					})
				}
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, census
}

// isLiveRootProducer — функция возвращает корень ЖИВОГО репозитория. Признак
// выведен из того, как эти помощники написаны в дереве, а не из их имён: имён у
// них два десятка и они разные.
//
// Форм ДВЕ, и обе настоящие:
//
//	os.Getwd + маркер "go.mod"   — оттолкнуться от рабочего каталога процесса;
//	runtime.Caller + filepath.Dir — оттолкнуться от файла САМОГО исходника.
//
// Вторая маркера не ищет и ничего не статит, поэтому пара «спросил каталог И
// назвал go.mod» её не узнаёт вовсе. Соседний гейт ЭТОГО ЖЕ пакета
// (`treewalkindex_test.go`, `walkRootProducers`) знает обе — и расхождение двух
// определений одного предмета означало бы, что происхождение прослеживается у
// одного гейта и молча теряется у другого. Добавлено по рецензии #696.
//
// Живых экземпляров второй формы среди ПИШУЩИХ проб на момент правки нет
// (перепись гейта до и после расширения: находок 0 в обоих случаях, см.
// сообщение коммита) — это долговечность признака, а не закрытая находка.
// Обе половины условия обязательны: одно лишь восхождение по каталогам
// (`filepath.Dir`) производителем НЕ делает, иначе им стал бы любой помощник,
// собирающий пути, и гейт залило бы ложными находками.
func isLiveRootProducer(fn *ast.FuncDecl) bool {
	var getwd, gomod, caller, climbs bool
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok {
					switch id.Name + "." + sel.Sel.Name {
					case "os.Getwd":
						getwd = true
					case "runtime.Caller":
						caller = true
					case "filepath.Dir":
						climbs = true
					}
				}
			}
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil && s == "go.mod" {
					gomod = true
				}
			}
		}
		return true
	})
	return (getwd && gomod) || (caller && climbs)
}

type liveWrite struct {
	pos       token.Pos
	what, why string
	live      bool
}

// liveScope — прослеживание живого корня внутри одной функции.
type liveScope struct {
	producers  map[string]bool
	liveParams map[string]map[int]bool
	live       map[string]bool     // переменные, значение которых происходит от живого корня
	slices     map[string][]string // строковые литералы, из которых собран срез
	writes     []liveWrite
	out        map[string]map[int]bool // куда живое значение уехало (callee → индексы аргументов)
}

func newLiveScope(producers map[string]bool, liveParams map[string]map[int]bool, fn *ast.FuncDecl) *liveScope {
	sc := &liveScope{
		producers:  producers,
		liveParams: liveParams,
		live:       map[string]bool{},
		slices:     map[string][]string{},
		out:        map[string]map[int]bool{},
	}
	// Параметры, признанные живыми по вызывающим.
	if idx := liveParams[fn.Name.Name]; len(idx) > 0 {
		pos := 0
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if idx[pos] {
					sc.live[name.Name] = true
				}
				pos++
			}
		}
	}
	sc.walk(fn.Body)
	return sc
}

func (s *liveScope) escapes() map[string]map[int]bool { return s.out }

func (s *liveScope) walk(body *ast.BlockStmt) {
	// Обход в порядке исходника: присваивание живого корня стоит выше записи,
	// а присваивание временного каталога — выше своей.
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			s.assign(v)
		case *ast.CallExpr:
			s.call(v)
		}
		return true
	})
}

func (s *liveScope) assign(a *ast.AssignStmt) {
	for i, lhs := range a.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || i >= len(a.Rhs) {
			continue
		}
		rhs := a.Rhs[i]
		if lits := exprStringLiterals(rhs); len(lits) > 0 {
			s.slices[id.Name] = lits
		}
		// Присваивание живого делает переменную живой; присваивание любого
		// другого значения — снимает метку (временный каталог именно так и
		// «перебивает» одноимённую переменную).
		s.live[id.Name] = s.isLive(rhs)
	}
}

// isLive — происходит ли ЗНАЧЕНИЕ выражения от живого корня.
//
// Разбор идёт по форме выражения, а не поиском живого имени где угодно внутри.
// Разница не косметическая: `make([]string, len(живое))` живого значения не
// содержит, и наивный обход объявлял бы живыми временные каталоги, розданные
// по числу элементов живого среза. Ровно эта ложная находка и получилась на
// первой редакции — гейт с ложным срабатыванием снимают первым.
func (s *liveScope) isLive(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return s.live[v.Name]
	case *ast.ParenExpr:
		return s.isLive(v.X)
	case *ast.StarExpr:
		return s.isLive(v.X)
	case *ast.IndexExpr:
		return s.isLive(v.X)
	case *ast.SliceExpr:
		return s.isLive(v.X)
	case *ast.SelectorExpr:
		return s.isLive(v.X)
	case *ast.BinaryExpr:
		// Склейка пути строками — та же производность.
		return s.isLive(v.X) || s.isLive(v.Y)
	case *ast.CompositeLit:
		for _, el := range v.Elts {
			if s.isLive(el) {
				return true
			}
		}
		return false
	case *ast.CallExpr:
		if id, ok := v.Fun.(*ast.Ident); ok && s.producers[id.Name] {
			return true
		}
		if !preservesOrigin(v) {
			// Результат произвольного вызова происхождения аргументов НЕ
			// наследует. Цена названа: помощник, возвращающий путь ПОД
			// переданным ему корнем, этим гейтом не прослеживается — точность
			// куплена полнотой сознательно, потому что первое же ложное
			// срабатывание отключило бы гейт целиком.
			//
			// Здесь же обрывается и временный каталог: `t.TempDir()` и
			// `os.MkdirTemp` — обычные вызовы, происхождения они не несут, и
			// отдельной ветки под них не нужно. Такая ветка тут стояла и по
			// рецензии #696 снята как ничего не решающая: отключение её целиком
			// не меняло ни одного вердикта. Свойство держат близнецы инъекции
			// (чтение живого дерева, копия дерева, СВОЁ дерево под git), а не
			// перечисление имён временных каталогов, которое пришлось бы
			// пополнять на каждый новый способ их завести.
			return false
		}
		for _, a := range v.Args {
			if s.isLive(a) {
				return true
			}
		}
		return false
	}
	return false
}

// preservesOrigin — вызовы, которые ВОЗВРАЩАЮТ то же значение в другой форме:
// сборка пути и наращивание среза аргументов. Всё остальное происхождения не
// передаёт.
func preservesOrigin(c *ast.CallExpr) bool {
	if id, ok := c.Fun.(*ast.Ident); ok {
		return id.Name == "append"
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "filepath" {
		return false
	}
	switch sel.Sel.Name {
	case "Join", "Clean", "Abs", "Dir", "ToSlash", "FromSlash", "EvalSymlinks":
		return true
	}
	// `filepath.Rel` и `filepath.Base` происхождение СНИМАЮТ: их результат —
	// относительный отрезок, корня в нём больше нет. Первая редакция считала
	// `Rel` сохраняющим, и копия дерева, собираемая как «относительный путь от
	// живого корня → положить под временный», объявлялась записью в живое
	// дерево. Это ровно тот ложный срабат, после которого гейт снимают.
	return false
}

func (s *liveScope) call(c *ast.CallExpr) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if ok {
		pkg, _ := sel.X.(*ast.Ident)
		switch {
		case pkg != nil && pkg.Name == "os" && fsWriters[sel.Sel.Name] && len(c.Args) > 0:
			s.writes = append(s.writes, liveWrite{
				pos:  c.Lparen,
				what: "os." + sel.Sel.Name,
				why: "запись по пути, производному от корня ЖИВОГО репозитория: прерывание " +
					"прогона оставит её в рабочей копии, а резервной копии, из которой её " +
					"вернуть, у прерванного прогона нет",
				live: s.isLive(c.Args[0]),
			})
		case pkg != nil && pkg.Name == "gitenv" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext"):
			s.gitCall(c, sel.Sel.Name == "CommandContext")
		case pkg != nil && pkg.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext"):
			s.execGitCall(c)
		}
	}
	// Передача живого значения в функцию того же пакета — один шаг наружу.
	if id, ok := c.Fun.(*ast.Ident); ok {
		for i, arg := range c.Args {
			if s.isLive(arg) {
				if s.out[id.Name] == nil {
					s.out[id.Name] = map[int]bool{}
				}
				s.out[id.Name][i] = true
			}
		}
	}
}

// gitCall — вызов git через общий пакет: первый аргумент рабочий каталог (у
// формы со сроком — второй), дальше аргументы командной строки.
func (s *liveScope) gitCall(c *ast.CallExpr, withCtx bool) {
	skip := 1
	if withCtx {
		skip = 2
	}
	if len(c.Args) < skip {
		return
	}
	dir := c.Args[skip-1]
	args := c.Args[skip:]
	sub, hasSub := s.subcommand(args)
	if !hasSub {
		return
	}
	live := s.isLive(dir)
	for _, a := range args {
		if s.isLive(a) {
			live = true
		}
	}
	s.writes = append(s.writes, liveWrite{
		pos:  c.Lparen,
		what: "git " + sub,
		why: "изменяющая git-команда против ЖИВОГО репозитория: прерывание прогона до " +
			"уборки оставляет записи в индексе, а состав корпуса гейты берут именно у него",
		live: live,
	})
}

// execGitCall — прямой запуск git в обход общего пакета. Такую форму отдельно
// запрещает соседний гейт; здесь она рассматривается ради полноты предиката.
func (s *liveScope) execGitCall(c *ast.CallExpr) {
	if len(c.Args) == 0 {
		return
	}
	first := c.Args[0]
	if lit, ok := first.(*ast.BasicLit); !ok || lit.Kind != token.STRING {
		return
	} else if v, err := strconv.Unquote(lit.Value); err != nil || v != "git" {
		return
	}
	args := c.Args[1:]
	sub, hasSub := s.subcommand(args)
	if !hasSub {
		return
	}
	live := false
	for _, a := range args {
		if s.isLive(a) {
			live = true
		}
	}
	s.writes = append(s.writes, liveWrite{
		pos:  c.Lparen,
		what: "git " + sub,
		why:  "изменяющая git-команда против ЖИВОГО репозитория",
		live: live,
	})
}

// subcommand — изменяющая подкоманда среди аргументов вызова либо среди
// литералов среза, собранного строкой выше (`args := []string{"-C", root, "add"}`).
func (s *liveScope) subcommand(args []ast.Expr) (string, bool) {
	for _, a := range args {
		for _, lit := range exprStringLiterals(a) {
			if gitMutators[lit] {
				return lit, true
			}
		}
		if id, ok := a.(*ast.Ident); ok {
			for _, lit := range s.slices[id.Name] {
				if gitMutators[lit] {
					return lit, true
				}
			}
		}
	}
	return "", false
}

// exprStringLiterals — все строковые литералы ВЫРАЖЕНИЯ (сам литерал, элементы
// составного литерала, аргументы `append`).
//
// Имя несёт «expr» не для красоты: рядом в пакете живёт `stringLiterals`, берущая
// литералы ФАЙЛА (гейт тона отказа, задача #718). Обе появились в один день в
// разных ветках, поэтому столкнулись только при сборке релиза — git конфликта не
// видел, файлы-то разные. Различай по тому, ЧТО разбирается: выражение или файл.
func exprStringLiterals(e ast.Expr) []string {
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if v, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, v)
			}
		}
		return true
	})
	return out
}

// TestProbesDoNotWriteIntoTheTreeTheyRunFrom — гейт по дереву.
func TestProbesDoNotWriteIntoTheTreeTheyRunFrom(t *testing.T) {
	root := repoRoot(t)
	sources := probeSources(t, root)

	if len(sources) == 0 {
		t.Fatal("обход не нашёл ни одной пробы — гейт беспредметен: либо состав дерева " +
			"взять не удалось, либо расширение проб изменилось. В обоих случаях зелёный " +
			"вердикт ниже был бы получен даром.")
	}

	findings, census := auditProbeWritesToLiveTree(sources)

	// Предпосылки предиката. Без производителя живого корня прослеживать нечего,
	// без единого места записи — не о чем судить: молчание тогда означало бы
	// поломку разбора, а не чистоту дерева.
	if census.Producers == 0 {
		t.Error("в дереве не найдено ни одного производителя живого корня — источник, " +
			"от которого предикат ведёт происхождение, исчез, и «ноль находок» ниже " +
			"неотличимо от «ноль прочитанного»")
	}
	if census.Writes == 0 {
		t.Error("в дереве не найдено ни одного места записи — распознавание записи сломано")
	}

	for _, f := range findings {
		t.Errorf("проба %s:%d (%s) пишет через %s:\n  %s\n\n"+
			"Исход один: изолировать — свой временный каталог (`t.TempDir()`), свой "+
			"репозиторий (`git init` в нём) либо копия дерева. Уборка в `t.Cleanup` "+
			"исходом НЕ является: прерывание до неё не доходит.",
			f.File, f.Line, f.Func, f.What, f.Why)
	}

	t.Logf("перепись: файлов проб разобрано %d, функций осмотрено %d, производителей "+
		"живого корня %d, мест записи осмотрено %d, из них по живому корню %d",
		census.Files, census.Funcs, census.Producers, census.Writes, census.Tainted)
}

// probeSources — исходники проб из состава дерева (индекс git), а не с диска:
// посторонний каталог рядом с репозиторием иначе влиял бы на вердикт.
func probeSources(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	osRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("корень %s не открыт: %v — состав проб взять неоткуда, и «ноль находок» "+
			"было бы утверждением ни о чём", root, err)
	}
	defer func() { _ = osRoot.Close() }()
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		body, ok := readTracked(osRoot, rel)
		if !ok {
			continue
		}
		out[rel] = body
	}
	return out
}
