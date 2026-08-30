// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import "fmt"

// cidr_fields.go — ЧЬИМ ИМЕНЕМ отказ называет диапазон подсети.
//
// ПРЕДМЕТ (kacho#1623). Имя поля в отказе — часть контракта: по нему вызывающий
// понимает, что править. Отказы подсети называли `v4_cidr_blocks` — имя
// ДОМЕННОГО поля (`domain.Subnet.V4CidrBlocks`), которого нет ни в одном
// сообщении контракта подсети. Вызывающий искал в своём теле ключ, которого туда
// не клал.
//
// ОДНО ИМЯ НА ВСЕ ГЛАГОЛЫ НЕ ГОДИТСЯ — И ИМЕННО ЭТО СОБЛАЗНЯЛО ЗАХАРДКОДИТЬ
// ДОМЕННОЕ. Диапазоны принимают три глагола, и поле у них РАЗНОЕ:
//
//	Create            ipv4_cidr_primary   скаляр  — первичный якорь
//	:addCidrBlocks    ipv4_cidr_blocks    массив
//	:removeCidrBlocks ipv4_cidr_blocks    массив
//
// Общие помощники (`validateSubnetNotReserved`, `validateSubnetCidrCardinality`)
// зовутся из всех трёх, поэтому имя обязано ПРИЕЗЖАТЬ ОТ ГЛАГОЛА, а не жить
// внутри помощника. Прежде оно жило внутри — отсюда одно доменное имя на всех.
//
// ИНДЕКС — ТОЖЕ УТВЕРЖДЕНИЕ, И НА `Create` ОНО БЫЛО ЛОЖНЫМ. Скаляр
// `ipv4CidrPrimary` получал отказ вида `v4_cidr_blocks[0]`, то есть отправителя
// отсылали к нулевому элементу массива, которого он не присылал. Слот называется
// там и только там, где вызывающий прислал НЕСКОЛЬКО значений и обязан понять,
// которое из них негодно.
//
// ФОРМА ИМЕНИ — camelCase. Единственный транспорт быстрых стартов — REST, и
// каждая страница `api/overview.mdx` обещает тела с camelCase-ключами.
//
// ЧТО ДЕРЖИТ ЭТИ ЛИТЕРАЛЫ ОТ ДРЕЙФА. Гейт дерева
// `internal/repohygiene` `TestResourceRefusalNamesAContractField`: имя поля,
// названное отказом use-case-пакета ресурса, обязано существовать среди полей
// сообщений этого ресурса в контракте. Литерал, разошедшийся с контрактом, —
// находка, а не стиль.

// cidrFields — имена полей контракта, которыми ОДИН глагол принимает диапазоны.
type cidrFields struct {
	// v4, v6 — json-имена полей запроса этого глагола.
	v4, v6 string
	// indexed — глагол принимает МАССИВ, поэтому отказ называет слот.
	// Скаляр слота не имеет: индекс у него был бы утверждением о теле,
	// которого вызывающий не отправлял.
	indexed bool
}

// slot — имя поля для отказа по i-му присланному значению.
func (f cidrFields) slot(name string, i int) string {
	if !f.indexed {
		return name
	}
	return fmt.Sprintf("%s[%d]", name, i)
}

// V4Slot / V6Slot — имя поля семейства для отказа по i-му значению.
func (f cidrFields) V4Slot(i int) string { return f.slot(f.v4, i) }
func (f cidrFields) V6Slot(i int) string { return f.slot(f.v6, i) }

var (
	// createCidrFields — `Create` принимает ОДИН первичный якорь на семейство.
	createCidrFields = cidrFields{v4: "ipv4CidrPrimary", v6: "ipv6CidrPrimary"}

	// blocksCidrFields — `:addCidrBlocks` / `:removeCidrBlocks` принимают
	// массивы; имена полей у обоих глаголов совпадают, поэтому и запись одна.
	blocksCidrFields = cidrFields{v4: "ipv4CidrBlocks", v6: "ipv6CidrBlocks", indexed: true}
)
