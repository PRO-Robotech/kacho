// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// census_test.go locks the gate's own premise: a run must say how much of the tree
// it opened, so "no findings" can never be read off "nothing read".
//
// Everything here drives the COMMAND the pipeline issues — `make -C services/registry
// audit-list-filter` for the real tree, and the very binary that target runs
// (`--root=<tree>`) for the injected ones — never the analyser function underneath.
// A census that only the library prints, or a finding only the library returns, would
// leave the thing CI actually executes unproven.
//
// The injections are made on COPIES OF THE REAL TREE rather than on hand-written
// fixtures, in both directions: a real resource losing its per-object filter must go
// red and name where, and a real refactor of the same shape — the same method moved
// to another file of the same package with its receiver renamed — must stay silent.
// A gate that only recognises the shape it was written against catches the fixture
// and misses the service.
package auditlistfilter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runCommand runs the gate exactly as the Makefile target does — same binary, same
// flags — against the tree at root, and returns its combined output and verdict.
func runCommand(t *testing.T, root string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", "run", "./tools/auditlistfilter/cmd/audit-list-filter", "--root", root)
	cmd.Dir = serviceRoot(t)
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}

// runMakeTarget runs `make -C services/registry audit-list-filter`, i.e. the literal
// command CI issues (ci_wiring_test.go asserts that it is the one wired).
func runMakeTarget(t *testing.T) (string, error) {
	t.Helper()
	cmd := exec.Command("make", "-C", serviceRoot(t), "audit-list-filter")
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}

// copyRealTree copies the parts of the real kacho-registry service the gate reads —
// the handler package and the composition root — into a temporary directory, so an
// injection can be made on real code without touching the working tree.
func copyRealTree(t *testing.T) string {
	t.Helper()
	src := serviceRoot(t)
	dst := t.TempDir()
	for _, rel := range []string{filepath.Join("internal", "handler"), "cmd"} {
		from := filepath.Join(src, rel)
		err := filepath.WalkDir(from, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			// _test.go КОПИРУЮТСЯ. Анализатор их не парсит, но одна проверка читает их
			// как ЗАКРЕПЛЕНИЕ предпосылки заявленного исключения (отказ ручки в
			// production). Копия без тестов лишала бы это закрепление предмета, и
			// инъекционные прогоны видели бы дерево иначе, чем настоящее.
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			r, relErr := filepath.Rel(src, path)
			if relErr != nil {
				return relErr
			}
			target := filepath.Join(dst, r)
			if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
				return mkErr
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			return os.WriteFile(target, data, 0o644)
		})
		if err != nil {
			t.Fatalf("copy %s: %v", rel, err)
		}
	}
	return dst
}

// mustReplace rewrites a file of the copied tree, failing the test when the anchor it
// is told to replace is not there — an injection that silently did nothing would make
// the gate look good for the wrong reason.
func mustReplace(t *testing.T, root, rel, oldText, newText string) {
	t.Helper()
	path := filepath.Join(root, rel)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if !strings.Contains(string(raw), oldText) {
		t.Fatalf("injection anchor not found in %s — the real tree moved, so this test "+
			"is no longer injecting what it claims:\n%s", rel, oldText)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), oldText, newText, 1)), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// realListMethod is the real RegistryHandler.List, verbatim from public.go. It is the
// anchor of both injections below: removing its filter must go red, and moving it
// elsewhere with a different receiver name must not.
const realListMethod = `func (h *RegistryHandler) List(ctx context.Context, req *registryv1.ListRegistriesRequest) (*registryv1.ListRegistriesResponse, error) {
	items, next, err := h.uc.List(ctx, registry.ListQuery{
		ProjectID: req.GetProjectId(),
		PageSize:  int64(req.GetPageSize()),
		PageToken: req.GetPageToken(),
		Filter:    req.GetFilter(),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	filtered, err := h.authz.filterRegistries(ctx, items)
	if err != nil {
		return nil, err
	}
	resp := &registryv1.ListRegistriesResponse{NextPageToken: next}
	for _, r := range filtered {
		resp.Registries = append(resp.Registries, h.uc.ProtoRegistry(r))
	}
	return resp, nil
}`

// TestGateStatesItsCensus — a passing run must report the extent of what it opened.
//
// The gate printed exactly `audit-list-filter: OK` and nothing else, so its output
// could not tell "five RPCs judged" from "the tree was never found". Every sibling
// gate of this class states its census; this asserts registry's does too, through the
// command CI issues rather than through the analyser.
func TestGateStatesItsCensus(t *testing.T) {
	out, err := runMakeTarget(t)
	if err != nil {
		t.Fatalf("gate must pass against the real kacho-registry tree: %v\n--- output ---\n%s", err, out)
	}
	for _, want := range []string{
		"handler file",                 // how much was read
		"composition-root file",        // …including the wiring half
		"List RPC",                     // how many resources were found
		"checked RegistryHandler.List", // and which of them were judged
	} {
		if !strings.Contains(out, want) {
			t.Errorf("census must state %q\n--- output ---\n%s", want, out)
		}
	}
	// A census of zero is the failure this exists to expose; it must never read as a pass.
	for _, never := range []string{"examined 0 ", "0 List RPC", "0 checked"} {
		if strings.Contains(out, never) {
			t.Errorf("gate passed while reporting %q — that is not a pass\n--- output ---\n%s", never, out)
		}
	}
	t.Log(strings.TrimSpace(out))
}

// TestGateInjectionOnRealTree — the two directions, on real code.
func TestGateInjectionOnRealTree(t *testing.T) {
	t.Run("defect: a real List loses its per-object filter", func(t *testing.T) {
		root := copyRealTree(t)
		mustReplace(t, root, filepath.Join("internal", "handler", "public.go"),
			`	filtered, err := h.authz.filterRegistries(ctx, items)
	if err != nil {
		return nil, err
	}
	resp := &registryv1.ListRegistriesResponse{NextPageToken: next}
	for _, r := range filtered {`,
			`	resp := &registryv1.ListRegistriesResponse{NextPageToken: next}
	for _, r := range items {`)

		out, err := runCommand(t, root)
		if err == nil {
			t.Fatalf("removing the per-object filter from a real List must fail the gate\n--- output ---\n%s", out)
		}
		for _, want := range []string{"RegistryHandler.List", "filterRegistries", "public.go:"} {
			if !strings.Contains(out, want) {
				t.Errorf("the finding must name %q — a red without a coordinate is not actionable\n--- output ---\n%s", want, out)
			}
		}
		t.Log(strings.TrimSpace(out))
	})

	t.Run("legitimate: the same method moved to another file, receiver renamed", func(t *testing.T) {
		root := copyRealTree(t)
		// Delete it from public.go…
		mustReplace(t, root, filepath.Join("internal", "handler", "public.go"), realListMethod, "")
		// …and re-declare it, unchanged in substance, in a sibling file of the same
		// package under a different receiver name. Splitting a package and renaming a
		// receiver are refactors no reviewer would stop; neither may move a resource
		// out of the gate's view, and neither may manufacture a red.
		moved := "package handler\n\n" + strings.ReplaceAll(realListMethod, "h.", "hd.")
		moved = strings.Replace(moved, "func (h *RegistryHandler)", "func (hd *RegistryHandler)", 1)
		if err := os.WriteFile(filepath.Join(root, "internal", "handler", "list_registries.go"), []byte(moved+"\n"), 0o644); err != nil {
			t.Fatalf("write list_registries.go: %v", err)
		}

		out, err := runCommand(t, root)
		if err != nil {
			t.Fatalf("a legitimate refactor of the same shape must stay silent: %v\n--- output ---\n%s", err, out)
		}
		if !strings.Contains(out, "checked RegistryHandler.List") {
			t.Errorf("the moved method must still be judged, not merely unnoticed\n--- output ---\n%s", out)
		}
	})

	t.Run("unjudged: a new List RPC nobody declared", func(t *testing.T) {
		// The census counts unjudged RPCs, and a count that has only ever been zero
		// proves nothing. This moves it: a List RPC added to the real handler must
		// appear in the census as unjudged AND be a finding — registry has no
		// whitelist, so nothing can leave the gate's judgement quietly.
		root := copyRealTree(t)
		path := filepath.Join(root, "internal", "handler", "public.go")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read public.go: %v", err)
		}
		added := string(raw) + `
func (h *RegistryHandler) ListWebhooks(ctx context.Context, req *registryv1.ListWebhooksRequest) (*registryv1.ListWebhooksResponse, error) {
	hooks, next, err := h.uc.ListWebhooks(ctx, req.GetRegistryId())
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &registryv1.ListWebhooksResponse{NextPageToken: next}
	for _, w := range hooks {
		resp.Webhooks = append(resp.Webhooks, toProtoWebhook(w))
	}
	return resp, nil
}
`
		if werr := os.WriteFile(path, []byte(added), 0o644); werr != nil {
			t.Fatalf("write public.go: %v", werr)
		}

		out, cerr := runCommand(t, root)
		if cerr == nil {
			t.Fatalf("a List RPC with no declared enforcement must fail the gate\n--- output ---\n%s", out)
		}
		for _, want := range []string{"1 unjudged", "unjudged RegistryHandler.ListWebhooks", "public.go:"} {
			if !strings.Contains(out, want) {
				t.Errorf("output must state %q\n--- output ---\n%s", want, out)
			}
		}
		t.Log(strings.TrimSpace(out))
	})

	t.Run("expired declaration: a rule whose handler is gone", func(t *testing.T) {
		root := copyRealTree(t)
		path := filepath.Join(root, "internal", "handler", "public.go")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read public.go: %v", err)
		}
		src := string(raw)
		start := strings.Index(src, "func (h *RegistryHandler) ListOperations(")
		if start < 0 {
			t.Fatal("ListOperations not found in the real public.go — this test no longer injects what it claims")
		}
		end := strings.Index(src[start:], "\n}\n")
		if end < 0 {
			t.Fatal("could not delimit ListOperations")
		}
		if werr := os.WriteFile(path, []byte(src[:start]+src[start+end+3:]), 0o644); werr != nil {
			t.Fatalf("write public.go: %v", werr)
		}

		out, cerr := runCommand(t, root)
		if cerr == nil {
			t.Fatalf("an enforcement rule whose handler is gone must be reported\n--- output ---\n%s", out)
		}
		if !strings.Contains(out, "ListOperations") {
			t.Errorf("the finding must name the expired declaration\n--- output ---\n%s", out)
		}
		t.Log(strings.TrimSpace(out))
	})
}

// TestGateNothingExaminedIsNotOK — "zero findings" must be unreachable from "zero
// read", and the refusal must say so in words an operator can act on.
//
// The absent-root case used to answer with a bare `read …: no such file or directory`
// and no census at all: a gate pointed at the wrong tree said nothing about having
// read nothing.
func TestGateNothingExaminedIsNotOK(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{
			name:  "the tree is not there at all",
			setup: func(t *testing.T) string { return t.TempDir() },
		},
		{
			name: "the handler package exists and holds no Go source",
			setup: func(t *testing.T) string {
				root := t.TempDir()
				for _, d := range []string{filepath.Join("internal", "handler"), "cmd"} {
					if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
						t.Fatalf("mkdir %s: %v", d, err)
					}
				}
				return root
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runCommand(t, tc.setup(t))
			if err == nil {
				t.Fatalf("the gate examined nothing and did not fail\n--- output ---\n%s", out)
			}
			if strings.Contains(out, "audit-list-filter: OK") {
				t.Fatalf("the gate reported OK having read nothing\n--- output ---\n%s", out)
			}
			// The census belongs on this path too: the number that explains the
			// refusal is the number of files read.
			if !strings.Contains(out, "handler file") {
				t.Errorf("the refusal must state how much was read\n--- output ---\n%s", out)
			}
			if !strings.Contains(out, "nothing") {
				t.Errorf("the refusal must say that nothing was inspected, not merely that a path is missing\n--- output ---\n%s", out)
			}
			t.Log(strings.TrimSpace(out))
		})
	}
}

// TestGateVerdictSignalIsFlowSensitive — предпосылка признака «вердикт гейта
// проверен». Признак собирался из ВСЕГО тела метода: множество сравнений
// `<ident> != nil` где угодно, включая ветку ДРУГОГО вызова и позицию ДО гейта. Имя
// `err` есть почти в каждом методе, поэтому признак выполнялся у метода, который
// вердикт гейта не смотрел вовсе.
//
// Инъекция в обе стороны, на копии настоящего дерева:
//   (а) вердикт гейта не проверяется, но рядом стоит НЕ ОТНОСЯЩЕЕСЯ к нему `err != nil`
//       → гейт обязан краснеть и назвать место;
//   (б) вердикт проверяется как положено → гейт обязан молчать (это и есть настоящее
//       дерево, оно проверяется остальными тестами файла).
func TestGateVerdictSignalIsFlowSensitive(t *testing.T) {
	t.Run("defect: an unrelated err != nil satisfies the signal", func(t *testing.T) {
		root := copyRealTree(t)
		mustReplace(t, root, filepath.Join("internal", "handler", "public.go"),
			`	if err := h.authz.checkRepo(ctx, registryID, repository, relationVList); err != nil {
		return nil, err
	}`,
			`	if err := ctx.Err(); err != nil {
		return nil, err
	}
	verdict := h.authz.checkRepo(ctx, registryID, repository, relationVList)
	_ = verdict`)

		out, err := runCommand(t, root)
		if err == nil {
			t.Fatalf("a gate whose verdict is dropped must fail the audit\n--- output ---\n%s", out)
		}
		if !strings.Contains(out, "verdict is never tested") {
			t.Errorf("the finding must say the verdict is not tested\n--- output ---\n%s", out)
		}
		if !strings.Contains(out, "public.go:") {
			t.Errorf("a red without a coordinate is not actionable\n--- output ---\n%s", out)
		}
		t.Log(strings.TrimSpace(out))
	})

	t.Run("legitimate: a verdict tested in a branch that returns stays silent", func(t *testing.T) {
		// Тот же метод, переписанный в двухшаговую форму: присваивание, затем ветвь с
		// возвратом. Существо то же, форма другая — гейт обязан молчать, иначе он ловит
		// написание, а не смысл, и первый же безобидный рефактор его отключит.
		root := copyRealTree(t)
		mustReplace(t, root, filepath.Join("internal", "handler", "public.go"),
			`	if err := h.authz.checkRepo(ctx, registryID, repository, relationVList); err != nil {
		return nil, err
	}`,
			`	scopeErr := h.authz.checkRepo(ctx, registryID, repository, relationVList)
	if scopeErr != nil {
		return nil, scopeErr
	}`)

		out, err := runCommand(t, root)
		if err != nil {
			t.Fatalf("the same check in another shape must stay silent: %v\n--- output ---\n%s", err, out)
		}
		t.Log(strings.TrimSpace(out))
	})
}

// TestGateSeesANilAuthorizerBehindAVariable — предпосылка проверки «корень не собирает
// обработчик без авторизатора». Она распознавала ТОЛЬКО литерал `nil`, а настоящий
// корень передаёт переменную, — то есть не могла сработать ни разу.
//
// Три направления:
//   (а) переменной присвоен nil → находка;
//   (б) объявлена без инициализатора и присвоена только внутри условия, а закрепления
//       отказа ручки в production НЕТ → находка (исключение без предмета не живёт);
//   (в) присвоена безусловно → молчание.
func TestGateSeesANilAuthorizerBehindAVariable(t *testing.T) {
	realWiring := `	var listAuthz handler.Authorizer
	if authzConn != nil {
		listAuthz = check.NewIAMCheckClient(authzConn)
	}`

	t.Run("defect: the variable is assigned nil", func(t *testing.T) {
		root := copyRealTree(t)
		mustReplace(t, root, filepath.Join("cmd", "kacho-registry", "serve.go"), realWiring,
			`	var listAuthz handler.Authorizer
	listAuthz = nil`)

		out, err := runCommand(t, root)
		if err == nil {
			t.Fatalf("a nil authorizer behind a variable must fail the audit\n--- output ---\n%s", out)
		}
		if !strings.Contains(out, "nil authorizer") || !strings.Contains(out, "serve.go:") {
			t.Errorf("the finding must name the shape and the place\n--- output ---\n%s", out)
		}
		t.Log(strings.TrimSpace(out))
	})

	t.Run("defect: conditional-only assignment with no production refusal pinned", func(t *testing.T) {
		root := copyRealTree(t)
		// Настоящая форма корня остаётся; убираем ЗАКРЕПЛЕНИЕ отказа ручки. Исключение,
		// у которого не стало предмета, обязано становиться находкой — иначе оно
		// унаследует слепую зону.
		unpinTests(t, root)

		out, err := runCommand(t, root)
		if err == nil {
			t.Fatalf("without the production-refusal pin the conditional nil must be a finding\n--- output ---\n%s", out)
		}
		if !strings.Contains(out, "assigned only inside a conditional") {
			t.Errorf("the finding must name how nil is reachable\n--- output ---\n%s", out)
		}
		t.Log(strings.TrimSpace(out))
	})

	t.Run("legitimate: unconditional assignment stays silent", func(t *testing.T) {
		root := copyRealTree(t)
		mustReplace(t, root, filepath.Join("cmd", "kacho-registry", "serve.go"), realWiring,
			`	listAuthz := check.NewIAMCheckClient(authzConn)`)

		out, err := runCommand(t, root)
		if err != nil {
			t.Fatalf("an unconditionally built authorizer must stay silent: %v\n--- output ---\n%s", err, out)
		}
		t.Log(strings.TrimSpace(out))
	})
}

// unpinTests removes, from the copied composition root, the tests that pin the
// production refusal of the knob which can leave the authorizer nil.
func unpinTests(t *testing.T, root string) {
	t.Helper()
	removed := 0
	err := filepath.WalkDir(filepath.Join(root, "cmd"), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if rmErr := os.Remove(path); rmErr != nil {
			return rmErr
		}
		removed++
		return nil
	})
	if err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if removed == 0 {
		t.Fatalf("unpin removed nothing — the copied tree carries no composition-root tests, " +
			"so this direction of the injection proves nothing")
	}
}
