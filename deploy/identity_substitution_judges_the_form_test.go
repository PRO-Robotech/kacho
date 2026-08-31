// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_substitution_judges_the_form_test.go — шаг подстановки обязан судить
// остаток по ФОРМЕ ссылки, а не по перечню имён.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Шаг, подставляющий величины в конфигурацию службы личности, проверял остаток
// ПО КОНКРЕТНОМУ ИМЕНИ. В той же карте настроек уже жили ссылки ТОЙ ЖЕ ФОРМЫ вне
// его поля зрения. Проверка по перечню имён растёт вместе с перечнем и НЕ растёт
// вместе с деревом: следующая величина уедет неподставленной, а шаг смолчит —
// потому что искал не её.
//
// Это тот же класс, что «распознаватель обязан знать ВСЕ законные формы записи
// предмета» (`testing.md` §«Гейт на класс», п.7): форма, о которой проверка не
// знает, не даёт ни красного, ни зелёного — она МОЛЧИТ, и всё записанное в ней
// оказывается вне наблюдения.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ТРИ ВЕЩИ, И ТРЕТЬЯ НЕСУЩАЯ
//
//  1. поиск остатка идёт КЛАССОМ СИМВОЛОВ по форме `${ИМЯ}`; перечнем имён
//     класс невыразим by construction;
//  2. отказ закрыт: пустая либо недоехавшая величина роняет запуск;
//  3. ПЕРЕЧЕНЬ ИМЁН, КОТОРЫМИ ШАГ ВЛАДЕЕТ, СОГЛАСОВАН С ЕГО ЖЕ ПЕРЕМЕННЫМИ.
//     Он объявлен рядом, потому что ссылка на секрет необязательна: отсутствующий
//     секрет даёт ОТСУТСТВУЮЩУЮ переменную, неотличимую от ссылки, чей источник —
//     переменная по ПУТИ КЛЮЧА самой службы личности. Без объявления отказ
//     перестал бы быть закрытым ровно в том случае, ради которого он заведён.
//     Два места об одном предмете разошлись бы молча — здесь они не могут: имя,
//     попавшее в одно и не попавшее в другое, роняет прогон.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА
//
//   - Проверяется, ЧЕМ судится остаток, а не сам механизм подстановки: он верен
//     и остаётся.
//   - Ссылка, у которой переменной нет вовсе, находкой НЕ считается: её источник
//     законен и другой. Что он есть, судит
//     deploy/tests/helm/identity-hook-credential-source-test.sh по отрендеренным
//     подам, где путь ключа вычислим. Здесь такая ссылка только пересчитывается.
//   - Проверка читает ОБЪЯВЛЕНИЕ, а не рендер: рендер требует `helm` и скачанных
//     зависимостей, поэтому проверка над ним умеет пропуститься, а пропущенная
//     проверка не краснеет никогда.
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

// substDefine — имя объявления шага подстановки берётся не из головы: это
// единственное объявление подчарта, чьё тело пишет файл, читаемый процессом.
// Здесь оно находится по признаку — телу, несущему перенаправление в этот файл.
var defineStart = regexp.MustCompile(`\{\{-?\s*define\s+"([^"]+)"\s*-?\}\}`)

// formClass — класс символов, которым ищется ссылка. Перечнем имён он невыразим:
// именно это и делает поиск судящим ФОРМУ.
const formClass = `[A-Za-z_][A-Za-z0-9_]*`

// ownedVarName — имя переменной, объявляющей перечень имён во владении шага.
// Оно выводится из самого шага: переменная, чьё значение — список имён, каждое
// из которых объявлено секретом в этом же контейнере.
const ownedListVar = "KACHO_IDENTITY_SUBSTITUTED_VARS"

// substFacts — то, что проверка вывела из объявления.
type substFacts struct {
	file        string
	body        string   // тело объявления шага подстановки
	ownedList   []string // имена, объявленные перечнем во владении
	secretVars  []string // переменные шага, приходящие из секрета
	selfRead    []string // из них: читаемые самим шагом (свой страж), не предмет подстановки
	configRefs  []string // имена ссылок `${…}` в конфигурации
	judgesForm  bool     // остаток ищется классом символов
	closedEmpty bool     // пустая/недоехавшая величина роняет запуск
	refuses     bool     // у ветки остатка есть ненулевой выход
}

// defineBodies — тела всех объявлений файла шаблона.
func defineBodies(body string) map[string]string {
	out := map[string]string{}
	idx := defineStart.FindAllStringSubmatchIndex(body, -1)
	for _, m := range idx {
		name := body[m[2]:m[3]]
		rest := body[m[1]:]
		endRe := regexp.MustCompile(`\{\{-?\s*end\s*-?\}\}`)
		// Тело объявления кончается на первом `end`, не открытом внутри него.
		depth, pos := 0, 0
		for {
			openIdx := regexp.MustCompile(`\{\{-?\s*(if|range|with|define|block)\b`).FindStringIndex(rest[pos:])
			endIdx := endRe.FindStringIndex(rest[pos:])
			if endIdx == nil {
				break
			}
			if openIdx != nil && openIdx[0] < endIdx[0] {
				depth++
				pos += openIdx[1]
				continue
			}
			if depth == 0 {
				out[name] = rest[:pos+endIdx[0]]
				break
			}
			depth--
			pos += endIdx[1]
		}
	}
	return out
}

// envSecretVars — переменные окружения блока, приходящие из секрета.
func envSecretVars(block string) []string {
	lines := strings.Split(block, "\n")
	var out []string
	for i, ln := range lines {
		m := envDeclLine.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		indent := len(m[1])
		for j := i + 1; j < len(lines); j++ {
			cur := lines[j]
			if strings.TrimSpace(cur) == "" {
				continue
			}
			if len(cur)-len(strings.TrimLeft(cur, " ")) <= indent {
				break
			}
			if strings.TrimSpace(cur) == "secretKeyRef:" {
				out = append(out, m[2])
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// envLiteralValue — литеральное значение переменной блока.
func envLiteralValue(block, name string) (string, bool) {
	lines := strings.Split(block, "\n")
	for i, ln := range lines {
		m := envDeclLine.FindStringSubmatch(ln)
		if m == nil || m[2] != name {
			continue
		}
		indent := len(m[1])
		for j := i + 1; j < len(lines); j++ {
			cur := lines[j]
			if strings.TrimSpace(cur) == "" {
				continue
			}
			if len(cur)-len(strings.TrimLeft(cur, " ")) <= indent {
				break
			}
			if s := scalarLine.FindStringSubmatch(cur); s != nil && s[2] == "value" {
				return strings.Trim(s[3], `"'`), true
			}
		}
	}
	return "", false
}

func substitutionFacts(t *testing.T) substFacts {
	t.Helper()
	f := substFacts{}

	// Шаг подстановки выводится ОДНАЖДЫ — соседним гейтом монтирования, — и
	// читается здесь. Два расчёта одного и того же разъезжаются молча.
	mf := identityMountFacts(t)
	f.file, f.body = mf.stepCoord, mf.stepBody

	tpls, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "templates", "*"))
	if err != nil {
		t.Fatalf("обход шаблонов подчартов: %v", err)
	}
	sort.Strings(tpls)
	for _, p := range tpls {
		b, rerr := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if rerr != nil {
			continue
		}
		// Конфигурация — то объявление, чьё тело несёт маршрут обратных вызовов.
		for _, blk := range defineBodies(string(b)) {
			if !strings.Contains(blk, callbackRoute) {
				continue
			}
			for _, m := range dollarRef.FindAllStringSubmatch(blk, -1) {
				f.configRefs = append(f.configRefs, m[1])
			}
		}
	}
	f.configRefs = uniqueSorted(f.configRefs)

	if f.body == "" {
		return f
	}
	f.secretVars = envSecretVars(f.body)
	if v, ok := envLiteralValue(f.body, ownedListVar); ok {
		f.ownedList = uniqueSorted(strings.Fields(v))
	}
	f.judgesForm = strings.Contains(f.body, formClass)
	f.closedEmpty = strings.Contains(f.body, ownedListVar+":?")
	f.refuses = strings.Contains(f.body, "exit 1")
	return f
}

func uniqueSorted(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами.

func scanSubstitution(f substFacts) []string {
	var out []string
	if !f.judgesForm {
		out = append(out, fmt.Sprintf(
			"%s: остаток ищется НЕ по форме — класса символов %q в шаге нет. "+
				"Проверка по перечню имён растёт вместе с перечнем и не растёт вместе с деревом: "+
				"ссылка иной формы уедет неподставленной, и шаг смолчит", f.file, formClass))
	}
	if !f.refuses {
		out = append(out, fmt.Sprintf(
			"%s: у шага нет ненулевого выхода — остаток не отвергается вовсе", f.file))
	}
	if !f.closedEmpty {
		out = append(out, fmt.Sprintf(
			"%s: перечень имён во владении (%s) не объявлен обязательным — "+
				"отсутствующая переменная стала бы неотличима от ссылки, чей источник "+
				"переменная по пути ключа, и отказ перестал бы быть закрытым",
			f.file, ownedListVar))
	}

	owned := map[string]bool{}
	for _, n := range f.ownedList {
		owned[n] = true
	}
	for _, n := range f.secretVars {
		if !owned[n] {
			// Законное исключение ОДНО, и оно проверяемо: переменная, которую
			// шаг читает САМ. Такая не подставляется в конфигурацию — её
			// применяет процесс (служба личности переопределяет ключи
			// переменными, и переменная бьёт файл), поэтому в перечне владения
			// шага ей места нет by construction: подставлять нечего.
			//
			// Молча это НЕ пропускается: требуется, чтобы величина читалась в
			// теле шага, то есть чтобы у переменной был СВОЙ страж. Переменная
			// из секрета, которую никто не подставляет и никто не читает,
			// остаётся находкой — её пустая величина прошла бы молча, и это
			// ровно то, ради чего проверка написана.
			if strings.Contains(f.body, "${"+n) {
				f.selfRead = append(f.selfRead, n)
				continue
			}
			out = append(out, fmt.Sprintf(
				"%s: переменная %s приходит из секрета, но не названа в %s — "+
					"её пустая либо недоехавшая величина прошла бы молча",
				f.file, n, ownedListVar))
		}
	}
	secret := map[string]bool{}
	for _, n := range f.secretVars {
		secret[n] = true
	}
	refs := map[string]bool{}
	for _, n := range f.configRefs {
		refs[n] = true
	}
	for _, n := range f.ownedList {
		if !secret[n] {
			out = append(out, fmt.Sprintf(
				"%s: %s называет %s, а переменной с таким именем у шага нет — "+
					"объявление пережило свой предмет", f.file, ownedListVar, n))
		}
		if !refs[n] {
			out = append(out, fmt.Sprintf(
				"%s: %s называет %s, а в конфигурации ссылки ${%s} нет — "+
					"шаг владеет тем, чего никто не просит", f.file, ownedListVar, n, n))
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЕРКА ПО ДЕРЕВУ

func TestIdentitySubstitutionJudgesTheFormNotOneName(t *testing.T) {
	f := substitutionFacts(t)

	var foreign []string
	owned := map[string]bool{}
	for _, n := range f.ownedList {
		owned[n] = true
	}
	for _, n := range f.configRefs {
		if !owned[n] {
			foreign = append(foreign, n)
		}
	}

	t.Logf("осмотрено: шаг подстановки %q; ссылок формы в конфигурации %d (%s); "+
		"во владении шага %d (%s); переменных из секрета %d, из них читает сам шаг %d (%s); "+
		"источник по пути ключа %d (%s); судит форму %v",
		f.file, len(f.configRefs), strings.Join(f.configRefs, ", "),
		len(f.ownedList), strings.Join(f.ownedList, ", "), len(f.secretVars),
		len(f.selfRead), strings.Join(f.selfRead, ", "),
		len(foreign), strings.Join(foreign, ", "), f.judgesForm)

	if f.body == "" {
		t.Fatalf("шаг подстановки не найден — либо он снят, либо разбор ослеп; " +
			"«ноль находок» здесь неотличимо от «ноль прочитанного», поэтому это отказ")
	}
	if len(f.configRefs) == 0 {
		t.Fatalf("в конфигурации не найдено ни одной ссылки формы ${ИМЯ} — " +
			"предмет подстановки исчез либо разбор ослеп")
	}

	for _, msg := range scanSubstitution(f) {
		t.Errorf("%s", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе.

func TestScanSubstitution_SelfTest(t *testing.T) {
	base := substFacts{
		file:        "шаблон.tpl:шаг",
		body:        "тело",
		ownedList:   []string{"НАШ_ТОКЕН"},
		secretVars:  []string{"НАШ_ТОКЕН"},
		configRefs:  []string{"НАШ_ТОКЕН", "ЧУЖОЙ_ПО_ПУТИ_КЛЮЧА"},
		judgesForm:  true,
		closedEmpty: true,
		refuses:     true,
	}

	// (0) КОНТРОЛЬ: согласованный шаг — молчание. Ссылка, у которой переменной
	//     нет (источник по пути ключа), находкой НЕ считается: это законный
	//     близнец, и без него отрицание зеленело бы на всём сломанном.
	if got := scanSubstitution(base); len(got) != 0 {
		t.Errorf("(0) согласованный шаг обязан молчать: %v", got)
	}

	// (A) ИНЪЕКЦИЯ — ровно исходный дефект: остаток судится по имени.
	byName := base
	byName.judgesForm = false
	got := scanSubstitution(byName)
	if len(got) == 0 || !strings.Contains(got[0], "НЕ по форме") {
		t.Errorf("(A) поиск по имени ПРОПУЩЕН: %v", got)
	}

	// (B) ИНЪЕКЦИЯ: секретная переменная не названа в перечне владения — её
	//     недоехавшая величина прошла бы молча.
	gap := base
	gap.secretVars = []string{"НАШ_ТОКЕН", "НОВЫЙ_ТОКЕН"}
	got = scanSubstitution(gap)
	if len(got) == 0 || !strings.Contains(got[0], "НОВЫЙ_ТОКЕН") {
		t.Errorf("(B) незаявленная секретная переменная ПРОПУЩЕНА: %v", got)
	}

	// (C) ИНЪЕКЦИЯ в обратную сторону: перечень владения пережил свой предмет.
	stale := base
	stale.ownedList = []string{"НАШ_ТОКЕН", "СНЯТЫЙ_ТОКЕН"}
	got = scanSubstitution(stale)
	if len(got) == 0 {
		t.Errorf("(C) перечень владения без предмета ПРОПУЩЕН")
	} else {
		joined := strings.Join(got, " | ")
		if !strings.Contains(joined, "СНЯТЫЙ_ТОКЕН") {
			t.Errorf("(C) находка не называет имя: %s", joined)
		}
	}

	// (D) ИНЪЕКЦИЯ: обязательность перечня снята — отказ перестал быть закрытым.
	open := base
	open.closedEmpty = false
	if got := scanSubstitution(open); len(got) == 0 {
		t.Errorf("(D) снятая обязательность перечня ПРОПУЩЕНА")
	}

	// (E) ИНЪЕКЦИЯ: у шага нет ненулевого выхода.
	silent := base
	silent.refuses = false
	if got := scanSubstitution(silent); len(got) == 0 {
		t.Errorf("(E) шаг без отказа ПРОПУЩЕН")
	}
}

// TestSubstitutionPredicates_RecogniseTheRealTree — разбор узнаёт формы
// НАСТОЯЩЕГО дерева: объявление шаблона, переменную из секрета, литеральное
// значение. Без этого самопроверка доказывала бы работоспособность ядра на
// входе, которого не бывает.
func TestSubstitutionPredicates_RecogniseTheRealTree(t *testing.T) {
	f := substitutionFacts(t)
	if len(f.secretVars) == 0 {
		t.Errorf("переменных из секрета у шага не найдено — разбор объявления ослеп")
	}
	if len(f.ownedList) == 0 {
		t.Errorf("перечень имён во владении не прочитан — разбор литерального значения ослеп")
	}
	// Предмет проверки в том, что в конфигурации есть ОБЕ полосы: наша и чужая
	// (по пути ключа). Ноль в любой из них означает, что сверять нечего.
	owned := map[string]bool{}
	for _, n := range f.ownedList {
		owned[n] = true
	}
	var ours, theirs int
	for _, n := range f.configRefs {
		if owned[n] {
			ours++
		} else {
			theirs++
		}
	}
	if ours == 0 || theirs == 0 {
		t.Errorf("ссылок во владении %d, по пути ключа %d — сверка осмысленна, "+
			"только пока в дереве есть обе полосы", ours, theirs)
	}
	t.Logf("предпосылки: переменных из секрета %d, имён во владении %d, "+
		"ссылок наших %d, чужих %d", len(f.secretVars), len(f.ownedList), ours, theirs)
}
