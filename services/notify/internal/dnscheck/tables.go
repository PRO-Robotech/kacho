// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dnscheck

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net"
	"regexp"
	"strings"
)

// Check — проверка Р19: закрытый перечень пакета, он же значение метки `check`
// признака `notify_mail_dns_posture`. NTF-4 расширяет перечень, а не пишет
// строку.
type Check string

const (
	CheckDKIM  Check = "dkim"
	CheckSPF   Check = "spf"
	CheckDMARC Check = "dmarc"
)

// checks — перечень проверок попытки в порядке текста отказа.
var checks = [...]Check{CheckDKIM, CheckSPF, CheckDMARC}

// Outcome — исход проверки: закрытый тип `{ok, violated(причина), noanswer}`.
type Outcome int

const (
	OutcomeOK Outcome = iota
	OutcomeViolated
	OutcomeNoAnswer
)

func (o Outcome) String() string {
	switch o {
	case OutcomeOK:
		return "ok"
	case OutcomeViolated:
		return "violated"
	case OutcomeNoAnswer:
		return "noanswer"
	}
	return "outcome(?)"
}

// Reason — причина нарушения: закрытый перечень пакета. Входит в текст отказа
// старта и в строку WARN перепроверки рядом с именем проверки — без
// содержимого записи.
type Reason string

const (
	ReasonSPFMissing       Reason = "spf_missing"
	ReasonSPFMultiple      Reason = "spf_multiple"
	ReasonSPFMalformed     Reason = "spf_malformed"
	ReasonSPFRedirect      Reason = "spf_redirect"
	ReasonSPFNoAll         Reason = "spf_no_all"
	ReasonSPFAllNotLast    Reason = "spf_all_not_last"
	ReasonSPFAllPermissive Reason = "spf_all_permissive"
	ReasonDMARCMissing     Reason = "dmarc_missing"
	ReasonDMARCMultiple    Reason = "dmarc_multiple"
	ReasonDMARCMalformed   Reason = "dmarc_malformed"
	ReasonDMARCPolicyNone  Reason = "dmarc_policy_none"
	ReasonDMARCPolicyBad   Reason = "dmarc_policy_invalid"
	ReasonDKIMMissing      Reason = "dkim_missing"
	ReasonDKIMMultiple     Reason = "dkim_multiple"
	ReasonDKIMMalformed    Reason = "dkim_malformed"
	ReasonDKIMKeyType      Reason = "dkim_key_type"
	ReasonDKIMHash         Reason = "dkim_hash"
	ReasonDKIMRevoked      Reason = "dkim_revoked"
	ReasonDKIMKeyMismatch  Reason = "dkim_key_mismatch"
)

// Result — исход проверки с причиной (причина — только у нарушения).
type Result struct {
	Outcome Outcome
	Reason  Reason
}

var okResult = Result{Outcome: OutcomeOK}

func violated(r Reason) Result { return Result{Outcome: OutcomeViolated, Reason: r} }

// Answer — род ответа DNS (§12а): закрытая таблица без корзины.
type Answer int

const (
	// AnswerGot — ответ получен.
	AnswerGot Answer = iota
	// AnswerNoRecord — записи нет.
	AnswerNoRecord
	// AnswerNone — ответа нет.
	AnswerNone
)

func (a Answer) String() string {
	switch a {
	case AnswerGot:
		return "got"
	case AnswerNoRecord:
		return "no_record"
	case AnswerNone:
		return "none"
	}
	return "answer(?)"
}

// classifyErr — род ответа по ошибке резолвера:
//
//	nil                                   → ответ получен
//	*net.DNSError с IsNotFound            → записи нет
//	*net.DNSError с IsTimeout/IsTemporary → ответа нет
//	*net.DNSError без трёх признаков      → ответа нет (сервер ответил не по протоколу)
//	иная ошибка (отмена контекста, сеть)  → ответа нет
func classifyErr(err error) Answer {
	if err == nil {
		return AnswerGot
	}
	var de *net.DNSError
	if errors.As(err, &de) && de.IsNotFound {
		return AnswerNoRecord
	}
	// Остальные строки таблицы — «ответа нет»: DNSError с IsTimeout или
	// IsTemporary, DNSError без трёх признаков (сервер ответил не по
	// протоколу), отмена контекста и отказ сети.
	return AnswerNone
}

// queryName — абсолютное имя запроса проверки (CX1-138 (в)): всегда с
// завершающей точкой, иначе под `ndots: 5` имя сначала ушло бы в домены поиска.
func queryName(c Check, selector, domain string) string {
	domain = strings.TrimSuffix(domain, ".")
	switch c {
	case CheckDKIM:
		return selector + "._domainkey." + domain + "."
	case CheckDMARC:
		return "_dmarc." + domain + "."
	case CheckSPF:
		return domain + "."
	}
	return domain + "."
}

// selectorLabel — метка селектора по RFC 6376 §3.1.
var selectorLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// ValidSelector — селектор DKIM: имя DNS из одной или нескольких меток через
// точку (RFC 6376 §3.1), не длиннее 253 октетов. Зовут загрузчик (причина P14
// «вне формы имени DNS») и [LoadPair].
func ValidSelector(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if !selectorLabel.MatchString(l) {
			return false
		}
	}
	return true
}

// ── SPF ───────────────────────────────────────────────────────────────────

// spfMechanisms — механизмы SPF (RFC 7208 §5).
var spfMechanisms = map[string]bool{
	"all": true, "include": true, "a": true, "mx": true, "ptr": true, "ip4": true, "ip6": true, "exists": true,
}

// spfModifier — терм `имя=значение` (RFC 7208 §6).
var spfModifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*=`)

// isSPF — запись начинается с `v=spf1`, за ним пробел или конец строки.
func isSPF(txt string) bool {
	if len(txt) < 6 || !strings.EqualFold(txt[:6], "v=spf1") {
		return false
	}
	return len(txt) == 6 || txt[6] == ' '
}

// parseSPF — таблица SPF §12а, строки сверху вниз, первая подходящая.
func parseSPF(txts []string) Result {
	var rec []string
	for _, t := range txts {
		if isSPF(t) {
			rec = append(rec, t)
		}
	}
	switch {
	case len(rec) == 0:
		return violated(ReasonSPFMissing)
	case len(rec) > 1:
		return violated(ReasonSPFMultiple)
	}
	allAt, allQual, redirect, mechAfterAll := -1, byte(0), false, false
	for i, term := range strings.Fields(rec[0])[1:] {
		if spfModifier.MatchString(term) {
			if strings.EqualFold(term[:strings.IndexByte(term, '=')], "redirect") {
				redirect = true
			}
			continue
		}
		qual := byte(0)
		if strings.ContainsRune("+-~?", rune(term[0])) {
			qual, term = term[0], term[1:]
		}
		name, _, _ := strings.Cut(term, ":")
		name, _, _ = strings.Cut(name, "/")
		name = strings.ToLower(name)
		if !spfMechanisms[name] || (name == "all" && !strings.EqualFold(term, "all")) {
			return violated(ReasonSPFMalformed)
		}
		if allAt >= 0 {
			mechAfterAll = true
		}
		if name == "all" && allAt < 0 {
			allAt, allQual = i, qual
		}
	}
	switch {
	case allAt < 0 && redirect:
		return violated(ReasonSPFRedirect)
	case allAt < 0:
		return violated(ReasonSPFNoAll)
	case mechAfterAll:
		return violated(ReasonSPFAllNotLast)
	case allQual == '-' || allQual == '~':
		return okResult
	}
	return violated(ReasonSPFAllPermissive)
}

// ── теги `имя=значение; …` (DMARC, DKIM) ─────────────────────────────────

var tagName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

type tag struct{ name, value string }

// parseTags — список тегов RFC 6376 §3.2: `имя=значение` через `;`, пробелы
// вокруг снимаются, завершающий `;` допустим. Повтор имени или терм не в
// форме — false.
func parseTags(s string) ([]tag, bool) {
	var out []tag
	seen := map[string]bool{}
	parts := strings.Split(s, ";")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" && i == len(parts)-1 {
			continue
		}
		name, value, ok := strings.Cut(p, "=")
		name = strings.TrimSpace(name)
		if !ok || !tagName.MatchString(name) {
			return nil, false
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, false
		}
		seen[key] = true
		out = append(out, tag{name: key, value: strings.TrimSpace(value)})
	}
	return out, true
}

func lookupTag(tags []tag, name string) (string, bool) {
	for _, t := range tags {
		if t.name == name {
			return t.value, true
		}
	}
	return "", false
}

// ── DMARC ─────────────────────────────────────────────────────────────────

// isDMARC — первый тег записи ровно `v=DMARC1`.
func isDMARC(txt string) bool {
	first, _, _ := strings.Cut(txt, ";")
	name, value, ok := strings.Cut(strings.TrimSpace(first), "=")
	return ok && strings.TrimSpace(name) == "v" && strings.TrimSpace(value) == "DMARC1"
}

// parseDMARC — таблица DMARC §12а.
func parseDMARC(txts []string) Result {
	var rec []string
	for _, t := range txts {
		if isDMARC(t) {
			rec = append(rec, t)
		}
	}
	switch {
	case len(rec) == 0:
		return violated(ReasonDMARCMissing)
	case len(rec) > 1:
		return violated(ReasonDMARCMultiple)
	}
	tags, ok := parseTags(rec[0])
	if !ok {
		return violated(ReasonDMARCMalformed)
	}
	p, ok := lookupTag(tags, "p")
	switch {
	case !ok:
		return violated(ReasonDMARCPolicyBad)
	case strings.EqualFold(p, "none"):
		return violated(ReasonDMARCPolicyNone)
	case strings.EqualFold(p, "quarantine"), strings.EqualFold(p, "reject"):
		return okResult
	}
	return violated(ReasonDMARCPolicyBad)
}

// ── DKIM ──────────────────────────────────────────────────────────────────

// parseDKIM — таблица DKIM §12а: строки одной записи уже склеены резолвером.
// want — открытый ключ, выведенный из закрытого ключа пары.
func parseDKIM(txts []string, want *rsa.PublicKey) Result {
	switch {
	case len(txts) == 0:
		return violated(ReasonDKIMMissing)
	case len(txts) > 1:
		return violated(ReasonDKIMMultiple)
	}
	tags, ok := parseTags(txts[0])
	if !ok {
		return violated(ReasonDKIMMalformed)
	}
	for i, t := range tags {
		if t.name == "v" && (i != 0 || t.value != "DKIM1") {
			return violated(ReasonDKIMMalformed)
		}
	}
	if k, ok := lookupTag(tags, "k"); ok && !strings.EqualFold(k, "rsa") {
		return violated(ReasonDKIMKeyType)
	}
	if h, ok := lookupTag(tags, "h"); ok && !hashListHasSHA256(h) {
		return violated(ReasonDKIMHash)
	}
	p, ok := lookupTag(tags, "p")
	if !ok {
		return violated(ReasonDKIMMalformed)
	}
	p = strings.Join(strings.Fields(p), "")
	if p == "" {
		return violated(ReasonDKIMRevoked)
	}
	der, err := base64.StdEncoding.DecodeString(p)
	if err != nil {
		return violated(ReasonDKIMMalformed)
	}
	pub, ok := parsePublicKey(der)
	if !ok {
		return violated(ReasonDKIMMalformed)
	}
	rsaPub, isRSA := pub.(*rsa.PublicKey)
	if !isRSA {
		return violated(ReasonDKIMKeyType)
	}
	if want == nil || rsaPub.E != want.E || rsaPub.N.Cmp(want.N) != 0 {
		return violated(ReasonDKIMKeyMismatch)
	}
	return okResult
}

// parsePublicKey — ключ записи: SubjectPublicKeyInfo либо PKCS#1.
func parsePublicKey(der []byte) (any, bool) {
	if pub, err := x509.ParsePKIXPublicKey(der); err == nil {
		return pub, true
	}
	if pub, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return pub, true
	}
	return nil, false
}

func hashListHasSHA256(h string) bool {
	for _, a := range strings.Split(h, ":") {
		if strings.EqualFold(strings.TrimSpace(a), "sha256") {
			return true
		}
	}
	return false
}
