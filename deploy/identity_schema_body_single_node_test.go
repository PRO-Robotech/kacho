// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_schema_body_single_node_test.go — ТЕЛО СХЕМЫ ЛИЧНОСТИ объявляется в
// профиле ОДНИМ узлом; всякое второе вхождение обязано быть ССЫЛКОЙ на него, а
// не копией литерала.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Тело схемы личности нужно ДВУМ сторонам настроек сразу: нашей секции (через
// `global.kacho.identity.inheritedSchemas`) и секции подчарта поставщика (через
// `kratos.kratos.config.identity.schemas`). Значения подчарта нашей секции не
// видны, поэтому соблазн — выписать тело второй раз литералом. Разойдутся такие
// копии молча: обе валидны, обе рендерятся, и какая из двух применится к живым
// строкам, решит порядок слияния, а не решение.
//
// Ровно так завелось третье место об одном предмете (задача #1239):
// `kratos.identitySchemas` в общих значениях повторяло тело унаследованной схемы
// дословно — совпадал даже `$id` — и при этом НЕ РЕЗОЛВИЛОСЬ: подчарт читает
// `.Values.kratos.identitySchemas`, то есть уровнем НИЖЕ, и в его карте настроек
// был единственный ключ. Мёртвое объявление хуже лишнего: следующий читатель
// примет его за действующее и станет править ЕГО вместо живого.
//
// Противоядие — ЯКОРЬ YAML. Тело стоит одним узлом, второй потребитель получает
// на него ССЫЛКУ (`*kachoInheritedIdentitySchemas`), и расхождение становится
// невозможным by construction, а не проверяемым. Это решение уже записано в
// самом профиле («копия литерала разошлась бы молча») — здесь оно обретает
// держателя.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ДВЕ ОСИ
//
//	(1) ЕДИНСТВЕННОСТЬ — в одном профиле тело схемы объявлено не более чем
//	    ОДНИМ узлом. Второе объявление — находка, называющая ОБА пути;
//	(2) ССЫЛАЕМОСТЬ — единственное объявление несёт ИМЯ ЯКОРЯ (на себе или на
//	    предке). Без якоря сослаться на тело нельзя, и следующая надобность
//	    породит копию — то есть ось (1) станет ложью не по недосмотру, а по
//	    построению.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТО НЕ УТВЕРЖДАЕТ
//
//   - НЕ утверждает, что тело ВЕРНО. Соответствие схемы посеву держит
//     identity_seed_matches_chart_schema_test.go, покрытие унаследованных схем —
//     identity_inherited_schemas_are_declared_test.go. Здесь не пересказывается
//     ни то, ни другое: два места об одном предмете разошлись бы молча.
//   - НЕ утверждает, что у объявления есть ЧИТАТЕЛЬ. Читателей у тела два, и
//     один из них — наш шаблон, который берёт значение путём, а не ссылкой;
//     требовать ссылку значило бы краснеть на профиле, где второй потребитель
//     ещё не заведён.
//   - НЕ судит профиль, тела схемы не несущий: замещать там нечего. Это
//     законный близнец, и он обязан молчать.
//
// ─────────────────────────────────────────────────────────────────────────────
// ССЫЛКА НЕ ЕСТЬ КОПИЯ — И ЭТО ДИСКРИМИНАТОР, А НЕ УДОБСТВО
//
// Обход идёт по узлам YAML и НЕ СПУСКАЕТСЯ в узел-ссылку (`yaml.AliasNode`).
// Разбор «в значения» развернул бы ссылку в те же данные, и якорь с копией стали
// бы неразличимы — то есть гейт краснел бы ровно на том решении, ради которого
// заведён. Законный близнец в самом дереве:
// `helm/umbrella/values.fe3455-ory-posture.yaml` несёт одно тело и одну ссылку.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДПОСЫЛКИ, ОБЪЯВЛЕННЫЕ ВСЛУХ
//
//	П1. Форм записи тела ДВЕ, и обе распознаются: голый JSON и `base64://`.
//	    Величина, объявленная `base64://` и НЕ РАЗБИРАЕМАЯ ни одной из четырёх
//	    кодировок, — НАХОДКА предпосылки, а не молчание: всё записанное в
//	    неизвестной форме ушло бы из-под наблюдения.
//	П2. Тел схемы во всём наборе профилей больше нуля. Ноль означает, что
//	    распознаватель перестал их узнавать, а не что дерево стало чистым.
//	П3. Объём осмотренного печатается ВСЕГДА: «ноль находок» обязано быть
//	    отличимо от «ноль прочитанного».
//
// Способность упасть и смолчать доказана инъекцией —
// identity_schema_body_single_node_injection_test.go.
package deploy_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// identityProfilesPattern — набор профилей развёртывания, читаемый от корня
// дерева. Выводится обходом индекса git, а не выписывается: рукописный перечень
// разошёлся бы с деревом молча.
const identityProfilesPattern = "deploy/helm/umbrella/values*.yaml"

// identityBodyDecl — одно объявление тела схемы личности: путь внутри профиля и
// имя якоря, под которым тело стоит ("" — якоря нет).
type identityBodyDecl struct {
	Path   string
	Anchor string
}

func (d identityBodyDecl) String() string {
	if d.Anchor == "" {
		return d.Path + " (без якоря)"
	}
	return d.Path + " (&" + d.Anchor + ")"
}

// identityBodyFinding — одна находка адъюдикатора. Причина строкой, а не кодом:
// стороны расхождения чинятся по-разному, и вердикт обязан это различать.
type identityBodyFinding struct {
	Profile string
	Reason  string
	Paths   []string
}

// Причины — поимённо, чтобы инъекция сверялась с ними, а не с текстом отказа.
const (
	schemaBodyDeclaredTwice = "тело схемы личности объявлено в профиле БОЛЕЕ ЧЕМ ОДНИМ узлом"
	schemaBodyNotAnchored   = "тело схемы личности объявлено узлом БЕЗ ЯКОРЯ"
)

// adjudicateIdentitySchemaBodies — ЕДИНСТВЕННЫЙ дискриминатор гейта. Зовётся и
// переписью по дереву, и инъекцией: копия предиката разошлась бы с оригиналом
// молча и доказывала бы саму себя.
func adjudicateIdentitySchemaBodies(profile string, decls []identityBodyDecl) []identityBodyFinding {
	if len(decls) == 0 {
		return nil // профиль тела не несёт — замещать нечего
	}
	var out []identityBodyFinding

	// Ось (1): единственность. Находка называет ОБА пути — иначе чинить её
	// пришлось бы наугад, гадая, какое из объявлений лишнее.
	if len(decls) > 1 {
		paths := make([]string, 0, len(decls))
		for _, d := range decls {
			paths = append(paths, d.String())
		}
		sort.Strings(paths)
		out = append(out, identityBodyFinding{Profile: profile, Reason: schemaBodyDeclaredTwice, Paths: paths})
	}

	// Ось (2): ссылаемость. Объявление без якоря переиспользовать нечем.
	for _, d := range decls {
		if d.Anchor == "" {
			out = append(out, identityBodyFinding{
				Profile: profile, Reason: schemaBodyNotAnchored, Paths: []string{d.Path},
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Reason != out[j].Reason {
			return out[i].Reason < out[j].Reason
		}
		return strings.Join(out[i].Paths, "|") < strings.Join(out[j].Paths, "|")
	})
	return out
}

// identitySchemaBodyDecls обходит профиль по УЗЛАМ YAML и собирает объявления
// тела схемы. Вторым значением — формы, которых распознаватель не знает
// (предпосылка П1): молчать о них нельзя, они ушли бы из-под наблюдения.
//
// Узел-ссылка (`yaml.AliasNode`) НЕ РАСКРЫВАЕТСЯ: ссылка — это второе
// употребление одного узла, а не второе объявление, и именно она есть искомое
// решение.
func identitySchemaBodyDecls(raw []byte) ([]identityBodyDecl, []string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, nil, err
	}
	var (
		decls   []identityBodyDecl
		unknown []string
	)
	var walk func(n *yaml.Node, path []string, anchor string)
	walk = func(n *yaml.Node, path []string, anchor string) {
		if n == nil || n.Kind == yaml.AliasNode {
			return
		}
		if n.Anchor != "" {
			anchor = n.Anchor
		}
		switch n.Kind {
		case yaml.DocumentNode:
			for _, c := range n.Content {
				walk(c, path, anchor)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				walk(n.Content[i+1], append(append([]string{}, path...), n.Content[i].Value), anchor)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				walk(c, append(append([]string{}, path...), fmt.Sprintf("[%d]", i)), anchor)
			}
		case yaml.ScalarNode:
			body, why := identitySchemaBodyOf(n.Value)
			if why != "" {
				unknown = append(unknown, strings.Join(path, ".")+": "+why)
				return
			}
			if body {
				decls = append(decls, identityBodyDecl{Path: strings.Join(path, "."), Anchor: anchor})
			}
		}
	}
	walk(&root, nil, "")
	sort.Slice(decls, func(i, j int) bool { return decls[i].Path < decls[j].Path })
	return decls, unknown, nil
}

// identitySchemaBodyOf решает, ЕСТЬ ЛИ в величине тело схемы личности.
//
// Первое значение — «это тело»; второе — причина, по которой величина осталась
// НЕРАСПОЗНАННОЙ (предпосылка П1). Обе формы записи тела, живущие в дереве,
// сводятся к одному вопросу: разбирается ли величина как объект JSON, у которого
// есть признак `properties.traits`. Это форма схемы личности, а не догадка по
// имени ключа: имя ключа менялось трижды, форма — ни разу.
//
// Адрес-ссылка (`file:///…`) телом НЕ является и обязана молчать: она указывает
// на тело, а не содержит его. Это законный близнец, и он есть в самом дереве —
// собственная схема продукта объявлена именно так.
func identitySchemaBodyOf(v string) (bool, string) {
	t := strings.TrimSpace(v)
	if strings.HasPrefix(t, "base64://") {
		payload := strings.TrimPrefix(t, "base64://")
		dec, ok := identityDecodeBase64(payload)
		if !ok {
			return false, "величина объявлена `base64://`, но не разбирается НИ ОДНОЙ из четырёх " +
				"кодировок — форма, которой распознаватель не знает"
		}
		t = strings.TrimSpace(dec)
	}
	if !strings.HasPrefix(t, "{") {
		return false, ""
	}
	var o map[string]any
	if json.Unmarshal([]byte(t), &o) != nil {
		return false, ""
	}
	props, ok := o["properties"].(map[string]any)
	if !ok {
		return false, ""
	}
	_, ok = props["traits"]
	return ok, ""
}

// identityDecodeBase64 пробует четыре кодировки, потому что их четыре и есть:
// знать только одну значило бы объявить неизвестной законную запись.
func identityDecodeBase64(s string) (string, bool) {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), true
		}
	}
	return "", false
}

// TestIdentitySchemaBodyIsDeclaredByASingleNode — (I) объявления.
func TestIdentitySchemaBodyIsDeclaredByASingleNode(t *testing.T) {
	profiles := trackedFiles(t, identityProfilesPattern)
	if len(profiles) == 0 {
		t.Fatalf("обход не нашёл НИ ОДНОГО профиля по образцу %q — вердикта нет: "+
			"«нарушений не найдено» здесь неотличимо от «ничего не прочитано»",
			identityProfilesPattern)
	}
	sort.Strings(profiles)

	var (
		bytesRead      int
		carrying       int
		bodies         int
		anchoredBodies int
		findings       []identityBodyFinding
	)
	for _, p := range profiles {
		raw := readTracked(t, p)
		bytesRead += len(raw)
		decls, unknown, err := identitySchemaBodyDecls([]byte(raw))
		if err != nil {
			t.Fatalf("профиль %s не разбирается как YAML: %v — обход по нему НЕ СОСТОЯЛСЯ, "+
				"и молчание здесь означало бы «не читали», а не «чисто»", p, err)
		}
		if len(unknown) > 0 {
			t.Fatalf("профиль %s несёт %d форм(ы) записи, которых распознаватель НЕ ЧИТАЕТ:\n  %s\n"+
				"Это не край и не редкость: всё записанное в такой форме уходит ИЗ-ПОД НАБЛЮДЕНИЯ — "+
				"гейт не даёт ни красного, ни зелёного, он молчит",
				p, len(unknown), strings.Join(unknown, "\n  "))
		}
		if len(decls) > 0 {
			carrying++
			bodies += len(decls)
			for _, d := range decls {
				if d.Anchor != "" {
					anchoredBodies++
				}
			}
		}
		findings = append(findings, adjudicateIdentitySchemaBodies(p, decls)...)
	}

	for _, f := range findings {
		switch f.Reason {
		case schemaBodyDeclaredTwice:
			t.Errorf("%s: %s.\nПути объявлений:\n  %s\nТело нужно ДВУМ сторонам настроек сразу, "+
				"и копия литерала разойдётся с оригиналом МОЛЧА: обе валидны, обе рендерятся, "+
				"какая применится — решит порядок слияния. Исход один: оставьте ОДИН узел с "+
				"якорем, а второму потребителю дайте ССЫЛКУ на него. Если второе объявление "+
				"вообще не резолвится (читатель берёт значение уровнем ниже) — снимите его: "+
				"мёртвое объявление хуже лишнего, следующий читатель станет править ЕГО",
				f.Profile, f.Reason, strings.Join(f.Paths, "\n  "))
		case schemaBodyNotAnchored:
			t.Errorf("%s: %s — путь %s.\nСослаться на такое объявление нечем, поэтому "+
				"следующая надобность в теле породит КОПИЮ, и разойдутся они молча. "+
				"Поставьте на узел имя якоря (`&<имя>`) и берите тело ссылкой",
				f.Profile, f.Reason, strings.Join(f.Paths, ", "))
		}
	}

	// П2 — распознаватель ещё узнаёт предмет.
	if bodies == 0 {
		t.Fatalf("во всех %d профилях не найдено НИ ОДНОГО тела схемы личности — это означает, "+
			"что распознаватель перестал его узнавать, а не что дерево стало чистым: тело "+
			"обязано быть объявлено хотя бы одним профилем", len(profiles))
	}
	// П3 — объём осмотренного печатается ВСЕГДА.
	t.Logf("перепись: профилей прочитано %d · байт %d · несут тело схемы %d · "+
		"объявлений тела %d · из них под якорем %d · находок %d",
		len(profiles), bytesRead, carrying, bodies, anchoredBodies, len(findings))
}
