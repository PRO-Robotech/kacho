// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_minting_census_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// TestEveryStackEitherMintsItsOwnTokensOrIsNamedWithAReason СПОСОБЕН упасть, и
// упасть ровно на своём предмете.
//
// ПРОГОНОВ ТРИ, И ТРЕТИЙ ОБЯЗАТЕЛЕН:
//
//	(A) КОНТРОЛЬ     — согласованный стенд: тело МОЛЧИТ. Без него красное ниже
//	                   могло бы приходить от чего угодно.
//	(B) НОВОЕ        — по одной оси на прогон, каждая ОДНОФАКТНО против контроля.
//	(C) СУЩЕСТВУЮЩЕЕ — разбор перечня через запятую (элементы, а не длина строки)
//	                   и самоистечение ведомости. Без этого прогона молчание
//	                   существующего контроля неотличимо от молчания мёртвого.
//
// ЗАКОННЫЙ БЛИЗНЕЦ на каждой оси — та же форма входа без дефекта: стенд, чеканки
// НЕ объявляющий, названный в ведомости и стоящий на внешнем поставщике (законен:
// у публикатора остаётся запись зеркала); перечень принимаемых из НЕСКОЛЬКИХ
// издателей с пробелами (законен: таков каждый чеканящий стенд); потребитель, не
// объявляющий издателя платформы там, где чеканки нет.
//
// ПОСАДКА И ПЕРЕКЛЮЧАТЕЛИ — В КАЖДОЙ ЗАКОННОЙ ФОРМЕ ЗАПИСИ, и в обе стороны:
// объявлено цепочкой, унаследовано у подчарта, удалено `null`; bool, строка,
// число, двусмысленная строка. Формы подаются ТЕКСТОМ YAML через тот же
// разборщик, что читает профили, а не литералом Go: литерал показал бы, как гейт
// судит значение, которого разборщик не производит.
//
// ВХОД СИНТЕТИЧЕСКИЙ НАМЕРЕННО: настоящее дерево несёт одно состояние —
// согласованное, — а предмет здесь расхождения.
package deploy_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// agreeingStack — согласованный стенд: чеканит, и оба потребителя принимают
// ровно нашего издателя тремя согласованными способами. Форма взята с дерева.
func agreeingStack() map[string]any {
	acceptance := func() map[string]any {
		return map[string]any{
			"platformIssuer": "https://kaname.example.test",
			// Перечень из ДВУХ издателей с пробелом после запятой — законная форма,
			// и она же законный близнец оси разбора перечня.
			"issuers": "https://kaname.example.test, https://provider.example.test",
			"issuerKeySets": "https://kaname.example.test=https://kaname-internal:9097/own.json," +
				"https://provider.example.test=https://kaname-internal:9097/mirror.json",
		}
	}
	return map[string]any{
		"kaname": map[string]any{
			"config": map[string]any{
				"authn": map[string]any{
					"tokenSigning": map[string]any{
						"enabled":           true,
						"issuer":            "https://kaname.example.test",
						"algorithm":         "ES256",
						"allowedAlgorithms": "ES256,RS256",
					},
					"presentedCredential": map[string]any{"enabled": true},
					"clientToken":         map[string]any{"enabled": true},
					"identityProvider":    "own",
				},
			},
			// Слушатель набора ключей сверяет вызывающего: форма боевого слоя.
			"mtls": map[string]any{
				"enable":                  true,
				"httpListeners":           false,
				"jwksProxy":               true,
				"jwksProxyClientAuthMode": "optional-mutual",
			},
		},
		"api-gateway": map[string]any{"tokenAcceptance": acceptance()},
		"registry":    map[string]any{"tokenAcceptance": acceptance()},
	}
}

// silentStack — стенд, чеканки НЕ объявляющий: ни ручек чеканки, ни издателя
// платформы у потребителей. Законен ТОЛЬКО на внешнем поставщике: там у
// публикатора остаётся запись зеркала чужого набора.
func silentStack() map[string]any {
	return map[string]any{
		"kaname": map[string]any{"config": map[string]any{"authn": map[string]any{
			"identityProvider": "external",
		}}},
		"api-gateway": map[string]any{},
		"registry":    map[string]any{},
	}
}

// parseYAML — вход инъекции ТЕКСТОМ, через тот же разборщик, что читает профили.
func parseYAML(t *testing.T, text string) map[string]any {
	t.Helper()
	var tree map[string]any
	if err := yaml.Unmarshal([]byte(text), &tree); err != nil {
		t.Fatalf("вход инъекции не разобран: %v\n%s", err, text)
	}
	return tree
}

// hasFinding — есть ли находка, называющая want.
func hasFinding(findings []string, want string) bool {
	for _, f := range findings {
		if strings.Contains(f, want) {
			return true
		}
	}
	return false
}

// refusesToStart — формулировка находок, предсказывающих отказ старта службы.
const refusesToStart = "служба откажет в старте"

// at — доступ к вложенной карте синтетического стенда для однофактной правки.
func at(tree map[string]any, path ...string) map[string]any {
	cur := tree
	for _, k := range path {
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	return cur
}

func emptyRegister() map[string]string { return map[string]string{} }

// ── (A) КОНТРОЛЬ ────────────────────────────────────────────────────────────

func TestMintingCensusInjection_ControlIsSilent(t *testing.T) {
	got := judgeStackMinting("synthetic", agreeingStack(), emptyRegister())
	if !got.on {
		t.Fatalf("контроль: согласованный стенд обязан читаться как чеканящий")
	}
	if len(got.findings) != 0 {
		t.Fatalf("контроль: на согласованном стенде тело обязано молчать, а оно назвало %d: %v",
			len(got.findings), got.findings)
	}
	t.Log("контроль: согласованный стенд — 0 находок; красное ниже приходит от инъекции")
}

// Законный близнец: стенд БЕЗ чеканки, названный в ведомости, и потребители
// издателя платформы не объявляют. Тело обязано молчать.
func TestMintingCensusInjection_SilentStackNamedInRegisterIsLegal(t *testing.T) {
	got := judgeStackMinting("dev-like", silentStack(),
		map[string]string{"dev-like": "первая фаза двухфазного подъёма"})
	if got.on {
		t.Fatal("законный близнец: стенд без ручек чеканки не вправе читаться как чеканящий")
	}
	if len(got.findings) != 0 {
		t.Fatalf("законный близнец: названный в ведомости стенд без чеканки находкой не "+
			"является, а тело назвало %d: %v", len(got.findings), got.findings)
	}
}

// ── (B) НОВОЕ свойство, по одной оси на прогон ───────────────────────────────

func TestMintingCensusInjection_MintingOnWithoutIssuerIsAFinding(t *testing.T) {
	tree := agreeingStack()
	at(tree, "kaname", "config", "authn", "tokenSigning")["issuer"] = ""

	got := judgeStackMinting("synthetic", tree, emptyRegister())
	assertNamed(t, got.findings, "издатель")
}

func TestMintingCensusInjection_MintingOnWithoutAlgorithmIsAFinding(t *testing.T) {
	tree := agreeingStack()
	at(tree, "kaname", "config", "authn", "tokenSigning")["allowedAlgorithms"] = ""

	got := judgeStackMinting("synthetic", tree, emptyRegister())
	assertNamed(t, got.findings, "допустимые подписи приёма")
}

func TestMintingCensusInjection_ConsumerNamingAnotherPlatformIssuerIsAFinding(t *testing.T) {
	tree := agreeingStack()
	// РОВНО ОДИН факт: край называет издателем платформы чужое имя. Именно так
	// расходились стороны полосы в #1184 — и обе были зелёными по отдельности.
	at(tree, "api-gateway", "tokenAcceptance")["platformIssuer"] = "https://someone.else.test"

	got := judgeStackMinting("synthetic", tree, emptyRegister())
	assertNamed(t, got.findings, "край")
	assertNamed(t, got.findings, "https://someone.else.test")
}

func TestMintingCensusInjection_IssuerAbsentFromAcceptedListIsAFinding(t *testing.T) {
	tree := agreeingStack()
	at(tree, "registry", "tokenAcceptance")["issuers"] = "https://provider.example.test"

	got := judgeStackMinting("synthetic", tree, emptyRegister())
	assertNamed(t, got.findings, "реестр")
	assertNamed(t, got.findings, "перечне принимаемых")
}

func TestMintingCensusInjection_MissingKeySetEntryIsAFinding(t *testing.T) {
	tree := agreeingStack()
	at(tree, "api-gateway", "tokenAcceptance")["issuerKeySets"] =
		"https://provider.example.test=https://kaname-internal:9097/mirror.json"

	got := judgeStackMinting("synthetic", tree, emptyRegister())
	assertNamed(t, got.findings, "набор ключей")
}

func TestMintingCensusInjection_ConsumerWithoutAcceptanceAtAllIsAFinding(t *testing.T) {
	tree := agreeingStack()
	delete(tree, "registry")

	got := judgeStackMinting("synthetic", tree, emptyRegister())
	assertNamed(t, got.findings, "перечня принимаемых издателей не объявляет")
}

func TestMintingCensusInjection_UnnamedSilentStackIsAFinding(t *testing.T) {
	got := judgeStackMinting("nameless", silentStack(), emptyRegister())
	assertNamed(t, got.findings, "НЕ НАЗВАН в ведомости")
}

func TestMintingCensusInjection_AcceptedIssuerNobodyMintsIsAFinding(t *testing.T) {
	tree := silentStack()
	// Чеканки нет, а край объявляет издателя платформы: принимается издатель,
	// которого никто не выпускает.
	at(tree, "api-gateway", "tokenAcceptance")["platformIssuer"] = "https://kaname.example.test"

	got := judgeStackMinting("dev-like", tree,
		map[string]string{"dev-like": "первая фаза двухфазного подъёма"})
	assertNamed(t, got.findings, "которого никто не выпускает")
}

// Возможность, ДЕРЖАЩАЯСЯ чеканкой, включена без неё — половина пары в чистом
// виде: арендатор предъявляет годное удостоверение, а реестра ключей, которым
// проверяют подпись, не существует вовсе.
func TestMintingCensusInjection_DependentWithoutMintingIsAFinding(t *testing.T) {
	tree := silentStack()
	at(tree, "kaname", "config", "authn", "presentedCredential")["enabled"] = true

	got := judgeStackMinting("dev-like", tree,
		map[string]string{"dev-like": "первая фаза двухфазного подъёма"})
	assertNamed(t, got.findings, "приём предъявленного удостоверения")
	assertNamed(t, got.findings, "без своей чеканки")
}

// Тот же вход, но чеканка ВКЛЮЧЕНА — законный близнец: зависимая возможность при
// живой чеканке находкой не является.
func TestMintingCensusInjection_DependentWithMintingIsLegal(t *testing.T) {
	got := judgeStackMinting("synthetic", agreeingStack(), emptyRegister())
	for _, f := range got.findings {
		if strings.Contains(f, "без своей чеканки") {
			t.Fatalf("законный близнец: зависимая возможность при включённой чеканке "+
				"находкой не является, а тело назвало: %q", f)
		}
	}
}

func TestMintingCensusInjection_ExcusedStackThatMintsIsAFinding(t *testing.T) {
	got := judgeStackMinting("synthetic", agreeingStack(),
		map[string]string{"synthetic": "причина, у которой больше нет предмета"})
	assertNamed(t, got.findings, "прощение выдано тому, кого теперь судят")
}

// ── (B1) ПОСАДКА own БЕЗ ЧЕКАНКИ: служба не поднимается, прощения нет ─────────

// Ровно то, что положило подъём всех стендов конвейера: посадка `own`, чеканка
// выключена, стенд назван в ведомости. Запись ведомости стенда не поднимает.
func TestMintingCensusInjection_OwnPostureWithoutMintingIsAFindingEvenWhenNamed(t *testing.T) {
	tree := silentStack()
	at(tree, "kaname", "config", "authn")["identityProvider"] = "own"

	got := judgeStackMinting("dev-like", tree,
		map[string]string{"dev-like": "первая фаза двухфазного подъёма"})
	if !got.own {
		t.Fatal("посадка `own`, объявленная цепочкой, обязана читаться как own")
	}
	assertNamed(t, got.findings, refusesToStart)
	assertNamed(t, got.findings, "запись ведомости этого НЕ прощает")
}

// Законный близнец: тот же стенд на внешнем поставщике — у публикатора остаётся
// запись зеркала, и о старте находки нет.
func TestMintingCensusInjection_ExternalPostureWithoutMintingDoesNotRefuse(t *testing.T) {
	got := judgeStackMinting("dev-like", silentStack(),
		map[string]string{"dev-like": "первая фаза двухфазного подъёма"})
	if got.own || hasFinding(got.findings, refusesToStart) {
		t.Fatalf("законный близнец: внешний поставщик без чеканки старт не роняет, а тело "+
			"назвало: %v", got.findings)
	}
}

// Посадка в каждой законной форме записи — и в обе стороны. Цепочка может
// объявить её сама, унаследовать у подчарта или удалить унаследованное `null`.
func TestMintingCensusInjection_PostureIsReadInEveryLegalForm(t *testing.T) {
	const external = "config:\n  authn:\n    identityProvider: external\n"
	const own = "config:\n  authn:\n    identityProvider: own\n"
	cases := []struct {
		name       string
		chain      string // объявления цепочки под `kaname:`
		subchart   string // умолчания подчарта
		wantOwn    bool
		wantRefuse bool
	}{
		{"объявлено цепочкой", own, external, true, true},
		{"объявлено в кавычках", "config:\n  authn:\n    identityProvider: \"own\"\n", external, true, true},
		{"унаследовано у подчарта", "config:\n  authn: {}\n", own, true, true},
		{"унаследованное внешнее", "config:\n  authn: {}\n", external, false, false},
		// null УДАЛЯЕТ ключ умолчания: служба посадки не видит и публикует
		// зеркало — записи у публикатора есть. Незаявленная посадка — предмет
		// гейтов посадки, а не этого.
		{"удалено null", "config:\n  authn:\n    identityProvider: null\n", own, false, false},
		// Разборщик службы дословен: это не own, и служба отказывает на разборе —
		// раньше публикатора и по предмету гейтов посадки.
		{"иной регистр", "config:\n  authn:\n    identityProvider: Own\n", external, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chain := map[string]any{"kaname": parseYAML(t, c.chain)}
			tree := coalesceSubchartDefaults(chain, "kaname", parseYAML(t, c.subchart))
			got := judgeStackMinting("synthetic", tree, map[string]string{"synthetic": "причина"})
			if got.own != c.wantOwn {
				t.Fatalf("посадка own прочитана как %v, ожидалось %v", got.own, c.wantOwn)
			}
			if r := hasFinding(got.findings, refusesToStart); r != c.wantRefuse {
				t.Fatalf("находка об отказе старта: есть=%v, ожидалось %v: %v",
					r, c.wantRefuse, got.findings)
			}
		})
	}
}

// Умолчания подчарта не меняются слиянием: они одни на все стенды, и стенд,
// объявивший своё, не вправе перекрасить соседа.
func TestMintingCensusInjection_CoalescingLeavesTheDefaultsUntouched(t *testing.T) {
	defaults := parseYAML(t, "config:\n  authn:\n    identityProvider: external\n")
	chain := map[string]any{"kaname": parseYAML(t, "config:\n  authn:\n    identityProvider: own\n")}
	_ = coalesceSubchartDefaults(chain, "kaname", defaults)
	if got := asString(lookupAny(defaults, "config", "authn", "identityProvider")); got != "external" {
		t.Fatalf("слияние изменило умолчания подчарта: %q", got)
	}
}

// ── (B2) ПЕРЕКЛЮЧАТЕЛЬ — как его прочтёт шаблон чарта ─────────────────────────

func TestMintingCensusInjection_SwitchIsReadAsTheChartReadsIt(t *testing.T) {
	cases := []struct {
		name    string
		value   string // значение `enabled:` текстом YAML
		wantOn  bool
		wantAmb bool
	}{
		{"bool true", "true", true, false},
		{"bool false", "false", false, false},
		{"yes без кавычек (YAML 1.1 — true)", "yes", true, false},
		{"строка в кавычках true", "\"true\"", true, false},
		{"число 1", "1", true, false},
		{"число 0", "0", false, false},
		{"пустая строка", "\"\"", false, false},
		{"null", "null", false, false},
		{"no — двусмысленно", "no", false, true},
		{"off в кавычках — двусмысленно", "\"off\"", false, true},
		{"false в кавычках — шаблон включает", "\"false\"", true, true},
		{"nO — не ложь YAML 1.1", "nO", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree := parseYAML(t, "enabled: "+c.value+"\n")
			on, problem := chartSwitch(tree["enabled"])
			if on != c.wantOn {
				t.Fatalf("включён=%v, ожидалось %v (разобрано как %T %v)",
					on, c.wantOn, tree["enabled"], tree["enabled"])
			}
			if (problem != "") != c.wantAmb {
				t.Fatalf("находка о форме: %q, ожидалась=%v", problem, c.wantAmb)
			}
		})
	}
}

// Чеканка, включённая строкой, — чеканка: посадка own на ней старт не роняет.
// Обратная сторона: ноль — выключено, и тогда роняет.
func TestMintingCensusInjection_MintingSwitchFormDecidesTheOwnAxis(t *testing.T) {
	for _, c := range []struct {
		value      any
		wantRefuse bool
	}{{"true", false}, {"yes", false}, {1, false}, {0, true}, {false, true}} {
		tree := agreeingStack()
		at(tree, "kaname", "config", "authn", "tokenSigning")["enabled"] = c.value
		got := judgeStackMinting("synthetic", tree, emptyRegister())
		if r := hasFinding(got.findings, refusesToStart); r != c.wantRefuse {
			t.Fatalf("enabled=%#v: находка об отказе старта есть=%v, ожидалось %v: %v",
				c.value, r, c.wantRefuse, got.findings)
		}
	}
}

// ── (B3) СЛУШАТЕЛЬ НАБОРА КЛЮЧЕЙ обязан сверять вызывающего ──────────────────

func TestMintingCensusInjection_ListenerThatDoesNotVerifyTheCallerIsAFinding(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m map[string]any)
		want   string
	}{
		{"режим не задан — умолчание шаблона одностороннее",
			func(m map[string]any) { delete(m, "jwksProxyClientAuthMode") }, "server-tls-only"},
		{"режим односторонний явно",
			func(m map[string]any) { m["jwksProxyClientAuthMode"] = "server-tls-only" }, "server-tls-only"},
		{"полистенная ручка false перекрывает общую true",
			func(m map[string]any) { m["jwksProxy"] = false; m["httpListeners"] = true }, "kaname.mtls.jwksProxy"},
		{"полистенной нет, общая выключена",
			func(m map[string]any) { delete(m, "jwksProxy") }, "kaname.mtls.httpListeners"},
		{"TLS службы выключен целиком",
			func(m map[string]any) { m["enable"] = false }, "kaname.mtls.enable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree := agreeingStack()
			c.mutate(at(tree, "kaname", "mtls"))
			got := judgeStackMinting("synthetic", tree, emptyRegister())
			assertNamed(t, got.findings, "вызывающего не сверяет")
			assertNamed(t, got.findings, c.want)
		})
	}
}

// Законные близнецы: взаимный режим; полистенной ручки нет, а общая включена;
// полистенная включена строкой.
func TestMintingCensusInjection_ListenerVerifyingTheCallerIsLegal(t *testing.T) {
	for name, mutate := range map[string]func(m map[string]any){
		"взаимный":                 func(m map[string]any) { m["jwksProxyClientAuthMode"] = "mutual" },
		"общая ручка включена":     func(m map[string]any) { delete(m, "jwksProxy"); m["httpListeners"] = true },
		"строка yes у полистенной": func(m map[string]any) { m["jwksProxy"] = "yes" },
	} {
		tree := agreeingStack()
		mutate(at(tree, "kaname", "mtls"))
		got := judgeStackMinting("synthetic", tree, emptyRegister())
		if len(got.findings) != 0 {
			t.Fatalf("законный близнец %q: находок быть не должно, а тело назвало %d: %v",
				name, len(got.findings), got.findings)
		}
	}
}

// ── (C) СУЩЕСТВУЮЩЕЕ ────────────────────────────────────────────────────────

// Разбор перечня считает ЭЛЕМЕНТЫ, а не длину строки. Одинокая запятая непуста
// по длине и пуста по существу — канонический вход этого класса.
func TestMintingCensusInjection_DegenerateListIsNotMembership(t *testing.T) {
	if commaListHas(",", "https://kaname.example.test") {
		t.Fatal("одинокая запятая обязана НЕ давать членства: перечень, признающий её, " +
			"объявляет принимаемым кого угодно")
	}
	if commaListHas("", "https://kaname.example.test") {
		t.Fatal("пустой перечень обязан НЕ давать членства")
	}
	// Обратная сторона: пробелы вокруг элемента членства НЕ отменяют — иначе гейт
	// краснел бы на законной, читаемой записи перечня.
	if !commaListHas(" https://kaname.example.test , https://other.test ", "https://kaname.example.test") {
		t.Fatal("пробелы вокруг элемента отменять членство не вправе")
	}
	// То же у записи «издатель → набор»: ключ без значения записью не является.
	if keySetsName("https://kaname.example.test", "https://kaname.example.test") {
		t.Fatal("ключ без адреса набора записью не является: проверять подпись всё равно нечем")
	}
}

// Ведомость истекает сама: запись про стенд, которого таблица состава не
// объявляет, — находка. Обратная сторона: объявленный стенд прощения не теряет.
func TestMintingCensusInjection_RegisterExpiresOnItsOwn(t *testing.T) {
	declared := map[string]bool{"dev": true, "prod": true}

	if got := judgeRegisterExpiry(map[string]string{"dev": "причина"}, declared); len(got) != 0 {
		t.Fatalf("законный близнец: запись про ОБЪЯВЛЕННЫЙ стенд находкой не является, "+
			"а получено %d: %v", len(got), got)
	}
	got := judgeRegisterExpiry(map[string]string{"stand-long-gone": "причина"}, declared)
	if len(got) != 1 {
		t.Fatalf("запись про необъявленный стенд обязана дать РОВНО одну находку, получено %d: %v",
			len(got), got)
	}
	if !strings.Contains(got[0], "stand-long-gone") {
		t.Fatalf("находка не называет стенд: %q", got[0])
	}
}

// assertNamed — находка обязана существовать И называть координату: на находку,
// называющую симптом, тратят прогон, а потом снимают гейт как непонятный.
func assertNamed(t *testing.T, findings []string, want string) {
	t.Helper()
	if len(findings) == 0 {
		t.Fatalf("инъекция не дала ни одной находки — тело гейта не судит этой оси, "+
			"и его молчание на дереве ничего не доказывает (ожидалось упоминание %q)", want)
	}
	for _, f := range findings {
		if strings.Contains(f, want) {
			return
		}
	}
	t.Fatalf("находки есть (%d), но ни одна не называет %q: %v", len(findings), want, findings)
}
