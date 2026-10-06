// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package dkim — подпись DKIM окончательного письма notify (RFC 6376).
//
// Что здесь решено и почему:
//
//   - Подписывается письмо в окончательных байтах сборщика: оно уже 7bit
//     (части `7bit` либо quoted-printable), поэтому ретранслятору нечего
//     перекодировать и подпись остаётся над теми байтами, что уйдут в `DATA`.
//     Подпись байтов письма не меняет: заголовок DKIM-Signature добавляется в
//     начало, после него — только dot-stuffing транспорта.
//   - Пара (селектор и ключ) читается у источника на КАЖДОМ письме: источник —
//     страж DNS установки, который меняет пару атомарно на такте
//     перепроверки; письмо подписано той парой, что действует в момент
//     подписи, и смена пары не требует перезапуска.
//   - Нулевая пара (страж ещё не прошёл старт) — ошибка: письма без подписи
//     нет, пути «отправить неподписанным» не существует.
//   - Алгоритм — rsa-sha256 (RSASSA-PKCS1-v1_5), канонизация relaxed/relaxed.
//     Подписываемые заголовки — закрытый перечень [signedHeaders]; From в h=
//     дважды: второе вхождение подписывает ОТСУТСТВИЕ второго From, и
//     добавленный в пути второй From ломает подпись. List-Unsubscribe —
//     в h=, только если заголовок в письме есть.
//   - Тегов l= (длина тела) и x= (срок) нет: l= позволяет дописать тело под
//     подписью, x= отдаёт исход проверки часам получателя.
package dkim

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkimkey"
)

// PairSource — источник пары, которой идёт подпись (страж DNS установки).
type PairSource interface {
	Pair() dkimkey.Pair
}

// Сторожа пакета — различимы через errors.Is. Ни одна ошибка не несёт
// значения ключа и байтов письма.
var (
	// ErrNoPair — у источника нет действующей пары (страж не прошёл старт).
	ErrNoPair = errors.New("dkim: пары подписи нет")
	// ErrShortKey — ключ пары короче dkimkey.MinKeyBits.
	ErrShortKey = errors.New("dkim: ключ пары короче допустимого")
	// ErrMessage — письмо вне формы: нет пустой строки между заголовками и
	// телом, заголовок без двоеточия либо From не ровно один.
	ErrMessage = errors.New("dkim: письмо вне формы")
)

// signedHeaders — заголовки, которые подписываются всегда, в порядке h=.
// Заголовок, которого в письме нет, подписывается как отсутствующий.
var signedHeaders = []string{"from", "from", "to", "subject", "date", "message-id", "mime-version", "content-type"}

// listUnsubscribe — подписывается, если заголовок в письме есть.
const listUnsubscribe = "list-unsubscribe"

// foldWidth — ширина строки заголовка подписи до переноса (RFC 5322 §2.1.1).
const foldWidth = 76

// Signer — подписчик писем одного домена. Безопасен для одновременного
// вызова: своего изменяемого состояния у него нет.
type Signer struct {
	domain string
	pairs  PairSource
}

// NewSigner собирает подписчик домена domain (d=, домен From установки) над
// источником пары pairs. Пустой домен либо отсутствующий источник — ошибка.
func NewSigner(domain string, pairs PairSource) (*Signer, error) {
	d := strings.TrimSuffix(strings.TrimSpace(domain), ".")
	var bad []string
	if d == "" {
		bad = append(bad, "домен подписи пуст")
	} else if !validDomain(d) {
		bad = append(bad, "домен подписи вне формы имени DNS")
	}
	if pairs == nil {
		bad = append(bad, "источник пары не передан")
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("dkim: подписчик: %s", strings.Join(bad, "; "))
	}
	return &Signer{domain: strings.ToLower(d), pairs: pairs}, nil
}

// validDomain — домен из меток [A-Za-z0-9-] длиной 1..63, не с дефиса на
// краях, общей длиной ≤ 253: значение тега d= не может нести «;» и пробелы.
func validDomain(d string) bool {
	if len(d) > 253 {
		return false
	}
	for _, l := range strings.Split(d, ".") {
		if len(l) == 0 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// Sign подписывает письмо msg текущей парой источника и возвращает письмо с
// заголовком DKIM-Signature в начале; байты msg не меняются. Нулевая пара
// либо письмо вне формы — ошибка и нет письма.
func (s *Signer) Sign(msg []byte) ([]byte, error) {
	p := s.pairs.Pair()
	if p.Key == nil || p.Selector == "" {
		return nil, ErrNoPair
	}
	if p.Key.N.BitLen() < dkimkey.MinKeyBits {
		return nil, ErrShortKey
	}
	fields, body, err := split(msg)
	if err != nil {
		return nil, err
	}
	keys, err := headerKeys(fields)
	if err != nil {
		return nil, err
	}

	bh := sha256.Sum256(relaxedBody(body))
	unsigned := signatureField(s.domain, p.Selector, keys, base64.StdEncoding.EncodeToString(bh[:]))

	h := sha256.New()
	for _, f := range pick(fields, keys) {
		h.Write([]byte(relaxedHeader(f)))
	}
	// Поле подписи — с пустым b= и без завершающего CRLF (RFC 6376 §3.7).
	h.Write([]byte(strings.TrimSuffix(relaxedHeader(unsigned), "\r\n")))
	sig, err := rsa.SignPKCS1v15(nil, p.Key, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("dkim: подпись: %w", err)
	}

	field := unsigned + fold(base64.StdEncoding.EncodeToString(sig), len("b=")+1) + "\r\n"
	out := make([]byte, 0, len(field)+len(msg))
	out = append(out, field...)
	return append(out, msg...), nil
}

// field — поле заголовка письма как оно лежит в байтах (с продолжениями и
// завершающим CRLF) и его имя в нижнем регистре.
type field struct {
	name string
	raw  string
}

// split — поля заголовка и тело письма. Тело — всё после первой пустой
// строки.
func split(msg []byte) ([]field, []byte, error) {
	end := bytes.Index(msg, []byte("\r\n\r\n"))
	if end < 0 {
		return nil, nil, fmt.Errorf("%w: нет пустой строки между заголовками и телом", ErrMessage)
	}
	var fields []field
	for _, line := range strings.SplitAfter(string(msg[:end+2]), "\r\n") {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if len(fields) == 0 {
				return nil, nil, fmt.Errorf("%w: продолжение до первого заголовка", ErrMessage)
			}
			fields[len(fields)-1].raw += line
			continue
		}
		name, _, ok := strings.Cut(line, ":")
		if !ok || strings.TrimRight(name, " \t") == "" {
			return nil, nil, fmt.Errorf("%w: заголовок без двоеточия", ErrMessage)
		}
		fields = append(fields, field{name: strings.ToLower(strings.TrimRight(name, " \t")), raw: line})
	}
	return fields, msg[end+4:], nil
}

// headerKeys — значение h= письма: [signedHeaders] и List-Unsubscribe, если
// он есть. From — ровно один (RFC 5322 §3.6): иначе подпись d= домена From
// не о чем.
func headerKeys(fields []field) ([]string, error) {
	from, unsub := 0, false
	for _, f := range fields {
		switch f.name {
		case "from":
			from++
		case listUnsubscribe:
			unsub = true
		}
	}
	if from != 1 {
		return nil, fmt.Errorf("%w: заголовков From %d, ожидался 1", ErrMessage, from)
	}
	keys := append([]string(nil), signedHeaders...)
	if unsub {
		keys = append(keys, listUnsubscribe)
	}
	return keys, nil
}

// pick — поля в порядке h= (RFC 6376 §5.4.2): каждое имя берёт последнее
// ещё не взятое вхождение снизу; имя без вхождения не даёт ничего.
func pick(fields []field, keys []string) []string {
	used := make([]bool, len(fields))
	var out []string
	for _, k := range keys {
		for i := len(fields) - 1; i >= 0; i-- {
			if !used[i] && fields[i].name == k {
				used[i] = true
				out = append(out, fields[i].raw)
				break
			}
		}
	}
	return out
}

// signatureField — поле DKIM-Signature с пустым b= (без завершающего CRLF):
// теги переносятся строками продолжения, h= — по двоеточиям.
func signatureField(domain, selector string, keys []string, bh string) string {
	var b strings.Builder
	b.WriteString("DKIM-Signature: v=1; a=rsa-sha256; c=relaxed/relaxed; d=" + domain + "; s=" + selector + ";\r\n")
	b.WriteString("\th=")
	width := len("\th=")
	for i, k := range keys {
		item := k
		if i < len(keys)-1 {
			item += ":"
		} else {
			item += ";"
		}
		if width+len(item) > foldWidth {
			b.WriteString("\r\n\t")
			width = 1
		}
		b.WriteString(item)
		width += len(item)
	}
	b.WriteString("\r\n\tbh=" + fold(bh, len("\tbh=")) + ";\r\n")
	b.WriteString("\tb=")
	return b.String()
}

// fold — значение base64, перенесённое строками продолжения по [foldWidth];
// first — ширина строки до значения. FWS внутри b= и bh= допустим и при
// проверке снимается (RFC 6376 §3.5).
func fold(v string, first int) string {
	var b strings.Builder
	room := foldWidth - first
	for len(v) > room {
		b.WriteString(v[:room] + "\r\n\t")
		v = v[room:]
		room = foldWidth - 1
	}
	b.WriteString(v)
	return b.String()
}

// relaxedHeader — канонизация relaxed поля заголовка (RFC 6376 §3.4.2):
// имя в нижнем регистре, продолжения склеены, пробельные последовательности —
// один пробел, пробелы по краям значения и у двоеточия сняты.
func relaxedHeader(raw string) string {
	name, value, _ := strings.Cut(raw, ":")
	value = strings.ReplaceAll(value, "\r\n", "")
	return strings.ToLower(strings.TrimRight(name, " \t")) + ":" + strings.TrimSpace(collapseWSP(value)) + "\r\n"
}

// relaxedBody — канонизация relaxed тела (RFC 6376 §3.4.4): пробелы в конце
// строк сняты, пробельные последовательности — один пробел, пустые строки в
// конце сняты; непустое тело оканчивается CRLF, пустое — пусто.
func relaxedBody(body []byte) []byte {
	lines := strings.Split(string(body), "\r\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(collapseWSP(l), " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

// collapseWSP — каждая последовательность пробелов и табуляций — один пробел.
func collapseWSP(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	ws := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' {
			ws = true
			continue
		}
		if ws {
			b.WriteByte(' ')
			ws = false
		}
		b.WriteByte(c)
	}
	if ws {
		b.WriteByte(' ')
	}
	return b.String()
}
