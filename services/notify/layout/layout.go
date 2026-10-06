// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package layout — общий макет письма notify (З25): одна HTML-вёрстка на
// html/template со стилями inline и табличной раскладкой и текстовая часть из
// тех же блоков.
//
// Макет получает уже разобранные блоки: тексты с выполненными подстановками и
// собранные ссылки. Решений о письме (какой блок выводится, откуда ссылка) он
// не принимает — их принимает рендер (internal/render) по проверенному набору
// атрибутов. Экранирование значений HTML-части — контекстное, html/template;
// текстовая часть выводит значения как есть.
//
// В HTML нет ни одного обращения к ресурсу, который почтовый клиент загрузил
// бы сам: ни картинок, ни таблиц стилей, ни url() в стилях (NTF1-G03). Часть
// multipart/related поэтому не заводится — картинок у макета нет.
package layout

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

// Kind — вид блока макета. Набор совпадает с видами блока тела шаблона
// (spec.BlockKinds); перевод вида шаблона в вид макета — исчерпывающий switch
// рендера.
type Kind string

// Виды блока макета.
const (
	KindHeading Kind = "heading"
	KindP       Kind = "p"
	KindButton  Kind = "button"
	KindCode    Kind = "code"
	KindList    Kind = "list"
	KindKV      Kind = "kv"
	KindWarning Kind = "warning"
	KindDivider Kind = "divider"
)

// Pair — строка блока kv: ключ — литерал шаблона, значение — текст с
// выполненными подстановками.
type Pair struct {
	Key   string
	Value string
}

// Block — блок письма. Поле по виду: Text у heading, p, warning, code; Items
// у list; Pairs у kv; Label и Href у button; у divider полей нет.
type Block struct {
	Kind  Kind
	Text  string
	Items []string
	Pairs []Pair
	Label string
	Href  string
}

// Letter — содержимое письма для макета.
type Letter struct {
	// Lang — локаль тела (атрибут lang документа).
	Lang string
	// Title — тема письма, декодированная (элемент title).
	Title string
	// Brand — имя отправителя установки (строка шапки).
	Brand string
	// Blocks — блоки тела в порядке шаблона.
	Blocks []Block
}

//go:embed letter.html.tmpl letter.txt.tmpl
var files embed.FS

var (
	htmlTmpl = htmltemplate.Must(htmltemplate.New("letter.html.tmpl").ParseFS(files, "letter.html.tmpl"))
	textTmpl = texttemplate.Must(texttemplate.New("letter.txt.tmpl").Option("missingkey=error").ParseFS(files, "letter.txt.tmpl"))
)

// Render собирает HTML-часть и текстовую часть письма. Переводы строк — LF;
// перевод в CRLF и кодирование частей — дело сборщика письма.
func Render(l Letter) (html, text string, err error) {
	for i, b := range l.Blocks {
		if !b.Kind.known() {
			return "", "", fmt.Errorf("layout: блок %d вида %q вне набора макета", i+1, string(b.Kind))
		}
	}
	var hb, tb bytes.Buffer
	if err := htmlTmpl.Execute(&hb, l); err != nil {
		return "", "", fmt.Errorf("layout: HTML-часть: %w", err)
	}
	if err := textTmpl.Execute(&tb, l); err != nil {
		return "", "", fmt.Errorf("layout: текстовая часть: %w", err)
	}
	return hb.String(), tb.String(), nil
}

// known — вид из набора макета. Исчерпывающий switch без default: вид вне
// набора шаблоны макета молча пропустили бы, поэтому он — отказ.
func (k Kind) known() bool {
	switch k {
	case KindHeading, KindP, KindButton, KindCode, KindList, KindKV, KindWarning, KindDivider:
		return true
	}
	return false
}
