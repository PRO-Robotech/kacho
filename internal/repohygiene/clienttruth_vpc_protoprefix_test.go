// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// protoPrefixClaimOptions — вход вердикта о НАСТОЯЩЕМ дереве.
//
// # Послабление, и почему оно именно такой формы
//
// Тот же класс живёт в контракте вычислений: два комментария приписывают
// сетевому интерфейсу префикс операции. Правятся они полосой того домена (#1601
// называет обе координаты). Запись стоит здесь, а не в виде суженного обхода:
// суженный обход молчал бы навсегда, а послабление ИСТЕКАЕТ САМО — как только
// строка выправлена, ему нечего исключать, и гейт требует его снять.
func protoPrefixClaimOptions(t *testing.T) ProtoPrefixClaimOptions {
	t.Helper()
	const computeInstanceSvc = "proto/kacho/cloud/compute/v1/instance_service.proto"
	const reason = "правится полосой compute той же линии, kacho#1601"
	return ProtoPrefixClaimOptions{
		Tree:      clientTruthRepoTree(t),
		ProtoRoot: "proto",
		Exemptions: []ProtoPrefixClaimExemption{
			{File: computeInstanceSvc, Name: "NetworkInterface", Prefix: "enp", Reason: reason},
		},
	}
}

// TestProtoCommentsAttributePrefixesToTheirType — вердикт о настоящем дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_vpc_protoprefix_injection_test.go`): здесь только вердикт.
func TestProtoCommentsAttributePrefixesToTheirType(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditProtoPrefixClaims(protoPrefixClaimOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта %d — обход пуст, вердикт беспредметен", census.ProtoFiles)
	}
	if census.KnownNames < 20 {
		t.Fatalf("имён в словаре префиксов %d — словарь не построен, сверять не с чем",
			census.KnownNames)
	}
	// Вторая половина вердикта выносится ТОЛЬКО о рассуженных утверждениях. Ноль
	// означал бы, что она не вынесена ни разу, — «находок ноль» получено даром.
	if census.Judged == 0 {
		t.Fatalf("утверждений о префиксе рассужено 0 (распознано %d) — сверка не состоялась",
			census.Claims)
	}
	// Правило домена обязано срабатывать: без него верный комментарий образа
	// хранилища объявлялся бы находкой. Ноль означает, что правило мертво.
	if census.DomainQualified == 0 {
		t.Fatalf("резолвов по домену 0 — правило домена не сработало ни разу")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("комментарий контракта приписывает префикс не тому типу "+
		"(утверждений рассужено %d, снято послаблением %d):\n%s\n\n"+
		"id-префикс — часть неизменяемой внешней координаты, и продукт предлагает читать "+
		"по нему тип. Словарь выводится из `"+PrefixSourceRel+"`; правьте комментарий, "+
		"а не этот список. Строка про устаревшее послабление означает обратное: предмет "+
		"исчез, снимите запись в `protoPrefixClaimOptions`.",
		census.Judged, census.Exempted, strings.Join(lines, "\n"))
}
