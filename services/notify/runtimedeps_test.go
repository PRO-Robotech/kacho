// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// runtimedeps_test.go — NTF1-D05: рантайм источника не тянет формат шаблонов.
//
// Источник ставит письмо одной типизированной функцией, порождённой
// генератором; формат шаблонов (`corelib/notify/spec`) нужен генератору и
// notify, но не процессу источника. Замыкание импортов корня
// `cmd/notify-probe` (`go list -deps`) обязано его не нести; находка называет
// файл и строку импорта, через который пакет формата вошёл, и цепочку пакетов
// от корня.
//
// Положительный контроль того же замыкания — пакеты, которые рантайм
// источника тянет законно: лента (`notify/feed`) и форма значений
// (`notify/form`). Без них «формата нет» неотличимо от «замыкание пусто».
package notify_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// sourceRuntimeForbidden — пакеты, которых рантайм источника не тянет.
var sourceRuntimeForbidden = []string{"github.com/PRO-Robotech/corelib/notify/spec"}

// sourceRuntimeRequired — законные пакеты замыкания: положительный контроль.
var sourceRuntimeRequired = []string{
	"github.com/PRO-Robotech/corelib/notify/feed",
	"github.com/PRO-Robotech/corelib/notify/form",
}

// runtimeDepsReport — исход NTF1-D05.
type runtimeDepsReport struct {
	closure   int
	forbidden map[string]int // запрещённый пакет → вхождений в замыкание (0|1)
	findings  []string
	missing   []string // положительный контроль, не найденный в замыкании
}

func (r runtimeDepsReport) String() string {
	keys := make([]string, 0, len(r.forbidden))
	for k, v := range r.forbidden {
		keys = append(keys, k+"="+strconv.Itoa(v))
	}
	sort.Strings(keys)
	return fmt.Sprintf("NTF1-D05: корень %s · пакетов в замыкании %d · запрещённых в замыкании %v · "+
		"контроль (законные пакеты) %d из %d · находок %d", notifyProbeRoot, r.closure, keys,
		len(sourceRuntimeRequired)-len(r.missing), len(sourceRuntimeRequired), len(r.findings))
}

// auditSourceRuntimeDeps — NTF1-D05 по дереву root с оверлеем overlay.
func auditSourceRuntimeDeps(t *testing.T, root string, overlay map[string]string) runtimeDepsReport {
	t.Helper()
	listing := goListDeps(t, root, overlay, false, "./services/notify/cmd/notify-probe")
	byPath := map[string]listedPkg{}
	for _, p := range listing {
		byPath[p.ImportPath] = p
	}
	r := runtimeDepsReport{closure: len(listing), forbidden: map[string]int{}}
	// Родитель в обходе в ширину от корня — для цепочки.
	parent := map[string]string{notifyProbeRoot: ""}
	queue := []string{notifyProbeRoot}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, imp := range byPath[cur].Imports {
			if _, seen := parent[imp]; !seen {
				parent[imp] = cur
				queue = append(queue, imp)
			}
		}
	}
	for _, req := range sourceRuntimeRequired {
		if _, ok := byPath[req]; !ok {
			r.missing = append(r.missing, req)
		}
	}
	for _, bad := range sourceRuntimeForbidden {
		if _, ok := byPath[bad]; !ok {
			r.forbidden[bad] = 0
			continue
		}
		r.forbidden[bad] = 1
		var chain []string
		for cur := bad; cur != ""; cur = parent[cur] {
			chain = append([]string{cur}, chain...)
		}
		// Координата — каждый файл пакета замыкания, импортирующий запрещённый.
		for _, p := range listing {
			if !containsStr(p.Imports, bad) {
				continue
			}
			for _, at := range importSites(t, root, p, bad, overlay) {
				r.findings = append(r.findings, fmt.Sprintf("%s: рантайм источника тянет формат шаблонов — "+
					"импорт %s (цепочка %s) (NTF1-D05)", at, bad, strings.Join(chain, " → ")))
			}
		}
	}
	sort.Strings(r.findings)
	return r
}

// importSites — «путь:строка» каждого импорта bad в файлах пакета p.
func importSites(t *testing.T, root string, p listedPkg, bad string, overlay map[string]string) []string {
	t.Helper()
	var out []string
	fset := token.NewFileSet()
	for _, name := range p.GoFiles {
		abs := filepath.Join(p.Dir, name)
		rel, err := filepath.Rel(root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = abs
		}
		rel = filepath.ToSlash(rel)
		src, err := sourceOf(abs, rel, overlay)
		if err != nil {
			t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: чтение %s: %v", abs, err)
		}
		f, err := parser.ParseFile(fset, rel, src, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: разбор импортов %s: %v", abs, err)
		}
		for _, spec := range f.Imports {
			if v, _ := strconv.Unquote(spec.Path.Value); v == bad {
				out = append(out, fmt.Sprintf("%s:%d", rel, fset.Position(spec.Pos()).Line))
			}
		}
	}
	if len(out) == 0 {
		out = append(out, p.ImportPath+" (файл импорта не найден разбором)")
	}
	return out
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TestNTF1D05_SourceRuntimeDoesNotPullTheTemplateFormat — замыкание импортов
// корня notify-probe без пакета формата шаблонов.
func TestNTF1D05_SourceRuntimeDoesNotPullTheTemplateFormat(t *testing.T) {
	t.Parallel()
	root := repoRootOf(t)
	r := auditSourceRuntimeDeps(t, root, nil)
	t.Log(r.String())
	if r.closure == 0 {
		t.Fatalf("пустое замыкание — не вердикт: %s", r)
	}
	if len(r.missing) > 0 {
		t.Fatalf("положительный контроль не выполнен: законных пакетов %v в замыкании нет — "+
			"обход не тот: %s", r.missing, r)
	}
	for _, f := range r.findings {
		t.Errorf("%s", f)
	}
}

// TestNTF1D05Injection — импорт формата оверлеем в корень и во внутренний пакет
// пробы; близнец — импорт законной формы значений.
func TestNTF1D05Injection(t *testing.T) {
	t.Parallel()
	root := repoRootOf(t)
	cases := []struct {
		name, rel, src, at string
		red                bool
	}{
		{"порча: импорт notify/spec в корне", "services/notify/cmd/notify-probe/zz_d05_inject.go",
			"package main\n\nimport _ \"github.com/PRO-Robotech/corelib/notify/spec\"\n",
			"services/notify/cmd/notify-probe/zz_d05_inject.go:3", true},
		{"порча: импорт notify/spec во внутреннем пакете глагола", "services/notify/cmd/notify-probe/internal/send/zz_d05_inject.go",
			"package send\n\nimport _ \"github.com/PRO-Robotech/corelib/notify/spec\"\n",
			"services/notify/cmd/notify-probe/internal/send/zz_d05_inject.go:3", true},
		{"близнец: импорт формы значений", "services/notify/cmd/notify-probe/zz_d05_inject.go",
			"package main\n\nimport _ \"github.com/PRO-Robotech/corelib/notify/form\"\n", "", false},
	}
	for _, tc := range cases {
		r := auditSourceRuntimeDeps(t, root, map[string]string{tc.rel: tc.src})
		got := strings.Join(r.findings, "\n")
		switch {
		case tc.red && !strings.Contains(got, tc.at):
			t.Errorf("%s: находки с координатой %s нет: %s", tc.name, tc.at, got)
		case tc.red && !strings.Contains(got, "рантайм источника тянет формат шаблонов"):
			t.Errorf("%s: находка называет не предмет: %s", tc.name, got)
		case !tc.red && len(r.findings) != 0:
			t.Errorf("%s: законный близнец объявлен находкой: %s", tc.name, got)
		}
		t.Logf("%s → %s; находки: %s", tc.name, r, got)
	}
}
