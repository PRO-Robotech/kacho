// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"encoding/json"
	"path"
	"sort"
	"strings"
)

// uideclaredlint.go — разбор объявленных команд консоли: манифесты пакетов,
// команды внутри скрипта, вызовы `npm run` и их разрешение в узлы.
//
// Живёт в прод-файле, а не в `_test.go`, ровно затем, чтобы прогон по дереву и
// проба инъекции звали ОДНУ функцию. Проба, повторяющая логику гейта своей
// копией, доказывала бы свойство копии — и расходилась бы с оригиналом молча.

// uiRoot — каталог консоли от корня репозитория.
const uiRoot = "ui-future"

// uiManifest — отслеживаемый package.json консоли.
type uiManifest struct {
	Rel     string // путь от корня репозитория (координата находки)
	Pkg     string // каталог пакета от ui-future; "" — корневой манифест
	Scripts map[string]string
}

// uiParseManifest разбирает package.json. Читается РАЗОБРАННЫЙ документ: в JSON
// комментария не существует как узла, поэтому объяснение правила не может сойти
// за его исполнение.
func uiParseManifest(rel, body string) (uiManifest, bool) {
	if !strings.HasPrefix(rel, uiRoot+"/") || path.Base(rel) != "package.json" {
		return uiManifest{}, false
	}
	if strings.Contains(rel, "/node_modules/") {
		return uiManifest{}, false
	}
	var doc struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return uiManifest{}, false
	}
	return uiManifest{
		Rel:     rel,
		Pkg:     path.Dir(strings.TrimPrefix(rel, uiRoot+"/")), // "." для корневого
		Scripts: doc.Scripts,
	}, true
}

// uiPkgOf нормализует каталог пакета: корневой манифест ui-future/package.json
// даёт "." от path.Dir — приводим к пустой строке, чтобы «корень» назывался
// одним способом, а не двумя.
func uiPkgOf(dir string) string {
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

// uiShellWords разбивает команду на слова, снимая кавычки.
//
// Кавычки снимаются, а не отбрасываются вместе с содержимым: аргумент stylelint
// записан В КАВЫЧКАХ (`"src/**/*.{css,scss}"`) именно затем, чтобы шаблон не
// раскрыла оболочка, — и если бы разбор терял содержимое кавычек, гейт не видел
// бы шаблона вовсе и молчал бы на каждом вызове.
func uiShellWords(s string) []string {
	var (
		out  []string
		cur  strings.Builder
		open bool
		q    byte
	)
	flush := func() {
		if open {
			out = append(out, cur.String())
			cur.Reset()
			open = false
		}
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if q != 0 {
			if ch == q && (i == 0 || s[i-1] != '\\') {
				q = 0
				continue
			}
			cur.WriteByte(ch)
			open = true
			continue
		}
		switch ch {
		case '\'', '"':
			q = ch
			open = true // пустые кавычки — тоже слово
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			cur.WriteByte(ch)
			open = true
		}
	}
	flush()
	return out
}

// uiShellCommands режет тело скрипта на отдельные команды по разделителям
// оболочки, не разрывая литералы в кавычках.
func uiShellCommands(s string) []string {
	var (
		out  []string
		cur  strings.Builder
		q    byte
		seps = "&|;\n"
	)
	push := func() {
		if c := strings.TrimSpace(cur.String()); c != "" {
			out = append(out, c)
		}
		cur.Reset()
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if q != 0 {
			cur.WriteByte(ch)
			if ch == q && (i == 0 || s[i-1] != '\\') {
				q = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			q = ch
			cur.WriteByte(ch)
			continue
		}
		if strings.IndexByte(seps, ch) >= 0 {
			push()
			continue
		}
		cur.WriteByte(ch)
	}
	push()
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Вызов stylelint: шаблон против названного файла.
// ─────────────────────────────────────────────────────────────────────────────

// uiGlobMeta — знаки, делающие аргумент ШАБЛОНОМ. Именно они превращают набор
// файлов в функцию дерева: сегодня он непуст, завтра пуст, а команда объявлена
// одна и та же.
const uiGlobMeta = "*?[{"

// uiEmptyInputAllowed — вызов объявил, что пустой набор для него не ошибка.
func uiEmptyInputAllowed(words []string) bool {
	for _, w := range words {
		if w == "--allow-empty-input" || w == "--aei" ||
			strings.HasPrefix(w, "--allow-empty-input=") {
			return true
		}
	}
	return false
}

// uiEmptyMatchSite — вызов, который на пустом наборе объявит ошибку.
type uiEmptyMatchSite struct {
	Rel     string // координата: путь к манифесту
	Pkg     string
	Script  string
	Command string
	Pattern string
}

// uiAuditEmptyMatch ищет в манифесте вызовы stylelint по ШАБЛОНУ без объявления
// «пустой набор — не ошибка».
//
// Возвращает три величины: сколько вызовов stylelint найдено всего, сколько из
// них идут по шаблону, и находки. Первые две — перепись: без них «ноль находок»
// неотличимо от «ноль прочитанного».
func uiAuditEmptyMatch(m uiManifest) (invocations, patterns int, findings []uiEmptyMatchSite) {
	names := make([]string, 0, len(m.Scripts))
	for n := range m.Scripts {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		for _, cmd := range uiShellCommands(m.Scripts[name]) {
			words := uiShellWords(cmd)
			if len(words) == 0 || path.Base(words[0]) != "stylelint" {
				continue
			}
			invocations++
			var pattern string
			for _, w := range words[1:] {
				if strings.HasPrefix(w, "-") {
					continue // ключ, а не операнд
				}
				if strings.ContainsAny(w, uiGlobMeta) {
					pattern = w
					break
				}
			}
			if pattern == "" {
				continue // назван конкретный файл: его отсутствие — настоящая находка
			}
			patterns++
			if uiEmptyInputAllowed(words) {
				continue
			}
			findings = append(findings, uiEmptyMatchSite{
				Rel: m.Rel, Pkg: uiPkgOf(m.Pkg), Script: name, Command: cmd, Pattern: pattern,
			})
		}
	}
	return invocations, patterns, findings
}

// ─────────────────────────────────────────────────────────────────────────────
// Разрешение `npm run` в узлы «скрипт в пакете».
// ─────────────────────────────────────────────────────────────────────────────

// uiScriptNode — один объявленный скрипт одного пакета.
type uiScriptNode struct {
	Pkg    string // каталог от ui-future; "" — корневой манифест
	Script string
}

func (n uiScriptNode) String() string {
	if n.Pkg == "" {
		return "<корень>:" + n.Script
	}
	return n.Pkg + ":" + n.Script
}

// uiUnresolvable — слово, значение которого статически неизвестно (подстановка
// конвейера или переменная оболочки). Такой вызов НЕ засчитывается ни в
// достигнутые, ни в пропущенные: он называется отдельной строкой переписи,
// иначе догадка о нём стала бы вердиктом.
func uiUnresolvable(w string) bool {
	return strings.Contains(w, "${{") || strings.Contains(w, "$")
}

// uiLeadingNoise — слова, которые могут стоять перед командой, не будучи ею.
var uiLeadingNoise = map[string]bool{
	"do": true, "then": true, "else": true, "time": true, "!": true, "exec": true,
}

// uiRunTarget — узел, который запускает эта команда, если она вообще запускает
// скрипт npm.
//
// Понимает формы, встречающиеся в дереве: `npm run X`, `npm run X --prefix P`,
// `npm --prefix P run X`, `npm test [--prefix P]`. Прочие подкоманды npm
// (`ci`, `install`, `audit`) скриптов не запускают и узла не дают.
//
// ГРАНИЦА РАЗБОРА НАЗВАНА, А НЕ СКРЫТА: имя пакета, собранное подстановкой
// (`--prefix "$p"`, `${{ matrix.pkg }}`), статически неизвестно — такой вызов
// возвращается как НЕРАЗРЕШИМЫЙ и попадает в перепись отдельным числом. Читатель,
// увидевший «не исполняется» рядом с ненулевым числом неразрешимых, знает, где
// смотреть; молчаливое отнесение их к «исполняется» сделало бы гейт зелёным на
// догадке, а к «не исполняется» — ложно красным.
func uiRunTarget(fromPkg, cmd string) (node uiScriptNode, ok, unresolved bool) {
	words := uiShellWords(cmd)
	// Снимаем то, что стоит ПЕРЕД командой и командой не является: ключевые слова
	// оболочки (`do npm run …` в теле цикла — настоящий запуск, и не увидеть его
	// значит объявить исполняемое неисполняемым) и присваивания окружения
	// (`FOO=bar npm run …`).
	for len(words) > 0 {
		w := words[0]
		if uiLeadingNoise[w] || (strings.Contains(w, "=") && !strings.HasPrefix(w, "-")) {
			words = words[1:]
			continue
		}
		break
	}
	if len(words) == 0 || path.Base(words[0]) != "npm" {
		return uiScriptNode{}, false, false
	}

	prefix := ""
	prefixUnknown := false
	for i := 1; i < len(words); i++ {
		if words[i] != "--prefix" || i+1 >= len(words) {
			continue
		}
		if uiUnresolvable(words[i+1]) {
			prefixUnknown = true
			break
		}
		prefix = path.Clean(words[i+1])
	}

	script := ""
	for i := 1; i < len(words); i++ {
		w := words[i]
		if w == "test" {
			script = "test"
			break
		}
		if w != "run" && w != "run-script" {
			continue
		}
		for j := i + 1; j < len(words); j++ {
			if strings.HasPrefix(words[j], "-") {
				if words[j] == "--prefix" {
					j++ // значение ключа скриптом не является
				}
				continue
			}
			script = words[j]
			break
		}
		break
	}
	if script == "" {
		return uiScriptNode{}, false, false
	}
	if prefixUnknown || uiUnresolvable(script) {
		return uiScriptNode{}, false, true
	}

	target := fromPkg
	if prefix != "" {
		// `--prefix` в этом дереве всегда относителен каталогу, из которого
		// команда запущена; для корневого манифеста это сам ui-future.
		target = uiPkgOf(path.Clean(path.Join(fromPkg, prefix)))
	}
	return uiScriptNode{Pkg: target, Script: script}, true, false
}

// uiReach — транзитивное замыкание: какие узлы исполняются, если запустить
// перечисленные.
//
// Возвращает и число неразрешимых вызовов, встреченных по дороге: они не
// «ничего», а именно неизвестность, и перепись обязана называть её отдельно.
func uiReach(byPkg map[string]uiManifest, seeds []uiScriptNode) (map[uiScriptNode]bool, int) {
	seen := map[uiScriptNode]bool{}
	unresolved := 0
	var walk func(uiScriptNode)
	walk = func(n uiScriptNode) {
		if seen[n] {
			return
		}
		seen[n] = true
		m, ok := byPkg[n.Pkg]
		if !ok {
			return
		}
		body, ok := m.Scripts[n.Script]
		if !ok {
			return
		}
		for _, cmd := range uiShellCommands(body) {
			child, ok, unres := uiRunTarget(n.Pkg, cmd)
			if unres {
				unresolved++
				continue
			}
			if ok {
				walk(child)
			}
		}
	}
	for _, s := range seeds {
		walk(s)
	}
	return seen, unresolved
}

// uiSortNodes — устойчивый порядок находок: вердикт не должен зависеть от
// обхода отображения.
func uiSortNodes(in map[uiScriptNode]bool) []uiScriptNode {
	out := make([]uiScriptNode, 0, len(in))
	for n := range in {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pkg != out[j].Pkg {
			return out[i].Pkg < out[j].Pkg
		}
		return out[i].Script < out[j].Script
	})
	return out
}
