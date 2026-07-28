// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// console_registry_proof_test.go — доказательство, что охват консоли реагирует
// на СОДЕРЖИМОЕ, а не на факт выполнения.
//
// Гейт, который обходит дерево и всегда выходит с нулём, — сам экземпляр того
// класса, который он ищет. Поэтому здесь:
//
//  1. чистый ресурс даёт ноль находок, а тот же ресурс с одним лишним полем —
//     ровно одну, с именем этого поля;
//  2. ИСТОРИЧЕСКИЙ дефект воспроизведён дословно: поле, живущее только в
//     сообщении правки, предложено при создании — и находится;
//  3. конструкция, которую разбор не понимает, ЛОМАЕТ его, а не проходит молча:
//     ссылка на неизвестную константу, `fields` не массивом, `template` не
//     стрелкой с объектом;
//  4. обход дерева действительно находит реестры, и их число утверждается —
//     иначе «ноль нарушений» было бы неотличимо от «ноль прочитанных файлов».

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// probeRegistry собирает синтетический файл реестра вокруг одного ресурса.
func probeRegistry(body string) string {
	return "import type { FormField } from \"./form-schema\";\n\n" +
		"export const REGISTRY: Record<string, ResourceSpec> = {\n" + body + "\n};\n"
}

const cleanNetworkSpec = `
  networks: {
    id: "networks",
    route: "networks",
    apiPath: "/vpc/v1/networks",
    payloadKey: "networks",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [],
    fields: [
      { name: "name", label: "Имя", type: "string", required: true },
      { name: "description", label: "Описание", type: "text" },
      { name: "labels", label: "Метки", type: "labels" },
      { name: "project_id", label: "Project", type: "string", hidden: true },
    ],
    template: ({ projectId }) => ({ project_id: projectId ?? "", name: "", description: "", labels: {} }),
  },`

// findingsOf разбирает синтетический реестр и возвращает находки по нему.
func findingsOf(t *testing.T, src string) []consoleFinding {
	t.Helper()
	parsed, err := parseConsoleRegistry("probe.tsx", src, nil)
	if err != nil {
		t.Fatalf("parse probe registry: %v", err)
	}
	if len(parsed.Specs) != 1 {
		t.Fatalf("probe registry must yield exactly 1 resource, got %d", len(parsed.Specs))
	}
	return consoleSpecFindings(parsed.Specs[0])
}

// TestConsoleScannerDetectsInjectedField — красно-зелёная пара на одном и том же
// ресурсе: без лишнего поля и с ним.
func TestConsoleScannerDetectsInjectedField(t *testing.T) {
	if got := findingsOf(t, probeRegistry(cleanNetworkSpec)); len(got) != 0 {
		t.Fatalf("GREEN baseline broken: a clean resource produced %d finding(s): %v", len(got), got)
	}

	injected := strings.Replace(cleanNetworkSpec,
		`{ name: "description", label: "Описание", type: "text" },`,
		`{ name: "description", label: "Описание", type: "text" },
      { name: "descriptionn", label: "Опечатка", type: "string" },`, 1)
	// Поле мутабельное и не спрятанное — значит его предлагают ОБЕ формы, и
	// обе находки настоящие. Ожидать одну значило бы согласиться, что половина
	// поверхности не проверяется.
	got := findingsOf(t, probeRegistry(injected))
	if len(got) != 2 {
		t.Fatalf("injected field must be reported by both forms, got %d finding(s): %v", len(got), got)
	}
	byOp := map[string]consoleFinding{}
	for _, f := range got {
		byOp[f.Op] = f
	}
	for op, wantFQN := range map[string]string{
		"create": "kacho.cloud.vpc.v1.NetworkService/Create",
		"update": "kacho.cloud.vpc.v1.NetworkService/Update",
	} {
		f, ok := byOp[op]
		if !ok {
			t.Fatalf("the %s form did not report the injected field: %v", op, got)
		}
		if f.Kind != keyUnknown || f.Key != "descriptionn" {
			t.Fatalf("%s finding must name the injected field, got %+v", op, f)
		}
		if f.FQN != wantFQN {
			t.Fatalf("%s finding must attribute the form to the RPC that serves it, got %q", op, f.FQN)
		}
	}
}

// TestConsoleScannerCatchesUpdateOnlyFieldOfferedAtCreate — тот самый дефект,
// ради которого охват и заводится, воспроизведён дословно.
//
// Поле, которого сообщение СОЗДАНИЯ не несёт, а сообщение ПРАВКИ несёт, было
// предложено в форме создания. Оператор выбирал значение, край выбрасывал ключ,
// ресурс возвращался с другим — за успешным тостом. `updateOnly` — ровно то,
// что удерживает такое поле от участия в создании; сняв его, получаем дефект
// обратно.
func TestConsoleScannerCatchesUpdateOnlyFieldOfferedAtCreate(t *testing.T) {
	const registrySpec = `
  registries: {
    id: "registries",
    route: "registries",
    apiPath: "/registry/v1/registries",
    payloadKey: "registries",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [],
    fields: [
      { name: "name", label: "Имя", type: "string", required: true },
      { name: "region_id", label: "Регион", type: "ref", refResource: "regions", required: true, immutable: true },
      { name: "default_repository_visibility", label: "Видимость", type: "enum", %s default: "PRIVATE", options: [] },
      { name: "project_id", label: "Project", type: "string", hidden: true },
    ],
    template: ({ projectId }) => ({ project_id: projectId ?? "", name: "", region_id: "" }),
  },`

	guarded := probeRegistry(strings.Replace(registrySpec, "%s", "updateOnly: true,", 1))
	if got := findingsOf(t, guarded); len(got) != 0 {
		t.Fatalf("GREEN: the field is declared update-only, so nothing should be reported, got %v", got)
	}

	unguarded := probeRegistry(strings.Replace(registrySpec, "%s", "", 1))
	got := findingsOf(t, unguarded)
	if len(got) != 1 {
		t.Fatalf("RED: an update-only field offered at create must be reported exactly once, got %d: %v", len(got), got)
	}
	if got[0].Op != "create" || got[0].Key != "default_repository_visibility" {
		t.Fatalf("finding must name the field and the form that sends it, got %+v", got[0])
	}
	if got[0].Message != "kacho.cloud.registry.v1.CreateRegistryRequest" {
		t.Fatalf("finding must name the message that does not carry it, got %q", got[0].Message)
	}
}

// TestConsoleScannerRefusesWhatItCannotRead — разбор обязан ЛОМАТЬСЯ на том,
// чего не понимает. Пропуск здесь и есть тот «ноль без содержания», ради
// невозможности которого гейт пишется.
func TestConsoleScannerRefusesWhatItCannotRead(t *testing.T) {
	for name, spec := range map[string]string{
		"field references an unknown const": `
  networks: {
    apiPath: "/vpc/v1/networks",
    ops: { create: true, update: false, delete: false },
    fields: [FIELD_DECLARED_SOMEWHERE_ELSE],
    template: () => ({}),
  },`,
		"fields is not an array": `
  networks: {
    apiPath: "/vpc/v1/networks",
    ops: { create: true, update: false, delete: false },
    fields: buildFields(),
    template: () => ({}),
  },`,
		"template is not an arrow returning an object": `
  networks: {
    apiPath: "/vpc/v1/networks",
    ops: { create: true, update: false, delete: false },
    fields: [],
    template: makeTemplate,
  },`,
		"ops is missing": `
  networks: {
    apiPath: "/vpc/v1/networks",
    fields: [],
    template: () => ({}),
  },`,
		"apiPath is computed": `
  networks: {
    apiPath: base + "/networks",
    ops: { create: true, update: false, delete: false },
    fields: [],
    template: () => ({}),
  },`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConsoleRegistry("probe.tsx", probeRegistry(spec), nil); err == nil {
				t.Fatal("the scanner accepted a construct it does not model: it would then check nothing and say nothing")
			}
		})
	}
}

// TestConsoleScannerReadsEveryRegistryInTheTree — обход находит реестры, и их
// количество утверждается.
//
// Гейт, ничего не прочитавший, выглядит ровно как гейт, ничего не нашедший.
// Поэтому здесь фиксируется, что обходом найден КАЖДЫЙ файл с этим именем и что
// у каждого remote консоли, объявляющего ресурсы, реестр есть.
func TestConsoleScannerReadsEveryRegistryInTheTree(t *testing.T) {
	root := repoRoot(t)
	files, err := consoleRegistryFiles(filepath.Join(root, "ui-future"))
	if err != nil {
		t.Fatalf("walk ui-future: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("the walk found no console registry at all")
	}
	// Список путей был бы вторым источником истины и разошёлся бы с деревом;
	// вместо него — независимая перепроверка тем же деревом, но по шаблону пути.
	byGlob, err := filepath.Glob(filepath.Join(root, "ui-future", "*", "src", "lib", consoleRegistryFileName))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(byGlob) != len(files) {
		t.Errorf("the walk found %d registries, the path pattern finds %d: one of them is missing files, and every resource in a missing file goes unchecked\n  walk: %v\n  glob: %v",
			len(files), len(byGlob), files, byGlob)
	}

	extern, err := consoleExportedStringConsts(filepath.Join(root, "ui-future"))
	if err != nil {
		t.Fatalf("collect exported string consts: %v", err)
	}
	for _, file := range files {
		rel := mustRel(root, file)
		blob, err := os.ReadFile(file) //nolint:gosec // путь получен обходом дерева репозитория
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		parsed, err := parseConsoleRegistry(rel, string(blob), extern)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if len(parsed.Specs) == 0 {
			t.Errorf("%s: parsed without error, yet not one resource came out of it", rel)
		}
	}
}

// TestConsoleMutationCallSeesARawFetch pins what counts as "this place sends a
// body to the server".
//
// The detector used to look only for the typed wrapper (`api.create`,
// `api.update`, `api.post`). A raw `fetch` with a mutating method was invisible
// to it — so a new hand-built call site could be added and the accounting guard
// above would keep reporting a complete surface. The blind spot was the
// detector's own, one level beneath the blind spot it exists to name.
//
// This is not hypothetical for this codebase: the console already carried raw
// `fetch` calls on the access-binding pages, which bypassed case conversion and
// did not even check whether the server had refused.
func TestConsoleMutationCallSeesARawFetch(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"typed wrapper", `await api.update(path, body)`, true},
		{"raw fetch with a body", "await fetch(`/vpc/v1/networks/${id}`, { method: \"PATCH\", body: JSON.stringify(b) })", true},
		{"raw fetch, method on another line", "await fetch(url, {\n  method: \"POST\",\n  body: payload,\n})", true},
		{"plain read is not a mutation", `const r = await fetch(url)`, false},
		{"a word that merely contains fetch", `const prefetching = true`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := consoleMutationCall.MatchString(c.src); got != c.want {
				t.Fatalf("detector saw %v, want %v for:\n%s", got, c.want, c.src)
			}
		})
	}
}
