// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// letter_g03_g04_test.go — NTF1-G03 (письмо собрано общим макетом: эталон
// .eml) и NTF1-G04 (значение атрибута с разметкой экранируется, близнец G03).
package render

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

// updateGolden — переписать эталон выводом рендера. Прогон с флагом вердикта
// не даёт: проба падает, назвав переписанный файл, — эталон судит следующий
// прогон без флага и перепись G03/G18 по его структуре.
var updateGolden = flag.Bool("golden.update", false, "переписать эталоны .eml выводом рендера (прогон не вердикт)")

const inviteGolden = goldenRoot + "/kaname/invite.ru.eml"

// TestRender_NTF1G03_InviteLetterEqualsGolden — Then: письмо побайтово
// равно services/notify/testdata/golden/kaname/invite.ru.eml; два рендера
// одного входа равны (часы управляемые, граница частей детерминирована).
func TestRender_NTF1G03_InviteLetterEqualsGolden(t *testing.T) {
	r := newRenderer(t)
	in := inviteLetter(t, testInviter)
	got, err := r.Render(in)
	if err != nil {
		t.Fatalf("Render(kaname/invite, ru): %v", err)
	}
	again, err := r.Render(in)
	if err != nil || !bytes.Equal(got, again) {
		t.Fatalf("два рендера одного входа различаются (err %v): письмо не детерминировано", err)
	}
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(inviteGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(inviteGolden, got, 0o644); err != nil { // #nosec G306 -- эталон дерева
			t.Fatal(err)
		}
		t.Fatalf("эталон %s переписан (%d байт) — прогон с -golden.update вердикта не даёт", inviteGolden, len(got))
	}
	want, err := os.ReadFile(inviteGolden)
	if err != nil {
		t.Fatalf("эталона %s нет: %v", inviteGolden, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("письмо (%d байт) не равно эталону %s (%d байт)", len(got), inviteGolden, len(want))
	}
}

// TestRender_NTF1G03_LetterShape — And: multipart/alternative из text/plain и
// text/html; стили inline; картинки только cid:; тема по RFC 2047; кнопка
// ведёт ровно на origin + /iam/invitations/accept?token=<token>; Message-ID
// по Р14; значение с не-ASCII именем в обеих частях.
func TestRender_NTF1G03_LetterShape(t *testing.T) {
	raw, err := newRenderer(t).Render(inviteLetter(t, testInviter))
	if err != nil {
		t.Fatalf("Render(kaname/invite, ru): %v", err)
	}
	l, err := emlprobe.Parse(raw)
	if err != nil {
		t.Fatalf("форма письма: %v", err)
	}
	if l.TextN != 1 || l.HTMLN != 1 {
		t.Fatalf("частей text/plain %d, text/html %d — ожидалось по одной; корень %s %v", l.TextN, l.HTMLN, l.TopType, l.TopParts)
	}
	for _, p := range l.Inline {
		if !strings.HasPrefix(p.ContentType, "image/") || p.ContentID == "" {
			t.Errorf("часть multipart/related вне формы «картинка с Content-ID»: %s cid %q", p.ContentType, p.ContentID)
		}
	}

	refs, anchors, styleElems, census, err := emlprobe.Inspect(l.HTML)
	if err != nil {
		t.Fatal(err)
	}
	if ext := emlprobe.External(refs); len(ext) != 0 {
		t.Errorf("обращения HTML не по cid: %+v", ext)
	}
	if styleElems != 0 {
		t.Errorf("элементов <style>/<link> %d — стили обязаны быть inline", styleElems)
	}
	if n, err := emlprobe.InlineStyled(l.HTML); err != nil || n == 0 {
		t.Errorf("элементов с inline style %d (%v) — макет без inline-стилей", n, err)
	}
	cids := map[string]bool{}
	for _, p := range l.Inline {
		cids[p.ContentID] = true
	}
	for _, r := range refs {
		if id := strings.TrimPrefix(r.Value, "cid:"); id != r.Value && !cids[id] {
			t.Errorf("cid:%s без части письма", id)
		}
	}

	rawSubject := l.Header.Get("Subject")
	if !emlprobe.IsASCII(rawSubject) || !emlprobe.HasEncodedWord(rawSubject) {
		t.Errorf("Subject %q не закодирован по RFC 2047", rawSubject)
	}
	if s, err := emlprobe.DecodeHeader(rawSubject); err != nil || s != "Приглашение в облако" {
		t.Errorf("тема декодирована %q (%v), ожидалась «Приглашение в облако»", s, err)
	}

	wantButton := testOrigin + inviteLiteral + "?token=" + testToken
	var button []string
	for _, a := range anchors {
		if !strings.HasPrefix(a.Href, testOrigin+"/") {
			t.Errorf("ссылка письма на другой узел: %q", a.Href)
		}
		if strings.Contains(a.Text, "Принять приглашение") {
			button = append(button, a.Href)
		}
	}
	if len(button) != 1 || button[0] != wantButton {
		t.Errorf("кнопка «Принять приглашение»: %q, ожидалась ровно одна на %q", button, wantButton)
	}
	if !strings.Contains(l.Text, wantButton) {
		t.Errorf("текстовая часть не несёт ссылку кнопки %q", wantButton)
	}
	txt, _ := emlprobe.TextContent(l.HTML)
	if !strings.Contains(txt, testInviter) || !strings.Contains(l.Text, testInviter) {
		t.Errorf("значение inviter_name %q не выведено в обе части", testInviter)
	}
	if got, want := l.Header.Get("Message-Id"), expectedMessageID("kaname", testRowID); got != want {
		t.Errorf("Message-ID %q, ожидался %q (Р14)", got, want)
	}
	if d, err := l.Header.Date(); err != nil || !d.Equal(testNow) {
		t.Errorf("Date %v (%v), ожидались управляемые часы %v", d, err, testNow)
	}
	t.Logf("G03: корень %s, части %v, inline %d, элементов HTML %d, обращений к ресурсам %d, ссылок %d",
		l.TopType, l.TopParts, len(l.Inline), census.Elements, census.Refs, len(anchors))
}

// TestRender_NTF1G04_MarkupInAttrIsEscaped — Given условия G03, но
// inviter_name = "<a href=x>Y</a>". Then в HTML-части значение экранировано,
// элементов <a> из атрибута 0; в текстовой — как есть. Близнец — G03: число
// элементов <a> то же.
func TestRender_NTF1G04_MarkupInAttrIsEscaped(t *testing.T) {
	const markup = "<a href=x>Y</a>"
	r := newRenderer(t)
	twinRaw, err := r.Render(inviteLetter(t, testInviter))
	if err != nil {
		t.Fatalf("близнец G03: %v", err)
	}
	raw, err := r.Render(inviteLetter(t, markup))
	if err != nil {
		t.Fatalf("Render с разметкой в inviter_name: %v", err)
	}
	twin, err := emlprobe.Parse(twinRaw)
	if err != nil {
		t.Fatal(err)
	}
	l, err := emlprobe.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, twinAnchors, _, _, err := emlprobe.Inspect(twin.HTML)
	if err != nil {
		t.Fatal(err)
	}
	_, anchors, _, _, err := emlprobe.Inspect(l.HTML)
	if err != nil {
		t.Fatal(err)
	}
	fromAttr := 0
	for _, a := range anchors {
		if a.Href == "x" {
			fromAttr++
		}
	}
	if fromAttr != 0 || len(anchors) != len(twinAnchors) {
		t.Errorf("элементов <a> из атрибута %d; всего %d при %d у близнеца G03", fromAttr, len(anchors), len(twinAnchors))
	}
	if txt, _ := emlprobe.TextContent(l.HTML); !strings.Contains(txt, markup) {
		t.Errorf("в HTML-части значение не выведено текстом (экранированным)")
	}
	if !strings.Contains(l.Text, markup) {
		t.Errorf("в текстовой части значение не «как есть»: %q", l.Text)
	}
	t.Logf("G04: ссылок %d (близнец %d), из атрибута %d", len(anchors), len(twinAnchors), fromAttr)
}
