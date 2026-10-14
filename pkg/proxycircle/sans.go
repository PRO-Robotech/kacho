// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package proxycircle

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// ParseLinkSANs разбирает ИМЕНА ЗВЕНЬЕВ В СЕРТИФИКАТЕ (kacho#3028, C4): имена,
// которые звено фронта несёт в клиентском сертификате, проверенном якорем
// установки. Адрес пода — не личность (под с теми же метками попадает в службу
// фронта, адрес ушедшего пода выдаётся другому); имя в сертификате выдаёт
// только тот, кто выпускает сертификаты звеньям.
//
// Одно суждение на двух читателей: край разбирает им ручку на старте, гейт
// рендера — значение, которое ручке выписывает каждая цепочка.
//
// Законно: пустой ввод (имён нет); имя SPIFFE `spiffe://<домен доверия>/<путь>`
// без порта, запроса, фрагмента и учётных данных; имя DNS по RFC 1123 в нижнем
// регистре. Повтор читается один раз, порядок объявления сохраняется.
//
// Отказ: подстановочный знак (совпал бы с сертификатом, которого звену не
// выдавали), адрес (адрес — не личность), URI другой схемы, SPIFFE без домена
// доверия или без пути (имя домена целиком — любая нагрузка установки).
func ParseLinkSANs(raw string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if err := checkLinkSAN(name); err != nil {
			return nil, fmt.Errorf("имя звена %q: %v", name, err)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

func checkLinkSAN(name string) error {
	if strings.Contains(name, "*") {
		return fmt.Errorf("подстановочный знак совпал бы с сертификатом, которого звену не выдавали")
	}
	if _, err := netip.ParseAddr(name); err == nil {
		return fmt.Errorf("адрес — не личность звена")
	}
	if !strings.Contains(name, "://") {
		if err := checkServiceName(name); err != nil {
			return fmt.Errorf("не имя DNS: %v", err)
		}
		return nil
	}
	u, err := url.Parse(name)
	switch {
	case err != nil:
		return fmt.Errorf("не URI: %v", err)
	case u.Scheme != "spiffe":
		return fmt.Errorf("схема %q — имя звена либо SPIFFE, либо DNS", u.Scheme)
	case u.Host == "" || u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return fmt.Errorf("SPIFFE без домена доверия либо с портом, запросом, фрагментом или учётными данными")
	case u.Path == "" || u.Path == "/":
		return fmt.Errorf("SPIFFE без пути — это весь домен доверия, а не звено")
	}
	return checkServiceName(u.Host)
}
