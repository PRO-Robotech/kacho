// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1" // регистрирует дескрипторы contract-а compute
)

// Доказательство способности гейта упасть И смолчать. Вход — НАСТОЯЩИЙ дефект из
// дерева (#1724) и НАСТОЯЩИЕ законные близнецы оттуда же, а не выдуманные формы:
// распознаватель, доказанный синтетикой собственного сочинения, доказывает лишь
// то, что автор помнил.

// ct3ComputeInjCorpus — корпус из одного файла с телом функции.
func ct3ComputeInjCorpus(body string) map[string]string {
	return map[string]string{
		"services/compute/internal/apps/kacho/api/instance/inj.go": "package instance\n\nfunc v() error {\n" + body + "\n\treturn nil\n}\n",
	}
}

func ct3ComputeInjAudit(t *testing.T, body string) ([]ct3ComputeFinding, ct3ComputeCensus) {
	t.Helper()
	idx, contractFields := ct3ComputeChildIndex()
	if contractFields == 0 {
		t.Fatal("предпосылка инъекции неверна: индекс подполей пуст")
	}
	return ct3ComputeAuditRefusalSubjects(ct3ComputeInjCorpus(body), idx)
}

// (а) ВОЗВРАЩЁННЫЙ ДЕФЕКТ — дословно та строка, что стояла в дереве до #1724.
// Гейт обязан покраснеть И НАЗВАТЬ КООРДИНАТУ: находка, называющая симптом вместо
// места, посылает читателя искать не там.
func TestCt3ComputeInj_ParentDeclaredWhileTextNamesChildren_IsFound(t *testing.T) {
	findings, cen := ct3ComputeInjAudit(t, `	return serviceerr.InvalidArg("boot_source",
		"bootSource name/resolvedDigest/materializedVolume/imageKind are output-only and must not be set on input")`)
	if len(findings) != 1 {
		t.Fatalf("возвращённый дефект НЕ найден: находок %d, перепись %+v", len(findings), cen)
	}
	f := findings[0]
	if f.Field != "boot_source" {
		t.Errorf("находка не назвала объявленное поле: %q", f.Field)
	}
	if f.Line == 0 || !strings.HasSuffix(f.File, "inj.go") {
		t.Errorf("находка не назвала координату: %s:%d", f.File, f.Line)
	}
	// Названы ВСЕ четыре подполя — иначе находка описывает дефект уже.
	if got := strings.Join(f.Children, ","); got != "imageKind,materializedVolume,name,resolvedDigest" {
		t.Errorf("находка перечислила подполя неполно: %v", f.Children)
	}
}

// (б) ЗАКОННЫЙ БЛИЗНЕЦ — тот же предмет в починенной форме. Гейт обязан молчать,
// иначе он ловит форму, а не существо, и первый же ложный срабат его отключит.
func TestCt3ComputeInj_ChildDeclaredAndTextAboutThatChild_IsSilent(t *testing.T) {
	findings, cen := ct3ComputeInjAudit(t, `	return serviceerr.InvalidArgFields(serviceerr.FieldViolation{})`+"\n"+
		`	_ = serviceerr.InvalidArg("boot_source.image_kind", "bootSource.imageKind is output-only and must not be set on input")`)
	if len(findings) != 0 {
		t.Fatalf("гейт покраснел на ПОЧИНЕННОЙ форме: %+v (перепись %+v)", findings, cen)
	}
}

// (в) Форма А — точечная запись подполя. Знать надо ОБЕ формы: форма, о которой
// распознаватель не знает, даёт не находку, а невидимость.
func TestCt3ComputeInj_DottedChildFormIsAlsoRead(t *testing.T) {
	findings, _ := ct3ComputeInjAudit(t,
		`	return serviceerr.InvalidArg("boot_source", "bootSource.imageKind is output-only and must not be set on input")`)
	if len(findings) != 1 || len(findings[0].Children) != 1 || findings[0].Children[0] != "imageKind" {
		t.Fatalf("точечная форма записи подполя не прочитана: %+v", findings)
	}
}

// (г) ЗАКОННЫЙ БЛИЗНЕЦ — якорь. Настоящая строка обработчика: подполе `address`
// у `primary_v4_address_spec` существует, и слово `address` в тексте есть, но
// ПОДЛЕЖАЩИМ там является родитель. Без якоря это была бы ложная находка.
func TestCt3ComputeInj_ChildNameInProseIsNotASubject(t *testing.T) {
	findings, cen := ct3ComputeInjAudit(t,
		`	add("network_interface_specs.primary_v4_address_spec", "networkInterfaceSpecs[].primaryV4AddressSpec is not supported: the address is allocated by the subnet's IPAM, compute cannot pin a requested one")`)
	if cen.Judgeable != 1 {
		t.Fatalf("предпосылка близнеца неверна: поле обязано иметь подполя, судимых %d", cen.Judgeable)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт принял подполе в ПРОЗЕ за подлежащее: %+v", findings)
	}
}

// (д) ЗАКОННЫЕ БЛИЗНЕЦЫ — значения перечисления и значение с точкой. Оба стоят
// сразу за именем поля, то есть форму B удовлетворяют; исключает их то, что
// объявленное поле СКАЛЯРНОЕ и подполей не имеет вовсе.
func TestCt3ComputeInj_EnumValueAfterScalarFieldIsNotAChild(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"значение перечисления", `	return serviceerr.InvalidArg("instance_kind", "instanceKind CONTAINER is not creatable yet: a registry image has no durable address today")`},
		{"значение с точкой", `	return serviceerr.InvalidArg("boot_source.type", "bootSource.type registry.image is not accepted yet: a registry image has no durable address today")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, cen := ct3ComputeInjAudit(t, tc.body)
			if cen.Refusals != 1 {
				t.Fatalf("отказ не распознан вовсе, близнец беспредметен: перепись %+v", cen)
			}
			if len(findings) != 0 {
				t.Fatalf("гейт принял ЗНАЧЕНИЕ за подполе: %+v", findings)
			}
		})
	}
}

// (е) Перепись обязана двигаться вместе с охватом: «судимых 0» означает, что
// правило не применялось ни разу, и молчание тогда ничего не значит.
func TestCt3ComputeInj_CensusSeparatesReadFromJudgeable(t *testing.T) {
	_, cen := ct3ComputeInjAudit(t,
		`	_ = serviceerr.InvalidArg("project_id", "projectId is required")`+"\n"+
			`	return serviceerr.InvalidArg("boot_source", "bootSource is required")`)
	if cen.Refusals != 2 {
		t.Fatalf("прочитано не два отказа, а %d", cen.Refusals)
	}
	if cen.Judgeable != 1 {
		t.Fatalf("судимым обязан быть ровно один (project_id — скаляр, boot_source — сообщение), получено %d", cen.Judgeable)
	}
}
