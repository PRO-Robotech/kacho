// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// when_g26_test.go — УК59 (CX1-51 (б)): блок с when выводится по
// ПРОВЕРЕННОМУ набору, а места ссылок — по spec.RefSites. Сторона рендера
// NTF1-G26 (а) и её близнеца; клетки (б)–(д) — исход строки в deliver
// (полоса N3), здесь не судятся.
package render

import (
	"errors"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/notify/form"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

func probeOptLetter(t *testing.T, raw map[string]any) Letter {
	t.Helper()
	tpl := corpusTemplate(t, "optional-when")
	return Letter{
		Namespace: "probe",
		RowID:     testRowID,
		Template:  tpl,
		Locale:    "ru",
		Attrs:     verified(t, tpl, raw),
		To:        mustNormalize(t, "user@example.invalid"),
		Date:      testNow,
	}
}

func renderParsed(t *testing.T, in Letter) *emlprobe.Letter {
	t.Helper()
	out, err := newRenderer(t).Render(in)
	if err != nil {
		t.Fatalf("Render(%s, %s): %v", in.Template.Name, in.Locale, err)
	}
	l, err := emlprobe.Parse(out)
	if err != nil {
		t.Fatalf("форма письма: %v", err)
	}
	return l
}

// TestRender_NTF1G26_WhenBlockFollowsVerifiedSet — (а) ключей inviter и
// target в наборе нет: тема «Проба probe»; блоков p{when: inviter} и
// button{when: target} нет; блок p без when есть. Близнец — inviter = i,
// target = /a: в теле «Вас пригласил i.», кнопка ведёт ровно на origin + /a.
func TestRender_NTF1G26_WhenBlockFollowsVerifiedSet(t *testing.T) {
	const (
		whenP       = "Вас пригласил"
		plainP      = "Текст без приглашающего."
		buttonLabel = "Открыть"
	)
	l := renderParsed(t, probeOptLetter(t, map[string]any{"subject_name": "probe"}))
	if s, err := emlprobe.DecodeHeader(l.Header.Get("Subject")); err != nil || s != "Проба probe" {
		t.Errorf("(а) тема %q (%v), ожидалась «Проба probe»", s, err)
	}
	htmlText, _ := emlprobe.TextContent(l.HTML)
	_, anchors, _, _, err := emlprobe.Inspect(l.HTML)
	if err != nil {
		t.Fatal(err)
	}
	for part, body := range map[string]string{"text/plain": l.Text, "text/html": htmlText} {
		if strings.Contains(body, whenP) {
			t.Errorf("(а) %s: блок p{when: inviter} выведен при незаданном inviter", part)
		}
		if !strings.Contains(body, plainP) {
			t.Errorf("(а) %s: блока p без when нет", part)
		}
	}
	for _, a := range anchors {
		if strings.Contains(a.Text, buttonLabel) {
			t.Errorf("(а) кнопка button{when: target} выведена при незаданном target: %q", a.Href)
		}
	}

	tw := renderParsed(t, probeOptLetter(t, map[string]any{"subject_name": "probe", "inviter": "i", "target": "/a"}))
	twText, _ := emlprobe.TextContent(tw.HTML)
	for part, body := range map[string]string{"text/plain": tw.Text, "text/html": twText} {
		if !strings.Contains(body, whenP+" i.") {
			t.Errorf("близнец %s: нет «%s i.»", part, whenP)
		}
	}
	_, twAnchors, _, _, err := emlprobe.Inspect(tw.HTML)
	if err != nil {
		t.Fatal(err)
	}
	var button []string
	for _, a := range twAnchors {
		if strings.Contains(a.Text, buttonLabel) {
			button = append(button, a.Href)
		}
	}
	if len(button) != 1 || button[0] != testOrigin+"/a" {
		t.Errorf("близнец: кнопка %q, ожидалась ровно одна на %q", button, testOrigin+"/a")
	}
}

// TestRender_NTF1G26_ZeroValueUnderWhenIsNotAbsence — ключ в наборе с
// form.Value{} — не «не задано»: набор не прошёл form, письма нет. Блок с
// when решается по проверенному набору, а не по наличию ключа (УК59).
func TestRender_NTF1G26_ZeroValueUnderWhenIsNotAbsence(t *testing.T) {
	r := newRenderer(t)
	in := probeOptLetter(t, map[string]any{"subject_name": "probe"})
	in.Attrs["inviter"] = form.Value{}
	out, err := r.Render(in)
	if !errors.Is(err, form.ErrUnset) || out != nil {
		t.Errorf("inviter = form.Value{} под when: письмо %d байт, ошибка %v — ожидались nil и form.ErrUnset", len(out), err)
	}
	// близнец — один факт: ключа inviter в наборе нет вовсе
	if _, err := r.Render(probeOptLetter(t, map[string]any{"subject_name": "probe"})); err != nil {
		t.Errorf("близнец (ключа inviter нет): %v", err)
	}
}

// TestRender_UK59_EveryBlockKindCarriesItsRefs — шаблон probe-all несёт все
// восемь видов блока spec.BlockKinds(); каждое место ссылки spec.RefSites
// с подстановкой выведено значением набора в обе части письма.
func TestRender_UK59_EveryBlockKindCarriesItsRefs(t *testing.T) {
	tpl := corpusTemplate(t, "blocks-all")
	vals := map[string]any{
		"subject_name": "probe-subj",
		"note":         "note-7f3a",
		"code":         "123456",
		"target":       "/a",
		"token":        testToken,
		"issued_at":    "2026-09-30T00:00:00Z",
	}
	l := renderParsed(t, Letter{
		Namespace: "probe", RowID: testRowID, Template: tpl, Locale: "ru",
		Attrs: verified(t, tpl, vals), To: mustNormalize(t, "user@example.invalid"), Date: testNow,
	})
	htmlText, _ := emlprobe.TextContent(l.HTML)
	_, anchors, _, _, err := emlprobe.Inspect(l.HTML)
	if err != nil {
		t.Fatal(err)
	}
	var body spec.Body
	for _, b := range tpl.Bodies {
		if b.Locale == "ru" {
			body = b
		}
	}
	sites, checked := 0, 0
	for _, b := range body.Blocks {
		for _, s := range spec.RefSites(b) {
			sites++
			if s.Form != spec.SiteText {
				continue
			}
			for _, ref := range s.Refs() {
				if ref == "issued_at" {
					continue // написание отметки времени в письме — не предмет этой пробы
				}
				checked++
				want := vals[ref].(string)
				if !strings.Contains(l.Text, want) || !strings.Contains(htmlText, want) {
					t.Errorf("блок %d (%s) %s: значение %s не выведено в обе части", b.Index, b.Kind, s.Place, ref)
				}
			}
		}
	}
	hrefs := map[string]bool{}
	for _, a := range anchors {
		hrefs[a.Href] = true
	}
	for _, want := range []string{testOrigin + "/a", testOrigin + inviteLiteral + "?token=" + testToken} {
		if !hrefs[want] {
			t.Errorf("кнопки на %q нет; ссылки %v", want, hrefs)
		}
	}
	t.Logf("УК59: блоков %d · мест ссылок %d · текстовых подстановок проверено %d", len(body.Blocks), sites, checked)
	if checked == 0 {
		t.Fatal("подстановок проверено 0 — проба без предмета")
	}
}
