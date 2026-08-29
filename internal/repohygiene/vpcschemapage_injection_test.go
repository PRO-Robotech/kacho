// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Доказательство способности гейта схемы vpc упасть И смолчать.
//
// Инъекция гоняет ТОТ ЖЕ предикат (`vpcSchemaAdjudicate`), которым судит гейт, —
// копия предиката доказывала бы способность упасть у копии. Вход синтетический:
// править живое дерево ради доказательства значило бы ставить опыт на работе
// соседней полосы.
//
// Каждый случай нарушает РОВНО ОДНУ ось: инъекция, роняющая заодно соседнюю
// проверку, не отличает работающий предикат от вакуумного.

// vpcSchemaFixture — согласованное дерево-образец: девять ресурсов с полным
// набором глаголов, две инфраструктурные таблицы, две служебные.
func vpcSchemaFixture() (live, diagram, services []string, rows []vpcSchemaPageRow) {
	live = []string{
		"addresses", "cidr_groups", "gateways", "network_interfaces", "networks",
		"operations", "route_tables", "security_groups", "subnets", "vpc_outbox",
		"address_pools",
		"subnet_cidr_blocks", "quota_sync_cursor",
	}
	diagram = []string{
		"addresses", "address_pools", "cidr_groups", "gateways", "network_interfaces",
		"networks", "operations", "route_tables", "security_groups", "subnets", "vpc_outbox",
	}
	services = []string{
		"AddressPoolService", "AddressService", "CidrGroupService", "GatewayService",
		"NetworkInterfaceService", "NetworkService", "RouteTableService",
		"SecurityGroupService", "SubnetService",
	}
	onDiagram := map[string]bool{}
	for _, d := range diagram {
		onDiagram[d] = true
	}
	for _, tbl := range live {
		mark := vpcSchemaDiagramNo
		if onDiagram[tbl] {
			mark = vpcSchemaDiagramYes
		}
		rows = append(rows, vpcSchemaPageRow{Table: tbl, OnDiagram: mark})
	}
	return live, diagram, services, rows
}

// TestVpcSchemaGateStaysSilentOnAConsistentTree — законный близнец.
//
// Без него отрицания ниже зеленели бы на предикате, который ругается на всё.
func TestVpcSchemaGateStaysSilentOnAConsistentTree(t *testing.T) {
	live, diagram, services, rows := vpcSchemaFixture()
	if got := vpcSchemaAdjudicate(live, diagram, rows, services); len(got) > 0 {
		t.Fatalf("на согласованном дереве находок быть не должно, получено %d: %v", len(got), got)
	}
}

func TestVpcSchemaGateFindsALiveTableMissingFromTheList(t *testing.T) {
	live, diagram, services, rows := vpcSchemaFixture()
	// Снимаем строку перечня, оставляя таблицу живой: перечень объявляет себя
	// полным и полным быть перестаёт.
	rows = dropVpcRow(rows, "quota_sync_cursor")
	assertVpcFinding(t, vpcSchemaAdjudicate(live, diagram, rows, services),
		"живой таблицы в нём нет", "quota_sync_cursor")
}

func TestVpcSchemaGateFindsAListedTableTheTreeDoesNotCreate(t *testing.T) {
	live, diagram, services, rows := vpcSchemaFixture()
	// Строка пережила свою таблицу: миграция её сняла, страница — нет.
	rows = append(rows, vpcSchemaPageRow{Table: "vpc_watch_cursors", OnDiagram: vpcSchemaDiagramNo})
	assertVpcFinding(t, vpcSchemaAdjudicate(live, diagram, rows, services),
		"которой цепочка миграций не создаёт", "vpc_watch_cursors")
}

func TestVpcSchemaGateFindsAColumnThatLiesAboutTheDiagram(t *testing.T) {
	live, diagram, services, rows := vpcSchemaFixture()
	// Столбец «На диаграмме» — единственное, чем объявлено различие проекций.
	// Соврав, он возвращает ровно то расхождение, ради снятия которого заведён.
	for i := range rows {
		if rows[i].Table == "subnet_cidr_blocks" {
			rows[i].OnDiagram = vpcSchemaDiagramYes
		}
	}
	assertVpcFinding(t, vpcSchemaAdjudicate(live, diagram, rows, services),
		"изображение говорит", "subnet_cidr_blocks")
}

func TestVpcSchemaGateFindsTheBoundaryTurningIntoAnEnumeration(t *testing.T) {
	live, diagram, services, rows := vpcSchemaFixture()
	// Дорисована живая таблица, которой свойство не требует: именно так граница
	// перестаёт быть свойством и становится списком.
	diagram = append(diagram, "subnet_cidr_blocks")
	for i := range rows {
		if rows[i].Table == "subnet_cidr_blocks" {
			rows[i].OnDiagram = vpcSchemaDiagramYes
		}
	}
	assertVpcFinding(t, vpcSchemaAdjudicate(live, diagram, rows, services),
		"которой свойство не требует", "subnet_cidr_blocks")
}

func TestVpcSchemaGateFindsAResourceTableAbsentFromTheDiagram(t *testing.T) {
	live, diagram, services, rows := vpcSchemaFixture()
	// Ресурс с полным набором глаголов есть, таблицы на диаграмме нет — то самое
	// отставание, с которого задача началась.
	diagram = dropVpcName(diagram, "cidr_groups")
	for i := range rows {
		if rows[i].Table == "cidr_groups" {
			rows[i].OnDiagram = vpcSchemaDiagramNo
		}
	}
	assertVpcFinding(t, vpcSchemaAdjudicate(live, diagram, rows, services),
		"свойство требует таблицу на диаграмме", "cidr_groups")
}

func TestVpcSchemaGateFindsANamingRuleItDoesNotCover(t *testing.T) {
	live, diagram, services, rows := vpcSchemaFixture()
	// Новая служба, чьё множественное число правилом не выводится. Гейт обязан
	// назвать ПРАВИЛО, а не тихо пропустить ресурс мимо диаграммы.
	services = append(services, "PolicyService")
	assertVpcFinding(t, vpcSchemaAdjudicate(live, diagram, rows, services),
		"правилу образования", "PolicyService")
}

func assertVpcFinding(t *testing.T, findings []string, want, subject string) {
	t.Helper()
	if len(findings) == 0 {
		t.Fatalf("инъекция не дала находки — гейт слеп к оси %q", want)
	}
	for _, f := range findings {
		if strings.Contains(f, want) && strings.Contains(f, subject) {
			return
		}
	}
	t.Fatalf("находка есть, но не называет предмет: искали %q + %q, получено %v", want, subject, findings)
}

func dropVpcRow(rows []vpcSchemaPageRow, table string) []vpcSchemaPageRow {
	out := rows[:0:0]
	for _, r := range rows {
		if r.Table != table {
			out = append(out, r)
		}
	}
	return out
}

func dropVpcName(names []string, name string) []string {
	out := names[:0:0]
	for _, n := range names {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}
