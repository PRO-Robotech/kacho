// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// client_identity_leaf_test.go — клиентская личность исходящего соединения не
// предъявляется СЕРВЕРНЫМ листом, и чарт, её объявивший, объявляет секрет клиента.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (задача #2186)
//
// Серверный лист выписывается с назначением `server auth` и БЕЗ `client auth` —
// это записано в самом шаблоне сертификата («server-only — NO client auth on the
// server leaf»). Слушатель, принимающий клиентов в режиме «проверить и
// потребовать», проверяет предъявленную цепь с назначением `clientAuth`, поэтому
// серверный лист эту проверку не проходит НИКОГДА: рукопожатие обрывается
// предупреждением `bad certificate`.
//
// Наблюдалось: собственные REST-фронты службы доступа шли к её же gRPC-слушателю,
// предъявляя серверный лист, — клиентского у службы не было вовсе, потому что
// файл значений не объявлял имя секрета клиента и блок клиентского сертификата
// не срабатывал ни разу. Любой запрос, доходивший до обработчика, отвечал
// `503 {"code":14,… "tls: bad certificate"}`; маршрутизация при этом была жива
// (неизвестный путь честно давал 404), и снаружи это выглядело поломкой службы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО НЕ ЛОВИЛОСЬ УЖЕ СТОЯВШИМ СТРАЖЕМ
//
// Страж старта процесса требует, чтобы удостоверение исходящего вызова было
// ОБЪЯВЛЕНО (`..._UPSTREAM_MTLS_ENABLE`), и его собственный комментарий говорит:
// «половина пары хуже отсутствия обеих — она выглядит настроенной». Именно эта
// половина и была: объявление есть, а названный им файл — чужой роли. Вторая
// половина (ЧЕМ именно представляемся) не проверялась ничем.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ — ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Рендер умбреллы требует собранных зависимостей (`helm dep build` ходит в сеть),
// поэтому проверка по рендеру в обычном прогоне ПРОПУСКАЕТСЯ, — а пропускающаяся
// проверка на измерении «ключа нет вовсе» бесполезна by construction: именно
// отсутствие ключа она и ловит. Тот же довод — у соседних
// kaname_listener_knobs_test.go и helm/umbrella/peer_transport_profiles_test.go.
//
// ─────────────────────────────────────────────────────────────────────────────
// ФОРМЫ ЗАПИСИ ПРЕДМЕТА — ИХ ДВЕ, И ОБЕ РАСПОЗНАЮТСЯ (testing.md §«Гейт на класс», п.7)
//
//	Форма A — переменная окружения `<ПОВЕРХНОСТЬ>_MTLS_CERTFILE`: в шаблоне парой
//	          `- name:` / `value:`, в профиле — ключом карты `env`. Сторона читается
//	          по имени: `_SERVER_MTLS_CERTFILE` — лист слушателя, всё прочее —
//	          личность исходящего вызова.
//
//	Форма Б — ключ файла настроек `certfile`/`cert-file`/`certFile`. Сторона
//	          читается по объемлющему ключу: под блоком `mtls` ключ `server` —
//	          лист слушателя, остальные ключи того блока — рёбра к соседям; вне
//	          блока `mtls` предметом считается только ключ, чьё имя содержит
//	          `client`. Ключ `authn.tls.cert-file` (серверный TLS слушателя,
//	          объявленный вне блока `mtls`) под предмет НЕ подпадает — и это
//	          проверено близнецом в доказательстве.
//
// Перепись печатает объём осмотренного ПО КАЖДОЙ форме: расширение
// распознавателя обязано менять осмотренное, иначе оно холостое.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Судится ОБЪЯВЛЕНИЕ. Что файл окажется в поде — свойство рендера, и здесь
//     оно не утверждается; утверждается вторая ось: чарт, объявивший клиентскую
//     личность, объявляет и СЕКРЕТ клиента. Без неё «починка», переставившая
//     путь на клиентский каталог и не заведшая секрета, зеленела бы при пустом
//     каталоге в поде.
//   - Вторая ось спрашивается только с чартов, объявляющих рабочую нагрузку:
//     секрет обязан объявлять тот, кто его монтирует. Профиль умбреллы,
//     называющий путь чужой личности, своей нагрузки не несёт и под неё не
//     подпадает.
//   - Назначение листа берётся из РОЛИ пути в объявлениях того же чарта, а не из
//     поля `usages` отрендеренного Certificate: рендер здесь недоступен. Роль —
//     наблюдаемая: тот же файл назван листом слушателя.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// ─────────────────────────────────────────────────────────────────────────────
// Распознаватели.

// leafEnvNamePair / leafEnvMapKey — форма A в двух записях.
var (
	leafEnvNamePair = regexp.MustCompile(`^\s*-\s*name:\s*([A-Z][A-Z0-9_]*_MTLS_CERTFILE)\s*$`)
	leafEnvValue    = regexp.MustCompile(`^\s*value:\s*(.+?)\s*$`)
	leafEnvMapKey   = regexp.MustCompile(`^\s*([A-Z][A-Z0-9_]*_MTLS_CERTFILE):\s*(.+?)\s*$`)
)

// leafConfigKey — форма Б. Отступ захвачен: по нему ищется объемлющий ключ.
var (
	leafConfigKey = regexp.MustCompile(`^(\s*)(?:cert-?file|cert-?File):\s*(.+?)\s*$`)
	leafBlockKey  = regexp.MustCompile(`^(\s*)([A-Za-z0-9_.-]+):\s*$`)
)

// leafServerEnv — имя переменной, называющей лист СЛУШАТЕЛЯ.
const leafServerEnvMark = "_SERVER_MTLS_CERTFILE"

// leafClientSecretKey — объявление секрета клиента в файле значений.
var leafClientSecretKey = regexp.MustCompile(`^\s*[A-Za-z0-9]*[Cc]lientSecretName:\s*(.+?)\s*$`)

// leafWorkloadKind — чарт объявляет рабочую нагрузку: секрет обязан объявлять
// тот, кто его монтирует.
var leafWorkloadKind = regexp.MustCompile(`(?m)^kind:\s*(Deployment|StatefulSet|DaemonSet)\s*$`)

// ─────────────────────────────────────────────────────────────────────────────
// Факты.

// certDecl — одно объявление сертификата: чем и где оно записано.
type certDecl struct {
	file string // координата: путь от корня дерева
	line int
	name string // имя переменной либо цепочка ключей
	path string // НОРМАЛИЗОВАННОЕ выражение пути
	form string // "env" | "config"
	// owner — САМЫЙ ВНЕШНИЙ ключ, под которым стоит объявление. У профиля
	// умбреллы это ключ подчарта: профиль настраивает ЧУЖОЙ компонент, и секрет
	// клиента для него объявляет тот чарт, чья нагрузка его монтирует, а не тот
	// файл, где написан путь. Без этой привязки проверка краснела бы на
	// исправном — а гейт, чья находка ложна, снимают первым.
	owner string
}

func (d certDecl) where() string { return fmt.Sprintf("%s:%d", d.file, d.line) }

// certScope — один чарт: всё, что он объявляет о сертификатах.
type certScope struct {
	chart        string // каталог чарта
	workload     bool   // чарт объявляет рабочую нагрузку
	clientSecret string // объявленное имя секрета клиента (пусто — не объявлен)
	client       []certDecl
	server       []certDecl
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы доказательство подавало ей
// синтетический вход, а не подделывало дерево.

type leafFinding struct {
	chart  string
	kind   string
	detail string
}

const (
	kindClientPresentsServerLeaf = "клиентская личность предъявляет СЕРВЕРНЫЙ лист"
	kindClientSecretNotDeclared  = "клиентская личность объявлена, а секрет клиента — нет"
)

// judgeClientIdentityLeaves — весь вердикт.
func judgeClientIdentityLeaves(scopes []certScope) []leafFinding {
	var out []leafFinding
	for _, sc := range scopes {
		// (1) Тот же файл назван и листом слушателя, и личностью исходящего
		// вызова. Серверный лист выписан БЕЗ `client auth`, поэтому проверка
		// клиентской цепи не пройдёт ни при каком входе.
		serverBy := map[string][]certDecl{}
		for _, s := range sc.server {
			serverBy[s.path] = append(serverBy[s.path], s)
		}
		for _, c := range sc.client {
			owners, clash := serverBy[c.path]
			if !clash {
				continue
			}
			names := make([]string, 0, len(owners))
			for _, o := range owners {
				names = append(names, fmt.Sprintf("%s (%s)", o.name, o.where()))
			}
			sort.Strings(names)
			out = append(out, leafFinding{
				chart: sc.chart, kind: kindClientPresentsServerLeaf,
				detail: fmt.Sprintf("%s в %s указывает на %s — тот же файл назван листом слушателя: %s; "+
					"серверный лист выписан без `client auth`, и слушатель отвергнет его на рукопожатии",
					c.name, c.where(), c.path, strings.Join(names, ", ")),
			})
		}

		// (2) Секрет клиента объявлен тем, кто его монтирует. Без этой оси
		// «починка», переставившая путь на клиентский каталог, зеленела бы при
		// пустом каталоге в поде.
		if sc.workload && len(sc.client) > 0 && sc.clientSecret == "" {
			names := make([]string, 0, len(sc.client))
			for _, c := range sc.client {
				names = append(names, fmt.Sprintf("%s (%s)", c.name, c.where()))
			}
			sort.Strings(names)
			out = append(out, leafFinding{
				chart: sc.chart, kind: kindClientSecretNotDeclared,
				detail: fmt.Sprintf("объявлено личностей исходящих вызовов %d (%s), а ключа "+
					"`clientSecretName` в значениях чарта нет — значит блок клиентского "+
					"сертификата не срабатывает никогда и названного файла в поде не будет",
					len(sc.client), strings.Join(names, ", ")),
			})
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Сбор фактов из дерева.

// leafScanRoots — корни обхода. Выводятся из раскладки монорепо, а не
// перечисляют чарты: новый чарт попадает под проверку сам.
func leafScanRoots() []string {
	return []string{
		umbrellaDir,
		filepath.Join(repoRoot, "services"),
		filepath.Join(repoRoot, "gateway", "deploy"),
	}
}

// leafYAML — файл, который проверка вообще читает.
func leafYAML(path string) bool {
	switch filepath.Ext(path) {
	case ".yaml", ".yml", ".tpl":
		return true
	}
	return false
}

// normalizeLeafPath — выражение пути в сравнимый вид: снимаются действия
// шаблона, конвейеры и кавычки. Сравниваются ВЫРАЖЕНИЯ, а не отрендеренные
// строки: рендер здесь недоступен, а два объявления одного файла записываются
// в чарте одинаково.
func normalizeLeafPath(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "{{-")
	s = strings.TrimPrefix(s, "{{")
	s = strings.TrimSuffix(s, "-}}")
	s = strings.TrimSuffix(s, "}}")
	if i := strings.Index(s, "| quote"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return strings.Join(strings.Fields(s), " ")
}

// leafEnclosingKeys — цепочка объемлющих ключей для строки с заданным отступом.
// Ищется ВВЕРХ по объявлениям с меньшим отступом; комментарии сюда не попадают —
// они сняты до разбора, иначе проверка краснела бы на собственном объяснении.
func leafEnclosingKeys(lines []string, idx, indent int) []string {
	var chain []string
	cur := indent
	for j := idx - 1; j >= 0; j-- {
		m := leafBlockKey.FindStringSubmatch(lines[j])
		if m == nil {
			continue
		}
		if len(m[1]) < cur {
			chain = append(chain, m[2])
			cur = len(m[1])
		}
	}
	return chain
}

// stripYAMLComments — строки-комментарии снимаются ЦЕЛИКОМ. Хвостовой
// комментарий в строке кода не режется: в этом корпусе объяснения пишутся
// целыми строками, а резать по `#` внутри строки значило бы резать литералы.
func stripYAMLComments(body string) []string {
	lines := strings.Split(body, "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			out[i] = ""
			continue
		}
		out[i] = l
	}
	return out
}

// scanCertDecls — объявления сертификатов одного файла, обе формы.
func scanCertDecls(file, body string) (client, server []certDecl, emptyPaths int) {
	lines := stripYAMLComments(body)
	for i, line := range lines {
		// Форма A, запись шаблона: `- name:` и `value:` следующей строкой.
		if m := leafEnvNamePair.FindStringSubmatch(line); m != nil {
			for j := i + 1; j < len(lines) && j <= i+2; j++ {
				v := leafEnvValue.FindStringSubmatch(lines[j])
				if v == nil {
					continue
				}
				d := certDecl{file: file, line: i + 1, name: m[1], path: normalizeLeafPath(v[1]), form: "env"}
				if d.path == "" {
					emptyPaths++
					break
				}
				if strings.Contains(m[1], leafServerEnvMark) {
					server = append(server, d)
				} else {
					client = append(client, d)
				}
				break
			}
			continue
		}
		// Форма A, запись карты значений.
		if m := leafEnvMapKey.FindStringSubmatch(line); m != nil {
			chain := leafEnclosingKeys(lines, i, leafIndentOf(line))
			d := certDecl{file: file, line: i + 1, name: m[1], path: normalizeLeafPath(m[2]),
				form: "env", owner: leafOutermost(chain)}
			if d.path == "" {
				emptyPaths++
				continue
			}
			if strings.Contains(m[1], leafServerEnvMark) {
				server = append(server, d)
			} else {
				client = append(client, d)
			}
			continue
		}
		// Форма Б: ключ файла настроек, сторона — по объемлющему ключу.
		if m := leafConfigKey.FindStringSubmatch(line); m != nil {
			chain := leafEnclosingKeys(lines, i, len(m[1]))
			side, ok := leafConfigSide(chain)
			if !ok {
				continue
			}
			name := strings.Join(reverseStrings(chain), ".") + ".certfile"
			d := certDecl{file: file, line: i + 1, name: name, path: normalizeLeafPath(m[2]),
				form: "config", owner: leafOutermost(chain)}
			if d.path == "" {
				emptyPaths++
				continue
			}
			if side == "server" {
				server = append(server, d)
			} else {
				client = append(client, d)
			}
		}
	}
	return client, server, emptyPaths
}

// leafConfigSide — сторона объявления формы Б. Второй результат — «это вообще
// наш предмет»: серверный TLS слушателя, объявленный ВНЕ блока `mtls`
// (`authn.tls.cert-file`), под предмет не подпадает.
func leafConfigSide(chain []string) (string, bool) {
	inMTLS := false
	for _, k := range chain {
		if strings.EqualFold(k, "mtls") {
			inMTLS = true
			break
		}
	}
	if len(chain) == 0 {
		return "", false
	}
	nearest := chain[0]
	if inMTLS {
		if strings.EqualFold(nearest, "server") {
			return "server", true
		}
		return "client", true
	}
	if strings.Contains(strings.ToLower(nearest), "client") {
		return "client", true
	}
	return "", false
}

// leafIndentOf — ширина отступа строки.
func leafIndentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// leafOutermost — самый внешний ключ цепочки (цепочка накапливается изнутри
// наружу, поэтому это её последний элемент).
func leafOutermost(chain []string) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[len(chain)-1]
}

func reverseStrings(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[len(in)-1-i] = s
	}
	return out
}

// leafChartDir — каталог чарта, которому принадлежит файл: ближайший предок с
// Chart.yaml. Не нашли — сам каталог файла (профили умбреллы попадают сюда).
func leafChartDir(file string) string {
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "Chart.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir || !strings.Contains(dir, string(filepath.Separator)) {
			return filepath.Dir(file)
		}
		dir = parent
	}
}

// chartDeclaresWorkload — чарт объявляет рабочую нагрузку. Читаются только
// СВОИ файлы чарта: шаблоны подчарта принадлежат подчарту.
func chartDeclaresWorkload(chartDir string, own []string) bool {
	for _, f := range own {
		if filepath.Base(filepath.Dir(f)) != "templates" {
			continue
		}
		raw, err := os.ReadFile(f) // #nosec G304 -- путь пришёл из индекса git через treecorpus
		if err != nil {
			continue
		}
		if leafWorkloadKind.MatchString(string(raw)) {
			return true
		}
	}
	return false
}

// chartClientSecret — объявленное чартом имя секрета клиента (пусто — не
// объявлено). Читаются ВСЕ файлы значений чарта: умолчание может быть пустым, а
// величину даёт профиль.
func chartClientSecret(chartDir string, own []string) string {
	for _, f := range own {
		if filepath.Dir(f) != chartDir || !strings.HasPrefix(filepath.Base(f), "values") {
			continue
		}
		raw, err := os.ReadFile(f) // #nosec G304 -- путь пришёл из индекса git через treecorpus
		if err != nil {
			continue
		}
		for _, line := range stripYAMLComments(string(raw)) {
			m := leafClientSecretKey.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			v := strings.TrimSpace(m[1])
			// Хвостовой комментарий снимается ЗДЕСЬ, а не при разборе строк:
			// имя секрета пробела не содержит, поэтому « #» — однозначная
			// граница, а резать по `#` в общем разборе значило бы резать литералы.
			if i := strings.Index(v, " #"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			v = strings.Trim(v, `"'`)
			if v != "" {
				return v
			}
		}
	}
	return ""
}

// collectCertScopes — обход дерева.
//
// Состав берётся из ИНДЕКСА git, а не обходом диска: обход читал бы игнорируемые
// каталоги (рабочие копии агентов, распаковки чартов, отчёты прогонов) и вердикт
// стал бы свойством рабочего каталога. `treecorpus.Under` заодно ОТКАЗЫВАЕТСЯ
// отвечать на прогоне, чей результат `go test` положит в кеш: состав дерева он
// берёт подпроцессом, инструменту невидимым, и над красным деревом напечаталось
// бы `ok (cached)` — строка, отличимая от настоящего прохода одним словом.
func collectCertScopes(t *testing.T) ([]certScope, int, map[string]int) {
	t.Helper()
	// Ключ значений → каталог чарта. ВЫВОДИТСЯ из зависимостей умбреллы, а не
	// перечисляется: новый подчарт попадает под привязку сам.
	owners := subchartDirs(t)

	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("абсолютный путь корня дерева: %v", err)
	}
	rel := func(abs string) string {
		r, rerr := filepath.Rel(absRoot, abs)
		if rerr != nil {
			return abs
		}
		return filepath.ToSlash(r)
	}
	// Каталог чарта, приведённый к тому же виду, что ключи `subchartDirs`
	// (они относительны рабочему каталогу пробы, то есть каталогу deploy/).
	relToCwd := func(abs string) string {
		r, rerr := filepath.Rel(filepath.Join(absRoot, "deploy"), abs)
		if rerr != nil {
			return abs
		}
		return filepath.ToSlash(r)
	}

	var tracked []string
	for _, root := range leafScanRoots() {
		abs, aerr := filepath.Abs(root)
		if aerr != nil {
			t.Fatalf("абсолютный путь корня %s: %v", root, aerr)
		}
		files, ferr := treecorpus.Under(abs)
		if ferr != nil {
			t.Fatalf("состав %s: %v — «ноль находок» стало бы свойством рабочего каталога", root, ferr)
		}
		tracked = append(tracked, files...)
	}
	sort.Strings(tracked)

	// Свои файлы чарта — те, чей ближайший предок с Chart.yaml есть этот чарт.
	ownFiles := map[string][]string{}
	for _, f := range tracked {
		ownFiles[leafChartDir(f)] = append(ownFiles[leafChartDir(f)], f)
	}

	byChart := map[string]*certScope{}
	filesRead := 0
	counts := map[string]int{}

	for _, abs := range tracked {
		if !leafYAML(abs) {
			continue
		}
		info, serr := os.Lstat(abs)
		if serr != nil || !info.Mode().IsRegular() {
			continue // ссылка либо запись индекса без файла на диске — читать нечего
		}
		raw, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git через treecorpus
		if rerr != nil {
			t.Fatalf("чтение %s: %v", rel(abs), rerr)
		}
		filesRead++
		client, server, empties := scanCertDecls(rel(abs), string(raw))
		counts["пустых путей"] += empties
		if len(client) == 0 && len(server) == 0 {
			continue
		}
		own := leafChartDir(abs)
		place := func(d certDecl, isClient bool) {
			chart := own
			if d.owner != "" {
				if dir, ok := owners[d.owner]; ok {
					if a, aerr := filepath.Abs(dir); aerr == nil {
						chart = a
					}
				}
			}
			sc, ok := byChart[chart]
			if !ok {
				sc = &certScope{
					chart:        relToCwd(chart),
					workload:     chartDeclaresWorkload(chart, ownFiles[chart]),
					clientSecret: chartClientSecret(chart, ownFiles[chart]),
				}
				byChart[chart] = sc
			}
			if isClient {
				sc.client = append(sc.client, d)
				counts["личностей исходящих вызовов, форма "+d.form]++
				return
			}
			sc.server = append(sc.server, d)
			counts["листов слушателей, форма "+d.form]++
		}
		for _, d := range client {
			place(d, true)
		}
		for _, d := range server {
			place(d, false)
		}
	}

	names := make([]string, 0, len(byChart))
	for k := range byChart {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]certScope, 0, len(names))
	for _, n := range names {
		out = append(out, *byChart[n])
	}
	return out, filesRead, counts
}

func TestClientIdentityNeverPresentsTheServerLeaf(t *testing.T) {
	scopes, filesRead, counts := collectCertScopes(t)

	// ПЕРЕПИСЬ — «ноль находок» обязано быть отличимо от «ноль прочитанного»,
	// и она печатается ПО КАЖДОЙ форме: расширение распознавателя обязано менять
	// осмотренное, иначе оно холостое.
	clientTotal, serverTotal, workloads, withSecret := 0, 0, 0, 0
	for _, sc := range scopes {
		clientTotal += len(sc.client)
		serverTotal += len(sc.server)
		if sc.workload {
			workloads++
		}
		if sc.clientSecret != "" {
			withSecret++
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("осмотрено: файлов значений и шаблонов %d · чартов с объявлениями %d "+
		"(из них с рабочей нагрузкой %d, объявляют секрет клиента %d) · "+
		"личностей исходящих вызовов %d · листов слушателей %d",
		filesRead, len(scopes), workloads, withSecret, clientTotal, serverTotal)
	for _, k := range keys {
		t.Logf("   %s: %d", k, counts[k])
	}
	for _, sc := range scopes {
		t.Logf("   чарт %-52s личностей %d · листов %d · нагрузка %v · секрет клиента %q",
			sc.chart, len(sc.client), len(sc.server), sc.workload, sc.clientSecret)
	}

	if filesRead == 0 {
		t.Fatalf("не прочитано ни одного файла — вердикт беспредметен: корни обхода %v", leafScanRoots())
	}
	if clientTotal == 0 {
		t.Fatalf("в дереве не найдено ни одной клиентской личности исходящего соединения — "+
			"вердикт беспредметен: распознаватели (%s | %s | %s) перестали её узнавать, "+
			"а не дерево стало чистым", leafEnvNamePair, leafEnvMapKey, leafConfigKey)
	}
	if serverTotal == 0 {
		t.Fatalf("в дереве не найдено ни одного листа слушателя — вердикт беспредметен: " +
			"сравнивать личность исходящего вызова не с чем")
	}

	findings := judgeClientIdentityLeaves(scopes)
	for _, f := range findings {
		t.Errorf("чарт %s · %s — %s", f.chart, f.kind, f.detail)
	}
}
