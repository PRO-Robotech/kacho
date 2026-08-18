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
			// ParseComments не запрашиваем намеренно: комментарии в разбор не
			// попадают вовсе, поэтому «упомянул в объяснении» физически не может
			// стать «провязал».
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
		})
	if err != nil {
		return nil, err
	}
	return s, nil
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
	Ledger       []FoundationLedgerEntry
	NoSubject    []FoundationNoSubject
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
	}
	sort.Strings(bad)
	return bad
}

// FoundationCell — вердикт по одной клетке (слушатель × возможность).
type FoundationCell struct {
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
	Listeners    []string
	Capabilities []string
	Files        int
	Carried      int
	NoSubject    int
	Excused      int
	Findings     []FoundationCell
	Stale        []string
}

func (c FoundationCensus) String() string {
	return fmt.Sprintf("перепись: возможностей %d · слушателей %d · клеток %d · прод-файлов разобрано %d · "+
		"несут %d · нет предмета %d · записанных пропусков %d · находок %d · истёкших записей %d",
		len(c.Capabilities), len(c.Listeners), len(c.Capabilities)*len(c.Listeners), c.Files,
		c.Carried, c.NoSubject, c.Excused, len(c.Findings), len(c.Stale))
}

// Adjudicate раскладывает клетки по четырём исходам и отдельно называет записи
// обеих ведомостей, которым больше нечего исключать.
func (r FoundationRoster) Adjudicate(listeners []string, scans map[string]*FoundationScan,
	providerScans map[string]*FoundationScan) FoundationCensus {

	cen := FoundationCensus{Listeners: append([]string(nil), listeners...)}
	for _, c := range r.Capabilities {
		cen.Capabilities = append(cen.Capabilities, c.Name)
	}
	sort.Strings(cen.Listeners)

	capNames := map[string]bool{}
	for _, c := range r.Capabilities {
		capNames[c.Name] = true
	}
	listenerSet := map[string]bool{}
	for _, l := range listeners {
		listenerSet[l] = true
	}

	// Достижимость по каждому слушателю — считается ОДИН раз и переиспользуется
	// обеими ведомостями: иначе «несёт» у вердикта и «несёт» у самоистечения
	// стали бы двумя предикатами об одном предмете и разошлись бы молча.
	reach := map[string]map[string]bool{}
	for _, l := range listeners {
		if s := scans[l]; s != nil {
			cen.Files += s.Files
			reach[l] = r.Reach(s, providerScans)
		} else {
			reach[l] = map[string]bool{}
		}
	}

	excusedWhole := map[string]FoundationLedgerEntry{}
	excusedCell := map[string]FoundationLedgerEntry{}
	for _, e := range r.Ledger {
		switch {
		case !capNames[e.Capability]:
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"ведомость пропусков называет возможность %q — такой в наборе нет", e.Capability))
			continue
		case e.Listener != "" && !listenerSet[e.Listener]:
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"ведомость пропусков называет слушателя %q (возможность %q) — такого в дереве нет",
				e.Listener, e.Capability))
			continue
		case e.Issue <= 0:
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"пропуск %q у %q не называет задачи: долг без задачи неотличим от забытого",
				e.Capability, listenerOrAll(e.Listener)))
			continue
		}
		if e.Listener == "" {
			// Запись «не несёт ни один» истекает на ПЕРВОМ усыновившем.
			for _, l := range listeners {
				if reach[l][e.Capability] {
					cen.Stale = append(cen.Stale, fmt.Sprintf(
						"пропуск %q объявлен целиком (задача #%d), а слушатель %q её уже несёт: "+
							"запись обязана стать пообъектной либо уйти", e.Capability, e.Issue, l))
					break
				}
			}
			excusedWhole[e.Capability] = e
			continue
		}
		if reach[e.Listener][e.Capability] {
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"пропуску %q у %q (задача #%d) больше нечего исключать — возможность усыновлена",
				e.Capability, e.Listener, e.Issue))
			continue
		}
		excusedCell[e.Listener+"\x00"+e.Capability] = e
	}

	noSubj := map[string]FoundationNoSubject{}
	for _, n := range r.NoSubject {
		switch {
		case !capNames[n.Capability]:
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"ведомость отсутствия предмета называет возможность %q — такой в наборе нет", n.Capability))
			continue
		case !listenerSet[n.Listener]:
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"ведомость отсутствия предмета называет слушателя %q (возможность %q) — такого в дереве нет",
				n.Listener, n.Capability))
			continue
		case strings.TrimSpace(n.Why) == "":
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"отсутствие предмета %q у %q не названо причиной: изъятие без причины есть маска",
				n.Capability, n.Listener))
			continue
		}
		if reach[n.Listener][n.Capability] {
			cen.Stale = append(cen.Stale, fmt.Sprintf(
				"запись «у %s нет предмета для %q» опровергнута деревом: возможность усыновлена",
				n.Listener, n.Capability))
			continue
		}
		noSubj[n.Listener+"\x00"+n.Capability] = n
	}

	for _, l := range cen.Listeners {
		for _, c := range r.Capabilities {
			key := l + "\x00" + c.Name
			switch {
			case reach[l][c.Name]:
				cen.Carried++
			case noSubj[key].Why != "":
				cen.NoSubject++
			case excusedCell[key].Issue > 0:
				cen.Excused++
			case excusedWhole[c.Name].Issue > 0:
				cen.Excused++
			default:
				cen.Findings = append(cen.Findings, FoundationCell{
					Listener: l, Capability: c.Name, Verdict: FoundationFinding,
					Detail: fmt.Sprintf("слушатель %s не несёт возможность %q (%s) и не объяснил почему",
						l, c.Name, c.Pkg),
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
