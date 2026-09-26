// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_serving_neighbours_test.go — раздача консоли отдаёт запросы ТОЛЬКО
// своим соседям: краю платформы, модулям консоли и, пока посадка его объявляет,
// внешнему экрану входа (задача #2874).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Проводка к соседу, которого у продукта нет, не безвредный остаток: она
// объявляет консоль зависящей от чужой службы, и первый же заход по её адресу
// получает ответ чужого края вместо отказа. Так стояла полоса к службе выдачи
// токена прежнего поставщика личности: читателя у неё в консоли не было ни
// одного, а раздача и ручка адреса оставались (снята #2733).
//
// Прежний страж этого предмета узнавал соседа по ИМЕНИ той службы. Такой страж
// видит ровно одно имя и молчит на любом другом: полоса к соседу под новым
// именем проходила бы его зелёной. И само имя, записанное в пробе, — привязка к
// снятому издателю, которую судит убывающий потолок (kacho#2730). Поэтому
// сосед здесь судится РОЛЬЮ, а не именем: множество законных соседей закрыто, и
// всякий сосед вне него — находка, как бы он ни назывался.
//
// ─────────────────────────────────────────────────────────────────────────────
// РОЛИ — И ОТКУДА КАЖДАЯ ВЫВОДИТСЯ
//
// Адрес соседа раздача получает только подстановкой окружения
// `${KACHO_UI_<ИМЯ>_UPSTREAM}` в `set`, а обращается к нему только
// `proxy_pass http://$<переменная>`. Роль соседа — одна из трёх:
//
//   - КРАЙ ПЛАТФОРМЫ — `KACHO_UI_API_GATEWAY_UPSTREAM`. Единственная выписанная
//     величина: всё, что консоль спрашивает у платформы, она спрашивает у края;
//   - МОДУЛЬ КОНСОЛИ — блок `location ^~ /<модуль>-remote/` с адресом
//     `KACHO_UI_<МОДУЛЬ>_UPSTREAM`, где `<модуль>` — каталог консоли со своей
//     конфигурацией сборщика (`ui-future/<модуль>/vite.config.ts`). Перечень
//     модулей выводится из индекса дерева, а не выписывается: модуль, заведённый
//     завтра, законен сам;
//   - ВНЕШНИЙ ЭКРАН ВХОДА — адрес, который отдаёт полоса церемоний (регулярка
//     формы `^/(…)(/|$)`, та же, что читает соседняя проба порядка), и тот же
//     адрес в именованном блоке, куда уходит статика через `try_files … @имя`.
//     Имени этого соседа проба не знает и не должна: роль выводится из формы
//     блока. Полосу держит посадка `external` (её объявляют чарт края и чарт
//     службы доступа); когда посадка снимется вместе с полосой (#1276), роль
//     опустеет, и проба останется зелёной без правки.
//
// Всё прочее — находка с координатой: сосед без роли; обращение по буквальному
// адресу мимо подстановки; переменная, не связанная с адресом соседа;
// подстановка адреса соседа вне `set`; именованный запасной путь к соседу,
// которого не отдаёт ни одна полоса церемоний.
//
// Вторая сторона той же проводки — РУЧКИ адресов в поде раздачи
// (`templates/deployment-host.yaml`). Ручка без читателя в раздаче — та же
// проводка без читателя; читатель без ручки получает пустую подстановку. Оба
// множества обязаны совпасть.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОБА НЕ ЧИТАЕТ
//
// Конфигураций сборщика в разработке (`vite.config.ts`) она не судит: их судит
// статическая перепись обращений к поставщику по узлам разбора (приёмка F8,
// `shared/src/test/console-provider-not-addressed.test.ts`). Прежнюю полосу
// сборщика к службе выдачи токена она находит обеими своими осями: адрес
// поставщика — по форме (`/.ory/…`, `/oauth2`), ручку его базы — осью ручек
// (`PROVIDER_KNOBS`, пока она стоит). Второго места об этом здесь не заводится.
//
// Читается ОБЪЯВЛЕНИЕ, а не рендер: у набора Go-проб нет инструмента
// развёртывания, а проба, зависящая от него, пропускалась бы молча. Ветви
// условий шаблона читаются обе — обе способны попасть в рендер.
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

// Роли соседа раздачи.
const (
	neighbourEdge   = "край платформы"
	neighbourModule = "модуль консоли"
	neighbourSignIn = "внешний экран входа"
)

// edgeUpstream — адрес края платформы: единственный выписанный сосед.
const edgeUpstream = "API_GATEWAY"

// deploymentHostRel — под раздачи консоли, объявляющий ручки адресов соседей.
var deploymentHostRel = filepath.Join("ui-future", "deploy", "templates", "deployment-host.yaml")

var (
	// neighbourSetRe — связывание переменной с адресом соседа:
	// `set $имя "${KACHO_UI_<ИМЯ>_UPSTREAM}";`.
	neighbourSetRe = regexp.MustCompile(`^\s*set\s+\$([A-Za-z0-9_]+)\s+"\$\{KACHO_UI_([A-Z0-9_]+)_UPSTREAM\}"\s*;\s*$`)
	// neighbourRefRe — подстановка адреса соседа в любом месте строки.
	neighbourRefRe = regexp.MustCompile(`\$\{KACHO_UI_([A-Z0-9_]+)_UPSTREAM\}`)
	// neighbourPassRe — обращение к соседу. Ведётся перечнем ВСЕХ директив
	// передачи запроса, а не одной `proxy_pass`: сосед за `grpc_pass` — тот же
	// сосед.
	neighbourPassRe = regexp.MustCompile(`^\s*(proxy_pass|grpc_pass|fastcgi_pass|uwsgi_pass|scgi_pass|memcached_pass)\s+([^;]*);`)
	// neighbourPassVarRe — понимаемая форма адреса передачи: схема и переменная.
	neighbourPassVarRe = regexp.MustCompile(`^(?:https?|grpcs?)://\$([A-Za-z0-9_]+)(?:/\S*)?$`)
	// moduleRemoteRe — предмет блока модуля консоли: `/<модуль>-remote/`.
	moduleRemoteRe = regexp.MustCompile(`^/([a-z0-9]+)-remote/$`)
	// hostUpstreamEnvRe — ручка адреса соседа в поде раздачи.
	hostUpstreamEnvRe = regexp.MustCompile(`^\s*-\s*name:\s*KACHO_UI_([A-Z0-9_]+)_UPSTREAM\s*$`)
)

// nginxCode — строка без комментария по правилу лексера раздачи: `#`
// открывает комментарий в начале лексемы и вне кавычек. `$uri#x` — одна
// лексема, а не лексема и комментарий.
func nginxCode(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && (i == 0 || strings.IndexByte(" \t;{}", line[i-1]) >= 0):
			return line[:i]
		}
	}
	return line
}

// yamlCode — строка шаблона без комментария YAML (`#` в начале строки или после
// пробела). В строках ручек кавычек с `#` нет, и разбор кавычек здесь не нужен.
func yamlCode(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return line[:i]
		}
	}
	return line
}

// neighbourUse — одно обращение раздачи к соседу.
type neighbourUse struct {
	line     int
	loc      string
	upstream string
	role     string
}

// neighbourCensus — что разбор прочёл и что нашёл.
type neighbourCensus struct {
	servers, locs, passes int
	uses                  []neighbourUse
	byRole                map[string]map[string]bool
	served, declared      map[string]bool
	findings              []string
}

// judgeConsoleNeighbours — суд одной пары объявлений: шаблона раздачи и пода,
// который её поднимает. `modules` — каталоги модулей консоли.
func judgeConsoleNeighbours(t *testing.T, serving, deployment string, modules map[string]bool) neighbourCensus {
	t.Helper()
	c := neighbourCensus{
		byRole:   map[string]map[string]bool{neighbourEdge: {}, neighbourModule: {}, neighbourSignIn: {}},
		served:   map[string]bool{},
		declared: map[string]bool{},
	}
	finding := func(line int, format string, args ...any) {
		c.findings = append(c.findings, fmt.Sprintf("строка %d: ", line)+fmt.Sprintf(format, args...))
	}

	servers := parseServingTemplate(t, serving)
	c.servers = len(servers)
	for _, srv := range servers {
		c.locs += len(srv.locs)

		// Именованные блоки, куда уходит статика (`try_files … @имя;` из любого
		// блока — и префиксного `/assets/`, и регулярки расширений): запасной путь.
		fallbacks := map[string]bool{}
		for _, l := range srv.locs {
			for _, raw := range strings.Split(l.body, "\n") {
				if m := namedFallbackRe.FindStringSubmatch(nginxCode(raw)); m != nil {
					fallbacks[m[1]] = true
				}
			}
		}

		signIn := map[string]bool{}
		type fallbackUse struct {
			line     int
			loc      string
			upstream string
		}
		var fallbackUses []fallbackUse

		for _, l := range srv.locs {
			isBand := false
			if l.re != nil {
				_, err := bandSegments(l.spec)
				isBand = err == nil
			}
			module := ""
			if l.mod == "^~" {
				if m := moduleRemoteRe.FindStringSubmatch(l.spec); m != nil {
					module = m[1]
				}
			}

			bound := map[string]string{}
			for i, raw := range strings.Split(l.body, "\n") {
				line := l.line + 1 + i
				code := nginxCode(raw)

				if m := neighbourSetRe.FindStringSubmatch(code); m != nil {
					bound[m[1]] = m[2]
					c.served[m[2]] = true
					continue
				}
				if m := neighbourRefRe.FindStringSubmatch(code); m != nil {
					finding(line, "адрес соседа %s подставлен вне `set $переменная \"…\";` в %s — "+
						"разбор его роли не устанавливает, и молчание здесь было бы пропуском", m[1], l.name())
					c.served[m[1]] = true
				}

				m := neighbourPassRe.FindStringSubmatch(code)
				if m == nil {
					continue
				}
				c.passes++
				arg := strings.TrimSpace(m[2])
				v := neighbourPassVarRe.FindStringSubmatch(arg)
				if v == nil {
					finding(line, "%s в %s обращается по адресу %q мимо подстановки адреса соседа — "+
						"такого соседа не объявляет ни одна посадка, а раздача ходит к нему на каждом стенде",
						m[1], l.name(), arg)
					continue
				}
				upstream, ok := bound[v[1]]
				if !ok {
					finding(line, "%s в %s идёт через `$%s`, но в этом блоке переменная не связана с "+
						"адресом соседа (`set $%s \"${KACHO_UI_<ИМЯ>_UPSTREAM}\";`) — сосед не установлен",
						m[1], l.name(), v[1], v[1])
					continue
				}

				use := neighbourUse{line: line, loc: l.name(), upstream: upstream}
				switch {
				case upstream == edgeUpstream:
					use.role = neighbourEdge
				case module != "" && modules[module] && upstream == strings.ToUpper(module):
					use.role = neighbourModule
				case isBand:
					use.role = neighbourSignIn
					signIn[upstream] = true
				case l.mod == "@" && fallbacks[l.spec]:
					fallbackUses = append(fallbackUses, fallbackUse{line, l.name(), upstream})
					continue
				default:
					finding(line, "%s отдаёт запросы соседу %s, у которого нет роли: это не край "+
						"платформы, не модуль консоли и не экран входа полосы церемоний. Проводка к "+
						"соседу без роли объявляет консоль зависящей от чужой службы", l.name(), upstream)
					continue
				}
				c.uses = append(c.uses, use)
				c.byRole[use.role][upstream] = true
			}
		}

		for _, f := range fallbackUses {
			if !signIn[f.upstream] {
				finding(f.line, "запасной путь статики %s уходит к соседу %s, которого не отдаёт ни одна "+
					"полоса церемоний этого серверного блока — сосед без роли", f.loc, f.upstream)
				continue
			}
			c.uses = append(c.uses, neighbourUse{line: f.line, loc: f.loc, upstream: f.upstream, role: neighbourSignIn})
		}
	}

	for _, raw := range strings.Split(deployment, "\n") {
		if m := hostUpstreamEnvRe.FindStringSubmatch(yamlCode(raw)); m != nil {
			c.declared[m[1]] = true
		}
	}
	for _, u := range sortedKeys(c.declared) {
		if !c.served[u] {
			c.findings = append(c.findings, fmt.Sprintf("под раздачи объявляет ручку адреса соседа "+
				"KACHO_UI_%s_UPSTREAM, а раздача её не читает — проводка без читателя", u))
		}
	}
	for _, u := range sortedKeys(c.served) {
		if !c.declared[u] {
			c.findings = append(c.findings, fmt.Sprintf("раздача читает адрес соседа KACHO_UI_%s_UPSTREAM, "+
				"а под раздачи такой ручки не объявляет — подстановка будет пустой", u))
		}
	}
	return c
}

// consoleModules — каталоги модулей консоли по индексу дерева: у модуля своя
// конфигурация сборщика. Оболочка (`host`) модулем самой себя не является.
func consoleModules(t *testing.T, root string) map[string]bool {
	t.Helper()
	files, err := treecorpus.Under(filepath.Join(root, "ui-future"))
	if err != nil {
		t.Fatalf("состав ui-future: %v — без индекса перечень модулей не выводится", err)
	}
	out := map[string]bool{}
	for _, abs := range files {
		rel, rerr := filepath.Rel(filepath.Join(root, "ui-future"), abs)
		if rerr != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) == 2 && parts[1] == "vite.config.ts" && parts[0] != "host" {
			out[parts[0]] = true
		}
	}
	return out
}

// readTreeFile — файл дерева по пути от корня; не читается — предпосылка исчезла.
func readTreeFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь выписан в этом пакете, внутри дерева
	if err != nil {
		t.Fatalf("%s не читается (%v) — предпосылка пробы исчезла", rel, err)
	}
	return string(b)
}

// roleText — роль и её соседи для переписи.
func roleText(c neighbourCensus) string {
	roles := []string{neighbourEdge, neighbourModule, neighbourSignIn}
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		parts = append(parts, fmt.Sprintf("%s %d %v", r, len(c.byRole[r]), sortedKeys(c.byRole[r])))
	}
	return strings.Join(parts, " · ")
}

// TestConsoleServingReachesOnlyItsOwnNeighbours — каждый сосед раздачи консоли
// имеет роль, и ручки адресов в поде совпадают с тем, что раздача читает.
func TestConsoleServingReachesOnlyItsOwnNeighbours(t *testing.T) {
	root := repoRootFromTest(t)
	modules := consoleModules(t, root)
	c := judgeConsoleNeighbours(t,
		readTreeFile(t, root, servingTemplateRel),
		readTreeFile(t, root, deploymentHostRel),
		modules)

	sort.Strings(c.findings)
	t.Logf("осмотрено: %s и %s; модулей консоли в индексе %d %v; серверных блоков %d, блоков раздачи %d, "+
		"директив передачи %d; соседи по ролям: %s; ручек адресов в поде %d, читаемых раздачей %d; находок %d",
		servingTemplateRel, deploymentHostRel, len(modules), sortedKeys(modules), c.servers, c.locs, c.passes,
		roleText(c), len(c.declared), len(c.served), len(c.findings))

	switch {
	case len(modules) == 0:
		t.Fatal("в индексе ui-future не найдено ни одного модуля консоли — роль «модуль» вывести не из чего")
	case c.servers == 0:
		t.Fatal("в шаблоне раздачи не найдено ни одного серверного блока — прочитано ноль")
	case c.passes == 0:
		t.Fatal("в шаблоне раздачи не найдено ни одной директивы передачи — судить нечего, " +
			"и «ноль находок» означало бы «ноль прочитанного»")
	case len(c.byRole[neighbourEdge]) == 0:
		t.Fatal("раздача не обращается к краю платформы ни одним блоком — предпосылка пробы исчезла " +
			"либо разбор перестал понимать форму блока")
	case len(c.byRole[neighbourModule]) == 0:
		t.Fatal("раздача не обращается ни к одному модулю консоли — предпосылка пробы исчезла " +
			"либо разбор перестал понимать форму блока")
	case len(c.declared) == 0:
		t.Fatal("под раздачи не объявляет ни одной ручки адреса соседа — вторая сторона проводки не прочитана")
	}
	for _, f := range c.findings {
		t.Error(f)
	}
}
