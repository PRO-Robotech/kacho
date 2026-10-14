// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_health_point_external_injection_test.go — суд точки живости на
// внешнем входе (kacho#3030) краснеет на каждом пути, которым она уходила
// наружу, и молчит на законном близнеце. Вход — НАСТОЯЩИЙ шаблон раздачи из
// дерева; каждый вариант меняет в нём ровно один факт.
package deploy_test

import (
	"strings"
	"testing"
)

const (
	hostHealthRefusal = "            if ($server_port = {{ .Values.publicFront.httpsPort }}) { return 404; }\n"
	hostHealthAnswer  = "            return 200 \"ok\\n\";\n"
	vpcRemoteHealth   = "        location = /vpc-remote/healthz {\n            return 404;\n        }\n"
	storageRemoteLane = "        location ^~ /storage-remote/ {\n"
)

func TestConsoleHealthPointGate_ProvenByInjection(t *testing.T) {
	serving := readTreeFile(t, repoRootFromTest(t), servingTemplateRel)

	// Законный близнец — дерево как есть: находок ноль, обход не пуст.
	if f, c := judgeHealthPointExposure(t, serving); len(f) != 0 || c.RemoteLanes == 0 || c.ModuleAnswers == 0 {
		t.Fatalf("дерево: находки %v при переписи %s — близнец обязан молчать на непустом обходе", f, c)
	}

	once := func(src, old, repl string) string {
		t.Helper()
		if n := strings.Count(src, old); n != 1 {
			t.Fatalf("образец инъекции %q встречается в шаблоне %d раз, а не один — инъекция беспредметна", old, n)
		}
		return strings.Replace(src, old, repl, 1)
	}
	// Ответ `200` оболочки — первый в файле: блоки модулей идут ниже.
	firstAnswer := func(src, repl string) string {
		t.Helper()
		i := strings.Index(src, hostHealthAnswer)
		if i < 0 {
			t.Fatalf("в шаблоне нет ответа точки живости %q", hostHealthAnswer)
		}
		return src[:i] + repl + src[i+len(hostHealthAnswer):]
	}

	variants := []struct {
		name    string
		src     string
		mention string
	}{
		{"отказ внешнему порту снят", once(serving, hostHealthRefusal, ""), "отвечает и на внешнем входе"},
		{"отказ стоит после ответа",
			firstAnswer(once(serving, hostHealthRefusal, ""), hostHealthAnswer+hostHealthRefusal), "ПОСЛЕ ответа"},
		{"отказ судит не тот порт",
			once(serving, hostHealthRefusal, strings.Replace(hostHealthRefusal, "httpsPort", "redirectPort", 1)),
			"отвечает и на внешнем входе"},
		{"точка модуля снова уходит полосой ассетов", once(serving, vpcRemoteHealth, ""), "/vpc-remote/healthz"},
		{"новая полоса модуля без своего отказа",
			once(serving, storageRemoteLane, "        location ^~ /probe-remote/ {\n            proxy_pass http://probe;\n        }\n\n"+storageRemoteLane),
			"/probe-remote/healthz"},
		{"внутренняя точка оболочки снята вместе с внешней", firstAnswer(serving, ""), "не отвечает `200`"},
	}
	for _, v := range variants {
		f, c := judgeHealthPointExposure(t, v.src)
		if len(f) == 0 {
			t.Errorf("%s: суд промолчал (перепись %s)", v.name, c)
			continue
		}
		if !strings.Contains(strings.Join(f, "\n"), v.mention) {
			t.Errorf("%s: находки не называют %q:\n%s", v.name, v.mention, strings.Join(f, "\n"))
		}
	}
}
