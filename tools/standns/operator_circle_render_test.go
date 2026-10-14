// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// operator_circle_render_test.go — каждая цепочка стенда, отрисованная в
// пространство, отличное от рабочего, выпускает лист оператора внутреннего
// слушателя края с именем ИЗ круга края (kacho#3151).
//
// Рендер в `kacho` расхождения не видит: там пространство релиза и закреплённое
// пространство личности совпадают. Поэтому пространство здесь — тестовое, как у
// стенда проб (`make -C deploy stand-ns-up`). Условие рендера — материализованные
// сабчарты (`make -C deploy helm-deps`).
package standns

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const renderNamespace = "t3151-probe"

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("путь пробы не известен — корень дерева взять неоткуда")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// renderChains — цепочки стендов из единственного читателя таблицы.
func renderChains(t *testing.T) map[string][]string {
	t.Helper()
	sh := filepath.Join(repoRoot(t), "deploy/tests/helm/stacks.sh")
	out, err := exec.Command("bash", sh, "--names").Output()
	if err != nil {
		t.Fatalf("читатель таблицы стендов отказал (%v) — состав стендов взять неоткуда", err)
	}
	chains := map[string][]string{}
	for _, name := range strings.Fields(string(out)) {
		args, aerr := exec.Command("bash", sh, "--args", name).Output()
		if aerr != nil {
			t.Fatalf("цепочка стенда %s не выдана читателем таблицы: %v", name, aerr)
		}
		fields := strings.Fields(string(args))
		for i := 0; i+1 < len(fields); i += 2 {
			if fields[i] == "-f" {
				chains[name] = append(chains[name], fields[i+1])
			}
		}
	}
	if len(chains) == 0 {
		t.Fatal("читатель таблицы не назвал ни одного стенда — таблица пуста")
	}
	return chains
}

func renderIn(t *testing.T, ns string, chain []string) []*doc {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm не в PATH — отрисовка стендов не исполнилась (это не «зелено»)")
	}
	umbrella := filepath.Join(repoRoot(t), "deploy/helm/umbrella")
	args := []string{"template", "kacho-umbrella", umbrella, "-n", ns}
	for _, p := range chain {
		args = append(args, "-f", filepath.Join(umbrella, p))
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("helm", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("отрисовка в %s отказала: %v\n%s", ns, err, stderr.String())
	}
	docs, err := parseDocs(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

func TestEveryChainRenderedOutsideTheWorkingNamespaceKeepsTheOperatorInTheEdgeCircle(t *testing.T) {
	chains := renderChains(t)
	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)
	var judged int
	var census []string
	for _, name := range names {
		docs := renderIn(t, renderNamespace, chains[name])
		f := collectOperatorCircle(docs)
		census = append(census, name+"(листов "+strconv.Itoa(f.leaves)+", круг "+strconv.Itoa(len(f.circle))+")")
		if f.leaves == 0 {
			continue
		}
		judged++
		for _, finding := range judgeOperatorCircle(f) {
			t.Errorf("%s -n %s: %s", name, renderNamespace, finding)
		}
		if name == "a8f60d" {
			injectOperatorMismatch(t, name, docs)
		}
	}
	t.Logf("перепись -n %s: цепочек %d, с листом оператора %d — %s", renderNamespace, len(names), judged, strings.Join(census, " · "))
	if judged == 0 {
		t.Fatalf("знаменатель пуст: ни одна из %d цепочек не выпустила лист оператора — проба не исполнилась", len(names))
	}
}

// injectOperatorMismatch — настоящий рендер цепочки с одним изменённым фактом:
// лист оператора в пространстве релиза (форма дефекта kacho#3151). Судья обязан
// покраснеть; законный близнец — тот же рендер без правки — уже судим выше.
func injectOperatorMismatch(t *testing.T, name string, docs []*doc) {
	t.Helper()
	injected := 0
	for _, d := range docs {
		if d.kind != "Certificate" || d.name != OperatorCertName {
			continue
		}
		uris := child(child(d.root, "spec"), "uris")
		if uris == nil || uris.Kind != yaml.SequenceNode {
			continue
		}
		for _, u := range uris.Content {
			u.Value = strings.Replace(u.Value, "/ns/"+WorkingNamespace+"/", "/ns/"+renderNamespace+"/", 1)
			injected++
		}
	}
	if injected == 0 {
		t.Fatalf("%s: инъекции некуда встать — лист оператора без имени в пространстве %s", name, WorkingNamespace)
	}
	found := judgeOperatorCircle(collectOperatorCircle(docs))
	if len(found) != 1 || !strings.Contains(found[0], "вне круга края") {
		t.Errorf("%s: инъекция расхождения (лист в пространстве релиза): ждали одну находку «вне круга края», получили %v", name, found)
	}
}
