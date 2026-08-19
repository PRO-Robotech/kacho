// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Перепись усыновления возможностей общего фундамента, вынесенная из гейта,
// чтобы проба инъекции могла подать сюда синтетическое дерево и доказать, что
// перепись умеет и краснеть, и молчать.
//
// # Почему усыновление НЕЛЬЗЯ мерить прямым упоминанием в каталоге слушателя
//
// Шесть слушателей из восьми собирают свои серверы через общий носитель
// (`pkg/servicehost`), а он ставит часть звеньев сам. Предикат «упомянул ли
// слушатель имя возможности» отвечает на них «нет» — и это ложь, а не находка:
// возможность доезжает ЧЕРЕЗ посредника. Такой предикат уже своего предмета, и
// его находки были бы ложными ровно там, где усыновление сделано правильно.
//
// Поэтому усыновление считается ДОСТИЖИМОСТЬЮ: возможность доезжает до
// слушателя либо его собственной проводкой, либо через посредника, которого он
// зовёт, — транзитивно, с проверкой того, что посредник и правда её несёт.

// FoundationUnit — единица счёта возможности: чему принадлежит одна клетка.
type FoundationUnit string

// Единиц ровно две, и обе объявляются ЯВНО: умолчание сделало бы выбор за
// автора набора молча, а выбран он был бы тем, что удобнее гейту.
const (
	// FoundationUnitListener — клетка на КАЖДОЕ место сборки сервера.
	FoundationUnitListener FoundationUnit = "слушатель"
	// FoundationUnitProcess — клетка на каталог: возможность заводится раз на
	// процесс и мест сборки не имеет.
	FoundationUnitProcess FoundationUnit = "процесс"
)

// FoundationCapability — возможность фундамента, объявленная обязательной для
// слушателей платформы.
//
// Признаков усыновления два, и они РАЗНЫЕ по существу. Возможность-пакет
// (сужатель списка) видна импортом: незачем перечислять её символы, их много и
// перечень устареет. Возможность-функция большого пакета (допуск, пределы,
// восстановление паники живут в `pkg/grpcsrv` рядом с кругом отправителей,
// который импортируют все) импортом НЕ видна — её видно только по вызванному
// символу. Мерить обе одним признаком значило бы либо считать усыновлённым
// каждого, кто импортировал пакет за другим, либо выписывать символы там, где
// они не нужны.
type FoundationCapability struct {
	// Name — имя возможности в переписи и в ведомости.
	Name string
	// Unit — ЕДИНИЦА СЧЁТА: чему принадлежит возможность.
	//
	// Различие не стилистическое, и первая редакция гейта на нём и ошиблась.
	// Возможность, которую провязывают В СЕРВЕР (звено цепочки, пределы
	// транспорта, ограничитель допуска), принадлежит МЕСТУ СБОРКИ: их в каталоге
	// бывает несколько, и снятие у одного из них — ровно тот дефект, который
	// счёт по каталогу не видит, потому что второй сервер отвечает за оба.
	// Возможность, которую заводят РАЗ НА ПРОЦЕСС (сужатель списочной выдачи,
	// трассировка), принадлежит КАТАЛОГУ: мест сборки у неё нет, и требовать её
	// у каждого значило бы задавать вопрос, у которого нет предмета.
	Unit FoundationUnit
	// Pkg — координата в дереве (`pkg/<имя>`). Гейт проверяет, что она
	// существует: возможность, объявленная обязательной и отсутствующая в
	// фундаменте, — находка, а не пустая клетка у всех слушателей.
	Pkg string
	// ImportPath — путь импорта, если возможность есть пакет целиком.
	ImportPath string
	// Symbols — селекторы вида "grpcsrv.NewAdmission", если возможность есть
	// одна функция пакета.
	Symbols []string
}

// FoundationProvider — посредник, несущий возможности за своего вызывающего.
//
// Объявляется ПОИМЁННО, а не выводится из того, что лежит в его каталоге:
// «пакет содержит возможность» и «эта точка входа её ставит» — разные вещи, и
// первое приняло бы за носителя всякий пакет, где возможность просто живёт.
type FoundationProvider struct {
	// Name — каталог посредника относительно корня репозитория.
	Name string
	// Entry — селектор точки входа, по вызову которой слушатель считается
	// пользующимся посредником.
	Entry string
	// Carries — имена возможностей, которые посредник ставит за вызывающего.
	// Заявление проверяется по дереву (см. VerifyProviderClaims): посредник,
	// объявленный носителем и не упоминающий возможность вовсе, — находка.
	Carries []string
}

// FoundationLedgerEntry — записанный пропуск усыновления.
//
// Это НЕ прощение и не «список известного красного»: запись обязана называть
// задачу, и её единственное назначение — сделать долг СЧЁТНЫМ. Ведомость
// самоистекает: запись, которой больше нечего исключать, роняет гейт.
type FoundationLedgerEntry struct {
	// Capability — имя возможности.
	Capability string
	// Listener — имя слушателя. Пустая строка означает «не несёт НИ ОДИН»:
	// восемь одинаковых записей вместо одной сделали бы ведомость нечитаемой,
	// а истекать они обязаны вместе — на первом же усыновившем.
	Listener string
	// Issue — номер задачи. Ноль незаконен: пропуск без задачи неотличим от
	// забытого.
	Issue int
	// Why — чем пропуск обоснован, коротко.
	Why string
}

// FoundationNoSubject — записанное отсутствие ПРЕДМЕТА у слушателя.
//
// Отличается от пропуска тем, что работы по нему не предвидится: возможности
// нечего делать в этом процессе. Задача такой записи не нужна — нужна причина.
// Самоистекает в обратную сторону: слушатель, который возможность УСЫНОВИЛ,
// опровергает запись о том, что предмета у него нет.
type FoundationNoSubject struct {
	Capability string
	Listener   string
	Why        string
}

// FoundationScan — то, что видно в исходниках каталога.
//
// Читается разбором синтаксиса, а не текстом: имя возможности стоит в
// объяснениях у самих звеньев (например, шапка помощника края называет
// восстановление паники прозой), и текстовый предикат засчитал бы комментарий
// за проводку. Гейт, зеленеющий на собственном объяснении, — записанный класс
// (`testing.md` §«Гейт на класс», п.4).
type FoundationScan struct {
	Imports map[string]bool
	Selects map[string]bool
	// Calls — имена, вызванные БЕЗ квалификатора пакета. Нужны ровно для одного:
	// внутри своего пакета возможность зовётся `DefaultServerLimits()`, а не
	// `grpcsrv.DefaultServerLimits()`, поэтому селекторный предикат на СВОЁМ
	// пакете отвечает «нет» — он уже своего предмета. Именно так первая редакция
	// этого гейта и объявила конструктор сервера не носителем пределов, будучи
	// им.
	//
	// Только позиция вызова: объявление (`func NewAdmission(...)`) вызовом не
	// является, поэтому пакет НЕ засчитывается носителем всего, что в нём просто
	// живёт. Это и есть разница между «возможность здесь объявлена» и «эта точка
	// входа её ставит».
	Calls map[string]bool
	Files int
}

// ScanGoTree разбирает не-тестовые исходники Go под dir и собирает импорты и
// селекторы вида `pkg.Sym`.
//
// Файл, который не разбирается, — ОТКАЗ, а не пропуск: пропущенный файл делает
// «ноль находок» неотличимым от «ноль прочитанного» ровно там, где проводка и
// могла бы спрятаться.
func ScanGoTree(dir string) (*FoundationScan, error) {
	s := &FoundationScan{Imports: map[string]bool{}, Selects: map[string]bool{}, Calls: map[string]bool{}}
	fset := token.NewFileSet()
	err := rootedWalk(dir,
		func(rel string) bool {
			return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
		},
		func(abs string, body []byte) error {
			return absorbGoFile(fset, abs, body, s)
		})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// scanGoFile — тот же разбор для ОДНОГО файла.
//
// Нужен пробе, которая ищет производителя входа по дереву: «упоминает текстом,
// но не вызывает» — вопрос о файле, а не о каталоге, и заданный каталогу он
// сливает упоминание одного файла с вызовом соседнего.
func scanGoFile(abs string, body []byte) (*FoundationScan, error) {
	s := &FoundationScan{Imports: map[string]bool{}, Selects: map[string]bool{}, Calls: map[string]bool{}}
	if err := absorbGoFile(token.NewFileSet(), abs, body, s); err != nil {
		return nil, err
	}
	return s, nil
}

// absorbGoFile — единственное место, где исходник превращается в признаки.
//
// Один разбор на обе стороны (каталог и файл): две копии разошлись бы молча, и
// разошлись бы они именно там, где проба доказывает, что комментарий провязкой
// не считается, — то есть контроль перестал бы контролировать.
func absorbGoFile(fset *token.FileSet, abs string, body []byte, s *FoundationScan) error {
	// ParseComments не запрашиваем намеренно: комментарии в разбор не попадают
	// вовсе, поэтому «упомянул в объяснении» физически не может стать «провязал».
	f, perr := parser.ParseFile(fset, abs, body, parser.SkipObjectResolution)
	if perr != nil {
		return fmt.Errorf("%s: разбор не удался: %w", abs, perr)
	}
	s.Files++
	for _, imp := range f.Imports {
		p, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil {
			return fmt.Errorf("%s: путь импорта %s не читается: %w", abs, imp.Path.Value, uerr)
		}
		s.Imports[p] = true
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := v.X.(*ast.Ident); ok {
				s.Selects[id.Name+"."+v.Sel.Name] = true
			}
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok {
				s.Calls[id.Name] = true
			}
		}
		return true
	})
	return nil
}

// Direct — видна ли возможность в этом каталоге СВОЕЙ проводкой, без посредника.
//
// Признак квалифицированный (импорт либо `пакет.Символ`) и применяется к
// каталогам ВНЕ пакета возможности — то есть ко всем слушателям.
func (s *FoundationScan) Direct(c FoundationCapability) bool {
	if c.ImportPath != "" && s.Imports[c.ImportPath] {
		return true
	}
	for _, sym := range c.Symbols {
		if s.Selects[sym] {
			return true
		}
	}
	return false
}

// DirectInOwnPackage — тот же вопрос, заданный ВНУТРИ пакета возможности, где
// квалификатора нет.
//
// Применяется ТОЛЬКО при сверке заявления посредника, чей каталог совпадает с
// каталогом возможности, и НИКОГДА при счёте достижимости: там неквалифицированное
// имя засчитало бы носителем всякий пакет, где возможность просто объявлена.
func (s *FoundationScan) DirectInOwnPackage(c FoundationCapability) bool {
	if s.Direct(c) {
		return true
	}
	for _, sym := range c.Symbols {
		if i := strings.LastIndex(sym, "."); i >= 0 {
			sym = sym[i+1:]
		}
		if s.Calls[sym] {
			return true
		}
	}
	return false
}

// FoundationRoster — объявленный набор: возможности, посредники и обе ведомости.
type FoundationRoster struct {
	Capabilities []FoundationCapability
	Providers    []FoundationProvider
	// Wrappers — каталоги, оборачивающие сборку сервера. Из них ВЫВОДИТСЯ
	// перечень точек входа, и по ним же исключаются внутренности обёрток.
	Wrappers  []FoundationWrapper
	Ledger    []FoundationLedgerEntry
	NoSubject []FoundationNoSubject
}

// Reach — какие возможности доезжают до каталога с этим разбором.
//
// Транзитивно: слушатель, зовущий носитель, получает и то, что носитель ставит
// сам, и то, что за носителем ставит конструктор сервера. Цепочку гейт проходит
// сам, а не принимает заявлением, — иначе «носитель несёт пределы» было бы
// рукописным утверждением, которое разойдётся с деревом молча.
func (r FoundationRoster) Reach(s *FoundationScan, providerScans map[string]*FoundationScan) map[string]bool {
	out := map[string]bool{}
	var walk func(*FoundationScan, map[string]bool)
	walk = func(cur *FoundationScan, seen map[string]bool) {
		for _, c := range r.Capabilities {
			if cur.Direct(c) {
				out[c.Name] = true
			}
		}
		for _, p := range r.Providers {
			if !cur.Selects[p.Entry] || seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			for _, name := range p.Carries {
				out[name] = true
			}
			if ps := providerScans[p.Name]; ps != nil {
				walk(ps, seen)
			}
		}
	}
	walk(s, map[string]bool{})
	return out
}

// VerifyProviderClaims — предпосылка гейта: посредник, объявленный носителем,
// обязан возможность УПОМИНАТЬ в своих исходниках.
//
// Проверяется ПРЯМОЕ упоминание, а не достижимость: посредник, обосновывающий
// своё заявление собственным же заявлением, доказывал бы его сам себе.
//
// Что эта проверка НЕ держит и это названо, а не умолчано: она ловит «объявлен
// носителем и не упоминает вовсе», но не «упоминает, а на этом пути не ставит».
// Второе — свойство поведения, и держат его пробы самого посредника на проводе
// (`pkg/grpcsrv/panicrecovery_chain_test.go`, `pkg/grpcsrv/limits_behaviour_test.go`,
// `pkg/servicehost/wiring_test.go`), а не обход дерева.
func (r FoundationRoster) VerifyProviderClaims(providerScans map[string]*FoundationScan) []string {
	byName := map[string]FoundationCapability{}
	for _, c := range r.Capabilities {
		byName[c.Name] = c
	}
	var bad []string
	for _, p := range r.Providers {
		s := providerScans[p.Name]
		if s == nil {
			bad = append(bad, fmt.Sprintf("посредник %q объявлен, а его каталога в дереве нет", p.Name))
			continue
		}
		if s.Files == 0 {
			bad = append(bad, fmt.Sprintf("посредник %q объявлен, а прод-исходников в его каталоге ноль", p.Name))
			continue
		}
		for _, name := range p.Carries {
			c, ok := byName[name]
			if !ok {
				bad = append(bad, fmt.Sprintf("посредник %q объявлен носителем %q — такой возможности в наборе нет", p.Name, name))
				continue
			}
			carries := s.Direct(c)
			if p.Name == c.Pkg {
				carries = s.DirectInOwnPackage(c)
			}
			if !carries {
				bad = append(bad, fmt.Sprintf("посредник %q объявлен носителем %q, а в его исходниках она не ставится: "+
					"заявление пережило свой предмет, и слушатели, зовущие его, считаются усыновившими ложно", p.Name, name))
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// VerifyCapabilities — предпосылка нулевого уровня: возможность, объявленная
// обязательной, обязана СУЩЕСТВОВАТЬ в фундаменте и иметь хоть один признак
// усыновления.
//
// Без первой проверки набор требовал бы усыновить несуществующее и собирал бы
// вокруг пустоты изъятия, выглядящие как учтённый долг. Без второй возможность
// была бы не усыновлена никем и никогда — не потому, что её не провязали, а
// потому что провязку нечем увидеть.
func (r FoundationRoster) VerifyCapabilities(root string) []string {
	var bad []string
	for _, c := range r.Capabilities {
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(c.Pkg))); err != nil || !fi.IsDir() {
			bad = append(bad, fmt.Sprintf("возможность %q объявлена обязательной, а её каталога %s "+
				"в фундаменте нет: набор требует усыновить то, чего не существует", c.Name, c.Pkg))
		}
		if c.ImportPath == "" && len(c.Symbols) == 0 {
			bad = append(bad, fmt.Sprintf("у возможности %q нет ни одного признака усыновления: "+
				"она была бы не усыновлена никем и никогда, а перепись — вечно красной", c.Name))
		}
		switch c.Unit {
		case FoundationUnitListener:
			// Импорт — свойство ФАЙЛА, а срез провязки берётся у ВЫЗОВА. Признак
			// «пакет где-то в каталоге импортирован» до места сборки не доезжает
			// вовсе, поэтому возможность, объявленная персональной для слушателя
			// и опознаваемая только импортом, не была бы усыновлена ни одним
			// местом и никогда — перепись стала бы вечно красной, а причина
			// лежала бы не в дереве, а в наборе.
			if len(c.Symbols) == 0 {
				bad = append(bad, fmt.Sprintf("возможность %q считается по месту сборки сервера, "+
					"а опознаётся только импортом: импорт — свойство файла, до места сборки он не "+
					"доезжает, и ни одно место её не усыновит никогда", c.Name))
			}
		case FoundationUnitProcess:
		default:
			bad = append(bad, fmt.Sprintf("у возможности %q не объявлена единица счёта (%q): "+
				"умолчание выбрало бы её за автора набора — и выбрало бы ту, что удобнее гейту, "+
				"а не ту, которой возможность принадлежит", c.Name, string(c.Unit)))
		}
	}
	sort.Strings(bad)
	return bad
}

// VerifyWrappers — предпосылка объявления обёрток.
//
// Объявление обёртки — ПОСЛАБЛЕНИЕ: оно выводит вызовы конструктора внутри её
// каталога из числа мест сборки. Послабление обязано истекать само, поэтому
// проверяется двумя вопросами, и оба — о дереве, а не о намерении:
//
//  1. обёртка и правда оборачивает сборку сервера — в её каталоге есть вызов
//     хоть одной точки входа. Иначе исключать в ней нечего, и объявление
//     превращается в способ вывести из переписи произвольный каталог;
//  2. обёртку и правда зовут СНАРУЖИ. У обёртки, которую никто не зовёт,
//     послабление лишено предмета: её внутренности исключены, а взамен не
//     посчитано ничего.
func (r FoundationRoster) VerifyWrappers(markers []string,
	listenerScans, wrapperScans map[string]*FoundationScan) []string {

	var bad []string
	for _, w := range r.Wrappers {
		s := wrapperScans[w.Dir]
		switch {
		case s == nil:
			bad = append(bad, fmt.Sprintf("обёртка %q объявлена, а её каталога в дереве нет", w.Dir))
			continue
		case s.Files == 0:
			bad = append(bad, fmt.Sprintf("обёртка %q объявлена, а прод-исходников в её каталоге ноль", w.Dir))
			continue
		}
		wraps := false
		for _, m := range markers {
			if m != w.Entry && s.Selects[m] {
				wraps = true
				break
			}
		}
		if !wraps {
			bad = append(bad, fmt.Sprintf("обёртка %q объявлена, а сборки сервера в её каталоге нет: "+
				"исключать в ней нечего, и объявление работает как способ вывести каталог из переписи", w.Dir))
		}
		called := false
		for name, sc := range listenerScans {
			if sc != nil && sc.Selects[w.Entry] && !wrapperOwns(name, r.Wrappers) {
				called = true
				break
			}
		}
		if !called {
			for dir, sc := range wrapperScans {
				if dir != w.Dir && sc != nil && sc.Selects[w.Entry] {
					called = true
					break
				}
			}
		}
		if !called {
			bad = append(bad, fmt.Sprintf("обёртку %q (%s) не зовёт никто: послабление, выводящее её "+
				"внутренности из числа мест сборки, лишилось предмета", w.Dir, w.Entry))
		}
	}
	sort.Strings(bad)
	return bad
}

// FoundationCell — вердикт по одной клетке (единица счёта × возможность).
type FoundationCell struct {
	// Listener — ИМЯ ЕДИНИЦЫ: место сборки сервера либо каталог, смотря чему
	// возможность принадлежит.
	Listener   string
	Capability string
	Verdict    string // "несёт" | "нет предмета" | "записанный пропуск" | "НАХОДКА"
	Detail     string
}

// Исходы клетки. Их ровно четыре, и четвёртый роняет прогон.
const (
	FoundationCarried = "несёт"
	FoundationNoSubj  = "нет предмета"
	FoundationExcused = "записанный пропуск"
	FoundationFinding = "НАХОДКА"
)

// FoundationCensus — объём осмотренного и итог по клеткам.
type FoundationCensus struct {
	// Listeners — каталоги-слушатели, Sites — места сборки сервера в них.
	// Печатаются ОБА: их расхождение (8 против 10) и есть то, ради чего единица
	// счёта переделана, и оно обязано быть видно в выводе, а не выводиться из
	// чтения кода.
	Listeners    []string
	Sites        []string
	Capabilities []string
	Files        int
	Carried      int
	NoSubject    int
	Excused      int
	Findings     []FoundationCell
	Stale        []string
	cells        int
}

func (c FoundationCensus) String() string {
	return fmt.Sprintf("перепись: возможностей %d · каталогов-слушателей %d · мест сборки сервера %d · "+
		"клеток %d · прод-файлов разобрано %d · несут %d · нет предмета %d · записанных пропусков %d · "+
		"находок %d · истёкших записей %d",
		len(c.Capabilities), len(c.Listeners), len(c.Sites), c.cells, c.Files,
		c.Carried, c.NoSubject, c.Excused, len(c.Findings), len(c.Stale))
}

// foundationUnit — одна единица счёта с уже посчитанной достижимостью.
type foundationUnit struct {
	// ID — как единица называется в переписи и в находке.
	ID string
	// Aliases — имена, которыми запись ведомости вправе её назвать. У места
	// сборки их два: собственное имя и КАТАЛОГ. Запись, названная каталогом,
	// покрывает все его места — иначе каждый пропуск пришлось бы выписывать
	// по разу на сервер, и перечень стал бы нечитаемым там, где долг общий.
	Aliases []string
	// Where — координата для текста находки; у каталога пуста.
	Where string
	Reach map[string]bool
}

func (u foundationUnit) named(name string) bool {
	for _, a := range u.Aliases {
		if a == name {
			return true
		}
	}
	return false
}

// Adjudicate раскладывает клетки по четырём исходам и отдельно называет записи
// обеих ведомостей, которым больше нечего исключать.
//
// Единица клетки берётся У ВОЗМОЖНОСТИ, а не одна на всю перепись: возможность,
// провязываемую в сервер, спрашивают с КАЖДОГО места сборки, а заводимую раз на
// процесс — с каталога. Единая единица была бы неверна в обе стороны сразу —
// либо частичная потеря провязки невидима, либо у сужателя списочной выдачи
// спрашивают с места, где его нет и быть не может.
func (r FoundationRoster) Adjudicate(dirs []string, scans map[string]*FoundationScan,
	sites []FoundationSite, providerScans map[string]*FoundationScan) FoundationCensus {

	cen := FoundationCensus{Listeners: append([]string(nil), dirs...)}
	for _, c := range r.Capabilities {
		cen.Capabilities = append(cen.Capabilities, c.Name)
	}
	sort.Strings(cen.Listeners)

	// Достижимость считается ОДИН раз на единицу и переиспользуется вердиктом и
	// обеими ведомостями: иначе «несёт» у клетки и «несёт» у самоистечения стали
	// бы двумя предикатами об одном предмете и разошлись бы молча.
	var procUnits []foundationUnit
	for _, d := range cen.Listeners {
		u := foundationUnit{ID: d, Aliases: []string{d}, Reach: map[string]bool{}}
		if s := scans[d]; s != nil {
			cen.Files += s.Files
			u.Reach = r.Reach(s, providerScans)
		}
		procUnits = append(procUnits, u)
	}
	var siteUnits []foundationUnit
	for _, s := range sites {
		cen.Sites = append(cen.Sites, s.ID)
		siteUnits = append(siteUnits, foundationUnit{
			ID:      s.ID,
			Aliases: []string{s.ID, s.Dir},
			Where:   fmt.Sprintf("%s:%d", s.File, s.Line),
			Reach:   r.Reach(s.Slice, providerScans),
		})
	}

	unitsFor := func(c FoundationCapability) []foundationUnit {
		if c.Unit == FoundationUnitListener {
			return siteUnits
		}
		return procUnits
	}

	byName := map[string]FoundationCapability{}
	for _, c := range r.Capabilities {
		byName[c.Name] = c
	}

	excusedWhole := map[string]FoundationLedgerEntry{}
	excusedCell := map[string]FoundationLedgerEntry{}
	for _, e := range r.Ledger {
		c, known := byName[e.Capability]
		switch {
		case !known:
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"ведомость пропусков называет возможность %q — такой в наборе нет", e.Capability))
			continue
		case e.Issue <= 0:
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"пропуск %q у %q не называет задачи: долг без задачи неотличим от забытого",
				e.Capability, listenerOrAll(e.Listener)))
			continue
		}
		units := unitsFor(c)
		if e.Listener == "" {
			// Запись «не несёт ни один» истекает на ПЕРВОМ усыновившем.
			for _, u := range units {
				if u.Reach[e.Capability] {
					cen.Stale = append(cen.Stale, fmt.Sprintf(
						"пропуск %q объявлен целиком (задача #%d), а %q её уже несёт: "+
							"запись обязана стать пообъектной либо уйти", e.Capability, e.Issue, u.ID))
					break
				}
			}
			excusedWhole[e.Capability] = e
			continue
		}
		var matched, live []foundationUnit
		for _, u := range units {
			if !u.named(e.Listener) {
				continue
			}
			matched = append(matched, u)
			if !u.Reach[e.Capability] {
				live = append(live, u)
			}
		}
		if len(matched) == 0 {
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"ведомость пропусков называет слушателя %q (возможность %q) — такого в дереве нет",
				e.Listener, e.Capability))
			continue
		}
		// Запись истекает, когда ВСЕ названные ею единицы возможность усыновили:
		// пока хоть одна не усыновила, ей ещё есть что исключать. Именно поэтому
		// запись, названная каталогом с двумя серверами, не истекает от того, что
		// провязку получил один из них, — и именно поэтому число «несут» в
		// переписи при этом растёт, а «записанных пропусков» падает.
		if len(live) == 0 {
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"пропуску %q у %q (задача #%d) больше нечего исключать — возможность усыновлена "+
					"всеми %d единицами, которые запись называет",
				e.Capability, e.Listener, e.Issue, len(matched)))
			continue
		}
		for _, u := range live {
			excusedCell[u.ID+"\x00"+e.Capability] = e
		}
	}

	noSubj := map[string]FoundationNoSubject{}
	for _, n := range r.NoSubject {
		c, known := byName[n.Capability]
		switch {
		case !known:
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"ведомость отсутствия предмета называет возможность %q — такой в наборе нет", n.Capability))
			continue
		case strings.TrimSpace(n.Why) == "":
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"отсутствие предмета %q у %q не названо причиной: изъятие без причины есть маска",
				n.Capability, n.Listener))
			continue
		}
		var matched []foundationUnit
		refuted := false
		for _, u := range unitsFor(c) {
			if !u.named(n.Listener) {
				continue
			}
			matched = append(matched, u)
			if u.Reach[n.Capability] {
				refuted = true
			}
		}
		switch {
		case len(matched) == 0:
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"ведомость отсутствия предмета называет слушателя %q (возможность %q) — такого в дереве нет",
				n.Listener, n.Capability))
			continue
		case refuted:
			// Здесь достаточно ОДНОЙ усыновившей единицы, и это не та же мерка,
			// что у пропуска: пропуск говорит «работы ещё нет», а эта запись —
			// «работы не предвидится, предмета нет». Единица, возможность
			// усыновившая, опровергает ВТОРОЕ целиком.
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"запись «у %s нет предмета для %q» опровергнута деревом: возможность усыновлена",
				n.Listener, n.Capability))
			continue
		}
		for _, u := range matched {
			noSubj[u.ID+"\x00"+n.Capability] = n
		}
	}

	for _, c := range r.Capabilities {
		for _, u := range unitsFor(c) {
			cen.cells++
			key := u.ID + "\x00" + c.Name
			switch {
			case u.Reach[c.Name]:
				cen.Carried++
			case noSubj[key].Why != "":
				cen.NoSubject++
			case excusedCell[key].Issue > 0:
				cen.Excused++
			case excusedWhole[c.Name].Issue > 0:
				cen.Excused++
			default:
				where := ""
				if u.Where != "" {
					where = " (" + u.Where + ")"
				}
				cen.Findings = append(cen.Findings, FoundationCell{
					Listener: u.ID, Capability: c.Name, Verdict: FoundationFinding,
					Detail: fmt.Sprintf("%s%s не несёт возможность %q (%s) и не объяснил почему",
						u.ID, where, c.Name, c.Pkg),
				})
			}
		}
	}
	sort.Strings(cen.Stale)
	return cen
}
func listenerOrAll(l string) string {
	if l == "" {
		return "всех слушателей"
	}
	return l
}

// DiscoverListeners выводит перечень слушателей ИЗ ДЕРЕВА, а не выписывает его.
//
// Кандидат — верхнеуровневый каталог репозитория либо каталог одного сервиса;
// слушателем он становится по УЛИКЕ: где-то в его прод-исходниках собирается
// gRPC-сервер. Рукописный перечень разошёлся бы с деревом молча — новый сервис
// просто не попал бы в перепись и выглядел бы усыновившим всё.
//
// Два каталога исключены ПО РОЛИ, а не по имени в списке отказов: сам фундамент
// (там конструктор и живёт — он не слушатель, а его источник) и каталог-родитель
// сервисов (его слушатели считаются по одному, иначе они посчитались бы дважды).
func DiscoverListeners(root string, serverMarkers []string) ([]string, map[string]*FoundationScan, []string, error) {
	const foundationDir, servicesParent = "pkg", "services"

	var candidates []string
	top, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("корень %s не читается: %w", root, err)
	}
	for _, e := range top {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.Name() == foundationDir || e.Name() == servicesParent {
			continue
		}
		candidates = append(candidates, e.Name())
	}
	svc, err := os.ReadDir(filepath.Join(root, servicesParent))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, nil, fmt.Errorf("каталог сервисов не читается: %w", err)
	}
	for _, e := range svc {
		if e.IsDir() {
			candidates = append(candidates, servicesParent+"/"+e.Name())
		}
	}
	sort.Strings(candidates)

	scans := map[string]*FoundationScan{}
	var listeners []string
	for _, c := range candidates {
		abs := filepath.Join(root, filepath.FromSlash(c))
		if fi, serr := os.Stat(abs); serr != nil || !fi.IsDir() {
			continue
		}
		s, serr := ScanGoTree(abs)
		if serr != nil {
			// Каталог без исходников Go разбором не считается: `fs.ErrNotExist`
			// сюда не приходит, а любая другая беда — честный отказ.
			if os.IsNotExist(serr) || strings.Contains(serr.Error(), fs.ErrNotExist.Error()) {
				continue
			}
			return nil, nil, nil, serr
		}
		if s.Files == 0 {
			continue
		}
		serves := false
		for _, m := range serverMarkers {
			if s.Selects[m] {
				serves = true
				break
			}
		}
		if !serves {
			continue
		}
		listeners = append(listeners, c)
		scans[c] = s
	}
	return listeners, scans, candidates, nil
}

// LedgerRecordsWhoseIssueIsClosed — записи, чья задача закрыта.
//
// Вторая ось самоистечения ведомости, и она НЕ выводится из первой. Запись
// снимается с прогона, когда возможность усыновлена, — это про дерево. Но
// запись держится ЗАДАЧЕЙ, а задачу закрывают отдельно от кода: закрыли #693,
// не провязав пределы у края, — и запись извиняет его дальше, вечно и молча.
// Ровно этот класс уже случился с прежней редакцией набора (две записи ссылались
// на задачи, закрытые за неделю до), и нашёлся он не гейтом, а перемером руками.
//
// Состояния подаются картой, а не берутся отсюда: измерение СЕТЕВОЕ, вердикт
// гейта не вправе быть функцией доступности трекера, а решение обязано быть
// проверяемо инъекцией без сети.
func (r FoundationRoster) LedgerRecordsWhoseIssueIsClosed(states map[int]string) []string {
	var bad []string
	for _, e := range r.Ledger {
		state, known := states[e.Issue]
		if !known || !strings.EqualFold(state, "CLOSED") {
			continue
		}
		bad = append(bad, fmt.Sprintf(
			"пропуск %q у %q держится задачей #%d, а она ЗАКРЫТА: запись пережила своё "+
				"основание и будет извинять пропуск вечно — либо возможность усыновлена и запись "+
				"уходит, либо задачу закрыли рано и её открывают обратно",
			e.Capability, listenerOrAll(e.Listener), e.Issue))
	}
	sort.Strings(bad)
	return bad
}

// LedgerIssues — различные номера задач ведомости, по возрастанию.
func (r FoundationRoster) LedgerIssues() []int {
	seen := map[int]bool{}
	var out []int
	for _, e := range r.Ledger {
		if e.Issue > 0 && !seen[e.Issue] {
			seen[e.Issue] = true
			out = append(out, e.Issue)
		}
	}
	sort.Ints(out)
	return out
}
