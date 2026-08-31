// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_mail_lane_single_declaration_test.go — MAIL-54 приёмки ID-MAIL-1.
//
// ПРЕДМЕТ. Почтовая полоса службы личности объявляется ОДНИМ местом — нашей
// конфигурацией личности, питаемой `global.kacho.identity.smtp.*` (решение Р27),
// и оснастка боевой раскатки посылает оператора ИМЕННО ТУДА.
//
// ЗАЧЕМ. Процесс службы личности получает НЕСКОЛЬКО файлов настроек и сливает
// их по порядку; наш идёт вторым. Значит раздел, объявленный нами, замещает
// одноимённый раздел поставщика ЦЕЛИКОМ, а не дополняет его. Пока об одной
// полосе высказываются два места, какое из них доедет до процесса — решает
// порядок слияния, а не решение. А пока оснастка раскатки называет оператору
// координату, отличную от единственного объявления, обязанность оператора
// НЕИСПОЛНИМА: он кладёт настоящий узел туда, куда его послали, раскатка
// зелёная, поды стартуют, письма уходят в никуда, и сигнала нет ни одного.
//
// ТРИ УТВЕРЖДЕНИЯ, ТРИ ПАРЫ ПЕРЕПИСИ. У сценария их три, и одно общее число не
// позволило бы отличить «ноль находок» от «ноль прочитанного» ни у одного:
//
//	объявления — мест, где встречается почтовая полоса, прочитано · объявлений найдено
//	оснастка   — указаний на координату прочитано · согласных с объявлением
//	перечни    — комментарных блоков прочитано · рукописных перечней разделов найдено
//
// ЕДИНИЦЫ РАЗНЫЕ, и это названо: у первых двух пар единица — МЕСТО (объявление
// либо указание), у третьей — КОММЕНТАРНЫЙ БЛОК. Смешение единиц дало бы
// перепись, которая не складывается сама с собой.
//
// ГРАНИЦА МЕХАНИЗМА, названная прямо. Прозаическое утверждение о ВЛАДЕНИИ
// полосой, не называющее ключей, этот гейт НЕ ловит и ловить не будет:
// лексиконный детектор над естественным языком в этом корпусе уже строился и
// провалил контроль в обе стороны. Декларативная половина достаёт до тех
// блоков, которые перечисляют разделы КЛЮЧАМИ, — впредь эта половина держится
// обзором, и так и записано. Обещать здесь гейт значило бы завести форму без
// содержания.
//
// ЧЕГО ГЕЙТ НЕ УТВЕРЖДАЕТ. Какая из двух координат замещается — свойство чужого
// процесса, из нашего дерева не читаемое. Для находки это и не нужно: две
// разные координаты одной полосы суть дефект при любом победителе.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// mailLaneFeedPath — ЕДИНСТВЕННЫЙ путь значений, из которого рендерится раздел
// `courier.smtp` нашей конфигурации личности. Он же — координата, которую
// оснастка раскатки обязана называть оператору.
const mailLaneFeedPath = "global.kacho.identity.smtp"

// mailLaneFeedURI — координата адреса подключения внутри полосы. Именно её
// оператор задаёт в слое учётных данных боевой площадки.
const mailLaneFeedURI = mailLaneFeedPath + ".connectionURI"

// Наш шаблон конфигурации личности — единственное законное объявление раздела
// `courier` — адресуется существующей константой `identityConfigTemplate`
// (identity_seed_matches_chart_schema_test.go). Второй копии координаты здесь
// не заводится: это ровно тот класс, который третье утверждение ниже ловит.

// cutoverScript — оснастка боевой раскатки. Перечень разрешённых координат слоя
// учётных данных в ней — ЕДИНСТВЕННОЕ указание, которое оператор вообще
// получает: второго читателя, способного заметить расхождение, у него нет.
const cutoverScript = "helm/umbrella/cutover-fe3455.sh"

// mailLaneMention — упоминание почтовой полосы в объявлении развёртывания.
// Ключевые формы обеих сторон: наша (`smtp:` / `connectionURI`) и поставщика
// (`courier:` / `connection_uri`).
var mailLaneMention = regexp.MustCompile(`(?m)^\s*(courier|smtp)\s*:|connection_?URI|connection_uri`)

// courierSectionDecl — объявление раздела `courier` В ЗНАЧЕНИЯХ, то есть
// встроенный блок профиля. Наш шаблон под этот предикат не подпадает: он
// рендерит раздел, а не объявляет его значением.
var courierSectionDecl = regexp.MustCompile(`(?m)^\s*courier\s*:`)

// umbrellaDeclarationFiles — все объявления развёртывания зонтичного чарта:
// профили, значения подчартов и шаблоны. Перечень ВЫВОДИТСЯ обходом, а не
// выписывается: выписанный разошёлся бы с деревом молча.
func umbrellaDeclarationFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".yaml", ".yml", ".tpl":
			out = append(out, filepath.ToSlash(p))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход объявлений зонтичного чарта: %v", err)
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("объявлений развёртывания не прочитано ни одного — предпосылка "+
			"проверки исчезла, а не дерево стало чистым (каталог %s)", root)
	}
	return out
}

// identityConfigSections — разделы верхнего уровня НАШЕЙ конфигурации личности.
// ВЫВОДЯТСЯ из шаблона: перечень, выписанный вторым местом, и есть тот класс,
// который третье утверждение сценария ловит.
func identityConfigSections(t *testing.T, tpl string) []string {
	t.Helper()
	raw, err := os.ReadFile(tpl)
	if err != nil {
		t.Fatalf("шаблон конфигурации личности %s не читается: %v", tpl, err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, `define "kacho.identity.configYaml"`) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("в %s нет определения `kacho.identity.configYaml` — форма шаблона "+
			"сменилась, и перечень разделов вывести неоткуда", tpl)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "{{- define ") {
			end = i
			break
		}
	}
	top := regexp.MustCompile(`(?m)^([a-z_]+):`)
	seen := map[string]bool{}
	var out []string
	for _, m := range top.FindAllStringSubmatch(strings.Join(lines[start:end], "\n"), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("разделов конфигурации личности не выведено ни одного — предикат "+
			"перестал читать форму шаблона %s. «Ноль находок» здесь неотличимо от "+
			"«ноль прочитанного», поэтому это отказ, а не тишина", tpl)
	}
	return out
}

// commentBlock — максимальный пробег подряд идущих комментарных строк.
type commentBlock struct {
	file string
	line int
	text string
}

func commentBlocks(t *testing.T, files []string) []commentBlock {
	t.Helper()
	var out []commentBlock
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		lines := strings.Split(string(raw), "\n")
		var cur []string
		curAt := 0
		flush := func() {
			if len(cur) > 0 {
				out = append(out, commentBlock{file: f, line: curAt, text: strings.Join(cur, "\n")})
				cur = nil
			}
		}
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				if cur == nil {
					curAt = i + 1
				}
				cur = append(cur, l)
				continue
			}
			flush()
		}
		flush()
	}
	return out
}

// mailLaneAssertions — все три утверждения MAIL-54 над НАЗВАННЫМ деревом.
//
// Корень параметризован затем, чтобы доказательство способности гейта упасть
// (инъекция) шло по КОПИИ в t.TempDir(), а не по рабочему дереву: состояние,
// которого проверка не заводила, она не трогает.
func mailLaneAssertions(t *testing.T, root, tpl, script string) []string {
	t.Helper()
	var findings []string
	files := umbrellaDeclarationFiles(t, root)
	sections := identityConfigSections(t, tpl)
	t.Logf("осмотрено: объявлений развёртывания %d, разделов нашей конфигурации личности %d: %v",
		len(files), len(sections), sections)

	// ── ПАРА 1: объявления ────────────────────────────────────────────────
	//
	// Читаются все места, где встречается почтовая полоса. ОБЪЯВЛЕНИЕМ
	// считается место, фиксирующее СОСТАВ раздела `courier`: наш шаблон,
	// который его рендерит, и встроенный блок `courier` в значениях профиля.
	//
	// Величина по пути питания (`global.kacho.identity.smtp.*`) объявлением НЕ
	// является: она КОРМИТ единственное объявление, а не заводит второе.
	// Встроенное УМОЛЧАНИЕ такой величины — отдельный предмет (Р3), и судит
	// его отдельный сценарий приёмки; смешение двух предметов в одной паре
	// дало бы перепись, по которой нельзя сказать, что именно найдено.
	mentions, declarations := 0, []string{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		body := string(raw)
		if !mailLaneMention.MatchString(body) {
			continue
		}
		mentions++

		isTemplate := strings.Contains(f, "/templates/")
		if isTemplate {
			// Наш шаблон — единственное законное объявление. Второй шаблон,
			// рендерящий `courier`, — находка.
			if courierSectionDecl.MatchString(body) && f != identityConfigTemplate {
				declarations = append(declarations,
					fmt.Sprintf("%s — второй ШАБЛОН, рендерящий раздел `courier`", f))
			}
			if f == identityConfigTemplate && courierSectionDecl.MatchString(body) {
				declarations = append(declarations,
					fmt.Sprintf("%s — наш шаблон (ЕДИНСТВЕННОЕ законное объявление)", f))
			}
			continue
		}

		// Значения: встроенный блок `courier` — всегда второе объявление.
		if courierSectionDecl.MatchString(body) {
			for i, l := range strings.Split(body, "\n") {
				if courierSectionDecl.MatchString(l) {
					declarations = append(declarations, fmt.Sprintf(
						"%s:%d — ВСТРОЕННЫЙ блок `courier` в значениях: он замещает наше "+
							"объявление либо замещается им, и какое победит, решал бы порядок слияния",
						f, i+1))
				}
			}
		}

	}
	sort.Strings(declarations)
	t.Logf("перепись · объявления: мест, где встречается почтовая полоса, прочитано %d · "+
		"объявлений найдено %d", mentions, len(declarations))
	if mentions == 0 {
		t.Fatalf("почтовая полоса не встретилась ни в одном объявлении развёртывания — " +
			"предикат перестал её узнавать. «Ноль находок» здесь неотличимо от " +
			"«ноль прочитанного», поэтому это отказ, а не тишина")
	}
	if len(declarations) != 1 {
		findings = append(findings, fmt.Sprintf(
			"объявлений почтовой полосы %d, а решение Р27 требует РОВНО ОДНО — "+
				"нашу конфигурацию личности, питаемую %s:\n  %s",
			len(declarations), mailLaneFeedPath, strings.Join(declarations, "\n  ")))
	}

	// ── ПАРА 2: оснастка ──────────────────────────────────────────────────
	//
	// Почтовая координата, названная оснасткой раскатки, обязана совпадать с
	// единственным объявлением. Несовпадение — находка, называющая ОБЕ
	// координаты и то, что они различны.
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("оснастка раскатки %s не читается (%v) — предпосылка второго "+
			"утверждения исчезла, а не оснастка стала согласной", script, err)
	}
	scriptBody := string(raw)
	coordLine := regexp.MustCompile(`(?m)^([A-Za-z0-9_.]*(?:courier|smtp|connectionURI|connection_uri)[A-Za-z0-9_.]*)\s*$`)
	var named, agreeing []string
	for _, m := range coordLine.FindAllStringSubmatch(scriptBody, -1) {
		named = append(named, m[1])
		if strings.HasPrefix(m[1], mailLaneFeedPath+".") {
			agreeing = append(agreeing, m[1])
		}
	}
	t.Logf("перепись · оснастка: указаний на координату прочитано %d · согласных с объявлением %d",
		len(named), len(agreeing))
	if len(named) == 0 {
		t.Fatalf("%s не называет ни одной почтовой координаты — предикат перестал их "+
			"узнавать либо перечень разрешённых координат сменил форму; это отказ, "+
			"а не тишина", script)
	}
	for _, c := range named {
		if strings.HasPrefix(c, mailLaneFeedPath+".") {
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"%s называет оператору почтовую координату %q, а полоса объявлена по "+
				"%q — координаты РАЗЛИЧНЫ. Оператор кладёт настоящий узел туда, куда его "+
				"послали, раскатка проходит, поды стартуют, письма уходят в никуда, и "+
				"сигнала нет ни одного. Какая из двух замещается, гейт не утверждает: две "+
				"разные координаты одной полосы суть дефект при любом победителе",
			script, c, mailLaneFeedURI))
	}

	// ── ПАРА 3: перечни ───────────────────────────────────────────────────
	//
	// Перечень разделов конфигурации личности машинно выводим из нашего
	// шаблона. Второе его изложение — то же «два места об одном предмете»,
	// что чинит Р11: рукописный перечень переживает правку шаблона молча.
	blocks := commentBlocks(t, files)
	var lists []string
	for _, b := range blocks {
		var named []string
		for _, s := range sections {
			re := regexp.MustCompile("`" + s + "`|(?:^|[^a-z_])" + s + ":")
			if re.MatchString(b.text) {
				named = append(named, s)
			}
		}
		if len(named) >= 2 {
			lists = append(lists, fmt.Sprintf("%s:%d — перечисляет разделы %v",
				b.file, b.line, named))
		}
	}
	sort.Strings(lists)
	t.Logf("перепись · перечни: комментарных блоков прочитано %d · рукописных перечней "+
		"разделов найдено %d", len(blocks), len(lists))
	if len(blocks) == 0 {
		t.Fatalf("комментарных блоков не прочитано ни одного — предикат перестал их " +
			"узнавать; это отказ, а не тишина")
	}
	for _, l := range lists {
		findings = append(findings, fmt.Sprintf(
			"рукописный перечень разделов конфигурации личности: %s.\n"+
				"Перечень выводится из %s (сегодня их %d: %v) — второе изложение "+
				"переживает правку шаблона молча и становится ложью, не будучи видимо "+
				"ничем. Снимите перечень либо сведите его к тому, что действительно "+
				"остаётся за подчартом поставщика", l, tpl,
			len(sections), sections))
	}
	return findings
}

// TestMailLaneIsDeclaredOnce — MAIL-54 на рабочем дереве.
func TestMailLaneIsDeclaredOnce(t *testing.T) {
	for _, f := range mailLaneAssertions(t, umbrellaDir, identityConfigTemplate,
		filepath.Join(umbrellaDir, filepath.Base(cutoverScript))) {
		t.Error(f)
	}
}
