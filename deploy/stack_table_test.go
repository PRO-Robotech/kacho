// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stack_table_test.go — состав стенда объявляется В ОДНОМ месте, и это место
// знает КАЖДЫЙ профиль дерева.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// «Стек» — это имя и упорядоченная цепочка файлов значений, которыми стенд
// поднимается (`-f a.yaml -f b.yaml …`). Цепочка нужна почти каждой проверке
// развёртывания, поэтому её выписывали столько раз, сколько проверок, — и
// копии разъехались.
//
// Расхождение копий не даёт красного НИ ОДНОЙ из них: каждая честно проверяет
// то, что сама объявила. Наружу это выглядит как согласие проверок, хотя они
// отвечают на вопрос о РАЗНЫХ стендах. Наблюдалось дважды за один вечер:
//
//   - слой посадки боевого набора вынесли в отслеживаемый файл — часть таблиц
//     о нём узнала, часть нет, и «ноль находок» по этому слою у вторых означало
//     «ноль прочитанного»;
//   - две таблицы разошлись о среднем слое накладки образов: одна перечисляла
//     его, другая нет. Их ответы на вопрос «объявил ли этот стек боевую
//     посадку» ВЗАИМОИСКЛЮЧАЮЩИЕ, и обе были зелёными.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ ДЕРЖИТСЯ
//
//  1. TestNoSecondCopyOfAStackChain — цепочку объявляет ТОЛЬКО таблица.
//     Файл, назвавший два и более профиля умбреллы в одной конструкции,
//     завёл вторую копию — находка с координатой.
//  2. TestStackTableNamesEveryTrackedProfile — таблица знает КАЖДЫЙ профиль,
//     который лежит в дереве. Профиль, которого не называет ни одна цепочка,
//     не осматривается ни одной проверкой развёртывания, и его «ноль находок»
//     неотличим от «ноль прочитанного». Осознанное исключение записывается с
//     причиной И с предикатом, по которому оно ИСТЕКАЕТ САМО.
//
// Обе печатают объём осмотренного: сколько файлов прочитано, сколько строк,
// сколько профилей известно. Обход, переставший узнавать дерево, обязан быть
// отличим от чистого дерева.
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

// repoRoot — корень репозитория, читаемый от каталога deploy.
const repoRoot = ".."

// profileNamePattern — имя файла значений умбреллы, каким его пишут в командной
// строке helm. Совпадение по ИМЕНИ, а не по пути: одна и та же цепочка
// пишется и `values.dev.yaml`, и `./helm/umbrella/values.dev.yaml`, и
// `"$UMBRELLA/values.dev.yaml"`.
var profileNamePattern = regexp.MustCompile(`values[A-Za-z0-9._-]*\.yaml`)

// knownUntrackedProfiles — профили, которые в дереве не лежат и лежать не
// могут, но в цепочке участвуют. Сегодня такой ровно один: слой учётных данных
// боевой площадки. Он назван здесь, чтобы предикат «это профиль умбреллы»
// узнавал его наравне с отслеживаемыми, — иначе строка, называющая ЕГО и один
// отслеживаемый профиль, читалась бы как одиночная и вторая копия цепочки
// прошла бы незамеченной.
var knownUntrackedProfiles = []string{"values.fe3455-ory.yaml"}

// umbrellaProfileNames — множество имён профилей умбреллы: отслеживаемые файлы
// дерева плюс известные неотслеживаемые. Выводится, а не выписывается.
func umbrellaProfileNames(t *testing.T) map[string]bool {
	t.Helper()
	tracked, err := treecorpus.Under(umbrellaDir)
	if err != nil {
		t.Fatalf("состав %s: %v — множество профилей взять неоткуда, "+
			"и «ноль находок» стало бы свойством рабочего каталога", umbrellaDir, err)
	}
	out := map[string]bool{}
	for _, p := range tracked {
		base := filepath.Base(p)
		if filepath.Dir(p) != mustAbs(t, umbrellaDir) {
			continue // подчарты и шаблоны — не профили умбреллы
		}
		if profileNamePattern.FindString(base) == base {
			out[base] = true
		}
	}
	for _, n := range knownUntrackedProfiles {
		out[n] = true
	}
	return out
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("абсолютный путь для %s: %v", p, err)
	}
	return abs
}

// ─────────────────────────────────────────────────────────────────────────────
// (1) ЦЕПОЧКУ ОБЪЯВЛЯЕТ ТОЛЬКО ТАБЛИЦА.

// chainCarrierCorpus — файлы, которые МОГУТ объявить цепочку: исполняемые
// проверки, скрипты, цели сборки и конвейер. Шаблоны чартов и сами профили сюда
// НЕ входят by construction — они helm не зовут, а прозу о соседних профилях
// пишут законно.
func chainCarrierCorpus(t *testing.T) []string {
	t.Helper()

	// Собственный файл гейта из обхода ИСКЛЮЧЁН, и это не послабление, а различение
	// предмета: его цепочки — строковые литералы инъекционных фикстур, то есть
	// вход, который гейт обязан РАЗБИРАТЬ, а не находка, которую он обязан
	// называть. Без исключения гейт краснеет на самом себе и не может быть зелёным
	// ни при каком состоянии дерева — то есть перестаёт что-либо утверждать о нём.
	//
	// Исключение узкое (ровно один путь) и ИСТЕКАЕТ САМО: если файл переименуют или
	// уберут, предпосылка ниже роняет прогон, и слепая зона не унаследуется молча.
	// Обходчик отдаёт АБСОЛЮТНЫЕ пути, поэтому сравнивать с относительным нельзя:
	// сравнение молча не сойдётся ни разу, и предпосылка ниже это поймает.
	selfRel := filepath.Join("deploy", "stack_table_test.go")
	self, err := filepath.Abs(filepath.Join(repoRoot, selfRel))
	if err != nil {
		t.Fatalf("абсолютный путь собственного файла гейта: %v", err)
	}
	if _, err := os.Stat(self); err != nil {
		t.Fatalf("исключению из обхода нечего исключать: %s не читается (%v). "+
			"Файл переименован или снят — верни исключение к действительности, "+
			"иначе фикстуры гейта снова станут его же находками", selfRel, err)
	}
	selfSkipped := 0

	var out []string
	for _, dir := range []string{"deploy", "gateway", ".github/workflows"} {
		root := filepath.Join(repoRoot, dir)
		files, err := treecorpus.Under(root)
		if err != nil {
			t.Fatalf("состав %s: %v", root, err)
		}
		for _, p := range files {
			if p == self {
				selfSkipped++
				continue
			}
			base := filepath.Base(p)
			switch {
			case strings.HasSuffix(base, ".sh"),
				strings.HasSuffix(base, "_test.go"),
				base == "Makefile",
				strings.Contains(p, string(filepath.Separator)+"workflows"+string(filepath.Separator)):
				out = append(out, p)
			}
		}
	}
	if selfSkipped != 1 {
		t.Fatalf("собственный файл гейта встречен %d раз(а) в обходе, ожидался ровно один: "+
			"обход изменился, и исключение либо не сработало, либо сработало дважды", selfSkipped)
	}
	// Сама таблица добавляется ЯВНО, а не попадает сюда расширением: тогда
	// пропуск ниже — живая ветка, а не украшение, и её строки входят в объём
	// осмотренного. Сменят таблице расширение — обход этого не заметит, и
	// единственная выписанная цепочка дерева станет находкой в собственном гейте.
	out = append(out, mustAbs(t, stacksTable))
	sort.Strings(out)
	return out
}

// logicalLines склеивает продолжения строк (`\` в конце) и вырезает
// комментарии: `#` в начале строки для shell/Makefile/yaml и `//` для Go.
// Комментарий вырезается ИМЕННО как строка целиком — вырезать хвост после `#`
// было бы неверно внутри кавычек, а нам достаточно начала: прозу о цепочке
// пишут отдельным комментарием, а не хвостом исполняемой строки.
func logicalLines(body string) []struct {
	No   int
	Text string
} {
	var out []struct {
		No   int
		Text string
	}
	raw := strings.Split(body, "\n")
	for i := 0; i < len(raw); i++ {
		trimmed := strings.TrimSpace(raw[i])
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		text := raw[i]
		no := i + 1
		for strings.HasSuffix(strings.TrimRight(text, " \t"), `\`) && i+1 < len(raw) {
			text = strings.TrimSuffix(strings.TrimRight(text, " \t"), `\`) + " " + raw[i+1]
			i++
		}
		out = append(out, struct {
			No   int
			Text string
		}{no, text})
	}
	return out
}

// chainLiterals — конструкции, называющие ДВА И БОЛЕЕ профиля умбреллы в одной
// логической строке. Два профиля рядом — это уже цепочка: порядок наложения
// определён, и он либо совпадает с таблицей, либо расходится с ней молча.
func chainLiterals(body string, known map[string]bool) []struct {
	No       int
	Profiles []string
	Text     string
} {
	var out []struct {
		No       int
		Profiles []string
		Text     string
	}
	for _, l := range logicalLines(body) {
		seen := map[string]bool{}
		var names []string
		for _, m := range profileNamePattern.FindAllString(l.Text, -1) {
			if !known[m] || seen[m] {
				continue
			}
			seen[m] = true
			names = append(names, m)
		}
		if len(names) >= 2 {
			out = append(out, struct {
				No       int
				Profiles []string
				Text     string
			}{l.No, names, strings.TrimSpace(l.Text)})
		}
	}
	return out
}

func TestNoSecondCopyOfAStackChain(t *testing.T) {
	known := umbrellaProfileNames(t)
	corpus := chainCarrierCorpus(t)
	table := mustAbs(t, stacksTable)

	lines, findings := 0, 0
	for _, p := range corpus {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		body := string(raw)
		lines += strings.Count(body, "\n") + 1
		if p == table {
			continue // единственное место, где цепочка и обязана быть выписана
		}
		rel, _ := filepath.Rel(mustAbs(t, repoRoot), p)
		for _, hit := range chainLiterals(body, known) {
			findings++
			t.Errorf("%s:%d — вторая копия цепочки стенда (%s). Состав стенда объявляет "+
				"ТОЛЬКО %s; копия расходится с ней молча, потому что каждая проверка честно "+
				"проверяет то, что сама объявила. Возьми цепочку из таблицы "+
				"(shell: tests/helm/stacks.sh, Go: deployStacks)",
				rel, hit.No, strings.Join(hit.Profiles, " + "), stacksTable)
		}
	}

	// Проверка СВОЕЙ предпосылки: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного». Обход, переставший узнавать профили или файлы, объявит
	// дерево чистым, ничего не осмотрев.
	if len(corpus) == 0 || len(known) == 0 || lines == 0 {
		t.Fatalf("обход ничего не прочитал: файлов=%d, строк=%d, известных профилей=%d — "+
			"предикат перестал узнавать дерево, а не дерево стало чистым",
			len(corpus), lines, len(known))
	}
	names := make([]string, 0, len(known))
	for n := range known {
		names = append(names, n)
	}
	sort.Strings(names)
	t.Logf("осмотрено: файлов, способных объявить цепочку — %d; строк — %d; "+
		"известных профилей умбреллы — %d (%s); вторых копий — %d",
		len(corpus), lines, len(known), strings.Join(names, " "), findings)
}

// ─────────────────────────────────────────────────────────────────────────────
// (2) ТАБЛИЦА ЗНАЕТ КАЖДЫЙ ПРОФИЛЬ ДЕРЕВА.

// profileExclusion — осознанное исключение из правила «каждый профиль назван
// цепочкой». Несёт причину И предикат, по которому истекает само: исключение,
// пережившее свой предмет, разрешает то, чего нет, и его унаследует следующая
// слепая зона.
type profileExclusion struct {
	why string
	// subject отвечает «предмет исключения ещё есть?» и объясняет, чем именно
	// он подтверждён. Предикат читает ДЕРЕВО, а не память автора.
	subject func(t *testing.T) (bool, string)
}

var recordedProfileExclusions = map[string]profileExclusion{
	"values.yaml": {
		why: "собственные умолчания чарта: helm читает этот файл ВСЕГДА и без `-f`, " +
			"поэтому слоем цепочки он не является и назвать его цепочкой нечем",
		subject: func(t *testing.T) (bool, string) {
			t.Helper()
			p := filepath.Join(umbrellaDir, "Chart.yaml")
			if _, err := os.Stat(p); err != nil {
				return false, fmt.Sprintf("%s рядом нет (%v) — это больше не корень чарта", p, err)
			}
			return true, "рядом лежит Chart.yaml — файл остаётся умолчаниями чарта"
		},
	},
	"values.digests.example.yaml": {
		why: "образец закрепления образов по digest: применять его как есть нельзя, " +
			"значения намеренно не являются digest'ами. Стеком он не становится, пока " +
			"остаётся образцом",
		subject: func(t *testing.T) (bool, string) {
			t.Helper()
			raw, err := os.ReadFile(filepath.Join(umbrellaDir, "values.digests.example.yaml"))
			if err != nil {
				return false, fmt.Sprintf("файл не читается (%v) — исключать нечего", err)
			}
			n := strings.Count(string(raw), "REPLACE_WITH_REAL_DIGEST")
			if n == 0 {
				return false, "заглушек REPLACE_WITH_REAL_DIGEST не осталось — файл стал " +
					"применимым как есть, то есть слоем развёртывания"
			}
			return true, fmt.Sprintf("заглушек REPLACE_WITH_REAL_DIGEST — %d, файл неприменим как есть", n)
		},
	},
}

func TestStackTableNamesEveryTrackedProfile(t *testing.T) {
	chains := deployStacks(t)
	known := umbrellaProfileNames(t)

	named := map[string]bool{}
	for _, chain := range chains {
		for _, p := range chain {
			named[p] = true
		}
	}

	untracked := map[string]bool{}
	for _, n := range knownUntrackedProfiles {
		untracked[n] = true
	}

	var missing []string
	tracked := 0
	for name := range known {
		if untracked[name] {
			continue // в дереве его нет — требовать от таблицы нечего
		}
		tracked++
		if named[name] {
			continue
		}
		if _, recorded := recordedProfileExclusions[name]; recorded {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)

	if tracked == 0 || len(chains) == 0 {
		t.Fatalf("обход ничего не прочитал: отслеживаемых профилей=%d, стеков в таблице=%d — "+
			"предикат перестал узнавать дерево, а не дерево стало чистым", tracked, len(chains))
	}
	t.Logf("осмотрено: отслеживаемых профилей умбреллы — %d; стеков в таблице — %d; "+
		"названо цепочками — %d; записанных исключений — %d; не названо — %d",
		tracked, len(chains), len(named), len(recordedProfileExclusions), len(missing))

	for _, name := range missing {
		t.Errorf("%s не назван НИ ОДНОЙ цепочкой таблицы %s — этот профиль не осматривает "+
			"ни одна проверка развёртывания, и её «ноль находок» по нему неотличим от «ноль "+
			"прочитанного». Либо внеси стек в таблицу, либо запиши исключение с причиной и "+
			"предикатом истечения в recordedProfileExclusions", name, stacksTable)
	}
}

// Записанное исключение обязано иметь предмет.
func TestRecordedProfileExclusions_StillHaveASubject(t *testing.T) {
	known := umbrellaProfileNames(t)
	chains := deployStacks(t)
	named := map[string]bool{}
	for _, chain := range chains {
		for _, p := range chain {
			named[p] = true
		}
	}

	for name, ex := range recordedProfileExclusions {
		if !known[name] {
			t.Errorf("исключение %q больше нечего исключать — такого профиля в дереве нет. "+
				"Удали запись из recordedProfileExclusions: исключение, пережившее свой "+
				"предмет, разрешает то, чего нет", name)
			continue
		}
		if named[name] {
			t.Errorf("исключение %q больше нечего исключать — профиль назван цепочкой таблицы. "+
				"Удали запись: держать оба значит объявлять профиль одновременно осмотренным "+
				"и освобождённым от осмотра", name)
			continue
		}
		ok, why := ex.subject(t)
		if !ok {
			t.Errorf("исключение %q потеряло основание: %s. Основание было: %s. "+
				"Либо внеси профиль в таблицу стеков, либо перепиши исключение под новое "+
				"основание — молчать здесь нельзя", name, why, ex.why)
			continue
		}
		t.Logf("исключение %q: предмет есть (%s)", name, why)
	}
	t.Logf("записанных исключений — %d", len(recordedProfileExclusions))
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе той же формы.

func TestChainLiterals_SelfTest(t *testing.T) {
	known := map[string]bool{
		"values.dev.yaml": true, "values.dev-prod.yaml": true, "values.prod.yaml": true,
	}

	// (а) внесённый дефект — вторая копия цепочки, в трёх формах, которыми её
	//     пишут в этом дереве.
	for _, body := range []string{
		"dev-prod:values.dev.yaml,values.dev-prod.yaml\n",
		"  \"dev-prod|values.dev.yaml values.dev-prod.yaml\"\n",
		"\t\"dev-prod\": {\"values.dev.yaml\", \"values.dev-prod.yaml\"},\n",
		"  helm template x . -f values.dev.yaml \\\n    -f values.dev-prod.yaml\n",
	} {
		got := chainLiterals(body, known)
		if len(got) != 1 || len(got[0].Profiles) != 2 {
			t.Fatalf("вторая копия цепочки не поймана в %q: %+v", body, got)
		}
	}

	// (б) законная конструкция ТОЙ ЖЕ формы — одиночный профиль. Молчит:
	//     одиночный `-f` цепочки не объявляет и противоречить составу не может.
	if got := chainLiterals("helm template x . -f values.prod.yaml\n", known); len(got) != 0 {
		t.Fatalf("одиночный профиль покрашен: %+v", got)
	}

	// (в) второй законный близнец — та же пара имён В КОММЕНТАРИИ. Проза о
	//     соседних профилях законна и обязана молчать, иначе гейт отключат
	//     первым же ложным срабатыванием.
	for _, body := range []string{
		"# values.dev.yaml / values.dev-prod.yaml — так поднимается стенд\n",
		"// values.dev.yaml + values.dev-prod.yaml\n",
	} {
		if got := chainLiterals(body, known); len(got) != 0 {
			t.Fatalf("комментарий покрашен (%q): %+v", body, got)
		}
	}

	// (г) третий законный близнец — имя, которое профилем умбреллы не является.
	//     Предикат обязан ключеваться на множестве из дерева, а не на форме имени.
	if got := chainLiterals("-f values.dev.yaml -f values.image-ids.yaml\n", known); len(got) != 0 {
		t.Fatalf("неизвестное имя засчитано за профиль: %+v", got)
	}

	// (д) повтор ОДНОГО профиля дважды в строке цепочкой не является.
	if got := chainLiterals("-f values.dev.yaml -f values.dev.yaml\n", known); len(got) != 0 {
		t.Fatalf("повтор одного профиля прочитан как цепочка: %+v", got)
	}
}

// Предикат обязан узнавать НАСТОЯЩЕЕ дерево, а не только синтетику.
func TestStackTablePredicates_RecogniseTheRealTree(t *testing.T) {
	known := umbrellaProfileNames(t)
	for _, want := range []string{
		"values.dev.yaml", "values.dev-prod.yaml", "values.prod.yaml",
		"values.fe3455-ory-posture.yaml", "values.prorobotech.yaml", "values.a8f60d.yaml",
	} {
		if !known[want] {
			t.Errorf("профиль %s не выведен из дерева — обход перестал его узнавать; выведено: %v",
				want, known)
		}
	}
	// Отрицание — только в паре с положительным: множество, в которое попадает
	// что угодно, зеленит проверку выше.
	for _, notAProfile := range []string{"Chart.yaml", "values.schema.json", "kaname"} {
		if known[notAProfile] {
			t.Errorf("%q засчитан профилем умбреллы — предикат стал слишком широким", notAProfile)
		}
	}

	// Корпус обязан включать все ЖИВЫЕ дома цепочек: shell-гейт, Go-проверку
	// шлюза, рецепт сборки, конвейер и саму таблицу. Дом, выпавший из обхода,
	// заводит вторую копию беззвучно — именно так копии и появились.
	corpus := chainCarrierCorpus(t)
	homes := map[string]bool{
		filepath.Join("deploy", "tests", "helm", "admin-hop-address-census-test.sh"): false,
		filepath.Join("gateway", "deploy", "revocation_endpoint_test.go"):            false,
		filepath.Join("deploy", "Makefile"):                                          false,
		filepath.Join(".github", "workflows", "ci.yaml"):                             false,
		filepath.Join("deploy", "stacks.txt"):                                        false,
	}
	root := mustAbs(t, repoRoot)
	for _, p := range corpus {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			continue
		}
		if _, ok := homes[rel]; ok {
			homes[rel] = true
		}
	}
	for name, seen := range homes {
		if !seen {
			t.Errorf("корпус не узнаёт дом цепочек %s — обход перестал его читать, и вторая "+
				"копия завелась бы там беззвучно (файлов в корпусе %d)", name, len(corpus))
		}
	}
}
