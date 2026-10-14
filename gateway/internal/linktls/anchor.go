// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package linktls

import (
	"crypto/x509"
)

// Anchor — ЯКОРЬ ЗВЕНЬЕВ ФРОНТА (kacho#3028, круг 5): удостоверяющий центр,
// который выпускает сертификаты ТОЛЬКО звеньям фронта края.
//
// # Почему не якорь установки
//
// Якорь установки (`KACHO_API_GATEWAY_MTLS_CA_FILE`) выпускает листы через
// кластерный выпускающий: сертификат с любым именем получает всякий, кто вправе
// завести запрос на сертификат в ЛЮБОМ пространстве имён кластера. Звено,
// узнаваемое по имени в листе этого якоря, узнавалось бы по имени, которое
// выдаёт себе кто угодно, — тот же род «доверие шире фронта», что и адрес
// пода. Якорь звеньев — отдельный удостоверяющий центр, выпускающий только в
// пространстве имён края (namespaced Issuer): лист с именем звена получает
// только тот, кто уже управляет краем.
//
// # Две стороны одного решения
//
// Цепочка, проверенная рукопожатием, кончается корнем одного из якорей. Корень
// решает, ЧТО этот лист:
//
//   - корень якоря звеньев — звено фронта (Issued): его имя читает оператор
//     адреса клиента, и ТОЛЬКО он;
//   - корень иного якоря — нагрузка установки (Foreign): её имя SPIFFE
//     читает полоса личности по сертификату.
//
// Лист звена личностью НЕ становится никогда: звено ретранслирует запросы
// всех своих клиентов, и личность звена на них была бы чужой личностью на
// каждом запросе. Лист установки звеном не становится никогда: его выпускают
// за пределами пространства края.
//
// Нулевое значение — якоря нет: Issued не находит ничего, Foreign отдаёт
// любой проверенный лист (поведение без звеньев).
type Anchor struct {
	roots []*x509.Certificate
}

// NewAnchor — якорь из корней удостоверяющего центра звеньев.
func NewAnchor(roots ...*x509.Certificate) Anchor {
	out := make([]*x509.Certificate, 0, len(roots))
	for _, r := range roots {
		if r != nil {
			out = append(out, r)
		}
	}
	return Anchor{roots: out}
}

// Empty — якорь не объявлен.
func (a Anchor) Empty() bool { return len(a.roots) == 0 }

// Roots — корни якоря (для пула клиентских удостоверяющих центров слушателя).
func (a Anchor) Roots() []*x509.Certificate { return append([]*x509.Certificate(nil), a.roots...) }

// Issued — лист первой проверенной цепочки, кончающейся корнем якоря
// звеньев, либо nil.
func (a Anchor) Issued(chains [][]*x509.Certificate) *x509.Certificate {
	for _, chain := range chains {
		if len(chain) > 0 && chain[0] != nil && a.owns(chain[len(chain)-1]) {
			return chain[0]
		}
	}
	return nil
}

// Foreign — лист первой проверенной цепочки, кончающейся НЕ корнем якоря
// звеньев, либо nil.
func (a Anchor) Foreign(chains [][]*x509.Certificate) *x509.Certificate {
	for _, chain := range chains {
		if len(chain) > 0 && chain[0] != nil && !a.owns(chain[len(chain)-1]) {
			return chain[0]
		}
	}
	return nil
}

// owns — корень ли это якоря. Сравнение по байтам сертификата (Equal), а не
// по имени субъекта: имя субъекта корня пишет тот, кто его выпустил.
func (a Anchor) owns(root *x509.Certificate) bool {
	if root == nil || len(root.Raw) == 0 {
		return false
	}
	for _, r := range a.roots {
		if r.Equal(root) {
			return true
		}
	}
	return false
}
