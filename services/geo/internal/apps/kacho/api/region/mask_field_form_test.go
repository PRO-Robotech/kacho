// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package region

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
)

// TestUpdateMaskAppliesFieldInEitherFormOfItsName — маска применяет поле
// независимо от того, в какой форме край назвал его имя.
//
// Дефект, ради которого написана: край разбирает `updateMask` через protojson,
// и тот приводит `countryCode` к `country_code`. Проверка маски знала обе формы,
// а ПРИМЕНЕНИЕ сверяло имя дословно — то есть запрос принимался, отвечал
// успехом и не менял ничего. Сквозная проба увидела это как «прочитано RU,
// ожидалось NL» на шаге проверки, то есть на два шага дальше причины.
func TestUpdateMaskAppliesFieldInEitherFormOfItsName(t *testing.T) {
	cases := []struct {
		name   string
		mask   []string
		status domain.GeoStatus // задаётся там, где маска называет status: пустой отвергается по делу
		want   bool             // ожидается ли применение countryCode
	}{
		{"форма контракта (её присылает край)", []string{"country_code"}, domain.GeoStatusUnspecified, true},
		{"форма JSON (её кладёт прямой вызов)", []string{"countryCode"}, domain.GeoStatusUnspecified, true},
		{"обе формы разом", []string{"country_code", "countryCode"}, domain.GeoStatusUnspecified, true},
		// Отрицание в паре с положительными: без него проба зеленела бы на
		// реализации, применяющей поле всегда.
		{"маска называет ДРУГОЕ поле", []string{"status"}, domain.GeoStatusUp, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := (&UseCase{}).buildUpdateParams(UpdateInput{
				CountryCode: "NL",
				Status:      c.status,
				Mask:        c.mask,
			})
			if err != nil {
				t.Fatalf("вход законен, а разбор отказал: %v", err)
			}
			got := p.CountryCode != nil
			if got != c.want {
				t.Fatalf("применение countryCode = %v, ожидалось %v (маска %v) — "+
					"поле объявлено изменяемым, значит форма имени решать не должна",
					got, c.want, c.mask)
			}
			if c.want && *p.CountryCode != "NL" {
				t.Fatalf("применено значение %q вместо NL", *p.CountryCode)
			}
		})
	}
}
