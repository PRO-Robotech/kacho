// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// newman_body_contract_test.go — гейт: ни один REST-запрос регрессионных
// suite'ов не шлёт краю ключ, которого нет в message запроса.
//
// Зачем гейт, а не разовый обход. Ключ, которого сервер не читает, ничем не
// выдаёт себя на прогоне: край отвечает `200`, кейс зеленеет, а поле не
// применено. Так уже случалось — редизайн переименовал поле запроса, устаревшие
// фикстуры продолжали слать старый ключ и продолжали получать `200`, ресурс
// создавался без данных, а разбираться пришлось в соседнем сервисе, куда
// приехал каскад. Единственный способ увидеть этот класс — сверять
// отправляемое с контрактом статически.
//
// Вердикт ставится по СОДЕРЖИМОМУ: тест падает счётчиком находок и печатает их
// все. Гейт, который печатает и выходит с нулём, сам был бы экземпляром того
// класса, который здесь искореняется.

// varPlaceholder — подстановка `{{runId}}`-переменных Postman. Значения на
// контракт не влияют: гейт сверяет ИМЕНА ключей, поэтому любая подстановка,
// оставляющая JSON разбираемым, годится.
var varPlaceholder = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// newmanFinding — находка с координатами в файле коллекции.
type newmanFinding struct {
	bodyFinding
	File string
	Line int
	Case string
}

func (f newmanFinding) String() string {
	return fmt.Sprintf("%s:%d [%s] %s", f.File, f.Line, f.Case, f.bodyFinding.String())
}

// newmanRequest — один запрос коллекции, извлечённый для разбора.
type newmanRequest struct {
	name   string
	method string
	path   string
	// bodyRaw — тело как оно лежит в коллекции (с `{{...}}`), пусто если тела нет.
	bodyRaw string
	line    int
}

func TestNewmanCollectionsSendNoUnknownRequestFields(t *testing.T) {
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "services", "*", "tests", "newman", "collections", "*.json"))
	if err != nil {
		t.Fatalf("glob collections: %v", err)
	}
	if len(files) == 0 {
		// Пустой набор входов — это НЕ «зелено»: гейт, которому нечего
		// проверять, ничего и не доказывает.
		t.Fatal("no newman collections found: gate has nothing to check, which is a failure, not a pass")
	}

	var findings []newmanFinding
	requests := 0
	bodies := 0

	for _, file := range files {
		reqs, err := parseNewmanCollection(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		rel := mustRel(root, file)
		for _, r := range reqs {
			requests++
			if r.bodyRaw == "" {
				continue
			}
			bodies++
			obj, err := decodeTemplatedJSONObject(r.bodyRaw)
			if err != nil {
				findings = append(findings, newmanFinding{
					bodyFinding: bodyFinding{
						Kind: "body-unparsed", Method: r.method, Path: r.path,
						Key: err.Error(),
					},
					File: rel, Line: r.line, Case: r.name,
				})
				continue
			}
			if obj == nil {
				// Тело — не JSON-объект (массив/скаляр). Именованных ключей нет.
				continue
			}
			for _, f := range analyzeRequestBody(r.method, r.path, obj) {
				findings = append(findings, newmanFinding{bodyFinding: f, File: rel, Line: r.line, Case: r.name})
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	// Разделение вердикта по предмету.
	//
	// mis-addressed — тело адресовано RPC, который его не читает. Это ТОТ САМЫЙ
	// класс: край отвечает `200`, поле не применено. Вердикт гейта — по нему,
	// и он обязан сходиться к нулю.
	//
	// unattributed — (метод, путь) не обслуживается краем ВООБЩЕ: grpc-gateway
	// отвечает 404/405, до разбора тела дело не доходит, контракта запроса не
	// существует. Такие запросы — намеренные пробы «этого маршрута быть не
	// должно» (`*-METHOD-PUT-NOT-ALLOWED`, `*-NOT-PUBLIC`, `*-EXTERNAL-ABSENT`),
	// и требовать от них чистого тела бессмысленно. Они печатаются всегда:
	// среди них попадаются и фикстуры, нацеленные в несуществующий маршрут по
	// ошибке. Гарантию, что сюда не проваливается РЕАЛЬНЫЙ маршрут, даёт
	// отдельная проверка TestHTTPBindingsMatchGeneratedRouteTable.
	var misAddressed, unattributed []newmanFinding
	for _, f := range findings {
		if f.Kind == routeUnresolved {
			unattributed = append(unattributed, f)
			continue
		}
		misAddressed = append(misAddressed, f)
	}

	t.Logf("scanned %d collections, %d requests, %d with a JSON body; %d mis-addressed, %d unattributed",
		len(files), requests, bodies, len(misAddressed), len(unattributed))

	if len(unattributed) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%d request(s) target a (method, path) the edge does not serve — no request contract to check:\n", len(unattributed))
		for _, f := range unattributed {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Log(b.String())
	}

	if len(misAddressed) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d request(s) send the edge a body key it does not read:\n", len(misAddressed))
	for _, f := range misAddressed {
		fmt.Fprintf(&b, "  %s\n", f)
	}
	t.Fatal(b.String())
}

// decodeTemplatedJSONObject разбирает тело Postman-запроса, подставив вместо
// `{{var}}` нейтральный литерал. Возвращает (nil, nil), если тело — валидный
// JSON, но не объект.
func decodeTemplatedJSONObject(raw string) (map[string]any, error) {
	substituted := varPlaceholder.ReplaceAllString(raw, "X")
	var v any
	if err := json.Unmarshal([]byte(substituted), &v); err != nil {
		return nil, fmt.Errorf("body is not parseable JSON after {{var}} substitution: %w", err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, nil
	}
	return obj, nil
}

// parseNewmanCollection извлекает все запросы коллекции вместе с номером строки
// их тела (для адресуемости находки).
func parseNewmanCollection(path string) ([]newmanRequest, error) {
	content, err := os.ReadFile(path) //nolint:gosec // путь собирается гейтом из фиксированного glob по дереву репозитория
	if err != nil {
		return nil, err
	}
	var doc struct {
		Item []json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("parse collection: %w", err)
	}
	lines := lineIndex(string(content))
	var out []newmanRequest
	var walk func(items []json.RawMessage, folder string) error
	walk = func(items []json.RawMessage, folder string) error {
		for _, raw := range items {
			var node struct {
				Name    string            `json:"name"`
				Item    []json.RawMessage `json:"item"`
				Request *struct {
					Method string          `json:"method"`
					URL    json.RawMessage `json:"url"`
					Body   *struct {
						Mode string `json:"mode"`
						Raw  string `json:"raw"`
					} `json:"body"`
				} `json:"request"`
			}
			if err := json.Unmarshal(raw, &node); err != nil {
				return err
			}
			if len(node.Item) > 0 {
				// Кейс newman — папка верхнего уровня; её имя и есть адрес
				// находки для того, кто пойдёт чинить.
				next := folder
				if next == "" {
					next = node.Name
				}
				if err := walk(node.Item, next); err != nil {
					return err
				}
				continue
			}
			if node.Request == nil {
				continue
			}
			urlRaw, err := requestURL(node.Request.URL)
			if err != nil {
				return fmt.Errorf("request %q: %w", node.Name, err)
			}
			name := node.Name
			if folder != "" {
				name = folder + " / " + node.Name
			}
			req := newmanRequest{
				name:   name,
				method: strings.ToUpper(node.Request.Method),
				path:   restPath(urlRaw),
			}
			if node.Request.Body != nil && node.Request.Body.Mode == "raw" {
				req.bodyRaw = node.Request.Body.Raw
				req.line = lines.lineOf(node.Request.Body.Raw)
			}
			out = append(out, req)
		}
		return nil
	}
	if err := walk(doc.Item, ""); err != nil {
		return nil, err
	}
	return out, nil
}

// requestURL достаёт строку URL: Postman хранит её либо объектом с полем `raw`,
// либо просто строкой.
func requestURL(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("request has no url")
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}
	var asObject struct {
		Raw string `json:"raw"`
	}
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return "", fmt.Errorf("url is neither string nor object: %w", err)
	}
	if asObject.Raw == "" {
		return "", fmt.Errorf("url object has empty raw")
	}
	return asObject.Raw, nil
}

// restPath отрезает базу (`{{baseUrl}}` / `{{internalBaseUrl}}` / `http://host`)
// и query-строку, оставляя REST-путь.
func restPath(url string) string {
	if i := strings.Index(url, "://"); i >= 0 {
		if j := strings.IndexByte(url[i+3:], '/'); j >= 0 {
			url = url[i+3+j:]
		} else {
			url = "/"
		}
	} else if strings.HasPrefix(url, "{{") {
		if j := strings.Index(url, "}}"); j >= 0 {
			url = url[j+2:]
		}
	}
	if i := strings.IndexByte(url, '?'); i >= 0 {
		url = url[:i]
	}
	if i := strings.IndexByte(url, '#'); i >= 0 {
		url = url[:i]
	}
	if !strings.HasPrefix(url, "/") {
		url = "/" + url
	}
	return url
}

// lineIdx отображает JSON-строковый литерал обратно в номер строки файла.
type lineIdx struct {
	content string
	// starts[i] — байтовое смещение начала строки i+1.
	starts []int
}

func lineIndex(content string) lineIdx {
	starts := []int{0}
	for i, r := range content {
		if r == '\n' {
			starts = append(starts, i+1)
		}
	}
	return lineIdx{content: content, starts: starts}
}

// lineOf возвращает номер строки, на которой лежит JSON-энкодинг value, либо 0.
func (l lineIdx) lineOf(value string) int {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	idx := strings.Index(l.content, string(encoded))
	if idx < 0 {
		return 0
	}
	return sort.SearchInts(l.starts, idx+1)
}

// repoRoot поднимается от пакета до корня репозитория (там лежит go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root (go.mod) not found above the package directory")
		}
		dir = parent
	}
}

func mustRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
