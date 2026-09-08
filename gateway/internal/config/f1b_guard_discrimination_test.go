// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_guard_discrimination_test.go — каждый страж старта отказывает СВОИМ
// отказом, и это утверждается, а не предполагается.
//
// # Почему такой файл вообще нужен
//
// Стражей у одного объявления два десятка, и они стоят подряд. Проба, которая
// подаёт негодный вход и радуется любому отказу, зеленеет, когда её страж снят,
// — потому что отказал соседний.
//
// Число здесь НЕ выписано ориентиром: сколько веток отказа в дереве, считает
// соседняя проба (`TestF1b_EveryRefusalInTheDeclarationParserIsNamedByTheTable`)
// и печатает переписью. Выписанное число разошлось бы с деревом при первом новом
// страже — ровно тем способом, каким разошлась первая редакция этого файла. Это не теория: ровно так и случилось с пробой
// вырожденного перечня, и обнаружил это ревьюер инъекцией, а не чтением. Отказ
// приходил от соседа («наш издатель вне перечня принимаемых»), чьё сообщение
// несёт ту же подстроку имени ручки.
//
// Отсюда форма. У каждого стража есть СВОЙ различитель — подстрока, которую не
// печатает больше никто, — и фикстура, на которой отказать может ТОЛЬКО он.
// Тогда снятие стража даёт либо проход (и проба краснеет), либо отказ соседа с
// чужим различителем (и проба краснеет тоже).
//
// # Чего этот файл не делает
//
// Он не проверяет, что страж отказывает ПРАВИЛЬНО — это предмет соседних проб
// с положительными контролями. Здесь предмет один: отказ принадлежит тому, кому
// приписан.
package config_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// f1bGuardCase — один страж: вход, на котором отказать может только он, и его
// собственный различитель.
type f1bGuardCase struct {
	// Name — чем этот страж является для читателя отказа.
	Name string
	// Cfg — минимальное объявление: всё, что могло бы отказать вместо предмета,
	// из него убрано намеренно.
	Cfg config.Config
	// Discriminator — подстрока, которую печатает ТОЛЬКО этот страж. Ею проба
	// утверждает, что отказ пришёл именно отсюда.
	Discriminator string
	// LiteralAnchor — подстрока, по которой этот страж находится В ИСХОДНИКЕ.
	//
	// Пусто ⇒ якорем служит сам Discriminator, и это обычный случай. Поле нужно
	// там, где различитель СОБИРАЕТСЯ ПРИ ВЫПОЛНЕНИИ и в тексте формата его
	// нет: у стража вырожденного перечня различителем служит «0 elements», а в
	// исходнике на этом месте стоит «%d elements».
	//
	// Два якоря — не послабление, а признание, что вопросы разные: «чей это
	// отказ» задаётся сообщению, «есть ли у ветки хозяин» — исходнику. Слить их
	// в один можно было бы, ослабив первый до литерала, — и тогда проба
	// перестала бы утверждать обе величины, ради которых её и чинили.
	LiteralAnchor string
	// RoutesThroughWrapper — литерал ОБЁРТКИ, через которую проходит отказ этого
	// стража, если он через обёртку проходит.
	//
	// Освобождение обёртки чужого отказа законно ровно тогда, когда за ней стоит
	// ХОТЯ БЫ ОДНА наша ветка, — и это ОБЪЯВЛЯЕТСЯ здесь, а не предполагается.
	// Обёртка, которую не назвал никто, обёрткой не является: это
	// самостоятельный страж без хозяина, и он невидим ровно по той причине, по
	// какой был невидим страж вырожденного перечня.
	RoutesThroughWrapper string
}

// literalAnchor возвращает якорь поиска в исходнике.
func (c f1bGuardCase) literalAnchor() string {
	if c.LiteralAnchor != "" {
		return c.LiteralAnchor
	}
	return c.Discriminator
}

// declarationParserFile — файл, где живёт разбор объявления приёма. Назван
// здесь, чтобы его переезд ронял пробу переписью, а не делал её беспредметной.
const declarationParserFile = "tokenissuers.go"

// declarationParserRefusalFloor — пол числа найденных отказов. Ниже него разбор
// нашёл не то, и утверждение «все названы» верно про пустое множество.
const declarationParserRefusalFloor = 15

// f1bDiscriminators — ЯКОРЯ ПОИСКА В ИСХОДНИКЕ всех рядов таблицы, одним
// источником: два списка об одном предмете разошлись бы молча.
func f1bDiscriminators() []string {
	out := make([]string, 0, len(f1bGuardCases()))
	for _, c := range f1bGuardCases() {
		out = append(out, c.literalAnchor())
	}
	return out
}

func f1bBase(env string) config.Config {
	return config.Config{AppEnv: env, APIDomain: "api.kacho.test"}
}

func TestF1b_EveryStartGuardRefusesWithItsOwnRefusal(t *testing.T) {
	cases := f1bGuardCases()
	f1bRunGuardCases(t, cases)
}

// f1bGuardCases — таблица стражей. Отдельной функцией, потому что читателей два:
// сама проба и перепись по дереву.
func f1bGuardCases() []f1bGuardCase {
	return []f1bGuardCase{
		{
			Name:          "вырожденный перечень издателей",
			Cfg:           func() config.Config { c := f1bBase("production"); c.TokenIssuers = ","; return c }(),
			Discriminator: "0 elements",
			LiteralAnchor: "declares no issuer element",
		},
		{
			Name: "издатель объявлен дважды В ПЕРЕЧНЕ",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy + "," + f1bLegacy
				return c
			}(),
			Discriminator: "one issuer, one record",
		},
		{
			// Стражей повтора ДВА — по одному на каждое объявление, — и они
			// разные: этот пропускался, пока таблица несла только соседний.
			// Обнаружено мутацией: снятие ЭТОГО стража не роняло ничего.
			Name: "издатель объявлен дважды В ПРИВЯЗКЕ ИСТОЧНИКОВ",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS + "," + f1bLegacy + "=" + f1bOursKS
				return c
			}(),
			Discriminator: "one issuer, one key-set record",
		},
		{
			Name: "издатель без объявленной записи источника",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				return c
			}(),
			Discriminator: "has no declared key-set record",
		},
		{
			Name: "запись источника без принимающего её издателя",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS + ",https://third.test=" + f1bLegKS
				return c
			}(),
			Discriminator: "outlives its subject",
		},
		{
			Name: "адрес записи не абсолютен",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=/.well-known/jwks.json"
				return c
			}(),
			Discriminator:        "is not absolute",
			RoutesThroughWrapper: "record for issuer %q: %w",
		},
		{
			Name: "адрес записи из одних разделителей",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=///"
				return c
			}(),
			Discriminator:        "consists of separators only",
			RoutesThroughWrapper: "record for issuer %q: %w",
		},
		{
			Name: "незащищённая схема адреса набора в производственной посадке",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=http://kaname-internal.kacho.svc:9097/x"
				return c
			}(),
			Discriminator: "trust anchor of signature verification",
		},
		{
			Name: "наш издатель вне перечня принимаемых",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS
				c.PlatformTokenIssuer = f1bOurs
				return c
			}(),
			Discriminator: "does not accept",
		},
		{
			Name: "наш издатель принимается без объявленного авторитета отзыва",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bOurs
				c.TokenIssuerKeySets = f1bOurs + "=" + f1bOursKS
				c.PlatformTokenIssuer = f1bOurs
				return c
			}(),
			Discriminator: "is not revocation",
		},
		{
			Name: "запись привязки не в форме «издатель=адрес»",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = "здесь-нет-знака-равенства"
				return c
			}(),
			Discriminator:        "is not «issuer=url»",
			RoutesThroughWrapper: "record for issuer %q: %w",
		},
		{
			Name: "запись привязки без издателя",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = "=" + f1bLegKS
				return c
			}(),
			Discriminator:        "names no issuer",
			RoutesThroughWrapper: "record for issuer %q: %w",
		},
		{
			Name: "адрес записи пуст",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "="
				return c
			}(),
			Discriminator:        "key-set URL is empty",
			RoutesThroughWrapper: "record for issuer %q: %w",
		},
		{
			Name: "адрес авторитета отзыва не абсолютен",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bOurs
				c.TokenIssuerKeySets = f1bOurs + "=" + f1bOursKS
				c.PlatformTokenIssuer = f1bOurs
				c.PlatformTokenRevocationURL = "тут-нет-ни-схемы-ни-узла"
				return c
			}(),
			Discriminator: "must be absolute",
		},
		{
			Name: "незащищённая схема авторитета отзыва в производственной посадке",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bOurs
				c.TokenIssuerKeySets = f1bOurs + "=" + f1bOursKS
				c.PlatformTokenIssuer = f1bOurs
				c.PlatformTokenRevocationURL = "http://kaname-internal.kacho.svc:9097/x"
				return c
			}(),
			Discriminator: "the answer decides access",
		},
		{
			Name: "адрес записи не разбирается как URL (ОБЪЯВЛЕННЫЙ путь)",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=://%%zz"
				return c
			}(),
			Discriminator:        "is not a parseable URL",
			RoutesThroughWrapper: "record for issuer %q: %w",
		},
		{
			// Страж БЕЗ ХОЗЯИНА, найденный мутационной переписью ревьюера:
			// единственная из двадцати веток, которая не держалась ничем.
			// Освобождение по обёртке на ней не работает — за ней НЕТ нашей
			// ветки, адрес разбирает библиотека, — значит это самостоятельный
			// страж, и ряд ему нужен свой.
			Name: "адрес набора не разбирается как URL на ЗАПАСНОМ пути",
			Cfg: func() config.Config {
				c := f1bBase("production")
				// Перечень НЕ объявлен: адрес приезжает прежним пином, и на этом
				// пути он не проходил проверки формы вовсе.
				c.HydraJWKSURL = "://%%zz"
				return c
			}(),
			Discriminator: "KACHO_HYDRA_JWKS_URL: key-set URL",
			// Различитель собирается ПРИ ВЫПОЛНЕНИИ (имя ручки подставляется),
			// а в исходнике на его месте шаблон — поэтому якорь поиска свой.
			LiteralAnchor: "key-set URL %q for issuer %q is not a URL",
		},
		{
			Name: "два объявления об одном предмете",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.TokenIssuers = f1bLegacy
				c.TokenIssuerKeySets = f1bLegacy + "=" + f1bLegKS
				c.HydraIssuer = f1bLegacy
				return c
			}(),
			Discriminator: "two declarations of one subject",
		},
		{
			Name: "наш издатель объявлен, а перечня нет вовсе",
			Cfg: func() config.Config {
				c := f1bBase("production")
				c.PlatformTokenIssuer = f1bOurs
				return c
			}(),
			Discriminator: "declares no issuer set",
		},
	}
}

// f1bRunGuardCases прогоняет таблицу: каждый страж обязан отказать СВОИМ отказом.
//
// Отдельной функцией, потому что читателей у таблицы двое: сама проба и
// перепись по дереву, которая сверяет ряды с ветками отказа в разборе.
func f1bRunGuardCases(t *testing.T, cases []f1bGuardCase) {
	t.Helper()
	seen := map[string]string{}
	for _, tc := range cases {
		bindings, err := tc.Cfg.TokenAcceptance()
		if err == nil {
			t.Errorf("страж «%s» не отказал (записей приёма %d) — вход, который он обязан "+
				"отвергать, прошёл до конца", tc.Name, len(bindings))
			continue
		}
		if !strings.Contains(err.Error(), tc.Discriminator) {
			t.Errorf("страж «%s»: отказ пришёл НЕ от него — искали различитель %q, получили: %v\n\n"+
				"Либо страж снят и вместо него отказал сосед, либо его сообщение перестало "+
				"нести собственный различитель. И то и другое делает пробу этого стража "+
				"неспособной упасть: она приняла бы чужой отказ за свой.",
				tc.Name, tc.Discriminator, err)
			continue
		}
		// Различитель обязан быть УНИКАЛЬНЫМ: два стража с одной подстрокой не
		// различаются, и проба одного зеленеет на отказе другого.
		if prev, dup := seen[tc.Discriminator]; dup {
			t.Errorf("различитель %q принадлежит сразу двум стражам («%s» и «%s») — "+
				"они неразличимы, и проба одного зеленеет на отказе другого",
				tc.Discriminator, prev, tc.Name)
		}
		seen[tc.Discriminator] = tc.Name
	}

	// Перепись считает РЯДЫ и различители; сколько веток отказа в дереве —
	// отдельное утверждение ниже, и без него это число говорит о самой таблице,
	// а не о предмете, ради которого она заведена.
	t.Logf("перепись таблицы: рядов %d, различителей уникальных %d", len(cases), len(seen))
	if len(cases) == 0 {
		t.Fatalf("таблица стражей пуста — «ноль находок» на ней означало бы «ноль прочитанного»")
	}
	// Диагноз ставится по ПРИЧИНЕ, а не по разнице чисел: `seen` не наполняется и
	// тогда, когда страж просто не сработал, — и прежняя редакция печатала
	// «таблица вырождена» там, где вырождена она не была. Настоящая причина
	// названа выше, своей строкой; здесь остаётся только то, о чём это число.
	if t.Failed() {
		return
	}
	if len(seen) != len(cases) {
		t.Fatalf("различителей %d при %d рядах — значит какие-то ряды делят одну подстроку "+
			"и не различаются между собой", len(seen), len(cases))
	}
}

// f1bWrapperIsClaimed отвечает, объявил ли хоть один ряд, что его отказ проходит
// через эту обёртку. Обёртка, которую не назвал никто, обёрткой не является —
// это страж без хозяина.
func f1bWrapperIsClaimed(literal string) bool {
	for _, c := range f1bGuardCases() {
		if c.RoutesThroughWrapper != "" && strings.Contains(literal, c.RoutesThroughWrapper) {
			return true
		}
	}
	return false
}

// refusalLiteral — форматная строка одного отказа, найденная В ДЕРЕВЕ.
type refusalLiteral struct {
	Line int
	Text string
}

// TestF1b_EveryRefusalInTheDeclarationParserIsNamedByTheTable — перечень
// стражей ВЫВОДИТСЯ из дерева, а не выписывается.
//
// # Предмет
//
// Таблица выше печатала переписью ДЛИНУ СОБСТВЕННОГО СПИСКА. Страж, заведённый
// завтра без ряда, ей невидим by construction — то есть «ноль находок» на ней
// неотличимо от «ноль прочитанного» ровно в том измерении, ради которого её
// заводили. Мутационная перепись это и показала: веток отказа в разборе
// объявления двадцать, рядов было двенадцать, и снятие шести не роняло ничего.
//
// Форма требования взята у соседней пробы (Ф1б-02а): перечень выводится из
// дерева, а не из памяти автора.
func TestF1b_EveryRefusalInTheDeclarationParserIsNamedByTheTable(t *testing.T) {
	src, err := os.ReadFile(declarationParserFile)
	if err != nil {
		t.Fatalf("разбор объявления не прочитан: %v", err)
	}
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, declarationParserFile, src, 0)
	if perr != nil {
		t.Fatalf("разбор объявления не разбирается: %v", perr)
	}

	var literals []refusalLiteral
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		isRefusal := (pkg.Name == "fmt" && sel.Sel.Name == "Errorf") ||
			(pkg.Name == "errors" && sel.Sel.Name == "New")
		if !isRefusal {
			return true
		}
		// Форматная строка собирается из соседних литералов конкатенацией —
		// склеиваем их все, иначе различитель, стоящий во второй строке,
		// «не найден» по причине переноса, а не по существу.
		var text string
		ast.Inspect(call.Args[0], func(m ast.Node) bool {
			if lit, ok := m.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if unq, uerr := strconv.Unquote(lit.Value); uerr == nil {
					text += unq
				}
			}
			return true
		})
		literals = append(literals, refusalLiteral{Line: fset.Position(call.Pos()).Line, Text: text})
		return true
	})

	discriminators := f1bDiscriminators()
	var uncovered []string
	wrappers := 0
	for _, lit := range literals {
		covered := false
		for _, d := range discriminators {
			if strings.Contains(lit.Text, d) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		// Обёртка чужого отказа несёт различитель ВНУТРЕННЕЙ ветки, а своего не
		// имеет и иметь не должна: она ничего не решает, только добавляет
		// координату.
		//
		// Но освобождение выдаётся не за наличие глагола оборачивания, а за
		// НАЗВАННУЮ внутреннюю ветку: обёртка законна тогда и только тогда,
		// когда хотя бы один ряд объявил, что его отказ через неё проходит.
		// Прежняя редакция освобождала по одному лишь `%w` — и пропускала ровно
		// тот случай, ради которого перепись заведена: самостоятельный страж,
		// оборачивающий отказ БИБЛИОТЕКИ, за которым нашей ветки нет вовсе.
		if strings.Contains(lit.Text, "%w") && f1bWrapperIsClaimed(lit.Text) {
			wrappers++
			continue
		}
		uncovered = append(uncovered, fmt.Sprintf("%s:%d — %q",
			declarationParserFile, lit.Line, lit.Text))
	}
	sort.Strings(uncovered)

	t.Logf("перепись дерева: отказов в разборе объявления %d, названо таблицей %d, "+
		"обёрток чужого отказа %d, не названо %d",
		len(literals), len(literals)-wrappers-len(uncovered), wrappers, len(uncovered))

	if len(literals) < declarationParserRefusalFloor {
		t.Fatalf("отказов найдено %d при пороге %d — разбор нашёл не то, и «все названы» "+
			"тогда верно про пустое множество", len(literals), declarationParserRefusalFloor)
	}
	if len(uncovered) > 0 {
		t.Fatalf("отказы, которых таблица различителей НЕ называет — %d:\n  %s\n\n"+
			"Такой отказ никому не приписан: проба, ждущая его, зеленеет на отказе соседа, "+
			"а снятие самого стража не роняет ничего. Именно так пряталась находка, из-за "+
			"которой эта таблица и заведена.\n"+
			"Исход: завести ряд с входом, на котором отказать может только этот страж, и с "+
			"его собственным различителем.",
			len(uncovered), strings.Join(uncovered, "\n  "))
	}
}
