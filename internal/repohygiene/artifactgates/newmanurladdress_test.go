// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// newmanurladdress — адрес, который newman ОТПРАВЛЯЕТ, совпадает с адресом,
// который кейс НАПИСАЛ (kacho#3230).
//
// Элемент коллекции несёт адрес объектом: `raw` и рядом разобранные `host`,
// `path`, `query`. Прогонщик собирает запрос из РАЗОБРАННЫХ полей, а `raw` при
// объекте не читает (postman-collection, `Url` от объекта; проверено на версии
// прогонщика машины: `{raw: ".../csrf?form=step-up", path: [...]}` даёт
// `.../csrf`). Генератор, положивший строку запроса только в `raw`, отправляет
// запрос без неё — и отказ службы («form must be one of …») читается как отказ
// по написанной форме, которой в запросе не было.
//
// Предикат — адрес, собранный из разобранных полей так же, как его собирает
// прогонщик, равен `raw` побайтово: и строка запроса, и путь. Обход — все
// отслеживаемые коллекции всех наборов; единица счёта — запрос (элемент с
// `request`).
//
// Путь судится тем же предикатом (kacho#3257): общий разбор сегментов
// отбрасывал пустой последний сегмент, и хвостовая косая черта `raw`
// (`/vpc/v1/networks/`, `/v2/`) уходила адресом без неё — кейс о хвостовой
// косой проверял другой адрес. Прогонщик собирает путь как `/` + сегменты
// через `/` (postman-collection `Url.getPath`), поэтому пустой сегмент —
// законная форма хвостовой и сдвоенной косой.

// urlAddressCensus — объём осмотренного: «ноль находок» отличим от «ноль
// прочитанного».
type urlAddressCensus struct {
	collections int
	requests    int
	withQuery   int // запросов, чей `raw` несёт строку запроса
	withSlash   int // запросов, чей путь в `raw` кончается косой чертой
}

// postmanURL — адрес элемента коллекции в объектной форме.
type postmanURL struct {
	Raw   string   `json:"raw"`
	Host  []string `json:"host"`
	Path  []string `json:"path"`
	Query []struct {
		Key   string  `json:"key"`
		Value *string `json:"value"`
	} `json:"query"`
}

// sentAddress — адрес, который прогонщик соберёт из разобранных полей.
func (u postmanURL) sentAddress() string {
	var b strings.Builder
	b.WriteString(strings.Join(u.Host, "."))
	if len(u.Path) > 0 {
		b.WriteString("/" + strings.Join(u.Path, "/"))
	}
	if len(u.Query) > 0 {
		pairs := make([]string, 0, len(u.Query))
		for _, q := range u.Query {
			if q.Value == nil {
				pairs = append(pairs, q.Key)
				continue
			}
			pairs = append(pairs, q.Key+"="+*q.Value)
		}
		b.WriteString("?" + strings.Join(pairs, "&"))
	}
	return b.String()
}

// pathOf — адрес без строки запроса (до первого `?`).
func pathOf(addr string) string {
	if i := strings.Index(addr, "?"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// urlAddressFindings — запросы коллекции, чей отправляемый адрес расходится с
// написанным. rel — путь коллекции для координаты находки.
func urlAddressFindings(rel string, body []byte, census *urlAddressCensus) ([]string, error) {
	var col struct {
		Item []json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(body, &col); err != nil {
		return nil, fmt.Errorf("%s: коллекция не разобрана: %w", rel, err)
	}
	var out []string
	var walk func(items []json.RawMessage, trail string) error
	walk = func(items []json.RawMessage, trail string) error {
		for _, raw := range items {
			var it struct {
				Name    string            `json:"name"`
				Item    []json.RawMessage `json:"item"`
				Request *struct {
					URL json.RawMessage `json:"url"`
				} `json:"request"`
			}
			if err := json.Unmarshal(raw, &it); err != nil {
				return fmt.Errorf("%s: элемент %q не разобран: %w", rel, trail, err)
			}
			where := trail + "/" + it.Name
			if it.Request == nil {
				if err := walk(it.Item, where); err != nil {
					return err
				}
				continue
			}
			census.requests++
			// Адрес строкой прогонщик разбирает сам — расходиться нечему.
			var asString string
			if json.Unmarshal(it.Request.URL, &asString) == nil {
				if strings.Contains(asString, "?") {
					census.withQuery++
				}
				if strings.HasSuffix(pathOf(asString), "/") {
					census.withSlash++
				}
				continue
			}
			var u postmanURL
			if err := json.Unmarshal(it.Request.URL, &u); err != nil {
				return fmt.Errorf("%s: адрес элемента %q не разобран: %w", rel, where, err)
			}
			if strings.Contains(u.Raw, "?") {
				census.withQuery++
			}
			if strings.HasSuffix(pathOf(u.Raw), "/") {
				census.withSlash++
			}
			if sent := u.sentAddress(); sent != u.Raw {
				out = append(out, fmt.Sprintf("%s %s: написан %q, отправится %q", rel, where, u.Raw, sent))
			}
		}
		return nil
	}
	if err := walk(col.Item, ""); err != nil {
		return nil, err
	}
	census.collections++
	return out, nil
}

// TestNewmanSentAddressIsTheWrittenOne — гейт по дереву: каждый запрос каждой
// отслеживаемой коллекции отправляется по адресу, который написан в `raw`.
func TestNewmanSentAddressIsTheWrittenOne(t *testing.T) {
	t.Parallel()
	tt := newTrackedTree(t, repoRoot(t))
	var rels []string
	for rel := range tt.files {
		if strings.HasSuffix(rel, ".postman_collection.json") {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	var census urlAddressCensus
	var findings []string
	for _, rel := range rels {
		body, err := os.ReadFile(filepath.Join(tt.root, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		got, err := urlAddressFindings(rel, body, &census)
		if err != nil {
			t.Fatal(err)
		}
		findings = append(findings, got...)
	}
	t.Logf("перепись: коллекций %d · запросов %d · из них со строкой запроса %d · с хвостовой косой %d · "+
		"находок %d", census.collections, census.requests, census.withQuery, census.withSlash, len(findings))
	// Предпосылка обхода: пустой обход — не вердикт.
	if census.collections == 0 || census.requests == 0 {
		t.Fatalf("обход не нашёл ни одной коллекции с запросами (коллекций %d, запросов %d) — "+
			"судить нечего, это не зелёный", census.collections, census.requests)
	}
	for _, f := range findings {
		t.Errorf("адрес запроса уйдёт не тем, что написан (строку запроса генератор обязан разложить в "+
			"`url.query`, путь — в сегменты без потери пустых): %s", f)
	}
}
