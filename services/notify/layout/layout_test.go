// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package layout

import (
	"strings"
	"testing"
)

// Вид блока вне набора макета — отказ, а не пропуск: шаблоны макета молча
// не вывели бы такой блок. Близнец — тот же блок известного вида.
func TestRender_UnknownKindIsRefused(t *testing.T) {
	if _, _, err := Render(Letter{Blocks: []Block{{Kind: "table", Text: "x"}}}); err == nil {
		t.Fatal("блок вида table выведен без отказа")
	}
	if _, _, err := Render(Letter{Blocks: []Block{{Kind: KindP, Text: "x"}}}); err != nil {
		t.Fatalf("близнец p: %v", err)
	}
}

// Каждый вид набора выводит своё содержимое в обе части: макет не теряет
// ни одного вида блока.
func TestRender_EveryKindReachesBothParts(t *testing.T) {
	blocks := []Block{
		{Kind: KindHeading, Text: "h-1"},
		{Kind: KindP, Text: "p-2"},
		{Kind: KindWarning, Text: "w-3"},
		{Kind: KindCode, Text: "c-4"},
		{Kind: KindList, Items: []string{"i-5", "i-6"}},
		{Kind: KindKV, Pairs: []Pair{{Key: "k-7", Value: "v-8"}}},
		{Kind: KindButton, Label: "b-9", Href: "https://console.example.invalid/x"},
		{Kind: KindDivider},
	}
	kinds := map[Kind]bool{}
	for _, b := range blocks {
		kinds[b.Kind] = true
	}
	for _, k := range []Kind{KindHeading, KindP, KindButton, KindCode, KindList, KindKV, KindWarning, KindDivider} {
		if !kinds[k] || !k.known() {
			t.Fatalf("вид %s без блока пробы либо вне набора", k)
		}
	}
	html, text, err := Render(Letter{Lang: "ru", Title: "t", Brand: "brand", Blocks: blocks})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"h-1", "p-2", "w-3", "c-4", "i-5", "i-6", "k-7", "v-8", "b-9", "https://console.example.invalid/x", "brand"} {
		if !strings.Contains(html, want) || !strings.Contains(text, want) {
			t.Errorf("%q выведено не в обе части", want)
		}
	}
	if !strings.Contains(html, "<hr ") || !strings.Contains(text, "----") {
		t.Error("разделитель выведен не в обе части")
	}
}
