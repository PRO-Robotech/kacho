// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_revocation_authority_test.go — край поднимается БЕЗ внешнего
// поставщика, и отзыв при этом читается У НАС.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ: возможность, объявленная и неисполнимая ни при каком входе
//
// Ручка посадки принимала `own`, а страж старта требовал на ней адрес
// ИНТРОСПЕКЦИИ ВНЕШНЕГО ПОСТАВЩИКА — безусловно, и требовал, чтобы путь адреса
// был ровно административным путём поставщика. На посадке `own` поставщика нет
// вовсе, поэтому законного значения у поля не существовало: пусто — отказ,
// наш собственный авторитет — отказ по пути. Два правила об одном поле, и
// исполнимого входа между ними нет (`api-conventions.md` §«Неисполнимая
// возможность»).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЛОСА ЗАМЕСТИЛА ТРЕБОВАНИЕ, А НЕ СНЯЛА ЕГО — И ТЕПЕРЬ ОНА ОДНА
//
// Это несущее свойство, и половина случаев ниже стоит ради него. Отзыв,
// действующий на выдаче и не действующий на предъявлении, отзывом не является
// (`security.md` §«Контроль, действующий на ВЫДАЧЕ, но не на ПРЕДЪЯВЛЕНИИ»).
// Поэтому край обязан требовать НАШЕГО авторитета — иначе снятие поставщика
// стало бы способом выключить проверку отзыва, ничего не объявляя.
//
// ЧТО ИЗМЕНИЛОСЬ ПОСЛЕ СНЯТИЯ ПОСТАВЩИКА. Половина случаев этого файла была
// ПАРАМИ «под own требуется — под external не требуется»: у каждой оси стоял
// положительный контроль на соседней полосе, и без него «отвергнуто» было бы
// неотличимо от стража, отвергающего всё. Полоса осталась одна, и такие пары
// сняты ВМЕСТЕ со своей второй половиной — не ослаблены, а стали
// беспредметными: соседней полосы, на которой требование не действует, больше
// не существует. Роль законного близнеца несёт теперь ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ
// `TestProdAcceptsAFullyConfiguredAuthority` (соседний файл) и годная фикстура
// `ourAuthorityWired`, у которой каждый случай ниже портит РОВНО ОДНУ ось.
package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Фикстура НАШЕГО авторитета отзыва: годная во всех четырёх осях. Случаи ниже
// портят РОВНО ОДНУ.
const (
	ourAuthorityURL  = tlsIntrospectURL
	ourAuthorityCA   = "/etc/api-gateway/platform-revocation-ca/ca.crt"
	ourAuthorityCert = "/etc/api-gateway/platform-revocation-identity/tls.crt"
	ourAuthorityKey  = "/etc/api-gateway/platform-revocation-identity/tls.key"
)

// ourAuthorityWired — годная полоса нашего авторитета целиком. Это
// ПОЛОЖИТЕЛЬНЫЙ БЛИЗНЕЦ всех отрицательных случаев ниже.
func ourAuthorityWired() RevocationConfig {
	return RevocationConfig{
		PlatformRevocationURL:      ourAuthorityURL,
		PlatformRevocationCAFile:   ourAuthorityCA,
		PlatformRevocationCertFile: ourAuthorityCert,
		PlatformRevocationKeyFile:  ourAuthorityKey,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось НАШЕГО авторитета: адрес, якорь доверия, клиентская пара.

// Хоп к нашему авторитету несёт предъявленный токен, поэтому открытым текстом
// он не идёт в производственном классе.
func TestOurAuthorityHopIsRefusedInPlaintext(t *testing.T) {
	cfg := ourAuthorityWired()
	cfg.PlatformRevocationURL = plainIntrospectURL
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatal("открытый хоп к нашему авторитету обязан отвергать старт")
	}
	if !strings.Contains(err.Error(), platformRevocationURLKnob+" is plaintext") {
		t.Fatalf("отказ обязан называть ручку и предмет, получено: %q", err.Error())
	}
}

// Шифрование без якоря доверия проверяет не того: рукопожатие с внутренним
// удостоверяющим центром не сходится с системными корнями.
func TestOurAuthorityHopNeedsAPinnedAnchor(t *testing.T) {
	cfg := ourAuthorityWired()
	cfg.PlatformRevocationCAFile = ""
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatal("хоп без якоря доверия обязан отвергать старт")
	}
	if !strings.Contains(err.Error(), platformRevocationCAKnob) {
		t.Fatalf("отказ обязан называть ручку якоря, получено: %q", err.Error())
	}
}

// Наш авторитет СПРАШИВАЕТ клиентский сертификат. Хоп без пары — контроль,
// отказывающий всегда и по одной и той же причине: объявлен, провязан, и не
// отказал бы ни разу по существу.
func TestOurAuthorityHopNeedsAnIdentityToPresent(t *testing.T) {
	cfg := ourAuthorityWired()
	cfg.PlatformRevocationCertFile = ""
	cfg.PlatformRevocationKeyFile = ""
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatal("хоп без клиентской пары обязан отвергать старт")
	}
	msg := err.Error()
	if !strings.Contains(msg, platformRevocationCertKnob) ||
		!strings.Contains(msg, platformRevocationKeyKnob) {
		t.Fatalf("отказ обязан называть обе ручки пары, получено: %q", msg)
	}
}

// Половина пары хуже отсутствия обеих: она выглядит настроенной.
func TestOurAuthorityHopRefusesHalfAnIdentity(t *testing.T) {
	t.Run("сертификат без ключа", func(t *testing.T) {
		cfg := ourAuthorityWired()
		cfg.PlatformRevocationKeyFile = ""
		err := validateProductionRevocationConfig("production", cfg)
		if err == nil || !strings.Contains(err.Error(), platformRevocationKeyKnob) {
			t.Fatalf("отказ обязан называть недостающую половину, получено: %v", err)
		}
	})
	t.Run("ключ без сертификата", func(t *testing.T) {
		cfg := ourAuthorityWired()
		cfg.PlatformRevocationCertFile = ""
		err := validateProductionRevocationConfig("production", cfg)
		if err == nil || !strings.Contains(err.Error(), platformRevocationCertKnob) {
			t.Fatalf("отказ обязан называть недостающую половину, получено: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// СТРАЖУ ОБЯЗАНЫ ПОКАЗАТЬ ТО, ЧТО ОН СУДИТ
//
// Проверка выше судит четыре величины. Композиционный корень, собравший
// `RevocationConfig` без них, оставил бы их нулевыми — и страж отвечал бы
// «нашего авторитета нет» ПРИ ЛЮБОЙ настройке: чарт задал бы ручки, секрет был
// бы смонтирован, а старт отвергался. Ровно этот класс уже стоил выкатки на
// соседней оси (`hop_client_wiring_test.go`), поэтому провязка утверждается
// отдельно от поведения.
//
// main() из пробы не исполним (он дозванивается до бэкендов и занимает порты),
// поэтому провязка утверждается ТАМ, ГДЕ ОНА ЖИВЁТ — в исходнике корня. Чтение
// исходника слабее исполнения и применяется намеренно ровно к тому свойству,
// которого «оно собирается» показать не может.

func TestCompositionRoot_ShowsOurRevocationAuthorityToTheGuard(t *testing.T) {
	src := compositionRoot(t)
	for _, want := range []struct{ field, source string }{
		{"PlatformRevocationURL", `cfg\.PlatformTokenRevocationURL`},
		{"PlatformRevocationCAFile", `cfg\.PlatformTokenRevocationCAFile`},
		{"PlatformRevocationCertFile", `cfg\.PlatformTokenRevocationCertFile`},
		{"PlatformRevocationKeyFile", `cfg\.PlatformTokenRevocationKeyFile`},
	} {
		re := regexp.MustCompile(`RevocationConfig\{(?s:.*?)` + want.field + `:\s*` + want.source)
		if !re.MatchString(src) {
			t.Errorf("страж не видит %s: композиционный корень обязан подать его из %s, иначе "+
				"величина остаётся нулевой и вердикт не зависит от настройки вовсе",
				want.field, want.source)
		}
	}
}

// TestCompositionRoot_ShowsNoForeignProviderAxisToTheGuard — ОТРИЦАТЕЛЬНАЯ
// сторона той же провязки, и она нужна ровно потому, что ось снималась.
//
// Страж больше не судит ни адресов чужого поставщика, ни посадки личности.
// Корень, продолжающий подавать их, подавал бы величины в поля, которых нет, —
// а корень, подающий посадку, вернул бы развилку требования молча.
func TestCompositionRoot_ShowsNoForeignProviderAxisToTheGuard(t *testing.T) {
	// Литерал берётся ЦЕЛИКОМ и по своим границам: `[^{}]*` не перешагивает
	// закрывающую скобку, тогда как нежадное `.*?` перешагивает — первая
	// редакция этой пробы так и покраснела, найдя `IntrospectionURL:` в
	// СОСЕДНЕМ литерале кеша интроспекции этажом ниже. Свой разборщик — тоже
	// распознаватель, и проверять его надо раньше, чем чужую работу.
	lit := regexp.MustCompile(`RevocationConfig\{([^{}]*)\}`).FindAllStringSubmatch(compositionRootVerbatim(t), -1)
	if len(lit) == 0 {
		t.Fatal("в композиционном корне нет ни одного литерала RevocationConfig — " +
			"предмет пробы недоступен, и её молчание сказано ни о чём")
	}
	t.Logf("перепись: литералов RevocationConfig в корне — %d", len(lit))
	for _, m := range lit {
		for _, gone := range []string{
			"IdentityProvider:", "IntrospectionURL:", "AdminURL:", "AdminCAFile:",
		} {
			if strings.Contains(m[1], gone) {
				t.Errorf("композиционный корень подаёт стражу снятую величину %q — "+
					"ось чужого поставщика вернулась в судимое без решения", gone)
			}
		}
	}
}

// TestCompositionRoot_ForeignAxisPredicateCanFail — САМОПРОВЕРКА предиката выше
// на СИНТЕТИКЕ.
//
// Предикат, не краснеющий на дефекте, не удерживает ничего, а опирайся
// самопроверка на живой корень — доказательство исчезло бы вместе с починкой,
// то есть ровно тогда, когда предикат достиг цели.
//
// Утверждаются ОБЕ стороны: снятая величина ВНУТРИ литерала — находка; та же
// строка в СОСЕДНЕМ литерале — молчание. Вторая половина и есть тот дефект,
// который первая редакция пробы допустила.
func TestCompositionRoot_ForeignAxisPredicateCanFail(t *testing.T) {
	re := regexp.MustCompile(`RevocationConfig\{([^{}]*)\}`)

	inside := "x := RevocationConfig{\n\tIntrospectionURL: cfg.Whatever,\n}\n"
	m := re.FindAllStringSubmatch(inside, -1)
	if len(m) != 1 || !strings.Contains(m[0][1], "IntrospectionURL:") {
		t.Fatalf("дефект внутри литерала не найден — предикат не краснеет на своём предмете: %v", m)
	}

	neighbour := "x := RevocationConfig{\n\tPlatformRevocationURL: cfg.A,\n}\n" +
		"y := IntrospectionCacheConfig{\n\tIntrospectionURL: cfg.B,\n}\n"
	m = re.FindAllStringSubmatch(neighbour, -1)
	if len(m) != 1 {
		t.Fatalf("литералов найдено %d, ожидался 1 — разбор захватил соседа", len(m))
	}
	if strings.Contains(m[0][1], "IntrospectionURL:") {
		t.Fatal("предикат захватил СОСЕДНИЙ литерал: законный близнец объявлен находкой")
	}
}

// TestCompositionRoot_BootLogNamesTheBudgetItApplies — УТВЕРЖДЕНИЕ О ЗАЩИТЕ НЕ
// ПЕРЕЖИВАЕТ СВОЙ ПРЕДМЕТ (C2).
//
// Журнал старта объявлял оператору `per_call_timeout_ms` из ручки
// `KACHO_INTROSPECTION_TIMEOUT_MS`. Предел, который эта ручка задавала, ушёл
// вместе со второй половиной читателя отзыва (кешем интроспекции чужого
// поставщика), а строка осталась — и стоит ровно там, куда оператор смотрит,
// проверяя, что бюджет задан.
//
// Исходов у такой строки два: применить бюджет и сделать утверждение истинным
// либо снять поле. Взят первый, и величина берётся ИЗ ТОГО ЖЕ объявления, что
// применяется на вызове: второй копии числа не заводится, разойтись нечему.
//
// Судится ДОСТИЖИМОЕ от `main()`, а не текст файла: строка журнала в функции,
// в которую корень не заходит, оператору не печатается.
func TestCompositionRoot_BootLogNamesTheBudgetItApplies(t *testing.T) {
	const (
		line  = "revocation check active on the authN path"
		field = "per_call_timeout_ms"
		want  = "middleware.OwnRevocationCallBudget.Milliseconds()"
	)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, compositionRootLabel, compositionRoot(t), parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("достижимый код корня не разбирается: %v", err)
	}

	// Судится ВЫРАЖЕНИЕ, поданное журналу, а не строка вокруг него. Своё
	// выражение проверяется раньше чужой работы: первая редакция этого случая
	// брала значение регуляркой `[^,\n)]+` и обрывала его на скобке внутри
	// `Milliseconds()` — то есть сравнивала одно и то же с самим собой и
	// краснела.
	var got string
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		first, isLit := call.Args[0].(*ast.BasicLit)
		if !isLit || first.Kind != token.STRING || first.Value != strconv.Quote(line) {
			return true
		}
		found = true
		for i := 1; i+1 < len(call.Args); i++ {
			key, isKey := call.Args[i].(*ast.BasicLit)
			if !isKey || key.Kind != token.STRING || key.Value != strconv.Quote(field) {
				continue
			}
			var buf bytes.Buffer
			if perr := printer.Fprint(&buf, fset, call.Args[i+1]); perr != nil {
				t.Fatalf("печать поданного значения: %v", perr)
			}
			got = buf.String()
		}
		return true
	})

	if !found {
		t.Fatal("в достижимом коде корня нет строки журнала о читателе отзыва — " +
			"предмет утверждения исчез, и это НЕ то же самое, что «утверждение истинно»")
	}
	if got == "" {
		t.Fatalf("строка журнала не объявляет %s — если поле снято намеренно, снимай "+
			"вместе с ним и этот случай: утверждения без предмета переживают свой "+
			"предмет ровно так же, как поля", field)
	}
	if got != want {
		t.Errorf("журнал старта объявляет оператору бюджет из %s, а на вызове применяется %s.\n\n"+
			"Величина, которую печатают, обязана быть той, которую применяют: иначе "+
			"оператор читает подтверждение защиты, которой на этой полосе нет.", got, want)
	}
}
