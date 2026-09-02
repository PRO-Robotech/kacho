// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// rollout_preflight_covers_required_secrets_test.go — предполёт боевой раскатки
// обязан проверять ВСЕ секреты, которых она требует, и у каждого обязан быть
// НАЗВАН производитель на этой площадке.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Предполётная часть скрипта боевой раскатки УМЕЕТ проверять наличие секрета —
// и проверяла ОДИН из четырёх. Остальные три заводит скрипт посева стенда, а на
// боевой площадке он не вызывается: значит их появление не проверялось и не
// производилось ничем.
//
// Наблюдаемая последовательность: предполёт зелёный → раскатка применяется → под
// ждёт недостающий секрет → `helm --wait` выстаивает свой предел и падает ПО
// СРОКУ, а не по причине. Вывод самого скрипта причины не называет; она видна
// только в событиях пода. Предполёт заведён ровно затем, чтобы отделить «условие
// не создано» от «сломан продукт»; для трёх секретов из четырёх он этой работы
// не делал, и оператор отделял одно от другого сам.
//
// Довод «так исторически» здесь снят по построению: механизм проверки УЖЕ БЫЛ —
// форма выбрана, применена к одному секрету и не распространена на остальные.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
//  1. у КАЖДОГО требуемого секрета есть строка в таблице производителей скрипта
//     раскатки: «заводит посев» производителем боевой площадки не является —
//     посев там не зовётся;
//  2. каждая строка таблицы имеет ПРЕДМЕТ: секрет, которого больше никто не
//     требует, — находка, а не запас. Послабление обязано истекать само;
//  3. перечень требуемого предполёт ВЫВОДИТ (рендер · посев · таблица), а не
//     проверяет одно имя;
//  4. отказ предполёта наступает ДО применения — раньше `helm upgrade` в тексте
//     скрипта.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА
//
//   - Проверяется ОХВАТ предполёта и наличие производителя, а не то, что секрет
//     существует в кластере: это рантайм, и его судит сам предполёт.
//   - Требуемое множество здесь выводится ИЗ ОБЪЯВЛЕНИЙ (профили цепочки и
//     шаблоны наших подчартов) плюс из скрипта посева. Рендер даёт больше — и
//     именно его читает предполёт в момент раскатки; проверка над рендером
//     умеет пропуститься там, где нет зависимостей чарта, поэтому здесь её нет.
//   - Смежное и другое: объявление `existingSecret` в профилях, у которого нет
//     ни производителя, ни проверяющего вовсе, — предмет отдельной задачи.
//     Здесь предмет — охват уже существующей проверки внутри скрипта раскатки.
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

// rolloutScript — рецепт боевой раскатки. Он один; вторая копия была бы тем же
// классом, который эта проверка ловит на уровне таблицы.
const rolloutScript = "helm/umbrella/cutover-fe3455.sh"

// seedScript — посев секретов стенда. На боевой площадке он не зовётся, и
// именно поэтому заведённые им секреты обязаны иметь ДРУГОГО производителя.
const seedScript = "scripts/dev-prod-secrets.sh"

// producerRow — строка таблицы производителей: `<секрет>|<кто заводит>`.
var producerRow = regexp.MustCompile(`(?m)^([a-z0-9][a-z0-9-]*)\|(\S.*)$`)

// renderAssign — рендер того же стека, из которого предполёт берёт обязательные
// ссылки. Форма привязана к ПРИСВАИВАНИЮ, а не к слову: слова `helm template`
// стоят и в текстах отказов этого же предполёта.
var renderAssign = regexp.MustCompile(`RENDER="\$\(helm template\b`)

// seedCreates — секрет, который заводит посев.
var seedCreates = regexp.MustCompile(`create secret generic ([a-z0-9][a-z0-9-]*)`)

// secretNamePos — позиции, в которых имя секрета вообще может быть названо
// объявлением. Перечень позиций, а не имён: имя — то, что выводится.
var secretNamePos = regexp.MustCompile(`(?m)^\s*(?:secretName|existingSecret|name):\s*([a-z0-9][a-z0-9-]*)\s*$`)

// preflightFacts — факты о предполёте и требуемом множестве.
type preflightFacts struct {
	required  []string          // требуемые секреты, выведенные из объявлений и посева
	producers map[string]string // таблица производителей рецепта
	mentioned map[string]bool   // имена, названные объявлениями цепочки либо посевом
	derives   []string          // источники, из которых предполёт выводит перечень
	before    bool              // отказ предполёта стоит РАНЬШЕ применения
	scanned   int               // объявлений осмотрено
}

func preflightFactsFromTree(t *testing.T) preflightFacts {
	t.Helper()
	f := preflightFacts{producers: map[string]string{}, mentioned: map[string]bool{}}

	chains := deployStacks(t)
	chain, ok := chains["fe3455"]
	if !ok {
		t.Fatalf("таблица стеков не объявляет цепочки боевой площадки — "+
			"предмет проверки исчез, а не стал чистым (%s)", stacksTable)
	}

	var files []string
	for _, p := range chain {
		files = append(files, filepath.Join(umbrellaDir, p))
	}
	for _, pat := range []string{
		filepath.Join(umbrellaDir, "charts", "*", "templates", "*"),
		filepath.Join(umbrellaDir, "templates", "*"),
	} {
		got, err := filepath.Glob(pat)
		if err != nil {
			t.Fatalf("обход %s: %v", pat, err)
		}
		files = append(files, got...)
	}
	sort.Strings(files)
	f.scanned = len(files)

	req := map[string]bool{}
	for _, p := range files {
		b, err := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if err != nil {
			t.Fatalf("чтение %s: %v", p, err)
		}
		body := string(b)
		for _, m := range secretNamePos.FindAllStringSubmatch(body, -1) {
			f.mentioned[m[1]] = true
		}
		for _, n := range mandatorySecretRefs(body) {
			req[n] = true
			f.mentioned[n] = true
		}
	}

	seed, err := os.ReadFile(seedScript) // #nosec G304 -- координата дерева, не пользовательский ввод
	if err != nil {
		t.Fatalf("скрипт посева %s не читается (%v) — вторую половину требуемого "+
			"множества вывести не из чего", seedScript, err)
	}
	for _, m := range seedCreates.FindAllStringSubmatch(string(seed), -1) {
		req[m[1]] = true
		f.mentioned[m[1]] = true
	}

	rollout, err := os.ReadFile(rolloutScript) // #nosec G304 -- координата дерева
	if err != nil {
		t.Fatalf("рецепт раскатки %s не читается (%v) — предмет проверки исчез", rolloutScript, err)
	}
	rb := string(rollout)
	// ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ. Признаки ниже ищутся по коду с вырезанными
	// строками-комментариями: слова `helm template` и `create secret generic`
	// стоят и в прозе этого же скрипта, объясняющей саму проверку, — и гейт по
	// подстроке краснел бы на собственном объяснении, а зеленел бы на снятой
	// проверке (`testing.md` §«Гейт на класс», п.4).
	code := stripShellComments(rb)
	for _, m := range producerRow.FindAllStringSubmatch(rb, -1) {
		f.producers[m[1]] = m[2]
		req[m[1]] = true
	}
	for n := range req {
		f.required = append(f.required, n)
	}
	sort.Strings(f.required)

	// Перечень требуемого ВЫВОДИТСЯ, и судится это по САМОМУ БЛОКУ ВЫВОДА, а не
	// по упоминанию слова где угодно в скрипте: те же слова стоят в текстах
	// отказов этого же предполёта, и проверка по подстроке зеленела бы на
	// сузившемся выводе, оставшись красной ровно там, где всё исправно.
	block := derivationBlock(code)
	if strings.Contains(block, "$RENDER") && renderAssign.MatchString(code) {
		f.derives = append(f.derives, "рендер")
	}
	if strings.Contains(block, "create secret generic") {
		f.derives = append(f.derives, "посев")
	}
	if strings.Contains(block, "REQUIRED_SECRET_PRODUCERS") {
		f.derives = append(f.derives, "таблица")
	}

	// Отказ обязан стоять РАНЬШЕ применения: иначе он не отказ, а объяснение
	// уже случившегося.
	iPre := strings.Index(code, "REQUIRED_SECRET_PRODUCERS")
	iApply := strings.Index(code, "helm upgrade \"$RELEASE\"")
	f.before = iPre >= 0 && iApply > iPre
	return f
}

// derivationBlock — тело подстановки, которой предполёт СОБИРАЕТ перечень
// требуемого. Пустая строка означает, что перечень больше не выводится вовсе, —
// и это находка, а не «источник не найден».
func derivationBlock(code string) string {
	const open = `REQUIRED="$(`
	i := strings.Index(code, open)
	if i < 0 {
		return ""
	}
	rest := code[i+len(open):]
	j := strings.Index(rest, "\n)\"")
	if j < 0 {
		return rest
	}
	return rest[:j]
}

// stripShellComments — вырезает строки-комментарии, сохраняя число строк.
// Признак ищется в том, что ИСПОЛНЯЕТСЯ; проза о самой проверке исполняемой
// частью не является.
func stripShellComments(body string) string {
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimLeft(ln, " \t"), "#") {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// mandatorySecretRefs — литеральные имена секретов в ОБЯЗАТЕЛЬНЫХ ссылках.
// Ссылка с `optional: true` предметом здесь не является: под с ней поднимется, а
// откажет уже страж старта сервиса — и такой секрет приходит из посева.
func mandatorySecretRefs(body string) []string {
	lines := strings.Split(body, "\n")
	var out []string
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "secretKeyRef:" {
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		name, optional := "", false
		for j := i + 1; j < len(lines); j++ {
			cur := lines[j]
			if strings.TrimSpace(cur) == "" {
				continue
			}
			if len(cur)-len(strings.TrimLeft(cur, " ")) <= indent {
				break
			}
			s := scalarLine.FindStringSubmatch(cur)
			if s == nil {
				continue
			}
			switch s[2] {
			case "name":
				name = strings.Trim(s[3], `"'`)
			case "optional":
				optional = strings.TrimSpace(s[3]) == "true"
			}
		}
		if optional || name == "" || strings.Contains(name, "{{") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами.

func scanPreflightCoverage(f preflightFacts) []string {
	var out []string
	for _, n := range f.required {
		if _, ok := f.producers[n]; !ok {
			out = append(out, fmt.Sprintf(
				"секрет %s требуется раскаткой, а производитель на боевой площадке НЕ НАЗВАН. "+
					"«Заводит посев» производителем здесь не является: посев на этой площадке не зовётся, "+
					"и недостающий секрет обходится оператору в молчаливое ожидание готовности "+
					"с отказом ПО СРОКУ, а не по причине", n))
		}
	}
	names := make([]string, 0, len(f.producers))
	for n := range f.producers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !f.mentioned[n] {
			out = append(out, fmt.Sprintf(
				"таблица производителей называет %s, а требования этого секрета в дереве больше нет — "+
					"строка пережила свой предмет; послабление обязано истекать само", n))
		}
	}
	want := []string{"рендер", "посев", "таблица"}
	for _, w := range want {
		found := false
		for _, d := range f.derives {
			if d == w {
				found = true
			}
		}
		if !found {
			out = append(out, fmt.Sprintf(
				"предполёт не выводит перечень требуемого из источника %q — "+
					"проверка по одному имени растёт вместе с перечнем и не растёт вместе с деревом", w))
		}
	}
	if !f.before {
		out = append(out, "отказ предполёта не стоит РАНЬШЕ применения: отказ после `helm upgrade` "+
			"перестаёт быть предполётом и становится объяснением уже случившегося")
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЕРКА ПО ДЕРЕВУ

func TestRolloutPreflightCoversEveryRequiredSecret(t *testing.T) {
	f := preflightFactsFromTree(t)

	covered := 0
	for _, n := range f.required {
		if _, ok := f.producers[n]; ok {
			covered++
		}
	}
	t.Logf("осмотрено: объявлений %d; требуется секретов %d (%s); с названным производителем %d; "+
		"строк таблицы %d; предполёт выводит из %v; отказ раньше применения %v",
		f.scanned, len(f.required), strings.Join(f.required, ", "), covered,
		len(f.producers), f.derives, f.before)

	if f.scanned == 0 {
		t.Fatalf("объявлений не прочитано ни одного — обход пуст, и это отказ, а не успех")
	}
	if len(f.required) == 0 {
		t.Fatalf("требуемых секретов не выведено ни одного — «ноль находок» здесь " +
			"неотличимо от «ноль прочитанного»")
	}
	if len(f.producers) == 0 {
		t.Fatalf("таблица производителей в %s не прочитана — либо она снята, "+
			"либо её форма изменилась и разбор ослеп", rolloutScript)
	}

	for _, msg := range scanPreflightCoverage(f) {
		t.Errorf("%s", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе.

func TestScanPreflightCoverage_SelfTest(t *testing.T) {
	base := preflightFacts{
		required:  []string{"секрет-а", "секрет-б"},
		producers: map[string]string{"секрет-а": "оператор", "секрет-б": "оператор"},
		mentioned: map[string]bool{"секрет-а": true, "секрет-б": true},
		derives:   []string{"рендер", "посев", "таблица"},
		before:    true,
		scanned:   10,
	}

	// (0) КОНТРОЛЬ: полный охват — молчание.
	if got := scanPreflightCoverage(base); len(got) != 0 {
		t.Errorf("(0) полный охват обязан молчать: %v", got)
	}

	// (A) ИНЪЕКЦИЯ — ровно исходный дефект: требуемый секрет без производителя.
	gap := base
	gap.required = append(append([]string{}, base.required...), "секрет-из-посева")
	gap.mentioned = map[string]bool{"секрет-а": true, "секрет-б": true, "секрет-из-посева": true}
	got := scanPreflightCoverage(gap)
	if len(got) == 0 || !strings.Contains(got[0], "секрет-из-посева") {
		t.Errorf("(A) требуемый секрет без производителя ПРОПУЩЕН: %v", got)
	}

	// (B) КОНТРОЛЬ ТОЙ ЖЕ ФОРМЫ: тот же секрет, но производитель назван —
	//     молчание. Без этого (A) зеленело бы на любом добавлении.
	twin := gap
	twin.producers = map[string]string{"секрет-а": "оператор", "секрет-б": "оператор",
		"секрет-из-посева": "оператор"}
	if got := scanPreflightCoverage(twin); len(got) != 0 {
		t.Errorf("(B) названный производитель обязан молчать: %v", got)
	}

	// (C) ИНЪЕКЦИЯ в обратную сторону: строка таблицы пережила свой предмет.
	stale := base
	stale.producers = map[string]string{"секрет-а": "оператор", "секрет-б": "оператор",
		"секрет-снятый": "оператор"}
	stale.required = []string{"секрет-а", "секрет-б", "секрет-снятый"}
	got = scanPreflightCoverage(stale)
	if len(got) == 0 || !strings.Contains(got[0], "секрет-снятый") {
		t.Errorf("(C) строка без предмета ПРОПУЩЕНА: %v", got)
	}

	// (D) ИНЪЕКЦИЯ: предполёт перестал выводить перечень из посева — ровно та
	//     слепота, из-за которой три секрета из четырёх не проверялись.
	narrow := base
	narrow.derives = []string{"рендер", "таблица"}
	got = scanPreflightCoverage(narrow)
	if len(got) == 0 || !strings.Contains(got[0], "посев") {
		t.Errorf("(D) сузившийся вывод перечня ПРОПУЩЕН: %v", got)
	}

	// (E) ИНЪЕКЦИЯ: отказ переехал за применение.
	late := base
	late.before = false
	if got := scanPreflightCoverage(late); len(got) == 0 {
		t.Errorf("(E) отказ после применения ПРОПУЩЕН")
	}
}

// TestPreflightPredicates_RecogniseTheRealTree — разбор узнаёт формы настоящего
// дерева: обязательную ссылку, необязательную, строку таблицы, создание секрета
// посевом. Без этого самопроверка доказывала бы работоспособность ядра на входе,
// которого не бывает.
func TestPreflightPredicates_RecogniseTheRealTree(t *testing.T) {
	req := mandatorySecretRefs("" +
		"      - name: ПЕРЕМЕННАЯ\n" +
		"        valueFrom:\n" +
		"          secretKeyRef:\n" +
		"            name: секрет-обязательный\n" +
		"            key: token\n" +
		"      - name: ДРУГАЯ\n" +
		"        valueFrom:\n" +
		"          secretKeyRef:\n" +
		"            name: секрет-необязательный\n" +
		"            key: token\n" +
		"            optional: true\n")
	if len(req) != 1 || req[0] != "секрет-обязательный" {
		t.Errorf("разбор обязательных ссылок вернул %v — необязательная обязана быть исключена, "+
			"обязательная найдена", req)
	}

	f := preflightFactsFromTree(t)
	if len(f.producers) < 2 {
		t.Errorf("строк таблицы производителей прочитано %d — форма строки узнаётся "+
			"неверно либо таблица выродилась", len(f.producers))
	}
	seeded := 0
	seed, err := os.ReadFile(seedScript) // #nosec G304 -- координата дерева
	if err != nil {
		t.Fatalf("чтение %s: %v", seedScript, err)
	}
	seeded = len(seedCreates.FindAllString(string(seed), -1))
	if seeded == 0 {
		t.Errorf("посев не создаёт ни одного секрета по разбору — предпосылка исчезла, " +
			"и требуемое множество вышло бы неполным молча")
	}
	t.Logf("предпосылки: строк таблицы %d, секретов заводит посев %d, требуется всего %d",
		len(f.producers), seeded, len(f.required))
}
