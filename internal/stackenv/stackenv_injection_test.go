// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package stackenv

// stackenv_injection_test.go — способность пробы стендов упасть доказана
// инъекцией на СИНТЕТИЧЕСКОМ дереве (t.TempDir()): таблица стендов, умбрелла из
// двух профилей и маленький чарт, рендеримый настоящим helm. Самопроверка на
// живых профилях покраснела бы в день, когда они станут полны, — то есть на
// достижении цели.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// synthTree кладёт дерево: стенды good (объявляет ручку) и bad (не объявляет),
// третий слой good-secret доставляет ручку секретом, а off не поднимает службу.
func synthTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"deploy/stacks.txt": "# синтетика\ngood:values.base.yaml,values.good.yaml\n" +
			"bad:values.base.yaml\nsecret:values.base.yaml,values.secret.yaml\noff:values.off.yaml\n",
		"deploy/helm/umbrella/values.yaml":        "probe:\n  mode: dev\n",
		"deploy/helm/umbrella/values.base.yaml":   "probe:\n  enabled: true\n",
		"deploy/helm/umbrella/values.good.yaml":   "probe:\n  authority: not-deployed\n",
		"deploy/helm/umbrella/values.secret.yaml": "probe:\n  authoritySecret: probe-creds\n",
		"deploy/helm/umbrella/values.off.yaml":    "probe:\n  enabled: false\n",
		"chart/Chart.yaml":                        "apiVersion: v2\nname: probe\nversion: 0.0.1\n",
		"chart/values.yaml":                       "mode: production\nauthority: \"\"\nauthoritySecret: \"\"\n",
		"chart/templates/configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: probe-config
data:
  config.yaml: |
    mode: {{ .Values.mode }}
`,
		"chart/templates/deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: probe
spec:
  template:
    spec:
      volumes:
        - name: config
          configMap:
            name: probe-config
      containers:
        - name: sidecar
          env:
            - name: OTHER
              value: "x"
        - name: probe
          args: ["--config", "/etc/probe/config.yaml"]
          volumeMounts:
            - name: config
              mountPath: /etc/probe
          env:
            - name: KACHO_PROBE_MODE
              value: {{ .Values.mode | quote }}
            {{- if .Values.authoritySecret }}
            - name: KACHO_PROBE_AUTHORITY
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.authoritySecret }}
                  key: authority
            {{- else }}
            - name: KACHO_PROBE_AUTHORITY
              value: {{ .Values.authority | quote }}
            {{- end }}
`,
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// probeGuard — страж синтетической службы: домен величин обязан быть объявлен, а
// файл конфигурации из --config — существовать и называть посадку.
func probeGuard(env Env) error {
	if os.Getenv("KACHO_PROBE_AUTHORITY") == "" {
		return errors.New("quota.authority (KACHO_PROBE_AUTHORITY) не задана")
	}
	raw, err := os.ReadFile(env.Flag("--config"))
	if err != nil {
		return err
	}
	if !strings.Contains(string(raw), "mode: dev") {
		return errors.New("файл конфигурации не несёт посадки")
	}
	return nil
}

func requireHelmForSelfCheck(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("%v: в конвейере исполнитель рендера обязан быть", ErrNoHelm)
		}
		t.Skipf("%v", ErrNoHelm)
	}
}

func TestJudgeFindsTheStackThatDoesNotDeclareWhatTheGuardRequires(t *testing.T) {
	requireHelmForSelfCheck(t)
	root := synthTree(t)
	s := Service{Name: "probe", Root: root, Chart: "chart", ValuesKey: "probe", EnvPrefix: "KACHO_PROBE_",
		GuardNames: []string{"probeGuard"}, Boot: probeGuard}

	r, err := Judge(s, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Инъекция: bad не объявил ручку — находка ровно по нему.
	if len(r.Findings) != 1 || r.Findings[0].Chain.Name != "bad" ||
		!strings.Contains(r.Findings[0].Err.Error(), "KACHO_PROBE_AUTHORITY") {
		t.Fatalf("ждали одну находку по стенду bad с названной ручкой: %+v", r.Findings)
	}
	// Законные близнецы: good объявил значением, secret — секретом (в объявлении
	// пусто намеренно); оба молчат. off службу не поднимает и не считается.
	if r.ChainsRead != 4 || r.ChainsRaised != 3 {
		t.Fatalf("перепись стендов не та: прочитано %d (ждали 4), поднимают %d (ждали 3)", r.ChainsRead, r.ChainsRaised)
	}
	if r.FromSecret != 1 {
		t.Fatalf("доставленных секретом %d, ждали 1 (стенд secret)", r.FromSecret)
	}
	// Смонтированный файл положен и путь в аргументе переписан на него: иначе страж
	// отказал бы на всех трёх стендах, а не на одном.
	if r.Files != 3 {
		t.Fatalf("смонтированных файлов %d, ждали по одному на стенд, поднимающий службу (3)", r.Files)
	}
}

// Ни один стенд службу не поднимает — «не выполнилось», а не зелёное.
func TestJudgeRefusesWhenNoStackRaisesTheService(t *testing.T) {
	requireHelmForSelfCheck(t)
	root := synthTree(t)
	if err := os.WriteFile(filepath.Join(root, "deploy/stacks.txt"), []byte("off:values.off.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Service{Name: "probe", Root: root, Chart: "chart", ValuesKey: "probe", EnvPrefix: "KACHO_PROBE_", Boot: probeGuard}
	_, err := Judge(s, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "НЕ ВЫПОЛНИЛОСЬ") {
		t.Fatalf("обход без поднимающих стендов обязан дать «не выполнилось»: %v", err)
	}
}

// Окружение процесса возвращается на место: пробы соседних стендов и соседние
// пробы пакета не наследуют подстановок.
func TestBootWithRestoresTheEnvironment(t *testing.T) {
	t.Setenv("KACHO_PROBE_KEEP", "before")
	_, _ = bootWith(Env{Values: map[string]string{"KACHO_PROBE_TEMP": "x", "KACHO_PROBE_KEEP": "during"}},
		func(Env) error {
			if os.Getenv("KACHO_PROBE_KEEP") != "during" || os.Getenv("KACHO_PROBE_TEMP") != "x" {
				t.Error("стражам подано не то окружение")
			}
			return nil
		})
	if os.Getenv("KACHO_PROBE_KEEP") != "before" {
		t.Fatalf("имя не возвращено: %q", os.Getenv("KACHO_PROBE_KEEP"))
	}
	if _, ok := os.LookupEnv("KACHO_PROBE_TEMP"); ok {
		t.Fatal("подстановка стенда пережила вопрос")
	}
}

func TestReadChainsRefusesAnUnparsedLineAndAnEmptyTable(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"unparsed": "dev values.dev.yaml\n", "empty": "# только комментарий\n"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadChains(p); err == nil {
			t.Errorf("%s: таблица принята, а «стендов меньше» неотличимо от «строка не узнана»", name)
		}
	}
}
