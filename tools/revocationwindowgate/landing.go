// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package revocationwindowgate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Profile is one values profile of the installation: its path (the coordinate
// a finding names) and its text.
type Profile struct {
	Path string
	Src  []byte
}

// Policy is what the sites are judged against: the declared window of every
// site key ("<process> <knob>") and the ceiling no window may exceed. The
// tree gate passes corelib authz.RevocationPolicy; the injection probes pass a
// synthetic one.
type Policy struct {
	Windows map[string]time.Duration
	Ceiling time.Duration
}

// Landed is the window a REQUIRED site gets from the installation: read from
// the production profile at the site's values path, with that coordinate.
type Landed struct {
	Key     string
	Window  time.Duration
	Profile string
	Line    int
}

// Verdict is the outcome of Judge, including what was examined.
type Verdict struct {
	Findings     []string
	Landed       []Landed
	ProfilesRead int
}

func (v *Verdict) findingf(format string, args ...any) {
	v.Findings = append(v.Findings, fmt.Sprintf(format, args...))
}

// Judge compares every site with the policy, in both directions.
//
// A site whose window is written in source (Site.InSource) is judged by that
// value. A REQUIRED site — no default in source, the value is left to the
// deployment — is judged by the value the installation gives it: prod is the
// production profile, and the window is read there at the values path the
// declaration's `knob` tag names (Site.ValuesPath). The gate keeps no list of
// paths of its own; the declaration names it. The comparison is the same one a
// source value gets — equal to the policy record, not above the ceiling — and
// every other profile that sets the key must set the record's value too.
//
// Before this branch existed a required site was red by construction, "a
// window without a value in source", however the installation declared it:
// the gate read the value only where the landing does not write it.
//
// Silence is never the outcome of a value that was not read: an absent key, a
// path the declaration does not name, a value that is not a duration and a
// profile that does not parse are each a finding with their coordinate.
func Judge(sites []Site, pol Policy, prod Profile, others []Profile) Verdict {
	var v Verdict
	prodDoc := v.parse(prod)
	type otherDoc struct {
		path string
		doc  *yaml.Node
	}
	var rest []otherDoc
	for _, p := range others {
		if d := v.parse(p); d != nil {
			rest = append(rest, otherDoc{p.Path, d})
		}
	}

	seen := map[string]bool{}
	for _, s := range sites {
		key := s.Service + " " + s.Knob
		seen[key] = true
		want, ok := pol.Windows[key]
		if !ok {
			v.findingf("окно не объявлено политикой: %s (%s:%d) держит %s, "+
				"но записи «%s» в pkg/authz.RevocationPolicy.Windows нет.\n"+
				"Окно отзыва — параметр безопасности: у него должен быть автор. "+
				"Внеси запись с обоснованием либо убери кеш.",
				key, s.File, s.Line, s.Window, key)
			continue
		}
		if s.InSource {
			v.compare(key, s, s.Window, "", want, pol.Ceiling)
			continue
		}
		if s.ValuesPath == "" {
			v.findingf("окно без пути в посадке: %s (%s:%d) — ручка обязательна, умолчания у неё нет, "+
				"и тега knob у объявления нет: пути values, по которому установка задаёт величину, "+
				"назвать нечем; политика объявляет %s.\n"+
				"Добавь объявлению тег knob:\"<путь values>\" — гейт читает величину окна по нему.",
				key, s.File, s.Line, want)
			continue
		}
		if prodDoc == nil {
			continue // профиль не разобран — находка уже есть
		}
		d, line, found, ok := v.windowAt(prodDoc, prod.Path, s.ValuesPath, key)
		switch {
		case !found:
			v.findingf("окно без величины в посадке: %s (%s:%d) — ручка обязательна, умолчания у неё нет, "+
				"а профиль %s по пути values %q её не задаёт; политика объявляет %s.\n"+
				"Величину окна обязательной ручки пишет установка: задай её в профиле по пути из тега "+
				"knob объявления (либо поправь тег, если он называет не тот путь).",
				key, s.File, s.Line, prod.Path, s.ValuesPath, want)
			continue
		case !ok:
			continue // не длительность — находка уже есть
		}
		v.Landed = append(v.Landed, Landed{Key: key, Window: d, Profile: prod.Path, Line: line})
		v.compare(key, s, d, fmt.Sprintf(" (профиль %s:%d, путь values %q)", prod.Path, line, s.ValuesPath),
			want, pol.Ceiling)

		for _, o := range rest {
			od, oline, ofound, ook := v.windowAt(o.doc, o.path, s.ValuesPath, key)
			if !ofound || !ook || od == want {
				continue
			}
			v.findingf("профиль разошёлся с политикой: %s — профиль %s:%d задаёт %s по пути values %q, "+
				"политика объявляет %s.\n"+
				"Профиль зонтика, задающий ключ окна, задаёт значение записи: иначе стенд меряет отзыв "+
				"по окну, которого политика не объявляла.",
				key, o.path, oline, od, s.ValuesPath, want)
		}
	}

	// Самоистечение: запись политики, которой больше нечего описывать, —
	// находка. Иначе перепись переживёт свой предмет.
	var stale []string
	for key := range pol.Windows {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		v.findingf("запись политики без предмета: «%s» объявлена в "+
			"pkg/authz.RevocationPolicy.Windows, но такой площадки в дереве нет.\n"+
			"Ручку переименовали или кеш убрали — сними запись, иначе перепись "+
			"описывает мир, которого нет.", key)
	}
	return v
}

// compare — сверка величины окна с записью политики и с потолком; where —
// откуда величина, если не из исходника.
func (v *Verdict) compare(key string, s Site, got time.Duration, where string, want, ceiling time.Duration) {
	if got != want {
		v.findingf("окно разошлось с политикой: %s (%s:%d) держит %s%s, политика объявляет %s.\n"+
			"Смена окна отзыва — решение, а не правка умолчания: обнови "+
			"pkg/authz.RevocationPolicy вместе с источником величины (или верни прежнее значение).",
			key, s.File, s.Line, got, where, want)
	}
	if got > ceiling {
		v.findingf("окно превышает потолок политики: %s (%s:%d) держит %s%s при потолке %s.\n"+
			"Потолок — это обещание, которое платформа даёт про отзыв доступа.",
			key, s.File, s.Line, got, where, ceiling)
	}
}

// parse разбирает профиль и считает его прочитанным; неразборный — находка.
func (v *Verdict) parse(p Profile) *yaml.Node {
	v.ProfilesRead++
	var doc yaml.Node
	if err := yaml.Unmarshal(p.Src, &doc); err != nil {
		v.findingf("профиль %s не разбирается как YAML: %v — величины окон из него не прочитаны", p.Path, err)
		return nil
	}
	return &doc
}

// windowAt — величина по пути values в разобранном профиле: found — ключ задан
// непустым значением; ok — значение прочитано длительностью (иначе находка уже
// записана). line — строка значения в профиле (для ссылки на якорь — строка
// ссылки).
func (v *Verdict) windowAt(doc *yaml.Node, path, valuesPath, key string) (d time.Duration, line int, found, ok bool) {
	n := lookup(doc, valuesPath)
	if n == nil {
		return 0, 0, false, false
	}
	line = n.Line
	n = deref(n)
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return 0, 0, false, false
	}
	if n.Kind != yaml.ScalarNode {
		v.findingf("окно не читается: %s — профиль %s:%d по пути values %q задаёт не скаляр, а длительность "+
			"Go (\"5s\") ожидалась", key, path, line, valuesPath)
		return 0, line, true, false
	}
	d, err := time.ParseDuration(n.Value)
	if err != nil {
		v.findingf("окно не читается: %s — профиль %s:%d по пути values %q задаёт %q, а это не "+
			"длительность Go (\"5s\")", key, path, line, valuesPath, n.Value)
		return 0, line, true, false
	}
	return d, line, true, true
}

// lookup — узел по пути values «a.b.c» (сегменты через точку, как их пишет тег
// knob); nil — пути в профиле нет. Ссылки на якоря и ключи слияния `<<`
// разрешаются: это законные формы записи values.
func lookup(doc *yaml.Node, valuesPath string) *yaml.Node {
	n := doc
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	for _, seg := range strings.Split(valuesPath, ".") {
		n = mappingGet(deref(n), seg)
		if n == nil {
			return nil
		}
	}
	return n
}

// mappingGet — значение ключа отображения; явный ключ старше ключа слияния.
func mappingGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	var merged []*yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, val := m.Content[i], m.Content[i+1]
		if k.Value == key && k.Tag != "!!merge" {
			return val
		}
		if k.Tag == "!!merge" {
			src := deref(val)
			if src.Kind == yaml.SequenceNode {
				for _, e := range src.Content {
					merged = append(merged, deref(e))
				}
			} else {
				merged = append(merged, src)
			}
		}
	}
	for _, src := range merged {
		if n := mappingGet(src, key); n != nil {
			return n
		}
	}
	return nil
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}
