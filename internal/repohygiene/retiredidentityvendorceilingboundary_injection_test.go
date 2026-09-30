// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Два ОТДЕЛЬНЫХ решения распознавателя (#2896), каждое со своей однофактной
// парой: путь API издателя, ВЛОЖЕННЫЙ в более длинный путь, и строка, которую
// единый дифф УДАЛЯЕТ. Оба — не ослабление суда, а отказ считать привязкой то,
// что ею не является; цена каждого названа переписью числом и словами, как цена
// границы слова.
//
// Вход собирается из словарей самого гейта (`ProviderSurfaces`,
// `retiredVendorMarks`), а не литералом: литерал с именем или путём издателя в
// строке кода был бы привязкой дерева платформы — ростом, который эти же пробы
// и судят.

// vendorNestedImport — путь импорта пакета, в чьём пути путь API стоит
// СЕРЕДИНОЙ: сегмент до него и сегмент после.
func vendorNestedImport(surface string) string {
	return "github.com/PRO-Robotech/corelib/internal" + surface + "/jwt"
}

// TestRetiredVendorCeiling_NestedSurface_ImportPathIsNotTheSurface — ЗАКОННЫЙ
// БЛИЗНЕЦ: путь API издателя, стоящий серединой пути импорта пакета, — адрес
// пакета, а не разговор с издателем.
//
// Предмет — 44 строки фундамента на пине v1.10.0-rc.3: пакеты
// `internal/oauth2/token/jwt` и `.../hmac` вложенного движка засчитывались осью
// пути API, и потолок дерева краснел на коде, ради которого снятие и делается.
func TestRetiredVendorCeiling_NestedSurface_ImportPathIsNotTheSurface(t *testing.T) {
	t.Parallel()
	for _, s := range ProviderSurfaces {
		line := "\t\"" + vendorNestedImport(s.Path) + "\""
		got, bindings := judgeOne(t, vendorFixture(map[string]string{
			"internal/engine/a.go": "package engine\n\nimport (\n" + line + "\n)\n"}, nil))
		if got != 0 {
			t.Errorf("путь %s серединой пути импорта засчитан привязкой (%d): %v — "+
				"адрес пакета прочитан как путь API издателя", s.Path, got, bindings)
		}
	}
}

// TestRetiredVendorCeiling_NestedSurface_PathEndingInTheSurfaceIsCaught — ОДИН
// ФАКТ против близнеца: сегмента ПОСЛЕ пути API нет. Путь кончается путём API
// — это он и есть.
func TestRetiredVendorCeiling_NestedSurface_PathEndingInTheSurfaceIsCaught(t *testing.T) {
	t.Parallel()
	for _, s := range ProviderSurfaces {
		line := "\t\"" + strings.TrimSuffix(vendorNestedImport(s.Path), "/jwt") + "\""
		got, bindings := judgeOne(t, vendorFixture(map[string]string{
			"internal/engine/a.go": "package engine\n\nimport (\n" + line + "\n)\n"}, nil))
		if got != 1 || bindings[0].Axis != vendorAxisSurface {
			t.Errorf("путь, КОНЧАЮЩИЙСЯ путём API %s, дал привязок %d (ось %q), ожидалась 1 "+
				"осью пути API — решение о вложенности смотрит не на сегмент после", s.Path,
				got, axisOf(bindings))
		}
	}
}

// TestRetiredVendorCeiling_NestedSurface_AuthorityBeforeIsCaught — ОДИН ФАКТ
// против близнеца ниже: перед путём API стоит АДРЕС узла, а не сегмент пути.
// Путь API с продолжением после него — законная форма разговора (`/admin/clients/`
// с идентификатором), и она засчитывается.
func TestRetiredVendorCeiling_NestedSurface_AuthorityBeforeIsCaught(t *testing.T) {
	t.Parallel()
	for _, s := range ProviderSurfaces {
		line := "u := \"https://iam.example.net:8443" + s.Path + "/cli-1\""
		got, bindings := judgeOne(t, vendorFixture(map[string]string{
			"internal/clients/a.go": "package clients\n" + line + "\n"}, nil))
		if got != 1 || bindings[0].Axis != vendorAxisSurface {
			t.Errorf("путь API %s сразу за адресом узла дал привязок %d (ось %q), ожидалась 1",
				s.Path, got, axisOf(bindings))
		}
	}
}

// TestRetiredVendorCeiling_NestedSurface_SegmentBeforeAndAfterIsSilent —
// ЗАКОННЫЙ БЛИЗНЕЦ предыдущей: та же строка, но перед путём API стоит СЕГМЕНТ
// пути. Путь API стоит серединой — отброшен, и отброшенное названо переписью.
func TestRetiredVendorCeiling_NestedSurface_SegmentBeforeAndAfterIsSilent(t *testing.T) {
	t.Parallel()
	for _, s := range ProviderSurfaces {
		line := "u := \"https://iam.example.net:8443/v1" + s.Path + "/cli-1\""
		corpora := vendorFixture(map[string]string{
			"internal/clients/a.go": "package clients\n" + line + "\n"}, nil)
		_, census, bindings, err := judgeRetiredVendorCeiling(corpora, vendorZeroCeilings)
		if err != nil {
			t.Fatalf("судья: %v", err)
		}
		c := census[vendorTreePlatform]
		if c.Bindings != 0 {
			t.Errorf("путь API %s серединой пути засчитан привязкой (%d): %v", s.Path, c.Bindings, bindings)
		}
		want := "v1" + s.Path + "/cli-1"
		if c.SurfaceNested != 1 || len(c.SurfaceNestedPaths) != 1 || c.SurfaceNestedPaths[0] != want {
			t.Errorf("перепись вложенного пути API: строк %d, путей %v, ожидалось 1 и [%s] — "+
				"цена решения не названа, и настоящая привязка ушла бы молча",
				c.SurfaceNested, c.SurfaceNestedPaths, want)
		}
	}
}

// TestRetiredVendorCeiling_NestedSurface_LawfulCallFormsStayCaught — формы
// разговора, которые словарь поверхностей объявляет законными (путь целиком,
// хвостом к базовому адресу, с идентификатором вслед), решением о вложенности
// не задеты.
func TestRetiredVendorCeiling_NestedSurface_LawfulCallFormsStayCaught(t *testing.T) {
	t.Parallel()
	for _, s := range ProviderSurfaces {
		for _, line := range []string{
			"u := base + \"" + s.Path + "\"",
			"u := base + \"" + s.Path + "/\" + id",
			"u := fmt.Sprintf(\"%s" + s.Path + "/%s\", base, id)",
			"  raw: \"{{baseUrl}}" + s.Path + "/{{clientId}}\"",
			"curl -X DELETE \"$ADMIN" + s.Path + "?subject=$S\"",
			"url: http://iam.local" + s.Path,
		} {
			got, _ := judgeOne(t, vendorFixture(map[string]string{
				"internal/clients/a.go": "package clients\n" + line + "\n"}, nil))
			if got != 1 {
				t.Errorf("законная форма разговора %q дала привязок %d, ожидалась 1", line, got)
			}
		}
	}
}

// vendorUnifiedDiff — единый дифф одного файла с одним куском: строка `old`
// удаляется, строка `neu` добавляется, вокруг — по строке контекста.
func vendorUnifiedDiff(header, old, neu string) string {
	return "diff -ruN a/x_test.go b/x_test.go\n" +
		"--- a/x_test.go\n" +
		"+++ b/x_test.go\n" +
		header + "\n" +
		" package x\n" +
		"-" + old + "\n" +
		"+" + neu + "\n" +
		" var t = 1\n"
}

// TestRetiredVendorCeiling_DiffRemoval_RemovedLineIsNotABinding — ЗАКОННЫЙ
// БЛИЗНЕЦ: строка, которую кусок единого диффа УДАЛЯЕТ, — запись снятия, а не
// привязка. Дерево, в которое дифф накладывается, этой строки НЕ несёт.
//
// Предмет — файл правки вложенного движка фундамента: правка чужой фикстуры,
// несущей имя издателя, записывается в нём удалённой строкой, и без этого
// решения переименование фикстуры переносило бы привязку из файла в запись о её
// снятии, не снимая числа.
func TestRetiredVendorCeiling_DiffRemoval_RemovedLineIsNotABinding(t *testing.T) {
	t.Parallel()
	named := "var s = \"" + retiredVendorMarks[0] + "\""
	corpora := vendorFixture(map[string]string{
		"internal/engine/PROVENANCE.patch": vendorUnifiedDiff("@@ -1,3 +1,3 @@", named, "var s = \"acme\""),
	}, nil)
	_, census, bindings, err := judgeRetiredVendorCeiling(corpora, vendorZeroCeilings)
	if err != nil {
		t.Fatalf("судья: %v", err)
	}
	c := census[vendorTreePlatform]
	if c.Bindings != 0 {
		t.Errorf("удалённая строка диффа засчитана привязкой (%d): %v", c.Bindings, bindings)
	}
	if c.DiffRemoved != 1 || c.DiffRemovedFiles != 1 {
		t.Errorf("перепись записей снятия: строк %d, файлов %d, ожидалось 1 и 1 — цена решения "+
			"не названа", c.DiffRemoved, c.DiffRemovedFiles)
	}
}

// TestRetiredVendorCeiling_DiffRemoval_AddedLineIsCaught — ОДИН ФАКТ: имя несёт
// ДОБАВЛЯЕМАЯ строка. Дифф, вносящий имя, привязывает дерево, в которое
// накладывается.
func TestRetiredVendorCeiling_DiffRemoval_AddedLineIsCaught(t *testing.T) {
	t.Parallel()
	named := "var s = \"" + retiredVendorMarks[0] + "\""
	got, bindings := judgeOne(t, vendorFixture(map[string]string{
		"internal/engine/PROVENANCE.patch": vendorUnifiedDiff("@@ -1,3 +1,3 @@", "var s = \"acme\"", named),
	}, nil))
	if got != 1 || bindings[0].Axis != vendorAxisName {
		t.Errorf("добавляемая строка диффа с именем дала привязок %d (ось %q), ожидалась 1", got, axisOf(bindings))
	}
}

// TestRetiredVendorCeiling_DiffRemoval_NoHunkHeaderIsCaught — ОДИН ФАКТ: у
// строки нет заголовка куска. Строка, начинающаяся минусом, без куска — не
// запись снятия: так пишется элемент списка YAML.
func TestRetiredVendorCeiling_DiffRemoval_NoHunkHeaderIsCaught(t *testing.T) {
	t.Parallel()
	named := "var s = \"" + retiredVendorMarks[0] + "\""
	got, _ := judgeOne(t, vendorFixture(map[string]string{
		"internal/engine/PROVENANCE.patch": vendorUnifiedDiff("", named, "var s = \"acme\""),
	}, nil))
	if got != 1 {
		t.Errorf("строка с минусом вне куска дала привязок %d, ожидалась 1 — решение о записи "+
			"снятия читает знак, а не кусок", got)
	}
	got, _ = judgeOne(t, vendorFixture(map[string]string{
		"deploy/helm/umbrella/values.z.yaml": "hosts:\n- " + retiredVendorMarks[0] + "-admin\n"}, nil))
	if got != 1 {
		t.Errorf("элемент списка YAML с именем дал привязок %d, ожидалась 1", got)
	}
}

// TestRetiredVendorCeiling_DiffRemoval_ContextLineIsCaught — ОДИН ФАКТ: имя
// несёт строка КОНТЕКСТА куска. Она есть по обе стороны диффа — в том числе в
// дереве, в которое он накладывается.
func TestRetiredVendorCeiling_DiffRemoval_ContextLineIsCaught(t *testing.T) {
	t.Parallel()
	body := "diff -ruN a/x_test.go b/x_test.go\n--- a/x_test.go\n+++ b/x_test.go\n" +
		"@@ -1,3 +1,3 @@\n" +
		" var s = \"" + retiredVendorMarks[0] + "\"\n" +
		"-var u = 1\n" +
		"+var u = 2\n" +
		" var t = 1\n"
	got, _ := judgeOne(t, vendorFixture(map[string]string{"internal/engine/PROVENANCE.patch": body}, nil))
	if got != 1 {
		t.Errorf("строка контекста куска с именем дала привязок %d, ожидалась 1", got)
	}
}

// TestRetiredVendorCeiling_DiffRemoval_HunkEndsAtItsCount — ОДИН ФАКТ: число
// строк куска исчерпано. Строка с минусом ПОСЛЕ исчерпанного куска запись
// снятия не продолжает — кусок кончается своим заголовком, а не следующим
// минусом.
func TestRetiredVendorCeiling_DiffRemoval_HunkEndsAtItsCount(t *testing.T) {
	t.Parallel()
	body := "diff -ruN a/x_test.go b/x_test.go\n--- a/x_test.go\n+++ b/x_test.go\n" +
		"@@ -1 +1 @@\n" +
		"-var u = 1\n" +
		"+var u = 2\n" +
		"-var s = \"" + retiredVendorMarks[0] + "\"\n"
	got, _ := judgeOne(t, vendorFixture(map[string]string{"internal/engine/PROVENANCE.patch": body}, nil))
	if got != 1 {
		t.Errorf("строка с минусом за исчерпанным куском дала привязок %d, ожидалась 1", got)
	}
}
