// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_rest_front_client_auth_vocabulary_injection_test.go — доказательство того,
// что сверка словаря режимов проверки клиента СПОСОБНА упасть, и падает на своём
// предмете (#2463).
//
// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ РОНЯЕТ ТОЛЬКО ПРОВЕРЯЕМОЕ
//
// Ни продукт, ни харнесс дерева не правятся: и разбор, и суждение принимают вход
// ДОВОДОМ — каталог с синтетическим объявлением, байты синтетического скрипта,
// перечень осей. Каждый мир отличается от законного близнеца ОДНИМ фактом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОСИ
//
//  1. РАСХОЖДЕНИЕ ВЕЛИЧИНЫ — находка называет ОБЕ стороны. Законный близнец: та
//     же величина, записанная ИНОЙ ФОРМОЙ (одинарные кавычки, пробелы вокруг
//     знака равенства, хвостовой комментарий) — молчание.
//  2. УМОЛЧАНИЕ — величина берётся у РАЗРЕШАТЕЛЯ, а не у первой константы:
//     смена ветви пустого входа обязана краснеть.
//  3. НЕНАКРЫТОЕ ИМЯ — величина харнесса, которой не отвечает ни одна ось,
//     названа: несверяемое не даёт ни красного, ни зелёного.
//  4. ДВОЙНОЕ ПРИСВАИВАНИЕ — находка: величина выразима двумя способами, и
//     решает порядок строк, а не объявление.
//  5. ПРОЗА НЕ ОБЪЯВЛЕНИЕ — величина в комментарии, в строке документации и
//     внутри функции присваиванием модульного уровня не является. Ось несущая:
//     комментарий харнесса называет продуктовые константы дословно, и гейт по
//     слову краснел бы на собственном объяснении проверяемого.
//  6. ПУСТОЙ ОБХОД — находка, а не тишина.
//  7. ОСЬ ИСТЕКАЕТ САМА — ось, ссылающаяся на константу, которой продукт не
//     объявляет, названа находкой.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// injProductDir кладёт синтетическое объявление продукта и отдаёт каталог.
func injProductDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mtls.go"), []byte(body), 0o600); err != nil {
		t.Fatalf("синтетическое объявление не записано: %v", err)
	}
	return dir
}

// injProductSource — объявление словаря с подставленными величинами.
func injProductSource(mutual, serverOnly string) string {
	return `package config

const (
	// clientAuthServerTLSOnly — режим назван и в этом комментарии: "` + serverOnly + `".
	clientAuthServerTLSOnly = "` + serverOnly + `"
	clientAuthMutual        = "` + mutual + `"
)

func resolveClientAuthMode(mode string) string {
	if mode == "" {
		return clientAuthServerTLSOnly
	}
	return mode
}
`
}

// injHarness — скрипт харнесса с подставленным телом.
func injHarness(body string) []byte {
	return []byte("#!/usr/bin/env python3\n\"\"\"харнесс.\"\"\"\n\n" + body + "\n")
}

// injRead — общий вход: словарь продукта и словарь харнесса.
func injRead(t *testing.T, product, harness string) (productClientAuthVocabulary, harnessVocabulary) {
	t.Helper()
	got, err := ReadProductClientAuthVocabulary(injProductDir(t, product))
	if err != nil {
		t.Fatalf("синтетическое объявление продукта не прочитано: %v", err)
	}
	return got, ReadHarnessVocabulary(injHarness(harness))
}

// lawfulHarness — зеркало, сходящееся с продуктом.
const lawfulHarness = "CLIENT_AUTH_DEFAULT = \"server-tls-only\"\nCLIENT_AUTH_MUTUAL = \"mutual\"\n"

// TestClientAuthVocabularyInjection_LawfulTwinStaysSilent — ЗАКОННЫЙ БЛИЗНЕЦ,
// первым: без него всякое красное ниже приходило бы от чего угодно.
func TestClientAuthVocabularyInjection_LawfulTwinStaysSilent(t *testing.T) {
	product, harness := injRead(t, injProductSource("mutual", "server-tls-only"), lawfulHarness)
	findings, census := AuditClientAuthMirror(product, harness, clientAuthMirror)
	if len(findings) != 0 {
		t.Fatalf("сходящееся зеркало объявлено находками: %v", findings)
	}
	if census.AgreeingAxes != len(clientAuthMirror) {
		t.Fatalf("сходится %d осей из %d", census.AgreeingAxes, len(clientAuthMirror))
	}
}

// TestClientAuthVocabularyInjection_OtherLawfulFormsAlsoStaySilent — та же
// величина, записанная иной формой Python.
//
// Ось несущая: форма, о которой разбор не знает, даёт не красное и не зелёное, а
// МОЛЧАНИЕ — то есть величина уходит из-под наблюдения, оставляя гейт зелёным.
func TestClientAuthVocabularyInjection_OtherLawfulFormsAlsoStaySilent(t *testing.T) {
	for _, c := range []struct{ name, harness string }{
		{"одинарные кавычки", "CLIENT_AUTH_DEFAULT = 'server-tls-only'\nCLIENT_AUTH_MUTUAL = 'mutual'\n"},
		{"без пробелов вокруг знака", "CLIENT_AUTH_DEFAULT=\"server-tls-only\"\nCLIENT_AUTH_MUTUAL=\"mutual\"\n"},
		{"хвостовой комментарий", "CLIENT_AUTH_DEFAULT = \"server-tls-only\"  # зеркало\nCLIENT_AUTH_MUTUAL = \"mutual\"\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			product, harness := injRead(t, injProductSource("mutual", "server-tls-only"), c.harness)
			findings, census := AuditClientAuthMirror(product, harness, clientAuthMirror)
			if len(findings) != 0 {
				t.Fatalf("законная форма записи объявлена находками: %v", findings)
			}
			if census.HarnessValues != 2 {
				t.Fatalf("присваиваний прочитано %d, ожидалось 2 — форма не узнана", census.HarnessValues)
			}
		})
	}
}

// TestClientAuthVocabularyInjection_ProductValueDriftIsCaught — продукт сменил
// величину взаимного режима; находка называет ОБЕ стороны.
func TestClientAuthVocabularyInjection_ProductValueDriftIsCaught(t *testing.T) {
	product, harness := injRead(t, injProductSource("mutual-tls", "server-tls-only"), lawfulHarness)
	findings, _ := AuditClientAuthMirror(product, harness, clientAuthMirror)
	if len(findings) != 1 {
		t.Fatalf("смена величины продукта дала находок %d, ожидалась 1: %v", len(findings), findings)
	}
	for _, want := range []string{"CLIENT_AUTH_MUTUAL", `"mutual"`, "clientAuthMutual", `"mutual-tls"`} {
		if !strings.Contains(findings[0], want) {
			t.Errorf("находка не называет %s: %s", want, findings[0])
		}
	}
}

// TestClientAuthVocabularyInjection_DefaultComesFromTheResolver — умолчание
// берётся у РАЗРЕШАТЕЛЯ.
//
// Смена ветви пустого входа не меняет ни одной константы: гейт, сверяющий
// умолчание с ПЕРВОЙ константой перечня, остался бы зелёным — и харнесс подавал
// бы лист там, где продукт его не требует, либо наоборот.
func TestClientAuthVocabularyInjection_DefaultComesFromTheResolver(t *testing.T) {
	flipped := `package config

const (
	clientAuthServerTLSOnly = "server-tls-only"
	clientAuthMutual        = "mutual"
)

func resolveClientAuthMode(mode string) string {
	if mode == "" {
		return clientAuthMutual
	}
	return mode
}
`
	product, harness := injRead(t, flipped, lawfulHarness)
	if product.DefaultVia != "clientAuthMutual" {
		t.Fatalf("разрешатель прочитан неверно: умолчание отвечает %q", product.DefaultVia)
	}
	findings, _ := AuditClientAuthMirror(product, harness, clientAuthMirror)
	if len(findings) != 1 {
		t.Fatalf("смена умолчания дала находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "CLIENT_AUTH_DEFAULT") {
		t.Errorf("находка не называет оси умолчания: %s", findings[0])
	}
}

// TestClientAuthVocabularyInjection_UncoveredHarnessNameIsCaught — величина
// харнесса, которой не отвечает ни одна ось, названа.
func TestClientAuthVocabularyInjection_UncoveredHarnessNameIsCaught(t *testing.T) {
	product, harness := injRead(t, injProductSource("mutual", "server-tls-only"),
		lawfulHarness+"CLIENT_AUTH_OPTIONAL = \"optional-mutual\"\n")
	findings, _ := AuditClientAuthMirror(product, harness, clientAuthMirror)
	if len(findings) != 1 {
		t.Fatalf("ненакрытая величина дала находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "CLIENT_AUTH_OPTIONAL") {
		t.Errorf("находка не называет величины: %s", findings[0])
	}
	if !strings.Contains(findings[0], "ни красного") {
		t.Errorf("находка не называет ПРИЧИНЫ (несверяемое молчит), а только симптом: %s", findings[0])
	}
}

// TestClientAuthVocabularyInjection_DoubleAssignmentIsCaught — двойное
// присваивание одного имени.
func TestClientAuthVocabularyInjection_DoubleAssignmentIsCaught(t *testing.T) {
	product, harness := injRead(t, injProductSource("mutual", "server-tls-only"),
		lawfulHarness+"CLIENT_AUTH_MUTUAL = \"mutual\"\n")
	if harness.Counts["CLIENT_AUTH_MUTUAL"] != 2 {
		t.Fatalf("присваиваний насчитано %d, ожидалось 2", harness.Counts["CLIENT_AUTH_MUTUAL"])
	}
	findings, _ := AuditClientAuthMirror(product, harness, clientAuthMirror)
	if len(findings) != 1 || !strings.Contains(findings[0], "присвоено 2") {
		t.Fatalf("двойное присваивание не названо: %v", findings)
	}
}

// TestClientAuthVocabularyInjection_ProseIsNotADeclaration — величина в
// комментарии, в строке документации и внутри функции объявлением НЕ является.
//
// Комментарий настоящего харнесса называет продуктовые константы дословно —
// гейт по слову краснел бы на собственном объяснении проверяемого.
func TestClientAuthVocabularyInjection_ProseIsNotADeclaration(t *testing.T) {
	prose := "# CLIENT_AUTH_MUTUAL = \"из комментария\"\n" +
		"\"\"\"В документации: CLIENT_AUTH_DEFAULT = \"из строки\".\"\"\"\n" +
		"def f():\n    CLIENT_AUTH_MUTUAL = \"из функции\"\n    return CLIENT_AUTH_MUTUAL\n" +
		lawfulHarness
	product, harness := injRead(t, injProductSource("mutual", "server-tls-only"), prose)
	if harness.Values["CLIENT_AUTH_MUTUAL"] != "mutual" {
		t.Errorf("величина взята из прозы: %q", harness.Values["CLIENT_AUTH_MUTUAL"])
	}
	if n := harness.Counts["CLIENT_AUTH_MUTUAL"]; n != 1 {
		t.Errorf("присваиваний насчитано %d — проза зачтена объявлением", n)
	}
	if findings, _ := AuditClientAuthMirror(product, harness, clientAuthMirror); len(findings) != 0 {
		t.Fatalf("проза дала находки: %v", findings)
	}
}

// TestClientAuthVocabularyInjection_ProductProseIsNotADeclaration — та же
// сторона у продукта: имя режима в комментарии константой не становится.
func TestClientAuthVocabularyInjection_ProductProseIsNotADeclaration(t *testing.T) {
	product, _ := injRead(t, injProductSource("mutual", "server-tls-only"), lawfulHarness)
	if got := product.Values["clientAuthServerTLSOnly"]; got != "server-tls-only" {
		t.Fatalf("величина продукта прочитана как %q — комментарий рядом называет её же, и разбор "+
			"обязан взять ОБЪЯВЛЕНИЕ", got)
	}
	if len(product.Values) != 2 {
		t.Fatalf("величин прочитано %d, ожидалось 2 — комментарий зачтён объявлением", len(product.Values))
	}
}

// TestClientAuthVocabularyInjection_EmptyWalkIsAFinding — три вида пустоты, и
// каждый обязан быть находкой, а не тишиной.
func TestClientAuthVocabularyInjection_EmptyWalkIsAFinding(t *testing.T) {
	for _, c := range []struct {
		name, product, harness, want string
	}{
		{"продукт не объявляет словаря", "package config\n", lawfulHarness, "продукт не объявил ни одной величины"},
		{"харнесс без присваиваний", injProductSource("mutual", "server-tls-only"), "# только проза\n", "не прочитано ни одного присваивания"},
		{"разрешателя нет", "package config\n\nconst clientAuthMutual = \"mutual\"\n", lawfulHarness, "не назвал константы"},
	} {
		t.Run(c.name, func(t *testing.T) {
			product, harness := injRead(t, c.product, c.harness)
			findings, _ := AuditClientAuthMirror(product, harness, clientAuthMirror)
			if len(findings) == 0 {
				t.Fatal("пустой обход прошёл молча — «зеркало сходится» верно тривиально, когда " +
					"сверять нечего")
			}
			var joined string
			for _, f := range findings {
				joined += f + "\n"
			}
			if !strings.Contains(joined, c.want) {
				t.Errorf("находка не называет причины (%q): %s", c.want, joined)
			}
		})
	}
}

// TestClientAuthVocabularyInjection_AxisExpiresByItself — ось, ссылающаяся на
// константу, которой продукт не объявляет, есть находка.
//
// Без этой половины перечень осей переживал бы свой предмет и молча перестал бы
// что-либо сверять.
func TestClientAuthVocabularyInjection_AxisExpiresByItself(t *testing.T) {
	product, harness := injRead(t, injProductSource("mutual", "server-tls-only"), lawfulHarness)
	axes := append([]vocabularyMirror{}, clientAuthMirror...)
	axes = append(axes, vocabularyMirror{HarnessName: "CLIENT_AUTH_MUTUAL", ProductName: "clientAuthGone"})
	findings, _ := AuditClientAuthMirror(product, harness, axes)
	if len(findings) != 1 {
		t.Fatalf("осиротевшая ось дала находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "пережил свой предмет") {
		t.Errorf("находка не называет причины: %s", findings[0])
	}
}
