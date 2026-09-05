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

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// migratorBinaryPath — путь бинаря внутри образа. Им зовут накат все манифесты
// развёртывания, поэтому он же служит признаком, по которому форма вызова
// узнаётся в манифесте.
const migratorBinaryPath = "/usr/local/bin/kacho-migrator"

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

// commandLine / argsLine — объявление формы в манифесте. Все семь пишут его
// потоковым стилем в одну строку; форму, записанную блочным стилем, разбор не
// прочитает — и потому НЕ МОЛЧИТ о ней: строка, называющая бинарь, но не давшая
// ни одного аргумента, роняет перепись (см. manifestForms).
var (
	commandLine = regexp.MustCompile(`^\s*command:\s*\[(.+)\]\s*$`)
	argsLine    = regexp.MustCompile(`^\s*args:\s*\[(.+)\]\s*$`)
)

// subchartKey — ключ подчарта в наложении значений зонта (`kacho-iam:` в нулевой
// колонке). Им наложение называет службу, которую настраивает.
var subchartKey = regexp.MustCompile(`^kacho-([a-z0-9]+):`)

// serviceOfManifest — чей это манифест. Выводится из ПУТИ, а не из содержимого:
// имя службы внутри файла бывает шаблонным (`{{ .Values.name }}`).
//
// Две раскладки, обе живые: `services/<svc>/deploy/…` и чарт зонта
// `deploy/helm/umbrella/charts/kacho-<svc>/…`. Путь, не подошедший ни под одну,
// возвращает пустую строку — тогда службу называет ключ подчарта, см.
// serviceForForm. Приписать форму соседу хуже, чем остановиться: покрытой
// оказалась бы не та точка наката.
func serviceOfManifest(rel string) string {
	rel = filepath.ToSlash(rel)
	if s, ok := strings.CutPrefix(rel, "services/"); ok {
		if i := strings.Index(s, "/"); i > 0 {
			return s[:i]
		}
		return ""
	}
	if s, ok := strings.CutPrefix(rel, "deploy/helm/umbrella/charts/kacho-"); ok {
		if i := strings.Index(s, "/"); i > 0 {
			return s[:i]
		}
	}
	return ""
}

// serviceForForm — чью форму вызова объявляет строка line файла rel.
//
// Источников ДВА, и второй не запасной: наложение значений зонта
// (`deploy/helm/umbrella/values.*.yaml`) настраивает ВСЕ подчарты одним файлом,
// поэтому путь там службы не называет — её называет ключ подчарта над строкой.
// Обход, знающий только путь, на таком файле остановился бы; знающий только
// первый попавшийся ключ — приписал бы форму соседу.
func serviceForForm(rel string, lines []string, at int) string {
	if s := serviceOfManifest(rel); s != "" {
		return s
	}
	for i := at; i >= 0; i-- {
		if m := subchartKey.FindStringSubmatch(lines[i]); m != nil {
			return m[1]
		}
	}
	return ""
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
			if !strings.Contains(string(body), migratorBinaryPath) {
				continue
			}
			rel, relErr := filepath.Rel(root, abs)
			if relErr != nil {
				t.Fatalf("относительный путь для %s: %v", abs, relErr)
			}
			for _, form := range formsInManifest(t, rel, string(body)) {
				if form.service == "" {
					t.Fatalf("%s объявляет форму вызова %s, но ЧЬЮ — не выводится ни из "+
						"пути, ни из ключа подчарта. Приписать форму соседу хуже, чем "+
						"остановиться: покрытой оказалась бы не та точка наката",
						form.origin, migratorBinaryPath)
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
func formsInManifest(t *testing.T, rel, body string) []invocationForm {
	t.Helper()

	var out []invocationForm
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		m := commandLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		argv, ok := parseFlowSequence(m[1])
		if !ok || argv[0] != migratorBinaryPath {
			if !ok && strings.Contains(line, migratorBinaryPath) {
				t.Fatalf("%s:%d объявляет форму вызова %s, но разбор её НЕ ПРОЧИТАЛ. "+
					"Молчаливый пропуск оставил бы форму вне наблюдения — то есть дал бы "+
					"ровно ту слепую зону, ради которой эта проба написана:\n%s",
					rel, i+1, migratorBinaryPath, line)
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
			t.Fatalf("%s:%d зовёт %s БЕЗ единого аргумента — пустая командная строка "+
				"мигратора есть отказ, а не накат", rel, i+1, migratorBinaryPath)
		}
		out = append(out, invocationForm{
			service: serviceForForm(rel, lines, i),
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
		service, _ := migrationsDirOf(pkg)
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

	t.Logf("перепись: манифестов прочитано %d, точек наката %d, форма выведена для %d",
		filesRead, len(points), covered)
}

// dsnParts — разобранный DSN пробы. Нужен той полосе доставки конфигурации, где
// сервис читает БД по частям, а не строкой.
type dsnParts struct {
	user, password, host, port, name string
}

func splitDSN(t *testing.T, dsn string) dsnParts {
	t.Helper()
	rest, ok := strings.CutPrefix(dsn, "postgres://")
	if !ok {
		t.Fatalf("DSN пробы не в форме postgres://… (%q) — разобрать по частям нечем", dsn)
	}
	if i := strings.IndexAny(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	cred, hostPart, ok := strings.Cut(rest, "@")
	if !ok {
		t.Fatalf("в DSN пробы нет учётных данных (%q)", dsn)
	}
	user, password, _ := strings.Cut(cred, ":")
	hostPort, name, ok := strings.Cut(hostPart, "/")
	if !ok {
		t.Fatalf("в DSN пробы нет имени базы (%q)", dsn)
	}
	host, port, ok := strings.Cut(hostPort, ":")
	if !ok {
		t.Fatalf("в DSN пробы нет порта (%q)", dsn)
	}
	return dsnParts{user: user, password: password, host: host, port: port, name: name}
}

// serviceEnvPrefix — приставка переменных окружения сервиса (`KACHO_VPC`).
func serviceEnvPrefix(service string) string {
	return "KACHO_" + strings.ToUpper(service)
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
//   - форма несёт `--config <путь>` → конфигурация файлом, путь подменяется;
//   - манифест называет `KACHO_<SVC>_CONFIG_PATH` → конфигурация файлом по этому пути;
//   - иначе → конфигурация окружением `KACHO_<SVC>_DB_*`.
//
// Флаг `--dsn` не подставляется НИКОГДА, и это предмет: первый источник приоритета
// (`--dsn` > `KACHO_MIGRATOR_DSN` > конфигурация) доказан пробой наката рядом, а
// последний — тот, которым пользуются все семь развёртываний, — не был доказан
// ничем. Именно его и гоняет эта полоса.
func invocationEnv(t *testing.T, service string, form invocationForm, dsn, dir string) ([]string, []string) {
	t.Helper()

	argv := append([]string(nil), form.argv...)
	// Переменная общего приоритета гасится: не погасив её, полоса конфигурации
	// зеленела бы на DSN из окружения прогона — то есть доказывала бы не себя.
	env := []string{"KACHO_MIGRATOR_DSN="}

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
// дереве не зовёт НИ ОДИН манифест и НИ ОДИН Makefile. Все семь развёртываний
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

	binDir := t.TempDir()
	cfgDir := t.TempDir()
	proven, failed := 0, 0

	for _, pkg := range points {
		service, migDir := migrationsDirOf(pkg)
		serviceForms := uniqueForms(forms[service])
		if len(serviceForms) == 0 {
			// Перепись выше уже назвала это находкой; здесь молчать нельзя тем
			// более — иначе «доказано 6 из 7» выглядело бы полным прогоном.
			t.Errorf("у %s нет выведенной формы вызова — накат в манифестной форме "+
				"НЕ ДОКАЗАН", service)
			failed++
			continue
		}

		bin := buildApplyPoint(t, root, binDir, pkg, service)

		for _, form := range serviceForms {
			ok := t.Run(service+"/"+strings.ReplaceAll(form.String(), " ", "_"), func(t *testing.T) {
				want := chainLength(t, root, migDir)
				if want == 0 {
					t.Fatalf("в %s НЕТ ни одного файла миграции — эталона для сверки "+
						"не существует", migDir)
				}
				dsn := pgtest.NewEmptyDB(t)
				argv, env := invocationEnv(t, service, form, dsn, cfgDir)

				out, err := runMigratorEnv(t, bin, env, argv...)
				if err != nil {
					t.Fatalf("накат %s в манифестной форме `%s` (%s) ОТКАЗАЛ (%v) — "+
						"это и означает «сервис не разворачивается»:\n%s",
						service, form, form.origin, err, out)
				}
				if got := appliedCount(t, dsn); got != want {
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
			})
			if !ok {
				failed++
			}
		}
	}

	t.Logf("перепись: точек наката %d, манифестных форм доказано %d", len(points), proven)

	switch {
	case failed > 0:
		t.Errorf("накат в манифестной форме доказан для %d, отказали %d — находки выше", proven, failed)
	case proven < len(points):
		t.Errorf("доказано %d форм при %d точках наката и нуле отказов — прогон отфильтрован "+
			"(-run), и его зелёное относится к %d, а не к %d",
			proven, len(points), proven, len(points))
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
