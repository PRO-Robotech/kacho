// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrationchains

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// DatabaseOf — имя базы, названное самим DSN, в обеих записях, которыми зовут
// точку наката: URL (`postgres://…/<база>`) и ключевая форма libpq
// (`… dbname=<база> …`, её собирает манифест развёртывания).
//
// Имя берётся ТОЛЬКО из строки: окружение (`PGDATABASE`) и умолчание «база =
// пользователь» источником не являются — точка, выбирающая цепочку по имени
// базы, иначе выбирала бы её по третьему месту решения. DSN без имени базы —
// отказ, а не пустое имя. Соединения функция не открывает и конфигурации
// драйвера не строит: разбор конфигурации соединения принадлежит общему
// тракту наката (`corelib/migratorcli`).
func DatabaseOf(dsn string) (string, error) {
	s := strings.TrimSpace(dsn)
	if strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", fmt.Errorf("DSN не разобран как URL: %w", err)
		}
		name := strings.TrimPrefix(u.Path, "/")
		if name == "" || strings.Contains(name, "/") {
			return "", errors.New("DSN не называет базу (путь URL пуст)")
		}
		return url.PathUnescape(name)
	}
	kv, err := keywordPairs(s)
	if err != nil {
		return "", err
	}
	name, ok := kv["dbname"]
	if !ok || name == "" {
		return "", errors.New("DSN не называет базу (ключа dbname нет)")
	}
	return name, nil
}

// keywordPairs разбирает ключевую форму libpq: пары `ключ = значение`,
// значение — слово либо строка в одинарных кавычках с экранированием `\`.
func keywordPairs(s string) (map[string]string, error) {
	out := map[string]string{}
	i := 0
	skip := func() {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
			i++
		}
	}
	for {
		skip()
		if i >= len(s) {
			return out, nil
		}
		start := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		key := s[start:i]
		skip()
		if key == "" || i >= len(s) || s[i] != '=' {
			return nil, fmt.Errorf("DSN не разобран: у ключа %q нет `=`", key)
		}
		i++
		skip()
		var val strings.Builder
		if i < len(s) && s[i] == '\'' {
			i++
			closed := false
			for i < len(s) {
				c := s[i]
				if c == '\\' && i+1 < len(s) {
					val.WriteByte(s[i+1])
					i += 2
					continue
				}
				i++
				if c == '\'' {
					closed = true
					break
				}
				val.WriteByte(c)
			}
			if !closed {
				return nil, fmt.Errorf("DSN не разобран: значение ключа %q без закрывающей кавычки", key)
			}
		} else {
			for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				val.WriteByte(s[i])
				i++
			}
		}
		out[key] = val.String()
	}
}
