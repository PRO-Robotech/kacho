// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// foreignidpname.go — ГЕЙТ КЛАССА: имя чужого поставщика личности, носимое
// НАШИМ ИМЕНЕМ — константой, функцией, типом, полем, параметром, переменной.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Чужой поставщик (kratos/hydra) на переезде жив: его издатель сегодня стоит в
// боевом профиле, его адреса приходят ручками посадки, его пути разобраны
// ведомостью `providersurface.go`. Всё это — ЕГО КООРДИНАТЫ, и они законны,
// пока он принимается в производстве.
//
// Отдельно от координат существует другое: наше собственное имя, носящее его
// название там, где содержание к нему отношения не имеет. Константа фикстуры
// `testHydraIss`, помощник `hydraClaims`, параметр `hydraMirroredKids` — всё
// это имена НАШИХ сущностей: адреса издателя, набора утверждений, перечня
// идентификаторов ключей. Ни одна из них не перестаёт работать, если поставщика
// снимут; снимут — и имя станет ложью, которую компилятор не заметит.
//
// Цена не в эстетике. Имя — это то, по чему предмет ищут. Пока наши сущности
// носят чужое название, предикат «что в дереве держится за поставщика» даёт
// ответ, в котором фикстуры неотличимы от настоящих разговоров с ним, и
// снятие поставщика каждый раз упирается в разбор, который никто не
// переписывал.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СУДИТСЯ УЗЕЛ РАЗБОРА, А НЕ СЛОВО
//
// Та же граница, что у `providersurface.go` и `retiredissuerclaim.go`: гейт по
// слову краснел бы на исправном дереве. Слово законно в строковом литерале
// (`"https://hydra.api.kacho.cloud"`, `"KACHO_REGISTRY_HYDRA_ISSUER"`,
// `"kacho-umbrella-hydra-public"`) и в комментарии (разбор переезда). Судится
// ровно ОБЪЯВЛЕННОЕ ИМЯ — узел синтаксического разбора, у которого есть
// координата и который правится переименованием, а не переписыванием факта.
//
// ─────────────────────────────────────────────────────────────────────────────
// ТРИ ИСХОДА, ЧЕТВЁРТОГО НЕТ
//
// У найденного имени исходов три: снять вместе с предметом одним изменением ·
// перевести на производимый деревом признак (пакет `jwks` производит его сам:
// `testPlatformIss` / `testLegacyIss`) · переутвердить новое свойство того же
// предмета. «Оставить как есть» исходом не является — для этого есть ведомость,
// и у каждой её записи стоит предикат снятия.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ГЕЙТ НЕ ВИДИТ — НАЗВАНО ВСЛУХ
//
// Имя, собранное в рантайме, и имя внешнего пакета, приходящее импортом, под
// ось не подпадают. Строковый литерал и комментарий — намеренно: это координаты
// и проза, у них свои держатели (`providersurface.go`, `retiredissuerclaim.go`).
// Дерево вне Go (python-фикстуры, shell, чарты) этот гейт не читает: там имя
// поставщика почти всегда координата стенда, и предикат по ним дал бы перебор.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"unicode"
)

// foreignIDPWords — слова, по которым узнаётся название чужого поставщика.
//
// Перечень закрыт и гоняется инъекцией поимённо: слово, о котором распознаватель
// не знает, даёт не красное и не зелёное, а молчание.
var foreignIDPWords = []string{"hydra", "kratos", "ory", "oryd"}

// foreignIDPNotWords — слова, которые ВЫГЛЯДЯТ как находка и ею не являются.
//
// `hydrate`/`hydration`/`dehydrated` — гидратация, другой референт. Он живёт в
// консоли, а не в Go, но распознаватель, не знающий о нём, назвал бы его
// находкой в первый же день, когда такое имя появится.
var foreignIDPNotWords = []string{"hydrat"}

// errForeignIDPEmptyCorpus — обход не принёс ни одного файла.
var errForeignIDPEmptyCorpus = errors.New("обход пуст: разобрано ноль файлов")

// Виды расхождений. Названы константами, чтобы инъекция утверждала ВИД, а не
// подстроку текста.
const (
	// ForeignIDPNameUnledgered — имя в области, которую ведомость не называет.
	ForeignIDPNameUnledgered = "имя вне ведомости"
	// ForeignIDPNameCountDrift — область названа, а число имён в ней разошлось
	// с объявленным. Расхождение ловится в ОБЕ стороны: вверх — поверхность
	// выросла; вниз — запись пережила часть своего предмета.
	ForeignIDPNameCountDrift = "число имён разошлось с ведомостью"
	// ForeignIDPNameStale — запись ведомости, которой больше нечего называть.
	ForeignIDPNameStale = "запись ведомости пережила предмет"
)

// ForeignIDPNameLedgerEntry — одна запись ведомости: область дерева, где имена
// сегодня остаются, вместе с ТОЧНЫМ их числом.
//
// Точным, а не потолком: потолок прощает рост до себя, и запись, поставленная
// «с запасом», перестаёт быть наблюдением.
type ForeignIDPNameLedgerEntry struct {
	// Area — первый сегмент пути с косой чертой (`gateway/`). Не произвольный
	// префикс: произвольный накрыл бы соседей, которых никто не рассматривал.
	Area string
	// Names — сколько имён этой оси в области сегодня. Точно.
	Names int
	// Why — почему они там сегодня есть.
	Why string
	// Until — при каком факте о дереве запись обязана быть снята.
	Until string
}

// ForeignIDPNameFinding — одно расхождение дерева с ведомостью.
type ForeignIDPNameFinding struct {
	// File — путь от корня дерева. У находки об области — сама область.
	File string
	// Line — координата имени. Ноль у находок об области целиком.
	Line int
	// Kind — вид расхождения.
	Kind string
	// Name — объявленное имя. Пусто у находок об области целиком.
	Name string
	// Word — слово перечня, по которому имя узнано.
	Word string
	// Detail — человеческая часть.
	Detail string
}

// ForeignIDPNameCensus — объём осмотренного: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type ForeignIDPNameCensus struct {
	// Files — разобрано файлов Go.
	Files int
	// Idents — осмотрено объявленных имён (весь знаменатель обхода).
	Idents int
	// Names — из них несущих название поставщика.
	Names int
	// Areas — областей, где такие имена найдены.
	Areas int
	// LedgerEntries — записей ведомости на входе.
	LedgerEntries int
	// LedgerNames — имён, объявленных ведомостью суммарно.
	LedgerNames int
	// Findings — находок.
	Findings int
}

func (c ForeignIDPNameCensus) String() string {
	return fmt.Sprintf("перепись: файлов Go %d · объявленных имён %d · из них с названием "+
		"поставщика %d в %d областях · записей ведомости %d (имён объявлено %d) · находок %d",
		c.Files, c.Idents, c.Names, c.Areas, c.LedgerEntries, c.LedgerNames, c.Findings)
}

// ForeignIDPNameHit — одно найденное имя вместе с координатой.
type ForeignIDPNameHit struct {
	File string
	Line int
	Kind string
	Name string
	Word string
}

// SplitIdentifierWords разбивает объявленное имя на слова по ВСЕМ законным
// формам записи: camelCase, PascalCase, snake_case, SCREAMING_SNAKE, цифровые
// границы и сплошные заглавные прогоны (`HydraClientID` → Hydra·Client·ID).
//
// Разбор нужен, чтобы `repository`, `factory`, `memory`, `history`, `directory`
// не становились находкой по подстроке «ory»: слово там одно, и оно не «ory».
func SplitIdentifierWords(name string) []string {
	var (
		out  []string
		cur  []rune
		runs = []rune(name)
	)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
		}
	}
	for i, r := range runs {
		switch {
		case r == '_' || r == '-':
			flush()
			continue
		case unicode.IsUpper(r):
			// Новое слово, если предыдущий был строчным либо цифрой, либо если
			// заглавный прогон кончается строчной (ID|Client → ID · Client).
			if i > 0 {
				prev := runs[i-1]
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					flush()
				} else if unicode.IsUpper(prev) && i+1 < len(runs) && unicode.IsLower(runs[i+1]) {
					flush()
				}
			}
		case unicode.IsDigit(r):
			if i > 0 && !unicode.IsDigit(runs[i-1]) {
				flush()
			}
		default:
			if i > 0 && unicode.IsDigit(runs[i-1]) {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return out
}

// ForeignIDPNameWord — несёт ли объявленное имя название поставщика, и каким
// словом перечня оно узнано. Пустая строка — не несёт.
func ForeignIDPNameWord(name string) string {
	for _, w := range SplitIdentifierWords(name) {
		lw := strings.ToLower(w)
		skip := false
		for _, nw := range foreignIDPNotWords {
			if strings.Contains(lw, nw) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, fw := range foreignIDPWords {
			if lw == fw || (len(fw) > 3 && strings.Contains(lw, fw)) {
				return fw
			}
		}
	}
	return ""
}

// foreignIDPArea — область пути: первый сегмент с косой чертой.
func foreignIDPArea(rel string) string {
	if i := strings.Index(rel, "/"); i >= 0 {
		return rel[:i+1]
	}
	return "./"
}

// CollectForeignIDPNames разбирает исходники (путь → содержимое) и возвращает
// ВСЕ объявленные имена, несущие название поставщика, вместе с переписью.
//
// Ошибка — только на неразбираемом исходнике: молчаливый пропуск нечитаемого
// файла превратил бы «не прочитали» в «имён нет».
func CollectForeignIDPNames(sources map[string]string) ([]ForeignIDPNameHit, ForeignIDPNameCensus, error) {
	census := ForeignIDPNameCensus{}
	if len(sources) == 0 {
		return nil, census, fmt.Errorf("%w — судить нечего", errForeignIDPEmptyCorpus)
	}
	rels := make([]string, 0, len(sources))
	for rel := range sources {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var hits []ForeignIDPNameHit
	for _, rel := range rels {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, rel, sources[rel], 0)
		if err != nil {
			return nil, census, fmt.Errorf("разбор %s: %w", rel, err)
		}
		census.Files++

		add := func(kind string, id *ast.Ident) {
			if id == nil || id.Name == "_" {
				return
			}
			census.Idents++
			if w := ForeignIDPNameWord(id.Name); w != "" {
				hits = append(hits, ForeignIDPNameHit{
					File: rel, Line: fset.Position(id.Pos()).Line,
					Kind: kind, Name: id.Name, Word: w,
				})
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.FuncDecl:
				add("функция", v.Name)
			case *ast.TypeSpec:
				add("тип", v.Name)
			case *ast.ValueSpec:
				for _, id := range v.Names {
					add("константа/переменная", id)
				}
			case *ast.AssignStmt:
				if v.Tok != token.DEFINE {
					return true
				}
				for _, l := range v.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						add("переменная", id)
					}
				}
			case *ast.Field:
				for _, id := range v.Names {
					add("поле/параметр", id)
				}
			case *ast.LabeledStmt:
				add("метка", v.Label)
			}
			return true
		})
	}
	census.Names = len(hits)
	areas := map[string]struct{}{}
	for _, h := range hits {
		areas[foreignIDPArea(h.File)] = struct{}{}
	}
	census.Areas = len(areas)
	return hits, census, nil
}

// JudgeForeignIDPNames сверяет найденные имена с ведомостью.
//
// Возвращает находки трёх видов и перепись. Пустой корпус — отказ.
func JudgeForeignIDPNames(
	sources map[string]string, ledger []ForeignIDPNameLedgerEntry,
) ([]ForeignIDPNameFinding, ForeignIDPNameCensus, error) {
	hits, census, err := CollectForeignIDPNames(sources)
	if err != nil {
		return nil, census, err
	}
	census.LedgerEntries = len(ledger)

	declared := make(map[string]ForeignIDPNameLedgerEntry, len(ledger))
	for _, e := range ledger {
		declared[e.Area] = e
		census.LedgerNames += e.Names
	}

	byArea := map[string][]ForeignIDPNameHit{}
	for _, h := range hits {
		a := foreignIDPArea(h.File)
		byArea[a] = append(byArea[a], h)
	}

	var findings []ForeignIDPNameFinding
	areas := make([]string, 0, len(byArea))
	for a := range byArea {
		areas = append(areas, a)
	}
	sort.Strings(areas)

	for _, a := range areas {
		got := byArea[a]
		e, listed := declared[a]
		if !listed {
			for _, h := range got {
				findings = append(findings, ForeignIDPNameFinding{
					File: h.File, Line: h.Line, Kind: ForeignIDPNameUnledgered,
					Name: h.Name, Word: h.Word,
					Detail: h.Kind + " носит название чужого поставщика («" + h.Word + "»)",
				})
			}
			continue
		}
		if len(got) != e.Names {
			findings = append(findings, ForeignIDPNameFinding{
				File: a, Kind: ForeignIDPNameCountDrift,
				Detail: fmt.Sprintf("ведомость объявляет имён %d, в дереве их %d — "+
					"перепишите число на %d либо снимите имена; предикат снятия записи: %s",
					e.Names, len(got), len(got), e.Until),
			})
		}
	}

	for _, e := range ledger {
		if len(byArea[e.Area]) == 0 {
			findings = append(findings, ForeignIDPNameFinding{
				File: e.Area, Kind: ForeignIDPNameStale,
				Detail: "в области не осталось ни одного имени этой оси — запись обязана " +
					"быть снята вместе с предметом; предикат её снятия: " + e.Until,
			})
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	census.Findings = len(findings)
	return findings, census, nil
}
