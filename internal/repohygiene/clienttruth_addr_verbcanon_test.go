// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// verbCanonOptions — вход вердикта о НАСТОЯЩЕМ дереве.
//
// # Шесть послаблений, и почему они именно такой формы
//
// Дефисная запись landed-глаголов домена vpc снимается СВОИМ изменением
// (`kacho#1624`): путь уже опубликован, поэтому смена написания — ломающее
// изменение, и делается оно новым каноном основным адресом плюс прежней записью
// дополнительной привязкой. Работа затрагивает плоскость края и в эту полосу не
// входит.
//
// Записи стоят здесь, а не в виде суженного обхода: суженный обход молчал бы
// навсегда, а послабление ИСТЕКАЕТ САМО — как только путь переведён, ему нечего
// исключать, и анализатор требует его снять.
//
// Предикат числа шесть, повторяемый:
//
//	git grep -ohE '"/[a-z]+/v1/[^"]*:[a-zA-Z][a-zA-Z0-9-]*"' -- proto \
//	  | sed -E 's/.*:([a-zA-Z][a-zA-Z0-9-]*)"/\1/' | grep -c -- -
func verbCanonOptions(t *testing.T) VerbCanonOptions {
	t.Helper()
	const reason = "landed-путь; смена написания ломающая — переводится задачей kacho#1624"
	return VerbCanonOptions{
		Tree:      clientTruthRepoTree(t),
		ProtoRoot: "proto",
		Exemptions: []VerbCanonExemption{
			{
				File:   "proto/kacho/cloud/vpc/v1/network_service.proto",
				Path:   "/vpc/v1/networks/{network_id}:add-cidr-blocks",
				Reason: reason,
			},
			{
				File:   "proto/kacho/cloud/vpc/v1/network_service.proto",
				Path:   "/vpc/v1/networks/{network_id}:remove-cidr-blocks",
				Reason: reason,
			},
			{
				File:   "proto/kacho/cloud/vpc/v1/subnet_service.proto",
				Path:   "/vpc/v1/subnets/{subnet_id}:add-cidr-blocks",
				Reason: reason,
			},
			{
				File:   "proto/kacho/cloud/vpc/v1/subnet_service.proto",
				Path:   "/vpc/v1/subnets/{subnet_id}:remove-cidr-blocks",
				Reason: reason,
			},
			{
				File:   "proto/kacho/cloud/vpc/v1/cidr_group_service.proto",
				Path:   "/vpc/v1/cidrGroups/{cidr_group_id}:add-cidr-blocks",
				Reason: reason,
			},
			{
				File:   "proto/kacho/cloud/vpc/v1/cidr_group_service.proto",
				Path:   "/vpc/v1/cidrGroups/{cidr_group_id}:remove-cidr-blocks",
				Reason: reason,
			},
		},
	}
}

// TestSuffixActionsAreWrittenInTheCanon — вердикт о настоящем дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_addr_verbcanon_injection_test.go`): здесь только вердикт.
func TestSuffixActionsAreWrittenInTheCanon(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditVerbCanon(verbCanonOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть.
	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта %d — обход пуст, вердикт беспредметен", census.ProtoFiles)
	}
	if census.WithVerb == 0 {
		t.Fatalf("адресов с суффикс-действием 0 (адресов всего %d) — сверка не состоялась",
			census.Paths)
	}
	// Канон обязан быть представлен: ноль каноничных путей означал бы, что канон
	// в дереве не встречается вовсе, и требовать его было бы не от чего.
	if census.Canonical == 0 {
		t.Fatalf("путей каноном 0 — канон в дереве не представлен, требование беспредметно")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("суффикс-действие записано не каноном "+
		"(адресов с действием %d, каноном %d, снято послаблением %d):\n%s\n\n"+
		"Канон — нижняя верблюжья запись; его независимо объявляют конвенция API "+
		"(пример `:addCidrBlocks`), каталог прав (запись строится из имени метода) и "+
		"пятьдесят один путь дерева. Клиент строит адрес соседнего ресурса по образцу "+
		"известного, а промах даёт 404 без тела. Строка про устаревшее послабление "+
		"означает обратное: путь переведён, снимите запись в `verbCanonOptions`.",
		census.WithVerb, census.Canonical, census.Exempted, strings.Join(lines, "\n"))
}
