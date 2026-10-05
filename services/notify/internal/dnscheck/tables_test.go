// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dnscheck

// tables_test.go — закрытые таблицы разбора записей и рода ответа DNS (полоса
// N14; замысел §12а «Проверки и род ответа», «Разбор содержимого — закрытые
// таблицы» (CX1-138 (а)), «Имена запросов абсолютные» (CX1-138 (в)),
// «Валидатор селектора» (CX1-138 (б))).
//
// По строке пробы на каждую строку таблиц §12а; проба печатает число строк по
// таблице. Контракт испытуемого (имена — из §12а; форма исхода — закрытый тип
// `{ok, violated(причина), noanswer}`):
//
//	type Outcome int                       // OutcomeOK, OutcomeViolated, OutcomeNoAnswer
//	type Reason string                     // spf_missing, …, dkim_key_mismatch
//	type Result struct { Outcome Outcome; Reason Reason }
//	func parseSPF(txts []string) Result
//	func parseDMARC(txts []string) Result
//	func parseDKIM(txts []string, want *rsa.PublicKey) Result   // строки одной записи уже склеены
//	type Answer int                        // AnswerGot, AnswerNoRecord, AnswerNone
//	func classifyErr(err error) Answer
//	type Check string                      // CheckDKIM = "dkim", CheckSPF = "spf", CheckDMARC = "dmarc"
//	func queryName(c Check, selector, domain string) string
//	func ValidSelector(s string) bool

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"testing"
)

type row struct {
	name    string
	txts    []string
	outcome Outcome
	reason  string
}

func checkRows(t *testing.T, table string, rows []row, parse func([]string) Result) {
	t.Helper()
	t.Logf("таблица %s: строк пробы %d", table, len(rows))
	if len(rows) == 0 {
		t.Fatalf("таблица %s: строк пробы 0 — проверять нечего", table)
	}
	for _, r := range rows {
		t.Run(table+"/"+r.name, func(t *testing.T) {
			got := parse(r.txts)
			if got.Outcome != r.outcome || string(got.Reason) != r.reason {
				t.Fatalf("%q → (%v, %q), ожидалось (%v, %q)", r.txts, got.Outcome, got.Reason, r.outcome, r.reason)
			}
		})
	}
}

// SPF — TXT `<домен From>.`; строки читаются сверху вниз, первая подходящая.
func TestParseSPFIsAClosedTable(t *testing.T) {
	ok, v := OutcomeOK, OutcomeViolated
	checkRows(t, "SPF", []row{
		{"нет v=spf1", []string{"google-site-verification=x"}, v, "spf_missing"},
		{"v=spf10 — не SPF", []string{"v=spf10 -all"}, v, "spf_missing"},
		{"TXT нет вовсе", nil, v, "spf_missing"},
		{"две записи v=spf1", []string{"v=spf1 -all", "v=spf1 ~all"}, v, "spf_multiple"},
		{"терм не разбирается", []string{"v=spf1 @@@ -all"}, v, "spf_malformed"},
		{"all нет, есть redirect", []string{"v=spf1 redirect=_spf.example.test"}, v, "spf_redirect"},
		{"all нет", []string{"v=spf1 ip4:192.0.2.0/24"}, v, "spf_no_all"},
		{"после all механизм", []string{"v=spf1 -all ip4:192.0.2.1"}, v, "spf_all_not_last"},
		{"+all", []string{"v=spf1 +all"}, v, "spf_all_permissive"},
		{"?all", []string{"v=spf1 ?all"}, v, "spf_all_permissive"},
		{"all без квалификатора", []string{"v=spf1 all"}, v, "spf_all_permissive"},
		{"-all", []string{"v=spf1 ip4:192.0.2.0/24 -all"}, ok, ""},
		{"~all", []string{"v=spf1 ~all"}, ok, ""},
		{"-ALL", []string{"V=SPF1 -ALL"}, ok, ""},
		{"-all exp=…", []string{"v=spf1 -all exp=explain.example.test"}, ok, ""},
		{"SPF рядом с чужой TXT", []string{"google-site-verification=x", "v=spf1 -all"}, ok, ""},
	}, parseSPF)
}

// DMARC — TXT `_dmarc.<домен From>.`.
func TestParseDMARCIsAClosedTable(t *testing.T) {
	ok, v := OutcomeOK, OutcomeViolated
	checkRows(t, "DMARC", []row{
		{"нет v=DMARC1", []string{"p=reject"}, v, "dmarc_missing"},
		{"первый тег не v=DMARC1", []string{"p=reject; v=DMARC1"}, v, "dmarc_missing"},
		{"две записи", []string{"v=DMARC1; p=reject", "v=DMARC1; p=quarantine"}, v, "dmarc_multiple"},
		{"тег повторён", []string{"v=DMARC1; p=reject; p=reject"}, v, "dmarc_malformed"},
		{"тег не имя=значение", []string{"v=DMARC1; p=reject; junk"}, v, "dmarc_malformed"},
		{"p= нет", []string{"v=DMARC1; rua=mailto:d@example.test"}, v, "dmarc_policy_invalid"},
		{"p=none", []string{"v=DMARC1; p=none"}, v, "dmarc_policy_none"},
		{"p=NONE", []string{"v=DMARC1; p=NONE"}, v, "dmarc_policy_none"},
		{"p=quarantine", []string{"v=DMARC1; p=quarantine"}, ok, ""},
		{"p=Reject", []string{"v=DMARC1; p=Reject"}, ok, ""},
		{"p= иное", []string{"v=DMARC1; p=monitor"}, v, "dmarc_policy_invalid"},
	}, parseDMARC)
}

// DKIM — TXT `<селектор>._domainkey.<домен From>.`; записи SPKI и PKCS#1 с тем
// же ключом — обе ok.
func TestParseDKIMIsAClosedTable(t *testing.T) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ключ: %v", err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ключ: %v", err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ключ ECDSA: %v", err)
	}
	spki := func(pub any) string {
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: SPKI: %v", err)
		}
		return base64.StdEncoding.EncodeToString(der)
	}
	pkcs1 := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PublicKey(&k.PublicKey))
	p := spki(&k.PublicKey)
	// Пробелы внутри p= снимаются (запись приходит строками TXT).
	spaced := p[:40] + " " + p[40:]

	ok, v := OutcomeOK, OutcomeViolated
	checkRows(t, "DKIM", []row{
		{"записи нет", nil, v, "dkim_missing"},
		{"записей две", []string{"v=DKIM1; k=rsa; p=" + p, "v=DKIM1; k=rsa; p=" + p}, v, "dkim_multiple"},
		{"тег повторён", []string{"v=DKIM1; k=rsa; k=rsa; p=" + p}, v, "dkim_malformed"},
		{"тег не разбирается", []string{"v=DKIM1; junk; p=" + p}, v, "dkim_malformed"},
		{"v= не первым", []string{"k=rsa; v=DKIM1; p=" + p}, v, "dkim_malformed"},
		{"v= не DKIM1", []string{"v=DKIM2; p=" + p}, v, "dkim_malformed"},
		{"k= не rsa", []string{"v=DKIM1; k=ed25519; p=" + p}, v, "dkim_key_type"},
		{"h= без sha256", []string{"v=DKIM1; h=sha1; p=" + p}, v, "dkim_hash"},
		{"p= нет", []string{"v=DKIM1; k=rsa"}, v, "dkim_malformed"},
		{"p= пуст — отозван", []string{"v=DKIM1; k=rsa; p="}, v, "dkim_revoked"},
		{"p= не base64", []string{"v=DKIM1; k=rsa; p=@@@"}, v, "dkim_malformed"},
		{"p= не ключ", []string{"v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString([]byte("not a key"))}, v, "dkim_malformed"},
		{"ключ не RSA", []string{"v=DKIM1; p=" + spki(&ec.PublicKey)}, v, "dkim_key_type"},
		{"ключ не совпадает", []string{"v=DKIM1; k=rsa; p=" + spki(&other.PublicKey)}, v, "dkim_key_mismatch"},
		{"совпадает, SPKI", []string{"v=DKIM1; k=rsa; p=" + p}, ok, ""},
		{"совпадает, PKCS#1", []string{"v=DKIM1; k=rsa; p=" + pkcs1}, ok, ""},
		{"совпадает, h=sha256, без v=", []string{"h=sha256; p=" + p}, ok, ""},
		{"совпадает, пробел в p=", []string{"v=DKIM1; k=RSA; p=" + spaced}, ok, ""},
	}, func(txts []string) Result { return parseDKIM(txts, &k.PublicKey) })
}

// Род ответа DNS — закрытая таблица без корзины (§12а).
func TestClassifyErrIsAClosedTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Answer
	}{
		{"nil — ответ получен", nil, AnswerGot},
		{"IsNotFound — записи нет", &net.DNSError{Err: "no such host", Name: "x.", IsNotFound: true}, AnswerNoRecord},
		{"IsTimeout — ответа нет", &net.DNSError{Err: "i/o timeout", Name: "x.", IsTimeout: true}, AnswerNone},
		{"IsTemporary — ответа нет", &net.DNSError{Err: "server misbehaving", Name: "x.", IsTemporary: true}, AnswerNone},
		{"DNSError без трёх признаков — ответа нет", &net.DNSError{Err: "bad", Name: "x."}, AnswerNone},
		{"отмена контекста — ответа нет", context.Canceled, AnswerNone},
		{"сеть — ответа нет", errors.New("connection refused"), AnswerNone},
	}
	t.Logf("таблица рода ответа: строк пробы %d", len(cases))
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyErr(c.err); got != c.want {
				t.Fatalf("classifyErr(%v) = %v, ожидалось %v", c.err, got, c.want)
			}
		})
	}
}

// Имена запросов абсолютные (CX1-138 (в)): каждое кончается точкой.
func TestQueryNamesAreAbsolute(t *testing.T) {
	cases := []struct {
		check Check
		want  string
	}{
		{CheckDKIM, "mail._domainkey.example.test."},
		{CheckDMARC, "_dmarc.example.test."},
		{CheckSPF, "example.test."},
	}
	for _, c := range cases {
		got := queryName(c.check, "mail", "example.test")
		if got != c.want {
			t.Fatalf("queryName(%s) = %q, ожидалось %q", c.check, got, c.want)
		}
		if !strings.HasSuffix(got, ".") {
			t.Fatalf("имя запроса %q не абсолютное", got)
		}
	}
}

// Валидатор селектора (CX1-138 (б)): метки через точку по RFC 6376 §3.1.
func TestValidSelectorFollowsRFC6376(t *testing.T) {
	for _, s := range []string{"sel_1", "", ".mail", "mail.", "-mail", "mail-", "ma il", "mail..x", strings.Repeat("a", 64)} {
		if ValidSelector(s) {
			t.Fatalf("селектор %q принят — он вне формы имени DNS", s)
		}
	}
	for _, s := range []string{"mail", "2026.mail", "s1-a", strings.Repeat("a", 63)} {
		if !ValidSelector(s) {
			t.Fatalf("законный селектор %q отвергнут", s)
		}
	}
}
