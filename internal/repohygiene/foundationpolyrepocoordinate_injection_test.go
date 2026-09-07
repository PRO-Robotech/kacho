// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// foundationpolyrepocoordinate_injection_test.go — доказательство того, что
// проверка прозы фундамента СПОСОБНА упасть, и того, что она молчит на законных
// близнецах.
//
// # Почему синтетический корень, а не правка дерева
//
// Проверка читает дерево, которое читают и соседние сессии. Внести в него дефект
// ради доказательства значило бы править общее состояние. Разбор вынесен в
// чистую функцию над ПРОИЗВОЛЬНЫМ корнем, и сюда подаётся корень, собранный в
// каталоге прогона.
//
// # Каждая инъекция меняет ровно один факт против контроля
//
// Контроль стоит первым и обязан МОЛЧАТЬ. Ось «краснеет» одна — она и есть
// предмет. Осей «молчит» пять, и у каждой полосы стоит ПАРА: «в полосе — молчит»
// и «та же строка вне полосы — находка». Без второй половины пропуск был бы
// маской, а не полосой.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// fpcRootWith собирает корень из перечисленных файлов. Файлов ровно столько,
// сколько подано: перепись тогда прямо называет, что прочитано ровно то, что
// подано.
func fpcRootWith(t *testing.T, files map[string]string) *treecorpus.Tree {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического корня не собран — инъекция беспредметна: %v", err)
	}
	return tree
}

// fpcSoundTree — законный близнец: проза фундамента называет координаты ЭТОГО
// дерева. Проверка обязана молчать.
func fpcSoundTree() map[string]string {
	return map[string]string{
		"pkg/authz/doc.go": "" +
			"// Реализация порта живёт в `pkg/authz/authziam`.\n" +
			"package authz\n",
		"pkg/authz/authziam/check.go": "package authziam\n",
	}
}

// ── Контроль: годный корень молчит ──────────────────────────────────────────

func TestFpcInjectionControl_SoundFoundationIsSilent(t *testing.T) {
	census, findings, err := scanFoundationProse(fpcRootWith(t, fpcSoundTree()))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("годный фундамент объявлен нарушением: проверка ловит форму, а не существо: %v", findings)
	}
	if census.filesRead != 2 {
		t.Fatalf("контроль беспредметен: прочитано %d файлов вместо 2", census.filesRead)
	}
	if census.coordinates != 0 || census.bareNames != 0 {
		t.Fatalf("контроль беспредметен: распознано лишнее (%s)", census)
	}
}

// ── Ось «краснеет»: мёртвая полирепо-координата — НАХОДКА с координатой ─────

func TestFpcInjection_DeadPolyrepoCoordinateIsFound(t *testing.T) {
	files := fpcSoundTree()
	files["pkg/authz/check_client.go"] = "" +
		"// Реализация — клиентский adapter `kacho-vpc/internal/clients/iam_authz_client.go`.\n" +
		"package authz\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("мёртвая координата не найдена — проверка вакуумна: %v (%s)", findings, census)
	}
	got := findings[0].String()
	for _, want := range []string{"pkg/authz/check_client.go:1", "kacho-vpc/internal/clients/iam_authz_client.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("находка не называет %q — читатель ищет координату глазами: %s", want, got)
		}
	}
}

// ── Молчит: та же форма, но координата РЕЗОЛВИТСЯ ───────────────────────────
//
// Проза пишет координату от корня СЛУЖБЫ, а не репозитория, поэтому совпадение
// ищется суффиксом. Без этой оси гейт краснел бы на живом адресе.

func TestFpcInjection_ResolvingCoordinateStaysSilent(t *testing.T) {
	files := fpcSoundTree()
	files["pkg/authz/edge.go"] = "// страж посадки объявлен в `kacho-geo/serve.go`\npackage authz\n"
	files["services/geo/cmd/kacho-geo/serve.go"] = "package main\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("живая координата объявлена мёртвой: %v", findings)
	}
	if census.coordinates != 1 || census.resolved != 1 {
		t.Fatalf("резолв не сосчитан: %s", census)
	}
}

func TestFpcInjection_TheSameCoordinateWithoutItsFileIsFound(t *testing.T) {
	files := fpcSoundTree()
	files["pkg/authz/edge.go"] = "// страж посадки объявлен в `kacho-geo/serve.go`\npackage authz\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("без своего файла та же координата обязана быть находкой — иначе резолв есть маска: %v", findings)
	}
}

// ── Молчит: голое имя без пути — это НАЗВАНИЕ службы, а не адрес ────────────

func TestFpcInjection_BareServiceNameStaysSilent(t *testing.T) {
	files := fpcSoundTree()
	files["pkg/authz/proxytuple.go"] = "// публичность выражает сам кортеж: kacho-registry пишет его.\npackage authz\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("голое имя службы объявлено координатой: %v", findings)
	}
	if census.bareNames != 1 {
		t.Fatalf("полоса голых имён не сосчитана — «ноль находок» неотличимо от «не искали»: %s", census)
	}
}

// ── Молчит: заголовок запроса, а не путь ────────────────────────────────────

func TestFpcInjection_RequestHeaderStaysSilent(t *testing.T) {
	files := fpcSoundTree()
	files["pkg/authz/subject.go"] = "// личность приходит парой x-kacho-principal-id/type\npackage authz\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("заголовок запроса объявлен полирепо-координатой: левая граница не проверяется: %v", findings)
	}
	if census.coordinates != 0 {
		t.Fatalf("заголовок сосчитан координатой: %s", census)
	}
}

// ── Полоса порождённого: молчит С клеймом, находка БЕЗ него ────────────────

func TestFpcInjection_GeneratedFileStaysSilent(t *testing.T) {
	files := fpcSoundTree()
	files["pkg/api/kacho/cloud/registry/v1/registry.pb.go"] = "" +
		"// Code generated by protoc-gen-go. DO NOT EDIT.\n" +
		"// ID реестра. Prefix \"reg\" (kacho-corelib/ids.NewID).\n" +
		"package registryv1\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("порождённый файл объявлен нарушением: его текст приходит из контракта: %v", findings)
	}
	if census.filesGenerated != 1 {
		t.Fatalf("полоса порождённых не сосчитана: %s", census)
	}
}

func TestFpcInjection_TheSameLineWithoutTheGeneratedMarkIsFound(t *testing.T) {
	files := fpcSoundTree()
	files["pkg/api/kacho/cloud/registry/v1/registry.pb.go"] = "" +
		"// ID реестра. Prefix \"reg\" (kacho-corelib/ids.NewID).\n" +
		"package registryv1\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("без клейма порождённого та же строка обязана быть находкой — иначе полоса есть маска: %v", findings)
	}
}

// ── Полоса популяции: молчит ВНЕ фундамента, находка ВНУТРИ него ───────────

func TestFpcInjection_OutsideTheFoundationStaysSilent(t *testing.T) {
	files := fpcSoundTree()
	files["services/compute/internal/repo/jsonb.go"] = "// Зеркалит kacho-vpc/internal/repo/jsonb.go.\npackage repo\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("дерево служб судится проверкой фундамента: популяция шире объявленной: %v", findings)
	}
	if census.filesRead != 2 {
		t.Fatalf("файл вне популяции сосчитан прочитанным: %s", census)
	}
}

func TestFpcInjection_TheSameLineInsideTheFoundationIsFound(t *testing.T) {
	files := fpcSoundTree()
	files["pkg/db/jsonb.go"] = "// Зеркалит kacho-vpc/internal/repo/jsonb.go.\npackage db\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("внутри фундамента та же строка обязана быть находкой: %v", findings)
	}
}

// ── Пустой обход отличим от нуля находок ───────────────────────────────────

func TestFpcInjection_EmptyWalkIsDistinguishableFromZeroFindings(t *testing.T) {
	_, _, err := scanFoundationProse(fpcRootWith(t, map[string]string{"README.md": "нет кода\n"}))
	if err == nil {
		t.Fatal("пустой обход выдан за зелёный прогон")
	}
	if !strings.Contains(err.Error(), "обход пуст") {
		t.Fatalf("отказ не называет причину: %v", err)
	}
}
