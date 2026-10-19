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
// ЧТО НЕСЁТ ЭТА ПАРА. Стороны у значения НЕ симметричны. Реестр, узнающий
// анонима, которого никто не чеканит, безвреден: такого `sub` ни у кого нет.
// Служба доступа, чеканящая анонимный токен, который реестр НЕ узнаёт, —
// дыра: плоскость данных читает такой `sub` как обычный принципал вида
// service_account, а этот вид модель прав относит к классу
// `system_class:authenticated` — вызывающий без удостоверения читал бы всё,
// выданное вошедшим, а не только опубликованное анониму. Поэтому у стороны
// личности СОБСТВЕННОЙ ручки нет: чеканка включается только единым источником,
// который тем же рендером зонта доезжает до пода реестра.
//
// Оси:
//
//	(а) единый источник доезжает до `registry-token.anonymous-client-id`;
//	(б) собственная ручка подчарта `config.apiServer.registryToken.anonymousClientId`
//	    ОТКАЗЫВАЕТ рендер в любой форме — одна (чеканка без узнающей стороны) и
//	    рядом с единым источником, даже равная ему (второго объявления нет);
//	    отказ называет ручку и единый источник;
//	(в) не объявлено ничего ⇒ ключа в карте настроек НЕТ (а не пустая строка),
//	    положительный контроль рядом;
//	(г) значение объявлено, а блока полосы нет (ни `issuer`, ни `service`) ⇒
//	    отказ: принятое и непрочитанное значение выглядело бы включённым потоком.
package umbrella_test

import (
	"regexp"
	"strings"
	"testing"
)

// laneName — законная сторона полосы, без которой блок `registry-token:` не
// рендерится вовсе: оси (а)–(в) отличаются от неё только анонимной ручкой.
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

// TestIAMAnonymousClientIDComesFromTheSingleDeclaration — ось (а).
func TestIAMAnonymousClientIDComesFromTheSingleDeclaration(t *testing.T) {
	t.Parallel()

	out, err := renderIAM(t, laneName, "global.kacho.registry.anonymousSubjectId=registry-anonymous")
	if err != nil {
		t.Fatalf("рендер отказал: %v\n%s", err, out)
	}
	if got, ok := anonymousClientIDInConfig(out); !ok || got != "registry-anonymous" {
		t.Errorf("сторона личности не выведена из единого источника: anonymous-client-id = %q (есть: %v)", got, ok)
	}
}

// TestIAMAnonymousClientIDHasNoOwnKnob — ось (б). Первый случай — предикат
// находки: собственная ручка без единого источника включала чеканку, а под
// реестра переменной не получал.
func TestIAMAnonymousClientIDHasNoOwnKnob(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"одна, без единого источника": {
			laneName, "config.apiServer.registryToken.anonymousClientId=registry-anonymous",
		},
		"рядом с единым источником, иная": {
			laneName, "global.kacho.registry.anonymousSubjectId=registry-anonymous",
			"config.apiServer.registryToken.anonymousClientId=other-anonymous",
		},
		"рядом с единым источником, равная": {
			laneName, "global.kacho.registry.anonymousSubjectId=same-anonymous",
			"config.apiServer.registryToken.anonymousClientId=same-anonymous",
		},
	}
	for name, sets := range cases {
		out, err := renderIAM(t, sets...)
		if err == nil {
			got, ok := anonymousClientIDInConfig(out)
			t.Errorf("%s: рендер ПРОШЁЛ (anonymous-client-id = %q, есть: %v) — служба доступа чеканила бы "+
				"анонимный токен по объявлению, которого под реестра не видит", name, got, ok)
			continue
		}
		for _, want := range []string{
			"config.apiServer.registryToken.anonymousClientId", "global.kacho.registry.anonymousSubjectId",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: отказ рендера не называет %q — оператору нечем чинить:\n%s", name, want, out)
			}
		}
	}
}

// TestIAMAnonymousClientIDAbsentWhenUndeclared — ось (в).
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

// TestIAMAnonymousClientIDWithoutLaneRefuses — ось (г).
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
