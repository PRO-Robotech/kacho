// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_serving_precedence_test.go — адрес потока личности обязан приходить
// в полосу потоков, а не в запасной путь статики и не в заглушку
// одностраничного приложения.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Раздача консоли выбирает блок НЕ по порядку в файле. Порядок у неё свой, и он
// перечислен в её же руководстве: точное совпадение · самый длинный префикс ·
// регулярки в порядке объявления · запомненный префикс. Из этого следует то,
// чего чтение сверху вниз не показывает:
//
//   - РЕГУЛЯРКА ПОБЕЖДАЕТ ОБЫЧНЫЙ ПРЕФИКС, где бы она ни стояла в файле;
//   - победу префикса даёт только пометка `^~`: она прекращает разбор регулярок;
//   - между собой регулярки решает ТЕКСТОВЫЙ порядок, и вот он читается сверху
//     вниз. Полоса потоков и регулярка статики — обе регулярки, поэтому адрес
//     под сегментом потока с расширением статики (`/login/…/x.js`) достаётся
//     той, что объявлена ВЫШЕ.
//
// Прежде проба судила и префиксный блок к публичному краю службы личности:
// обычный префикс проигрывал регулярке статики, и вспомогательный сценарий
// беспарольного входа уезжал в запасной путь (измерено на настоящем nginx
// 2026-08-24). Блок снят из раздачи вместе со своим адресом (#2733) — ни одна
// цепочка его не рендерила, — и половина пробы о нём снята вместе с предметом.
//
// Заглушка. Адрес, не покрытый ни одной полосой, попадает
// в `location /`, а та отдаёт `index.html` с кодом `200`. Отказ тогда выглядит
// УСПЕХОМ: браузер получает двухсотый код и пустую оболочку, маршрутизатор
// которой такого пути не знает и уводит на главную. Ни `404`, ни записи в
// журнале — только «страница не открывается». Поэтому каждый сегмент, который
// полоса потоков объявляет, обязан в неё же и приходить.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ПРОБА ЧИТАЕТ И ЧЕГО НЕ ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЕ — шаблон конфигурации из чарта консоли. Ни кластера, ни
// helm, ни сети: рендер требует инструмента развёртывания, которого в наборе
// проб нет, и проба, зависящая от него, пропускалась бы молча — а молчание
// пропущенной пробы неотличимо от зелёного.
//
// Разбор идёт по ИСПОЛНЯЕМОЙ части: вставки шаблона `{{ … }}` и подстановки
// окружения `${…}` снимаются перед счётом вложенности, потому что их фигурные
// скобки принадлежат не раздаче. Строка, где имя ручки названо прозой в
// комментарии, читателем не является.
//
// Множества выводятся из дерева, а не выписываются здесь: восходящие узлы
// службы личности — по имени переменной окружения, расширения статики — из
// самой регулярки, сегменты потоков — из самой полосы. Форма, которую разбор не
// понимает, ОТВЕРГАЕТСЯ отказом, а не пропускается: разбор, молча пропускающий
// непонятое, превращает «ноль находок» в «ноль прочитанного».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОБА НЕ УТВЕРЖДАЕТ
//
// Согласия полосы потоков с адресами, которые объявляет сама служба личности
// (`ui_url:` в её чартах), — это предмет `deploy/identity_flow_path_is_served_test.go`,
// и здесь он не пересказывается: два места об одном предмете расходятся молча.
// Здесь предмет — ПОРЯДОК разрешения внутри раздачи, которого не утверждает
// никто.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// servingTemplateRel — объявление раздачи консоли относительно корня дерева.
var servingTemplateRel = filepath.Join("ui-future", "deploy", "templates", "configmap-nginx.yaml")

// helmActionRe — исполняемая вставка шаблона. Её фигурные скобки принадлежат
// helm, а не раздаче, и в счёт вложенности блоков не идут.
var helmActionRe = regexp.MustCompile(`\{\{-?.*?-?\}\}`)

// envRefRe — подстановка окружения `${ИМЯ}`: скобки принадлежат envsubst.
var envRefRe = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

// identityUpstreamRe — восходящий узел службы личности, названный блоком.
// Признак — ИМЯ переменной окружения, а не адрес: адрес задаёт профиль, и
// выписывать его здесь значило бы завести вторую копию профиля.
var identityUpstreamRe = regexp.MustCompile(`\$\{(KACHO_UI_(?:KRATOS|HYDRA)[A-Z0-9_]*_UPSTREAM)\}`)

// namedFallbackRe — уход в именованный блок: `try_files … @имя;`.
var namedFallbackRe = regexp.MustCompile(`try_files\s+[^;]*(@[A-Za-z0-9_]+)\s*;`)

// serverOpenRe / locationOpenRe — открытие блоков в исполняемой части.
var (
	serverOpenRe   = regexp.MustCompile(`^\s*server\s*\{\s*$`)
	locationOpenRe = regexp.MustCompile(`^\s*location\s+(\S+)(?:\s+(\S+))?\s*\{\s*$`)
)

// extAlternationRe — перечень расширений статики: `\.(a|b|c)$`, в том числе в
// незахватывающей форме `\.(?:a|b|c)$` — обе встречаются в этом же файле, и
// разбор, спотыкающийся о вторую, краснел бы на законной форме из дерева.
var extAlternationRe = regexp.MustCompile(`\\\.\((?:\?:)?([^()]*)\)\$$`)

// plainExtRe — понимаемая форма одного расширения.
var plainExtRe = regexp.MustCompile(`^[a-z0-9]+$`)

// bandSegmentsRe — перечень сегментов полосы потоков: `^/(a|b|c)(/|$)`.
var bandSegmentsRe = regexp.MustCompile(`^\^/\(([^()]*)\)\(/\|\$\)$`)

// nginxLoc — один блок раздачи в том виде, в каком её разрешение его читает.
type nginxLoc struct {
	mod  string // "=", "^~", "~", "~*", "@" либо "" (обычный префикс)
	spec string
	line int
	body string
	re   *regexp.Regexp // только для "~" и "~*"
}

// name — как блок называется в находке.
func (l nginxLoc) name() string {
	if l.mod == "" {
		return fmt.Sprintf("`location %s` (строка %d)", l.spec, l.line)
	}
	return fmt.Sprintf("`location %s %s` (строка %d)", l.mod, l.spec, l.line)
}

// nginxServer — один серверный блок и его блоки в порядке объявления.
type nginxServer struct {
	line int
	locs []nginxLoc
}

// executablePart — строка без того, что раздаче не принадлежит.
func executablePart(line string) string {
	return envRefRe.ReplaceAllString(helmActionRe.ReplaceAllString(line, ""), "")
}

// parseServingTemplate — серверные блоки и их блоки из объявления раздачи.
func parseServingTemplate(t *testing.T, text string) []nginxServer {
	t.Helper()

	var servers []nginxServer
	depth, inServer, curLoc, locDepth := 0, -1, -1, 0

	for i, raw := range strings.Split(text, "\n") {
		code := executablePart(raw)
		delta := strings.Count(code, "{") - strings.Count(code, "}")

		switch {
		case depth == 0 && serverOpenRe.MatchString(code):
			servers = append(servers, nginxServer{line: i + 1})
			inServer = len(servers) - 1
			depth += delta
			continue

		case inServer >= 0 && curLoc < 0 && depth == 1:
			if m := locationOpenRe.FindStringSubmatch(code); m != nil {
				loc := makeLoc(t, m[1], m[2], i+1)
				servers[inServer].locs = append(servers[inServer].locs, loc)
				curLoc = len(servers[inServer].locs) - 1
				locDepth = depth
				depth += delta
				continue
			}
		}

		if curLoc >= 0 {
			servers[inServer].locs[curLoc].body += raw + "\n"
		}
		depth += delta
		if curLoc >= 0 && depth <= locDepth {
			curLoc = -1
		}
		if inServer >= 0 && depth <= 0 {
			inServer, depth = -1, 0
		}
	}
	return servers
}

// makeLoc — разбор объявления блока. Непонятая форма — отказ, а не пропуск.
func makeLoc(t *testing.T, tok, arg string, line int) nginxLoc {
	t.Helper()

	l := nginxLoc{line: line}
	switch tok {
	case "=", "^~", "~", "~*":
		l.mod, l.spec = tok, arg
	default:
		if strings.HasPrefix(tok, "@") {
			l.mod, l.spec = "@", tok
		} else {
			l.mod, l.spec = "", tok
		}
	}
	if l.spec == "" {
		t.Fatalf("строка %d: у блока `location %s` нет предмета — разбор не понимает эту форму, "+
			"а молчаливый пропуск сделал бы «ноль находок» неотличимым от «ноль прочитанного»", line, tok)
	}
	if l.mod == "~" || l.mod == "~*" {
		src := l.spec
		if l.mod == "~*" {
			src = "(?i)" + src
		}
		re, err := regexp.Compile(src)
		if err != nil {
			t.Fatalf("строка %d: регулярка блока %q не разбирается (%v) — форма изменилась, "+
				"и порядок разрешения больше не установлен", line, l.spec, err)
		}
		l.re = re
	}
	return l
}

// selectLocation — какой блок раздача выберет для этого адреса.
//
// Порядок разрешения — тот же, что у самой раздачи: точное совпадение · самый
// длинный префикс (и, если он помечен `^~`, разбор регулярок прекращается) ·
// регулярки в порядке объявления · запомненный префикс. Именованные блоки
// (`@имя`) по адресу не выбираются — в них уходят только изнутри.
//
// Регулярки разбираются здешним движком, а не тем, что в раздаче. Формы, что
// есть в дереве (`^/(a|b)(/|$)`, `\.(js|css)$`), понимаются обоими одинаково;
// форма, которую здешний движок не понял, отвергается отказом в makeLoc.
func selectLocation(uri string, locs []nginxLoc) (int, string) {
	for i, l := range locs {
		if l.mod == "=" && l.spec == uri {
			return i, "точное совпадение"
		}
	}

	best := -1
	for i, l := range locs {
		if l.mod != "" && l.mod != "^~" {
			continue
		}
		if strings.HasPrefix(uri, l.spec) && (best < 0 || len(l.spec) > len(locs[best].spec)) {
			best = i
		}
	}
	if best >= 0 && locs[best].mod == "^~" {
		return best, "самый длинный префикс, помеченный `^~` — регулярки не разбирались"
	}

	for i, l := range locs {
		if l.re != nil && l.re.MatchString(uri) {
			return i, "первая совпавшая регулярка"
		}
	}
	if best >= 0 {
		return best, "самый длинный префикс (ни одна регулярка не совпала)"
	}
	return -1, "не выбран ни один блок"
}

// expandExtensions — расширения из перечня регулярки статики.
func expandExtensions(reSrc string) ([]string, error) {
	m := extAlternationRe.FindStringSubmatch(reSrc)
	if m == nil {
		return nil, fmt.Errorf("перечень расширений в %q не разбирается формой `\\.(…)$`", reSrc)
	}
	var out []string
	for _, tok := range strings.Split(m[1], "|") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		optional := strings.HasSuffix(tok, "?")
		base := strings.TrimSuffix(tok, "?")
		if !plainExtRe.MatchString(base) {
			return nil, fmt.Errorf("расширение %q записано формой, которую разбор не понимает", tok)
		}
		if optional {
			out = append(out, base[:len(base)-1])
		}
		out = append(out, base)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("перечень расширений в %q пуст", reSrc)
	}
	sort.Strings(out)
	return out, nil
}

// bandSegments — сегменты, которые полоса потоков объявляет своими.
func bandSegments(reSrc string) ([]string, error) {
	m := bandSegmentsRe.FindStringSubmatch(reSrc)
	if m == nil {
		return nil, fmt.Errorf("полоса потоков %q не разбирается формой `^/(…)(/|$)`", reSrc)
	}
	var out []string
	for _, seg := range strings.Split(m[1], "|") {
		if seg = strings.TrimSpace(seg); seg != "" {
			out = append(out, seg)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("полоса потоков %q не назвала ни одного сегмента", reSrc)
	}
	sort.Strings(out)
	return out, nil
}

// servingTemplate — объявление раздачи, разобранное на серверные блоки.
func servingTemplate(t *testing.T) ([]nginxServer, string) {
	t.Helper()
	path := filepath.Join(repoRootFromTest(t), servingTemplateRel)
	body, err := os.ReadFile(path) // #nosec G304 -- путь собран из корня этого дерева
	if err != nil {
		t.Fatalf("объявление раздачи %s не читается (%v) — предпосылка пробы исчезла", path, err)
	}
	return parseServingTemplate(t, string(body)), path
}

// TestIdentityRequestReachesTheIdentityServiceNotTheFallback — предмет ПОРЯДКА.
//
// Утверждается не наличие блоков, а то, КАКОЙ из них выигрывает: наличие обоих
// при неверном порядке даёт ровно тот дефект, ради которого проба заведена.
//
// Префиксного блока к службе личности в раздаче больше нет: полоса к публичному
// краю поставщика (`/.ory/…`) не рендерилась ни на одной цепочке и снята из
// чарта вместе со своим адресом (#2733). Вместе с ней снята и половина этой
// пробы, судившая порядок «префикс службы против регулярки статики», — её
// предмета не стало. Возврат такого блока ловят убывающий потолок привязок
// (kacho#2730: строка с именем поставщика) и рендер-гейт посадки
// (`deploy/tests/helm/console-serves-identity-flows-test.sh`: полоса `/.ory/…`
// на посадке own — находка).
func TestIdentityRequestReachesTheIdentityServiceNotTheFallback(t *testing.T) {
	servers, path := servingTemplate(t)

	locsTotal, staticTotal, bandTotal, checks := 0, 0, 0, 0

	for _, srv := range servers {
		locsTotal += len(srv.locs)

		var bands []int
		exts := map[string]bool{}
		for i, l := range srv.locs {
			if l.re != nil && identityUpstreamRe.MatchString(l.body) {
				bands = append(bands, i)
			}
			if l.re != nil && namedFallbackRe.MatchString(l.body) {
				got, err := expandExtensions(l.spec)
				if err != nil {
					t.Fatalf("%s: %v — форма изменилась, и множество расширений больше не выводится "+
						"из дерева; молчаливый пропуск здесь означал бы проверку без предмета", path, err)
				}
				for _, e := range got {
					exts[e] = true
				}
			}
		}
		bandTotal += len(bands)
		staticTotal += len(exts)

		// Сегмент полосы потоков обязан прийти в полосу, а не в заглушку.
		for _, bi := range bands {
			segs, err := bandSegments(srv.locs[bi].spec)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			for _, seg := range segs {
				// Адреса ПОД сегментом берутся с расширениями статики намеренно:
				// полоса объявляет своим и вложенный путь (`(/|$)`), а между
				// собой регулярки решает ТЕКСТОВЫЙ порядок. Без такого адреса
				// утверждение о порядке было бы вакуумным — перестановка полосы
				// и статики не изменила бы ничего в проверяемом множестве.
				probes := []string{"/" + seg, "/" + seg + "/"}
				for _, ext := range sortedKeys(exts) {
					probes = append(probes, "/"+seg+"/probe."+ext)
				}
				for _, uri := range probes {
					got, why := selectLocation(uri, srv.locs)
					checks++
					if got == bi {
						continue
					}
					winner, extra := "никакой", ""
					if got >= 0 {
						winner = srv.locs[got].name()
						if srv.locs[got].mod == "" && srv.locs[got].spec == "/" {
							extra = " Это заглушка одностраничного приложения: она отдаёт `index.html` " +
								"с кодом `200`, поэтому ОТКАЗ ВЫГЛЯДИТ УСПЕХОМ — браузер получает " +
								"двухсотый код и пустую оболочку, а маршрутизатор уводит на главную."
						}
					}
					t.Errorf("серверный блок со строки %d: адрес потока %q объявлен полосой %s, "+
						"но достаётся %s (%s).%s",
						srv.line, uri, srv.locs[bi].name(), winner, why, extra)
				}
			}
		}
	}

	t.Logf("осмотрено: %s; серверных блоков %d; блоков раздачи %d; из них отдают запросы службе "+
		"личности регуляркой-полосой %d; расширений статики %d; проверено разрешений адреса %d",
		path, len(servers), locsTotal, bandTotal, staticTotal, checks)

	switch {
	case len(servers) == 0:
		t.Fatal("в объявлении не найдено ни одного серверного блока — прочитано ноль, " +
			"и молчание пробы не является утверждением о раздаче")
	case bandTotal == 0:
		t.Fatal("не найдено полосы потоков личности — предпосылка пробы исчезла")
	case staticTotal == 0:
		t.Fatal("не найдено регулярки статики с уходом в именованный блок — сталкивать " +
			"порядок не с чем, и «всё сошлось» здесь означало бы «нечему было сходиться»")
	case checks == 0:
		t.Fatal("не проверено ни одного разрешения адреса")
	}
}

// TestServingPrecedenceDiscriminatorCutsBothWays — разрешение обязано ловить
// неверный порядок и МОЛЧАТЬ на законном близнеце той же формы.
//
// Обе оси проверяются раздельно, потому что причины у них разные: префикс
// проигрывает регулярке из-за ОТСУТСТВИЯ пометки, а регулярка регулярке — из-за
// ТЕКСТОВОГО порядка. Проба, покрывающая обе одной фикстурой, на починке одной
// оси зеленела бы по второй.
func TestServingPrecedenceDiscriminatorCutsBothWays(t *testing.T) {
	// %s — пометка блока службы личности: пусто (обычный префикс) либо `^~`.
	const prefixShape = `
server {
    location %s /.ory/probe/public/ {
        set $probe_upstream "${KACHO_UI_KRATOS_UI_UPSTREAM}";
        proxy_pass http://$probe_upstream;
    }

    location ~* \.(js|css)$ {
        try_files $uri @probe_fallback;
    }

    location @probe_fallback {
        set $probe_fallback "${KACHO_UI_KRATOS_UI_UPSTREAM}";
        proxy_pass http://$probe_fallback;
    }

    location / {
        try_files $uri $uri/ /index.html;
    }
}
`
	const wantURI = "/.ory/probe/public/probe.js"

	bare := parseServingTemplate(t, fmt.Sprintf(prefixShape, ""))
	if len(bare) != 1 {
		t.Fatalf("разбор фикстуры дал %d серверных блоков вместо одного", len(bare))
	}
	got, why := selectLocation(wantURI, bare[0].locs)
	if got < 0 || bare[0].locs[got].re == nil {
		t.Fatalf("дефект не воспроизведён: %q достался %v (%s), а обязан был достаться "+
			"регулярке статики — иначе проба не способна упасть на настоящем дефекте", wantURI, got, why)
	}

	marked := parseServingTemplate(t, fmt.Sprintf(prefixShape, "^~"))
	got, why = selectLocation(wantURI, marked[0].locs)
	if got < 0 || marked[0].locs[got].mod != "^~" {
		t.Fatalf("законный близнец не молчит: с пометкой `^~` адрес %q обязан достаться блоку "+
			"службы личности, а достался %v (%s)", wantURI, got, why)
	}

	// Вторая ось — текстовый порядок двух регулярок.
	const bandFirst = `
server {
    location ~ ^/(login|registration)(/|$) {
        set $probe_ui "${KACHO_UI_KRATOS_UI_UPSTREAM}";
        proxy_pass http://$probe_ui;
    }

    location ~* \.(js|css)$ {
        try_files $uri @probe_fallback;
    }

    location @probe_fallback {
        proxy_pass http://probe;
    }
}
`
	const staticFirst = `
server {
    location ~* \.(js|css)$ {
        try_files $uri @probe_fallback;
    }

    location ~ ^/(login|registration)(/|$) {
        set $probe_ui "${KACHO_UI_KRATOS_UI_UPSTREAM}";
        proxy_pass http://$probe_ui;
    }

    location @probe_fallback {
        proxy_pass http://probe;
    }
}
`
	const styled = "/registration/style.css"

	ok := parseServingTemplate(t, bandFirst)
	if got, why = selectLocation(styled, ok[0].locs); got != 0 {
		t.Fatalf("законный порядок объявлен неверным: %q обязан достаться полосе потоков, "+
			"а достался блоку %d (%s)", styled, got, why)
	}
	swapped := parseServingTemplate(t, staticFirst)
	if got, why = selectLocation(styled, swapped[0].locs); got != 0 {
		t.Fatalf("перестановка регулярок не поймана: %q при статике, объявленной первой, обязан "+
			"достаться ЕЙ, а достался блоку %d (%s) — значит разрешение не читает текстовый порядок",
			styled, got, why)
	}
	if swapped[0].locs[0].re == nil {
		t.Fatal("фикстура перестановки собрана неверно: первым блоком стоит не регулярка")
	}
}

// TestExtensionExpansionRejectsFormsItCannotRead — разбор перечня расширений
// обязан отвергать непонятое, а не пропускать его молча.
func TestExtensionExpansionRejectsFormsItCannotRead(t *testing.T) {
	want := []string{"css", "js", "woff", "woff2"}
	// Обе формы перечня, что есть в дереве: захватывающая — у полосы статики
	// оболочки, незахватывающая — у модулей.
	for _, good := range []string{`\.(js|css|woff2?)$`, `\.(?:js|css|woff2?)$`} {
		got, err := expandExtensions(good)
		if err != nil {
			t.Fatalf("понимаемая форма %q отвергнута: %v", good, err)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("форма %q разобрана неверно: %v вместо %v", good, got, want)
		}
	}
	for _, bad := range []string{`\.(js|[a-z]+)$`, `\.(js|c.s)$`, `(js|css)`} {
		if _, err := expandExtensions(bad); err == nil {
			t.Errorf("форма %q принята разбором, хотя он её не понимает — молчаливый пропуск "+
				"превратил бы «ноль находок» в «ноль прочитанного»", bad)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ВТОРАЯ ПОЛОВИНА — ПОКРЫТИЕ, а не порядок
//
// Проверка выше утверждает, куда приходит адрес, который полоса объявила своим.
// Про то, КАКИЕ адреса полоса обязана объявить, она не говорит ничего: полоса,
// потерявшая сегмент, осталась бы зелёной — сегмента просто не стало бы в
// проверяемом множестве. А потерянный сегмент это ровно тот дефект, который
// описан выше: адрес уезжает в заглушку и отказ выглядит успехом.
//
// Внешний источник «что обязано быть покрыто» — ВТОРОЕ объявление той же
// полосы, которое консоль держит для разработки (перечень путей, отдаваемых
// службе личности сборщиком). Две полосы одного механизма обязаны сверяться
// МЕЖДУ СОБОЙ: расхождение здесь означает, что в разработке и на стенде продукт
// ведёт себя по-разному, и решал это никто.
//
// Согласие полосы с адресами, которые объявляет сама служба личности
// (`ui_url:`), держит `deploy/identity_flow_path_is_served_test.go` — третьего
// места об этом предмете здесь не заводится.

// devProxyBandRe — объявление полосы в конфигурации сборщика.
var devProxyBandRe = regexp.MustCompile(`(?s)kratosUiRoutes\s*=\s*\[(.*?)\]`)

// devProxyRouteRe — один путь в этом объявлении.
var devProxyRouteRe = regexp.MustCompile(`"/([A-Za-z0-9_-]+)"`)

// TestServingBandCoversEveryRouteTheConsoleSendsToTheIdentityService — множества
// сегментов двух объявлений одной полосы обязаны СОВПАДАТЬ.
//
// Требуется равенство, а не покрытие в одну сторону: сегмент, объявленный
// раздачей и неизвестный сборщику, — та же несогласованность с другого конца.
func TestServingBandCoversEveryRouteTheConsoleSendsToTheIdentityService(t *testing.T) {
	root := repoRootFromTest(t)

	// Полоса раздачи.
	servers, path := servingTemplate(t)
	serving, bandsFound := map[string]bool{}, 0
	for _, srv := range servers {
		for _, l := range srv.locs {
			if l.re == nil || !identityUpstreamRe.MatchString(l.body) {
				continue
			}
			segs, err := bandSegments(l.spec)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			bandsFound++
			for _, s := range segs {
				serving[s] = true
			}
		}
	}

	// Полоса сборщика — по составу индекса, а не обходом диска.
	files, err := treecorpus.Under(filepath.Join(root, "ui-future"))
	if err != nil {
		t.Fatalf("состав ui-future: %v — без индекса «ноль находок» неотличимо от «ноль прочитанного»", err)
	}
	devProxy, declaredIn, filesRead := map[string]bool{}, []string{}, 0
	for _, abs := range files {
		if filepath.Base(abs) != "vite.config.ts" {
			continue
		}
		body, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git этого дерева
		if rerr != nil {
			t.Fatalf("%s: %v", abs, rerr)
		}
		filesRead++
		m := devProxyBandRe.FindSubmatch(body)
		if m == nil {
			continue
		}
		rel, _ := filepath.Rel(root, abs)
		declaredIn = append(declaredIn, rel)
		for _, r := range devProxyRouteRe.FindAllSubmatch(m[1], -1) {
			devProxy[string(r[1])] = true
		}
	}

	t.Logf("осмотрено: полос раздачи %d, сегментов %d %v; конфигураций сборщика прочитано %d, "+
		"полосу объявляют %d %v, сегментов %d %v",
		bandsFound, len(serving), sortedKeys(serving),
		filesRead, len(declaredIn), declaredIn, len(devProxy), sortedKeys(devProxy))

	switch {
	case bandsFound == 0:
		t.Fatal("раздача не объявляет полосы потоков личности — сверять не с чем")
	case filesRead == 0:
		t.Fatal("не прочитано ни одной конфигурации сборщика — прочитано ноль, и молчание " +
			"проверки не является утверждением о согласии полос")
	case len(declaredIn) == 0:
		t.Fatal("ни одна конфигурация сборщика не объявляет полосу путей службы личности — " +
			"внешнего источника «что обязано быть покрыто» не осталось, и требование покрытия " +
			"стало бы тождественно-истинным")
	}

	for _, seg := range sortedKeys(devProxy) {
		if serving[seg] {
			continue
		}
		t.Errorf("консоль отдаёт службе личности путь %q, а полоса раздачи его НЕ объявляет "+
			"(объявлены: %v). Непокрытый адрес достаётся заглушке одностраничного приложения, "+
			"та отдаёт `index.html` с кодом `200` — отказ выглядит УСПЕХОМ: браузер получает "+
			"двухсотый код и пустую оболочку, а маршрутизатор уводит на главную. "+
			"Объявлено это в %v.", "/"+seg, sortedKeys(serving), declaredIn)
	}
	for _, seg := range sortedKeys(serving) {
		if devProxy[seg] {
			continue
		}
		t.Errorf("полоса раздачи объявляет путь %q, которого нет в полосе сборщика (%v): "+
			"в разработке и на стенде этот адрес обслуживается по-разному, и решал это никто. "+
			"Объявлено в %v.", "/"+seg, sortedKeys(devProxy), declaredIn)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// СНЯТАЯ СЛУЖБА ВЫДАЧИ ТОКЕНА (задача #2733)
//
// Служба выдачи токена прежнего поставщика личности снимается: в консоли её не
// читает НИКТО — ни одного обращения к её краю из кода, ни одного читателя её
// ручек. Осталась только проводка: восходящий узел в раздаче, полосы сборщика и
// ручки их адресов. Проводка без читателя — не безвредный остаток: она
// объявляет продукт зависящим от службы, которой у него нет, и первый же
// заход по её адресу получает ответ от чужого края вместо отказа.
//
// Дом утверждения — ЭТОТ файл: он и так единственное место, которое читает обе
// стороны проводки (объявление раздачи и объявления сборщика). Второе место об
// этом предмете расходилось бы с ним молча.
//
// ПРИЗНАК — имя службы в адресе либо в имени ручки, без учёта регистра.
// `hydrat` в признак НЕ входит, и это решение, а не упущение: `hydrate` —
// собственное имя наполнения состояния в консоли (`contextApi.hydrate`) и часть
// имени стороннего пакета (`@radix-ui/react-use-is-hydrated`). Ось по голому
// `hydra` краснела бы на коде, к снимаемой службе отношения не имеющем.

// retiredTokenIssuerMark — имя снятой службы выдачи токена (нижний регистр).
const retiredTokenIssuerMark = "hydra"

// retiredTokenIssuerFalseMark — приставка, которую признак НЕ засчитывает.
const retiredTokenIssuerFalseMark = "hydrat"

// namesRetiredTokenIssuer — несёт ли строка имя снятой службы. Строка обязана
// быть уже в нижнем регистре.
func namesRetiredTokenIssuer(lower string) bool {
	return strings.Contains(strings.ReplaceAll(lower, retiredTokenIssuerFalseMark, ""), retiredTokenIssuerMark)
}

// TestConsoleNamesNoUpstreamOfTheRetiredTokenIssuer — ни объявление раздачи, ни
// объявления сборщика не называют снятую службу выдачи токена.
//
// Корпус берётся из состава дерева, а не выписывается здесь: конфигурация,
// заведённая завтра, попадает под суд сама. Пустой обход — отказ, а не зелёное.
func TestConsoleNamesNoUpstreamOfTheRetiredTokenIssuer(t *testing.T) {
	root := repoRootFromTest(t)

	files, err := treecorpus.Under(filepath.Join(root, "ui-future"))
	if err != nil {
		t.Fatalf("состав ui-future: %v — без индекса «ноль находок» неотличимо от «ноль прочитанного»", err)
	}

	// Судятся ОБЪЯВЛЕНИЯ проводки: раздача консоли и конфигурации сборщика.
	// Прочее дерево судит убывающий потолок привязок (kacho#2730); здесь его
	// число не пересказывается.
	judged, linesRead, findings := []string{}, 0, 0
	for _, abs := range files {
		base := filepath.Base(abs)
		rel, _ := filepath.Rel(root, abs)
		if base != "vite.config.ts" && filepath.ToSlash(rel) != filepath.ToSlash(servingTemplateRel) {
			continue
		}
		body, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git этого дерева
		if rerr != nil {
			t.Fatalf("%s: %v — прочитать объявление не удалось, и проверка НЕ ИСПОЛНЯЛАСЬ", rel, rerr)
		}
		judged = append(judged, filepath.ToSlash(rel))
		for i, line := range strings.Split(string(body), "\n") {
			linesRead++
			exec := executablePart(line)
			if !namesRetiredTokenIssuer(strings.ToLower(exec)) {
				continue
			}
			findings++
			t.Errorf("%s:%d — объявление называет СНЯТУЮ службу выдачи токена: %q.\n"+
				"В консоли её не читает никто: ни обращения к её краю, ни читателя её ручек. "+
				"Оставленная проводка объявляет продукт зависящим от службы, которой у него "+
				"нет, и первый же заход по её адресу получает ответ чужого края вместо отказа.",
				filepath.ToSlash(rel), i+1, strings.TrimSpace(line))
		}
	}

	t.Logf("осмотрено: путей в индексе ui-future %d, из них судимых объявлений %d %v, "+
		"строк прочитано %d; найдено привязок к снятой службе выдачи токена %d",
		len(files), len(judged), judged, linesRead, findings)

	switch {
	case len(files) == 0:
		t.Fatal("индекс ui-future пуст — прочитано ноль, и молчание проверки не является " +
			"утверждением о проводке")
	case len(judged) == 0:
		t.Fatal("не найдено ни одного судимого объявления (раздача, конфигурации сборщика) — " +
			"судить не по чему")
	case linesRead == 0:
		t.Fatal("судимые объявления не дали ни одной строки")
	}
}

// TestRetiredTokenIssuerMarkCutsBothWays — признак обязан ловить имя снятой
// службы и МОЛЧАТЬ на законном близнеце, отличающемся одной буквой.
//
// Без второй половины признак «всё, где есть hydra» покраснел бы на
// `contextApi.hydrate` и на `@radix-ui/react-use-is-hydrated` — то есть на коде,
// к снимаемой службе отношения не имеющем, и его перестали бы читать.
func TestRetiredTokenIssuerMarkCutsBothWays(t *testing.T) {
	type probe struct {
		line string
		want bool
	}
	// Входы — настоящие строки из дерева этой ревизии, а не выдуманные.
	cases := []probe{
		{`const hydra = process.env.KACHO_HYDRA_BASE || "http://localhost:4444";`, true},
		{`        location ^~ /.ory/hydra/public/ {`, true},
		{`            set $hydra_public "${KACHO_UI_HYDRA_PUBLIC_UPSTREAM}";`, true},
		{`      contextApi.hydrate({ account: context.account });`, false},
		{`        "@radix-ui/react-use-is-hydrated": "0.1.3",`, false},
		{`      "/.ory/kratos/public": {`, false},
	}
	for _, c := range cases {
		got := namesRetiredTokenIssuer(strings.ToLower(c.line))
		if got != c.want {
			t.Errorf("признак снятой службы выдачи токена на строке %q дал %v, ожидалось %v",
				c.line, got, c.want)
		}
	}
	t.Logf("осмотрено: входов %d (из них положительных 3, законных близнецов 3)", len(cases))
}
