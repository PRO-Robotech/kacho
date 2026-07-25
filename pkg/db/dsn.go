// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package db

import (
	"net/url"
	"strings"
)

// DefaultSSLMode — режим, который применяет libpq/pgx, когда DSN вообще не несёт
// `sslmode`. `prefer` допускает plaintext-fallback, поэтому отсутствие параметра
// НЕЛЬЗЯ рапортовать как защищённое соединение.
const DefaultSSLMode = "prefer"

// SSLModeFromDSN возвращает sslmode, который реально доезжает до пула.
//
// Читать сырое config-поле недостаточно: часть сервисов держит sslmode прямо в
// URL (`repository.postgres.url`), часть деривит его в composeDSN/baseDSN
// (пустое → `disable`), а третьи — комбинируют (raw-URL выигрывает у поля).
// Единственный честный источник — сама строка, отданная pgxpool.
//
// Возвращает НОРМАЛИЗОВАННЫЙ (lower-case) режим; пустой DSN → пустая строка.
// Никогда не возвращает ничего, кроме режима — DSN несёт пароль, а результат
// уходит в лог.
func SSLModeFromDSN(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return ""
	}
	// URL-форма (`postgres://…?sslmode=…`) — точный разбор query.
	if u, err := url.Parse(dsn); err == nil && u.Query().Has("sslmode") {
		if mode := strings.TrimSpace(u.Query().Get("sslmode")); mode != "" {
			return strings.ToLower(mode)
		}
	}
	// Fallback-скан: keyword-форма (`host=… sslmode=require`) и URL, который
	// url.Parse не осилил (напр. спецсимвол в пароле) — иначе реальный
	// `require` был бы отрапортован как `prefer`.
	if i := strings.Index(strings.ToLower(dsn), "sslmode="); i >= 0 {
		rest := dsn[i+len("sslmode="):]
		if j := strings.IndexAny(rest, "& \t"); j >= 0 {
			rest = rest[:j]
		}
		if mode := strings.TrimSpace(rest); mode != "" {
			return strings.ToLower(mode)
		}
	}
	return DefaultSSLMode
}
