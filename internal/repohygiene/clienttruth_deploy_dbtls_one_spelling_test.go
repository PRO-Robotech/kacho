// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_deploy_dbtls_one_spelling_test.go — ключ, включающий TLS до базы,
// пишется ОДНИМ способом.
//
// Ключ звался в дереве двумя написаниями: `sslMode` у vpc/compute/iam/storage и
// `sslmode` у geo/nlb/registry. Обе половины лежали в ОДНОМ профиле, поэтому
// вопрос «весь ли флот шифрован», заданный одним `grep sslMode values.prod.yaml`,
// молча отвечал про половину флота и выглядел исчерпывающим.
//
// Это тот вид ошибки, который не выдаёт себя ничем: оба написания означают одно,
// сегодня обе половины стоят в `require`, и ответ СЛУЧАЙНО верен. Он перестанет
// быть верным ровно тогда, когда кто-нибудь переведёт один сервис на открытый
// текст в написании, которого проверяющий не спросил.
//
// Гейт судит ОБЪЯВЛЕНИЯ — файлы значений чартов и профили, — а не рендер: ключ
// объявляется здесь, оператор грепает здесь, и проверке не нужны ни зависимости
// чарта, ни helm, поэтому она не может пропуститься.
//
// Канон — `sslMode` (camelCase, как прочие ключи продукта). Это ПЕРЕИМЕНОВАНИЕ
// ПОД ОХРАНОЙ, а не совместимость: канон объявлен умолчанием чарта, поэтому
// профиль, задающий прежнее написание, даёт расхождение и отказ рендера с обоими
// значениями в тексте. Механизм гарантирует не приём старого имени, а отсутствие
// МОЛЧАЛИВОГО отката к умолчанию — для ключа, чьё умолчание у половины чартов
// `disable`, такой откат означал бы открытый текст до базы.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// canonicalDBTLSKey — единственное законное написание.
const canonicalDBTLSKey = "sslMode"

// legacyDBTLSKey — написание, выведенное из объявлений. В шаблонах оно остаётся
// принимаемым переходно; в объявлениях его быть не должно.
const legacyDBTLSKey = "sslmode"

// valuesFiles — каждый файл значений в дереве: умолчания чартов и профили.
// Перечень выводится обходом: профиль, заведённый завтра, попадает под гейт сам.
func valuesFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	// Умолчания чартов: services/*/deploy, gateway/deploy, зонтик и его сабчарты.
	chartRoots := []string{
		filepath.Join(root, "services"),
		filepath.Join(root, "deploy", "helm", "umbrella", "charts"),
	}
	for _, dir := range chartRoots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			// values.yaml И профили рядом с ним: у чарта бывает свой overlay
			// (`values.dev.yaml`). Первая редакция брала только `values.yaml`,
			// и прежний адрес, доживший в таком overlay, был для гейта невидим —
			// нашёл его СОСЕДНИЙ гейт, требующий читателя у ключа профиля.
			for _, base := range []string{
				filepath.Join(dir, e.Name(), "deploy"),
				filepath.Join(dir, e.Name()),
			} {
				for _, cand := range valuesInDir(base) {
					add(cand)
				}
			}
		}
	}
	for _, p := range valuesInDir(filepath.Join(root, "gateway", "deploy")) {
		add(p)
	}
	// Профили зонтика — все values*.yaml.
	umbrella := filepath.Join(root, "deploy", "helm", "umbrella")
	if entries, err := os.ReadDir(umbrella); err == nil {
		for _, e := range entries {
			n := e.Name()
			if !e.IsDir() && strings.HasPrefix(n, "values") && strings.HasSuffix(n, ".yaml") {
				add(filepath.Join(umbrella, n))
			}
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatal("обход не нашёл ни одного файла значений — гейт не утверждает ничего")
	}
	return out
}

// valuesInDir — файлы значений каталога: `values.yaml` и профили рядом с ним
// (`values.<что-то>.yaml`). Перечень выводится обходом, а не выписывается.
func valuesInDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, "values") && strings.HasSuffix(n, ".yaml") {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}

// findKeyPaths обходит разобранный YAML и возвращает пути до КЛЮЧЕЙ с данным
// именем. Судит узел-ключ отображения, а не текст: имя ключа встречается в этих
// файлах и в комментариях — в том числе в абзаце, объясняющем сам запрет, — и
// гейт по подстроке краснел бы на собственном объяснении.
func findKeyPaths(node *yaml.Node, name string, trail []string) []string {
	var out []string
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for i, c := range node.Content {
			step := trail
			if node.Kind == yaml.SequenceNode {
				step = append(append([]string{}, trail...), "["+strconv.Itoa(i)+"]")
			}
			out = append(out, findKeyPaths(c, name, step)...)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			k, v := node.Content[i], node.Content[i+1]
			next := append(append([]string{}, trail...), k.Value)
			if k.Value == name {
				out = append(out, strings.Join(next, "."))
			}
			out = append(out, findKeyPaths(v, name, next)...)
		}
	}
	return out
}

// TestDatabaseTLSKeyHasOneSpelling — во всех объявлениях ключ TLS до базы пишется
// канонически.
//
// Проваливается на: ключе прежнего написания в любом файле значений и на пустом
// обходе (гейт, не прочитавший ни одного объявления, не утверждает ничего).
func TestDatabaseTLSKeyHasOneSpelling(t *testing.T) {
	root := repoRoot(t)
	files := valuesFiles(t, root)

	var canonical, filesRead, filesWithKey int
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var doc yaml.Node
		if uerr := yaml.Unmarshal(raw, &doc); uerr != nil {
			t.Fatalf("parse %s: %v", path, uerr)
		}
		filesRead++

		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		legacy := findKeyPaths(&doc, legacyDBTLSKey, nil)
		canon := findKeyPaths(&doc, canonicalDBTLSKey, nil)
		canonical += len(canon)
		if len(canon) > 0 || len(legacy) > 0 {
			filesWithKey++
		}
		for _, p := range legacy {
			t.Errorf("%s: ключ %s объявлен прежним написанием (%s). Вопрос «весь ли флот "+
				"шифрован», заданный одним grep по канону, ответил бы про флот БЕЗ этого "+
				"сервиса и выглядел бы исчерпывающим. Переименовать в %s; переходный приём "+
				"старого имени уже несёт шаблон чарта (<чарт>.dbSslMode)",
				rel, p, legacyDBTLSKey, canonicalDBTLSKey)
		}
	}

	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	if filesRead == 0 {
		t.Fatal("прочитано ноль файлов значений — гейт судил бы по пустоте")
	}
	if canonical == 0 {
		t.Fatalf("во всех %d файлах значений не нашлось ни одного ключа %s — либо ключ "+
			"переименован целиком, либо разбор перестал его видеть; и то и другое находка",
			filesRead, canonicalDBTLSKey)
	}
	t.Logf("перепись: файлов значений %d · из них объявляют ключ %d · канонических ключей %d",
		filesRead, filesWithKey, canonical)
}
