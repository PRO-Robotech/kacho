// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_docs_refused_value_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что анализатор
// способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`ct2_docs_refused_value_test.go`) о способности падать не говорит ничего.
//
// Каждой инъекции приложен законный близнец ТОЙ ЖЕ формы, обязанный молчать.
// Отдельно проверяется главный способ ошибиться: значение из ПЕРЕЧНЯ ДОПУСТИМЫХ
// не есть отвергаемое, и страница вправе называть его молча.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type refusedValueStand struct{ root string }

func newRefusedValueStand(t *testing.T) *refusedValueStand {
	t.Helper()
	s := &refusedValueStand{root: t.TempDir()}

	s.write(t, "services/probe/internal/api/create.go", `
package api

func validate(kind string, srcType string, strategy string) error {
	if kind == "CONTAINER" {
		// Отказ, построенный ПРЯМЫМ вызовом.
		return invalidArg("instance_kind",
			"instanceKind CONTAINER is not creatable yet: a registry image has no durable address today")
	}
	if srcType == "registry.image" {
		// Отказ, построенный локальным ЗАМЫКАНИЕМ: распознаватель, привязанный
		// к имени построителя, этой формы не увидел бы.
		add := func(field, msg string) error { return invalidArg(field, msg) }
		return add("boot_source.type",
			"bootSource.type registry.image is not accepted yet: a registry image has no durable address today")
	}
	if srcType == "registry.snapshot" {
		// Объяснение отказа называет ДОПУСТИМОЕ значение. Распознаватель,
		// берущий значения из всего сообщения, объявил бы storage.image
		// отвергаемым — то есть краснел бы на единственном рабочем источнике.
		return invalidArg("boot_source.type",
			"bootSource.type registry.snapshot is not accepted yet: use storage.image instead")
	}
	if strategy != "SPREAD" && strategy != "PACK" {
		// ПЕРЕЧЕНЬ ДОПУСТИМЫХ — не отказ по значению. Ни SPREAD, ни PACK
		// отвергаемыми не являются, и страница вправе называть их молча.
		return invalidArg("placement_strategy", "placementStrategy must be one of SPREAD, PACK")
	}
	return nil
}

func invalidArg(field, msg string) error { return nil }
`)

	// Законная страница: называет оба отвергаемых значения И несёт сообщения
	// отказа — причём ПЕРЕНЕСЁННЫЕ по строкам, как это и бывает в разметке.
	s.write(t, "services/probe/docs/content/getting-started.mdx", `
Род `+"`CONTAINER`"+` объявлен контрактом, но на создании отвергается синхронно:
`+"`INVALID_ARGUMENT \"instanceKind CONTAINER is not creatable yet: a registry image has no"+`
durable address today"`+"`"+`. По той же причине отвергается и `+"`registry.image`"+`:
`+"`INVALID_ARGUMENT \"bootSource.type registry.image is not accepted yet: a registry image"+`
has no durable address today"`+"`"+`.

Стратегия размещения — `+"`SPREAD`"+` или `+"`PACK`"+`: это перечень ДОПУСТИМЫХ значений,
и говорить об отказе тут не о чем.
`)
	return s
}

func (s *refusedValueStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *refusedValueStand) run(t *testing.T) ([]DocsRefusedValueFinding, DocsRefusedValueCensus) {
	t.Helper()
	var log strings.Builder
	findings, census, err := AuditDocsRefusedValue(DocsRefusedValueOptions{
		Root: s.root,
		Services: []DocsRefusedValueService{
			{Name: "probe", CodeDir: "services/probe/internal", DocsDir: "services/probe/docs/content"},
		},
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return findings, census
}

// TestDocsRefusedValueInjection_SilentOnALawfulTree — положительный контроль.
func TestDocsRefusedValueInjection_SilentOnALawfulTree(t *testing.T) {
	s := newRefusedValueStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законной странице найдено %d: %v", len(findings), findings)
	}
	if census.Judged == 0 {
		t.Fatalf("рассужено 0 — молчание получено пустым обходом, а не законностью страницы")
	}
	names := strings.Join(census.RefusedNames, " ")
	for _, want := range []string{"CONTAINER", "registry.image"} {
		if !strings.Contains(names, want) {
			t.Errorf("в словаре отвергаемых нет %q — эта форма отказа не распознана; словарь: %v",
				want, census.RefusedNames)
		}
	}
}

// TestDocsRefusedValueInjection_ValueInTheExplanationIsNotRefused — значение,
// названное в ОБЪЯСНЕНИИ отказа («… use storage.image instead»), отвергаемым не
// является. Без среза подлежащего единственный рабочий источник загрузки попал
// бы в словарь отвергаемых, и гейт краснел бы на каждой верной странице.
func TestDocsRefusedValueInjection_ValueInTheExplanationIsNotRefused(t *testing.T) {
	s := newRefusedValueStand(t)
	_, census := s.run(t)
	for _, got := range census.RefusedNames {
		if got == "storage.image" {
			t.Fatalf("значение из ОБЪЯСНЕНИЯ отказа попало в словарь отвергаемых: %v",
				census.RefusedNames)
		}
	}
	if len(census.RefusedNames) < 3 {
		t.Fatalf("отвергаемых значений %d — ожидались три (CONTAINER, registry.image, "+
			"registry.snapshot); словарь неполон, и молчание выше ничего не доказывает: %v",
			len(census.RefusedNames), census.RefusedNames)
	}
	// И страница, называющая допустимое значение без слова об отказе, молчит.
	findings := s.injectPage(t, "services/probe/docs/content/api/boot-ok.mdx",
		"Единственный принимаемый источник загрузки — `storage.image`.\n")
	if len(findings) != 0 {
		t.Fatalf("страница, называющая ДОПУСТИМОЕ значение, объявлена нарушением: %v", findings)
	}
}

// TestDocsRefusedValueInjection_AllowedValuesAreNotRefused — главный способ
// ошибиться: значение из ПЕРЕЧНЯ ДОПУСТИМЫХ попало бы в словарь отвергаемых, и
// гейт краснел бы на каждой странице, называющей `SPREAD` или `PACK`.
func TestDocsRefusedValueInjection_AllowedValuesAreNotRefused(t *testing.T) {
	s := newRefusedValueStand(t)
	_, census := s.run(t)
	for _, allowed := range []string{"SPREAD", "PACK"} {
		for _, got := range census.RefusedNames {
			if got == allowed {
				t.Errorf("значение %q из перечня ДОПУСТИМЫХ попало в словарь отвергаемых — "+
					"распознаватель судит по присутствию в тексте отказа, а не по подлежащему", allowed)
			}
		}
	}
	// И страница, называющая их без единого слова об отказе, обязана молчать.
	s.write(t, "services/probe/docs/content/api/strategy.mdx",
		"Стратегия размещения принимает `SPREAD` и `PACK`.\n")
	findings, _ := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("страница, называющая ДОПУСТИМЫЕ значения, объявлена нарушением: %v", findings)
	}
}

func (s *refusedValueStand) injectPage(t *testing.T, rel, body string) []DocsRefusedValueFinding {
	t.Helper()
	s.write(t, rel, body)
	findings, _ := s.run(t)
	return findings
}

func requireRefusedFinding(t *testing.T, findings []DocsRefusedValueFinding, wantFile, wantValue string) {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v", len(findings), findings)
	}
	f := findings[0]
	if f.File != wantFile {
		t.Errorf("находка называет файл %q, а инъекция внесена в %q", f.File, wantFile)
	}
	if f.Line == 0 {
		t.Errorf("находка не называет строку — по ней нельзя дойти до места")
	}
	if f.Value != wantValue {
		t.Errorf("находка называет значение %q, ожидалось %q", f.Value, wantValue)
	}
	if !strings.Contains(f.String(), wantValue) || !strings.Contains(f.String(), "отвергает") {
		t.Errorf("текст находки не восстанавливает следующий шаг: %s", f.String())
	}
}

// TestDocsRefusedValueInjection_EnumValueNamedWithoutTheRefusal — форма 1:
// значение перечисления названо, отказ не проговорён. Ровно дефект kacho#1642.
func TestDocsRefusedValueInjection_EnumValueNamedWithoutTheRefusal(t *testing.T) {
	s := newRefusedValueStand(t)
	got := s.injectPage(t, "services/probe/docs/content/api/instance.mdx",
		"Род инстанса — `VM` или `CONTAINER`.\n")
	requireRefusedFinding(t, got, "services/probe/docs/content/api/instance.mdx", "CONTAINER")
}

// TestDocsRefusedValueInjection_DottedValueNamedWithoutTheRefusal — форма 2:
// точечный дискриминатор назван, отказ не проговорён.
func TestDocsRefusedValueInjection_DottedValueNamedWithoutTheRefusal(t *testing.T) {
	s := newRefusedValueStand(t)
	got := s.injectPage(t, "services/probe/docs/content/api/boot.mdx",
		"Источник загрузки: `storage.image` либо `registry.image`.\n")
	requireRefusedFinding(t, got, "services/probe/docs/content/api/boot.mdx", "registry.image")
}

// TestDocsRefusedValueInjection_WrappedMessageCounts — сообщение отказа,
// ПЕРЕНЕСЁННОЕ по строкам, обязано засчитываться. Без свёртки пробелов гейт
// краснел бы на верной странице — а такую проверку отключают первой.
func TestDocsRefusedValueInjection_WrappedMessageCounts(t *testing.T) {
	s := newRefusedValueStand(t)
	got := s.injectPage(t, "services/probe/docs/content/api/instance.mdx",
		"Род `CONTAINER` отвергается: `INVALID_ARGUMENT \"instanceKind CONTAINER is not\n"+
			"creatable yet: a registry image has no durable address today\"`.\n")
	if len(got) != 0 {
		t.Fatalf("перенос сообщения по строкам объявлен нарушением: %v", got)
	}
}

// TestDocsRefusedValueInjection_WholeWordOnly — `CONTAINER` внутри
// `containerSpec` упоминанием значения не является.
func TestDocsRefusedValueInjection_WholeWordOnly(t *testing.T) {
	s := newRefusedValueStand(t)
	got := s.injectPage(t, "services/probe/docs/content/api/spec.mdx",
		"Среда исполнения — `CONTAINERD`; конфигурация рода — `containerSpec`.\n")
	if len(got) != 0 {
		t.Fatalf("подстрока внутри другого имени принята за упоминание значения: %v", got)
	}
}

// TestDocsRefusedValueInjection_EmptyWalkIsVisible — обход, которому нечего
// читать, обязан быть ОТЛИЧИМ от обхода без находок.
func TestDocsRefusedValueInjection_EmptyWalkIsVisible(t *testing.T) {
	s := newRefusedValueStand(t)
	var log strings.Builder
	findings, census, err := AuditDocsRefusedValue(DocsRefusedValueOptions{
		Root: s.root,
		Services: []DocsRefusedValueService{
			{Name: "probe", CodeDir: "services/probe/internal", DocsDir: "services/probe/docs/nothing-here"},
		},
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("на пустом обходе найдено %d", len(findings))
	}
	if census.DocFiles != 0 || census.Judged != 0 {
		t.Fatalf("пустой обход отчитался как непустой: страниц %d, рассужено %d",
			census.DocFiles, census.Judged)
	}
}
