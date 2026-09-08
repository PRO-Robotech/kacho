// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// idempotency_namespace_injection_test.go — доказательство того, что обе проверки
// idempotency_namespace_test.go СПОСОБНЫ упасть, падают ТОЛЬКО на своём предмете и
// называют координату.
//
// # Почему доказательство заведено вместе с переустройством
//
// Проверка значений существовала и была зелёной. Её переустроили: разбор вынесен в
// функцию, распознаватель научился новой форме записи (константа, вычисляемая через
// другую константу), рядом встала вторая проверка. Совпадение чисел переписи до и после
// способность падать НЕ доказывает — гейт, потерявший её, на чистом дереве выглядит точно
// так же. Доказывает только инъекция.
//
// # Оси и почему их разделение честное
//
//	контроль            — копия как есть: молчат обе проверки;
//	двойная форма       — краснеет ТОЛЬКО проверка единого источника: значения совпадают;
//	двойная + расхождение — краснеет проверка значений: она пережила переустройство;
//	законный близнец    — ресурс той же формы БЕЗ дефекта: молчат обе;
//	пустой обход        — вердикта нет вовсе, а не молчаливое зелёное.
//
// Ось «двойная форма» служит законным близнецом оси «двойная + расхождение»: одна и та же
// форма записи, различающаяся ровно значением. Без неё проверка значений ловила бы форму, а
// не существо.
//
// Обратного близнеца — «расхождение значений при едином источнике» — не существует BY
// CONSTRUCTION, и это не пробел доказательства, а то самое свойство, ради которого работа
// делалась: там, где источник один, разойтись нечему.
//
// Инъекция идёт по КОПИИ пакета в t.TempDir(): состояние, которого проверка не заводила,
// она не трогает — ни рабочую копию, ни индекс.

package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyProviderPackage — копия каталога пакета в каталог пробы. Возвращает корень копии.
func copyProviderPackage(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "provider")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("каталог копии: %v", err)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("каталог пакета не прочитан: %v", err)
	}
	copied := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		raw, err := os.ReadFile(e.Name()) // путь — имя из обхода каталога пакета
		if err != nil {
			t.Fatalf("чтение %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), raw, 0o600); err != nil {
			t.Fatalf("запись %s: %v", e.Name(), err)
		}
		copied++
	}
	if copied == 0 {
		t.Fatal("в копию не попало ни одного файла — инъекция была бы беспредметна")
	}
	return dst
}

// injectionOutcome — что сказали обе проверки о поданном дереве.
type injectionOutcome struct {
	value      []string
	source     []string
	comparable int
	single     int
}

func runAudit(t *testing.T, dir string) injectionOutcome {
	t.Helper()
	audit, err := auditIdempotencyNamespaces(dir)
	if err != nil {
		t.Fatalf("разбор копии %s: %v", dir, err)
	}
	comparable, single, _, _ := audit.census()
	return injectionOutcome{
		value:      audit.namespaceValueFindings(),
		source:     audit.namespaceSourceFindings(),
		comparable: comparable,
		single:     single,
	}
}

// edit — замена ровно одного вхождения. Замена, ничего не изменившая, роняет пробу: иначе
// «инъекция внесена» означало бы «файл прочитан».
func edit(t *testing.T, path, old, replacement string) {
	t.Helper()
	raw, err := os.ReadFile(path) // путь собран самой пробой в t.TempDir()
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	s := string(raw)
	if strings.Count(s, old) != 1 {
		t.Fatalf("в %s ожидалось ровно одно вхождение %q, найдено %d — инъекция не внесена",
			filepath.Base(path), old, strings.Count(s, old))
	}
	if err := os.WriteFile(path, []byte(strings.Replace(s, old, replacement, 1)), 0o600); err != nil {
		t.Fatalf("запись %s: %v", path, err)
	}
}

func named(findings []string, file string) bool {
	for _, f := range findings {
		if strings.HasPrefix(f, file+":") {
			return true
		}
	}
	return false
}

// Контроль: на дереве как есть молчат ОБЕ проверки, и перепись сходится.
func TestInjectionControlIsSilentOnTheTreeAsItIs(t *testing.T) {
	got := runAudit(t, copyProviderPackage(t))
	if len(got.value) != 0 {
		t.Errorf("на неизменённой копии проверка значений нашла %d: %v", len(got.value), got.value)
	}
	if len(got.source) != 0 {
		t.Errorf("на неизменённой копии проверка единого источника нашла %d: %v", len(got.source), got.source)
	}
	if got.comparable == 0 || got.single != got.comparable {
		t.Errorf("перепись контроля: сверяемых %d, выведено из одного объявления %d — "+
			"контроль обязан быть чистым, иначе оси инъекции не отделены",
			got.comparable, got.single)
	}
	t.Logf("контроль: сверяемых объявлений %d, все выведены из одного объявления", got.comparable)
}

// Двойная форма при СОВПАДАЮЩИХ значениях: краснеет только проверка единого источника.
func TestInjectionSplitDeclarationRedensOnlyTheSingleSourceCheck(t *testing.T) {
	dir := copyProviderPackage(t)
	path := filepath.Join(dir, "network_resource.go")
	edit(t, path, "resp.TypeName = typeNameVPCNetwork",
		`resp.TypeName = req.ProviderTypeName + "_vpc_network"`)
	edit(t, path, "\t\ttypeNameVPCNetwork, plan.ProjectID", "\t\t\"kacho_vpc_network\", plan.ProjectID")

	got := runAudit(t, dir)
	if !named(got.source, "network_resource.go") {
		t.Errorf("двойная форма не найдена проверкой единого источника; находки: %v", got.source)
	}
	if len(got.value) != 0 {
		t.Errorf("проверка значений покраснела на СОВПАДАЮЩИХ значениях — она ловит форму, "+
			"а не расхождение: %v", got.value)
	}
	if got.single != got.comparable-1 {
		t.Errorf("перепись не назвала ровно одно расщеплённое объявление: сверяемых %d, "+
			"выведено из одного %d", got.comparable, got.single)
	}
}

// Двойная форма при РАЗНЫХ значениях: проверка значений пережила переустройство.
//
// От оси выше отличается РОВНО ОДНИМ фактом — значением в пространстве ключа.
func TestInjectionDivergedValueRedensTheValueCheck(t *testing.T) {
	dir := copyProviderPackage(t)
	path := filepath.Join(dir, "network_resource.go")
	edit(t, path, "resp.TypeName = typeNameVPCNetwork",
		`resp.TypeName = req.ProviderTypeName + "_vpc_network"`)
	edit(t, path, "\t\ttypeNameVPCNetwork, plan.ProjectID", "\t\t\"kacho_vpc_netwerk\", plan.ProjectID")

	got := runAudit(t, dir)
	if !named(got.value, "network_resource.go") {
		t.Errorf("расхождение значений не найдено; находки: %v", got.value)
	}
	for _, f := range got.value {
		if !strings.Contains(f, "kacho_vpc_netwerk") || !strings.Contains(f, "network_resource.go:") {
			t.Errorf("находка не называет координату и расходящееся значение:\n%s", f)
		}
	}
}

// Законный близнец: ресурс ТОЙ ЖЕ формы, объявленный правильно, молчания не нарушает.
//
// Без него обе проверки ловили бы «в файле есть Metadata и вызов создания», а не существо.
func TestInjectionLegitimateTwinStaysSilent(t *testing.T) {
	dir := copyProviderPackage(t)
	before := runAudit(t, dir)

	twin := `package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

const typeNameTwinResource = providerTypeName + "_twin_resource"

type twinResource struct{}

func (r *twinResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = typeNameTwinResource
}

func (r *twinResource) Create() {
	_ = client.IdempotencyKey(typeNameTwinResource, "addr", nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "twin_resource.go"), []byte(twin), 0o600); err != nil {
		t.Fatalf("запись близнеца: %v", err)
	}

	got := runAudit(t, dir)
	if len(got.value) != 0 || len(got.source) != 0 {
		t.Errorf("законный близнец объявлен находкой: значения %v, источник %v", got.value, got.source)
	}
	if got.comparable != before.comparable+1 || got.single != before.single+1 {
		t.Errorf("близнец не попал в перепись: было сверяемых %d/выведено %d, стало %d/%d — "+
			"проверка его не читает, и её молчание о нём ничего не значит",
			before.comparable, before.single, got.comparable, got.single)
	}

	// Тот же близнец, отличающийся РОВНО ОДНИМ фактом — литералом на месте вызова.
	edit(t, filepath.Join(dir, "twin_resource.go"),
		"client.IdempotencyKey(typeNameTwinResource,", `client.IdempotencyKey("kacho_twin_resource",`)
	broken := runAudit(t, dir)
	if !named(broken.source, "twin_resource.go") {
		t.Errorf("двойная форма у близнеца не найдена; находки: %v", broken.source)
	}
}

// Символическая форма: имя из таблицы описаний читают ОБА написания — молчание; литерал на
// одной из сторон — находка. Отличие ровно в одном факте.
func TestInjectionSymbolicSourceIsRecognisedOnBothSides(t *testing.T) {
	dir := copyProviderPackage(t)
	path := filepath.Join(dir, "flat.go")

	if got := runAudit(t, dir); named(got.source, "flat.go") {
		t.Fatalf("таблица описаний объявлена двойной формой на неизменённом дереве: %v", got.source)
	}

	edit(t, path, "id, err := awaitCreate(ctx, r.c, postPath, idField, r.spec.tfName,",
		`id, err := awaitCreate(ctx, r.c, postPath, idField, "kacho_storage_volume",`)
	got := runAudit(t, dir)
	if !named(got.source, "flat.go") {
		t.Errorf("литерал вместо поля описания не найден; находки: %v", got.source)
	}
}

// Беспредметный обход не выдаётся за чистый: вердикта нет ВОВСЕ.
func TestInjectionEmptyWalkIsNotSilentGreen(t *testing.T) {
	empty := t.TempDir()
	if _, err := auditIdempotencyNamespaces(empty); err == nil {
		t.Error("пустой каталог разобран без отказа — «находок нет» означало бы «ничего не прочитано»")
	}

	dir := copyProviderPackage(t)
	edit(t, filepath.Join(dir, "iam_type_names.go"),
		`const providerTypeName = "kacho"`, `const providerTypeNameRenamed = "kacho"`)
	if _, err := auditIdempotencyNamespaces(dir); err == nil {
		t.Error("пакет без объявления имени провайдера разобран без отказа — составные имена " +
			"типов не с чем сводить, и молчание здесь означало бы слепоту")
	}
}
