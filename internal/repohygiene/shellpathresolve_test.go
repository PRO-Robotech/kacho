// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// shellpathresolve_test.go — РАЗРЕШЕНИЕ пути цели записи и отсев путей, которые
// дерево само объявило своей свалкой (гейт изоляции shell-проб, #724).
//
// # Зачем это отдельно от происхождения
//
// Происхождение отвечает «откуда взялось значение», и одного его мало: проба
// законно кладёт СВОИ артефакты рядом с собой (`tests/k6/results/`,
// `tests/authz-fixtures/out/`), и путь туда тоже произведён от корня. Вреда в
// этом нет по построению: состав корпуса гейты берут у ИНДЕКСА, а такие
// каталоги дерево объявило игнорируемыми — прерванный прогон не оставит там ни
// изменённого отслеживаемого файла, ни фантомной записи в индексе, то есть не
// сделает лживой ни одну проверку.
//
// Отсев ведётся НЕ списком исключений, а фактом дерева: `.gitignore` рядом с
// самим каталогом. Исключение истекает само — снимут объявление, и путь тут же
// станет находкой. Замер, ради которого это написано: без отсева гейт давал на
// чистом стволе 3 находки, из них 0 настоящих; гейт с ложным срабатыванием
// снимают первым.
//
// # Чего разрешение НЕ умеет — названо
//
// Путь, вычислимый только в рантайме (имя из вывода команды, подстановка по
// маске, значение из окружения без умолчания), остаётся НЕразрешённым. Такая
// цель считается находкой, если её происхождение живое: гейт предпочитает
// красное молчанию там, где не может доказать безопасность.
package repohygiene

import (
	"path"
	"strings"
)

// shellResolve — буквальный путь выражения относительно корня дерева, если он
// вычислим статически. Пустая строка означает «неизвестно», а не «корень».
//
// Опоры ровно три, и все три — то же, чем определяется живой корень:
// путь самого скрипта (`$0`, `${BASH_SOURCE[…]}`), значение уже разрешённой
// переменной и подстановка, сохраняющая путь (`cd … && pwd`, `dirname`,
// `git rev-parse --show-toplevel`).
func (e *shellEnv) shellResolve(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '$' && c != '`' {
			b.WriteByte(c)
			i++
			continue
		}
		if c == '`' {
			return "" // старая форма подстановки: разрешать не беремся
		}
		if i+1 >= len(s) {
			return ""
		}
		switch s[i+1] {
		case '(':
			inner, after, ok := shellCutSubst(s[i+2:], '(', ')')
			if !ok {
				return ""
			}
			v := e.shellResolveSubst(inner)
			if v == "" {
				return ""
			}
			b.WriteString(v)
			s, i = after, 0
		case '{':
			end := strings.IndexByte(s[i+1:], '}')
			if end < 0 {
				return ""
			}
			v := e.shellResolveBrace(s[i+2 : i+1+end])
			if v == "" {
				return ""
			}
			b.WriteString(v)
			i = i + 1 + end + 1
		default:
			j := i + 1
			for j < len(s) && isShNameByte(s[j]) {
				j++
			}
			if j == i+1 {
				return ""
			}
			v := e.shellVarValue(s[i+1 : j])
			if v == "" {
				return ""
			}
			b.WriteString(v)
			i = j
		}
	}
	return b.String()
}

// shellResolveBrace — содержимое `${…}`: имя, индекс массива, умолчание.
func (e *shellEnv) shellResolveBrace(in string) string {
	// `${VAR:-умолчание}` / `${VAR-…}` / `${VAR:=…}`: переменная приходит из
	// окружения, и статически известно только умолчание — а именно оно и
	// называет путь В ДЕРЕВЕ, ради которого разрешение затевалось.
	for _, sep := range []string{":-", ":=", "-", "="} {
		if k := strings.Index(in, sep); k > 0 {
			name := in[:k]
			if v := e.shellVarValue(name); v != "" {
				return v
			}
			return e.shellResolve(in[k+len(sep):])
		}
	}
	if k := strings.IndexByte(in, '['); k > 0 { // ${BASH_SOURCE[0]}, ${ARR[i]}
		return e.shellVarValue(in[:k])
	}
	return e.shellVarValue(in)
}

func (e *shellEnv) shellVarValue(name string) string {
	switch name {
	case "0", "BASH_SOURCE":
		return e.selfPath
	}
	return e.vals[name]
}

// shellResolveSubst — подстановка, сохраняющая путь.
func (e *shellEnv) shellResolveSubst(inner string) string {
	if strings.Contains(inner, "rev-parse") && strings.Contains(inner, "--show-toplevel") {
		return "."
	}
	cmds, _ := shellParse(inner)
	var last string
	for _, c := range cmds {
		if len(c.words) == 0 {
			continue
		}
		switch shellBase(c.words[0].lit) {
		case "cd":
			for _, w := range c.words[1:] {
				if w.lit == "--" {
					continue
				}
				if v := e.shellResolve(w.exp); v != "" {
					last = path.Clean(v)
				} else {
					return ""
				}
			}
		case "dirname":
			for _, w := range c.words[1:] {
				if w.lit == "--" {
					continue
				}
				if v := e.shellResolve(w.exp); v != "" {
					last = path.Dir(path.Clean(v))
				} else {
					return ""
				}
			}
		case "pwd", "realpath", "readlink":
			// Значение уже собрано предыдущим `cd`; сама по себе команда пути
			// не добавляет.
		default:
			return ""
		}
	}
	return last
}

// shellCleanRel — путь цели относительно корня дерева, если он внутри дерева.
// Пустая строка означает «не установлено» либо «наружу от корня».
func shellCleanRel(p string) string {
	if p == "" {
		return ""
	}
	if strings.ContainsAny(p, "*?") {
		return "" // маска: конкретной цели нет
	}
	c := path.Clean(p)
	if strings.HasPrefix(c, "/") || c == ".." || strings.HasPrefix(c, "../") {
		return ""
	}
	return c
}
