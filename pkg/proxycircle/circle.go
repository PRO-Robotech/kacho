// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// Package proxycircle — круг доверенных звеньев адреса клиента: сети, от
// TCP-пиров из которых край принимает заголовок пересылки (kacho#3028).
//
// Одно суждение на двух читателей: край разбирает им ручку
// KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS на старте, гейт рендера —
// значение, которое ручке выписывает каждая цепочка развёртывания. Два
// выражения «что законно в круге» по разным файлам разошлись бы молча, и
// гейт зеленел бы на том, на чём край отказывает в старте, или наоборот.
//
// Что законно:
//   - пустой круг — «не доверяю никому»: источник — сам TCP-пир. Это умолчание,
//     а не «не сужаю»;
//   - сеть, целиком лежащая в одном из частных диапазонов (NonPublicRanges):
//     звено перед краем — под кластера, и его сеть всегда частная.
//
// Что отказ: неразборная запись (молча выпав из круга, она превратила бы своё
// звено в «чужого») и сеть, задевающая хоть один адрес вне частных
// диапазонов, — доверие заголовку от пира снаружи кластера.
package proxycircle

import (
	"fmt"
	"net/netip"
	"strings"
)

// nonPublic — диапазоны, в которых живёт сеть подов: RFC 1918, RFC 6598
// (общее адресное пространство провайдера), уникальные локальные IPv6
// (RFC 4193). Петли здесь нет: проброс порта приходит к процессу с петли, и
// доверие ей отдало бы заголовок любому, кто пробрасывает порт.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fc00::/7"),
}

// NonPublicRanges — копия перечня частных диапазонов.
func NonPublicRanges() []netip.Prefix { return append([]netip.Prefix(nil), nonPublic...) }

// Parse разбирает круг: сети через запятую, пробелы и пустые элементы
// допустимы. Пустой ввод — пустой круг без ошибки.
func Parse(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		entry := strings.TrimSpace(item)
		if entry == "" {
			continue
		}
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("запись %q — не сеть в записи CIDR (%v)", entry, err)
		}
		p = p.Masked()
		if !insideNonPublic(p) {
			return nil, fmt.Errorf("сеть %q выходит за частные диапазоны %v — заголовок адреса принимался бы "+
				"от пира вне кластера", entry, nonPublic)
		}
		out = append(out, p)
	}
	return out, nil
}

func insideNonPublic(p netip.Prefix) bool {
	for _, r := range nonPublic {
		if r.Addr().Is4() == p.Addr().Is4() && r.Bits() <= p.Bits() && r.Contains(p.Addr()) {
			return true
		}
	}
	return false
}
