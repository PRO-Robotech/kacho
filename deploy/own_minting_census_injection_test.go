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
// НЕ объявляющий и названный в ведомости (законен: таков `dev`); перечень
// принимаемых из НЕСКОЛЬКИХ издателей с пробелами (законен: таков каждый
// чеканящий стенд); потребитель, не объявляющий издателя платформы там, где
// чеканки нет.
//
// ВХОД СИНТЕТИЧЕСКИЙ НАМЕРЕННО: настоящее дерево несёт одно состояние —
// согласованное, — а предмет здесь семь расхождений.
package deploy_test

import (
	"strings"
	"testing"
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
				},
			},
		},
		"api-gateway": map[string]any{"tokenAcceptance": acceptance()},
		"registry":    map[string]any{"tokenAcceptance": acceptance()},
	}
}

// silentStack — стенд, чеканки НЕ объявляющий: ни ручек чеканки, ни издателя
// платформы у потребителей. Законное состояние — таков `dev`.
func silentStack() map[string]any {
	return map[string]any{
		"kaname":      map[string]any{"config": map[string]any{"authn": map[string]any{}}},
		"api-gateway": map[string]any{},
		"registry":    map[string]any{},
	}
}

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
