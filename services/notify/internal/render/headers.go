// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/PRO-Robotech/corelib/notify/form"
)

// maxWordLen — длина одного слова RFC 2047 вместе с обрамлением. Предел
// стандарта — 75; меньше, чтобы первая строка с именем заголовка
// («Subject: ») уложилась в 78 знаков.
const maxWordLen = 66

// wordOpen, wordClose — обрамление слова RFC 2047 в кодировке Q.
const (
	wordOpen  = "=?utf-8?q?"
	wordClose = "?="
)

// fold — перенос строки заголовка: CRLF и пробел продолжения.
const fold = "\r\n "

// HeaderValue — значение заголовка из form.HeaderText. Нулевой
// form.HeaderText — ошибка с причиной form.ErrUnset, заголовка нет.
//
// Значение пишется как есть, только если это печатный ASCII без «=?», не
// длиннее слова; иначе — словами RFC 2047 в кодировке Q целиком, по одному
// на строку продолжения. Своё кодирование, а не mime.QEncoding: тот оставляет
// ASCII-значение как есть, и значение атрибута вида «=?utf-8?q?…?=» читатель
// почты раскрыл бы как слово — тема показала бы не то, что выписал источник.
func HeaderValue(h form.HeaderText) (string, error) {
	v, err := h.Value()
	if err != nil {
		return "", fmt.Errorf("render: значение заголовка: %w", err)
	}
	if isPrintableASCII(v) && !strings.Contains(v, "=?") && len(v) <= maxWordLen {
		return v, nil
	}
	return strings.Join(encodeWords(v), fold), nil
}

// phrase — отображаемое имя адреса (From). Как есть — только буквы, цифры и
// одиночные пробелы ASCII; иначе — слова RFC 2047: кавычки и спецсимволы
// RFC 5322 в имени отправителя не разбираются.
func phrase(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("render: имя отправителя: %w", form.ErrEmpty)
	}
	if len(name) <= maxWordLen && isPlainPhrase(name) {
		return name, nil
	}
	return strings.Join(encodeWords(name), fold), nil
}

// encodeWords кодирует s словами RFC 2047 (Q, utf-8). Каждое слово несёт
// целые символы и не длиннее maxWordLen. Пробел — «_», печатные
// буквы, цифры и «!*+-/» — как есть, прочие октеты — «=XX».
func encodeWords(s string) []string {
	const room = maxWordLen - len(wordOpen) - len(wordClose)
	var words []string
	var cur strings.Builder
	for _, r := range s {
		enc := qRune(r)
		if cur.Len()+len(enc) > room {
			words = append(words, wordOpen+cur.String()+wordClose)
			cur.Reset()
		}
		cur.WriteString(enc)
	}
	if cur.Len() > 0 {
		words = append(words, wordOpen+cur.String()+wordClose)
	}
	return words
}

func qRune(r rune) string {
	if r == ' ' {
		return "_"
	}
	if qSafe(r) {
		return string(r)
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	var b strings.Builder
	for _, c := range buf[:n] {
		fmt.Fprintf(&b, "=%02X", c)
	}
	return b.String()
}

// qSafe — символ, который кодировка Q в имени и в теме пишет как есть
// (RFC 2047 §5 (3)): буквы и цифры ASCII и «!*+-/».
func qSafe(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		r == '!' || r == '*' || r == '+' || r == '-' || r == '/'
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func isPlainPhrase(s string) bool {
	if s[0] == ' ' || s[len(s)-1] == ' ' || strings.Contains(s, "  ") {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != ' ' && !isAlnum(c) {
			return false
		}
	}
	return true
}

// isDotAtom — локальная часть, которая пишется без кавычек (RFC 5322
// dot-atom).
func isDotAtom(s string) bool {
	if s == "" || s[0] == '.' || s[len(s)-1] == '.' || strings.Contains(s, "..") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || isAlnum(c) || strings.IndexByte("!#$%&'*+-/=?^_`{|}~", c) >= 0 {
			continue
		}
		return false
	}
	return true
}

// quoteString — локальная часть строкой в кавычках (RFC 5322 quoted-string):
// «\» и «"» экранируются. Вход — печатный ASCII (asciiAddress).
func quoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' || s[i] == '"' {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('"')
	return b.String()
}

// rowDigest — hex(sha256(namespace ‖ 0x00 ‖ id))[:32]: левая часть
// Message-ID (Р14) и граница частей письма. Один и тот же для повторов одной
// строки.
func rowDigest(namespace, id string) string {
	sum := sha256.Sum256([]byte(namespace + "\x00" + id))
	return hex.EncodeToString(sum[:])[:32]
}
