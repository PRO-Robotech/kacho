// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notify_dns_profiles_test.go — держатель NTF1-P12 (NTF-1 Р19 «Профиль стенда»;
// замысел §12а «Профили», «Зона стенда», CX1-129, CX1-130, CX1-135, CX1-136,
// CX1-141; Д102, Д104, Д106; полоса D10).
//
// Профилей семь (deploy/stacks.txt), и у каждого признак почтовой полосы
// объявлен явно в deploy/stacks-mail.txt: `stand` — приёмник стенда этого
// релиза, `operator` — узла в дереве нет, `relay` — внешний ретранслятор.
// Держатель судит четыре утверждения на двух слоях и печатает признак и исход
// каждого профиля:
//
//	(а) множества профилей stacks.txt и stacks-mail.txt равны;
//	(б) признак совпадает с узлом почты слитых значений профиля
//	    (`stand` ⇔ хост узла — `<релиз>-mailpit` и `mailpit.enabled`);
//	(в) `global.kacho.standDNS.enabled: true` ⇒ признак `stand`;
//	(г) на рендере: notify рендерится и признак `stand` ⇒ у пода notify
//	    `dnsPolicy: None` и `dnsConfig`; признак не `stand` ⇒ `dnsConfig` нет;
//	    у `a8f60d` объектов notify 0 (Д102);
//	(д) фикстурные слои deploy/testdata/stand-dns/: зона поверх узла оператора —
//	    отказ рендера с именем ключа и хостом; поверх `dev` — `dnsConfig` и
//	    `KACHO_NOTIFY_STAND_DNS=<релиз>-mailpit`; зона при выключенном приёмнике
//	    — отказ блоком (6) стража почтовой полосы, текст называет оба ключа;
//	(е) профиль с включённой зоной берёт `serviceIP` и `clusterDomain` из файла
//	    своего кластера (третье поле строки stacks-mail.txt).
//
// Плюс: у загрузчика notify нет ручки адреса резолвера и ручки, пропускающей
// проверку DNS. Способность упасть — инъекции настоящим входом дерева
// (TestNTF1P12StandDNSProfilesInjections).

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	stacksMailTable    = "stacks-mail.txt"
	standDNSRelease    = "kacho-umbrella"
	standDNSFixtureDir = "testdata/stand-dns"
)

// mailRow — строка stacks-mail.txt.
type mailRow struct {
	kind        string // stand | operator | relay
	clusterFile string // только у stand
}

var mailRowRe = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*):(stand|operator|relay)(?::(\S+))?$`)

func readStacksMail(t *testing.T) map[string]mailRow {
	t.Helper()
	raw, err := os.ReadFile(stacksMailTable)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: таблица признаков %s не читается: %v", stacksMailTable, err)
	}
	out := map[string]mailRow{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := mailRowRe.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("КРАСНЫЙ: строка %q таблицы %s не разобрана — признак обязан быть stand|operator|relay", line, stacksMailTable)
		}
		if (m[2] == "stand") != (m[3] != "") {
			t.Fatalf("КРАСНЫЙ: строка %q: файл кластера несёт ровно признак stand", line)
		}
		out[m[1]] = mailRow{kind: m[2], clusterFile: m[3]}
	}
	if len(out) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s ни одной строки — судить нечего", stacksMailTable)
	}
	return out
}

// profileFacts — слитые значения профиля и слой, объявивший каждую ручку зоны.
type profileFacts struct {
	chain   []string
	merged  map[string]any
	ipLayer string // последний слой цепочки, задавший standDNS.serviceIP
	cdLayer string // то же для clusterDomain
}

func profileFactsOf(t *testing.T, chain []string, layers map[string]map[string]any) profileFacts {
	t.Helper()
	merged := mergeValues(map[string]any{}, readYAML(t, filepath.Join(umbrellaDir, "values.yaml")))
	f := profileFacts{chain: chain}
	for _, p := range chain {
		layer, ok := layers[p]
		if !ok {
			layer = readYAML(t, filepath.Join(umbrellaDir, p))
		}
		merged = mergeValues(merged, layer)
		if v, ok := lookup(layer, "global", "kacho", "standDNS", "serviceIP"); ok {
			f.ipLayer = p
			if v == nil {
				f.ipLayer = p + " (null)"
			}
		}
		if v, ok := lookup(layer, "global", "kacho", "standDNS", "clusterDomain"); ok {
			f.cdLayer = p
			if v == nil {
				f.cdLayer = p + " (null)"
			}
		}
	}
	f.merged = merged
	return f
}

// mailKindOf — признак, который выводится из узла почты слитых значений.
func mailKindOf(merged map[string]any) (string, string) {
	raw, _ := lookup(merged, "global", "kacho", "identity", "smtp", "connectionURI")
	uri := strings.TrimSpace(strings.ReplaceAll(nstr(raw), "{{ .Release.Name }}", standDNSRelease))
	if uri == "" {
		return "operator", "узла нет"
	}
	u, err := url.Parse(uri)
	host := ""
	if err == nil {
		host = u.Hostname()
	}
	mp, _ := lookup(merged, "mailpit", "enabled")
	if host == standDNSRelease+"-mailpit" && mp == true {
		return "stand", "узел — приёмник " + host
	}
	return "relay", "узел — " + host
}

func standDNSOn(merged map[string]any) bool {
	v, _ := lookup(merged, "global", "kacho", "standDNS", "enabled")
	return v == true
}

// judgeStandDNSProfiles — (а), (б), (в), (е) на слитых значениях. Чистая
// функция над фактами: её зовут и дерево, и инъекции.
func judgeStandDNSProfiles(stacks map[string][]string, rows map[string]mailRow,
	facts map[string]profileFacts) (lines, findings []string) {
	var names []string
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, ok := rows[n]; !ok {
			findings = append(findings, n+": (а) профиля нет в "+stacksMailTable)
		}
	}
	for n := range rows {
		if _, ok := stacks[n]; !ok {
			findings = append(findings, n+": (а) строка "+stacksMailTable+" без профиля в stacks.txt")
		}
	}
	for _, n := range names {
		row, ok := rows[n]
		if !ok {
			continue
		}
		f := facts[n]
		kind, why := mailKindOf(f.merged)
		zone := standDNSOn(f.merged)
		var bad []string
		if kind != row.kind {
			bad = append(bad, "(б) признак "+row.kind+", а "+why)
		}
		if zone && row.kind != "stand" {
			bad = append(bad, "(в) зона DNS стенда включена при признаке "+row.kind)
		}
		if zone && row.kind == "stand" {
			for _, l := range []struct{ key, layer string }{{"serviceIP", f.ipLayer}, {"clusterDomain", f.cdLayer}} {
				if l.layer != row.clusterFile {
					bad = append(bad, "(е) global.kacho.standDNS."+l.key+" приходит из слоя «"+l.layer+
						"», а файл кластера профиля — "+row.clusterFile)
				}
			}
		}
		outcome := "зелёный"
		if len(bad) > 0 {
			outcome = "КРАСНЫЙ"
			for _, b := range bad {
				findings = append(findings, n+": "+b)
			}
		}
		lines = append(lines, n+": признак "+row.kind+", зона "+map[bool]string{true: "включена", false: "выключена"}[zone]+
			", источник serviceIP «"+f.ipLayer+"» — "+outcome)
	}
	sort.Strings(findings)
	return lines, findings
}

func allProfileFacts(t *testing.T, stacks map[string][]string, layers map[string]map[string]any) map[string]profileFacts {
	t.Helper()
	out := map[string]profileFacts{}
	for n, chain := range stacks {
		out[n] = profileFactsOf(t, chain, layers)
	}
	return out
}

// notifyPodDNS — у пода notify рендера: есть ли объекты notify, dnsPolicy,
// наличие dnsConfig и значение KACHO_NOTIFY_STAND_DNS.
func notifyPodDNS(t *testing.T, rendered string) (objs int, policy string, hasConfig bool, standDNS string) {
	t.Helper()
	for _, o := range parseRendered(t, rendered) {
		if !strings.Contains(o.source, "/charts/notify/") {
			continue
		}
		objs++
		switch o.kind {
		case "Deployment":
			spec := nPodSpec(o)
			policy = nstr(spec["dnsPolicy"])
			_, hasConfig = spec["dnsConfig"].(map[string]any)
		case "ConfigMap":
			standDNS = nstr(ndig(o.doc, "data", "KACHO_NOTIFY_STAND_DNS"))
		}
	}
	return objs, policy, hasConfig, standDNS
}

// TestNTF1P12StandDNSProfiles — держатель NTF1-P12 на дереве как оно есть.
func TestNTF1P12StandDNSProfiles(t *testing.T) {
	stacks := deployStacks(t)
	rows := readStacksMail(t)
	facts := allProfileFacts(t, stacks, nil)
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	lines, findings := judgeStandDNSProfiles(stacks, rows, facts)
	t.Logf("профилей %d (stacks.txt), строк признаков %d (%s)", len(stacks), len(rows), stacksMailTable)
	for _, l := range lines {
		t.Log("  " + l)
	}
	for _, f := range findings {
		t.Errorf("КРАСНЫЙ: %s", f)
	}

	// (г) на рендере каждого профиля.
	var names []string
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	rendered := 0
	// Рендер — цепочками ГЕЙТА: `prod` несёт слой оператора из каталога образцов
	// (число доверенных прыжков края поставка не несёт, приёмка NTF-2 Р8, Д51);
	// суждение (а)–(в) выше читает профили дословно.
	renderStacks := deployStacksForRender(t, "operator.yaml")
	for _, n := range names {
		out, err := renderStandProfile(t, c, renderStacks[n])
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: рендер профиля %s отказал: %v\n%s", n, err, lastLines(out, 5))
		}
		rendered++
		objs, policy, hasCfg, sd := notifyPodDNS(t, out)
		t.Logf("  рендер %s: объектов notify %d, dnsPolicy %q, dnsConfig %v, KACHO_NOTIFY_STAND_DNS %q", n, objs, policy, hasCfg, sd)
		stand := rows[n].kind == "stand"
		switch {
		case objs > 0 && stand && standDNSOn(facts[n].merged) && (policy != "None" || !hasCfg || sd != standDNSRelease+"-mailpit"):
			t.Errorf("КРАСНЫЙ: %s: (г) notify рендерится на стенде с зоной, а под без dnsPolicy None/dnsConfig "+
				"либо ручка зоны %q не хост приёмника", n, sd)
		case objs > 0 && stand && !standDNSOn(facts[n].merged):
			t.Errorf("КРАСНЫЙ: %s: (г) notify рендерится на профиле stand без зоны DNS стенда — страж DNS "+
				"установки не пройдёт на стенде", n)
		case objs > 0 && !stand && (hasCfg || sd != "off"):
			t.Errorf("КРАСНЫЙ: %s: (г) признак %s, а у пода notify dnsConfig=%v, ручка зоны %q", n, rows[n].kind, hasCfg, sd)
		}
		if n == "a8f60d" && objs != 0 {
			t.Errorf("КРАСНЫЙ: a8f60d: объектов notify %d — по Д102 notify здесь не рендерится до публикации записей", objs)
		}
	}
	if rendered != len(stacks) {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: отрендерено %d профилей из %d", rendered, len(stacks))
	}

	// Ручек обхода у загрузчика нет: ни адреса резолвера, ни пропуска проверки.
	cfgSrc, err := os.ReadFile("../services/notify/internal/config/config.go")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: загрузчик notify не читается: %v", err)
	}
	knobs := regexp.MustCompile(`envconfig:"(KACHO_NOTIFY_[A-Z0-9_]+)"`).FindAllStringSubmatch(string(cfgSrc), -1)
	if len(knobs) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: в загрузчике notify не найдено ни одной ручки — перепись слепа")
	}
	bypass := regexp.MustCompile(`RESOLVER|NAMESERVER|DNS_SERVER|DNS_ADDR|SKIP|BYPASS|DNS_CHECK_DISABLE|DNS_OFF`)
	for _, k := range knobs {
		if bypass.MatchString(k[1]) {
			t.Errorf("КРАСНЫЙ: у загрузчика notify ручка %s — адрес резолвера либо обход проверки DNS", k[1])
		}
	}
	t.Logf("ручек загрузчика notify осмотрено %d, ручек обхода 0 ожидалось", len(knobs))
}

// TestNTF1P12StandDNSProfilesInjections — способность держателя упасть на
// настоящем входе дерева и законный близнец каждой инъекции.
func TestNTF1P12StandDNSProfilesInjections(t *testing.T) {
	stacks := deployStacks(t)
	rows := readStacksMail(t)

	judgeWith := func(layerEdits map[string]func(map[string]any)) []string {
		layers := map[string]map[string]any{}
		for p, edit := range layerEdits {
			l := readYAML(t, filepath.Join(umbrellaDir, p))
			if l == nil {
				l = map[string]any{}
			}
			edit(l)
			layers[p] = l
		}
		_, f := judgeStandDNSProfiles(stacks, rows, allProfileFacts(t, stacks, layers))
		return f
	}
	setStandDNS := func(enabled bool, withAddr bool) func(map[string]any) {
		return func(l map[string]any) {
			sd := map[string]any{"enabled": enabled}
			if withAddr {
				sd["serviceIP"], sd["clusterDomain"] = "10.96.0.53", "cluster.local"
			}
			g, _ := l["global"].(map[string]any)
			if g == nil {
				g = map[string]any{}
				l["global"] = g
			}
			k, _ := g["kacho"].(map[string]any)
			if k == nil {
				k = map[string]any{}
				g["kacho"] = k
			}
			k["standDNS"] = sd
		}
	}
	expectRed := func(name, profile string, got []string) {
		t.Helper()
		for _, f := range got {
			if strings.HasPrefix(f, profile+":") {
				t.Logf("инъекция «%s» → красный: %s", name, f)
				return
			}
		}
		t.Errorf("инъекция «%s»: держатель промолчал о %s (находки: %v)", name, profile, got)
	}

	// Близнец: дерево как есть — находок нет.
	if f := judgeWith(nil); len(f) != 0 {
		t.Fatalf("близнец (дерево как есть) красный — инъекции ниже недействительны: %v", f)
	}
	expectRed("зона включена в values.a8f60d.yaml", "a8f60d",
		judgeWith(map[string]func(map[string]any){"values.a8f60d.yaml": setStandDNS(true, false)}))
	expectRed("зона включена в values.prod.yaml", "prod",
		judgeWith(map[string]func(map[string]any){"values.prod.yaml": setStandDNS(true, true)}))
	expectRed("prorobotech с зоной без своего объявления адреса", "prorobotech",
		judgeWith(map[string]func(map[string]any){"values.prorobotech.yaml": setStandDNS(true, false)}))
	for _, drop := range []string{"fe3455", "a8f60d"} {
		cut := map[string]mailRow{}
		for k, v := range rows {
			if k != drop {
				cut[k] = v
			}
		}
		_, f := judgeStandDNSProfiles(stacks, cut, allProfileFacts(t, stacks, nil))
		expectRed("строка "+drop+" снята из "+stacksMailTable, drop, f)
	}

	// (г) инъекция: ключи пробы a8f60d сняты — notify в рендере a8f60d.
	out, err := renderStandProfile(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{}), stacks["a8f60d"],
		"global.kacho.notifications.modules.notifyProbe.enabled=true", "notify.image.tag=fixture")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: рендер a8f60d с пробой отказал: %v\n%s", err, lastLines(out, 5))
	}
	if objs, _, _, _ := notifyPodDNS(t, out); objs == 0 {
		t.Errorf("инъекция «ключи пробы a8f60d сняты»: notify в рендере не появился — проба (г) судила бы пустоту")
	} else {
		t.Logf("инъекция «ключи пробы a8f60d сняты» → объектов notify %d: (г) a8f60d красный", objs)
	}
}

// standDNSRead — ЧТЕНИЕ ключа зоны шаблоном: `.Values….standDNS` либо ключ
// аргументом `dig`/`index` (`"standDNS"`). Имя ключа в тексте отказа чтением
// не является.
var standDNSRead = regexp.MustCompile(`\.Values[\w.]*\.standDNS\b|"standDNS"`)

// TestNTF1P12StandDNSRenderLayers — (д): зона только у приёмника стенда этого
// релиза; решает рендер, держит старт (CX1-135, Д104, Д106 (а)).
func TestNTF1P12StandDNSRenderLayers(t *testing.T) {
	stacks := deployStacks(t)
	on, err := filepath.Abs(filepath.Join(standDNSFixtureDir, "operator-on.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	off, err := filepath.Abs(filepath.Join(standDNSFixtureDir, "receiver-off.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	operator, err := filepath.Abs(notifyStandaloneSample)
	if err != nil {
		t.Fatal(err)
	}
	base := notifyUmbrellaCopy(t, umbrellaCopyOpts{})

	// Зона поверх узла оператора → отказ рендера с ключом и хостом узла.
	prodOp := append(append([]string{}, stacks["prod"]...), operator, on)
	out, rerr := renderStandProfile(t, base, prodOp)
	if rerr == nil || !strings.Contains(out, "global.kacho.standDNS.enabled") || !strings.Contains(out, "relay.operator.example") {
		t.Errorf("КРАСНЫЙ: зона DNS стенда поверх узла оператора: отказа рендера с ключом и хостом нет (err=%v):\n%s",
			rerr, lastLines(out, 4))
	} else {
		t.Logf("зона поверх узла оператора → отказ рендера: %s", lastLines(out, 2))
	}

	// Близнец: тот же слой поверх dev — под с dnsConfig и ручкой зоны.
	out, rerr = renderStandProfile(t, base, append(append([]string{}, stacks["dev"]...), on))
	if rerr != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец (зона поверх dev) отказал: %v\n%s", rerr, lastLines(out, 4))
	}
	if objs, policy, hasCfg, sd := notifyPodDNS(t, out); objs == 0 || policy != "None" || !hasCfg || sd != standDNSRelease+"-mailpit" {
		t.Errorf("КРАСНЫЙ: близнец (зона поверх dev): объектов %d, dnsPolicy %q, dnsConfig %v, ручка %q", objs, policy, hasCfg, sd)
	}

	// Зона при выключенном приёмнике → отказ блоком (6) с обоими ключами.
	out, rerr = renderStandProfile(t, base, append(append([]string{}, stacks["prod"]...), off))
	if rerr == nil || !strings.Contains(out, "mailpit.enabled") || !strings.Contains(out, "global.kacho.standDNS.enabled") {
		t.Errorf("КРАСНЫЙ: зона при выключенном приёмнике: отказа с mailpit.enabled и global.kacho.standDNS.enabled нет (err=%v):\n%s",
			rerr, lastLines(out, 4))
	}

	// Инъекция: блок (6) стража почтовой полосы снят на копии зонтика —
	// рендер проходит, то есть отказ выше держит ровно этот блок.
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
		"templates/identity-mail-lane-guard.yaml": replaceOnce(
			"{{- if not ((.Values.mailpit).enabled) }}\n{{/* Зона DNS стенда",
			"{{- if false }}\n{{/* Зона DNS стенда"),
	}})
	// Цепочка prod БЕЗ образца узла оператора: слой несёт свой узел целиком, а
	// половина пары удостоверения образца отказала бы раньше блока (6).
	var files []string
	for _, p := range stacks["prod"] {
		files = append(files, filepath.Join(c.umbrella, p))
	}
	files = append(files, off)
	if out, err := c.render(files); err != nil {
		t.Errorf("инъекция «блок (6) снят»: рендер по-прежнему отказывает — отказ держит не этот блок:\n%s", lastLines(out, 3))
	} else {
		t.Log("инъекция «блок (6) снят» → рендер прошёл: отказ выше держит блок (6)")
	}

	// Перепись места решения: ключ зоны читается только в _standdns.tpl.
	var readers []string
	err = filepath.Walk("helm", func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.Contains(p, "/templates/") {
			return err
		}
		b, rerr := os.ReadFile(p) // #nosec G304 -- обход собственного дерева
		if rerr != nil {
			return rerr
		}
		if standDNSRead.Match(b) {
			readers = append(readers, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: обход шаблонов: %v", err)
	}
	if len(readers) != 1 || filepath.Base(readers[0]) != "_standdns.tpl" {
		t.Errorf("КРАСНЫЙ: ключ global.kacho.standDNS читается шаблонами %v — место решения одно, _standdns.tpl", readers)
	}
}

// renderStandProfile — `helm template` ФИКСТУРНОЙ КОПИИ зонтика c цепочкой
// слоёв (имена профилей — от каталога зонтика копии, абсолютные пути — как
// есть) и наборами `--set`. Копия, а не зонтик дерева: file://-зависимости
// (vpc, compute, notify, …) git не ведёт, их материализует владелец
// (notifyUmbrellaCopy → helm-umbrella-deps.sh). Рендер дерева в задании юнитов
// отказывал условием («missing in charts/ directory»), а в рабочей копии
// рендерил архивы, собранные когда-то прежде, — не исходник этой ревизии.
// helm не в PATH — «НЕ ВЫПОЛНИЛОСЬ» при CI (requireHelmForNotify), а не пропуск.
func renderStandProfile(t *testing.T, c umbrellaCopy, chain []string, sets ...string) (string, error) {
	t.Helper()
	requireHelmForNotify(t)
	var files []string
	for _, p := range chain {
		if !filepath.IsAbs(p) {
			p = filepath.Join(c.umbrella, p)
		}
		files = append(files, p)
	}
	return c.render(files, sets...)
}
