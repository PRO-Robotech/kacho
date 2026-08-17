// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// unknown_body_fields_proof_test.go — доказательство, что гейт реагирует на
// СОДЕРЖИМОЕ, а не на факт выполнения.
//
// Гейт, который обходит дерево и всегда выходит с нулём, — сам экземпляр того
// класса, который он ищет. Поэтому здесь: (1) чистое тело даёт ноль находок;
// (2) подстановка заведомо неизвестного ключа в ту же фикстуру даёт ровно одну
// находку с этим ключом; (3) таблица биндингов сверяется с НЕЗАВИСИМО
// построенной таблицей маршрутов authz-middleware, иначе «ноль находок» мог бы
// означать «сканер не нашёл маршрут и промолчал».

// TestUnknownBodyKeysDetectsInjectedKey — красно-зелёная пара на синтетической
// коллекции: тот же запрос без лишнего ключа и с ним.
func TestUnknownBodyKeysDetectsInjectedKey(t *testing.T) {
	const (
		method = "POST"
		path   = "/vpc/v1/networks"
	)
	clean := map[string]any{
		"projectId":   "prj-x",
		"name":        "net-x",
		"description": "d",
		"labels":      map[string]any{"suite": "newman"},
	}
	if got := analyzeRequestBody(method, path, clean); len(got) != 0 {
		t.Fatalf("GREEN baseline broken: valid body produced %d finding(s): %v", len(got), got)
	}

	injected := map[string]any{}
	for k, v := range clean {
		injected[k] = v
	}
	injected["descriptionn"] = "typo"

	got := analyzeRequestBody(method, path, injected)
	if len(got) != 1 {
		t.Fatalf("injected key must produce exactly 1 finding, got %d: %v", len(got), got)
	}
	if got[0].Kind != keyUnknown || got[0].Key != "descriptionn" {
		t.Fatalf("finding must name the injected key, got %+v", got[0])
	}
	if got[0].FQN != "kacho.cloud.vpc.v1.NetworkService/Create" {
		t.Fatalf("finding must attribute the request to the RPC that serves it, got %q", got[0].FQN)
	}
}

// TestUnknownBodyKeysAcceptsBothWireNamings — protojson принимает и json_name
// (camelCase), и оригинальное proto-имя; обе формы обязаны считаться известными,
// иначе гейт завалил бы корректные тела.
func TestUnknownBodyKeysAcceptsBothWireNamings(t *testing.T) {
	for _, body := range []map[string]any{
		{"projectId": "p", "name": "n"},
		{"project_id": "p", "name": "n"},
	} {
		if got := analyzeRequestBody("POST", "/vpc/v1/networks", body); len(got) != 0 {
			t.Fatalf("body %v must be clean, got %v", body, got)
		}
	}
}

// TestUnknownBodyKeysDescendsIntoNestedMessages — неизвестный ключ на глубине
// обязан находиться и адресоваться путём, иначе «чисто» означало бы лишь
// «верхний уровень чист».
func TestUnknownBodyKeysDescendsIntoNestedMessages(t *testing.T) {
	// Target — вложенное сообщение в списке `targets` у nlb AddTargets.
	body := map[string]any{
		"targetGroupId": "tg-x",
		"targets": []any{
			map[string]any{"instanceId": "ins-x"},
			map[string]any{"instanceId": "ins-y", "bogusInner": 1},
		},
	}
	got := analyzeRequestBody("POST", "/nlb/v1/targetGroups/tg-x:addTargets", body)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 nested finding, got %d: %v", len(got), got)
	}
	if got[0].Key != "targets[1].bogusInner" {
		t.Fatalf("nested finding must carry its path, got %q", got[0].Key)
	}
}

// TestUnknownBodyKeysTreatsMapKeysAsData — ключи map — произвольные строки, а не
// имена полей. Спуск в них дал бы ложную находку на каждой метке.
func TestUnknownBodyKeysTreatsMapKeysAsData(t *testing.T) {
	body := map[string]any{
		"projectId": "p",
		"labels":    map[string]any{"anything-at-all": "v", "second": "w"},
	}
	if got := analyzeRequestBody("POST", "/vpc/v1/networks", body); len(got) != 0 {
		t.Fatalf("map keys must not be read as field names, got %v", got)
	}
}

// TestNewmanScannerReadsCollectionsFromDisk — сквозная красно-зелёная пара на
// файле: сканер обязан прочитать коллекцию, сопоставить путь и увидеть ключ.
func TestNewmanScannerReadsCollectionsFromDisk(t *testing.T) {
	scan := func(t *testing.T, bodyRaw string) []bodyFinding {
		t.Helper()
		dir := t.TempDir()
		file := filepath.Join(dir, "probe.postman_collection.json")
		doc := map[string]any{
			"info": map[string]any{"name": "probe"},
			"item": []any{map[string]any{
				"name": "PROBE-CASE",
				"item": []any{map[string]any{
					"name": "create",
					"request": map[string]any{
						"method": "POST",
						"url":    map[string]any{"raw": "{{baseUrl}}/vpc/v1/networks"},
						"body":   map[string]any{"mode": "raw", "raw": bodyRaw},
					},
				}},
			}},
		}
		blob, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		if err := os.WriteFile(file, blob, 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		reqs, err := parseNewmanCollection(file)
		if err != nil {
			t.Fatalf("parse fixture: %v", err)
		}
		if len(reqs) != 1 {
			t.Fatalf("fixture must yield exactly 1 request, got %d", len(reqs))
		}
		r := reqs[0]
		if r.name != "PROBE-CASE / create" {
			t.Fatalf("request must be attributed to its case folder, got %q", r.name)
		}
		if r.path != "/vpc/v1/networks" {
			t.Fatalf("base must be stripped from the url, got %q", r.path)
		}
		if r.line == 0 {
			t.Fatal("request body must be addressable by line number")
		}
		obj, err := decodeTemplatedJSONObject(r.bodyRaw)
		if err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return analyzeRequestBody(r.method, r.path, obj)
	}

	if got := scan(t, `{"projectId": "{{projectId}}", "name": "net-{{runId}}"}`); len(got) != 0 {
		t.Fatalf("GREEN: clean fixture must produce no findings, got %v", got)
	}
	got := scan(t, `{"projectId": "{{projectId}}", "name": "net-{{runId}}", "injectedUnknown": "x"}`)
	if len(got) != 1 || got[0].Key != "injectedUnknown" {
		t.Fatalf("RED: injected key must be reported exactly once, got %v", got)
	}
}

// TestHTTPBindingsMatchGeneratedRouteTable — таблица биндингов, собранная
// сканером из дескрипторов в рантайме, обязана совпадать с таблицей маршрутов
// authz-middleware, которую генерирует protoc-плагин на сборке.
//
// Это защита от тихого недосчёта: если бы сканер терял маршруты, все тела на
// них молча выпали бы из проверки и гейт зеленел бы, ничего не проверив. Две
// таблицы строятся РАЗНЫМИ механизмами из одного источника — расхождение
// означает дефект одной из них.
func TestHTTPBindingsMatchGeneratedRouteTable(t *testing.T) {
	router := middleware.NewRestRouter()
	bindings := loadedHTTPBindings()
	if len(bindings) == 0 {
		t.Fatal("binding table is empty: proto descriptors are not linked into the test binary")
	}
	// Пара (метод, путь) отображается в МНОЖЕСТВО FQN, а не в один.
	//
	// Прежде здесь стояло сравнение с единственным значением, и предпосылка была
	// верна ровно до тех пор, пока один путь объявлял один сервис. Окно расширения
	// ADM-1 (S1→S3) её сняло: административная поверхность публикуется на ТОМ ЖЕ
	// каноническом пути, что несёт внутренняя, и одну пару объявляют два FQN.
	//
	// Сравнение единственных значений на этом не просто краснело — оно краснело
	// НЕДЕТЕРМИНИРОВАННО, в обе стороны: обе таблицы выбирали своего кандидата
	// сами, и какой из двух выпадет, решал порядок обхода. То есть проба измеряла
	// собственный недетерминизм, а не расхождение таблиц.
	//
	// Предмет пробы от этого не изменился: сканер не должен ТЕРЯТЬ маршруты, иначе
	// тела на них молча выпадут из проверки. Поэтому утверждается принадлежность
	// множеству: FQN, который сканер видит на этой паре, обязан быть среди тех,
	// что таблица на ней объявляет.
	tableByPair := map[string]map[string]bool{}
	for _, r := range middleware.RestRoutesForProof() {
		key := r.Method + " " + r.Template
		if tableByPair[key] == nil {
			tableByPair[key] = map[string]bool{}
		}
		tableByPair[key][r.FQN] = true
	}

	known := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		known[b.fqn] = true
		concrete := concretePath(b.template)
		if _, ok := router.Resolve(b.method, concrete); !ok {
			t.Errorf("%s %s (%s): the generated route table does not resolve it", b.method, concrete, b.fqn)
			continue
		}
		if fqns := tableByPair[b.method+" "+b.template]; !fqns[b.fqn] {
			t.Errorf("%s %s: сканер видит %s, а таблица маршрутов на этой паре объявляет %v — "+
				"маршрут потерян одной из таблиц, и тела на нём выпадут из проверки",
				b.method, concrete, b.fqn, keysOf(fqns))
		}
	}

	// Обратное направление — то, из-за которого «ноль находок» может лгать.
	// RPC, известный каталогу маршрутов, но невидимый сканеру, означал бы, что
	// тела на его пути классифицируются как «маршрут не найден» и не
	// проверяются вовсе.
	//
	// Законная причина невидимости ровно одна: сервис вообще не слинкован в
	// бинарь края — тогда край и сам его не обслуживает (grpc-gateway отдаст
	// 404), проверять там нечего. Любая другая — потеря маршрута сканером.
	// Предикат, а не список имён: слинкуют сервис — проверка сработает.
	for fqn := range router.PathTemplates() {
		if known[fqn] {
			continue
		}
		service := fqn
		if i := strings.IndexByte(fqn, '/'); i >= 0 {
			service = fqn[:i]
		}
		if _, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(service)); err == nil {
			t.Errorf("%s: linked into the edge binary and present in the route table, yet invisible to the scanner — bodies on its path would go unchecked", fqn)
		}
	}
}

// concretePath подставляет в шаблон конкретные сегменты вместо `{field}`.
// Значение выбрано так, чтобы не совпасть ни с одним литеральным сегментом
// реальных путей (иначе подстановка сама создала бы неоднозначность).
func concretePath(template string) string {
	parts := strings.Split(strings.TrimPrefix(template, "/"), "/")
	for i, p := range parts {
		open := strings.IndexByte(p, '{')
		closeIdx := strings.LastIndexByte(p, '}')
		if open < 0 || closeIdx < open {
			continue
		}
		value := "Xid"
		if strings.Contains(p[open:closeIdx], "**") {
			// `{x=**}` поглощает один или больше сегментов — подставляем два,
			// чтобы проверить именно многосегментное поведение.
			value = "Xid/Xsub"
		}
		parts[i] = p[:open] + value + p[closeIdx+1:]
	}
	return "/" + strings.Join(parts, "/")
}

// keysOf — имена FQN пары в устойчивом порядке, чтобы текст отказа не плавал
// между прогонами и его можно было сравнить глазами.
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
