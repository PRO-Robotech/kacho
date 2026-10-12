// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_issuer_origin_test.go — издатель службы личности управляемого стенда
// стоит на ТОМ ЖЕ происхождении, что консоль стенда (kacho#3113).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Документ обнаружения сервера авторизации (RFC 8414) служба выводит из ОДНОЙ
// величины — издателя своей чеканки: issuer, authorization_endpoint и
// token_endpoint стоят на его происхождении. Отдельной ручки «публичный базовый
// адрес» у службы нет. Цепочка, чья консоль приходит слоем площадки, но чей
// издатель унаследован у слоя разработки, публикует снаружи адреса имени,
// которое вне кластера не разрешается: обнаружение отвечает, а каждая точка,
// которую оно называет, ведёт в никуда — и токены несут чужой `iss`.
//
// Происхождение консоли такой цепочки в дереве — заглушка (RFC 2606), настоящее
// приходит слоем площадки вне git. Поэтому проба судит ФОРМУ, а не домен:
// издатель объявлен заглушкой ТОГО ЖЕ происхождения, что консоль, во всех семи
// местах, где его называют чеканка и оба потребителя. Тогда слой площадки
// заменяет обе величины, а страж слоя (templates/site-layer-guard.yaml) не
// пускает к кластеру ни оставленную заглушку издателя, ни разошедшиеся копии.
//
// Издатель ОБЪЯВЛЯЕТСЯ, а не выводится шаблоном из адреса консоли: адрес,
// производный от адреса соседа, — класс hard-no-derived-security-address.
//
// ─────────────────────────────────────────────────────────────────────────────
// КОГО СУДИТ
//
// Цепочки deploy/stacks.txt, у которых происхождение консоли
// (global.kacho.identity.appBaseURL) объявлено заглушкой площадки. Цепочка с
// настоящим именем разработки (console.kacho.local) — законный близнец: её
// издатель — имя разработки, и проба о ней молчит; цепочка без объявленного
// происхождения не судится вовсе (консоль её не знает, сравнивать не с чем).
//
// Читает ОБЪЯВЛЕНИЯ, а не рендер (hard-declarative-values-test): свёртка цепочки
// — resolveStack/mergeInto этого пакета, ровно как helm складывает `-f`.
package umbrella_test

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// placeholderHost — заглушка площадки: то же правило, что у стража слоя
// площадки (templates/site-layer-guard.yaml, `$placeholderHost`).
var placeholderHost = regexp.MustCompile(`(?i)^(.*\.)?(invalid|example|example\.com|example\.net|example\.org)\.?$`)

// issuerPlace — одно место, где цепочка называет издателя платформы.
type issuerPlace struct {
	name string
	path []string
	// kind: "value" — значение равно издателю; "list" — перечень через запятую
	// содержит издателя; "keyset" — перечень `издатель=адрес` называет издателя.
	kind string
}

// issuerPlaces — семь мест: чеканка и по три у каждого потребителя. Тот же
// перечень согласованности судит deploy/own_minting_census_test.go — но между
// собой; здесь — против происхождения консоли.
var issuerPlaces = []issuerPlace{
	{"kaname.config.authn.tokenSigning.issuer", []string{"kaname", "config", "authn", "tokenSigning", "issuer"}, "value"},
	{"api-gateway.tokenAcceptance.platformIssuer", []string{"api-gateway", "tokenAcceptance", "platformIssuer"}, "value"},
	{"api-gateway.tokenAcceptance.issuers", []string{"api-gateway", "tokenAcceptance", "issuers"}, "list"},
	{"api-gateway.tokenAcceptance.issuerKeySets", []string{"api-gateway", "tokenAcceptance", "issuerKeySets"}, "keyset"},
	{"registry.tokenAcceptance.platformIssuer", []string{"registry", "tokenAcceptance", "platformIssuer"}, "value"},
	{"registry.tokenAcceptance.issuers", []string{"registry", "tokenAcceptance", "issuers"}, "list"},
	{"registry.tokenAcceptance.issuerKeySets", []string{"registry", "tokenAcceptance", "issuerKeySets"}, "keyset"},
}

// issuerOriginVerdict — исход по одной цепочке.
type issuerOriginVerdict struct {
	judged   bool // происхождение консоли — заглушка площадки
	places   int
	agreeing int
	findings []string
}

// consoleOrigin — происхождение консоли (scheme://host[:port]) из объявления;
// ok=false, если объявления нет.
func consoleOrigin(merged map[string]any) (origin, host string, ok bool) {
	v, _ := lookup(merged, "global", "kacho", "identity", "appBaseURL")
	s, _ := v.(string)
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return s, "", true
	}
	return u.Scheme + "://" + u.Host, u.Hostname(), true
}

// judgeIssuerOrigin — ТЕЛО пробы, отдельно от обхода: инъекции зовут его же.
func judgeIssuerOrigin(stack string, merged map[string]any) issuerOriginVerdict {
	origin, host, declared := consoleOrigin(merged)
	if !declared || !placeholderHost.MatchString(host) {
		return issuerOriginVerdict{}
	}
	v := issuerOriginVerdict{judged: true}
	for _, p := range issuerPlaces {
		v.places++
		raw, _ := lookup(merged, p.path...)
		got, _ := raw.(string)
		got = strings.TrimSpace(got)
		var agree bool
		switch p.kind {
		case "value":
			agree = got == origin
		case "list":
			agree = got != "" && containsItem(strings.Split(got, ","), origin)
		case "keyset":
			agree = got != "" && keySetNames(got, origin)
		}
		if agree {
			v.agreeing++
			continue
		}
		shown := got
		if shown == "" {
			shown = "<не объявлено>"
		}
		v.findings = append(v.findings, fmt.Sprintf(
			"%s: %s = %q не называет издателем происхождение консоли стенда %q — обнаружение (RFC 8414) "+
				"публиковало бы адреса чужого имени, а потребитель судил бы `iss` другого издателя",
			stack, p.name, shown, origin))
	}
	return v
}

func containsItem(items []string, want string) bool {
	for _, it := range items {
		if strings.TrimSpace(it) == want {
			return true
		}
	}
	return false
}

func keySetNames(sets, want string) bool {
	for _, entry := range strings.Split(sets, ",") {
		name, _, found := strings.Cut(strings.TrimSpace(entry), "=")
		if found && strings.TrimSpace(name) == want {
			return true
		}
	}
	return false
}

func foldStack(t *testing.T, chain []string, edit func(profile string, tree map[string]any)) map[string]any {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range chain {
		tree := umbrellaValues(t, profile)
		if edit != nil {
			edit(profile, tree)
		}
		merged = mergeInto(merged, tree)
	}
	return merged
}

// Издатель цепочки с консолью площадки стоит на происхождении консоли во всех
// семи местах.
func TestManagedStandIssuerSitsOnTheConsoleOrigin(t *testing.T) {
	stacks := deployableStacks(t)
	judged, places, agreeing := 0, 0, 0
	var findings []string
	for _, name := range sortedStackNames(stacks) {
		v := judgeIssuerOrigin(name, foldStack(t, stacks[name], nil))
		if !v.judged {
			continue
		}
		judged++
		places += v.places
		agreeing += v.agreeing
		findings = append(findings, v.findings...)
	}
	t.Logf("перепись: цепочек %d · с консолью площадки (судимых) %d · мест издателя %d · совпавших с консолью %d",
		len(stacks), judged, places, agreeing)
	if judged == 0 {
		t.Fatal("ни одна цепочка не объявляет консоль заглушкой площадки — проба судила бы пустоту; " +
			"если управляемых стендов больше нет, снимите пробу вместе с ними")
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИИ — настоящим входом из дерева: цепочка a8f60d как она лежит, с
// одной правкой; и законные близнецы.

const managedStack = "a8f60d"

func managedChain(t *testing.T) []string {
	t.Helper()
	chain, ok := deployableStacks(t)[managedStack]
	if !ok {
		t.Fatalf("цепочки %s в %s нет — инъекции не на чем ставить", managedStack, stacksTable)
	}
	return chain
}

// Снятие объявления издателя из слоя площадки: издатель наследуется у слоя
// разработки, и проба обязана это увидеть во всех семи местах.
func TestInjection_IssuerRemovedFromTheManagedProfileIsFound(t *testing.T) {
	chain := managedChain(t)
	last := chain[len(chain)-1]
	merged := foldStack(t, chain, func(profile string, tree map[string]any) {
		if profile != last {
			return
		}
		for _, p := range issuerPlaces {
			deletePath(tree, p.path...)
		}
	})
	v := judgeIssuerOrigin(managedStack, merged)
	if !v.judged || len(v.findings) != len(issuerPlaces) {
		t.Fatalf("снятый издатель найден не во всех местах: судима=%v находок %d из %d: %v",
			v.judged, len(v.findings), len(issuerPlaces), v.findings)
	}
}

// Пустой издатель: находка называет ключ.
func TestInjection_EmptyIssuerIsFoundByItsKey(t *testing.T) {
	merged := foldStack(t, managedChain(t), nil)
	setPath(merged, "", "kaname", "config", "authn", "tokenSigning", "issuer")
	v := judgeIssuerOrigin(managedStack, merged)
	if len(v.findings) != 1 || !strings.Contains(v.findings[0], "kaname.config.authn.tokenSigning.issuer") {
		t.Fatalf("пустой издатель чеканки не найден своим ключом: %v", v.findings)
	}
}

// Частичная правка: потребитель края разошёлся с чеканкой.
func TestInjection_OneConsumerOnAnotherOriginIsFound(t *testing.T) {
	merged := foldStack(t, managedChain(t), nil)
	setPath(merged, "https://kaname.kacho.local", "api-gateway", "tokenAcceptance", "platformIssuer")
	v := judgeIssuerOrigin(managedStack, merged)
	if len(v.findings) != 1 || !strings.Contains(v.findings[0], "api-gateway.tokenAcceptance.platformIssuer") {
		t.Fatalf("разошедшийся потребитель не найден: %v", v.findings)
	}
}

// Законный близнец: цепочка разработки — консоль на настоящем имени разработки,
// издатель — имя разработки; проба молчит и цепочку не судит.
func TestInjection_DevelopmentChainIsNotJudged(t *testing.T) {
	stacks := deployableStacks(t)
	chain, ok := stacks["dev-prod"]
	if !ok {
		t.Fatalf("цепочки dev-prod в %s нет — близнеца не на чем проверить", stacksTable)
	}
	if v := judgeIssuerOrigin("dev-prod", foldStack(t, chain, nil)); v.judged || len(v.findings) != 0 {
		t.Fatalf("цепочка разработки судима (%v) либо дала находки: %v", v.judged, v.findings)
	}
}

// Законный близнец: синтетическая цепочка с консолью площадки и издателем той
// же заглушки во всех семи местах — проба молчит и засчитывает все семь.
func TestInjection_AgreeingManagedChainIsSilent(t *testing.T) {
	const origin = "https://console.probe.invalid"
	merged := map[string]any{}
	setPath(merged, origin, "global", "kacho", "identity", "appBaseURL")
	for _, p := range issuerPlaces {
		val := origin
		if p.kind == "keyset" {
			val = origin + "=https://kaname-internal.kacho.svc:9097/.well-known/kaname/jwks.json"
		}
		setPath(merged, val, p.path...)
	}
	v := judgeIssuerOrigin("synthetic", merged)
	if !v.judged || len(v.findings) != 0 || v.agreeing != len(issuerPlaces) {
		t.Fatalf("согласная цепочка: судима=%v совпавших %d из %d, находки %v",
			v.judged, v.agreeing, len(issuerPlaces), v.findings)
	}
}

func deletePath(tree map[string]any, path ...string) {
	cur := tree
	for i, k := range path {
		if i == len(path)-1 {
			delete(cur, k)
			return
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
}

func setPath(tree map[string]any, val string, path ...string) {
	cur := tree
	for i, k := range path {
		if i == len(path)-1 {
			cur[k] = val
			return
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
}
