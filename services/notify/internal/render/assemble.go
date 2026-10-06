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
// обе части — utf-8 в quoted-printable. Message-ID — Р14:
// <rowDigest@домен отправителя>. Граница частей выводится из того же
// rowDigest и начинается с «=_»: в теле quoted-printable такой
// последовательности не бывает (знак «=» там — «=3D» либо мягкий перенос), и
// письмо детерминировано без генератора случайных чисел.
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
		header("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := writeQP(&b, p.body); err != nil {
			return nil, fmt.Errorf("render: часть %s: %w", p.mediaType, err)
		}
		b.WriteString("\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes(), nil
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
