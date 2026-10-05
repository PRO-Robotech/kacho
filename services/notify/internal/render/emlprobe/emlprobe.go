// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package emlprobe — разборщик письма для проб рендера notify (NTF1-G03,
// G04, G18, G26): структура MIME, части, ссылки и обращения HTML к ресурсам.
//
// Пакет — оснастка проб, а не рендер: он не строит письмо, а читает уже
// собранное, и рендер его не импортирует. Разбор отделён от проб рендера
// намеренно: его собственные пробы (emlprobe_test.go) исполняются и тогда,
// когда рендера в дереве ещё нет, — сломанный разборщик не может выдать себя
// за отсутствующий рендер.
package emlprobe

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// Part — лист MIME-дерева.
type Part struct {
	ContentType string // медиатип без параметров, нижним регистром
	ContentID   string // Content-ID без угловых скобок; пусто — нет
	Body        []byte // после снятия Content-Transfer-Encoding
}

// Letter — разобранное письмо.
type Letter struct {
	Header   mail.Header
	TopType  string   // медиатип корня
	TopParts []string // медиатипы непосредственных частей корня, по порядку
	Text     string   // text/plain-часть
	HTML     string   // text/html-часть
	Inline   []Part   // части multipart/related, кроме HTML
	TextN    int      // сколько text/plain-частей найдено
	HTMLN    int      // сколько text/html-частей найдено
}

// Parse разбирает письмо. Корень — multipart/alternative из text/plain и
// text/html; HTML-часть может стоять внутри multipart/related вместе с
// картинками (З25). Иная форма — ошибка с её описанием.
func Parse(raw []byte) (*Letter, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("emlprobe: заголовок письма не разбирается: %w", err)
	}
	l := &Letter{Header: msg.Header}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("emlprobe: Content-Type корня не разбирается: %w", err)
	}
	l.TopType = mt
	if mt != "multipart/alternative" {
		return l, fmt.Errorf("emlprobe: корень %q, ожидался multipart/alternative", mt)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		p, err := mr.NextRawPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return l, fmt.Errorf("emlprobe: часть корня не читается: %w", err)
		}
		pt, pparams, err := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if err != nil {
			return l, fmt.Errorf("emlprobe: Content-Type части не разбирается: %w", err)
		}
		l.TopParts = append(l.TopParts, pt)
		switch pt {
		case "text/plain":
			b, err := decodeBody(p.Header.Get("Content-Transfer-Encoding"), p)
			if err != nil {
				return l, err
			}
			l.Text = string(b)
			l.TextN++
		case "text/html":
			b, err := decodeBody(p.Header.Get("Content-Transfer-Encoding"), p)
			if err != nil {
				return l, err
			}
			l.HTML = string(b)
			l.HTMLN++
		case "multipart/related":
			if err := l.readRelated(p, pparams["boundary"]); err != nil {
				return l, err
			}
		default:
			return l, fmt.Errorf("emlprobe: часть корня %q вне формы multipart/alternative{text/plain, text/html}", pt)
		}
	}
	return l, nil
}

func (l *Letter) readRelated(r io.Reader, boundary string) error {
	mr := multipart.NewReader(r, boundary)
	for {
		p, err := mr.NextRawPart()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("emlprobe: часть multipart/related не читается: %w", err)
		}
		pt, _, err := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if err != nil {
			return fmt.Errorf("emlprobe: Content-Type части related не разбирается: %w", err)
		}
		b, err := decodeBody(p.Header.Get("Content-Transfer-Encoding"), p)
		if err != nil {
			return err
		}
		if pt == "text/html" {
			l.HTML = string(b)
			l.HTMLN++
			continue
		}
		cid := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(p.Header.Get("Content-ID")), "<"), ">")
		l.Inline = append(l.Inline, Part{ContentType: pt, ContentID: cid, Body: b})
	}
}

func decodeBody(cte string, r io.Reader) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "", "7bit", "8bit", "binary":
		return io.ReadAll(r)
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(r))
	case "base64":
		return io.ReadAll(base64.NewDecoder(base64.StdEncoding, newlineStripper{r}))
	}
	return nil, fmt.Errorf("emlprobe: Content-Transfer-Encoding %q вне перечня", cte)
}

type newlineStripper struct{ r io.Reader }

func (n newlineStripper) Read(p []byte) (int, error) {
	buf := make([]byte, len(p))
	k, err := n.r.Read(buf)
	j := 0
	for _, c := range buf[:k] {
		if c != '\r' && c != '\n' {
			p[j] = c
			j++
		}
	}
	return j, err
}

// DecodeHeader декодирует слова RFC 2047 значения заголовка.
func DecodeHeader(v string) (string, error) {
	return new(mime.WordDecoder).DecodeHeader(v)
}

// IsASCII — значение без байт вне ASCII (сырое значение заголовка по RFC 5322).
func IsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// HasEncodedWord — есть ли в сыром значении слово RFC 2047.
func HasEncodedWord(s string) bool {
	ls := strings.ToLower(s)
	return strings.Contains(ls, "=?utf-8?q?") || strings.Contains(ls, "=?utf-8?b?")
}

// Ref — обращение HTML к ресурсу: то, что почтовый клиент загрузит сам.
type Ref struct {
	Element string // имя элемента
	Attr    string // атрибут либо "style" / "<style>"
	Value   string
}

// Anchor — элемент <a>.
type Anchor struct {
	Href string
	Text string
}

// Census — знаменатель разбора HTML.
type Census struct {
	Elements int // элементов обойдено
	Refs     int // обращений к ресурсам найдено (в том числе cid:)
}

// resourceAttrs — атрибуты, значение которых клиент загружает без действия
// читателя. <a href> — переход по нажатию, не загрузка; <link href> —
// загрузка (таблица стилей, значок).
var resourceAttrs = map[string]bool{
	"src": true, "srcset": true, "background": true, "poster": true,
	"data": true, "lowsrc": true, "dynsrc": true, "longdesc": true,
}

// Inspect обходит HTML: обращения к ресурсам, элементы <a>, элементы
// <style> и <link>. Внешним считается обращение, значение которого не
// начинается с «cid:».
func Inspect(doc string) (refs []Ref, anchors []Anchor, styleElems int, c Census, err error) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return nil, nil, 0, c, fmt.Errorf("emlprobe: HTML не разбирается: %w", err)
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			c.Elements++
			switch n.Data {
			case "style":
				styleElems++
				refs = append(refs, Ref{Element: "style", Attr: "<style>", Value: textOf(n)})
			case "link":
				styleElems++
			case "a":
				anchors = append(anchors, Anchor{Href: attr(n, "href"), Text: strings.TrimSpace(textOf(n))})
			}
			for _, a := range n.Attr {
				k := strings.ToLower(a.Key)
				switch {
				case resourceAttrs[k]:
					refs = append(refs, Ref{Element: n.Data, Attr: k, Value: strings.TrimSpace(a.Val)})
				case k == "href" && n.Data == "link":
					refs = append(refs, Ref{Element: n.Data, Attr: k, Value: strings.TrimSpace(a.Val)})
				case k == "style":
					for _, u := range cssURLs(a.Val) {
						refs = append(refs, Ref{Element: n.Data, Attr: "style", Value: u})
					}
				}
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(root)
	c.Refs = len(refs)
	return refs, anchors, styleElems, c, nil
}

// External — обращения не по cid:. Содержимое <style> внешним считается
// всегда: стили письма — только inline (З25).
func External(refs []Ref) []Ref {
	var out []Ref
	for _, r := range refs {
		if r.Attr == "<style>" || !strings.HasPrefix(strings.ToLower(r.Value), "cid:") {
			out = append(out, r)
		}
	}
	return out
}

// InlineStyled — сколько элементов несут атрибут style.
func InlineStyled(doc string) (int, error) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return 0, err
	}
	n := 0
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.ElementNode && attr(x, "style") != "" {
			n++
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(root)
	return n, nil
}

// TextContent — текст документа HTML (узлы текста подряд, сущности раскрыты).
func TextContent(doc string) (string, error) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return "", err
	}
	return textOf(root), nil
}

func cssURLs(style string) []string {
	var out []string
	ls := strings.ToLower(style)
	for i := strings.Index(ls, "url("); i >= 0; {
		rest := style[i+4:]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			out = append(out, strings.Trim(strings.TrimSpace(rest), `'"`))
			break
		}
		out = append(out, strings.Trim(strings.TrimSpace(rest[:end]), `'"`))
		next := strings.Index(ls[i+4+end:], "url(")
		if next < 0 {
			break
		}
		i = i + 4 + end + next
	}
	if strings.Contains(ls, "@import") {
		out = append(out, "@import")
	}
	return out
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

// CorelibCorpusDir — каталог фикстуры замороженного корпуса формата шаблонов
// corelib (notify/spec/corpus/v1/<fixture>) по пину go.mod: фикстура берётся
// пином, а не копией (polyrepo, запрет копии).
func CorelibCorpusDir(fixture string) (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/PRO-Robotech/corelib").Output() // #nosec G204 -- argv фиксирован
	if err != nil {
		return "", fmt.Errorf("emlprobe: каталог модуля corelib не найден: %w", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", errors.New("emlprobe: каталог модуля corelib пуст (модуль не скачан)")
	}
	return filepath.Join(dir, "notify", "spec", "corpus", "v1", fixture), nil
}
