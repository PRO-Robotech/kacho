// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// resource_id_shape_owned_types_test.go — тип, которым владеет служба kacho, не
// уходит со строгой формы полосы 5b МОЛЧА (kacho#2976, замечание ревью
// безопасности).
//
// ПРЕДМЕТ. Строгость записи выводится из каталога (markIDShapeJudged): тип,
// который называет конкретной областью метод ДРУГОГО корня, считается чужим и
// судится прежним судом одним префиксом. Значит, одна строка каталога службы
// доступа с областью, скажем, `storage_snapshot` вернула бы этот тип на суд
// префиксом — и ни одна проба поведения не покраснела бы: они берут по одному
// типу.
//
// ОЖИДАЕМОЕ — ИЗ ОБЪЯВЛЕНИЯ ВЛАДЕЛЬЦА, А НЕ СПИСКОМ. Тип модели прав, которым
// владеет служба, объявлен в её манифесте домена (`services/<svc>/manifest.yaml`,
// раздел `resources[].objectType`) — это то, что модуль объявляет платформе о
// своих правах. Из чеканки (`ids.NewID`/`NewHyphenID`) перечень не выводится:
// она называет ПРЕФИКС (`snp`), а связи префикса с типом модели
// (`storage_snapshot`) данными нет нигде — её пришлось бы выписать, то есть
// завести ровно тот ручной перечень, от которого проба уходит.
//
// СВЕРКА В ОБЕ СТОРОНЫ. Ожидаемое = типы манифестов kacho, служащие конкретной
// областью хотя бы одному методу kacho. Находка:
//   - тип ожидаемого, не судимый строго (откат на суд префиксом), с именем
//     строки другого корня, которая его увела;
//   - тип, судимый строго, которого не объявляет ни один манифест kacho.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ownedTypeShapeCensus — объём осмотренного: без него «находок 0» неотличимо от
// «прочитано 0».
type ownedTypeShapeCensus struct {
	manifests, ownedTypes, catalogRows, expected, strict int
}

// readKachoOwnedTypes — типы модели прав, объявленные манифестами служб kacho,
// с именем службы-владельца.
func readKachoOwnedTypes(t *testing.T) (map[string]string, int) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "services", "*", "manifest.yaml"))
	if err != nil {
		t.Fatalf("обход манифестов: %v", err)
	}
	owned := map[string]string{}
	for _, p := range paths {
		raw, rerr := os.ReadFile(p) // #nosec G304 — путь из обхода дерева
		if rerr != nil {
			t.Fatalf("чтение %s: %v", p, rerr)
		}
		var m struct {
			Module    string `yaml:"module"`
			Resources []struct {
				ObjectType string `yaml:"objectType"`
			} `yaml:"resources"`
		}
		if uerr := yaml.Unmarshal(raw, &m); uerr != nil {
			t.Fatalf("разбор %s: %v", p, uerr)
		}
		if m.Module == "" || len(m.Resources) == 0 {
			t.Fatalf("манифест %s без module или без resources — разбор не о том", p)
		}
		for _, r := range m.Resources {
			if r.ObjectType == "" {
				t.Fatalf("манифест %s: ресурс без objectType", p)
			}
			owned[r.ObjectType] = m.Module
		}
	}
	return owned, len(paths)
}

// ownedTypeShapeFindings — сверка строгих типов загруженного каталога с типами,
// которыми владеют службы kacho.
func ownedTypeShapeFindings(t *testing.T, catalogJSON []byte, owned map[string]string) ([]string, ownedTypeShapeCensus) {
	t.Helper()
	c := NewPermissionCatalog()
	if err := c.LoadFromBytes(catalogJSON); err != nil {
		t.Fatalf("загрузка каталога: %v", err)
	}
	return ownedTypeShapeFindingsOf(*c.entries.Load(), owned)
}

// ownedTypeShapeFindingsOf — сверка над записями, уже помеченными загрузкой.
// Отделена от загрузки, чтобы проба могла испортить пометку саму по себе.
func ownedTypeShapeFindingsOf(entries map[string]CatalogEntry, owned map[string]string) ([]string, ownedTypeShapeCensus) {
	expected := map[string]struct{}{}  // типы манифестов kacho, область метода kacho
	strict := map[string]struct{}{}    // типы, судимые строго хоть одной записью
	lenient := map[string]struct{}{}   // типы kacho-метода на суде префиксом
	foreignBy := map[string][]string{} // тип → строки другого корня, называющие его
	for fqn, e := range entries {
		if !isConcreteResourceScope(e) {
			continue
		}
		typ := e.ScopeExtractor.ObjectType
		if fqnRoot(fqn) != idMintingRoot {
			foreignBy[typ] = append(foreignBy[typ], fqn)
			continue
		}
		if _, ok := owned[typ]; ok {
			expected[typ] = struct{}{}
		}
		if e.judgesIDShape {
			strict[typ] = struct{}{}
		} else {
			lenient[typ] = struct{}{}
		}
	}

	var findings []string
	for typ := range expected {
		_, isStrict := strict[typ]
		_, isLenient := lenient[typ]
		if isStrict && !isLenient {
			continue
		}
		by := foreignBy[typ]
		sort.Strings(by)
		cause := "чужой строки нет — откат пометки в самой загрузке каталога"
		if len(by) > 0 {
			cause = "его называют областью строки другого корня: " + strings.Join(by, ", ")
		}
		findings = append(findings, fmt.Sprintf(
			"тип %s (служба %s) ушёл со строгой формы полосы 5b на суд префиксом; %s",
			typ, owned[typ], cause))
	}
	for typ := range strict {
		if _, ok := owned[typ]; !ok {
			findings = append(findings, fmt.Sprintf(
				"тип %s судится строгой формой, но ни один манифест службы kacho его не объявляет", typ))
		}
	}
	sort.Strings(findings)
	return findings, ownedTypeShapeCensus{
		ownedTypes: len(owned), catalogRows: len(entries),
		expected: len(expected), strict: len(strict),
	}
}

// requireOwnedTypeCensus — предпосылка пробы: пустой обход не вердикт.
func requireOwnedTypeCensus(t *testing.T, cen ownedTypeShapeCensus) {
	t.Helper()
	t.Logf("перепись: манифестов kacho %d · типов объявлено %d · строк каталога %d · ожидаемо строгих %d · судимых строго %d",
		cen.manifests, cen.ownedTypes, cen.catalogRows, cen.expected, cen.strict)
	if cen.manifests == 0 || cen.ownedTypes == 0 || cen.catalogRows == 0 || cen.expected == 0 {
		t.Fatalf("пустой обход — вердикта нет: %+v", cen)
	}
}

// withInjectedRow — копия вшитого каталога с одной добавленной строкой.
func withInjectedRow(t *testing.T, row string) []byte {
	t.Helper()
	var rows []json.RawMessage
	if err := json.Unmarshal(EmbeddedPermissionCatalogJSON(), &rows); err != nil {
		t.Fatalf("разбор вшитого каталога: %v", err)
	}
	rows = append(rows, json.RawMessage(row))
	out, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("сборка каталога: %v", err)
	}
	return out
}

// kanameRowScoping — строка службы доступа, называющая тип своей конкретной
// областью.
func kanameRowScoping(objectType string) string {
	return `{"fqn":"kaname.cloud.iam.v1.ShapeProbeService/Get","permission":"iam.shapeProbes.get",` +
		`"required_relation":"viewer","scope_extractor":{"object_type":"` + objectType +
		`","from_request_field":"probe_id"},"required_acr_min":"2"}`
}

// TestKachoOwnedTypesStayOnTheStrictIDShape — во вшитом каталоге каждый тип,
// которым владеет служба kacho и который служит областью её методу, судится
// строгой формой, и строгих типов вне манифестов нет.
func TestKachoOwnedTypesStayOnTheStrictIDShape(t *testing.T) {
	owned, manifests := readKachoOwnedTypes(t)
	findings, cen := ownedTypeShapeFindings(t, EmbeddedPermissionCatalogJSON(), owned)
	cen.manifests = manifests
	requireOwnedTypeCensus(t, cen)
	for _, f := range findings {
		t.Error(f)
	}
}

// TestKachoOwnedTypeShapeGate_KanameRowNamingAKachoTypeIsFound — инъекция:
// строка kaname с областью `storage_snapshot` уводит тип со строгой формы, и
// сверка называет тип и строку.
func TestKachoOwnedTypeShapeGate_KanameRowNamingAKachoTypeIsFound(t *testing.T) {
	owned, manifests := readKachoOwnedTypes(t)
	findings, cen := ownedTypeShapeFindings(t, withInjectedRow(t, kanameRowScoping("storage_snapshot")), owned)
	cen.manifests = manifests
	requireOwnedTypeCensus(t, cen)
	if len(findings) != 1 {
		t.Fatalf("ждали ровно одну находку о storage_snapshot, получили %d: %v", len(findings), findings)
	}
	for _, want := range []string{"storage_snapshot", "служба storage", "kaname.cloud.iam.v1.ShapeProbeService/Get"} {
		if !strings.Contains(findings[0], want) {
			t.Errorf("находка не называет %q: %s", want, findings[0])
		}
	}
}

// TestKachoOwnedTypeShapeGate_KanameRowNamingItsOwnTypeIsSilent — близнец: строка
// kaname с областью `project` (тип службы доступа) ничего не меняет.
func TestKachoOwnedTypeShapeGate_KanameRowNamingItsOwnTypeIsSilent(t *testing.T) {
	owned, manifests := readKachoOwnedTypes(t)
	findings, cen := ownedTypeShapeFindings(t, withInjectedRow(t, kanameRowScoping("project")), owned)
	cen.manifests = manifests
	requireOwnedTypeCensus(t, cen)
	if len(findings) != 0 {
		t.Fatalf("строка kaname со своей областью не должна давать находок: %v", findings)
	}
}

// TestKachoOwnedTypeShapeGate_StrictTypeNoManifestDeclaresIsFound — инъекция
// обратного направления: метод kacho с областью типа, которого не объявляет ни
// один манифест, судится строго, и сверка называет этот тип.
func TestKachoOwnedTypeShapeGate_StrictTypeNoManifestDeclaresIsFound(t *testing.T) {
	owned, manifests := readKachoOwnedTypes(t)
	row := `{"fqn":"kacho.cloud.compute.v1.ShapeProbeService/Get","permission":"compute.shapeProbes.get",` +
		`"required_relation":"viewer","scope_extractor":{"object_type":"compute_shape_probe",` +
		`"from_request_field":"probe_id"},"required_acr_min":"1"}`
	findings, cen := ownedTypeShapeFindings(t, withInjectedRow(t, row), owned)
	cen.manifests = manifests
	requireOwnedTypeCensus(t, cen)
	if len(findings) != 1 || !strings.Contains(findings[0], "compute_shape_probe") {
		t.Fatalf("ждали ровно одну находку о compute_shape_probe, получили %d: %v", len(findings), findings)
	}
}

// TestKachoOwnedTypeShapeGate_MarkLostWithoutAForeignRowNamesTheLoad — откат
// пометки без чужой строки (пометку снимает сама загрузка) называется ЭТОЙ
// причиной, а не строкой другого корня с пустым перечнем.
func TestKachoOwnedTypeShapeGate_MarkLostWithoutAForeignRowNamesTheLoad(t *testing.T) {
	owned, _ := readKachoOwnedTypes(t)
	c := NewPermissionCatalog()
	if err := c.LoadFromBytes(EmbeddedPermissionCatalogJSON()); err != nil {
		t.Fatalf("загрузка каталога: %v", err)
	}
	entries := map[string]CatalogEntry{}
	for fqn, e := range *c.entries.Load() {
		if e.ScopeExtractor.ObjectType == "storage_snapshot" {
			e.judgesIDShape = false
		}
		entries[fqn] = e
	}
	findings, _ := ownedTypeShapeFindingsOf(entries, owned)
	if len(findings) != 1 {
		t.Fatalf("ждали ровно одну находку о storage_snapshot, получили %d: %v", len(findings), findings)
	}
	f := findings[0]
	if !strings.Contains(f, "storage_snapshot") || !strings.Contains(f, "чужой строки нет — откат пометки в самой загрузке каталога") {
		t.Errorf("находка называет не ту причину: %s", f)
	}
	if strings.Contains(f, "строки другого корня") {
		t.Errorf("находка называет причиной строку другого корня, которой нет: %s", f)
	}
}
