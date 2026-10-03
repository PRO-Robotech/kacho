// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package issuercanon — ЕДИНСТВЕННОЕ место, где значение издателя токена
// приводится к канонической форме (приёмка KA1, Р5; kacho#2758).
//
// # Предмет
//
// Издатель приходит с двух сторон: из ОБЪЯВЛЕНИЯ края (перечень издателей,
// ключи привязки к наборам, наш издатель) и из `iss` предъявленного токена,
// который штампует служба. Обе стороны пишут одного издателя по-разному —
// завершающая косая, регистр схемы и хоста, порт по умолчанию, завершающая
// точка хоста, процентное кодирование, — и точное сравнение строк молча
// отвергает токен, который обязан приниматься, при зелёном положительном пути на
// той форме, что совпала случайно. Починка одного представителя класс не
// снимает: класс снимает один канон на пути сравнения ОБЕИХ сторон. Второе место
// нормализации воспроизвело бы тот же класс между двумя канонами.
//
// # Канон
//
// Абсолютный URL со схемой `http` или `https`, в котором: схема и хост в нижнем
// регистре; порт по умолчанию схемы (`:443` для `https`, `:80` для `http`) снят;
// завершающая точка хоста снята; процентное кодирование незарезервированных
// символов (RFC 3986 §2.3) раскрыто, а шестнадцатеричные цифры остальных кодов —
// в верхнем регистре; завершающая `/` пути снята (пустой путь и `/` — одно).
// Путь, запрос и всё прочее регистра не теряют. Закодированный зарезервированный
// символ (`%2F`) остаётся закодированным: раскрыть его значило бы назвать один
// издатель другим.
//
// Канон выбирает ЗАПИСЬ приёма; ключ проверки подписи по-прежнему берётся только
// из набора выбранной записи, поэтому канон круга подписантов не расширяет.
package issuercanon

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Canonical приводит значение издателя к канонической форме. Значение, не
// являющееся абсолютным http(s)-URL, — ошибка: такому издателю записи быть не
// может, и сравнивать его не с чем.
func Canonical(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("issuer %q is not a parseable URL: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Host == "" || u.Opaque != "" {
		return "", fmt.Errorf("issuer %q is not an absolute http(s) URL", raw)
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return "", fmt.Errorf("issuer %q names no host", raw)
	}
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	hostport := host
	if strings.Contains(host, ":") { // IPv6-литерал
		hostport = "[" + host + "]"
	}
	if port != "" {
		hostport = net.JoinHostPort(host, port)
	}
	path, err := normalizePercent(u.EscapedPath())
	if err != nil {
		return "", fmt.Errorf("issuer %q: %w", raw, err)
	}
	path = strings.TrimSuffix(path, "/")
	out := scheme + "://"
	if u.User != nil {
		out += u.User.String() + "@"
	}
	out += hostport + path
	if u.ForceQuery || u.RawQuery != "" {
		q, qerr := normalizePercent(u.RawQuery)
		if qerr != nil {
			return "", fmt.Errorf("issuer %q: %w", raw, qerr)
		}
		out += "?" + q
	}
	if u.Fragment != "" {
		f, ferr := normalizePercent(u.EscapedFragment())
		if ferr != nil {
			return "", fmt.Errorf("issuer %q: %w", raw, ferr)
		}
		out += "#" + f
	}
	return out, nil
}

// Equal — один ли это издатель. Значение, не приводимое к канону, не равно
// ничему, в том числе самому себе: сравнивать его не с чем.
func Equal(a, b string) bool {
	ca, err := Canonical(a)
	if err != nil {
		return false
	}
	cb, err := Canonical(b)
	return err == nil && ca == cb
}

// normalizePercent раскрывает коды незарезервированных символов и поднимает
// регистр шестнадцатеричных цифр остальных (RFC 3986 §6.2.2.1, §6.2.2.2).
func normalizePercent(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
			return "", fmt.Errorf("malformed percent-encoding at offset %d", i)
		}
		c := unhex(s[i+1])<<4 | unhex(s[i+2])
		if isUnreserved(c) {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(s[i+1:i+3]))
		}
		i += 2
	}
	return b.String(), nil
}

func isUnreserved(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}
