// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

// TestUpdateMaskFormParityGateCanFailAndCanStayQuiet — доказательство судьи
// инъекцией В ОБЕ СТОРОНЫ на синтетическом входе: дефект краснеет с координатой,
// законный близнец той же формы молчит.
//
// Без второй половины гейт ловил бы форму, а не существо, и первый же законный
// набор его отключил бы.
func TestUpdateMaskFormParityGateCanFailAndCanStayQuiet(t *testing.T) {
	const head = "package p\n\nfunc UpdateMask(a, b string, k map[string]struct{}) error { return nil }\n\n"

	cases := []struct {
		name    string
		src     string
		want    int // ожидаемых находок
		wantSet int // ожидаемых наборов под судом
	}{
		{
			name: "(+) camelCase без двойника — находка",
			src: head + `var s = map[string]struct{}{"status": {}, "countryCode": {}}
func f() { _ = UpdateMask("m", "x", s) }`,
			want: 1, wantSet: 1,
		},
		{
			name: "(−) обе формы рядом — молчит",
			src: head + `var s = map[string]struct{}{"status": {}, "countryCode": {}, "country_code": {}}
func f() { _ = UpdateMask("m", "x", s) }`,
			want: 0, wantSet: 1,
		},
		{
			name: "(−) вложенное поле обеими формами — молчит",
			src: head + `var s = map[string]struct{}{"infra.hostClasses": {}, "infra.host_classes": {}}
func f() { _ = UpdateMask("m", "x", s) }`,
			want: 0, wantSet: 1,
		},
		{
			name: "(+) вложенное поле одной формой — находка",
			src: head + `var s = map[string]struct{}{"infra.hostClasses": {}}
func f() { _ = UpdateMask("m", "x", s) }`,
			want: 1, wantSet: 1,
		},
		{
			name: "(−) набор той же формы, но в проверку НЕ переданный — не предмет",
			src: head + `var other = map[string]struct{}{"countryCode": {}}
var s = map[string]struct{}{"status": {}}
func f() { _ = UpdateMask("m", "x", s) }`,
			want: 0, wantSet: 1,
		},
		{
			name: "(−) только односложные — многословных нет, судить нечего",
			src: head + `var s = map[string]struct{}{"status": {}, "labels": {}}
func f() { _ = UpdateMask("m", "x", s) }`,
			want: 0, wantSet: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sets, files, calls := CollectMaskSets(map[string]string{"svc/a.go": c.src})
			if files != 1 || calls != 1 {
				t.Fatalf("вход не разобран: файлов %d, вызовов %d — проба судила бы пустоту", files, calls)
			}
			if len(sets) != c.wantSet {
				t.Fatalf("наборов под судом %d, ожидалось %d", len(sets), c.wantSet)
			}
			got, _ := JudgeMaskForms(sets)
			if len(got) != c.want {
				t.Fatalf("находок %d, ожидалось %d: %+v", len(got), c.want, got)
			}
			if c.want > 0 && got[0].File == "" {
				t.Fatalf("находка без координаты — по такой нечего чинить")
			}
		})
	}
}
