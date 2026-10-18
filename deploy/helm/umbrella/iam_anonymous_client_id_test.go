// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iam_anonymous_client_id_test.go — сторона ЛИЧНОСТИ у анонимного потока докерной
// полосы ВЫВОДИТСЯ из того же объявления, что сторона реестра (приёмка REG-PUB-1
// Р9 п. 2, kacho#3274).
//
// Парная к services/registry/deploy/anonymous_subject_test.go: там те же оси
// утверждаются о стороне РЕЕСТРА. Объявление одно —
// `global.kacho.registry.anonymousSubjectId`, — потому что `global` есть
// единственное, что видно из обоих контекстов подчартов (довод `serviceAud`,
// iam_lane_service_aud_test.go).
//
// Оси:
//
//	(а) единый источник доезжает до `registry-token.anonymous-client-id`;
//	(б) без него действует собственная ручка подчарта (одиночная установка);
//	(в) объявлены обе и не сходятся ⇒ рендер ОТКАЗЫВАЕТ, называя обе ручки и обе величины;
//	(г) законный близнец: объявлены обе и сходятся ⇒ рендер молчит;
//	(д) не объявлено ни одной ⇒ ключа в карте настроек НЕТ (а не пустая строка),
//	    положительный контроль рядом;
//	(е) значение объявлено, а блока полосы нет (ни `issuer`, ни `service`) ⇒
//	    отказ: принятое и непрочитанное значение выглядело бы включённым потоком.
package umbrella_test

import (
	"regexp"
	"strings"
	"testing"
)

// laneName — законная сторона полосы, без которой блок `registry-token:` не
// рендерится вовсе: оси (а)–(д) отличаются от неё только анонимной ручкой.
const laneName = "global.kacho.registry.serviceAud=lane.example"

var registryTokenAnonymous = regexp.MustCompile(
	`(?m)^\s*registry-token:\s*$(?:\n\s+.*)*?\n\s+anonymous-client-id:\s*"?([^"\n]*)"?`)

// anonymousClientIDInConfig — значение `anonymous-client-id` в блоке
// `registry-token:` и признак его наличия.
func anonymousClientIDInConfig(rendered string) (string, bool) {
	m := registryTokenAnonymous.FindStringSubmatch(rendered)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// TestIAMAnonymousClientIDComesFromTheSingleDeclaration — оси (а) и (б).
func TestIAMAnonymousClientIDComesFromTheSingleDeclaration(t *testing.T) {
	t.Parallel()

	out, err := renderIAM(t, laneName, "global.kacho.registry.anonymousSubjectId=registry-anonymous")
	if err != nil {
		t.Fatalf("рендер отказал: %v\n%s", err, out)
	}
	if got, ok := anonymousClientIDInConfig(out); !ok || got != "registry-anonymous" {
		t.Errorf("сторона личности не выведена из единого источника: anonymous-client-id = %q (есть: %v)", got, ok)
	}

	out, err = renderIAM(t, laneName, "config.apiServer.registryToken.anonymousClientId=solo-anonymous")
	if err != nil {
		t.Fatalf("рендер отказал: %v\n%s", err, out)
	}
	if got, ok := anonymousClientIDInConfig(out); !ok || got != "solo-anonymous" {
		t.Errorf("без единого источника собственная ручка обязана действовать: anonymous-client-id = %q (есть: %v)", got, ok)
	}
}

// TestIAMAnonymousClientIDRefusesUnlinkedSides — оси (в) и (г).
func TestIAMAnonymousClientIDRefusesUnlinkedSides(t *testing.T) {
	t.Parallel()

	out, err := renderIAM(t, laneName,
		"global.kacho.registry.anonymousSubjectId=registry-anonymous",
		"config.apiServer.registryToken.anonymousClientId=other-anonymous")
	if err == nil {
		t.Fatalf("рендер ПРОШЁЛ на разошедшихся объявлениях — аноним получил бы токен, которого реестр не узнаёт\n%s", out)
	}
	for _, want := range []string{
		"global.kacho.registry.anonymousSubjectId", "kaname.config.apiServer.registryToken.anonymousClientId",
		"registry-anonymous", "other-anonymous",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("отказ рендера не называет %q — оператору нечем чинить:\n%s", want, out)
		}
	}

	out, err = renderIAM(t, laneName,
		"global.kacho.registry.anonymousSubjectId=same-anonymous",
		"config.apiServer.registryToken.anonymousClientId=same-anonymous")
	if err != nil {
		t.Fatalf("сошедшиеся объявления обязаны рендериться, получен отказ: %v\n%s", err, out)
	}
	if got, ok := anonymousClientIDInConfig(out); !ok || got != "same-anonymous" {
		t.Errorf("anonymous-client-id = %q (есть: %v), ждали same-anonymous", got, ok)
	}
}

// TestIAMAnonymousClientIDAbsentWhenUndeclared — ось (д).
func TestIAMAnonymousClientIDAbsentWhenUndeclared(t *testing.T) {
	t.Parallel()

	out, err := renderIAM(t, laneName)
	if err != nil {
		t.Fatalf("рендер отказал: %v\n%s", err, out)
	}
	if got, ok := anonymousClientIDInConfig(out); ok {
		t.Errorf("ничего не объявлено, а anonymous-client-id в карте настроек есть (%q) — "+
			"ключ обязан отсутствовать, а не рендериться пустым", got)
	}
	// Положительный контроль: блок полосы на этом рендере ЕСТЬ, иначе отсутствие
	// ключа выше было бы отсутствием блока, а не решением шаблона.
	if laneServiceInConfig(t, out) != "lane.example" {
		t.Fatalf("блок registry-token не отрендерен — проба выше судила бы пустоту")
	}
}

// TestIAMAnonymousClientIDWithoutLaneRefuses — ось (е).
func TestIAMAnonymousClientIDWithoutLaneRefuses(t *testing.T) {
	t.Parallel()

	out, err := renderIAM(t, "global.kacho.registry.anonymousSubjectId=registry-anonymous")
	if err == nil {
		t.Fatalf("значение анонимного потока принято без блока полосы — принято и не прочитано\n%s", out)
	}
	if !strings.Contains(out, "anonymousSubjectId") {
		t.Errorf("отказ не называет ручку анонимного потока:\n%s", out)
	}
}
