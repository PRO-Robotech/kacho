// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// neighbour_address_producer_test.go — АДРЕС СОСЕДА, КОТОРЫЙ ПРОЦЕСС НАБИРАЕТ,
// ОБЯЗАН ИМЕТЬ ПРОИЗВОДИТЕЛЯ В ОБЪЯВЛЕНИИ.
//
// ─────────────────────────────────────────────────────────────────────────────
// СВОЙСТВО, КОТОРОЕ ЭНФОРСИМ
//
// Живое значение адреса соседа приходит в под ИЗ ДВУХ МЕСТ: объявления (шаблон
// либо файл значений) — или ВКОМПИЛИРОВАННОГО УМОЛЧАНИЯ, если объявление
// молчит. Второе выглядит как настройка по умолчанию, а является адресом,
// которого НЕ НАЗЫВАЕТ НИ ОДИН ПРОФИЛЬ.
//
// Отсюда норма: переменная, чьё умолчание называет соседа в кластере и которую
// не выставляет НИ ОДИН чарт, — находка.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО ПРЕДПОСЫЛКА ПРЕДПОЛЁТНОЙ ПРОВЕРКИ, А НЕ ПРИДИРКА К СТИЛЮ
//
// Задача #145 просит проверку, которая перед объявлением стенда поднятым
// разрешает КАЖДЫЙ АДРЕС СОСЕДА ИЗ ПРОФИЛЯ. Оборот «из профиля» и есть слабое
// место: адрес, которого профиль не называет, такая проверка не увидит НИКОГДА
// — она переберёт то, что объявлено, отчитается полнотой и промолчит ровно про
// тот адрес, у которого нет ни одного читателя-профиля. То есть предполётная
// проверка без этой нормы неполна BY CONSTRUCTION, и неполна незаметно.
//
// Три следствия, каждое наблюдалось в этом дереве:
//
//  1. РЕЛИЗ ВКОМПИЛИРОВАН В БИНАРЬ. Умолчание вида
//     `<релиз>-kratos-public.<ns>.svc` держится совпадением имени релиза с тем,
//     что кто-то однажды набрал. Установка под другим именем релиза (или в
//     другом пространстве имён) ломает адрес МОЛЧА — и ни один профиль при
//     этом не меняется, потому что ни один профиль его и не называл.
//
//  2. ДВА МЕСТА ОБ ОДНОМ ПРЕДМЕТЕ. Имя Service живёт в шаблоне чарта, а
//     умолчание — в коде. Они не связаны ничем; расходятся молча. Прецедент
//     лежит рядом в комментарии `gateway/deploy/values.yaml` к ветке geo:
//     голый хост не резолвился, и КАЖДЫЙ запрос домена отвечал отказом.
//
//  3. ОТКАЗ ЧИТАЕТСЯ КАК ЧУЖОЙ. Нерезолвящееся имя приходит наверх полосой
//     недоступности — то есть выглядит как отказ СОСЕДА, а не как отказ ИМЕНИ,
//     и разбирается соответственно долго (см. тело #145).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЕ, А НЕ РЕНДЕР
//
// Проба читает ОБЪЯВЛЕНИЕ (исходники и файлы чартов), а не отрендеренный
// манифест: ей не нужны ни helm, ни материализованные зависимости, ни кластер —
// значит она НЕ МОЖЕТ ПРОПУСТИТЬСЯ там, где их нет. Соседняя проба формы адреса
// (`tests/helm/neighbour-address-form-test.sh`) закрывает вторую половину — то,
// что видно только в рендере. Дублем она не является: там предмет — ФОРМА
// написания уже объявленного адреса, здесь — НАЛИЧИЕ объявления вообще.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОБА НЕ ДОКАЗЫВАЕТ (названо, чтобы её зелёное не читалось шире)
//
// Она НЕ доказывает, что адрес РЕЗОЛВИТСЯ. «У адреса есть производитель» и
// «имя разрешается в поднятом кластере» — разные утверждения, и второе без
// кластера не проверяется ничем. Сверять хост с именем Service декларативно
// тоже нельзя: имена собираются шаблонами (`{{ include "x.fullname" . }}`,
// `{{ .Values.name }}`), и вычислять их здесь значило бы завести ВТОРУЮ
// реализацию разрешения имён чарта — второе место об одном предмете, которое
// разъедется молча. Авторитет по именам — рендер и кластер, а не эта проба.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// ─────────────────────────────────────────────────────────────────────────────
// КЛАССИФИКАТОРЫ — ЧИСТЫЕ ФУНКЦИИ.
//
// Вынесены затем, чтобы проба-инъекция (ниже) могла скормить им синтетический
// вход БЕЗ дерева и потребовать покраснеть на внесённом дефекте и промолчать на
// законном близнеце той же формы.
// ─────────────────────────────────────────────────────────────────────────────

// neighbourHostRe — хост кластера: имя, оканчивающееся на `.svc` либо
// `.svc.cluster.local`, с необязательной завершающей точкой. Форму написания
// (короткая/полная/абсолютная) здесь НЕ судим — это предмет соседней пробы.
var neighbourHostRe = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9.-]*\.svc(\.cluster\.local)?\.?`)

// listenAddrRe — собственный листенер (`:9090`), а не адрес соседа.
var listenAddrRe = regexp.MustCompile(`^:\d+$`)

// addressishRe — значение вообще похоже на адрес: несёт схему либо `хост:порт`.
var addressishRe = regexp.MustCompile(`://|:\d+(/|$)`)

// addrKind — к какому виду относится значение умолчания.
//
//	neighbour — сосед в кластере (`*.svc[.cluster.local]`)
//	listen    — собственный листенер (`:порт`)
//	external  — адрес за пределами кластера
//	plain     — не адрес вовсе (режим, имя файла, длительность)
func addrKind(value string) string {
	switch {
	case listenAddrRe.MatchString(value):
		return "listen"
	case neighbourHostRe.MatchString(value):
		return "neighbour"
	case addressishRe.MatchString(value):
		return "external"
	default:
		return "plain"
	}
}

// envProducerRe — СТРОГИЙ признак производителя: имя стоит объявлением
// переменной окружения (`- name: KACHO_…`), а не упоминанием в прозе.
//
// Слабый признак (просто вхождение имени в файл чарта) здесь НЕПРИГОДЕН и это
// померено: у трёх переменных дерева имя встречается ТОЛЬКО в комментарии
// файла значений. Гейт на слабом признаке засчитал бы комментарий за
// производителя — то есть был бы тем самым классом, который ловит.
func envProducerRe(env string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*-\s*name:\s*` + regexp.QuoteMeta(env) + `\s*$`)
}

// structTagRe / tagEnvRe / tagDefaultRe — объявление умолчания тегом структуры.
var (
	structTagRe  = regexp.MustCompile("`[^`]*`")
	tagEnvRe     = regexp.MustCompile(`envconfig:"([A-Z0-9_]+)"`)
	tagDefaultRe = regexp.MustCompile(`default:"([^"]*)"`)
)

// viperDefaultRe — второй механизм объявления умолчаний в этом дереве.
var viperDefaultRe = regexp.MustCompile(`SetDefault\(\s*"([^"]+)"\s*,\s*"([^"]*)"`)

// dialDefault — одно объявление умолчания.
type dialDefault struct {
	Key   string // имя переменной окружения либо ключ конфигурации
	Value string
	File  string
	Line  int
}

func (d dialDefault) at() string { return fmt.Sprintf("%s:%d", d.File, d.Line) }

// scanTagDefaults — умолчания, объявленные тегом структуры, в одном файле.
func scanTagDefaults(path, body string) []dialDefault {
	var out []dialDefault
	for i, line := range strings.Split(body, "\n") {
		for _, tag := range structTagRe.FindAllString(line, -1) {
			env := tagEnvRe.FindStringSubmatch(tag)
			def := tagDefaultRe.FindStringSubmatch(tag)
			if env == nil || def == nil {
				continue
			}
			out = append(out, dialDefault{Key: env[1], Value: def[1], File: path, Line: i + 1})
		}
	}
	return out
}

// scanViperDefaults — умолчания, объявленные вызовом, в одном файле.
func scanViperDefaults(path, body string) []dialDefault {
	var out []dialDefault
	for i, line := range strings.Split(body, "\n") {
		for _, m := range viperDefaultRe.FindAllStringSubmatch(line, -1) {
			out = append(out, dialDefault{Key: m[1], Value: m[2], File: path, Line: i + 1})
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ВТОРОЙ МЕХАНИЗМ — ОБЪЯВЛЕН ПОИМЁННО, И ЗАПИСЬ САМОИСТЕКАЕТ
//
// Умолчания в этом дереве объявляются ДВУМЯ способами, и производитель у них
// разной формы: у переменной окружения это строка `- name: …` в шаблоне, у
// ключа конфигурации — та же ветка, отрендеренная в ConfigMap. Второй проверять
// тем же предикатом нельзя, а НЕ СМОТРЕТЬ на него — значит завести гейт,
// слепой к целому виду предмета.
//
// Поэтому второй механизм переписан ПОИМЁННО: каждая запись называет
// производителя координатой. Запись без предмета в дереве — находка
// (самоистечение), сосед без записи — тоже находка.
var viperNeighbourProducers = map[string]string{
	"extapi.iam.endpoint": "services/vpc/deploy/templates/configmap.yaml — ветка extapi.iam.endpoint " +
		"рендерится всегда (умолчание шаблона поверх .Values.iamAddr)",
	"extapi.geo.endpoint": "services/vpc/deploy/templates/configmap.yaml — ветка extapi.geo.endpoint " +
		"рендерится всегда (умолчание шаблона поверх .Values.extapi.geo.endpoint)",
}

// externalDefaults — умолчания-адреса ЗА пределами кластера: у них нет и не
// должно быть Service, поэтому норма про производителя к ним не относится.
//
// Список нужен не ради послабления, а ради СЛЕПОЙ ЗОНЫ: без него значение, не
// попавшее ни в «сосед», ни в «листенер», тихо уходило бы в «прочее», и новый
// адрес соседа, записанный ОДНОСЕГМЕНТНЫМ именем (`iam:9090` — оно резолвится
// поисковым списком и `.svc` не содержит), не разбирался бы ВООБЩЕ. Запись без
// предмета в дереве — находка.
var externalDefaults = map[string]string{
	"api.kacho.local:443": "объявляемый ВНЕШНИЙ адрес края — то, что край сообщает о себе " +
		"клиенту снаружи; Service с таким именем нет и быть не должно",
	"https://api.kacho.local/iam/token": "внешний адрес выдачи docker-токена, который " +
		"край печатает клиенту в заголовке; резолвится снаружи, не в кластере",
}

// ─────────────────────────────────────────────────────────────────────────────
// ОБХОД ДЕРЕВА
// ─────────────────────────────────────────────────────────────────────────────

// trackedFiles — файлы, которые увидит CI на свежем checkout'е, а не мусор
// рабочего дерева. Пустой ответ — ОТКАЗ: «ноль находок» и «ноль прочитанного»
// обязаны быть различимы.
func trackedFiles(t *testing.T, patterns ...string) []string {
	t.Helper()
	args := append([]string{"-C", repoRoot, "ls-files", "-z", "--"}, patterns...)
	out, err := gitenv.Command("", args...).Output()
	if err != nil {
		t.Fatalf("git ls-files %v не отработал (%v) — обход не прочитал НИ ОДНОГО файла, "+
			"и «ноль находок» здесь ничего не значило бы", patterns, err)
	}
	var files []string
	for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// readTracked — содержимое отслеживаемого файла по пути от корня дерева.
// Нечитаемый файл — ОТКАЗ: пропустить его молча значило бы сузить обход и
// отчитаться полнотой.
func readTracked(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("не прочитан отслеживаемый файл %s (%v) — обход сузился бы молча", rel, err)
	}
	return string(b)
}

// isChartYAML — файл, в котором может стоять объявление переменной пода.
func isChartYAML(path string) bool {
	switch filepath.Ext(path) {
	case ".yaml", ".yml", ".tpl":
	default:
		return false
	}
	return strings.HasPrefix(path, "deploy/") || strings.Contains(path, "/deploy/")
}

// TestEveryDialledNeighbourAddressHasAProducerInTheDeclaration — гейт нормы.
func TestEveryDialledNeighbourAddressHasAProducerInTheDeclaration(t *testing.T) {
	// ── сторона 1: что процесс НАБЕРЁТ, если объявление промолчит ──────────
	goFiles := trackedFiles(t, "*.go")
	var tagDefs, viperDefs []dialDefault
	goRead := 0
	for _, f := range goFiles {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		body := readTracked(t, f)
		goRead++
		tagDefs = append(tagDefs, scanTagDefaults(f, body)...)
		viperDefs = append(viperDefs, scanViperDefaults(f, body)...)
	}
	if goRead == 0 {
		t.Fatalf("прочитано 0 не-тестовых .go — обходить было нечего; это «не выполнилось», а не «чисто»")
	}
	if len(tagDefs) == 0 {
		t.Fatalf("в %d не-тестовых .go не найдено НИ ОДНОГО умолчания тегом структуры — "+
			"предикат перестал узнавать форму объявления, и вердикт НЕ ВЫНЕСЕН", goRead)
	}

	// ── сторона 2: что объявлено чартами ──────────────────────────────────
	var chartFiles []string
	for _, f := range trackedFiles(t, "*.yaml", "*.yml", "*.tpl") {
		if isChartYAML(f) {
			chartFiles = append(chartFiles, f)
		}
	}
	if len(chartFiles) == 0 {
		t.Fatalf("не прочитано НИ ОДНОГО файла чарта — производителя искать негде; " +
			"это «не выполнилось», а не «чисто»")
	}
	chartBody := make(map[string]string, len(chartFiles))
	for _, f := range chartFiles {
		chartBody[f] = readTracked(t, f)
	}

	producerOf := func(env string) (string, bool) {
		re := envProducerRe(env)
		names := make([]string, 0, len(chartBody))
		for f := range chartBody {
			names = append(names, f)
		}
		sort.Strings(names)
		for _, f := range names {
			if re.MatchString(chartBody[f]) {
				return f, true
			}
		}
		return "", false
	}

	// ── разбор ────────────────────────────────────────────────────────────
	var neighbours, externals []dialDefault
	kinds := map[string]int{}
	for _, d := range tagDefs {
		k := addrKind(d.Value)
		kinds[k]++
		switch k {
		case "neighbour":
			neighbours = append(neighbours, d)
		case "external":
			externals = append(externals, d)
		}
	}

	t.Logf("объём: не-тестовых .go %d, умолчаний тегом %d (сосед %d, листенер %d, внешних %d, не-адрес %d), "+
		"файлов чартов %d, умолчаний вызовом %d",
		goRead, len(tagDefs), kinds["neighbour"], kinds["listen"], kinds["external"], kinds["plain"],
		len(chartFiles), len(viperDefs))

	// (A) сосед без производителя — находка.
	missing := 0
	for _, d := range neighbours {
		if at, ok := producerOf(d.Key); ok {
			t.Logf("  ✓ %s — производитель %s", d.Key, at)
			continue
		}
		missing++
		t.Errorf("АДРЕС СОСЕДА БЕЗ ПРОИЗВОДИТЕЛЯ В ОБЪЯВЛЕНИИ\n"+
			"    переменная: %s\n"+
			"    умолчание : %q\n"+
			"    объявлено : %s\n"+
			"    ни один файл чарта не несёт строки `- name: %s`, поэтому живым значением\n"+
			"    работает вкомпилированное умолчание. Его не называет НИ ОДИН профиль:\n"+
			"    предполётная проверка «по профилю» не увидит его никогда, а установка\n"+
			"    под другим именем релиза или в другом пространстве имён сломает адрес молча.\n"+
			"    Исход: объявить переменную в чарте сервиса-потребителя (как объявлены соседние).",
			d.Key, d.Value, d.at(), d.Key)
	}

	// (B) внешний адрес обязан быть назван — иначе «прочее» становится слепой зоной.
	for _, d := range externals {
		if _, ok := externalDefaults[d.Value]; ok {
			continue
		}
		t.Errorf("УМОЛЧАНИЕ-АДРЕС, НЕ ОТНЕСЁННОЕ НИ К СОСЕДУ, НИ К ВНЕШНЕМУ\n"+
			"    переменная: %s\n"+
			"    умолчание : %q\n"+
			"    объявлено : %s\n"+
			"    Если это сосед, записанный без `.svc` (односегментное имя резолвится\n"+
			"    поисковым списком) — норма выше к нему ОТНОСИТСЯ, и разбор её обошёл.\n"+
			"    Если это адрес снаружи — внесите его в externalDefaults с причиной.",
			d.Key, d.Value, d.at())
	}

	// (C) второй механизм: сосед обязан быть назван поимённо.
	for _, d := range viperDefs {
		if addrKind(d.Value) != "neighbour" {
			continue
		}
		if _, ok := viperNeighbourProducers[d.Key]; ok {
			continue
		}
		t.Errorf("СОСЕД, ОБЪЯВЛЕННЫЙ ВТОРЫМ МЕХАНИЗМОМ, НЕ НАЗВАН ПОИМЁННО\n"+
			"    ключ      : %s\n"+
			"    умолчание : %q\n"+
			"    объявлено : %s\n"+
			"    Производитель у ключа конфигурации другой формы, чем у переменной\n"+
			"    окружения, и общим предикатом не проверяется. Назовите производителя\n"+
			"    координатой в viperNeighbourProducers — либо снимите умолчание.",
			d.Key, d.Value, d.at())
	}

	// (D) исключение живёт, пока у него есть ПРЕДМЕТ.
	live := map[string]bool{}
	for _, d := range viperDefs {
		live[d.Key] = true
	}
	for key := range viperNeighbourProducers {
		if !live[key] {
			t.Errorf("ЗАПИСИ ВТОРОГО МЕХАНИЗМА НЕЧЕГО ИМЕНОВАТЬ: ключа %q в дереве больше нет.\n"+
				"    Исключение без предмета унаследует следующую слепую зону — снять его.", key)
		}
	}
	liveVal := map[string]bool{}
	for _, d := range tagDefs {
		liveVal[d.Value] = true
	}
	for val := range externalDefaults {
		if !liveVal[val] {
			t.Errorf("ЗАПИСИ ВНЕШНИХ АДРЕСОВ НЕЧЕГО ИСКЛЮЧАТЬ: умолчания %q в дереве больше нет.\n"+
				"    Исключение без предмета унаследует следующую слепую зону — снять его.", val)
		}
	}

	if kinds["neighbour"] == 0 {
		// Достижение цели, а не поломка: проба объявляет перепись и проходит.
		t.Logf("умолчаний, называющих соседа, в дереве нет — норма выполняется by construction")
	}
	t.Logf("вердикт: соседей %d, из них без производителя %d", len(neighbours), missing)
}

// TestNeighbourProducerClassifiersCanRedden — ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ.
//
// Гейт выше читает дерево, и на здоровом дереве он зелёный — то есть сам по
// себе он НЕ доказывает, что способен покраснеть. Здесь классификаторы
// получают синтетический вход и обязаны: покраснеть на внесённом дефекте И
// промолчать на ЗАКОННОМ БЛИЗНЕЦЕ ТОЙ ЖЕ ФОРМЫ.
func TestNeighbourProducerClassifiersCanRedden(t *testing.T) {
	t.Run("вид значения", func(t *testing.T) {
		for _, c := range []struct {
			value string
			want  string
		}{
			// дефект: сосед короткой и полной формой, в голом виде и в URL
			{"kaname.kacho.svc:9090", "neighbour"},
			{"kaname.kacho.svc.cluster.local:9090", "neighbour"},
			{"http://kacho-umbrella-kratos-public.kacho.svc:80", "neighbour"},
			{"https://kaname-internal.kacho.svc:9097/.well-known/jwks.json", "neighbour"},
			// законные близнецы той же формы: их норма НЕ касается
			{":9090", "listen"},
			{":8080", "listen"},
			{"api.kacho.local:443", "external"},
			{"https://api.kacho.local/iam/token", "external"},
			{"production", "plain"},
			{"require", "plain"},
			{"30s", "plain"},
			{"", "plain"},
		} {
			if got := addrKind(c.value); got != c.want {
				t.Errorf("addrKind(%q) = %q, ожидалось %q", c.value, got, c.want)
			}
		}
	})

	t.Run("производитель — объявление, а не проза", func(t *testing.T) {
		const env = "KACHO_API_GATEWAY_STORAGE_GRPC"
		re := envProducerRe(env)

		// (а) НАСТОЯЩЕЕ объявление — обязано считаться производителем.
		real := "        env:\n" +
			"            - name: " + env + "\n" +
			"              value: \"kacho-storage.kacho.svc:9090\"\n"
		if !re.MatchString(real) {
			t.Errorf("объявление переменной НЕ признано производителем — гейт краснел бы на исправном дереве")
		}

		// (б) УПОМИНАНИЕ В ПРОЗЕ — производителем считаться НЕ должно.
		// Это и есть тот вход, на котором слабый предикат («имя встречается в
		// файле») засчитал бы комментарий за объявление и промолчал бы на
		// живом дефекте.
		prose := "# адрес хранилища приезжает переменной " + env + ", см. соседний блок\n" +
			"# (" + env + ") — историческая справка\n"
		if re.MatchString(prose) {
			t.Errorf("комментарий засчитан за производителя — гейт молчал бы на живом дефекте")
		}

		// (в) ЧУЖОЕ имя с тем же префиксом — не производитель этой переменной.
		other := "            - name: " + env + "_EXTRA\n"
		if re.MatchString(other) {
			t.Errorf("переменная с другим именем засчитана за производителя")
		}
	})

	t.Run("разбор объявлений", func(t *testing.T) {
		body := "type Config struct {\n" +
			"\tA string `envconfig:\"KACHO_X_IAM_GRPC\" default:\"kaname.kacho.svc:9090\"`\n" +
			"\tB string `envconfig:\"KACHO_X_LISTEN\"   default:\":9090\"`\n" +
			"\tC string `json:\"c\"`\n" +
			"}\n"
		got := scanTagDefaults("synthetic.go", body)
		if len(got) != 2 {
			t.Fatalf("разобрано %d объявлений, ожидалось 2: %+v", len(got), got)
		}
		if got[0].Key != "KACHO_X_IAM_GRPC" || got[0].Value != "kaname.kacho.svc:9090" || got[0].Line != 2 {
			t.Errorf("первое объявление разобрано неверно: %+v", got[0])
		}
		if got[1].Line != 3 {
			t.Errorf("номер строки второго объявления неверен: %+v", got[1])
		}

		vp := scanViperDefaults("synthetic.go", "\tv.SetDefault(\"extapi.iam.endpoint\", \"iam.kacho.svc:9090\")\n")
		if len(vp) != 1 || vp[0].Key != "extapi.iam.endpoint" {
			t.Fatalf("умолчание вызовом разобрано неверно: %+v", vp)
		}
	})
}
