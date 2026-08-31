// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_delivery_flows_declared_once_injection_test.go — доказательство того,
// что MAIL-18 СПОСОБЕН упасть, и падает ТОЛЬКО на своём предмете.
//
// Прогонов четыре: контроль · инъекция потока подтверждения · инъекция почтовой
// полосы · законный близнец. Близнец обязателен: без него гейт ловил бы «любой
// ключ потока в профиле», а не «поток, ДОСТАВЛЯЕМЫЙ ПИСЬМОМ», — и краснел бы на
// исправном дереве, где профили законно называют потоки, письмом не
// доставляемые.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deliveryFlowFixture — копия зонтичного чарта под инъекцию.
type deliveryFlowFixture struct{ root string }

func newDeliveryFlowFixture(t *testing.T) deliveryFlowFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "umbrella")
	copyTree(t, umbrellaDir, root)
	return deliveryFlowFixture{root: root}
}

func (f deliveryFlowFixture) run(t *testing.T) []string {
	t.Helper()
	return deliveryFlowFindings(t, f.root)
}

// inject вставляет строки в блок потоков профиля стенда. Якорь обязан быть
// ЕДИНСТВЕННЫМ: правка по неоднозначному якорю сядет в первое вхождение,
// условия инъекции может не создать, и зелёный прогон означал бы «дефект не
// воспроизведён», а не «гейт исправен».
func (f deliveryFlowFixture) inject(t *testing.T, profile, anchor, added string) {
	t.Helper()
	path := filepath.Join(f.root, profile)
	raw := readFileForTest(t, path)
	if n := strings.Count(raw, anchor); n != 1 {
		t.Fatalf("якорь инъекции %q встречается в %s %d раз, а нужен ровно один — "+
			"иначе условие инъекции может не создаться, и зелёное будет означать "+
			"«дефект не воспроизведён», а не «гейт исправен»", anchor, profile, n)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(raw, anchor, added+anchor, 1)), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDeliveryFlowGateFailsOnAReturnedDefect(t *testing.T) {
	const profile = "values.dev.yaml"
	// Якорь — начало блока потоков профиля стенда; в дереве он единственный.
	const anchor = "        flows:\n          registration:\n"

	t.Run("контроль: сведённое дерево — молчание", func(t *testing.T) {
		f := newDeliveryFlowFixture(t)
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт краснеет на НЕТРОНУТОЙ копии — его находки не про инъекцию, "+
				"и прогоны ниже недействительны:\n%s", strings.Join(found, "\n"))
		}
	})

	t.Run("инъекция: профиль снова высказывается о подтверждении", func(t *testing.T) {
		f := newDeliveryFlowFixture(t)
		f.inject(t, profile, anchor, "        flows:\n          verification:\n            enabled: false\n")
		found := f.run(t)
		if len(found) == 0 {
			t.Fatalf("возвращённое второе мнение о потоке подтверждения гейт НЕ нашёл — " +
				"то есть #1234 воспроизводится, а гейт остаётся зелёным")
		}
		joined := strings.Join(found, "\n")
		// Находка обязана называть КООРДИНАТУ: находка без имени стенда и
		// профиля посылает читателя искать не там, на неё тратят прогон, а
		// потом снимают гейт как непонятный.
		for _, want := range []string{"dev", profile, "verification"} {
			if !strings.Contains(joined, want) {
				t.Errorf("находка не называет %q — диагностика есть часть свойства, "+
					"а не украшение:\n%s", want, joined)
			}
		}
	})

	t.Run("инъекция: профиль снова высказывается о почтовой полосе", func(t *testing.T) {
		f := newDeliveryFlowFixture(t)
		f.inject(t, profile, anchor, "        courier:\n          smtp:\n            connection_uri: \"smtp://elsewhere.invalid:25/\"\n")
		if found := f.run(t); len(found) == 0 {
			t.Errorf("возвращённое второе мнение о почтовой полосе гейт НЕ нашёл")
		}
	})

	t.Run("близнец: поток, письмом НЕ доставляемый, — молчание", func(t *testing.T) {
		// `settings` объявляется профилями законно и письмом не доставляется.
		// Покраснев на нём, гейт стал бы красным на исправном дереве — а такой
		// снимают первым, и вместе с ним уходит требование.
		f := newDeliveryFlowFixture(t)
		f.inject(t, profile, anchor, "        flows:\n          settings:\n            ui_url: \"/settings\"\n")
		if found := f.run(t); len(found) > 0 {
			t.Errorf("гейт покраснел на потоке, который письмом НЕ доставляется, — он "+
				"ловит форму («ключ потока в профиле»), а не существо («поток, "+
				"доставляемый письмом»):\n%s", strings.Join(found, "\n"))
		}
	})
}
