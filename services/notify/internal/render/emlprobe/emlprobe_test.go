// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// emlprobe_test.go — пробы самого разборщика: он видит то, что утверждают
// пробы рендера, и молчит на законном близнеце. Синтетика собрана руками, без
// рендера: эти пробы исполняются и до него.
package emlprobe_test

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

// letter — синтетическое письмо формы З25 с HTML-телом html.
func letter(html string) []byte {
	return []byte("From: =?utf-8?q?=D0=9E=D0=B1=D0=BB=D0=B0=D0=BA=D0=BE?= <noreply@example.invalid>\r\n" +
		"To: user@example.invalid\r\n" +
		"Subject: =?utf-8?q?=D0=9F=D1=80=D0=B8=D0=B3=D0=BB=D0=B0=D1=88=D0=B5=D0=BD=D0=B8=D0=B5?=\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"b1\"\r\n" +
		"\r\n" +
		"--b1\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"=D0=81=D0=BB=D0=BA=D0=B0 https://console.example.invalid/x\r\n" +
		"--b1\r\n" +
		"Content-Type: multipart/related; boundary=\"b2\"\r\n" +
		"\r\n" +
		"--b2\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		b64(html) + "\r\n" +
		"--b2\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-ID: <logo@kacho>\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"iVBORw0KGgo=\r\n" +
		"--b2--\r\n" +
		"--b1--\r\n")
}

// b64 — base64 строками по 76 символов, как в письме.
func b64(s string) string {
	e := base64.StdEncoding.EncodeToString([]byte(s))
	var out []string
	for len(e) > 76 {
		out = append(out, e[:76])
		e = e[76:]
	}
	return strings.Join(append(out, e), "\r\n")
}

const lawfulHTML = `<html><body><table style="width:100%"><tr><td style="color:#000">` +
	`<img src="cid:logo@kacho" alt="">&lt;a href=x&gt;Y&lt;/a&gt;` +
	`<a href="https://console.example.invalid/x" style="background:#06c">Open</a></td></tr></table></body></html>`

func TestEmlProbeParsesTheLetterShape(t *testing.T) {
	l, err := emlprobe.Parse(letter(lawfulHTML))
	if err != nil {
		t.Fatalf("законное письмо не разобрано: %v", err)
	}
	if l.TopType != "multipart/alternative" || strings.Join(l.TopParts, ",") != "text/plain,multipart/related" {
		t.Fatalf("структура %s %v", l.TopType, l.TopParts)
	}
	if l.TextN != 1 || l.HTMLN != 1 || len(l.Inline) != 1 || l.Inline[0].ContentID != "logo@kacho" {
		t.Fatalf("части: text %d html %d inline %+v", l.TextN, l.HTMLN, l.Inline)
	}
	if !strings.Contains(l.Text, "Ёлка") || !strings.Contains(l.HTML, "cid:logo@kacho") {
		t.Fatalf("тела не декодированы: text %q html %q", l.Text, l.HTML)
	}
	raw := l.Header.Get("Subject")
	if !emlprobe.IsASCII(raw) || !emlprobe.HasEncodedWord(raw) {
		t.Fatalf("сырой Subject %q", raw)
	}
	if s, err := emlprobe.DecodeHeader(raw); err != nil || s != "Приглашение" {
		t.Fatalf("Subject декодирован %q, %v", s, err)
	}
	t.Logf("emlprobe: законное письмо разобрано: корень %s, части %v, inline %d", l.TopType, l.TopParts, len(l.Inline))
}

func TestEmlProbeRefusesAForeignShape(t *testing.T) {
	raw := strings.Replace(string(letter(lawfulHTML)), "multipart/alternative", "multipart/mixed", 1)
	if _, err := emlprobe.Parse([]byte(raw)); err == nil {
		t.Fatal("корень multipart/mixed разобран как законная форма")
	}
}

// TestEmlProbeFindsEachExternalResourceForm — инъекции: каждая форма
// внешнего обращения находится; близнец (cid: и <a href>) — молчание.
func TestEmlProbeFindsEachExternalResourceForm(t *testing.T) {
	refs, anchors, styles, c, err := emlprobe.Inspect(lawfulHTML)
	if err != nil {
		t.Fatal(err)
	}
	if ext := emlprobe.External(refs); len(ext) != 0 || styles != 0 {
		t.Fatalf("близнец: внешних %v, style/link %d", ext, styles)
	}
	if c.Refs != 1 || len(anchors) != 1 || anchors[0].Href != "https://console.example.invalid/x" || anchors[0].Text != "Open" {
		t.Fatalf("близнец: обращений %d, ссылки %+v", c.Refs, anchors)
	}
	txt, err := emlprobe.TextContent(lawfulHTML)
	if err != nil || !strings.Contains(txt, "<a href=x>Y</a>") {
		t.Fatalf("экранированный текст не раскрыт: %q %v", txt, err)
	}
	inj := map[string]string{
		"img http":     `<img src="http://other.example.invalid/p.png">`,
		"img https":    `<img src="https://other.example.invalid/p.png">`,
		"img relative": `<img src="/p.png">`,
		"srcset":       `<img src="cid:a" srcset="https://other.example.invalid/p.png 2x">`,
		"background":   `<table><tr><td background="https://other.example.invalid/b.png"></td></tr></table>`,
		"style url":    `<div style="background-image:url('https://other.example.invalid/b.png')"></div>`,
		"style import": `<div style="@import 'x'"></div>`,
		"link":         `<link rel="stylesheet" href="https://other.example.invalid/s.css">`,
		"style elem":   `<style>p{color:red}</style>`,
		"iframe":       `<iframe src="https://other.example.invalid/"></iframe>`,
	}
	for name, frag := range inj {
		refs, _, _, _, err := emlprobe.Inspect("<html><body>" + frag + "</body></html>")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(emlprobe.External(refs)) == 0 {
			t.Errorf("инъекция %q не найдена: обращения %+v", name, refs)
		}
	}
	n, err := emlprobe.InlineStyled(lawfulHTML)
	if err != nil || n != 3 {
		t.Fatalf("inline style: %d, %v", n, err)
	}
}

// TestEmlProbeCorpusFixturesAreWhatTheProbesAssume — фикстуры проб рендера
// берутся из замороженного корпуса corelib по пину; здесь проверено, что они
// несут те факты, на которые пробы опираются. Отказ здесь — сломанная
// фикстура, а не отсутствующий рендер.
func TestEmlProbeCorpusFixturesAreWhatTheProbesAssume(t *testing.T) {
	load := func(fixture string) spec.Template {
		dir, err := emlprobe.CorelibCorpusDir(fixture)
		if err != nil {
			t.Fatal(err)
		}
		cat, census, err := spec.LoadFS(os.DirFS(dir), ".")
		if err != nil {
			t.Fatalf("фикстура %s не проходит валидатор: %v", fixture, err)
		}
		if len(cat.Templates) != 1 {
			t.Fatalf("фикстура %s: шаблонов %d (census %+v)", fixture, len(cat.Templates), census)
		}
		return cat.Templates[0]
	}
	inv := load("invite")
	if inv.Name != "invite" || inv.Class != spec.ClassSecurity || len(inv.Attrs) != 2 {
		t.Fatalf("invite: %s %s attrs %d", inv.Name, inv.Class, len(inv.Attrs))
	}
	if inv.Subject["ru"] != "Приглашение в облако" {
		t.Fatalf("invite: тема ru %q", inv.Subject["ru"])
	}
	opt := load("optional-when")
	whens := 0
	for _, b := range opt.Bodies {
		for _, bl := range b.Blocks {
			if bl.When != "" {
				whens++
			}
		}
	}
	if opt.Name != "probe-opt" || opt.Class != spec.ClassNotice || whens != 4 {
		t.Fatalf("probe-opt: %s %s блоков с when %d", opt.Name, opt.Class, whens)
	}
	all := load("blocks-all")
	kinds := map[spec.BlockKind]bool{}
	for _, b := range all.Bodies {
		for _, bl := range b.Blocks {
			kinds[bl.Kind] = true
		}
	}
	for _, k := range spec.BlockKinds() {
		if !kinds[k] {
			t.Errorf("probe-all: вида блока %s нет", k)
		}
	}
	t.Logf("фикстуры корпуса: invite (%s), probe-opt (when %d), probe-all (видов блока %d из %d)",
		inv.Class, whens, len(kinds), len(spec.BlockKinds()))
}
