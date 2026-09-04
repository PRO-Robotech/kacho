// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package computev1

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// DNS снимается с контракта compute — на входе И на выходе.
//
// ВХОД был недостижим по построению. `DnsRecordSpec` достигался только через
// `PrimaryAddressSpec.dns_record_specs` и `OneToOneNatSpec.dns_record_specs`, а сами эти
// сообщения — только через поля NIC-спеки, которые сервис ОТВЕРГАЕТ ПО ИМЕНИ
// (`network_interface_specs.primary_v4_address_spec` / `…v6…`), и через запросы двух
// RPC, у которых нет обработчика (`UpdateNetworkInterface`, `AddOneToOneNat` — отвечают
// `Unimplemented`). Ни одной открытой двери.
//
// ВЫХОД был достижим — и это важнее, потому что постановка «недостижимо по построению»
// к нему НЕ применима. `Instance.network_interfaces[].primary_v4_address` производится
// прод-кодом (`internal/protoconv`), `OneToOneNat` внутри него — тоже. Но `dns_records`
// не заполнялось НИ В ОДНОЙ из двух точек: ни у адреса, ни у NAT. То есть поле честно
// доезжало до клиента — всегда пустым, на каждом Get и List, и отличить «записей нет»
// от «поле не реализовано» было нельзя.
//
// Ни там, ни там это не «поле на будущее». DNS-запись не атрибут интерфейса машины: ей
// нужна зона, которой кто-то владеет, набор записей со своим жизненным циклом, TTL и
// семантикой распространения, и своя авторизация — это домен со своей приёмкой. `ttl` и
// `dns_zone_id` ссылались на понятия, которых у платформы нет вовсе. Тот же разбор и то
// же решение уже применены в vpc (pkg/api/kacho/cloud/vpc/v1/address_dns_contract_test.go).
//
// ФОРМА СНЯТИЯ РАЗЛИЧАЕТ ДВА СЛУЧАЯ, и это не косметика:
//   - у поля есть объемлющее сообщение, на которое ссылается живое поле → номер И имя
//     РЕЗЕРВИРУЮТСЯ (пир мог прислать эти байты; слот нельзя переиспользовать);
//   - сообщение осталось сиротой, на него не ссылается ни одно поле → УДАЛЯЕТСЯ, и
//     резервировать нечего: сообщение, на которое никто не ссылается, на wire не было.
//
// Запрет здесь ПАКЕТНЫЙ, в отличие от запрета host-affinity рядом (тот сужен до
// публичной полосы, потому что физика размещения на внутреннем листенере законна). DNS
// же не «чувствительная» форма, а ЧУЖОЙ ДОМЕН: его нет ни на публичной поверхности
// compute, ни на внутренней, и появиться он должен своим доменом, а не полем в чужом
// сообщении.

// vacatedDnsSlots — сообщение → освобождённый слот. Эти сообщения ЖИВЫ (на них
// ссылаются действующие поля), поэтому номер и имя остаются зарезервированными.
var vacatedDnsSlots = map[protoreflect.Name]struct {
	number protoreflect.FieldNumber
	name   protoreflect.Name
}{
	"PrimaryAddress":     {3, "dns_records"},
	"OneToOneNat":        {3, "dns_records"},
	"PrimaryAddressSpec": {3, "dns_record_specs"},
	"OneToOneNatSpec":    {3, "dns_record_specs"},
}

// withdrawnDnsMessages — сообщения, осиротевшие после снятия полей выше.
var withdrawnDnsMessages = []protoreflect.Name{"DnsRecord", "DnsRecordSpec"}

// TestNoComputeMessageDeclaresDnsField — ни одно сообщение домена не объявляет
// DNS-поле. Обход всего пакета (включая вложенные сообщения — см. `computeMessages`),
// а не список руками: иначе новое сообщение вернёт форму молча.
func TestNoComputeMessageDeclaresDnsField(t *testing.T) {
	banned := map[protoreflect.Name]string{
		"dns_records":      "dnsRecords",
		"dns_record_specs": "dnsRecordSpecs",
		"dns_record":       "dnsRecord",
		"dns_record_spec":  "dnsRecordSpec",
	}
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			for protoName, jsonName := range banned {
				if f.Name() == protoName || f.JSONName() == jsonName {
					t.Errorf("%s declares DNS field %q (number %d): DNS is a domain of its "+
						"own — zones, record sets, TTL, propagation, authorization — not an "+
						"attribute of a machine's network interface, and nothing in the "+
						"service ever filled or read it", md.FullName(), f.Name(), f.Number())
				}
			}
		}
	})
}

// TestVacatedDnsSlotsStayReserved — снятие объявлено в самом контракте: и имя, и номер
// каждого освобождённого слота зарезервированы, поэтому слот не вернётся с другим
// смыслом против пира, который всё ещё шлёт старое поле.
func TestVacatedDnsSlotsStayReserved(t *testing.T) {
	visited := map[protoreflect.Name]bool{}
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		want, ok := vacatedDnsSlots[md.Name()]
		if !ok {
			return
		}
		visited[md.Name()] = true

		names := md.ReservedNames()
		haveName := false
		for i := 0; i < names.Len(); i++ {
			if names.Get(i) == want.name {
				haveName = true
			}
		}
		if !haveName {
			t.Errorf("%s does not reserve the name %q", md.FullName(), want.name)
		}

		ranges := md.ReservedRanges()
		haveNum := false
		for i := 0; i < ranges.Len(); i++ {
			r := ranges.Get(i)
			if want.number >= r[0] && want.number < r[1] {
				haveNum = true
			}
		}
		if !haveNum {
			t.Errorf("%s does not reserve field number %d", md.FullName(), want.number)
		}
	})
	for name := range vacatedDnsSlots {
		if !visited[name] {
			t.Errorf("%s was never visited — the message this test names is not declared in "+
				"the package, so the assertion about it never ran", name)
		}
	}
}

// TestWithdrawnDnsMessagesAreGone — сообщения, на которые больше не ссылается ни одно
// поле, удалены. Оставить их значит рекламировать форму, которую сервис исполнить не
// может, и пригласить новое поле на неё сослаться.
func TestWithdrawnDnsMessagesAreGone(t *testing.T) {
	computeMessages(t, func(md protoreflect.MessageDescriptor) {
		for _, gone := range withdrawnDnsMessages {
			if md.Name() == gone {
				t.Errorf("kacho.cloud.compute.v1 still declares message %s: no field "+
					"references it and the service implements no DNS behaviour",
					md.FullName())
			}
		}
	})
}
