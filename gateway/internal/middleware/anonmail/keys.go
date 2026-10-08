// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"errors"
	"fmt"
	"hash/fnv"
	"net/netip"
	"sort"
)

// Длины префиксов ключей (приёмка NTF-2, Р5): источник — адрес целиком
// (IPv4 /32, IPv6 /64), подсети — /24 у IPv4, /56 и /48 у IPv6.
const (
	sourceLenV4 = 32
	sourceLenV6 = 64
	subnetLenV4 = 24
	subnetLen56 = 56
	subnetLen48 = 48
)

// SubnetKey — ключ подсети одной длины префикса.
type SubnetKey struct {
	Len int
	Key string
}

// Keys — ключи одного запроса: источник и его подсети в порядке убывания
// длины префикса.
type Keys struct {
	Source  string
	Subnets []SubnetKey
}

// All — все ключи запроса: источник, затем подсети.
func (k Keys) All() []string {
	out := make([]string, 0, 1+len(k.Subnets))
	out = append(out, k.Source)
	for _, s := range k.Subnets {
		out = append(out, s.Key)
	}
	return out
}

var errUnkeyableAddress = errors.New("anonmail: client address is not an IP literal")

// KeysFor выводит ключи из клиентского адреса — результата
// `ContextExtractor.ClientIP` (CX2-12 (а)): тот же адрес край отдаёт службе в
// `X-Forwarded-For`. Адрес, который не разбирается как литерал IP, ключом не
// становится: свести такие запросы к общему ключу значило бы счесть их одним
// источником по случайности.
func KeysFor(addr string) (Keys, error) {
	a, err := netip.ParseAddr(addr)
	if err != nil || a.Zone() != "" {
		return Keys{}, fmt.Errorf("%w: %q", errUnkeyableAddress, addr)
	}
	a = a.Unmap()
	if a.Is4() {
		return Keys{
			Source:  key("src/v4/", a, sourceLenV4),
			Subnets: []SubnetKey{{Len: subnetLenV4, Key: key("net/v4/", a, subnetLenV4)}},
		}, nil
	}
	return Keys{
		Source: key("src/v6/", a, sourceLenV6),
		Subnets: []SubnetKey{
			{Len: subnetLen56, Key: key("net/v6/", a, subnetLen56)},
			{Len: subnetLen48, Key: key("net/v6/", a, subnetLen48)},
		},
	}, nil
}

func key(class string, a netip.Addr, bits int) string {
	p, _ := a.Prefix(bits) // bits в границах семейства по построению
	return class + p.String()
}

// lockPair — ключ рекомендательной блокировки формы с ДВУМЯ int4 (З8, CX2-11):
// класс — константа звена, объект — свёртка ключа. Форма с двумя int4 живёт в
// другом пространстве ключей PostgreSQL, чем форма одним bigint, поэтому с
// блокировкой схемы хранилища (`pg_advisory_lock(schemaLockID)`) не
// пересекается ни при каком значении свёртки.
type lockPair struct {
	class, obj int32
}

// anonMailLockClass — класс рекомендательных блокировок звена: одна константа
// на дерево края.
const anonMailLockClass int32 = 0x616d6c31 // "aml1"

// keyLockPair — свёртка ключа в пару блокировки. Ту же свёртку берут полосатые
// мьютексы хранилища memory: один ключ — одна полоса в обоих хранилищах.
func keyLockPair(k string) lockPair {
	h := fnv.New32a()
	_, _ = h.Write([]byte("anonmail:" + k))
	return lockPair{class: anonMailLockClass, obj: int32(h.Sum32())} // #nosec G115 -- свёртка: перенос знака не меняет различимости
}

// sortedLockPairs — пары блокировок ключей запроса, отсортированные по
// значению и без повторов: порядок взятия один для любых двух запросов, в том
// числе при совпадении свёрток ключей разных классов, поэтому взаимной
// блокировки нет. Совпадение свёрток двух ключей только сериализует их лишний
// раз и счёта не меняет (счёт ведётся по строке ключа).
func sortedLockPairs(keys []string, fold func(string) lockPair) []lockPair {
	seen := make(map[lockPair]bool, len(keys))
	out := make([]lockPair, 0, len(keys))
	for _, k := range keys {
		p := fold(k)
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].class != out[j].class {
			return out[i].class < out[j].class
		}
		return out[i].obj < out[j].obj
	})
	return out
}
