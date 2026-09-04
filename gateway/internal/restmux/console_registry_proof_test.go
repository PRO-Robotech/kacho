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
//  4. обход дерева действительно находит реестры, и их число утверждается ПО
//     ФОРМАМ — иначе «ноль нарушений» было бы неотличимо от «ноль прочитанных
//     файлов», а проекции общего реестра — от реестров со спеками;
//  5. проекция ведёт К осматриваемому реестру, а не выводится из обхода: домен,
//     чьи спеки переехали в общий реестр, обязан оставаться проверяемым — там.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
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
	parsed, err := parseConsoleRegistry("probe.tsx", src, consoleExterns{})
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
			if _, err := parseConsoleRegistry("probe.tsx", probeRegistry(spec), consoleExterns{}); err == nil {
				t.Fatal("the scanner accepted a construct it does not model: it would then check nothing and say nothing")
			}
		})
	}
}

// ── Проекция общего реестра ─────────────────────────────────────────────────
//
// Форм записи реестра в дереве ДВЕ, и вторая появилась вместе со сведением
// форка: домен, чьи спеки переехали в общий реестр, держит не копию, а
// ре-экспорт модуля целиком. Константы на верхнем уровне у такого файла нет —
// и сканер, знающий одну форму, краснел на верном коде.

// probeProjection — файл-проекция дословно той формы, что несёт дерево:
// комментарий и ОДИН оператор — ре-экспорт модуля целиком.
const probeProjection = "// Реестр ресурсов — ОДИН на всю консоль.\n" +
	"//\n" +
	"// Здесь стояла копия; содержание сведено, и остался указатель.\n" +
	"export * from \"@shared/lib/resource-registry\";\n"

// TestConsoleScannerReadsAProjectionOfTheSharedRegistry — вторая законная форма
// читается, а не роняет разбор.
func TestConsoleScannerReadsAProjectionOfTheSharedRegistry(t *testing.T) {
	parsed, err := parseConsoleRegistry("probe.tsx", probeProjection, consoleExterns{})
	if err != nil {
		t.Fatalf("проекция общего реестра — законная форма записи, а разбор её отверг: %v", err)
	}
	if len(parsed.Specs) != 0 {
		t.Fatalf("у проекции своих спек нет by construction, разбор дал %d", len(parsed.Specs))
	}
}

// TestConsoleScannerRefusesAFileThatIsNeitherForm — требование не ослаблено:
// файл, не объявляющий реестр и не проецирующий его, по-прежнему ломает разбор.
//
// Это вторая половина пары. Без неё «сканер научился читать проекцию»
// неотличимо от «сканер перестал требовать хоть что-нибудь», а такое послабление
// дало бы зелёное при непроверенных спеках — то есть маску.
func TestConsoleScannerRefusesAFileThatIsNeitherForm(t *testing.T) {
	for name, src := range map[string]string{
		"пустой файл":                     "",
		"одни комментарии":                "// реестр переехал\n// а куда — не сказано\n",
		"копия без ре-экспорта":           "const REGISTRY_OLD = {};\n",
		"ре-экспорт ИМЁН, не модуля":      "export { REGISTRY } from \"@shared/lib/resource-registry\";\n",
		"ре-экспорт плюс своё объявление": "export * from \"@shared/lib/resource-registry\";\nexport const EXTRA = { a: \"b\" };\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConsoleRegistry("probe.tsx", src, consoleExterns{}); err == nil {
				t.Fatal("сканер принял файл, который не является ни реестром, ни проекцией: он тогда ничего не проверяет и ничего об этом не говорит")
			}
		})
	}
}

// TestConsoleProjectionModuleResolves — спецификатор ре-экспорта разрешается в
// путь файла, а неразрешимый остаётся ОТКАЗОМ.
//
// Алиас берётся из tsconfig приложения; здесь он подаётся тем же видом, что
// собирает чтение дерева, — дублёр, собранный отдельно, принимал бы не то же
// самое.
func TestConsoleProjectionModuleResolves(t *testing.T) {
	aliases := map[string]string{"@shared/": "/ui/shared/src", "@/": "/ui/compute/src"}
	const dir = "/ui/compute/src/lib"

	for name, tc := range map[string]struct{ spec, want string }{
		"через алиас":     {"@shared/lib/resource-registry", "/ui/shared/src/lib/resource-registry.tsx"},
		"через свой":      {"@/lib/resource-registry", "/ui/compute/src/lib/resource-registry.tsx"},
		"относительный":   {"../../../shared/src/lib/resource-registry", "/ui/shared/src/lib/resource-registry.tsx"},
		"соседним файлом": {"./resource-registry", "/ui/compute/src/lib/resource-registry.tsx"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := consoleResolveModule(dir, aliases, tc.spec)
			if err != nil {
				t.Fatalf("%q не разрешился, хотя форма законная: %v", tc.spec, err)
			}
			if got != tc.want {
				t.Fatalf("%q разрешился в %q, ожидалось %q", tc.spec, got, tc.want)
			}
		})
	}

	// Обратная сторона: спецификатор, не покрытый ни одним алиасом, — отказ.
	// Молчаливый пропуск дал бы проекцию, ведущую в никуда, при зелёном гейте.
	if got, err := consoleResolveModule(dir, aliases, "@nowhere/lib/resource-registry"); err == nil {
		t.Fatalf("спецификатор без алиаса разрешился в %q: проекция вела бы туда, куда сканер не смотрит", got)
	}
}

// TestConsoleProjectionMustLeadToAScannedRegistry — проекция ведёт К РЕЕСТРУ,
// который сканер читает, а не выводится из обхода.
//
// В этом и состоит различие между второй законной формой и маской. Исключить
// проекцию из обхода было бы дёшево и дало бы зелёное — при том что спеки
// домена, переехавшие в общий реестр, никто бы не проверял. Пара утверждений
// ниже роняет ровно этот случай и молчит на законном.
func TestConsoleProjectionMustLeadToAScannedRegistry(t *testing.T) {
	declaring := map[string]bool{"ui-future/shared/src/lib/resource-registry.tsx": true}

	good := map[string]string{"ui-future/compute/src/lib/resource-registry.tsx": "ui-future/shared/src/lib/resource-registry.tsx"}
	if got := consoleProjectionsWithoutARegistry(good, declaring); len(got) != 0 {
		t.Fatalf("проекция ведёт в осматриваемый реестр, а гейт нашёл %d: %v", len(got), got)
	}

	bad := map[string]string{"ui-future/compute/src/lib/resource-registry.tsx": "ui-future/shared/src/lib/registry-that-nobody-reads.tsx"}
	got := consoleProjectionsWithoutARegistry(bad, declaring)
	if len(got) != 1 {
		t.Fatalf("проекция ведёт туда, где реестра нет, — обязана быть находка, получено %d: %v", len(got), got)
	}
	for _, want := range []string{"ui-future/compute/src/lib/resource-registry.tsx", "registry-that-nobody-reads.tsx"} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("находка обязана называть и проекцию, и то, куда она ведёт; %q не содержит %q", got[0], want)
		}
	}
}

// TestConsoleScannerReadsEveryRegistryInTheTree — обход находит реестры, и их
// количество утверждается ПО ФОРМАМ.
//
// Гейт, ничего не прочитавший, выглядит ровно как гейт, ничего не нашедший.
// Поэтому здесь фиксируется, что обходом найден КАЖДЫЙ файл с этим именем и что
// у каждого remote консоли, объявляющего ресурсы, реестр есть.
//
// Форм записи ДВЕ, и складывать их в одно число нельзя: пять проекций и ни
// одного объявления дали бы «прочитано пять реестров» при нуле прочитанных
// спек. Поэтому перепись печатает обе величины, а проекция обязана вести к
// реестру, который сканер читает КАК РЕЕСТР, — исключить её из обхода значило
// бы получить зелёное за непроверенный домен.
func TestConsoleScannerReadsEveryRegistryInTheTree(t *testing.T) {
	root := repoRoot(t)
	consoleRoot := filepath.Join(root, "ui-future")
	files, err := consoleRegistryFiles(consoleRoot)
	if err != nil {
		t.Fatalf("walk ui-future: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("the walk found no console registry at all")
	}
	// Список путей был бы вторым источником истины и разошёлся бы с деревом;
	// вместо него — независимая перепроверка тем же деревом, но по шаблону пути.
	byGlob, err := treecorpus.Glob(filepath.Join(consoleRoot, "*", "src", "lib", consoleRegistryFileName))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(byGlob) != len(files) {
		t.Errorf("the walk found %d registries, the path pattern finds %d: one of them is missing files, and every resource in a missing file goes unchecked\n  walk: %v\n  glob: %v",
			len(files), len(byGlob), files, byGlob)
	}

	ext, err := consoleTreeExterns(consoleRoot)
	if err != nil {
		t.Fatal(err)
	}

	var forms consoleRegistryForms
	declaring := map[string]bool{}
	targets := map[string]string{}
	for _, file := range files {
		rel := mustRel(root, file)
		blob, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		parsed, err := parseConsoleRegistry(rel, string(blob), ext)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		forms.add(parsed)
		if parsed.Projection != "" {
			target, err := consoleProjectionTarget(consoleRoot, file, parsed.Projection)
			if err != nil {
				t.Errorf("%s:%d: %v", rel, parsed.ProjectionLine, err)
				continue
			}
			targets[rel] = mustRel(root, target)
			continue
		}
		declaring[rel] = true
		if len(parsed.Specs) == 0 {
			t.Errorf("%s: parsed without error, yet not one resource came out of it", rel)
		}
	}
	for _, finding := range consoleProjectionsWithoutARegistry(targets, declaring) {
		t.Error(finding)
	}
	// Ноль ОБЪЯВЛЯЮЩИХ — не «нарушений нет», а «спек не прочитано ни одной»:
	// проекции сами по себе не несут ничего, что можно было бы проверить.
	if forms.Declaring == 0 {
		t.Fatal("every registry in the tree is a projection of another one: not a single resource was read, and a scan that read nothing is not a scan that found nothing")
	}
	t.Logf("перепись: %s", forms)
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

// composedRegistry — синтетический реестр, чьи поля собраны помощником с
// подстановкой аргумента. Форма дословно повторяет ту, что реестр использует
// для двух симметричных семейств VIP-источника.
func composedRegistry(helperBody, spec string) string {
	return "import type { FormField } from \"./form-schema\";\n\n" +
		helperBody + "\n\n" +
		"export const REGISTRY: Record<string, ResourceSpec> = {\n" + spec + "\n};\n"
}

const composedHelper = "function familyFields(family: \"v4\" | \"v6\", label: string): FormField[] {\n" +
	"  const mode = `_${family}_source`;\n" +
	"  return [\n" +
	"    { name: mode, label: `Источник (${label})`, type: \"enum\", immutable: true, options: [] },\n" +
	"    { name: `${family}_source.subnet_id`, label: `Подсеть (${label})`, type: \"ref\", refResource: \"subnets\", immutable: true },\n" +
	"    { name: `${family}_source.address_id`, label: `Адрес (${label})`, type: \"ref\", refResource: \"addresses\", immutable: true },\n" +
	"  ];\n" +
	"}"

const composedSpec = `
  "load-balancers": {
    id: "load-balancers",
    route: "load-balancers",
    apiPath: "/nlb/v1/networkLoadBalancers",
    payloadKey: "networkLoadBalancers",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [],
    fields: [
      { name: "name", label: "Имя", type: "string", required: true },
      ...familyFields("v4", "IPv4"),
      ...familyFields("v6", "IPv6"),
      { name: "project_id", label: "Project", type: "string", hidden: true },
    ],
    template: ({ projectId }) => ({ project_id: projectId ?? "", name: "" }),
  },`

// TestConsoleScannerExpandsComposedFieldSets — набор полей, вынесенный в
// помощник, раскрывается с подстановкой аргументов вызова.
//
// Утверждаются ИМЕНА, а не только количество: раскрытие, давшее нужное число
// полей с неразрешёнными именами, прошло бы проверку на количество и не
// проверило бы ни одного настоящего ключа.
func TestConsoleScannerExpandsComposedFieldSets(t *testing.T) {
	parsed, err := parseConsoleRegistry("probe.tsx", composedRegistry(composedHelper, composedSpec), consoleExterns{})
	if err != nil {
		t.Fatalf("parse composed registry: %v", err)
	}
	if len(parsed.Specs) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(parsed.Specs))
	}
	var got []string
	for _, f := range parsed.Specs[0].Fields {
		got = append(got, f.Name)
	}
	want := []string{
		"name",
		"_v4_source", "v4_source.subnet_id", "v4_source.address_id",
		"_v6_source", "v6_source.subnet_id", "v6_source.address_id",
		"project_id",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d fields, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("field %d: expected %q, got %q (all: %v)", i, want[i], got[i], got)
		}
	}
}

// TestConsoleScannerChecksInsideAComposedFieldSet — красно-зелёная пара НА
// ПОМОЩНИКЕ: поле, добавленное внутрь набора, проверяется в каждом развороте.
//
// Без этого «раскрытие работает» означало бы лишь «разбор не упал»: набор мог бы
// раскрываться и не участвовать в сверке, а гейт оставался бы зелёным ровно там,
// куда его только что расширили.
func TestConsoleScannerChecksInsideAComposedFieldSet(t *testing.T) {
	clean := composedRegistry(composedHelper, composedSpec)
	parsed, err := parseConsoleRegistry("probe.tsx", clean, consoleExterns{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := consoleSpecFindings(parsed.Specs[0]); len(got) != 0 {
		t.Fatalf("GREEN baseline broken: composed set produced %d finding(s): %v", len(got), got)
	}

	injected := strings.Replace(composedHelper,
		"    { name: `${family}_source.address_id`,",
		"    { name: `${family}_source.bogus_ref`, label: \"x\", type: \"string\", immutable: true },\n"+
			"    { name: `${family}_source.address_id`,", 1)
	parsed, err = parseConsoleRegistry("probe.tsx", composedRegistry(injected, composedSpec), consoleExterns{})
	if err != nil {
		t.Fatalf("parse injected: %v", err)
	}
	got := consoleSpecFindings(parsed.Specs[0])
	// Помощник разворачивается дважды — значит и находок две, по одной на
	// семейство, с РАЗНЫМИ подставленными именами. Инъекция минимальна:
	// добавленное поле несёт те же флаги, что соседи по набору, иначе она
	// меняла бы сразу две вещи и находки пришлось бы объяснять.
	if len(got) != 2 {
		t.Fatalf("a field added inside the helper must be reported once per expansion, got %d: %v", len(got), got)
	}
	seen := map[string]bool{}
	for _, f := range got {
		seen[f.Key] = true
	}
	for _, want := range []string{"v4_source.bogus_ref", "v6_source.bogus_ref"} {
		if !seen[want] {
			t.Fatalf("expected a finding naming %q, got %v", want, got)
		}
	}
}

// TestConsoleScannerRefusesCompositionItCannotRead — граница модели композиции.
//
// Раскрывается ровно одна форма: вызов помощника ЭТОГО файла, чьё тело —
// связывания и возврат массива, с литеральными аргументами. За её пределами
// разбор обязан ЛОМАТЬСЯ: набор полей, состав которого не виден в исходнике,
// нельзя ни проверить, ни честно объявить проверенным.
func TestConsoleScannerRefusesCompositionItCannotRead(t *testing.T) {
	for name, tc := range map[string]struct{ helper, spec string }{
		"helper is not declared in this file": {
			helper: "const unrelated = 1;",
			spec:   composedSpec,
		},
		"argument is computed, not a literal": {
			helper: composedHelper,
			spec:   strings.Replace(composedSpec, `...familyFields("v4", "IPv4"),`, `...familyFields(pickFamily(), "IPv4"),`, 1),
		},
		"helper body branches instead of returning a literal": {
			helper: "function familyFields(family: string, label: string): FormField[] {\n" +
				"  if (family === \"v4\") return [];\n" +
				"  return [{ name: \"x\", label: \"x\", type: \"string\" }];\n" +
				"}",
			spec: composedSpec,
		},
		"helper returns something that is not an array": {
			helper: "function familyFields(family: string, label: string): FormField[] {\n" +
				"  return buildThem(family);\n" +
				"}",
			spec: composedSpec,
		},
		"field name is a computed expression, not a substituted name": {
			helper: strings.Replace(composedHelper,
				"{ name: `${family}_source.subnet_id`,",
				"{ name: `${family.toUpperCase()}_source.subnet_id`,", 1),
			spec: composedSpec,
		},
		"call passes a different number of arguments": {
			helper: composedHelper,
			spec:   strings.Replace(composedSpec, `...familyFields("v4", "IPv4"),`, `...familyFields("v4"),`, 1),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConsoleRegistry("probe.tsx", composedRegistry(tc.helper, tc.spec), consoleExterns{}); err == nil {
				t.Fatal("the scanner accepted a composition it cannot read: it would then check nothing and say nothing")
			}
		})
	}
}

// TestConsoleComposedSetsAreActuallyExpandedInTheTree — раскрытие работает НА
// РЕАЛЬНОМ дереве, а не только на синтетике.
//
// Утверждается количество РАСКРЫТОГО, а не число прочитанных файлов: раскрытие,
// давшее ноль полей, выглядит ровно так же зелено, как раскрытие, давшее шесть.
// И если реестр однажды перестанет собирать поля помощником, тест не «позеленеет
// сам собой» — он скажет, что предмет исчез, и решение снять проверку будет
// принято явно, а не по недосмотру.
func TestConsoleComposedSetsAreActuallyExpandedInTheTree(t *testing.T) {
	root := repoRoot(t)
	consoleRoot := filepath.Join(root, "ui-future")
	files, err := consoleRegistryFiles(consoleRoot)
	if err != nil {
		t.Fatalf("walk ui-future: %v", err)
	}
	ext, err := consoleTreeExterns(consoleRoot)
	if err != nil {
		t.Fatal(err)
	}

	expanded := 0
	var forms consoleRegistryForms
	var where []string
	for _, file := range files {
		blob, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		rel := mustRel(root, file)
		parsed, err := parseConsoleRegistry(rel, string(blob), ext)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		forms.add(parsed)
		for _, spec := range parsed.Specs {
			for _, f := range spec.Fields {
				// Имя, оставшееся шаблоном, означало бы, что подстановка не
				// произошла и в сверку уехала форма записи вместо ключа.
				if strings.ContainsAny(f.Name, "`$") {
					t.Errorf("%s [%s]: field name %q was never resolved — the expansion produced the source form, not a key",
						rel, spec.ID, f.Name)
				}
			}
			if spec.ExpandedFields > 0 {
				expanded += spec.ExpandedFields
				where = append(where, fmt.Sprintf("%s [%s]: %d", rel, spec.ID, spec.ExpandedFields))
			}
		}
	}
	if expanded == 0 {
		t.Fatal("not one resource in the tree composes its field set any more: this proof has lost its subject — remove it deliberately rather than let it pass on nothing")
	}
	sort.Strings(where)
	t.Logf("перепись: %s; %d field(s) reached the check through expansion of a helper: %v", forms, expanded, where)
}

// ── Набор полей, объявленный в ОБЩЕМ модуле ─────────────────────────────────
//
// Реестров у консоли несколько, и один и тот же ресурс рисуют два из них. Набор
// полей такого ресурса обязан быть объявлен ОДИН раз и в общем модуле: копии
// расходятся молча — так уже разошлись ветви проверки живости, заведённые в
// одном реестре и невыразимые в том, который эту форму пользователю и
// показывает (#375).
//
// Для сканера это было неизвестное имя, а неизвестное имя он честно считает
// отказом читать ВЕСЬ файл. Следствие (#554): гейт читал ОДИН реестр из пяти,
// падал на втором и до трёх оставшихся не доходил вовсе — при том что три из
// четырёх упавших проб существуют ровно затем, чтобы «ноль находок» было
// отличимо от «ноль прочитанного».

// sharedHelperModule — синтетический общий модуль. Форма дословно повторяет ту,
// что несёт дерево: ЭКСПОРТИРОВАННЫЙ помощник, который сам разворачивает
// НЕПУБЛИЧНОГО соседа с подстановкой аргумента, плюс помощник, возвращающий одно
// поле.
const sharedHelperModule = "import type { FormField } from \"./form-schema\";\n\n" +
	"function branchFields(branch: \"http\" | \"https\"): FormField[] {\n" +
	"  const when = { field: \"_hc\", equals: branch };\n" +
	"  return [\n" +
	"    { name: `health_check.${branch}.port`, label: \"порт\", type: \"int\", visibleWhen: when },\n" +
	"    { name: `health_check.${branch}.path`, label: \"путь\", type: \"string\", visibleWhen: when },\n" +
	"  ];\n" +
	"}\n\n" +
	"export function probeFields(): FormField[] {\n" +
	"  return [\n" +
	"    { name: \"_hc\", label: \"чем проверять\", type: \"enum\", options: [] },\n" +
	"    ...branchFields(\"http\"),\n" +
	"    ...branchFields(\"https\"),\n" +
	"  ];\n" +
	"}\n\n" +
	"export function targetsFieldProbe(): FormField {\n" +
	"  return { name: \"targets\", label: \"Цели\", type: \"array\", itemFields: [ { name: \"weight\", label: \"Вес\", type: \"int\" } ] };\n" +
	"}\n"

// sharedHelperSpec — ресурс, чьи поля собраны помощниками ОБЩЕГО модуля: набор
// разворачивается спредом, одиночное поле стоит элементом. Обе формы живут в
// дереве, и обе обязаны читаться.
const sharedHelperSpec = `
  "target-groups": {
    id: "target-groups",
    route: "target-groups",
    apiPath: "/nlb/v1/targetGroups",
    payloadKey: "targetGroups",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    columns: [],
    fields: [
      { name: "name", label: "Имя", type: "string", required: true },
      ...probeFields(),
      targetsFieldProbe(),
      { name: "project_id", label: "Project", type: "string", hidden: true },
    ],
    template: ({ projectId }) => ({ project_id: projectId ?? "", name: "" }),
  },`

// sharedHelperExterns прогоняет синтетический модуль через ТОТ ЖЕ сборщик, что
// исполняется на дереве: дублёр, собранный отдельно, принимал бы не то же самое.
func sharedHelperExterns(modules map[string]string) consoleExterns {
	return consoleExterns{helpers: consoleFieldHelpersFromModules(modules)}
}

// TestConsoleScannerExpandsHelperOfASharedModule — GREEN: помощник общего модуля
// раскрывается, и раскрывается В СВОЕЙ области видимости.
//
// Утверждаются ИМЕНА, а не количество: раскрытие, давшее нужное число полей с
// неразрешёнными именами, прошло бы проверку на количество и не проверило бы ни
// одного настоящего ключа. Отдельно утверждается, что помощник дотянулся до
// НЕПУБЛИЧНОГО соседа своего модуля: в файле-потребителе такого имени нет вовсе,
// и чтение тела в чужой области видимости молча потеряло бы четыре поля из семи.
func TestConsoleScannerExpandsHelperOfASharedModule(t *testing.T) {
	ext := sharedHelperExterns(map[string]string{"shared/src/lib/probe-form.ts": sharedHelperModule})
	parsed, err := parseConsoleRegistry("probe.tsx", probeRegistry(sharedHelperSpec), ext)
	if err != nil {
		t.Fatalf("parse registry using a shared helper: %v", err)
	}
	if len(parsed.Specs) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(parsed.Specs))
	}
	spec := parsed.Specs[0]
	var got []string
	for _, f := range spec.Fields {
		got = append(got, f.Name)
	}
	want := []string{
		"name",
		"_hc", "health_check.http.port", "health_check.http.path", "health_check.https.port", "health_check.https.path",
		"targets",
		"project_id",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d fields, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("field %d: expected %q, got %q (all: %v)", i, want[i], got[i], got)
		}
	}
	// Провенанс: шесть полей пришли из ДРУГОГО файла, и это обязано быть
	// сказано — сверка с сырым текстом реестра иначе объявила бы находкой
	// единый источник, то есть ровно то, ради чего он и заводится.
	if spec.ExternFields != 6 {
		t.Errorf("6 field(s) came from a shared module, the parse reports %d — the raw-text cross-check would then blame the single source", spec.ExternFields)
	}
	// Одно поле-массив пришло помощником, и его под-поля обязаны доехать: иначе
	// «поле есть» означало бы «состав элемента не проверяется».
	for _, f := range spec.Fields {
		if f.Name != "targets" {
			continue
		}
		if len(f.ItemFields) != 1 || f.ItemFields[0].Name != "weight" {
			t.Errorf("the item fields of a helper-returned array field must reach the check, got %+v", f.ItemFields)
		}
	}
}

// TestConsoleScannerChecksInsideASharedHelper — ЧТО ИМЕННО СТАЛО ВИДНО.
//
// Красно-зелёная пара НА ОБЩЕМ МОДУЛЕ: поле, добавленное внутрь помощника,
// проверяется в каждом реестре, который этот помощник разворачивает. Без этого
// «помощник раскрывается» означало бы лишь «разбор не упал»: набор мог бы
// раскрываться и не участвовать в сверке, а гейт оставался бы зелёным ровно
// там, куда его только что расширили.
func TestConsoleScannerChecksInsideASharedHelper(t *testing.T) {
	clean := sharedHelperExterns(map[string]string{"shared/src/lib/probe-form.ts": sharedHelperModule})
	parsed, err := parseConsoleRegistry("probe.tsx", probeRegistry(sharedHelperSpec), clean)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := consoleSpecFindings(parsed.Specs[0]); len(got) != 0 {
		t.Fatalf("GREEN baseline broken: a clean shared helper produced %d finding(s): %v", len(got), got)
	}

	// Инъекция минимальна: поле той же формы, что соседи по набору, с ключом,
	// которого сообщение не несёт. Помощник разворачивается для ДВУХ ветвей —
	// значит и находок по нему две, с разными подставленными именами.
	injected := strings.Replace(sharedHelperModule,
		"    { name: `health_check.${branch}.path`,",
		"    { name: `health_check.${branch}.bogus`, label: \"x\", type: \"string\" },\n"+
			"    { name: `health_check.${branch}.path`,", 1)
	ext := sharedHelperExterns(map[string]string{"shared/src/lib/probe-form.ts": injected})
	parsed, err = parseConsoleRegistry("probe.tsx", probeRegistry(sharedHelperSpec), ext)
	if err != nil {
		t.Fatalf("parse injected: %v", err)
	}
	got := consoleSpecFindings(parsed.Specs[0])
	seen := map[string]bool{}
	for _, f := range got {
		seen[f.Key] = true
	}
	for _, want := range []string{"health_check.http.bogus", "health_check.https.bogus"} {
		if !seen[want] {
			t.Fatalf("a field added inside the shared helper must be reported, expected %q among %v", want, got)
		}
	}
	// Координата ведёт в файл ПОМОЩНИКА, а не в реестр: этих строк в реестре нет
	// вовсе, и адрес находки, указывающий туда, посылает искать не там.
	for _, f := range got {
		if strings.HasSuffix(f.Key, ".bogus") && f.File != "shared/src/lib/probe-form.ts" {
			t.Errorf("finding %q must address the file that declares the field, got %q", f.Key, f.File)
		}
	}
}

// TestConsoleScannerRefusesSharedCompositionItCannotRead — граница модели.
//
// Расширение области видимости до общих модулей — не послабление: за пределами
// узкой формы разбор обязан по-прежнему ЛОМАТЬСЯ. Каждый случай ниже — это
// набор полей, состав которого в исходнике не виден, а значит и объявить его
// проверенным нельзя.
func TestConsoleScannerRefusesSharedCompositionItCannotRead(t *testing.T) {
	for name, tc := range map[string]struct {
		modules map[string]string
		spec    string
	}{
		// Помощник объявлен, но НЕ экспортирован: импортировать его нельзя, и
		// принять такую ссылку значило бы разрешить чтение того, чего в файле
		// не видно.
		"helper is not exported from its module": {
			modules: map[string]string{"shared/src/lib/probe-form.ts": strings.Replace(sharedHelperModule, "export function probeFields", "function probeFields", 1)},
			spec:    sharedHelperSpec,
		},
		// Ни одного общего модуля в области — тот самый дефект #554 дословно.
		"no shared module in scope at all": {
			modules: map[string]string{},
			spec:    sharedHelperSpec,
		},
		// Одно имя в двух общих модулях: какое из двух импортирует реестр,
		// сканер знать не может, и догадка сделала бы его тихо неверным.
		"the same helper name in two shared modules": {
			modules: map[string]string{
				"shared/src/lib/probe-form.ts": sharedHelperModule,
				"shared/src/lib/other-form.ts": strings.Replace(sharedHelperModule, "probe.${branch}", "other.${branch}", -1),
			},
			spec: sharedHelperSpec,
		},
		// Набор РАЗВОРАЧИВАЮТ спредом, одиночное поле СТАВЯТ элементом. Перепутав
		// их, получаешь либо поле-массив вместо набора, либо набор, свёрнутый в
		// одно поле, — и то и другое молча меняет состав тела.
		"a set is called as an entry instead of being spread": {
			modules: map[string]string{"shared/src/lib/probe-form.ts": sharedHelperModule},
			spec:    strings.Replace(sharedHelperSpec, "...probeFields(),", "probeFields(),", 1),
		},
		"a single field is spread instead of being called": {
			modules: map[string]string{"shared/src/lib/probe-form.ts": sharedHelperModule},
			spec:    strings.Replace(sharedHelperSpec, "targetsFieldProbe(),", "...targetsFieldProbe(),", 1),
		},
		// Тело помощника с ветвлением: состав зависит не от аргументов, и в
		// исходнике его не видно.
		"shared helper branches instead of returning a literal": {
			modules: map[string]string{"shared/src/lib/probe-form.ts": "export function probeFields(): FormField[] {\n  if (x) return [];\n  return [{ name: \"y\", label: \"y\", type: \"string\" }];\n}\n" +
				"export function targetsFieldProbe(): FormField {\n  return { name: \"targets\", label: \"Цели\", type: \"array\", itemFields: [] };\n}\n"},
			spec: sharedHelperSpec,
		},
		// Вычисленный аргумент: набор зависит от того, чего в исходнике нет.
		"argument to a shared helper is computed": {
			modules: map[string]string{"shared/src/lib/probe-form.ts": strings.Replace(sharedHelperModule, "export function probeFields(): FormField[] {", "export function probeFields(kind: string): FormField[] {", 1)},
			spec:    strings.Replace(sharedHelperSpec, "...probeFields(),", "...probeFields(pickKind()),", 1),
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseConsoleRegistry("probe.tsx", probeRegistry(tc.spec), sharedHelperExterns(tc.modules))
			if err == nil {
				t.Fatal("the scanner accepted a composition it cannot read: it would then check nothing and say nothing")
			}
			// Отказ обязан НАЗЫВАТЬ место: отказ без координаты равносилен
			// молчанию — искать его предмет негде.
			if !strings.Contains(err.Error(), "probe.tsx:") {
				t.Errorf("the refusal must address a place in the source, got: %v", err)
			}
		})
	}
}

// TestConsoleSharedFieldHelpersAreReadFromTheTree — на РЕАЛЬНОМ дереве, а не
// только на синтетике: общие помощники собираются, реестры их разворачивают, и
// БЕЗ них дерево читается не полностью.
//
// Вторая половина — воспроизведение дефекта #554 на живом дереве: изъяв из
// области видимости общие модули, обязаны получить отказ, и отказ обязан
// назвать помощника. Иначе «сборщик что-то собрал» неотличимо от «сборщик не
// нужен», и при следующем переносе набора в общий модуль всё повторится.
//
// Проба ИСТЕКАЕТ САМА: если однажды ни один реестр не станет брать набор полей
// из общего модуля, она скажет, что предмет исчез, и решение снять её будет
// принято явно, а не по недосмотру.
func TestConsoleSharedFieldHelpersAreReadFromTheTree(t *testing.T) {
	root := repoRoot(t)
	consoleRoot := filepath.Join(root, "ui-future")
	files, err := consoleRegistryFiles(consoleRoot)
	if err != nil {
		t.Fatalf("walk ui-future: %v", err)
	}
	ext, err := consoleTreeExterns(consoleRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Та же область МИНУС общие помощники — состояние сканера до #554.
	without := consoleExterns{strings: ext.strings, values: ext.values}

	externFields, consumers := 0, 0
	var forms consoleRegistryForms
	var blindWithout []string
	for _, file := range files {
		rel := mustRel(root, file)
		blob, rerr := os.ReadFile(file)
		if rerr != nil {
			t.Fatalf("%s: %v", rel, rerr)
		}
		parsed, perr := parseConsoleRegistry(rel, string(blob), ext)
		if perr != nil {
			t.Fatalf("%s: %v", rel, perr)
		}
		forms.add(parsed)
		uses := 0
		for _, spec := range parsed.Specs {
			uses += spec.ExternHelperFields
		}
		if uses > 0 {
			consumers++
			externFields += uses
		}
		if _, berr := parseConsoleRegistry(rel, string(blob), without); berr != nil {
			blindWithout = append(blindWithout, rel)
		}
	}

	if externFields == 0 {
		t.Fatal("not one registry in the tree takes its field set from a shared module any more: this proof has lost its subject — remove it deliberately rather than let it pass on nothing")
	}
	if len(blindWithout) == 0 {
		t.Error("dropping the shared-module helpers from scope changed nothing: either they are not load-bearing, or the scanner stopped reading them — both mean this gate no longer proves what it claims")
	}
	sort.Strings(blindWithout)
	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	t.Logf("перепись: %s, помощников из общих модулей собрано %d, реестров-потребителей %d, полей пришло оттуда %d; без этой области нечитаемы %v",
		forms, len(ext.helpers), consumers, externFields, blindWithout)
}

// TestSanitizeReshapeIsNotRemoval — снятие ключа с последующим присваиванием
// ТОГО ЖЕ ключа не выводит его поддерево из-под проверки.
//
// Разница не косметическая. Ключ, снятый и положенный обратно, уходит на провод
// — просто в другой форме; ключ, снятый и не возвращённый, не уходит. Считать
// первое удалением значит молча потерять охват вместе со всем поддеревом, и
// потерять именно там, где форма описывает вложенную структуру. Ровно так
// шесть полей источника VIP выпали из сверки, пока гейт рапортовал, что
// прочитал их.
func TestSanitizeReshapeIsNotRemoval(t *testing.T) {
	const reshape = `sanitize: (obj) => {
      const out: Record<string, unknown> = { ...obj };
      const built = buildIt(out);
      delete out.v4_source;
      delete out.form_only_leftover;
      if (built) out.v4_source = built;
      return out;
    }`
	eff := analyzeSanitize(reshape)

	if eff.Removed["v4_source"] {
		t.Error("a key that is deleted and then re-assigned is reshaped, not removed: dropping it takes its whole subtree out of the check")
	}
	if !eff.Added["v4_source"] {
		t.Error("the re-assignment must be seen, otherwise the key would not be checked at all")
	}
	if !eff.Removed["form_only_leftover"] {
		t.Error("a key that is deleted and never re-assigned really is removed: keeping it would report a body the console does not send")
	}

	// И то же на уровне собранного тела: поддерево обязано дойти до сверки.
	spec := consoleSpec{
		ID: "probe", MutationBasePath: "/nlb/v1/networkLoadBalancers", CanCreate: true,
		Fields: []consoleField{
			{Name: "v4_source.subnet_id"},
			{Name: "form_only_leftover"},
		},
		SanitizeSource: reshape,
	}
	body := consoleCreateBody(spec, analyzeSanitize(reshape))
	sub, ok := body["v4_source"].(map[string]any)
	if !ok {
		t.Fatalf("the reshaped key must survive as the shape the registry declares, got %#v", body["v4_source"])
	}
	if _, ok := sub["subnet_id"]; !ok {
		t.Errorf("the subtree under a reshaped key must reach the check, got %#v", sub)
	}
	if _, ok := body["form_only_leftover"]; ok {
		t.Error("a genuinely removed key must not reach the check")
	}
}

// TestConsoleSharedRefsResolve — ссылка на общее объявление обязана КУДА-ТО вести.
//
// ПРЕДМЕТ. Раздел, который монтируют два приложения, объявляет спеку один раз, а
// второй реестр ссылается на неё (`SHARED_REGISTRY["<id>"]`). Сканер такую запись
// пропускает без разбора — спеку проверяют там, где она объявлена. Без этой пробы
// пропуск был бы безусловным: ссылка на несуществующий идентификатор прошла бы
// молча, и раздел остался бы непроверенным ВООБЩЕ — ни здесь, ни там.
//
// Доказано инъекцией: подмена идентификатора на отсутствующий в общем реестре
// роняет пробу с координатой; на исправном дереве она молчит и печатает перепись.
func TestConsoleSharedRefsResolve(t *testing.T) {
	root := repoRoot(t)
	consoleRoot := filepath.Join(root, "ui-future")
	files, err := consoleRegistryFiles(consoleRoot)
	if err != nil {
		t.Fatalf("walk ui-future: %v", err)
	}
	ext, err := consoleTreeExterns(consoleRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Что объявляет ОБЩИЙ реестр — цель всех ссылок.
	sharedIDs := make(map[string]bool)
	refs := 0
	var forms consoleRegistryForms
	type ref struct{ file, id string }
	var found []ref

	for _, file := range files {
		rel := mustRel(root, file)
		blob, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		parsed, err := parseConsoleRegistry(rel, string(blob), ext)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		forms.add(parsed)
		shared := strings.Contains(rel, "/shared/")
		for _, spec := range parsed.Specs {
			if shared {
				sharedIDs[spec.ID] = true
			}
			if spec.SharedRef != "" {
				refs++
				found = append(found, ref{rel, spec.SharedRef})
			}
		}
	}

	for _, r := range found {
		if !sharedIDs[r.id] {
			t.Errorf("%s: ссылка на %q, которого в общем реестре нет — раздел остался бы непроверенным ни здесь, ни там", r.file, r.id)
		}
	}
	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	t.Logf("перепись: %s, записей общего реестра %d, ссылок на них %d", forms, len(sharedIDs), refs)
}
