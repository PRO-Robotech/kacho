// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"sort"
	"strings"
)

// Протокол и диапазон портов SG-правила — закрытые множества, а не свободный текст.
//
// Контракт объявлен в самом сообщении правила: `protocol_name` — значение из
// реестра номеров протоколов IANA, `protocol_number` — номер оттуда же,
// `PortRange.from_port`/`to_port` — `0-65535`. Значение вне множества принять
// нельзя: правило с несуществующим протоколом или с портом вне диапазона
// продукт выразить не может, поэтому вызывающий обязан получить отказ, а не
// успех на правило, которое ничего не разрешает и ничего не запрещает.
//
// ОДНО отступление от объявленного интервала — `-1` на обеих границах. Это
// собственный sentinel продукта «любой порт»: он старше этой проверки, его
// пишут доменная модель, интеграционные тесты и e2e-кейс, и снятие его было бы
// ломающим изменением, а не ужесточением. Отступление записано в реестр
// намеренных решений сервиса (`docs/architecture/07-known-divergences.md` §23)
// вместе с тем, что у «любого порта» в контракте есть и второе написание —
// незаполненный `ports`.
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
// В набор входят записи реестра, У КОТОРЫХ ЕСТЬ ключевое слово. Пять записей
// его не имеют — 61 «any host internal protocol», 63 «any local network», 68
// «any distributed file system», 99 «any private encryption scheme», 114 «any
// 0-hop protocol»: именем они не адресуются вовсе, им отвечает ветка
// `protocol_number`. Многословные записи реестра представлены общепринятым
// токеном (`isis` для «ISIS over IPv4», `mobility-header` для «Mobility
// Header»).
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

// protocolNameAliases — написания, под которыми тот же протокол знают операторы
// и `/etc/protocols`, но которых нет в колонке ключевых слов реестра. Приняты
// намеренно: закрытый набор имён сужает написание, а не выразимость, и отвечать
// отказом на `ospf` или `icmpv6` значило бы наказывать за орфографию реестра.
// Набор только РАСШИРЯЕТ приём и потому не может сломать вызывающего.
var protocolNameAliases = map[string]string{
	"ospf":    "ospfigp",   // 89: реестр печатает OSPFIGP, пишут все ospf
	"icmpv6":  "ipv6-icmp", // 58: реестр печатает IPv6-ICMP
	"mobile":  "min-ipv4",  // 55: прежнее имя реестра, живо в /etc/protocols
	"ipencap": "ipv4",      // 4:  имя из /etc/protocols
	"all":     "any",       // распространённое написание «любой протокол»
}

// KnownProtocolNames возвращает ПОЛНЫЙ набор имён, которые принимает
// `IsKnownProtocolName`, в нижнем регистре и отсортированным: собственное
// «любой протокол», ключевые слова реестра IANA и принятые псевдонимы.
//
// Существует не ради прод-кода (там достаточно предиката), а чтобы паритет
// набора можно было ДОКАЗАТЬ: тот же инвариант выражен ещё и ограничением базы
// (`kacho_sg_protocol_name_valid`, миграция 0027), а два источника истины
// расходятся молча. Гейт паритета перечисляет этот набор, требует того же
// ответа от базы и заодно утверждает объём осмотренного — чтобы «ноль
// расхождений» отличалось от «ноль прочитанного».
func KnownProtocolNames() []string {
	out := make([]string, 0, len(ianaProtocolNames)+len(protocolNameAliases)+1)
	out = append(out, strings.ToLower(AnyProtocolName))
	for n := range ianaProtocolNames {
		out = append(out, n)
	}
	for a := range protocolNameAliases {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// IsKnownProtocolName сообщает, входит ли имя в набор, который продукт умеет
// выразить: собственное «любой протокол», ключевое слово реестра IANA либо
// принятый псевдоним. Регистр не важен. Пустая строка означает «протокол не
// задан» (в proto — незаполненная ветка oneof) и в набор не входит:
// обязательность решает вызывающая проверка, а не этот предикат.
func IsKnownProtocolName(name string) bool {
	lower := strings.ToLower(name)
	if alias, ok := protocolNameAliases[lower]; ok {
		lower = alias
	}
	if lower == strings.ToLower(AnyProtocolName) {
		return true
	}
	_, ok := ianaProtocolNames[lower]
	return ok
}
