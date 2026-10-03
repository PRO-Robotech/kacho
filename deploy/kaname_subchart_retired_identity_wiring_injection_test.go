// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_subchart_retired_identity_wiring_injection_test.go — судьи
// kaname_subchart_retired_identity_wiring_test.go падают на возвращённом дефекте
// и молчат на законном близнеце, отличающемся ОДНИМ фактом (kacho#2818).
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// retiredSettingsShape — исходник в форме перечня пиненного модуля.
const retiredSettingsShape = `package config

type retiredSetting struct{ Key, Env, Why string }

var retiredSettings = []retiredSetting{
	{
		Key: "authn.identity-provider",
		Env: "KANAME_AUTHN__IDENTITY_PROVIDER",
		Why: "single posture",
	},
}
`

func TestKanameRetiredWiringInjection_RefusedSettingsParseByTheirShape(t *testing.T) {
	rows, err := refusedSettingsOfSource([]byte(retiredSettingsShape))
	if err != nil || len(rows) != 1 || rows[0].Key != "authn.identity-provider" ||
		rows[0].Env != "KANAME_AUTHN__IDENTITY_PROVIDER" {
		t.Fatalf("законная форма перечня: строк %d (%+v), ошибка %v", len(rows), rows, err)
	}
	for name, src := range map[string]string{
		"перечень пуст":         "package config\n\nvar retiredSettings = []retiredSetting{}\n",
		"объявления нет":        "package config\n\nvar otherSettings = []retiredSetting{{Key: \"a\", Env: \"A\"}}\n",
		"не литерал":            "package config\n\nvar retiredSettings = load()\n",
		"строка без переменной": "package config\n\nvar retiredSettings = []retiredSetting{{Key: \"a.b\"}}\n",
		"поле не литерал":       "package config\n\nvar retiredSettings = []retiredSetting{{Key: k, Env: \"A\"}}\n",
		"поля без имён":         "package config\n\nvar retiredSettings = []retiredSetting{{\"a.b\", \"A\", \"x\"}}\n",
	} {
		if rows, err := refusedSettingsOfSource([]byte(src)); err == nil {
			t.Errorf("%s: разбор принял форму и вернул %+v — судить доставку было бы не по чему", name, rows)
		}
	}
}

func TestKanameRetiredWiringInjection_RefusedKeyInARealRenderIsFound(t *testing.T) {
	stacks := deployStacks(t)
	chain, ok := stacks["own"]
	if !ok {
		t.Fatal("стека own в таблице нет — вход инъекции исчез, а не дерево стало чистым")
	}
	out, err := renderStackSubchart(t, "own", stackIdentityValues(t, chain))
	if err != nil {
		t.Fatalf("рендер стека own: %v\n%s", err, out)
	}
	refused := []refusedSetting{{Key: "authn.identity-provider", Env: "KANAME_AUTHN__IDENTITY_PROVIDER"}}

	twin, env := renderedDelivery(t, out)
	authn, ok := twin["authn"].(map[string]any)
	if !ok {
		t.Fatal("в настоящем рендере нет раздела authn — вход инъекции сменил форму")
	}
	delete(authn, "identity-provider")
	delete(env, "KANAME_AUTHN__IDENTITY_PROVIDER")
	if f := judgeRefusedDelivery("own", refused, twin, env); len(f) != 0 {
		t.Fatalf("близнец без снятого ключа обязан молчать: %v", f)
	}
	for _, v := range []any{"own", "", nil} {
		authn["identity-provider"] = v
		f := judgeRefusedDelivery("own", refused, twin, env)
		if len(f) != 1 || !strings.Contains(f[0], "authn.identity-provider") {
			t.Errorf("ключ объявлен со значением %#v — ожидалась одна находка с именем ключа, получено %v", v, f)
		}
	}
	delete(authn, "identity-provider")
	env["KANAME_AUTHN__IDENTITY_PROVIDER"] = true
	if f := judgeRefusedDelivery("own", refused, twin, env); len(f) != 1 || !strings.Contains(f[0], "KANAME_AUTHN__IDENTITY_PROVIDER") {
		t.Errorf("снятая переменная в поде — ожидалась одна находка с её именем, получено %v", f)
	}
}

func TestKanameRetiredWiringInjection_VendorWiringIsFoundOnEveryAxis(t *testing.T) {
	cm := func(name string, labels map[string]any, data map[string]any) map[string]any {
		return map[string]any{"kind": "ConfigMap", "metadata": map[string]any{"name": name, "labels": labels}, "data": data}
	}
	pod := func(env, ports []any) map[string]any {
		return map[string]any{"kind": "Deployment", "metadata": map[string]any{"name": "kaname"},
			"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
				"containers": []any{map[string]any{"name": "kaname", "image": "docker.io/prorobotech/kaname:1", "env": env, "ports": ports}},
			}}}}
	}
	twin := []map[string]any{
		cm("kaname-config", map[string]any{"app": "kaname"}, map[string]any{"config.yaml": "authn:\n  domain: x\n"}),
		pod([]any{map[string]any{"name": "KANAME_CONFIG_PATH"}}, []any{map[string]any{"name": "grpc"}}),
	}
	if f := judgeVendorWiring("близнец", twin); len(f) != 0 {
		t.Fatalf("законный близнец обязан молчать: %v", f)
	}
	for name, docs := range map[string][]map[string]any{
		"имя карты":            {cm("kaname-kratos-config", nil, nil), twin[1]},
		"метка":                {cm("kaname-a", map[string]any{"kacho.cloud/component": "kratos-config"}, nil), twin[1]},
		"ключ данных":          {cm("kaname-b", nil, map[string]any{"kratos.yaml": "x"}), twin[1]},
		"ключ настроек службы": {cm("kaname-config", nil, map[string]any{"config.yaml": "authn:\n  hydra-issuer: x\n"}), twin[1]},
		"переменная":           {twin[0], pod([]any{map[string]any{"name": "KANAME_HYDRA_ISSUER"}}, nil)},
		"образ":                {twin[0], pod(nil, nil), {"kind": "Deployment", "metadata": map[string]any{"name": "x"}, "spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "c", "image": "oryd/kratos:v1"}}}}}}},
	} {
		if f := judgeVendorWiring(name, docs); len(f) != 1 {
			t.Errorf("%s: ожидалась ровно одна находка, получено %v", name, f)
		}
	}
}

func TestKanameRetiredWiringInjection_HooksLaneFollowsWhatThePinReads(t *testing.T) {
	docs := []map[string]any{
		{"kind": "Deployment", "metadata": map[string]any{"name": "kaname"}, "spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"containers": []any{map[string]any{"name": "kaname",
				"env":   []any{map[string]any{"name": "KANAME_HOOK_TOKEN"}, map[string]any{"name": "KANAME_HOOKS_SERVER_MTLS_ENABLE"}},
				"ports": []any{map[string]any{"name": "http-hooks"}}}},
		}}}},
		{"kind": "Service", "metadata": map[string]any{"name": "kaname-internal"}, "spec": map[string]any{"ports": []any{map[string]any{"name": "http-hooks"}}}},
	}
	lane := hooksLaneOf(docs)
	if len(lane) != 4 {
		t.Fatalf("полоса в рендере: ожидалось 4 места (две переменные, порт пода, порт Service), получено %v", lane)
	}
	if f := judgeHooksLane("s", false, lane); len(f) != 4 {
		t.Errorf("пин полосу не читает, рендер её несёт — ожидалось 4 находки, получено %v", f)
	}
	if f := judgeHooksLane("s", true, lane); len(f) != 0 {
		t.Errorf("пин полосу читает — утверждение не судит, получено %v", f)
	}
	if f := judgeHooksLane("s", false, hooksLaneOf(docs[1:1])); len(f) != 0 {
		t.Errorf("пин не читает и рендер не несёт — молчание, получено %v", f)
	}
}

func TestKanameRetiredWiringInjection_HooksReaderIsALiteralOfANonTestSource(t *testing.T) {
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, c := range map[string]struct {
		files map[string]string
		want  bool
	}{
		"литерал в исходнике":  {map[string]string{"a.go": "package a\n\nconst e = \"KANAME_HOOK_TOKEN\"\n"}, true},
		"приставка транспорта": {map[string]string{"a.go": "package a\n\nconst e = \"KANAME_HOOKS_SERVER_MTLS\"\n"}, true},
		"только комментарий":   {map[string]string{"a.go": "package a\n\n// KANAME_HOOK_TOKEN снят\nconst e = 1\n"}, false},
		"только проба":         {map[string]string{"a.go": "package a\n", "a_test.go": "package a\n\nconst e = \"KANAME_HOOK_TOKEN\"\n"}, false},
	} {
		dir := t.TempDir()
		for f, body := range c.files {
			write(dir, f, body)
		}
		got, files, err := moduleReadsHooksLane(dir)
		if err != nil || files == 0 || got != c.want {
			t.Errorf("%s: читатель %v (ожидалось %v), исходников %d, ошибка %v", name, got, c.want, files, err)
		}
	}
	if _, _, err := moduleReadsHooksLane(t.TempDir()); err == nil {
		t.Error("пустой каталог модуля: обход пуст, а разбор не отказал")
	}
}
