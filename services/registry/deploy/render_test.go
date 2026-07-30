// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// render_test.go — общие помощники deploy-гейтов чарта registry.
//
// Два рода гейтов, и они дополняют друг друга:
//   - РЕНДЕР (helmTemplate/helmTemplateMustFail) — проверяет логику шаблона:
//     что именно выезжает в кластер при данных значениях;
//   - ДЕКЛАРАЦИЯ (umbrellaValues) — читает файлы значений профилей и потому НЕ
//     МОЖЕТ пропуститься на машине без helm/без `helm dep build`. Значение,
//     отсутствие которого молчаливо, проверяется только так (эталон —
//     gateway/deploy/token_shape_test.go).
package deploy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// helmTemplate рендерит чарт с --set-переопределениями. На машине без helm
// рендер-гейт скипается (чтобы `go test ./...` оставался зелёным), но в CI (env
// CI) отсутствие бинарника — ЖЁСТКИЙ провал, а не скип: рендер-гейты не должны
// молча становиться инертными на джобе, которая гейтит мёрж.
func helmTemplate(t *testing.T, sets ...string) string {
	t.Helper()
	requireHelm(t)
	out, err := runHelmTemplate(sets...)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	return out
}

// helmTemplateMustFail — рендер ОБЯЗАН провалиться, а его сообщение — содержать
// wantMsg. Так проверяется отказ шаблона от небезопасной конфигурации: гейт
// доказан инъекцией в обе стороны (helmTemplate рядом доказывает, что законная
// конфигурация рендерится молча).
func helmTemplateMustFail(t *testing.T, wantMsg string, sets ...string) {
	t.Helper()
	requireHelm(t)
	out, err := runHelmTemplate(sets...)
	if err == nil {
		t.Fatalf("helm template SUCCEEDED, want refusal mentioning %q\n%s", wantMsg, out)
	}
	if !strings.Contains(out, wantMsg) {
		t.Fatalf("helm template refused, but its message does not name the knob %q "+
			"(an operator cannot act on it)\n%s", wantMsg, out)
	}
}

func requireHelm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm binary not on PATH in CI — render-guard must run, not skip (add azure/setup-helm to the job)")
		}
		t.Skip("helm binary not on PATH — skipping deploy render-guard")
	}
}

func runHelmTemplate(sets ...string) (string, error) {
	args := []string{"template", "."}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	cmd := exec.Command("helm", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// docOf возвращает YAML-документ рендера, чей путь-источник содержит source и в
// котором встречается marker (например kind+name). Пусто → тест падает: «ноль
// найденного» обязано быть отличимо от «ноль просмотренного».
func docOf(t *testing.T, rendered, source, marker string) string {
	t.Helper()
	docs := strings.Split(rendered, "\n---\n")
	seen := 0
	for _, d := range docs {
		if !strings.Contains(d, source) {
			continue
		}
		seen++
		if strings.Contains(d, marker) {
			return d
		}
	}
	t.Fatalf("rendered output has no document from %q containing %q (%d documents from that source examined)",
		source, marker, seen)
	return ""
}

// umbrellaValues загружает профиль umbrella как обобщённое дерево.
func umbrellaValues(t *testing.T, profile string) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "..", "deploy", "helm", "umbrella", profile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return tree
}

// digOpt читает значение по пути ключей; второй результат — найдено ли.
func digOpt(tree map[string]any, path ...string) (any, bool) {
	var cur any = tree
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[key]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// umbrellaProfiles — все файлы значений umbrella. Перечень ЧИТАЕТСЯ с диска, а
// не зашивается списком: новый профиль попадает под гейты сам, а не после того,
// как кто-то вспомнит дописать его в тест.
func umbrellaProfiles(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "deploy", "helm", "umbrella")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read umbrella dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, "values") || !strings.HasSuffix(n, ".yaml") {
			continue
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		t.Fatal("no umbrella values profiles found — the declaration gate would silently examine nothing")
	}
	return out
}

// registryEnabledProfiles — профили, в которых чарт registry включён. Возвращает
// и общее число просмотренных профилей, чтобы «ноль подходящих» было отличимо от
// «ноль прочитанных».
func registryEnabledProfiles(t *testing.T) (enabled []string, examined int) {
	t.Helper()
	for _, p := range umbrellaProfiles(t) {
		examined++
		v, ok := digOpt(umbrellaValues(t, p), "registry", "enabled")
		if ok && v == true {
			enabled = append(enabled, p)
		}
	}
	return enabled, examined
}

func toStr(v any) string {
	s, _ := v.(string)
	return s
}
