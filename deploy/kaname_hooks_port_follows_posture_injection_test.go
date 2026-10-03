// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_hooks_port_follows_posture_injection_test.go — проверка
// kaname_hooks_port_follows_posture_test.go СПОСОБНА упасть и способна смолчать.
//
// Вход — НАСТОЯЩИЙ: копия подчарта из дерева во временном каталоге, в шаблоне
// которой возвращена ровно одна прежняя строка. Прогонов три, и каждый меняет
// один факт против контроля:
//
//   - контроль — копия без правки: находок ноль (законный близнец инъекции);
//   - инъекция А — проба готовности возвращена на порт `http-hooks`: красное,
//     названы стенд и слот пробы;
//   - инъекция Б — у внутреннего Service возвращён порт `http-hooks`: красное,
//     названы стенд и Service; порта у пода при этом нет, то есть красное
//     приходит от своей оси, а не от соседней.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyKanameSubchart — копия подчарта из дерева во временный каталог; edit —
// правка одного файла шаблонов (путь от каталога подчарта → замена). Правка,
// чья исходная строка в шаблоне не найдена, — ОТКАЗ: инъекция, не нашедшая
// своего предмета, ничего не доказывает.
func copyKanameSubchart(t *testing.T, file, from, to string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "kaname")
	if err := os.CopyFS(dst, os.DirFS(iamSubchartDir)); err != nil {
		t.Fatalf("копия подчарта не удалась: %v", err)
	}
	if file == "" {
		return dst
	}
	path := filepath.Join(dst, file)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("инъекция: %v", err)
	}
	if n := strings.Count(string(raw), from); n != 1 {
		t.Fatalf("инъекция: строка %q встречается в %s %d раз, а не один — предмет инъекции "+
			"не найден однозначно, и красное было бы не от него", from, file, n)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), from, to, 1)), 0o600); err != nil {
		t.Fatalf("инъекция: %v", err)
	}
	return dst
}

// findingsNaming — находки, в тексте которых стоят все поданные подстроки.
func findingsNaming(findings []string, parts ...string) int {
	n := 0
	for _, f := range findings {
		all := true
		for _, p := range parts {
			if !strings.Contains(f, p) {
				all = false
				break
			}
		}
		if all {
			n++
		}
	}
	return n
}

func TestKanameHooksPortInjection_ControlCopyIsSilent(t *testing.T) {
	findings, census := auditKanameHooksPort(t, copyKanameSubchart(t, "", "", ""))
	t.Logf("контроль: %s", census)
	if len(findings) != 0 {
		t.Fatalf("копия подчарта без правки дала %d находок — проверка краснеет не от предмета:\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

func TestKanameHooksPortInjection_ReadinessBackOnHooksIsFound(t *testing.T) {
	chart := copyKanameSubchart(t, "templates/deployment.yaml",
		"              path: /readyz\n              port: metrics\n",
		"              path: /readyz\n              port: http-hooks\n")
	findings, census := auditKanameHooksPort(t, chart)
	t.Logf("инъекция А: %s", census)
	got := findingsNaming(findings, "readinessProbe", `"http-hooks"`)
	if got != census.Stacks {
		t.Fatalf("проба готовности возвращена на http-hooks, а находок о ней %d из %d стендов:\n  %s",
			got, census.Stacks, strings.Join(findings, "\n  "))
	}
	if n := findingsNaming(findings, "стенд dev ", "readinessProbe"); n != 1 {
		t.Errorf("находка не называет стенд: о стенде dev находок %d, а не 1", n)
	}
	if other := len(findings) - got; other != 0 {
		t.Errorf("инъекция одного слота дала %d посторонних находок — красное пришло не только от "+
			"предмета:\n  %s", other, strings.Join(findings, "\n  "))
	}
}

func TestKanameHooksPortInjection_UngatedServicePortIsFound(t *testing.T) {
	chart := copyKanameSubchart(t, "templates/service-internal.yaml",
		"    - name: http-jwks\n",
		"    - name: http-hooks\n      port: 9092\n      targetPort: http-hooks\n    - name: http-jwks\n")
	findings, census := auditKanameHooksPort(t, chart)
	t.Logf("инъекция Б: %s", census)
	got := findingsNaming(findings, "внутренний Service маршрутизирует порт http-hooks")
	if got != census.Own {
		t.Fatalf("порт хуков возвращён у Service, а находок о нём %d из %d стендов:\n  %s",
			got, census.Own, strings.Join(findings, "\n  "))
	}
	if other := len(findings) - got; other != 0 {
		t.Errorf("инъекция порта Service дала %d посторонних находок — ось пода отозвалась на "+
			"чужой предмет:\n  %s", other, strings.Join(findings, "\n  "))
	}
}
