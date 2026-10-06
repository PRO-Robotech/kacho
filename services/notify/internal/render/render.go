// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package render — сборщик письма notify (З25): тема и тело по проверенному
// набору атрибутов, общий макет (services/notify/layout), ссылки и заголовки.
//
// Что здесь решено и почему:
//
//   - Блоки перебираются по spec.RefSites — единственной функции мест ссылок
//     (CX1-51 (а)); блок с when выводится, только если его атрибут в
//     проверенном наборе задан (CX1-51 (б), УК59). Ключ с нулевым
//     form.Value{} — не «не задано»: набор не прошёл form, письма нет.
//   - Ссылка собирается только из form.Path и form.Token сложением строк с
//     origin установки; разрешения относительного адреса нет (CX1-18).
//   - Значение заголовка собирается только из form.HeaderText и кодируется
//     по RFC 2047 (headers.go); From один на установку, Reply-To у письма
//     не выражается ни для какого класса (NTF1-G18).
//   - Message-ID — <hex(sha256(namespace ‖ 0x00 ‖ id))[:32]@домен
//     отправителя> (Р14); Date — момент, переданный вызывающим.
//   - Письмо 7bit: часть, чей текст — ASCII со строками ≤ 998, идёт `7bit`,
//     иначе — quoted-printable; заголовки ASCII, строки заголовков ≤ 78
//     (подпись DKIM берёт окончательные байты и не перекодирует их).
//   - Нулевые значения непрозрачных типов у каждого приёмника — ошибка с
//     причиной form.ErrUnset либо address.ErrUnset (NTF1-B27, B28 (нуль)).
//
// Ошибки пакета называют атрибут и правило, но не значение: значения
// атрибутов — данные адресата, а ошибку рендера пишет журнал исполнителя.
package render

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/form"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/layout"
)

// Сторожа пакета — различимы через errors.Is.
var (
	// ErrOrigin — origin установки вне формы: абсолютный https:// с узлом,
	// без удостоверения, пути, параметров и фрагмента.
	ErrOrigin = errors.New("render: origin вне формы")
	// ErrAddressNotASCII — адрес с не-ASCII локальной частью: в заголовок
	// 7bit-письма без SMTPUTF8 он не записывается.
	ErrAddressNotASCII = errors.New("render: адрес с не-ASCII локальной частью")
	// ErrLocale — у шаблона нет темы либо тела этой локали.
	ErrLocale = errors.New("render: у шаблона нет этой локали")
	// ErrAttrUndeclared — в наборе ключ, которого шаблон не объявляет.
	ErrAttrUndeclared = errors.New("render: атрибут не объявлен шаблоном")
	// ErrAttrKind — вид значения не равен виду, объявленному шаблоном.
	ErrAttrKind = errors.New("render: вид значения не равен объявленному")
	// ErrAttrMissing — обязательного атрибута либо атрибута, на который
	// ссылается выводимое место, в наборе нет.
	ErrAttrMissing = errors.New("render: атрибута нет в наборе")
	// ErrLetter — описание письма неполно: пространство, идентификатор строки,
	// момент письма.
	ErrLetter = errors.New("render: описание письма неполно")
)

// Sender — отправитель установки: ручки notify.smtp.fromName и
// notify.smtp.fromAddress (З20).
type Sender struct {
	Name    form.HeaderText
	Address address.Normalized
}

// Config — то, из чего собирается рендер.
type Config struct {
	// Origin — origin установки (notify.origin): база каждой ссылки письма.
	Origin string
	// From — отправитель установки.
	From Sender
}

// Renderer — сборщик письма одной установки. Неизменяем после New и
// безопасен для одновременного использования.
type Renderer struct {
	origin string
	// from — значение заголовка From, собранное один раз.
	from string
	// brand — имя отправителя (строка шапки макета).
	brand string
	// domain — домен адреса отправителя: правая часть Message-ID.
	domain string
}

// New проверяет origin и отправителя и собирает рендер. Нулевые значения
// отправителя — ошибка с причиной form.ErrUnset либо address.ErrUnset.
func New(cfg Config) (*Renderer, error) {
	if err := CheckOrigin(cfg.Origin); err != nil {
		return nil, err
	}
	brand, err := cfg.From.Name.Value()
	if err != nil {
		return nil, fmt.Errorf("render: имя отправителя: %w", err)
	}
	addr, err := asciiAddress(cfg.From.Address, "адрес отправителя")
	if err != nil {
		return nil, err
	}
	name, err := phrase(brand)
	if err != nil {
		return nil, err
	}
	return &Renderer{
		origin: cfg.Origin,
		from:   name + fold + "<" + addr + ">",
		brand:  brand,
		domain: addr[strings.LastIndexByte(addr, '@')+1:],
	}, nil
}

// Letter — описание письма одной строки ленты.
type Letter struct {
	// Namespace — пространство ленты (модуль источника): часть Message-ID.
	Namespace string
	// RowID — идентификатор строки ленты: часть Message-ID.
	RowID string
	// Template — проверенный шаблон сборки той же ревизии, что строка.
	Template spec.Template
	// Locale — локаль темы и тела.
	Locale string
	// Attrs — проверенный набор: только заданные атрибуты. Optional без
	// значения здесь отсутствует ключом.
	Attrs map[string]form.Value
	// To — адресат, нормализованный address.Normalize.
	To address.Normalized
	// Date — момент письма (заголовок Date).
	Date time.Time
}

// Render собирает письмо. Ошибка — письма нет (nil).
func (r *Renderer) Render(l Letter) ([]byte, error) {
	if l.Namespace == "" || l.RowID == "" {
		return nil, fmt.Errorf("%w: пустое пространство либо идентификатор строки", ErrLetter)
	}
	if l.Date.IsZero() {
		return nil, fmt.Errorf("%w: момент письма не задан", ErrLetter)
	}
	vals, err := checkAttrs(l.Template, l.Attrs)
	if err != nil {
		return nil, err
	}
	to, err := asciiAddress(l.To, "адресат")
	if err != nil {
		return nil, err
	}
	subjectTpl, ok := l.Template.Subject[l.Locale]
	if !ok {
		return nil, fmt.Errorf("%w: тема %s/%s", ErrLocale, l.Template.Name, l.Locale)
	}
	body, ok := bodyOf(l.Template, l.Locale)
	if !ok {
		return nil, fmt.Errorf("%w: тело %s/%s", ErrLocale, l.Template.Name, l.Locale)
	}
	subjectText, err := substitute(subjectTpl, vals, "тема")
	if err != nil {
		return nil, err
	}
	subjectHT, err := form.ParseHeaderText(subjectText)
	if err != nil {
		return nil, fmt.Errorf("render: тема %s/%s вне формы заголовка: %w", l.Template.Name, l.Locale, err)
	}
	subject, err := HeaderValue(subjectHT)
	if err != nil {
		return nil, err
	}
	blocks, err := r.blocks(body, vals)
	if err != nil {
		return nil, err
	}
	htmlPart, textPart, err := layout.Render(layout.Letter{
		Lang: l.Locale, Title: subjectText, Brand: r.brand, Blocks: blocks,
	})
	if err != nil {
		return nil, err
	}
	return assemble(envelope{
		from:    r.from,
		to:      to,
		subject: subject,
		date:    l.Date,
		digest:  rowDigest(l.Namespace, l.RowID),
		domain:  r.domain,
	}, textPart, htmlPart)
}

// checkAttrs судит набор по объявлению шаблона и выдаёт значения строками.
// Порядок обхода — по имени: при нескольких нарушениях ошибка одна и та же.
func checkAttrs(tpl spec.Template, attrs map[string]form.Value) (map[string]string, error) {
	declared := make(map[string]spec.Attr, len(tpl.Attrs))
	for _, a := range tpl.Attrs {
		declared[a.Name] = a
	}
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	vals := make(map[string]string, len(attrs))
	for _, name := range names {
		v := attrs[name]
		a, ok := declared[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s/%s", ErrAttrUndeclared, tpl.Name, name)
		}
		s, err := v.Value()
		if err != nil {
			return nil, fmt.Errorf("render: атрибут %s/%s: %w", tpl.Name, name, err)
		}
		if v.Kind() != a.Kind {
			return nil, fmt.Errorf("%w: %s/%s объявлен %s, значение %s", ErrAttrKind, tpl.Name, name, a.Kind, v.Kind())
		}
		vals[name] = s
	}
	for _, a := range tpl.Attrs {
		if _, ok := vals[a.Name]; !ok && a.Presence == spec.PresenceRequired {
			return nil, fmt.Errorf("%w: обязательный %s/%s", ErrAttrMissing, tpl.Name, a.Name)
		}
	}
	return vals, nil
}

func bodyOf(tpl spec.Template, locale string) (spec.Body, bool) {
	for _, b := range tpl.Bodies {
		if b.Locale == locale {
			return b, true
		}
	}
	return spec.Body{}, false
}

// blocks переводит блоки тела в блоки макета. Места ссылок — spec.RefSites;
// блок с when пропускается, если его атрибута в проверенном наборе нет.
func (r *Renderer) blocks(body spec.Body, vals map[string]string) ([]layout.Block, error) {
	out := make([]layout.Block, 0, len(body.Blocks))
	for _, b := range body.Blocks {
		if b.When != "" {
			if _, given := vals[b.When]; !given {
				continue
			}
		}
		lb, err := r.block(b, vals)
		if err != nil {
			return nil, err
		}
		out = append(out, lb)
	}
	return out, nil
}

// block — один блок. Исчерпывающий switch по spec.BlockKinds без default:
// вид без ветки — ошибка, а не пропуск.
func (r *Renderer) block(b spec.Block, vals map[string]string) (layout.Block, error) {
	sites := spec.RefSites(b)
	where := fmt.Sprintf("блок %d (%s)", b.Index, b.Kind)
	texts := make([]string, len(sites))
	for i, s := range sites {
		if s.Form != spec.SiteText {
			continue
		}
		t, err := substitute(s.Text, vals, where+" "+s.Place)
		if err != nil {
			return layout.Block{}, err
		}
		texts[i] = t
	}
	switch b.Kind {
	case spec.BlockHeading:
		return layout.Block{Kind: layout.KindHeading, Text: texts[0]}, nil
	case spec.BlockP:
		return layout.Block{Kind: layout.KindP, Text: texts[0]}, nil
	case spec.BlockWarning:
		return layout.Block{Kind: layout.KindWarning, Text: texts[0]}, nil
	case spec.BlockCode:
		return layout.Block{Kind: layout.KindCode, Text: texts[0]}, nil
	case spec.BlockList:
		return layout.Block{Kind: layout.KindList, Items: texts}, nil
	case spec.BlockKV:
		pairs := make([]layout.Pair, len(b.Pairs))
		for i, p := range b.Pairs {
			pairs[i] = layout.Pair{Key: p.Key, Value: texts[i]}
		}
		return layout.Block{Kind: layout.KindKV, Pairs: pairs}, nil
	case spec.BlockButton:
		href, err := r.buttonLink(b, sites[0], vals, where)
		if err != nil {
			return layout.Block{}, err
		}
		return layout.Block{Kind: layout.KindButton, Label: b.Button.Text, Href: href}, nil
	case spec.BlockDivider:
		return layout.Block{Kind: layout.KindDivider}, nil
	}
	return layout.Block{}, fmt.Errorf("render: %s: вид блока вне набора", where)
}

// buttonLink — ссылка кнопки. Место ссылки кнопки — имя атрибута (SiteAttr):
// path-атрибут целиком либо token-атрибут при литерале пути шаблона.
func (r *Renderer) buttonLink(b spec.Block, site spec.RefSite, vals map[string]string, where string) (string, error) {
	v, ok := vals[site.Text]
	if !ok {
		return "", fmt.Errorf("%w: %s: %s", ErrAttrMissing, where, site.Text)
	}
	if b.Button.Token == "" {
		p, err := form.ParsePath(v)
		if err != nil {
			return "", fmt.Errorf("render: %s: путь %s: %w", where, site.Text, err)
		}
		return PathLink(r.origin, p)
	}
	p, err := form.ParsePath(b.Button.Path)
	if err != nil {
		return "", fmt.Errorf("render: %s: литерал пути кнопки: %w", where, err)
	}
	tok, err := form.ParseToken(v)
	if err != nil {
		return "", fmt.Errorf("render: %s: токен %s: %w", where, site.Text, err)
	}
	return TokenLink(r.origin, p, tok)
}

// substitute выполняет подстановки {{ имя }} текста места.
func substitute(text string, vals map[string]string, where string) (string, error) {
	segs, err := spec.Segments(text)
	if err != nil {
		return "", fmt.Errorf("render: %s: %w", where, err)
	}
	var b strings.Builder
	for _, s := range segs {
		if s.Attr == "" {
			b.WriteString(s.Literal)
			continue
		}
		v, ok := vals[s.Attr]
		if !ok {
			return "", fmt.Errorf("%w: %s: %s", ErrAttrMissing, where, s.Attr)
		}
		b.WriteString(v)
	}
	return b.String(), nil
}

// asciiAddress — значение адреса для заголовка: нуль — address.ErrUnset,
// не-ASCII — ErrAddressNotASCII. Домен Normalize уже привёл к A-label.
func asciiAddress(n address.Normalized, role string) (string, error) {
	v, err := n.Value()
	if err != nil {
		return "", fmt.Errorf("render: %s: %w", role, err)
	}
	if !isPrintableASCII(v) {
		return "", fmt.Errorf("%w: %s", ErrAddressNotASCII, role)
	}
	local := v[:strings.LastIndexByte(v, '@')]
	if !isDotAtom(local) {
		local = quoteString(local)
	}
	return local + v[strings.LastIndexByte(v, '@'):], nil
}
