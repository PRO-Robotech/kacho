// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outsourced_image_pin_agrees_with_module_pin_injection_test.go — доказательство
// падучести соседа: он СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Инъекция подаёт вход РЕШЕНИЮ (`outsourcedPinDisagreements`, `tagNamesVersion`),
// а не подделывает дерево: решение вынесено чистой функцией ровно затем.
//
// У КАЖДОЙ оси — законный близнец. Ось без него доказывает, что проверка умеет
// краснеть, и не доказывает, что она различает: гейт, краснеющий на верном коде,
// отключают первым.
//
// ОДНО-ФАКТНОСТЬ ДЕЛЬТЫ. Дефектный вход отличается от близнеца РОВНО ОДНИМ
// названным фактом — тегом либо версией, — и никогда двумя: иначе неизвестно,
// который дал красное.
package deploy_test

import (
	"strings"
	"testing"
)

// TestOutsourcedPinPredicateCutsBothWays — решение по настоящему входу.
func TestOutsourcedPinPredicateCutsBothWays(t *testing.T) {
	// База — пара, в которой согласны обе стороны. Каждый случай ниже меняет
	// в ней ОДНО поле.
	base := outsourcedPin{
		Stand: "dev", Component: "kaname.image",
		Repo: "docker.io/prorobotech/kaname", Tag: "v0.4.0",
		Module: "github.com/PRO-Robotech/kaname", Version: "v0.4.0",
	}
	withTag := func(tag string) outsourcedPin { p := base; p.Tag = tag; return p }
	withVersion := func(v string) outsourcedPin { p := base; p.Version = v; return p }

	cases := []struct {
		name    string
		pin     outsourcedPin
		finding bool
	}{
		{"версия выпуска названа тегом дословно — согласие", base, false},
		{
			"ДЕФЕКТ: тег называет другую ревизию, версия та же",
			withTag("main-efa5d1f3"), true,
		},
		{
			"псевдоверсия и тег с ПОЛНЫМ коротким хешем — согласие",
			func() outsourcedPin {
				p := withVersion("v0.0.0-20260914001934-236058295b3e")
				p.Tag = "main-236058295b3e"
				return p
			}(), false,
		},
		{
			"псевдоверсия и тег с УСЕЧЁННЫМ хешем — согласие: конвейеры тегуют разной длиной",
			func() outsourcedPin {
				p := withVersion("v0.0.0-20260914001934-236058295b3e")
				p.Tag = "main-23605829"
				return p
			}(), false,
		},
		{
			"ДЕФЕКТ: псевдоверсия и тег с ЧУЖИМ хешем",
			func() outsourcedPin {
				p := withVersion("v0.0.0-20260914001934-236058295b3e")
				p.Tag = "main-efa5d1f3d63c"
				return p
			}(), true,
		},
		{
			"ДЕФЕКТ: хвост тега короче семи знаков — совпадение перестаёт что-либо доказывать",
			func() outsourcedPin {
				p := withVersion("v0.0.0-20260914001934-236058295b3e")
				p.Tag = "main-236058"
				return p
			}(), true,
		},
		{"ДЕФЕКТ: тег пуст — согласие называть нечем", withTag(""), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := outsourcedPinDisagreements([]outsourcedPin{c.pin})
			switch {
			case c.finding && len(got) == 0:
				t.Errorf("дефект внесён, а находки нет: тег %q против версии %q",
					c.pin.Tag, c.pin.Version)
			case !c.finding && len(got) != 0:
				t.Errorf("законный близнец объявлен находкой: %v", got)
			}
			if !c.finding || len(got) == 0 {
				return
			}
			// НАХОДКА НАЗЫВАЕТ КООРДИНАТУ. Диагностика — часть свойства: находка,
			// называющая симптом вместо места, посылает читателя искать не там.
			for _, want := range []string{c.pin.Component, c.pin.Stand, c.pin.Module, c.pin.Version} {
				if !strings.Contains(got[0], want) {
					t.Errorf("находка не называет %q: %s", want, got[0])
				}
			}
		})
	}
}

// TestOutsourcedPinCensusCountsWhatItRead — перепись отличает «ноль находок» от
// «ноль прочитанного».
//
// Проверка предпосылки у соседа роняет прогон на пустом обходе; здесь
// утверждается вторая половина — что величины переписи и правда считают
// прочитанное, а не объявлены константой.
func TestOutsourcedPinCensusCountsWhatItRead(t *testing.T) {
	pins, census := outsourcedImagePins(t, "..")

	if census.Modules < 2 {
		t.Errorf("модулей продукта в go.mod насчитано %d: дерево пинит как минимум "+
			"фундамент и вынесенную службу — перепись читает не то", census.Modules)
	}
	if census.Pairs != len(pins) {
		t.Errorf("пар к сверке в переписи %d, а отдано %d — перепись расходится с "+
			"тем, что уходит в решение", census.Pairs, len(pins))
	}
	for _, p := range pins {
		if p.Module == "" || p.Version == "" || p.Repo == "" {
			t.Errorf("пара собрана неполной: %+v", p)
		}
	}
}
