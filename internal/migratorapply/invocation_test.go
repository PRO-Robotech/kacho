// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migratorapply_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/corelib/treecorpus"
	"github.com/PRO-Robotech/kacho/internal/migrationchains"
	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// migratorBinDir — каталог, в который образ кладёт накатчик. Общий у всех
// продуктов; различается ИМЯ файла, и его называет владелец имён.
const migratorBinDir = "/usr/local/bin/"

// migratorBinaryPathOf — путь накатчика службы, ВЫВЕДЕННЫЙ у владельца имён.
//
// Здесь стоял литерал `"/usr/local/bin/kacho-migrator"` — второе место об одном
// предмете. После #2245 имя накатчика одно на ПРОДУКТ, и продуктов два:
// `kacho-migrator` у шести служб платформы, `kaname-migrator` у Kaname. Литерал
// был верен ровно для шести, а седьмую форму НЕ ВИДЕЛ — и это молчание, а не
// находка (kacho#2183).
func migratorBinaryPathOf(serviceDir string) string {
	return migratorBinDir + productnaming.MigratorBinary(serviceDir)
}

// migratorBinaryPaths — все пути накатчиков дерева. Выводятся из перечня
// продуктов, а не выписываются: выписанный разошёлся бы с ним на следующем
// вынесенном продукте — молча, как разошёлся литерал.
func migratorBinaryPaths() []string {
	names := productnaming.ProductNames()
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, migratorBinDir+n+"-migrator")
	}
	return out
}

// isMigratorPath — принадлежит ли argv[0] какому-нибудь накатчику дерева.
func isMigratorPath(p string) bool {
	for _, known := range migratorBinaryPaths() {
		if p == known {
			return true
		}
	}
	return false
}

// mentionsAnyMigrator — упоминает ли манифест накатчик хоть одного продукта.
func mentionsAnyMigrator(body string) bool {
	for _, known := range migratorBinaryPaths() {
		if strings.Contains(body, known) {
			return true
		}
	}
	return false
}

// manifestRoots — каталоги, под которыми ищется объявление формы вызова.
//
// Корней ДВА, и это не запас. Задача #1650 называет обход `services/*/deploy`, но
// под ним лежат манифесты ШЕСТИ точек наката из семи: у geo в `services/geo/deploy`
// только `Chart.yaml` и `values.yaml`, а его развёртывание объявлено чартом зонта.
// Обход одного корня оставил бы седьмую форму вне наблюдения — ровно тем молчанием,
// против которого предикат задачи и написан.
var manifestRoots = []string{"services", "deploy"}

// invocationForm — форма вызова, ВЫВЕДЕННАЯ из манифеста: аргументы после пути
// бинаря плюс файл, из которого форма прочитана.
//
// Форма — не argv целиком: путь бинаря у всех один и различать формы не может.
type invocationForm struct {
	service string
	argv    []string
	origin  string
}

func (f invocationForm) String() string { return strings.Join(f.argv, " ") }

// commandLine / argsLine — объявление формы в манифесте. Все точки наката пишут его
// потоковым стилем в одну строку; форму, записанную блочным стилем, разбор не
// прочитает — и потому НЕ МОЛЧИТ о ней: строка, называющая бинарь, но не давшая
// ни одного аргумента, роняет перепись (см. manifestForms).
var (
	commandLine = regexp.MustCompile(`^\s*command:\s*\[(.+)\]\s*$`)
	argsLine    = regexp.MustCompile(`^\s*args:\s*\[(.+)\]\s*$`)
)

// serviceForForm — чью форму вызова объявляет строка line файла rel.
//
// Ответ берётся у владельца имён частей, [productnaming.PartOfLine], а не
// выводится здесь второй копией: своя копия уже расходилась с ним дважды
// (kacho#2260, kacho#2915).
//
// Две раскладки пути вне подчартов зонта (Д77 (а)) — чарт поставки
// `deploy/helm/<чарт>/…` и шаблон зонта `deploy/helm/umbrella/templates/<файл>`
// (служба — первый сегмент имени файла до `-` или `.`) — дают службу, ТОЛЬКО
// если каталог `services/<служба>` есть в индексе git tree. Иначе путь службы
// не даёт: чарт без службы в дереве — не служба, и приписать ему форму значило
// бы покрыть точку наката, которой нет. Тогда действует прежний порядок —
// ключ подчарта, затем отказ «ЧЬЮ — не выводится».
func serviceForForm(tree *treecorpus.Tree, rel string, lines []string, at int) string {
	rel = filepath.ToSlash(rel)
	if svc, ok := umbrellaTemplateService(rel); ok {
		if tree.HasDir("services/" + svc) {
			return svc
		}
		return keyedService(lines, at)
	}
	svc, _ := productnaming.PartOfLine(rel, lines, at)
	if svc != "" && isDeliveryChart(rel) && !tree.HasDir("services/"+svc) {
		return keyedService(lines, at)
	}
	return svc
}

// umbrellaTemplatesPrefix — шаблоны самого зонта (не подчартов).
const umbrellaTemplatesPrefix = "deploy/helm/umbrella/templates/"

// umbrellaTemplateService — служба шаблона зонта по имени файла.
func umbrellaTemplateService(rel string) (string, bool) {
	name, ok := strings.CutPrefix(rel, umbrellaTemplatesPrefix)
	if !ok || strings.Contains(name, "/") {
		return "", false
	}
	if i := strings.IndexAny(name, "-."); i > 0 {
		return name[:i], true
	}
	return "", false
}

// isDeliveryChart — путь лежит в чарте поставки вне зонта `deploy/helm/<чарт>/`.
func isDeliveryChart(rel string) bool {
	s, ok := strings.CutPrefix(rel, "deploy/helm/")
	if !ok {
		return false
	}
	i := strings.IndexByte(s, '/')
	return i > 0 && s[:i] != "umbrella" && s[:i] != "vendor"
}

// keyedService — прежний порядок без вывода из пути: служба по ключу подчарта
// над строкой. Путь подаётся нейтральный, чтобы владелец имён судил только
// ключ, а не раскладку пути, уже отвергнутую выше.
func keyedService(lines []string, at int) string {
	svc, _ := productnaming.PartOfLine("", lines, at)
	return svc
}

// parseFlowSequence разбирает потоковую последовательность YAML из строк в
// кавычках: `"a", "b"` → []string{"a","b"}.
//
// Элемент, который не разобрался, — ОТКАЗ разбора всей последовательности, а не
// пропуск: форма вызова, прочитанная наполовину, хуже непрочитанной — она
// выглядит покрытой.
func parseFlowSequence(body string) ([]string, bool) {
	var out []string
	for _, raw := range strings.Split(body, ",") {
		item := strings.TrimSpace(raw)
		if item == "" {
			return nil, false
		}
		unquoted, err := strconv.Unquote(item)
		if err != nil {
			return nil, false
		}
		out = append(out, unquoted)
	}
	return out, len(out) > 0
}

// manifestForms — формы вызова, ВЫВЕДЕННЫЕ из манифестов развёртывания.
//
// Перечень не выписывается ни в каком виде: он собирается обходом индекса git под
// manifestRoots. Новая форма — и новый сервис вместе с ней — попадает под
// наблюдение сама; выписанный перечень разошёлся бы с деревом молча, и разошёлся
// бы именно на новом сервисе, где слепая зона дороже всего.
//
// Возвращает формы по сервисам и число прочитанных файлов: «ноль форм» обязано
// быть отличимо от «ноль прочитанного».
func manifestForms(t *testing.T, root string) (map[string][]invocationForm, int) {
	t.Helper()

	forms := make(map[string][]invocationForm)
	filesRead := 0
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("индекс git не прочитан (%v) — чья форма вызова, судится по каталогу службы в "+
			"индексе, и без него вывод службы из пути был бы догадкой", err)
	}

	for _, sub := range manifestRoots {
		files, err := treecorpus.UnderWithSuffix(filepath.Join(root, sub), ".yaml", ".yml", ".tpl")
		if err != nil {
			t.Fatalf("состав манифестов под %s НЕ ИЗМЕРЕН (%v) — перечень форм вызова "+
				"взять неоткуда, а пустой перечень здесь означал бы зелёную пробу "+
				"с нулём покрытого", sub, err)
		}
		for _, abs := range files {
			body, err := os.ReadFile(abs)
			if err != nil {
				t.Fatalf("манифест %s не прочитан: %v", abs, err)
			}
			filesRead++
			if !mentionsAnyMigrator(string(body)) {
				continue
			}
			rel, relErr := filepath.Rel(root, abs)
			if relErr != nil {
				t.Fatalf("относительный путь для %s: %v", abs, relErr)
			}
			for _, form := range formsInManifest(t, tree, rel, string(body)) {
				if form.service == "" {
					t.Fatalf("%s объявляет форму вызова накатчика (%v), но ЧЬЮ — не выводится "+
						"ни из пути, ни из ключа подчарта. Приписать форму соседу хуже, чем "+
						"остановиться: покрытой оказалась бы не та точка наката",
						form.origin, migratorBinaryPaths())
				}
				forms[form.service] = append(forms[form.service], form)
			}
		}
	}
	return forms, filesRead
}

// formsInManifest вытаскивает формы вызова из одного манифеста.
//
// Читается пара «command + следующий args»: nlb объявляет путь бинаря в command, а
// аргументы — отдельным args, и разбор, знающий только command, потерял бы ровно ту
// форму, ради которой заведена задача #1650.
func formsInManifest(t *testing.T, tree *treecorpus.Tree, rel, body string) []invocationForm {
	t.Helper()

	var out []invocationForm
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		m := commandLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		argv, ok := parseFlowSequence(m[1])
		if !ok || !isMigratorPath(argv[0]) {
			if !ok && mentionsAnyMigrator(line) {
				t.Fatalf("%s:%d объявляет форму вызова накатчика (%v), но разбор её НЕ ПРОЧИТАЛ. "+
					"Молчаливый пропуск оставил бы форму вне наблюдения — то есть дал бы "+
					"ровно ту слепую зону, ради которой эта проба написана:\n%s",
					rel, i+1, migratorBinaryPaths(), line)
			}
			continue
		}
		argv = argv[1:]
		// Аргументы отдельным ключом идут строкой ниже — так пишет nlb.
		if i+1 < len(lines) {
			if am := argsLine.FindStringSubmatch(lines[i+1]); am != nil {
				extra, extraOK := parseFlowSequence(am[1])
				if !extraOK {
					t.Fatalf("%s:%d — аргументы формы вызова не разобраны:\n%s", rel, i+2, lines[i+1])
				}
				argv = append(argv, extra...)
			}
		}
		if len(argv) == 0 {
			t.Fatalf("%s:%d зовёт накатчик БЕЗ единого аргумента — пустая командная строка "+
				"мигратора есть отказ, а не накат", rel, i+1)
		}
		out = append(out, invocationForm{
			service: serviceForForm(tree, rel, lines, i),
			argv:    argv,
			origin:  fmt.Sprintf("%s:%d", rel, i+1),
		})
	}
	return out
}

// TestEveryApplyPointHasItsInvocationFormDerived — перепись форм вызова.
//
// Утверждает ровно одно: у КАЖДОЙ точки наката дерева форма вызова выведена из
// манифеста. Живой базы не требует и потому исполняется в кратком прогоне — новая
// точка наката без объявленной формы находится дёшево, а не через двадцать минут
// конвейера.
//
// Без этой половины доказательство наката ниже было бы зелено на любом ЧИСЛЕ
// покрытых форм: оно гоняет то, что ему дали, и молчит о том, чего не дали.
func TestEveryApplyPointHasItsInvocationFormDerived(t *testing.T) {
	root := repoRoot(t)
	points := applyPoints(t, root)
	if len(points) == 0 {
		t.Fatal("точек наката НЕ НАЙДЕНО — обход пуст, перепись беспредметна")
	}

	forms, filesRead := manifestForms(t, root)
	if filesRead == 0 {
		t.Fatal("манифестов НЕ ПРОЧИТАНО ни одного — «ноль форм» здесь неотличимо " +
			"от «ноль прочитанного»")
	}

	covered := 0
	for _, pkg := range points {
		service := pointService(pkg)
		got := forms[service]
		if len(got) == 0 {
			t.Errorf("у точки наката %s НЕТ ни одной формы вызова в манифестах под %v. "+
				"Форма, которую никто не объявил, не может быть доказана: накат этого "+
				"сервиса остаётся вне наблюдения молча",
				service, manifestRoots)
			continue
		}
		covered++
		for _, f := range got {
			t.Logf("  %-9s %-46s ← %s", service, f, f.origin)
		}
	}

	// ОБРАТНАЯ сторона, и без неё перепись сходилась бы сама с собой. Прежде цикл
	// шёл только по точкам наката: форма, у которой точки нет, не доказывалась и
	// НЕ РОНЯЛА перепись. Ровно так седьмая точка выпала из наблюдения молча —
	// её форма в манифестах была, а точки в перечне не было (kacho#2183).
	inPoints := make(map[string]bool, len(points))
	for _, pkg := range points {
		inPoints[pointService(pkg)] = true
	}
	// ТРЕТИЙ ИСХОД, названный после разреза монорепо: форма объявлена нашим
	// манифестом для части, чьи ИСХОДНИКИ живут в другом репозитории. Точки
	// наката у неё здесь нет и быть не может — её накатчик собирается своим
	// деревом, — а форма вызова остаётся НАШИМ обязательством: её печатает наш
	// шаблон, и разойтись ей есть с чем.
	//
	// Такая часть выносится в отдельное число переписи, а не в находку: слитая
	// с «форм без точки наката», она читалась бы как слепота обхода — то есть
	// как ровно тот дефект, ради которого обратная сторона и заведена.
	orphan := make([]string, 0)
	elsewhere := make([]string, 0, 1)
	for service := range forms {
		if inPoints[service] {
			continue
		}
		if !productnaming.SourcesInThisTree(service) {
			elsewhere = append(elsewhere, service)
			continue
		}
		orphan = append(orphan, service)
	}
	sort.Strings(orphan)
	sort.Strings(elsewhere)
	for _, service := range orphan {
		t.Errorf("манифесты объявляют форму вызова накатчика службы %q (%v), а точки "+
			"наката у неё в дереве НЕТ. Исходов два: точка есть и обход её не видит "+
			"(тогда слеп обход — так выпала седьмая, kacho#2183), либо формы объявлены "+
			"на несуществующий накат. Молчание здесь неотличимо от полноты",
			service, forms[service])
	}

	if len(elsewhere) > 0 {
		t.Logf("форма вызова объявлена для части с исходниками в другом репозитории: %v — "+
			"её точка наката судится её деревом, здесь второй стороны нет", elsewhere)
	}
	t.Logf("перепись: манифестов прочитано %d, точек наката в дереве %d, форма выведена "+
		"для %d, форм без точки наката %d, форм у частей с исходниками вне дерева %d",
		filesRead, len(points), covered, len(orphan), len(elsewhere))
}

// dsnParts — разобранный DSN пробы. Нужен той полосе доставки конфигурации, где
// сервис читает БД по частям, а не строкой.
type dsnParts struct {
	user, password, host, port, name string
}

// splitDSN читает части разбором драйвера (pgconn.ParseConfig, Д93): части те,
// с которыми драйвер соединился бы по этой строке, — в любой её записи и без
// процентного кодирования в учётных данных.
func splitDSN(t *testing.T, dsn string) dsnParts {
	t.Helper()
	c, err := pgconn.ParseConfig(dsn)
	if err != nil {
		t.Fatal("DSN пробы не разобран драйвером (текст разбора несёт куски строки)")
	}
	if c.Database == "" {
		t.Fatal("в DSN пробы нет имени базы")
	}
	return dsnParts{user: c.User, password: c.Password, host: c.Host,
		port: strconv.Itoa(int(c.Port)), name: c.Database}
}

// serviceEnvPrefix — приставка переменных окружения службы, спрошенная у
// ОБЪЯВЛЕННОГО ВЛАДЕЛЬЦА имён.
//
// Здесь стояло `"KACHO_" + strings.ToUpper(service)`. Литерал был верен для
// шести служб платформы и НЕВЕРЕН для седьмой: часть, получившая собственное имя,
// объявляет окружение с приставкой своего продукта (`iam` → `KANAME`), и владелец
// имён это прямо говорит. Второй словарь об одном предмете разошёлся с первым — и
// разошёлся МОЛЧА, ровно как обещает godoc самого владельца.
//
// Цена расхождения измерена: `configPathEnvOf` строился из этой приставки, поэтому
// полоса доставки конфигурации у седьмой службы не опознавалась вовсе. Проба
// уходила в полосу окружения, ставила `KACHO_IAM_DB_*`, которых накатчик не
// читает, и он поднимался на УМОЛЧАНИЯХ — то есть шёл в `127.0.0.1:5432` вместо
// базы пробы. Отказ выглядел как недоступность базы, а был расхождением словарей.
func serviceEnvPrefix(service string) string {
	return productnaming.EnvPrefix(service)
}

// configPathEnvOf — имя переменной, которой манифест называет путь конфигурации.
func configPathEnvOf(service string) string { return serviceEnvPrefix(service) + "_CONFIG_PATH" }

// serviceConfigYAML — минимальная конфигурация сервиса, дающая накату DSN.
//
// Ключ один на все три службы файловой полосы (`repository.postgres.url`) — это
// не совпадение: DSN у vpc, iam и nlb приходит из него, и своей редакции у каждой
// быть не должно.
//
// `mode: dev` и открытый круг отправителей стоят здесь потому, что конфигурацию
// службы валидирует её собственная стража старта: боевой стенд монтирует накату
// ТУ ЖЕ конфигурацию, что и самой службе, и та проходит боевую посадку целиком.
// Предмет этой пробы — форма вызова и источник DSN, а не посадка; воспроизводить
// здесь боевой профиль значило бы проверять чужое свойство и краснеть от него.
func serviceConfigYAML(dsn string) string {
	return "mode: dev\n" +
		"authz:\n  trust-any-forwarder: true\n" +
		"repository:\n  postgres:\n    url: " + dsn + "\n"
}

// invocationEnv — окружение и argv, которыми форма запускается против живой базы.
//
// DSN доставляется ТОЙ полосой, которую называет сам манифест, и полоса выводится
// из него, а не из перечня сервисов:
//
//   - манифест формы называет `KACHO_MIGRATOR_DSN` → DSN этой переменной (Д77 (в):
//     точка, не читающая конфигурации процесса, — notify — получает DSN только ею);
//   - форма несёт `--config <путь>` → конфигурация файлом, путь подменяется;
//   - манифест называет `KACHO_<SVC>_CONFIG_PATH` → конфигурация файлом по этому пути;
//   - иначе → конфигурация окружением `KACHO_<SVC>_DB_*`.
//
// Переменная `KACHO_MIGRATOR_DSN` гасится у всех служб, чей манифест её НЕ
// называет: иначе полоса конфигурации зеленела бы на DSN из окружения прогона.
//
// Флаг `--dsn` не подставляется НИКОГДА, и это предмет: первый источник приоритета
// (`--dsn` > `KACHO_MIGRATOR_DSN` > конфигурация) доказан пробой наката рядом, а
// последний — тот, которым пользуются все развёртывания дерева, — не был доказан
// ничем. Именно его и гоняет эта полоса.
func invocationEnv(t *testing.T, service string, form invocationForm, dsn, dir string, lane dsnLane) ([]string, []string) {
	t.Helper()

	argv := append([]string(nil), form.argv...)
	if lane == laneMigratorDSN {
		return argv, []string{migratorDSNEnv + "=" + dsn}
	}
	// Переменная общего приоритета гасится: не погасив её, полоса конфигурации
	// зеленела бы на DSN из окружения прогона — то есть доказывала бы не себя.
	env := []string{migratorDSNEnv + "="}

	writeConfig := func(path string) {
		if err := os.WriteFile(path, []byte(serviceConfigYAML(dsn)), 0o600); err != nil {
			t.Fatalf("конфигурация %s не записана: %v", service, err)
		}
	}

	for i := 0; i < len(argv)-1; i++ {
		if argv[i] != "--config" {
			continue
		}
		path := filepath.Join(dir, service+".yaml")
		writeConfig(path)
		argv[i+1] = path
		return argv, env
	}

	if manifestNamesConfigPath(t, service) {
		path := filepath.Join(dir, service+".yaml")
		writeConfig(path)
		return argv, append(env, configPathEnvOf(service)+"="+path)
	}

	p := splitDSN(t, dsn)
	pfx := serviceEnvPrefix(service)
	return argv, append(env,
		pfx+"_DB_HOST="+p.host,
		pfx+"_DB_PORT="+p.port,
		pfx+"_DB_USER="+p.user,
		pfx+"_DB_PASSWORD="+p.password,
		pfx+"_DB_NAME="+p.name,
		pfx+"_DB_SSLMODE=disable",
	)
}

// migratorDSNEnv — переменная DSN второго приоритета общего тракта наката.
const migratorDSNEnv = "KACHO_MIGRATOR_DSN"

// dsnLane — полоса доставки DSN форме вызова.
type dsnLane int

const (
	// laneServiceConfig — конфигурация службы (файл или окружение KACHO_<SVC>_*).
	laneServiceConfig dsnLane = iota
	// laneMigratorDSN — переменная KACHO_MIGRATOR_DSN, названная манифестом формы.
	laneMigratorDSN
)

func (l dsnLane) String() string {
	if l == laneMigratorDSN {
		return migratorDSNEnv
	}
	return "конфигурация службы"
}

// laneOf — полоса, которую называет манифест: файл, из которого форма
// выведена, называет KACHO_MIGRATOR_DSN — полоса этой переменной; иначе —
// конфигурация службы. Признак берётся у манифеста формы, а не у перечня служб.
func laneOf(t *testing.T, root string, form invocationForm) dsnLane {
	t.Helper()
	file, _, _ := strings.Cut(form.origin, ":")
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		t.Fatalf("манифест формы %s не прочитан: %v", form.origin, err)
	}
	if strings.Contains(string(body), migratorDSNEnv) {
		return laneMigratorDSN
	}
	return laneServiceConfig
}

// manifestNamesConfigPath — называет ли манифест сервиса путь конфигурации.
// Признак берётся у дерева, а не у перечня служб: новый сервис получает ту полосу
// доставки, которую объявил сам.
func manifestNamesConfigPath(t *testing.T, service string) bool {
	t.Helper()
	root := repoRoot(t)
	needle := configPathEnvOf(service)
	for _, sub := range manifestRoots {
		files, err := treecorpus.UnderWithSuffix(filepath.Join(root, sub), ".yaml", ".yml", ".tpl")
		if err != nil {
			t.Fatalf("состав манифестов под %s не измерен: %v", sub, err)
		}
		for _, abs := range files {
			body, err := os.ReadFile(abs)
			if err != nil {
				t.Fatalf("манифест %s не прочитан: %v", abs, err)
			}
			if strings.Contains(string(body), needle) {
				return true
			}
		}
	}
	return false
}

// TestEveryMigratorAppliesItsChainInItsManifestForm — доказательство наката В ТОЙ
// ФОРМЕ, КОТОРОЙ НАКАТ ЗОВУТ.
//
// Проба наката рядом (apply_test.go) гоняет `up --dsn <DSN>` — форму, которой в
// дереве не зовёт НИ ОДИН манифест и НИ ОДИН Makefile. Все развёртывания дерева
// берут DSN из конфигурации, то есть из ПОСЛЕДНЕГО источника приоритета, и эта
// ветка не была доказана ничем. Здесь она и доказывается — каждым сервисом в его
// собственной форме, выведенной из его манифеста.
//
// Различие не косметическое: у nlb флаг стоит ПЕРЕД подкомандой
// (`--config <путь> up`), у шести соседей аргумент один (`up`), а DSN приходит
// разными полосами конфигурации. Разбор аргументов уже терял флаг, написанный не
// на своём месте, и накат уезжал не на ту базу, оставаясь на вид успешным.
func TestEveryMigratorAppliesItsChainInItsManifestForm(t *testing.T) {
	if testing.Short() {
		t.Skip("накат идёт против живой базы (testcontainers): под кратким режимом пропускается, " +
			"гоняет цель test-pg-outside-selection")
	}
	root := repoRoot(t)
	points := applyPoints(t, root)
	if len(points) == 0 {
		t.Fatal("точек наката НЕ НАЙДЕНО — обход пуст, доказывать нечего")
	}
	forms, filesRead := manifestForms(t, root)
	if filesRead == 0 {
		t.Fatal("манифестов НЕ ПРОЧИТАНО ни одного — доказывать нечего")
	}

	chainsOf := map[string][]migrationchains.Chain{}
	for _, c := range treeChains(t, root) {
		chainsOf[c.Point] = append(chainsOf[c.Point], c)
	}

	binDir := t.TempDir()
	cfgDir := t.TempDir()
	proven, failed, outOfPoll := 0, 0, 0
	perService := map[string]int{}
	// pointsOutOfPoll — точки, у которых ВСЕ формы вне опроса (носитель не
	// отрендерен профилем, Д91): в знаменатель они не входят.
	pointsOutOfPoll := 0

	for _, pkg := range points {
		service := pointService(pkg)
		serviceForms := uniqueForms(forms[service])
		if len(serviceForms) == 0 {
			// Перепись выше уже назвала это находкой; здесь молчать нельзя тем
			// более — иначе «доказано 6 из 7» выглядело бы полным прогоном.
			t.Errorf("у %s нет выведенной формы вызова — накат в манифестной форме "+
				"НЕ ДОКАЗАН", service)
			failed++
			continue
		}
		chains := chainsOf[pkg]
		if len(chains) == 0 {
			t.Errorf("у точки %s нет ни одной цепочки в перечне дерева — накат в манифестной "+
				"форме доказывать не на чем", pkg)
			failed++
			continue
		}

		bin := buildApplyPoint(t, root, binDir, pkg, service)
		pointPolled := false

		// Каждая выведенная форма — на строке ЕЁ базы (Д84): у точки, выбирающей
		// цепочку по имени базы, форма доказывается только на строке, чей
		// Database равен dbname манифеста формы; у точки без таблицы — на её
		// строке, как прежде (Д77 (б)).
		for _, form := range serviceForms {
			lane := laneOf(t, root, form)
			pairing := formRows(root, form, chains)
			for _, f := range pairing.findings {
				t.Errorf("служба %s, форма `%s` (%s): %s", service, form, form.origin, f)
				failed++
			}
			if pairing.outOfPoll != "" {
				outOfPoll++
				t.Logf("  %s · форма `%s` · источник %s", pairing.outOfPoll, form, form.origin)
			} else {
				pointPolled = true
			}
			for _, c := range pairing.rows {

				name := chainLabel(c) + "/" + strings.ReplaceAll(form.String(), " ", "_")
				ok := t.Run(name, func(t *testing.T) {
					want := chainLength(t, root, c.Dir)
					if want == 0 {
						t.Fatalf("в %s НЕТ ни одного файла миграции — эталона для сверки "+
							"не существует", c.Dir)
					}
					dsn := chainDSN(t, c)
					argv, env := invocationEnv(t, service, form, dsn, cfgDir, lane)

					out, err := runMigratorEnv(t, bin, env, argv...)
					if err != nil {
						t.Fatalf("накат %s в манифестной форме `%s` (%s, полоса DSN %s, база %s) "+
							"ОТКАЗАЛ (%v) — это и означает «сервис не разворачивается»:\n%s",
							service, form, form.origin, lane, databaseColumn(c), err, out)
					}
					got := appliedCount(t, dsn)
					t.Logf("  служба %s · форма `%s` · источник %s · Database %s · полоса DSN %s · "+
						"применено %d / объявлено %d", service, form, form.origin, databaseColumn(c), lane, got, want)
					if got != want {
						t.Fatalf("накат %s в форме `%s` вышел успехом, но применил %d миграций "+
							"из %d объявленных цепочкой. Успех на неполном накате хуже отказа: "+
							"схема не та, а вердикт зелёный.\n%s", service, form, got, want, out)
					}
					// Повторный накат: init-контейнер запускается на КАЖДОМ развёртывании.
					if out, err := runMigratorEnv(t, bin, env, argv...); err != nil {
						t.Fatalf("повторный накат %s в форме `%s` отказал (%v):\n%s",
							service, form, err, out)
					}
					if got := appliedCount(t, dsn); got != want {
						t.Fatalf("повторный накат %s изменил число применённых: %d вместо %d",
							service, got, want)
					}
					proven++
					perService[service]++
				})
				if !ok {
					failed++
				}
			}
		}
		if !pointPolled {
			pointsOutOfPoll++
		}
	}

	services := make([]string, 0, len(perService))
	for svc := range perService {
		services = append(services, svc)
	}
	sort.Strings(services)
	for _, svc := range services {
		t.Logf("  доказано форм×строк у %s: %d", svc, perService[svc])
	}
	polled := len(points) - pointsOutOfPoll
	t.Logf("перепись: точек наката %d, из них в опросе %d, вне опроса %d (носитель формы не отрендерен "+
		"профилем — строки выше, Д91); манифестных форм×строк доказано %d, форм вне опроса %d",
		len(points), polled, pointsOutOfPoll, proven, outOfPoll)

	switch {
	case failed > 0:
		t.Errorf("накат в манифестной форме доказан для %d, отказали %d — находки выше", proven, failed)
	case polled == 0:
		t.Errorf("точек в опросе 0 из %d — доказывать нечего, это отказ, а не зелёный", len(points))
	case proven < polled:
		t.Errorf("доказано %d форм при %d точках в опросе и нуле отказов — прогон отфильтрован "+
			"(-run), и его зелёное относится к %d, а не к %d",
			proven, polled, proven, polled)
	}
}

// TestPointWithoutServiceConfigRefusesTheConfigLane — инъекция Д77 (в): форма
// службы, чей манифест подаёт DSN переменной KACHO_MIGRATOR_DSN, запущенная
// полосой конфигурации (KACHO_<SVC>_DB_*, переменная погашена), ОТКАЗЫВАЕТ —
// точка конфигурации процесса не читает. Близнец — та же форма полосой
// KACHO_MIGRATOR_DSN в TestEveryMigratorAppliesItsChainInItsManifestForm
// (применено = объявлено). Будь у точки запасная полоса — инъекция позеленела
// бы, и доказательство в манифестной форме доказывало бы не ту полосу.
func TestPointWithoutServiceConfigRefusesTheConfigLane(t *testing.T) {
	if testing.Short() {
		t.Skip("накат идёт против живой базы (testcontainers): под кратким режимом пропускается, " +
			"гоняет цель test-pg-outside-selection")
	}
	root := repoRoot(t)
	forms, _ := manifestForms(t, root)
	chainsOf := map[string][]migrationchains.Chain{}
	for _, c := range treeChains(t, root) {
		chainsOf[c.Service] = append(chainsOf[c.Service], c)
	}
	judged := 0
	services := make([]string, 0, len(forms))
	for svc := range forms {
		services = append(services, svc)
	}
	sort.Strings(services)
	for _, service := range services {
		for _, form := range uniqueForms(forms[service]) {
			if laneOf(t, root, form) != laneMigratorDSN || len(chainsOf[service]) == 0 {
				continue
			}
			c := chainsOf[service][0]
			bin := buildApplyPoint(t, root, t.TempDir(), c.Point, service)
			dsn := chainDSN(t, c)
			argv, env := invocationEnv(t, service, form, dsn, t.TempDir(), laneServiceConfig)
			out, err := runMigratorEnv(t, bin, env, argv...)
			if err == nil {
				t.Errorf("форма %s (%s) полосой конфигурации службы ПРИМЕНИЛА накат — у точки "+
					"есть запасная полоса DSN, и манифестная полоса %s не единственная:\n%s",
					service, form.origin, migratorDSNEnv, out)
				continue
			}
			judged++
			t.Logf("  %s `%s` (%s): полоса конфигурации отвергнута, как и должна: %v",
				service, form, form.origin, err)
		}
	}
	t.Logf("перепись: форм с полосой %s судимо %d", migratorDSNEnv, judged)
	if judged == 0 {
		t.Fatalf("форм с полосой %s в дереве 0 — инъекции не на чем стоять", migratorDSNEnv)
	}
}

// TestPointRefusesADatabaseOutsideItsTable — инъекция CX1-115 (г): строке с
// именем базы подана база pgtest.NewEmptyDB (имя `kacho_*_tNNNN`) → точка
// отказывает с именем базы. Близнец — база с именем строки в
// TestEveryMigratorAppliesItsChainToALiveDatabase (применено = объявлено).
func TestPointRefusesADatabaseOutsideItsTable(t *testing.T) {
	if testing.Short() {
		t.Skip("накат идёт против живой базы (testcontainers): под кратким режимом пропускается, " +
			"гоняет цель test-pg-outside-selection")
	}
	root := repoRoot(t)
	judged := 0
	for _, c := range treeChains(t, root) {
		if c.Database == "" {
			continue
		}
		bin := buildApplyPoint(t, root, t.TempDir(), c.Point, c.Service)
		dsn := pgtest.NewEmptyDB(t)
		name, _ := migrationchains.DatabaseOf(dsn)
		out, err := runMigrator(t, bin, "up", "--dsn", dsn)
		switch {
		case err == nil:
			t.Errorf("точка %s приняла базу %s вне своей таблицы — у неё есть запасная ветка на "+
				"имя базы:\n%s", c.Point, name, out)
		case !strings.Contains(out, name):
			t.Errorf("точка %s отказала базе %s, не назвав её:\n%s", c.Point, name, out)
		default:
			judged++
			t.Logf("  точка %s · строка %s · база %s отвергнута с именем", c.Point, c.Database, name)
		}
	}
	t.Logf("перепись: строк с именем базы судимо %d", judged)
	if judged == 0 {
		t.Fatal("строк таблиц цепочек с именем базы в дереве 0 — инъекции не на чем стоять")
	}
}

// uniqueForms схлопывает повторы: одна и та же форма объявлена и шаблоном чарта, и
// его values. Доказывать её дважды незачем, а вот потерять — нельзя.
func uniqueForms(in []invocationForm) []invocationForm {
	seen := make(map[string]bool, len(in))
	var out []invocationForm
	for _, f := range in {
		key := f.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}
