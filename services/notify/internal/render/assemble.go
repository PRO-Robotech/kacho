// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package render

import (
	"bytes"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"
)

// envelope — заголовки письма, собранные до сборки частей.
type envelope struct {
	from    string
	to      string
	subject string
	date    time.Time
	// digest — rowDigest строки: Message-ID и граница частей.
	digest string
	// domain — домен отправителя: правая часть Message-ID.
	domain string
}

// assemble собирает письмо multipart/alternative из text/plain и text/html,
// обе части — utf-8; кодировка части — [partEncoding]. Message-ID — Р14:
// <rowDigest@домен отправителя>. Граница частей выводится из того же
// rowDigest и начинается с «=_»: в теле quoted-printable такой
// последовательности не бывает (знак «=» там — «=3D» либо мягкий перенос), а
// часть 7bit, в чьём тексте граница встречается, идёт quoted-printable; письмо
// детерминировано без генератора случайных чисел.
func assemble(e envelope, text, html string) ([]byte, error) {
	boundary := "=_alt_" + e.digest
	var b bytes.Buffer
	header := func(name, value string) { b.WriteString(name + ": " + value + "\r\n") }
	header("From", e.from)
	header("To", e.to)
	header("Subject", e.subject)
	header("Date", e.date.Format(time.RFC1123Z))
	header("Message-ID", "<"+e.digest+"@"+e.domain+">")
	header("Auto-Submitted", "auto-generated")
	header("MIME-Version", "1.0")
	// параметр границы — строкой продолжения: строка заголовка ≤ 78
	header("Content-Type", strings.Replace(
		mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": boundary}), "; ", ";"+fold, 1))
	b.WriteString("\r\n")
	for _, p := range []struct{ mediaType, body string }{
		{"text/plain", text},
		{"text/html", html},
	} {
		b.WriteString("--" + boundary + "\r\n")
		header("Content-Type", mime.FormatMediaType(p.mediaType, map[string]string{"charset": "utf-8"}))
		cte := partEncoding(p.body, boundary)
		header("Content-Transfer-Encoding", cte)
		b.WriteString("\r\n")
		if cte == cte7bit {
			write7bit(&b, p.body)
		} else if err := writeQP(&b, p.body); err != nil {
			return nil, fmt.Errorf("render: часть %s: %w", p.mediaType, err)
		}
		b.WriteString("\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes(), nil
}

// Кодировки части, которые выпускает сборщик. `8bit` и `binary` он не
// выпускает: окончательные байты письма — 7bit до подписи DKIM, и
// ретранслятору нечего перекодировать после неё (подпись осталась бы над
// другими байтами).
const (
	cte7bit = "7bit"
	cteQP   = "quoted-printable"
)

// maxLine7bit — предел строки 7bit-части без CRLF (RFC 5322 §2.1.1).
const maxLine7bit = 998

// partEncoding — `7bit`, если текст части представим как есть: каждый октет
// ASCII без NUL и одиночного CR, строки не длиннее [maxLine7bit] и ни одна не
// начинается с разделителя частей; иначе — `quoted-printable`.
func partEncoding(body, boundary string) string {
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c >= 0x80 || c == 0 || (c == '\r' && (i+1 == len(body) || body[i+1] != '\n')) {
			return cteQP
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if len(line) > maxLine7bit || strings.HasPrefix(line, "--"+boundary) {
			return cteQP
		}
	}
	return cte7bit
}

// write7bit — тело 7bit-части как есть, переводы строк CRLF.
func write7bit(b *bytes.Buffer, body string) {
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
}

// writeQP — тело части в quoted-printable: строки ≤ 76, переводы строк CRLF,
// октеты ≥ 0x80 — «=XX».
func writeQP(b *bytes.Buffer, body string) error {
	w := quotedprintable.NewWriter(b)
	if _, err := w.Write([]byte(strings.ReplaceAll(body, "\r\n", "\n"))); err != nil {
		return err
	}
	return w.Close()
}
