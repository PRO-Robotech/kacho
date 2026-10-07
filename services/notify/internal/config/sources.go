// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

// RecipientForm — форма адресата, которую notify принимает из ленты источника.
// В NTF-1 форма одна — адрес (приёмка Р6); адресация субъекта — NTF-3.
type RecipientForm string

// RecipientAddress — адресат задан адресом почты.
const RecipientAddress RecipientForm = "address"

// RecipientForms — закрытый перечень форм адресата.
func RecipientForms() []RecipientForm { return []RecipientForm{RecipientAddress} }

// Authorization — чем notify подтверждает право источника на письмо.
type Authorization string

const (
	// AuthorizationResolveSend — `ResolveSend` у kaname на каждую строку.
	AuthorizationResolveSend Authorization = "resolveSend"
	// AuthorizationCertificate — записанное исключение пространства `kaname`
	// (приёмка Р3): точный SAN сервера ленты вместо `ResolveSend`. Кому оно
	// допустимо, судит страж полосы перечня источников (NTF1-G22).
	AuthorizationCertificate Authorization = "certificate"
)

// Authorizations — закрытый перечень значений `authorization`.
func Authorizations() []Authorization {
	return []Authorization{AuthorizationResolveSend, AuthorizationCertificate}
}

// Source — запись перечня источников (З20, NTF1-G01).
type Source struct {
	// Module — пространство источника: имя его ленты и первая часть имени
	// шаблона строки (`<модуль>/<имя>`).
	Module string
	// FeedAddr — адрес сервера ленты источника (`узел:порт`).
	FeedAddr string
	// SAN — точный URI SPIFFE сервера ленты из декларации `<модуль>.spiffe`.
	SAN string
	// Classes — классы писем, которые notify забирает у источника.
	Classes []feed.Class
	// RecipientForms — формы адресата, которые notify принимает у источника;
	// пустой перечень законен (поле при этом обязано быть).
	RecipientForms []RecipientForm
	// Authorization — чем подтверждается право источника на письмо.
	Authorization Authorization
}

// SourceRecordFields — поля записи перечня, все обязательные, в порядке
// объявления. Единственный перечень: по нему разбор требует наличия поля и
// отвергает лишнее.
func SourceRecordFields() []string {
	return []string{"module", "feedAddr", "san", "classes", "recipientForms", "authorization"}
}

// validateSources разбирает перечень строго: пустой перечень, запись без поля,
// лишнее поле, значение вне закрытого перечня и повтор модуля — отказ старта с
// именем ручки, номером записи, модулем и полем.
func (c *Config) validateSources(fs *findings) {
	k := KnobOfField("Sources")
	if c.unset[k.Env] {
		return
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(c.Sources), &raw); err != nil {
		fs.add(k, "перечень источников не разбирается как JSON-массив записей: %v", err)
		return
	}
	if len(raw) == 0 {
		fs.add(k, "перечень источников пуст: notify без источника поднимать не для чего, а чарт "+
			"рендерит notify только при непустом перечне (NTF1-N04)")
		return
	}
	var out []Source
	seen := map[string]bool{}
	bad := false
	for i, rec := range raw {
		src, problems := parseSourceRecord(rec)
		where := fmt.Sprintf("запись #%d", i+1)
		if src.Module != "" {
			where += fmt.Sprintf(" (модуль %q)", src.Module)
		}
		for _, p := range problems {
			fs.add(k, "%s: %s", where, p)
			bad = true
		}
		if len(problems) > 0 {
			continue
		}
		if seen[src.Module] {
			fs.add(k, "%s: модуль %q назван в перечне дважды", where, src.Module)
			bad = true
			continue
		}
		seen[src.Module] = true
		out = append(out, src)
	}
	if !bad {
		c.sources = out
	}
}

// parseSourceRecord разбирает одну запись. Модуль возвращается и при прочих
// находках — им отказ адресует запись.
func parseSourceRecord(rec map[string]json.RawMessage) (Source, []string) {
	var (
		src      Source
		problems []string
	)
	fields := SourceRecordFields()
	extra := make([]string, 0)
	for key := range rec {
		if !slices.Contains(fields, key) {
			extra = append(extra, key)
		}
	}
	slices.Sort(extra)
	for _, key := range extra {
		problems = append(problems, fmt.Sprintf("лишнее поле %q — у записи только %s",
			key, strings.Join(fields, ", ")))
	}
	present := func(key string) (json.RawMessage, bool) {
		v, ok := rec[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			problems = append(problems, fmt.Sprintf("нет поля %q", key))
			return nil, false
		}
		return v, true
	}
	str := func(key string, raw json.RawMessage) (string, bool) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			problems = append(problems, fmt.Sprintf("поле %q не строка", key))
			return "", false
		}
		return s, true
	}
	list := func(key string, raw json.RawMessage) ([]string, bool) {
		var l []string
		if err := json.Unmarshal(raw, &l); err != nil {
			problems = append(problems, fmt.Sprintf("поле %q не массив строк", key))
			return nil, false
		}
		return l, true
	}

	if raw, ok := present("module"); ok {
		if s, ok := str("module", raw); ok {
			if !isDNSLabel(s) {
				problems = append(problems, fmt.Sprintf("поле %q: %q не DNS-метка (строчные буквы, "+
					"цифры, дефис, до 63 знаков)", "module", s))
			} else {
				src.Module = s
			}
		}
	}
	if raw, ok := present("feedAddr"); ok {
		if s, ok := str("feedAddr", raw); ok {
			if err := checkHostPort(s); err != nil {
				problems = append(problems, fmt.Sprintf("поле %q: %v", "feedAddr", err))
			} else {
				src.FeedAddr = s
			}
		}
	}
	if raw, ok := present("san"); ok {
		if s, ok := str("san", raw); ok {
			if err := checkSPIFFE(s); err != nil {
				problems = append(problems, fmt.Sprintf("поле %q: %v", "san", err))
			} else {
				src.SAN = s
			}
		}
	}
	if raw, ok := present("classes"); ok {
		if l, ok := list("classes", raw); ok {
			src.Classes, problems = closedSet("classes", l, limits.NetClasses(), false, problems)
		}
	}
	if raw, ok := present("recipientForms"); ok {
		if l, ok := list("recipientForms", raw); ok {
			src.RecipientForms, problems = closedSet("recipientForms", l, RecipientForms(), true, problems)
		}
	}
	if raw, ok := present("authorization"); ok {
		if s, ok := str("authorization", raw); ok {
			if !slices.Contains(Authorizations(), Authorization(s)) {
				problems = append(problems, fmt.Sprintf("поле %q: %q вне перечня %v", "authorization", s, Authorizations()))
			} else {
				src.Authorization = Authorization(s)
			}
		}
	}
	return src, problems
}

// closedSet переводит перечень строк в значения закрытого перечня без
// повторов. allowEmpty — законен ли пустой перечень.
func closedSet[T ~string](field string, in []string, allowed []T, allowEmpty bool, problems []string) ([]T, []string) {
	if len(in) == 0 && !allowEmpty {
		return nil, append(problems, fmt.Sprintf("поле %q пусто: источник без значений этого поля не "+
			"даёт notify ничего", field))
	}
	out := make([]T, 0, len(in))
	for _, s := range in {
		v := T(s)
		if !slices.Contains(allowed, v) {
			return nil, append(problems, fmt.Sprintf("поле %q: %q вне перечня %v", field, s, allowed))
		}
		if slices.Contains(out, v) {
			return nil, append(problems, fmt.Sprintf("поле %q: %q назван дважды", field, s))
		}
		out = append(out, v)
	}
	return out, problems
}

func isDNSLabel(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func checkHostPort(s string) error {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return fmt.Errorf("%q не в форме узел:порт: %v", s, err)
	}
	if host == "" {
		return fmt.Errorf("%q: нет узла", s)
	}
	if _, err := parsePort(port); err != nil {
		return fmt.Errorf("%q: %v", s, err)
	}
	return nil
}

func checkSPIFFE(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("%q не разбирается: %v", s, redactURLError(err))
	}
	switch {
	case u.Scheme != "spiffe":
		return fmt.Errorf("%q: схема обязана быть spiffe — notify сверяет точный URI SAN сервера ленты", s)
	case u.Host == "":
		return fmt.Errorf("%q: нет домена доверия", s)
	case u.User != nil, u.Port() != "":
		return fmt.Errorf("%q: удостоверение и порт в SPIFFE ID не допускаются", s)
	case u.Path == "" || u.Path == "/":
		return fmt.Errorf("%q: нет пути рабочей нагрузки", s)
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(s, "#"):
		return fmt.Errorf("%q: параметры и фрагмент в SPIFFE ID не допускаются", s)
	}
	return nil
}
