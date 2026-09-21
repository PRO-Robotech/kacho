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
// ЧТО ГЕЙТ ВИДИТ — ПЕРЕЧЕНЬ УЗЛОВ, ЗАКРЫТЫЙ И ПРОВЕРЕННЫЙ ПООТДЕЛЬНОСТИ
//
// Объявленным именем считаются: имя пакета · псевдоним импорта · функция ·
// тип · константа/переменная · короткое объявление · переменная `range` ·
// поле/параметр (включая приёмник и параметр типа) · метка. Каждый узел гоняется
// инъекцией ОТДЕЛЬНО (`_injection_test.go`, «форма записи»): узел, о котором
// распознаватель не знает, даёт не красное и не зелёное, а молчание — и для него
// не растёт даже знаменатель.
//
// Три узла — имя пакета, псевдоним импорта и переменная `range` — добраны
// опытом приёмки, который нашёл на них молчание. Классы были ЛАТЕНТНЫМИ: в
// дереве таких имён нет ни одного, и число находок от добора не изменилось
// (103 ИМЕНИ до и после). Изменилось обещание заголовка, ради которого проба и
// стоит.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ГЕЙТ НЕ ВИДИТ — НАЗВАНО ВСЛУХ
//
// Имя, собранное в рантайме, и имя внешнего пакета, приходящее импортом БЕЗ
// псевдонима, под ось не подпадают: второе не наше и переименованием не
// правится. Строковый литерал и комментарий — намеренно: это координаты и
// проза, у них свои держатели (`providersurface.go`, `retiredissuerclaim.go`).
// Порождённые стабы отсеяны со своим основанием (`ForeignIDPNameSkipRules`).
// Дерево вне Go (python-фикстуры, shell, чарты) этот гейт не читает: там имя
// поставщика почти всегда координата стенда, и предикат по ним дал бы перебор.
//
// ТРЁХБУКВЕННОЕ ИМЯ, СЛИПШЕЕСЯ СПЛОШНОЙ СТРОЧНОЙ, — МОЛЧИТ. Правило отбора
// `len(fw) > 3` в `ForeignIDPNameWord` даёт вхождение подстроки только длинным
// словам перечня (`hydra`, `kratos`); трёхбуквенное `ory` сверяется ТОЛЬКО
// равенством. Следствие названо прямо: `oryfoo`, `oryflows`, `orysession`,
// `ORYFLOWS` не дают ни красного, ни зелёного — они молчат. Это тот же род
// слепоты, за который эта работа уже возвращалась однажды, здесь закрытый для
// одной семьи слов и НЕ закрытый для этой.
//
// Почему правило не меняется: цена закрытия ЗАМЕРЕНА. Свой замер @6b8246108e4
// на чистом дереве по узлам «пакет · функция · тип · константа/переменная ·
// поле/параметр»: имён, чьё слово содержит «ory», — 327, и вхождение сделало бы
// находкой 326 из них сверх нынешних 103. Причина в том, что «ory» лежит внутри
// `repository`, `factory`, `memory`, `history`, `directory`, `category`,
// `advisory`, `inventory`, `mandatory`. Независимый замер приёмки по более
// широкому набору узлов дал 455 имён — числа разные, потому что разные
// знаменатели, но порядок один и вывод общий. Гейт с сотнями находок на
// исправном дереве снимается первым же обходом — то есть закрытие этого класса
// стоило бы самого гейта. Разбор имени на слова (`SplitIdentifierWords`) закрывает его для всех
// РАЗДЕЛЁННЫХ форм записи — `oryFlows`, `ory_flows`, `ORY_FLOWS`; открытой
// остаётся только сплошная строчная склейка.
//
// ИМЕНА ФАЙЛОВ И КАТАЛОГОВ — НЕ ВИДИТ ВОВСЕ, И ЭТО НЕ «ДЕРЕВО ВНЕ GO».
// Название поставщика живёт не только в узлах разбора: оно стоит в именах путей.
// Замер @6b8246108e4 на чистом дереве: `git ls-files` даёт 28 отслеженных путей,
// чей базовый сегмент или сегмент каталога несёт название поставщика, из них 5
// не-порождённых `.go` (все в `gateway/`) и один каталог-носитель
// (`kratos-selfservice-ui`). Это НАШИ имена, они правятся переименованием и
// переживут снятие поставщика ложью — то есть ровно предмет этой оси, только в
// ДРУГОЙ ЕДИНИЦЕ (путь, а не узел разбора).
//
// Здесь он не добирается намеренно: у пути другой знаменатель (28 против 204888),
// другая цена правки (переименование тянет импорты каталога-пакета, ссылки
// документов, задания конвейера и историю) и другой отбор — 16 из 28 путей
// названы именем ЧУЖОГО артефакта (`hydra-0.62.1.tgz`, `kratos-0.62.1.tgz`,
// шаблоны его подчарта), которое не наше и переименованием не правится вовсе.
// Предикат, который это различает, — отдельная работа со своей ведомостью и
// своей инъекцией, а не строка в этом гейте. До тех пор ни один замер по цели
// эти 28 путей НЕ СЧИТАЛ: все считали имена внутри кода.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВА РАЗНЫХ «103» — ЕДИНИЦА НАЗЫВАЕТСЯ ПРИ КАЖДОМ ЧИСЛЕ
//
// У этой полосы два замера одного предмета, и числа у них совпали СЛУЧАЙНО:
// 103 ИМЕНИ (объявленный идентификатор Go) по всему дереву — предмет этого
// гейта; 103 СТРОКИ с именем поставщика в пакете `services/registry/internal/
// clients/jwks` на базе bec320cf47d — предмет строчного замера полосы, снятый
// до одной строки. Разные единицы, разные области, разные предметы. Поэтому
// перепись называет единицу при каждом числе, и то же требуется от всякого,
// кто эти числа пересказывает.

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
//
// Сверяется РАВЕНСТВОМ слова, а не вхождением подстроки. Вхождением перечень
// гасил бы находку целиком: `hydratest`, `hydratokenTTL` — имена, где `hydrat`
// оказался стыком двух слов, записанных сплошной строчной, — не находились бы
// ВОВСЕ, и для них не рос бы даже знаменатель. Опыт приёмки нашёл ровно этот
// класс; он латентный (сегодня в дереве из `hydrat-` только формы гидратации),
// но достижим первым же именем такого вида.
//
// ПЕРЕЧЕНЬ НЕ ЗАКРЫТ, и это сказано прямо, потому что прежняя редакция этой
// шапки объявляла его закрытым — неверно. Семья слов гидратации незакрываема
// перечислением: опыт приёмки прогнал двадцать три её формы и нашёл ВОСЕМЬ, не
// названных здесь, — `rehydration`, `dehydration`, `hydrator`, `hydrators`,
// `hydratable`, `unhydrated`, `prehydrate` и слово `Rehydration` внутри
// составного имени (`CacheRehydrationPolicy`). Несимметрия видна и в самом
// перечне: `hydration` в нём есть, а `rehydration` и `dehydration` — нет, хотя
// `rehydrate` и `dehydrate` стоят.
//
// ГРАНИЦА НАЗВАНА И ИЗМЕРЕНА: всякая форма гидратации вне перечня даёт ЛОЖНУЮ
// НАХОДКУ, а не молчание. Направление выбрано намеренно и остаётся верным при
// неполном перечне: ложная находка краснеет с координатой и снимается одной
// строкой здесь же, молчание не видно никогда. Гнаться за семьёй слов перечень
// не будет; он пополняется в день, когда такое имя в дереве появится.
var foreignIDPNotWords = []string{
	"hydrate", "hydrates", "hydrated", "hydrating", "hydration",
	"rehydrate", "rehydrates", "rehydrated", "rehydrating",
	"dehydrate", "dehydrates", "dehydrated", "dehydrating",
}

// ForeignIDPNameSkipRule — одно правило отсева ЭТОГО гейта.
//
// Отсев не наследуется. Чужой перечень, заведённый под чужой предмет (у
// лицензионного гейта это `.git .claude docs node_modules vendor bin`), твоим
// предикатом не является: он сужает обход по основанию, которое здесь никто не
// переутверждал, и любой файл вошёл бы в него, просто выбрав каталог. Замерено
// на bec320cf47d: под ВСЕМИ шестью сегментами отслеженных `.go` ноль — то есть
// унаследованный перечень не отсеивал НИЧЕГО и был чистым обещанием.
//
// Каждое правило несёт основание и ОБЯЗАНО иметь предмет: правило, которому
// нечего отсеивать, — находка пробы, а не безобидная строка.
type ForeignIDPNameSkipRule struct {
	// Name — как правило называется в переписи.
	Name string
	// Why — основание: почему эти файлы судить нечем.
	Why string
	// Match — предикат по пути от корня дерева.
	Match func(rel string) bool
}

// ForeignIDPNameSkipRules — отсев этого гейта. Перечень закрыт.
var ForeignIDPNameSkipRules = []ForeignIDPNameSkipRule{
	{
		Name: "порождённые стабы контракта",
		Why: "имя в них приходит из `.proto` и переименованием НЕ правится: правка " +
			"пережила бы ровно до следующей генерации. Предмет там — контракт, и " +
			"держат его proto-полоса и край, а не этот гейт",
		Match: func(rel string) bool {
			return strings.HasSuffix(rel, ".pb.go") || strings.HasSuffix(rel, ".pb.gw.go")
		},
	},
}

// ForeignIDPNameSkipCount — сколько путей отсеяло одно правило.
type ForeignIDPNameSkipCount struct {
	Rule string
	N    int
}

// ForeignIDPNameSkipRuleFor — каким правилом отсеян путь. Пусто — не отсеян.
func ForeignIDPNameSkipRuleFor(rel string) string {
	for _, r := range ForeignIDPNameSkipRules {
		if r.Match(rel) {
			return r.Name
		}
	}
	return ""
}

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
	// Listed — сколько путей предложено обходу индексом.
	Listed int
	// Skipped — сколько из них отсеяно правилами. Печатается отдельно: без
	// этого числа «там ничего нет» неотличимо от «туда не смотрели».
	Skipped int
	// SkippedBy — отсеянное по правилам, в порядке их объявления.
	SkippedBy []ForeignIDPNameSkipCount
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

// String — единица счёта названа ПРИ КАЖДОМ числе намеренно: рядом с этим
// гейтом живёт второй замер того же предмета в СТРОКАХ, и числа у них совпадают
// случайно. «103 имени по дереву» и «103 строки в пакете» — разные предметы.
func (c ForeignIDPNameCensus) String() string {
	var by strings.Builder
	for i, s := range c.SkippedBy {
		if i > 0 {
			by.WriteString(", ")
		}
		fmt.Fprintf(&by, "%s %d", s.Rule, s.N)
	}
	if by.Len() == 0 {
		by.WriteString("правил не объявлено")
	}
	return fmt.Sprintf("перепись: путей предложено %d · отсеяно путей %d (%s) · "+
		"разобрано файлов Go %d · осмотрено объявленных ИМЁН %d · из них ИМЁН с названием "+
		"поставщика %d в %d областях · записей ведомости %d (ИМЁН объявлено %d) · находок %d",
		c.Listed, c.Skipped, by.String(), c.Files, c.Idents, c.Names, c.Areas,
		c.LedgerEntries, c.LedgerNames, c.Findings)
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
			if lw == nw {
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
		// Имя пакета — объявленное имя файла, и правится оно переименованием
		// так же, как всякое другое.
		add("пакет", file.Name)
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.FuncDecl:
				add("функция", v.Name)
			case *ast.ImportSpec:
				// Псевдоним импорта — НАШЕ имя: чужой пакет им не называется, а
				// переживёт поставщика той же ложью. Импорт без псевдонима не
				// судится: там имя приходит из чужого модуля.
				add("псевдоним импорта", v.Name)
			case *ast.RangeStmt:
				if v.Tok != token.DEFINE {
					return true
				}
				if id, ok := v.Key.(*ast.Ident); ok {
					add("переменная range", id)
				}
				if id, ok := v.Value.(*ast.Ident); ok {
					add("переменная range", id)
				}
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

// ForeignIDPNameComposition — СОСТАВ найденного по областям.
//
// Ведомость держит СУММУ, и замещение внутри области проходит молча: одно имя
// ушло, другое пришло, число не изменилось. Сумму гейт судить не перестаёт —
// состав печатается рядом, чтобы подмена была видна глазами на обзоре диффа.
func ForeignIDPNameComposition(hits []ForeignIDPNameHit) []string {
	byArea := map[string][]string{}
	for _, h := range hits {
		byArea[foreignIDPArea(h.File)] = append(byArea[foreignIDPArea(h.File)],
			fmt.Sprintf("%s:%d %s (%s)", h.File, h.Line, h.Name, h.Kind))
	}
	areas := make([]string, 0, len(byArea))
	for a := range byArea {
		areas = append(areas, a)
	}
	sort.Strings(areas)
	out := make([]string, 0, len(hits)+len(areas))
	for _, a := range areas {
		rows := byArea[a]
		sort.Strings(rows)
		out = append(out, fmt.Sprintf("── область %s: ИМЁН %d", a, len(rows)))
		out = append(out, rows...)
	}
	return out
}
