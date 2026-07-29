// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Снятые с контракта ручки не возвращаются.
//
// Поле запроса или настройки, которое сервис принимает и ни разу не читает, — это
// обещание, за которое никто не отвечает: оператор задаёт значение, чарт его
// доставляет, а поведение не меняется, и узнать об этом можно только по
// последствиям. Две такие ручки у vpc сняты целиком:
//
//   - `authz.tuple-write.*` — прямой FGA-writer убран (регистрация владения идёт
//     намерением в своей транзакции и очередью через iam), а ветка настройки,
//     чарт и профили продолжали описывать снятое поведение;
//   - `authz.list-filter.model-id` — версию модели авторизации закрепляет за собой
//     iam (единственный, кто ходит в хранилище прав), и запрос BatchCheck поля под
//     неё не имеет вовсе, поэтому доставить сюда было нечего.
//
// Проверка держит их снятыми: возвращаются такие ручки не заново продуманными, а
// копированием у соседа, и тогда всё повторяется. Экземпляров два, поэтому и
// перечень закрытый — но осматривается ВСЁ дерево сервиса и ВСЕ профили стенда,
// а не места, где ручки лежали.
var retiredKnobs = []struct {
	token string
	why   string
}{
	{"tuple-write", "authz.tuple-write.* retired: the direct FGA writer is gone (ownership is registered by intent + queue via iam)"},
	{"tupleWrite", "authz.tupleWrite chart key retired together with authz.tuple-write.*"},
	{"TUPLE_WRITE", "KACHO_VPC_AUTHZ__TUPLE_WRITE__* retired together with authz.tuple-write.*"},
	{"TupleWriteConfig", "the Go type behind authz.tuple-write.* is retired"},
	{"AuthZ.TupleWrite", "the Go field behind authz.tuple-write.* is retired"},
	{"list-filter.model-id", "authz.list-filter.model-id retired: iam pins the authorization model; BatchCheck has no field for a caller-supplied one"},
	{"LIST_FILTER__MODEL_ID", "KACHO_VPC_AUTHZ__LIST_FILTER__MODEL_ID retired together with authz.list-filter.model-id"},
	{"listFilter.modelId", "the chart key behind authz.list-filter.model-id is retired"},
	{"ListFilter.ModelID", "the Go field behind authz.list-filter.model-id is retired"},
}

// scannedExtensions — что осматриваем в дереве сервиса. Настройка живёт в коде,
// в шаблонах чарта и в профилях; списка «где искать» по местам прошлых находок
// быть не должно.
var scannedExtensions = map[string]bool{
	".go": true, ".yaml": true, ".yml": true, ".tpl": true, ".sh": true, ".md": true,
}

// selfPath — единственный файл, освобождённый от проверки: он и есть перечень
// снятых ручек, поэтому обязан их называть. Освобождение проверяет СВОЙ предмет
// (файл существует), иначе переименование превратило бы проверку в осмотр
// дерева, из которого молча выпал бы настоящий файл.
const selfPath = "retired_knobs_test.go"

func TestRetiredKnobsStayRetired(t *testing.T) {
	if _, err := os.Stat(selfPath); err != nil {
		t.Fatalf("self-exemption has no subject (%s): %v", selfPath, err)
	}

	const serviceRoot = "../../../.."

	var (
		scanned int
		hits    []string
	)
	err := filepath.WalkDir(serviceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "out", "collections":
				return filepath.SkipDir
			}
			return nil
		}
		if !scannedExtensions[filepath.Ext(path)] {
			return nil
		}
		if filepath.Base(path) == selfPath {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		scanned++
		for lineNo, line := range strings.Split(string(body), "\n") {
			for _, k := range retiredKnobs {
				if strings.Contains(line, k.token) {
					hits = append(hits, filepath.Clean(path)+":"+itoa(lineNo+1)+" — "+k.why)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk service tree: %v", err)
	}

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if scanned == 0 {
		t.Fatal("the service tree scan read no files — the check proves nothing")
	}
	t.Logf("scanned %d file(s) under %s", scanned, serviceRoot)
	if len(hits) > 0 {
		t.Fatalf("retired knob(s) reintroduced:\n  %s", strings.Join(hits, "\n  "))
	}
}

// TestRetiredKnobsAbsentFromStandProfiles — те же ручки в профилях стенда.
// Профили лежат вне дерева сервиса и адресуют его блоком `vpc:`, поэтому здесь
// разбирается YAML, а не текст: у соседей (compute, nlb) свои `tupleWrite`, и
// поиск подстрокой отвергал бы чужую живую настройку.
func TestRetiredKnobsAbsentFromStandProfiles(t *testing.T) {
	profiles, err := filepath.Glob("../../../../../../deploy/helm/umbrella/values*.yaml")
	if err != nil {
		t.Fatalf("glob stand profiles: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("no stand profile matched — the check proves nothing")
	}

	var hits []string
	for _, p := range profiles {
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("read %s: %v", p, rerr)
		}
		var root map[string]any
		if uerr := yaml.Unmarshal(body, &root); uerr != nil {
			t.Fatalf("parse %s: %v", p, uerr)
		}
		for _, path := range [][]string{
			{"vpc", "authz", "tupleWrite"},
			{"vpc", "authz", "listFilter", "modelId"},
		} {
			if _, ok := lookupYAML(root, path); ok {
				hits = append(hits, filepath.Clean(p)+": "+strings.Join(path, "."))
			}
		}
	}
	t.Logf("parsed %d stand profile(s)", len(profiles))
	if len(hits) > 0 {
		t.Fatalf("retired knob(s) still set for vpc in stand profiles:\n  %s", strings.Join(hits, "\n  "))
	}
}

func lookupYAML(node any, path []string) (any, bool) {
	cur := node
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
