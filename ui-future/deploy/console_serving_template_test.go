// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_serving_template_test.go — РАЗБОР ОБЪЯВЛЕНИЯ РАЗДАЧИ КОНСОЛИ и
// порядок, которым раздача выбирает блок для адреса.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Пробы пакета судят раздачу консоли по её ОБЪЯВЛЕНИЮ — шаблону конфигурации из
// чарта (`templates/configmap-nginx.yaml`): соседи раздачи
// (console_serving_neighbours_test.go) и полоса потока подписки
// (subscription_stream_serving_test.go). Разбор у них один, и он здесь.
//
// Раздача выбирает блок НЕ по порядку в файле. Порядок у неё свой, и он
// перечислен в её же руководстве: точное совпадение · самый длинный префикс ·
// регулярки в порядке объявления · запомненный префикс. Из этого следует то,
// чего чтение сверху вниз не показывает:
//
//   - РЕГУЛЯРКА ПОБЕЖДАЕТ ОБЫЧНЫЙ ПРЕФИКС, где бы она ни стояла в файле;
//   - победу префикса даёт только пометка `^~`: она прекращает разбор регулярок;
//   - между собой регулярки решает ТЕКСТОВЫЙ порядок, и вот он читается сверху
//     вниз.
//
// Прежде разбор жил в пробе полосы к внешнему экрану входа
// (identity_serving_precedence_test.go); полоса снята вместе с поставщиком
// личности (#1276), и проба снята вместе со своим предметом. Разбор и
// доказательство того, что выбор блока режет в обе стороны, остались здесь:
// их читают живые пробы пакета.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО РАЗБОР ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЕ. Ни кластера, ни helm, ни сети: рендер требует инструмента
// развёртывания, которого в наборе проб нет, и проба, зависящая от него,
// пропускалась бы молча — а молчание пропущенной пробы неотличимо от зелёного.
//
// Разбор идёт по ИСПОЛНЯЕМОЙ части: вставки шаблона `{{ … }}` и подстановки
// окружения `${…}` снимаются перед счётом вложенности, потому что их фигурные
// скобки принадлежат не раздаче. Форма, которую разбор не понимает,
// ОТВЕРГАЕТСЯ отказом, а не пропускается: разбор, молча пропускающий
// непонятое, превращает «ноль находок» в «ноль прочитанного».
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

// servingTemplateRel — объявление раздачи консоли относительно корня дерева.
var servingTemplateRel = filepath.Join("ui-future", "deploy", "templates", "configmap-nginx.yaml")

// helmActionRe — исполняемая вставка шаблона. Её фигурные скобки принадлежат
// helm, а не раздаче, и в счёт вложенности блоков не идут.
var helmActionRe = regexp.MustCompile(`\{\{-?.*?-?\}\}`)

// envRefRe — подстановка окружения `${ИМЯ}`: скобки принадлежат envsubst.
var envRefRe = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

// namedFallbackRe — уход в именованный блок: `try_files … @имя;`.
var namedFallbackRe = regexp.MustCompile(`try_files\s+[^;]*(@[A-Za-z0-9_]+)\s*;`)

// serverOpenRe / locationOpenRe — открытие блоков в исполняемой части.
var (
	serverOpenRe   = regexp.MustCompile(`^\s*server\s*\{\s*$`)
	locationOpenRe = regexp.MustCompile(`^\s*location\s+(\S+)(?:\s+(\S+))?\s*\{\s*$`)
)

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

// bandSegments — сегменты, которые регулярка-полоса `^/(…)(/|$)` объявляет своими.
func bandSegments(reSrc string) ([]string, error) {
	m := bandSegmentsRe.FindStringSubmatch(reSrc)
	if m == nil {
		return nil, fmt.Errorf("полоса %q не разбирается формой `^/(…)(/|$)`", reSrc)
	}
	var out []string
	for _, seg := range strings.Split(m[1], "|") {
		if seg = strings.TrimSpace(seg); seg != "" {
			out = append(out, seg)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("полоса %q не назвала ни одного сегмента", reSrc)
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

// TestServingPrecedenceDiscriminatorCutsBothWays — выбор блока обязан ловить
// неверный порядок и МОЛЧАТЬ на законном близнеце той же формы.
//
// Обе оси проверяются раздельно, потому что причины у них разные: префикс
// проигрывает регулярке из-за ОТСУТСТВИЯ пометки, а регулярка регулярке — из-за
// ТЕКСТОВОГО порядка. Проба, покрывающая обе одной фикстурой, на починке одной
// оси зеленела бы по второй. Фикстуры синтетические: адреса и восходящие узлы
// в них — пробные, а не адреса какого-либо соседа.
func TestServingPrecedenceDiscriminatorCutsBothWays(t *testing.T) {
	// %s — пометка префиксного блока: пусто (обычный префикс) либо `^~`.
	const prefixShape = `
server {
    location %s /probe/public/ {
        set $probe_upstream "${KACHO_UI_PROBE_UPSTREAM}";
        proxy_pass http://$probe_upstream;
    }

    location ~* \.(js|css)$ {
        try_files $uri @probe_fallback;
    }

    location @probe_fallback {
        set $probe_fallback "${KACHO_UI_PROBE_UPSTREAM}";
        proxy_pass http://$probe_fallback;
    }

    location / {
        try_files $uri $uri/ /index.html;
    }
}
`
	const wantURI = "/probe/public/probe.js"

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
		t.Fatalf("законный близнец не молчит: с пометкой `^~` адрес %q обязан достаться "+
			"префиксному блоку, а достался %v (%s)", wantURI, got, why)
	}

	// Вторая ось — текстовый порядок двух регулярок.
	const bandFirst = `
server {
    location ~ ^/(alpha|beta)(/|$) {
        set $probe_band "${KACHO_UI_PROBE_UPSTREAM}";
        proxy_pass http://$probe_band;
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

    location ~ ^/(alpha|beta)(/|$) {
        set $probe_band "${KACHO_UI_PROBE_UPSTREAM}";
        proxy_pass http://$probe_band;
    }

    location @probe_fallback {
        proxy_pass http://probe;
    }
}
`
	const styled = "/beta/style.css"

	ok := parseServingTemplate(t, bandFirst)
	if got, why = selectLocation(styled, ok[0].locs); got != 0 {
		t.Fatalf("законный порядок объявлен неверным: %q обязан достаться регулярке-полосе, "+
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
	if segs, err := bandSegments(ok[0].locs[0].spec); err != nil || strings.Join(segs, ",") != "alpha,beta" {
		t.Fatalf("сегменты регулярки-полосы %q разобраны неверно: %v (%v)", ok[0].locs[0].spec, segs, err)
	}
}
