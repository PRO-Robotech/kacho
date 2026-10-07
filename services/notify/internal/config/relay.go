// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/corelib/notify/address"
)

// RelayAddress — разобранный адрес ретранслятора `notify.smtp.connectionURI`
// (Д44, CX1-77). Читатель один — набор соединения SMTP корня: узел и порт —
// адрес соединения, режим — способ установки TLS, узел — имя проверки
// сертификата, имя — имя `AUTH`.
type RelayAddress struct {
	// Host — узел адреса: непустое имя DNS (RFC 1123) либо IP-литерал (без
	// квадратных скобок).
	Host string
	// Port — порт только из адреса, в [1..65535].
	Port int
	// ImplicitTLS — схема `smtps`; ложь — `smtp`, STARTTLS обязателен.
	ImplicitTLS bool
	// Username — раскодированное имя пользователя адреса; пусто — адрес без
	// userinfo (сессия без `AUTH`).
	Username string
}

// relaySchemes — закрытая таблица «схема → режим TLS». Строки `default` нет:
// схема вне таблицы — отказ (CX1-77).
var relaySchemes = map[string]bool{
	"smtp":  false, // STARTTLS
	"smtps": true,  // неявный TLS
}

// relayFieldError — нарушение одного поля адреса. Текст называет поле и не
// несёт ни адреса, ни учётной части.
type relayFieldError struct {
	field string
	why   string
}

func (e relayFieldError) String() string {
	return "адрес ретранслятора, поле «" + e.field + "»: " + e.why
}

// parseRelayURI — закрытый разбор адреса ретранслятора (З20 «Узел почты», §8):
// схема ровно `smtp`/`smtps`; узел — имя DNS либо IP-литерал; порт явный, в
// [1..65535]; без пароля, параметров запроса, пути и фрагмента; userinfo —
// только с непустым раскодированным именем.
func parseRelayURI(s string) (RelayAddress, *relayFieldError) {
	u, err := url.Parse(s)
	if err != nil {
		return RelayAddress{}, &relayFieldError{"адрес", "не разбирается: " + redactURLError(err).Error()}
	}
	implicit, known := relaySchemes[u.Scheme]
	if !known {
		return RelayAddress{}, &relayFieldError{"схема", "значение " + strconv.Quote(u.Scheme) +
			" вне таблицы режимов: smtp — STARTTLS, smtps — неявный TLS"}
	}
	if u.Opaque != "" {
		return RelayAddress{}, &relayFieldError{"узел", "адрес без «//» после схемы — узла нет"}
	}
	host := u.Hostname()
	switch {
	case host == "":
		return RelayAddress{}, &relayFieldError{"узел", "пуст"}
	case !relayHostForm(host):
		return RelayAddress{}, &relayFieldError{"узел", "не имя DNS (RFC 1123) и не IP-литерал"}
	}
	if u.Port() == "" {
		return RelayAddress{}, &relayFieldError{"порт", "не задан: порт берётся только из адреса, по схеме он не подставляется"}
	}
	port, perr := parsePort(u.Port())
	if perr != nil {
		return RelayAddress{}, &relayFieldError{"порт", perr.Error()}
	}
	if _, has := u.User.Password(); has {
		return RelayAddress{}, &relayFieldError{"пароль", "пароль в адресе не допускается — удостоверение " +
			"приходит ручкой notify.smtp.credential"}
	}
	if u.User != nil && u.User.Username() == "" {
		return RelayAddress{}, &relayFieldError{"имя пользователя", "пустое: адрес с «@» без имени — не «имени нет»"}
	}
	if u.RawQuery != "" || u.ForceQuery {
		return RelayAddress{}, &relayFieldError{"запрос", "параметры запроса не допускаются: параметров, " +
			"меняющих шифрование или проверку, у notify нет"}
	}
	if (u.Path != "" && u.Path != "/") || (u.RawPath != "" && u.RawPath != "/") {
		return RelayAddress{}, &relayFieldError{"путь", "путь не допускается (только пустой либо «/»)"}
	}
	if u.Fragment != "" || strings.Contains(s, "#") {
		return RelayAddress{}, &relayFieldError{"фрагмент", "фрагмент не допускается"}
	}
	return RelayAddress{Host: host, Port: port, ImplicitTLS: implicit, Username: u.User.Username()}, nil
}

// relayHostForm — IP-литерал либо имя DNS по RFC 1123: метки
// `[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?` через точку, всего ≤ 253.
func relayHostForm(h string) bool {
	if net.ParseIP(h) != nil {
		return true
	}
	if len(h) > 253 {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, r := range l {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// Relay — разобранный адрес ретранслятора; годен только после успешного
// [Config.Validate].
func (c Config) Relay() RelayAddress { return c.relay }

// FromDomain — домен адреса отправителя в форме `address.NormalizeDomain`:
// домен `From` и домен проверок DNS установки (Д101). Годен только после
// успешного [Config.Validate].
func (c Config) FromDomain() string { return c.fromDomain }

// validateRelay — страж адреса ретранслятора (Д44): незаданный уже назван
// общим перебором; пустой и нарушивший поле — отказ с именем ручки и поля.
// Пароль в текст отказа не попадает: адрес в тексте не повторяется.
func (c *Config) validateRelay(fs *findings) {
	k := KnobOfField("SMTPConnectionURI")
	if c.unset[k.Env] {
		return
	}
	if c.SMTPConnectionURI == "" {
		fs.add(k, "значение пусто; умолчания у адреса ретранслятора нет")
		return
	}
	r, ferr := parseRelayURI(c.SMTPConnectionURI)
	if ferr != nil {
		fs.add(k, "%s", ferr)
		return
	}
	c.relay, c.relayParsed = r, true
}

// validateFromAddress — адрес отправителя (З20 «Отправитель»): форма
// `notify/address` с непустым доменом. Значение адреса в текст отказа не
// попадает — пакет address ошибок со значением не выпускает.
func (c *Config) validateFromAddress(fs *findings) {
	k := KnobOfField("SMTPFromAddress")
	if c.unset[k.Env] {
		return
	}
	if c.SMTPFromAddress == "" {
		fs.add(k, "значение пусто")
		return
	}
	if _, err := address.Normalize(c.SMTPFromAddress); err != nil {
		fs.add(k, "значение вне формы адреса notify/address (локальная часть, «@», непустой домен): %v", err)
		return
	}
	at := strings.LastIndexByte(c.SMTPFromAddress, '@')
	d, err := address.NormalizeDomain(c.SMTPFromAddress[at+1:])
	if err != nil {
		fs.add(k, "значение вне формы адреса notify/address: домен: %v", err)
		return
	}
	v, err := d.Value()
	if err != nil {
		fs.add(k, "значение вне формы адреса notify/address: домен: %v", err)
		return
	}
	c.fromDomain = v
}

// validateStandDNS — ручка зоны DNS стенда (Д104): `off` либо хост узла почты.
// Иное — отказ с хостом узла без учётной части. Неразобранный адрес уже назван
// своей находкой и здесь не судится второй раз.
func (c *Config) validateStandDNS(fs *findings) {
	k := KnobOfField("StandDNS")
	if c.unset[k.Env] {
		return
	}
	switch {
	case c.StandDNS == "":
		fs.add(k, "значение пусто: ожидается off либо хост приёмника стенда")
	case c.StandDNS == "off":
	case !c.relayParsed:
	case !strings.EqualFold(c.StandDNS, c.relay.Host):
		fs.add(k, "зона DNS стенда при узле почты не приёмник стенда: значение %q не равно хосту узла почты %q "+
			"(ручка %s)", c.StandDNS, c.relay.Host, KnobOfField("SMTPConnectionURI"))
	}
}
