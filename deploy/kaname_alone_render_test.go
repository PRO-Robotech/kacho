// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_alone_render_test.go — ОДИНОЧНЫЙ РЕНДЕР ПОДЧАРТА kaname ЧЕРЕЗ ОБЁРТКУ
// (NTF-1, полоса D2; замысел З28 «Помощники — по одному телу», CX1-113, N35-2).
//
// Шаблон `ConfigMap` подчарта kaname берёт флаг почты службы помощником
// `kacho.notifications.enabledFor`; тело помощника одно на релиз и лежит в чарте
// notify (deploy/helm/notify/templates/_flag.tpl, _sources.tpl). Рендер зонтика
// его видит, одиночный рендер `charts/kaname` — нет («no template»). Поэтому
// одиночный рендер подчарта в Go идёт только через `renderKanameAlone`:
// копия каталога подчарта во временном каталоге, в её `templates/` — побайтовые
// копии файлов помощников, первым слоем — узлы `global.kacho.notifications` и
// `global.kacho.spiffe`, выписанные из values.yaml зонтика в момент рендера.
// Двойник на шелле — `render_kaname_alone` (deploy/tests/helm/lib/render-chain.sh).
//
// Держит единственность обёртки TestNTFD2_KanameAloneRenderWrappersExist; прямой
// `helm template … charts/kaname` вне обёртки — TestKanameIsRenderedAloneOnlyThroughTheWrapper.

package deploy_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// kanameAloneHelperFiles — файлы помощников чарта notify, которые обёртка
// кладёт в копию подчарта.
var kanameAloneHelperFiles = []string{"_flag.tpl", "_sources.tpl"}

// kanameAloneGlobals — слой узлов `global`, выписанных из values.yaml зонтика.
func kanameAloneGlobals(t *testing.T, dir string) string {
	t.Helper()
	vals := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	kacho := map[string]any{}
	for _, node := range []string{"notifications", "spiffe"} {
		if v, ok := lookup(vals, "global", "kacho", node); ok && v != nil {
			kacho[node] = v
		}
	}
	if _, ok := kacho["notifications"]; !ok {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s/values.yaml нет узла global.kacho.notifications — одиночному рендеру kaname подать нечего", umbrellaDir)
	}
	body, err := yaml.Marshal(map[string]any{"global": map[string]any{"kacho": kacho}})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "globals.yaml")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// copyDirTree — побайтовая копия каталога src в dst.
func copyDirTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		body, rerr := os.ReadFile(p) // #nosec G304 -- путь из обхода каталога чарта
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, body, 0o600)
	})
}

// renderKanameAlone — `helm template <release> <копия chartDir> -f <узлы global>
// args…`. chartDir — каталог подчарта kaname дерева либо его фикстурной копии.
// Отказ фикстуры (копия, файлы помощников) — «НЕ ВЫПОЛНИЛОСЬ» отказом пробы;
// возврат — вывод и ошибка helm.
func renderKanameAlone(t *testing.T, release, chartDir string, args ...string) (string, error) {
	t.Helper()
	work := t.TempDir()
	cp := filepath.Join(work, "kaname")
	if err := copyDirTree(chartDir, cp); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: копия подчарта %s не снята: %v", chartDir, err)
	}
	for _, f := range kanameAloneHelperFiles {
		body, err := os.ReadFile(filepath.Join(notifyChartDir, "templates", f))
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файл помощников %s чарта notify не прочитан: %v", f, err)
		}
		if err := os.WriteFile(filepath.Join(cp, "templates", f), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	full := append([]string{"template", release, cp, "-f", kanameAloneGlobals(t, work)}, args...)
	out, err := exec.Command("helm", full...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева и пробы
	return string(out), err
}

// TestKanameAloneRenderSeesTheNotificationsHelper — обёртка рендерит подчарт и
// флаг почты службы в `ConfigMap` равен выводу помощника; близнец-отрицание —
// тот же рендер без копии помощников (голый `helm template`) отказывает
// «no template», то есть обёртка несущая, а не декоративная.
func TestKanameAloneRenderSeesTheNotificationsHelper(t *testing.T) {
	requireHelmNTF(t)
	chart := filepath.Join(umbrellaDir, "charts", "kaname")
	args := []string{"-n", "kacho", "-f", filepath.Join(umbrellaDir, "values.dev.yaml"),
		"--show-only", "templates/configmap.yaml"}
	out, err := renderKanameAlone(t, "kacho-umbrella", chart, args...)
	if err != nil {
		t.Fatalf("обёртка: одиночный рендер подчарта kaname отказал: %v\n%s", err, lastLines(out, 6))
	}
	objs := parseRendered(t, out)
	var got any
	for _, o := range objs {
		if o.kind != "ConfigMap" {
			continue
		}
		var cfg map[string]any
		if err := yaml.Unmarshal([]byte(nstr(ndig(o.doc, "data", "config.yaml"))), &cfg); err != nil {
			t.Fatalf("config.yaml ConfigMap kaname не разбирается: %v", err)
		}
		got, _ = lookup(cfg, "notifications", "enabled")
	}
	if got != true {
		t.Errorf("обёртка: ConfigMap kaname notifications.enabled = %#v — ожидался вывод помощника true (флаг установки values.yaml)", got)
	}
	kOff, err := renderKanameAlone(t, "kacho-umbrella", chart, append(args, "--set", ntfModulesKey+".kaname.enabled=false")...)
	if err != nil || !strings.Contains(kOff, "enabled: false") {
		t.Errorf("обёртка: переопределение kaname=false не дошло до ConfigMap (%v):\n%s", err, lastLines(kOff, 6))
	}
	bare, berr := exec.Command("helm", append([]string{"template", "kacho-umbrella", chart}, args...)...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы пробы
	if berr == nil || !strings.Contains(string(bare), `no template "kacho.notifications.enabledFor"`) {
		t.Errorf("близнец: голый рендер подчарта без обёртки не отказал «no template» (%v) — обёртка ничего не несёт:\n%s", berr, lastLines(string(bare), 4))
	}
	t.Logf("обёртка: notifications.enabled=%v; переопределение kaname=false доходит; голый рендер — отказ «no template»", got)
}

// kanameAloneDirectRe — прямой одиночный рендер подчарта kaname в Go: вызов
// `helm` с аргументом, называющим каталог подчарта.
var kanameAloneDirectRe = regexp.MustCompile(`(?m)^\s*(args\s*:?=\s*\[\]string\{\s*"template"[^\n]*(iamSubchartDir|iamChartDir|charts", "kaname"|charts/kaname)|.*exec\.Command\("helm"[^\n]*(iamSubchartDir|charts/kaname))`)

// TestKanameIsRenderedAloneOnlyThroughTheWrapper — в Go пакета deploy прямого
// `helm template` над каталогом подчарта kaname нет: каждый одиночный рендер —
// через renderKanameAlone. Находка — файл и строка.
func TestKanameIsRenderedAloneOnlyThroughTheWrapper(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	if len(files) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: файлов Go в deploy/ ноль")
	}
	sort.Strings(files)
	var hits []string
	for _, f := range files {
		body, err := os.ReadFile(f) // #nosec G304 -- путь из обхода каталога
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if kanameAloneDirectRe.MatchString(line) {
				hits = append(hits, f+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	if len(hits) > 0 {
		t.Errorf("прямой одиночный рендер подчарта kaname мимо renderKanameAlone — %d:\n  %s", len(hits), strings.Join(hits, "\n  "))
	}
	t.Logf("осмотрено файлов Go %d; прямых рендеров подчарта kaname %d", len(files), len(hits))
}
