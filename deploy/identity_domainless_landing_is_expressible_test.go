// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_domainless_landing_is_expressible_test.go — посадка БЕЗ ДОМЕННОГО
// ИМЕНИ обязана быть ВЫРАЗИМОЙ, и объявленная — исполненной.
//
// # Предмет
//
// Консоль управляемого кластера a8f60d стоит на голом IP-литерале: доменного
// имени у площадки нет, внешнего TLS нет. Пока настройки службы личности никто
// не читал, профиль мог о них молчать безнаказанно; со ствола они монтируются
// вторым `--config`, то есть читаются процессом, и молчание профиля означает
// наследование адресов стенда разработки.
//
// Само по себе это чинится объявлением адреса. Но объявить посадку целиком
// было НЕЧЕМ: ключ `session.cookie.domain` печатался БЕЗУСЛОВНО, а пустое
// значение подменялось умолчанием — то есть host-only печенье не выражалось ни
// при каком входе. Цена именно такая, как выглядит: RFC 6265 §5.1.3 (domain
// matching) объявляет совпадение с IP-литералом ЛОЖНЫМ всегда, а §5.3 п.6
// отбрасывает печенье, чей `Domain` не совпал, — значит любое значение здесь
// заставляет браузер отбросить печенье ЦЕЛИКОМ, и вход не состоится молча.
// Тот же класс у ключей доступа: RP ID по WebAuthn L3 §5.1.3 обязан быть
// валидным доменным именем, у IP-литерала его нет by construction.
//
// # Что здесь утверждается — ТРИ ОСИ
//
//  1. СОГЛАСИЕ ОБЪЯВЛЕНИЯ С ФАКТОМ, в обе стороны: хост внешнего origin есть
//     IP-литерал ⟺ посадка объявлена `domainless: true`. Односторонняя проверка
//     пропустила бы и IP-посадку, забывшую объявиться, и объявление без
//     предмета.
//  2. ИСПОЛНЕНИЕ В РЕНДЕРЕ: посадка без имени НЕ несёт ключа `domain`, не несёт
//     ключей доступа и не несёт ни одного адреса чужой посадки; посадка с
//     именем ключ `domain` несёт и несёт непустым.
//  3. ПРЕДПОСЫЛКА САМОЙ ПРОВЕРКИ: содержимое настроек читается ТОЛЬКО из
//     `.Values.global`, поэтому рендер подчарта kaname с цепочкой профилей
//     умбреллы даёт то же содержимое, что рендер умбреллы. Это не удобство:
//     архивы зависимостей умбреллы git не отслеживает (deploy/.gitignore,
//     `helm/umbrella/charts/*.tgz`), поэтому в свежем клоне умбрелла не
//     рендерится вовсе, а подчарт — отслеживается и рендерится. Замер на
//     ревизии заведения: карта настроек обоих рендеров совпала побайтово
//     (10387 байт). Предпосылку держит утверждение ниже, а не эта запись.
//
// Способность гейта упасть и смолчать доказана инъекцией НАСТОЯЩИМ входом —
// identity_domainless_landing_injection_test.go.
package deploy_test

import (
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const iamSubchartDir = umbrellaDir + "/charts/kaname"

// renderIdentitySubchart рендерит подчарт kaname с перечисленными файлами
// значений и точечными установками.
//
// Отсутствие helm в CI — жёсткий провал, а не пропуск: гейт, молча ставший
// инертным на джобе, гейтящей мёрж, гейтом не является. Та же дисциплина, что у
// helm/umbrella/iam_lane_service_aud_test.go.
func renderIdentitySubchart(t *testing.T, valueFiles []string, sets ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	args := []string{"template", "kacho-umbrella", iamSubchartDir, "-n", "kacho"}
	for _, f := range valueFiles {
		args = append(args, "-f", filepath.Join(umbrellaDir, f))
	}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	return string(out), err
}

// identityConfigOf достаёт из рендера ТЕЛО настроек службы личности
// (`kratos.yaml` карты настроек) и разбирает его как YAML.
func identityConfigOf(t *testing.T, rendered string) (map[string]any, string) {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	docs := 0
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if err != nil {
			break
		}
		docs++
		if doc == nil {
			continue
		}
		data, _ := doc["data"].(map[string]any)
		body, ok := data["kratos.yaml"].(string)
		if !ok {
			continue
		}
		var cfg map[string]any
		if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
			t.Fatalf("тело настроек личности не разбирается как YAML: %v", err)
		}
		return cfg, body
	}
	t.Fatalf("в рендере нет карты настроек с ключом kratos.yaml (документов разобрано %d, байт %d) — "+
		"вердикта НЕТ: «ключа domain не найдено» здесь неотличимо от «настроек не найдено вовсе»",
		docs, len(rendered))
	return nil, ""
}

// sessionCookieDomain — ЕДИНСТВЕННЫЙ дискриминатор гейта: печатается ли ключ
// `session.cookie.domain` и с какой величиной. Зовётся и переписью по дереву, и
// инъекцией — копии предиката не заводится.
func sessionCookieDomain(cfg map[string]any) (value string, present bool) {
	session, _ := cfg["session"].(map[string]any)
	cookie, _ := session["cookie"].(map[string]any)
	if cookie == nil {
		return "", false
	}
	v, ok := cookie["domain"]
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

// webauthnEnabled — объявлена ли полоса ключей доступа и её `rp.id`.
func webauthnEnabled(cfg map[string]any) (enabled bool, rpID string, declared bool) {
	ss, _ := cfg["selfservice"].(map[string]any)
	methods, _ := ss["methods"].(map[string]any)
	wa, _ := methods["webauthn"].(map[string]any)
	if wa == nil {
		return false, "", false
	}
	e, _ := wa["enabled"].(bool)
	conf, _ := wa["config"].(map[string]any)
	rp, _ := conf["rp"].(map[string]any)
	id, _ := rp["id"].(string)
	return e, id, true
}

// browserFacingURLs — абсолютные адреса, которые исполняет БРАУЗЕР. Собираются
// ПО ИМЕНАМ КЛЮЧЕЙ, а не обходом поддерева: рядом, в том же `selfservice`, живут
// адреса обратных вызовов НА ВНУТРЕННИЙ слушатель (`hook.config.url`), и обход
// поддерева объявил бы их чужой посадкой — обвинил бы исправное.
func browserFacingURLs(cfg map[string]any) []string {
	browserKeys := map[string]bool{
		"ui_url":                     true,
		"default_browser_return_url": true,
		"allowed_return_urls":        true,
		"origins":                    true,
	}
	var out []string
	var collect func(any)
	collect = func(v any) {
		switch t := v.(type) {
		case []any:
			for _, sub := range t {
				collect(sub)
			}
		case string:
			if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
				out = append(out, t)
			}
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, sub := range t {
				if browserKeys[k] {
					collect(sub)
					continue
				}
				walk(sub)
			}
		case []any:
			for _, sub := range t {
				walk(sub)
			}
		}
	}
	ss, _ := cfg["selfservice"].(map[string]any)
	walk(ss)
	serve, _ := cfg["serve"].(map[string]any)
	public, _ := serve["public"].(map[string]any)
	cors, _ := public["cors"].(map[string]any)
	collect(cors["allowed_origins"])
	sort.Strings(out)
	return out
}

// identityOfStack — действующие значения службы личности для стека: профили
// накладываются слева направо, ровно как их получает helm, поверх умолчаний
// чарта.
func identityOfStack(t *testing.T, chain []string) map[string]any {
	t.Helper()
	merged := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	for _, p := range chain {
		merged = mergeValues(merged, readYAML(t, filepath.Join(umbrellaDir, p)))
	}
	node, ok := lookup(merged, "global", "kacho", "identity")
	if !ok {
		t.Fatalf("у стека %v нет узла global.kacho.identity — сверять нечего, и это отказ", chain)
	}
	id, _ := node.(map[string]any)
	return id
}

// externalOriginHost — хост внешнего origin посадки. Пустой `appBaseURL`
// означает «вывести из домена» — ровно то, что делает шаблон.
func externalOriginHost(t *testing.T, id map[string]any) string {
	t.Helper()
	raw, _ := id["appBaseURL"].(string)
	if strings.TrimSpace(raw) == "" {
		sub, _ := id["appSubdomain"].(string)
		dom, _ := id["domain"].(string)
		return sub + "." + dom
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("appBaseURL %q не разбирается как адрес: %v", raw, err)
	}
	return u.Hostname()
}

// Вердикты согласия объявления с фактом. Строки, а не булево: «не сходится»
// имеет ДВЕ стороны, и они чинятся по-разному.
const (
	landingAgrees        = "сходится"
	landingIPNotDeclared = "IP-посадка не объявлена"
	landingDeclaredNoIP  = "объявление без предмета"
)

// landingDeclarationVerdict — ЕДИНСТВЕННЫЙ адъюдикатор оси 1. Зовётся и
// переписью по дереву, и инъекцией: копии предиката не заводится.
func landingDeclarationVerdict(originHost string, declared bool) string {
	isIP := net.ParseIP(originHost) != nil
	switch {
	case isIP && !declared:
		return landingIPNotDeclared
	case !isIP && declared:
		return landingDeclaredNoIP
	default:
		return landingAgrees
	}
}

func TestIdentity_LandingWithoutADomainNameIsExpressible(t *testing.T) {
	stacks := deployStacks(t)

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var domainless, withDomain, urlsChecked, bytesRead int
	for _, name := range names {
		chain := stacks[name]
		id := identityOfStack(t, chain)
		host := externalOriginHost(t, id)
		declared, _ := id["domainless"].(bool)

		// ── ось 1: объявление сходится с фактом, В ОБЕ СТОРОНЫ ───────────────
		switch landingDeclarationVerdict(host, declared) {
		case landingIPNotDeclared:
			t.Errorf("стек %q: внешний origin — IP-литерал %s, а посадка НЕ объявлена "+
				"`global.kacho.identity.domainless: true`. Печенье сессии получит ключ "+
				"`domain`, браузер отбросит его целиком (RFC 6265 §5.1.3), и вход не "+
				"состоится молча", name, host)
		case landingDeclaredNoIP:
			t.Errorf("стек %q: посадка объявлена без доменного имени, а хост внешнего "+
				"origin — %s, то есть доменное имя у неё есть. Объявление потеряло "+
				"предмет: печенье станет host-only и ключи доступа выключатся там, где "+
				"и то и другое выразимо", name, host)
		}

		out, err := renderIdentitySubchart(t, chain)
		if err != nil {
			t.Fatalf("стек %q: рендер отказал: %v\n%s", name, err, out)
		}
		cfg, body := identityConfigOf(t, out)
		bytesRead += len(body)

		// ── ось 2: рендер исполняет объявленное ─────────────────────────────
		value, present := sessionCookieDomain(cfg)
		waOn, rpID, waDeclared := webauthnEnabled(cfg)
		if !waDeclared {
			t.Errorf("стек %q: полоса ключей доступа не объявлена вовсе — состояние, "+
				"которого не выбирал никто", name)
		}
		if declared {
			domainless++
			if present {
				t.Errorf("стек %q: посадка без доменного имени, а ключ session.cookie.domain "+
					"напечатан (%q). Браузер отбросит печенье целиком", name, value)
			}
			if waOn {
				t.Errorf("стек %q: посадка без доменного имени, а полоса ключей доступа "+
					"включена (rp.id = %q). RP ID обязан быть валидным доменным именем "+
					"(WebAuthn L3 §5.1.3) — полоса отвергалась бы на каждой попытке", name, rpID)
			}
		} else {
			withDomain++
			if !present {
				t.Errorf("стек %q: посадка с доменным именем, а ключа session.cookie.domain "+
					"нет. Печенье стало host-only на посадке, где это никто не решал", name)
			} else if strings.TrimSpace(value) == "" {
				t.Errorf("стек %q: ключ session.cookie.domain напечатан пустым — "+
					"провайдер отвергнет настройки", name)
			}
			if !waOn {
				t.Errorf("стек %q: полоса ключей доступа выключена на посадке с доменным "+
					"именем — способ входа снят там, где он выразим", name)
			}
		}

		// ── ось 2b: ни одного адреса ЧУЖОЙ посадки ──────────────────────────
		urls := browserFacingURLs(cfg)
		if len(urls) == 0 {
			t.Errorf("стек %q: браузерных адресов в настройках не найдено ни одного — "+
				"вердикта нет, «все свои» неотличимо от «ни одного прочитанного»", name)
		}
		for _, raw := range urls {
			urlsChecked++
			u, err := url.Parse(raw)
			if err != nil {
				t.Errorf("стек %q: адрес %q не разбирается: %v", name, raw, err)
				continue
			}
			if u.Hostname() != host {
				t.Errorf("стек %q: браузерный адрес %q ведёт на %s, тогда как внешний "+
					"origin посадки — %s. Профиль молчит о своей посадке и наследует "+
					"чужую", name, raw, u.Hostname(), host)
			}
		}
	}

	t.Logf("перепись: стеков осмотрено %d · без доменного имени %d · с доменным именем %d · "+
		"браузерных адресов сверено %d · тела настроек прочитано байт %d",
		len(names), domainless, withDomain, urlsChecked, bytesRead)
	if domainless == 0 || withDomain == 0 {
		t.Fatalf("перепись односторонняя (без имени %d, с именем %d) — утверждение о "+
			"РАЗЛИЧИИ посадок не проверено ни на одной паре", domainless, withDomain)
	}
}

// TestIdentity_ConfigBodyReadsOnlyGlobalValues — предпосылка оси 3.
//
// Рендер подчарта в одиночку равен рендеру умбреллы ровно потому, что тело
// настроек не читает НИЧЕГО, кроме `.Values.global` (и `.Release`). Появится
// ссылка на значения подчарта — предпосылка станет ложной, а гейт выше начнёт
// судить не тот текст, который едет на стенд.
func TestIdentity_ConfigBodyReadsOnlyGlobalValues(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(identityConfigTemplate))
	if err != nil {
		t.Fatalf("объявление настроек личности не прочитано (%s): %v", identityConfigTemplate, err)
	}
	lines := strings.Split(string(raw), "\n")

	// Тела режутся ПО ЗАГОЛОВКАМ объявлений, а не по первому `{{- end -}}`:
	// внутри тела стоят свои условия со своими `end`, и разбор «до первого
	// конца» обрывался бы на них. Поймано на себе — перепись показала три
	// ссылки там, где их двенадцать, и «нарушений нет» означало «не дочитано».
	type body struct {
		from, to int
	}
	want := []string{
		`{{- define "kacho.identity.hooksAuthority" -}}`,
		`{{- define "kacho.identity.configYaml" -}}`,
		`{{- define "kacho.identity.schemaJSON" -}}`,
	}
	starts := map[string]int{}
	var defineLines []int
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimRight(ln, " "), "{{- define ") {
			defineLines = append(defineLines, i)
			starts[strings.TrimRight(ln, " ")] = i
		}
	}
	bodies := map[string]body{}
	for _, w := range want {
		i, ok := starts[w]
		if !ok {
			t.Fatalf("объявления %s в шаблоне НЕТ — предпосылка исчезла, а не подтвердилась", w)
		}
		end := len(lines)
		for _, d := range defineLines {
			if d > i && d < end {
				end = d
			}
		}
		bodies[w] = body{from: i + 1, to: end}
	}

	var refs, offenders, scanned int
	for name, b := range bodies {
		for _, ln := range lines[b.from:b.to] {
			scanned++
			for _, tail := range strings.Split(ln, ".Values.")[1:] {
				refs++
				if !strings.HasPrefix(tail, "global") {
					offenders++
					t.Errorf("%s читает `.Values.%s…` — не из `global`. Значение подчарта "+
						"невидимо из контекста подчарта провайдера, где считается отпечаток "+
						"содержимого, и невидимо из рендера подчарта, которым судит гейт выше",
						name, strings.SplitN(strings.SplitN(tail, " ", 2)[0], "}", 2)[0])
				}
			}
		}
	}
	if refs == 0 {
		t.Fatal("ссылок на значения не прочитано ни одной — «нарушений нет» здесь " +
			"неотличимо от «ничего не прочитано»")
	}
	t.Logf("перепись: строк шаблона %d · объявлений тела %d · строк тела осмотрено %d · "+
		"ссылок на значения %d · вне `global` %d",
		len(lines), len(bodies), scanned, refs, offenders)
}
