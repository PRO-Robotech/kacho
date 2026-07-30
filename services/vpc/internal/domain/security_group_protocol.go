// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "strings"

// Протокол и диапазон портов SG-правила — закрытые множества, а не свободный текст.
//
// Контракт объявлен в самом сообщении правила: `protocol_name` — значение из
// реестра номеров протоколов IANA, `protocol_number` — номер оттуда же,
// `PortRange.from_port`/`to_port` — `0-65535`. Значение вне множества принять
// нельзя: правило с несуществующим протоколом или с портом вне диапазона
// продукт выразить не может, поэтому вызывающий обязан получить отказ, а не
// успех на правило, которое ничего не разрешает и ничего не запрещает.
//
// Escape-hatch для протокола, у которого нет ключевого слова, — вторая ветка
// того же oneof: `protocol_number`. Поэтому закрытый набор ИМЁН безопасен: он
// сужает написание, а не выразимость.

const (
	// AnyProtocolName — собственное значение продукта «любой протокол».
	// Его пишет builder default-SG каждой сети (`NewDefaultSecurityGroupRules`),
	// и оно же принимается от вызывающего (регистр не важен).
	AnyProtocolName = "ANY"

	// AnyPort — «любой порт» в границах диапазона правила. Обе границы
	// принимают его только вместе: полудиапазон «от любого до 80» смысла не
	// имеет и отвергается.
	AnyPort int64 = -1

	// MinPort / MaxPort — границы, объявленные в `PortRange` (`0-65535`).
	MinPort int64 = 0
	MaxPort int64 = 65535

	// MinProtocolNumber / MaxProtocolNumber — границы номера протокола IANA.
	// `AnyProtocolNumber` — собственный sentinel продукта «любой протокол»
	// (его пишет builder default-SG рядом с `AnyProtocolName`).
	AnyProtocolNumber int64 = -1
	MinProtocolNumber int64 = 0
	MaxProtocolNumber int64 = 255
)

// ianaProtocolNames — ключевые слова реестра номеров протоколов IANA
// (https://www.iana.org/assignments/protocol-numbers/protocol-numbers.xhtml),
// приведённые к нижнему регистру: сравнение регистронезависимо, потому что
// реестр печатает их заглавными, а клиенты пишут строчными.
//
// В набор входят записи реестра, У КОТОРЫХ ЕСТЬ ключевое слово. Записи без него
// («any host internal protocol», «any local network», «any private encryption
// scheme», «any 0-hop protocol») именем не адресуются вовсе — им отвечает
// ветка `protocol_number`. Многословные записи реестра представлены
// общепринятым токеном (`isis` для «ISIS over IPv4», `mobility-header` для
// «Mobility Header»).
var ianaProtocolNames = map[string]struct{}{
	"hopopt": {}, "icmp": {}, "igmp": {}, "ggp": {}, "ipv4": {}, "st": {},
	"tcp": {}, "cbt": {}, "egp": {}, "igp": {}, "bbn-rcc-mon": {}, "nvp-ii": {},
	"pup": {}, "argus": {}, "emcon": {}, "xnet": {}, "chaos": {}, "udp": {},
	"mux": {}, "dcn-meas": {}, "hmp": {}, "prm": {}, "xns-idp": {},
	"trunk-1": {}, "trunk-2": {}, "leaf-1": {}, "leaf-2": {}, "rdp": {},
	"irtp": {}, "iso-tp4": {}, "netblt": {}, "mfe-nsp": {}, "merit-inp": {},
	"dccp": {}, "3pc": {}, "idpr": {}, "xtp": {}, "ddp": {}, "idpr-cmtp": {},
	"tp++": {}, "il": {}, "ipv6": {}, "sdrp": {}, "ipv6-route": {},
	"ipv6-frag": {}, "idrp": {}, "rsvp": {}, "gre": {}, "dsr": {}, "bna": {},
	"esp": {}, "ah": {}, "i-nlsp": {}, "swipe": {}, "narp": {}, "min-ipv4": {},
	"tlsp": {}, "skip": {}, "ipv6-icmp": {}, "ipv6-nonxt": {}, "ipv6-opts": {},
	"cftp": {}, "sat-expak": {}, "kryptolan": {}, "rvd": {}, "ippc": {},
	"sat-mon": {}, "visa": {}, "ipcv": {}, "cpnx": {}, "cphb": {}, "wsn": {},
	"pvp": {}, "br-sat-mon": {}, "sun-nd": {}, "wb-mon": {}, "wb-expak": {},
	"iso-ip": {}, "vmtp": {}, "secure-vmtp": {}, "vines": {}, "ttp": {},
	"iptm": {}, "nsfnet-igp": {}, "dgp": {}, "tcf": {}, "eigrp": {},
	"ospfigp": {}, "sprite-rpc": {}, "larp": {}, "mtp": {}, "ax.25": {},
	"ipip": {}, "micp": {}, "scc-sp": {}, "etherip": {}, "encap": {},
	"gmtp": {}, "ifmp": {}, "pnni": {}, "pim": {}, "aris": {}, "scps": {},
	"qnx": {}, "a/n": {}, "ipcomp": {}, "snp": {}, "compaq-peer": {},
	"ipx-in-ip": {}, "vrrp": {}, "pgm": {}, "l2tp": {}, "ddx": {}, "iatp": {},
	"stp": {}, "srp": {}, "uti": {}, "smp": {}, "sm": {}, "ptp": {},
	"isis": {}, "fire": {}, "crtp": {}, "crudp": {}, "sscopmce": {},
	"iplt": {}, "sps": {}, "pipe": {}, "sctp": {}, "fc": {},
	"rsvp-e2e-ignore": {}, "mobility-header": {}, "udplite": {},
	"mpls-in-ip": {}, "manet": {}, "hip": {}, "shim6": {}, "wesp": {},
	"rohc": {}, "ethernet": {}, "aggfrag": {}, "nsh": {}, "homa": {},
	"bit-emu": {},
}

// IsKnownProtocolName сообщает, входит ли имя в набор, который продукт умеет
// выразить: собственное «любой протокол» либо ключевое слово реестра IANA.
// Регистр не важен. Пустая строка означает «протокол не задан» (в proto —
// незаполненная ветка oneof) и в набор не входит: обязательность решает
// вызывающая проверка, а не этот предикат.
func IsKnownProtocolName(name string) bool {
	lower := strings.ToLower(name)
	if lower == strings.ToLower(AnyProtocolName) {
		return true
	}
	_, ok := ianaProtocolNames[lower]
	return ok
}
