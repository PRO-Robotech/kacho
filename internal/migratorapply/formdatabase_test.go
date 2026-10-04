// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migratorapply_test

// formdatabase_test.go — пара «форма вызова × строка таблицы цепочек» (Д84).
//
// Точка, выбирающая цепочку по имени базы (таблица chains.yaml), накатывает в
// той базе, которую называет DSN формы. Форма поэтому ставится в пару ТОЛЬКО
// со строкой, у которой Database равен dbname манифеста формы: форма чарта
// notify (dbname kacho_notify), поставленная в пару со строкой пробы
// (kacho_notifyprobe), доказывала бы накат, которого её манифест не делает.
//
// Строки нет, а форма её базу называет, — исход решает РЕНДЕР чарта формы
// (Д91). Запись в [outOfPollUntilRendered] и чарт, не рендерящий ни одного
// объекта, — форма вне опроса: печатается отдельной строкой «<служба>: профиль
// <имя> его не рендерит — вне опроса» с числом носителей и в знаменатель не
// входит; это зелёный по названному объёму, а не третья категория. Чарт
// рендерит объекты (носитель формы развёртывается) либо рендер не состоялся —
// красный: «не рендерит» не установлено, а накат формы отказал бы на стенде.
// Записи нет — красный. Запись истекает сама: появилась строка — запись
// находка; чарт начал рендерить — красный до появления строки.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
)

// outOfPollUntilRendered — базы, которые форма манифеста называет, а строки
// таблицы цепочек у них ещё нет, и чарт формы пока ничего не рендерит: «база →
// причина». Запись без предмета — находка (см. pairForm).
//
// Пуста: запись kacho_notify снята полосой N7 (kacho#2915, Д84) вместе с
// появлением строки kacho_notify в таблице цепочек — форма чарта notify
// доказывается накатом на строке своей базы. Механизм записи остаётся под
// пробой TestFormPairsOnlyWithTheRowOfItsDatabase на синтетических записях.
var outOfPollUntilRendered = map[string]string{}

// formPairing — исход пары одной формы со строками её точки.
type formPairing struct {
	// rows — строки, на которых форма доказывается.
	rows []migrationchains.Chain
	// outOfPoll — строка «<служба>: профиль <имя> его не рендерит — вне
	// опроса …» по записи [outOfPollUntilRendered], утверждённая рендером;
	// пусто — форма в опросе.
	outOfPoll string
	// findings — красное: пара не выводится, запись пережила предмет, чарт
	// рендерит носителя формы без строки либо рендер не состоялся.
	findings []string
}

// formChart — чарт формы и его профиль: каталог чарта от корня и файл
// значений, с которым он рендерится (values.yaml самого чарта — тот же, из
// которого выведен dbname формы).
type formChart struct {
	service, dir string
}

// chartRender — число объектов рендера чарта dir (абсолютный путь) его
// собственными значениями; отказ рендера — ошибка.
type chartRender func(dir string) (int, error)

// pairForm — строки точки, с которыми ставится в пару форма с базой formDB.
//
// Точка без таблицы (у всех строк Database пуст) по имени базы не выбирает:
// форма ставится с её строкой, как было. Точка с таблицей — только строка с
// Database == formDB, сравнение на равенство, не по подстроке. Строки нет, а
// запись есть — исход решает рендер чарта формы (render).
func pairForm(formDB string, chart formChart, root string, chains []migrationchains.Chain,
	records map[string]string, render chartRender) formPairing {
	byDB := false
	for _, c := range chains {
		if c.Database != "" {
			byDB = true
		}
	}
	if !byDB {
		return formPairing{rows: chains}
	}
	var p formPairing
	if formDB == "" {
		p.findings = append(p.findings, "dbname формы не выведен, а точка выбирает цепочку по имени базы — "+
			"пару «форма × строка» поставить не на чем")
		return p
	}
	for _, c := range chains {
		if c.Database == formDB {
			p.rows = append(p.rows, c)
		}
	}
	why, recorded := records[formDB]
	switch {
	case len(p.rows) > 0 && recorded:
		p.findings = append(p.findings, fmt.Sprintf("запись outOfPollUntilRendered[%q] пережила предмет: строка "+
			"с этой базой в таблице есть — запись снимается тем же изменением", formDB))
	case len(p.rows) == 0 && recorded:
		p.findings, p.outOfPoll = outOfPoll(formDB, chart, root, why, render)
	case len(p.rows) == 0:
		p.findings = append(p.findings, fmt.Sprintf("форма называет базу %q, а строки с ней в таблице "+
			"цепочек точки нет — накат этой формы отказал бы на стенде", formDB))
	}
	return p
}

// outOfPoll — исход формы с записью и без строки: рендер чарта формы.
func outOfPoll(formDB string, chart formChart, root, why string, render chartRender) ([]string, string) {
	if chart.dir == "" {
		return []string{fmt.Sprintf("форма с базой %q объявлена вне чарта — «носитель не рендерится» "+
			"рендером не утверждается, а строки нет", formDB)}, ""
	}
	n, err := render(filepath.Join(root, filepath.FromSlash(chart.dir)))
	switch {
	case err != nil:
		return []string{fmt.Sprintf("рендер чарта %s не состоялся — «носитель формы не рендерится» не "+
			"установлено, а строки %q нет: %v", chart.dir, formDB, err)}, ""
	case n > 0:
		return []string{fmt.Sprintf("чарт %s рендерит %d объектов — носитель формы с базой %q развёртывается, "+
			"а строки с ней в таблице цепочек нет: накат отказал бы на стенде", chart.dir, n, formDB)}, ""
	}
	return nil, fmt.Sprintf("%s: профиль %s/values.yaml его не рендерит (объектов 0) — вне опроса; "+
		"носителей формы 1; %s", chart.service, chart.dir, why)
}

// helmRender — [chartRender] настоящим helm: `helm template` каталога чарта
// его собственными значениями, число документов с `kind`. helm вне PATH —
// ошибка, а не «объектов 0».
func helmRender(dir string) (int, error) {
	if _, err := exec.LookPath("helm"); err != nil {
		return 0, errors.New("helm не в PATH — рендер нечем исполнить")
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("helm", "template", "kacho-form", dir) // #nosec G204 -- каталог чарта дерева либо копии пробы
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("helm template %s: %v: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	n := 0
	dec := yaml.NewDecoder(&stdout)
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		if err != nil {
			return 0, fmt.Errorf("рендер чарта %s не разобран: %w", dir, err)
		}
		if doc["kind"] != nil {
			n++
		}
	}
}

// chartOfForm — чарт формы по её источнику: `<чарт>/templates/<файл>`;
// манифест вне чарта — пустой каталог.
func chartOfForm(service string, form invocationForm) formChart {
	file, _, _ := strings.Cut(form.origin, ":")
	dir, _, ok := strings.Cut(file, "/templates/")
	if !ok {
		return formChart{service: service}
	}
	return formChart{service: service, dir: dir}
}

// formRows — пара формы со строками её точки, как её ставит доказательство
// наката в манифестной форме: dbname манифеста выводится только у точки,
// выбирающей цепочку по имени базы; у точки без таблицы форма ставится с её
// строкой. Один источник пары для доказательства и для пробы на дереве.
func formRows(root string, form invocationForm, chains []migrationchains.Chain) formPairing {
	return formRowsRendered(root, form, chains, helmRender)
}

// formRowsRendered — formRows с исполнителем рендера render (инъекция пробы).
func formRowsRendered(root string, form invocationForm, chains []migrationchains.Chain, render chartRender) formPairing {
	byDB := false
	for _, c := range chains {
		if c.Database != "" {
			byDB = true
		}
	}
	if !byDB {
		return formPairing{rows: chains}
	}
	db, err := manifestDatabase(root, form)
	if err != nil {
		return formPairing{findings: []string{"dbname формы не выведен из манифеста: " + err.Error()}}
	}
	service := form.service
	if service == "" && len(chains) > 0 {
		service = chains[0].Service
	}
	return pairForm(db, chartOfForm(service, form), root, chains, outOfPollUntilRendered, render)
}

// dsnValueLine — строка значения переменной KACHO_MIGRATOR_DSN под её `- name:`.
var dsnValueLine = regexp.MustCompile(`^\s*value:\s*(.+?)\s*$`)

// printfValue — значение формой `{{ printf "<формат>" <аргументы> | quote }}`.
var printfValue = regexp.MustCompile(`^\{\{-?\s*printf\s+"((?:[^"\\]|\\.)*)"\s+(.+?)\s*\|\s*quote\s*-?\}\}$`)

// fmtVerb — глагол формата printf (%% — не глагол).
var fmtVerb = regexp.MustCompile(`%[-+# 0]*[0-9]*[a-zA-Z%]`)

// manifestDatabase — dbname, который манифест формы подаёт точке переменной
// KACHO_MIGRATOR_DSN. Знает две записи значения: литерал в кавычках и
// `printf` шаблона чарта с аргументом `.Values.<путь>` на месте dbname
// (значение — из values.yaml чарта формы). Иная запись — ошибка с координатой,
// а не пустое имя: недочитанная форма выглядела бы покрытой.
func manifestDatabase(root string, form invocationForm) (string, error) {
	file, _, _ := strings.Cut(form.origin, ":")
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file))) // #nosec G304 -- манифест дерева
	if err != nil {
		return "", fmt.Errorf("манифест %s не прочитан: %w", file, err)
	}
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "- name: "+migratorDSNEnv || i+1 >= len(lines) {
			continue
		}
		m := dsnValueLine.FindStringSubmatch(lines[i+1])
		if m == nil {
			return "", fmt.Errorf("%s:%d: у %s нет строки value: — запись значения не знакома разбору",
				file, i+2, migratorDSNEnv)
		}
		dsn, err := dsnOfValue(root, file, m[1])
		if err != nil {
			return "", fmt.Errorf("%s:%d: %w", file, i+2, err)
		}
		return migrationchains.DatabaseOf(dsn)
	}
	return "", fmt.Errorf("%s не подаёт %s — dbname формы не выводится", file, migratorDSNEnv)
}

// dsnOfValue — DSN из записи значения: литерал либо printf с подстановкой
// аргументов `.Values.<путь>` из values.yaml чарта.
func dsnOfValue(root, file, value string) (string, error) {
	if s, err := strconv.Unquote(value); err == nil {
		return s, nil
	}
	m := printfValue.FindStringSubmatch(value)
	if m == nil {
		return "", fmt.Errorf("значение %s не литерал и не `printf … | quote` — разбор его не знает", value)
	}
	format, err := strconv.Unquote(`"` + m[1] + `"`)
	if err != nil {
		return "", fmt.Errorf("формат printf не разобран: %w", err)
	}
	args := strings.Fields(m[2])
	values, err := chartValues(root, file)
	if err != nil {
		return "", err
	}
	verbs := 0
	var out strings.Builder
	last := 0
	for _, loc := range fmtVerb.FindAllStringIndex(format, -1) {
		out.WriteString(format[last:loc[0]])
		last = loc[1]
		if format[loc[0]:loc[1]] == "%%" {
			out.WriteByte('%')
			continue
		}
		if verbs >= len(args) {
			return "", fmt.Errorf("глаголов формата больше, чем аргументов (%d)", len(args))
		}
		v, err := valueAt(values, args[verbs])
		if err != nil {
			return "", err
		}
		out.WriteString(v)
		verbs++
	}
	out.WriteString(format[last:])
	if verbs != len(args) {
		return "", fmt.Errorf("глаголов формата %d, аргументов %d", verbs, len(args))
	}
	return out.String(), nil
}

// chartValues — values.yaml чарта, которому принадлежит шаблон file
// (`<чарт>/templates/<файл>` → `<чарт>/values.yaml`).
func chartValues(root, file string) (map[string]any, error) {
	chart, _, ok := strings.Cut(file, "/templates/")
	if !ok {
		return nil, fmt.Errorf("%s не шаблон чарта — values.yaml не выводится", file)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(chart), "values.yaml")) // #nosec G304 -- чарт дерева
	if err != nil {
		return nil, fmt.Errorf("values.yaml чарта %s не прочитан: %w", chart, err)
	}
	var v map[string]any
	if err := yaml.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("values.yaml чарта %s не разобран: %w", chart, err)
	}
	return v, nil
}

// valueAt — значение аргумента `.Values.a.b` в values чарта. Иной аргумент —
// ошибка: подставить его значение разбор не умеет.
func valueAt(values map[string]any, arg string) (string, error) {
	p, ok := strings.CutPrefix(arg, ".Values.")
	if !ok {
		return "", fmt.Errorf("аргумент printf %q не `.Values.<путь>`", arg)
	}
	var cur any = values
	for _, seg := range strings.Split(p, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("путь %s: %q не узел", arg, seg)
		}
		if cur, ok = m[seg]; !ok {
			return "", fmt.Errorf("путь %s: ключа %q в values.yaml нет", arg, seg)
		}
	}
	switch v := cur.(type) {
	case string:
		return v, nil
	case int, float64, bool:
		return fmt.Sprint(v), nil
	}
	return "", fmt.Errorf("путь %s ведёт не к скаляру (%T)", arg, cur)
}

// TestFormPairsOnlyWithTheRowOfItsDatabase — Д84, Д91 на синтетике: форма
// ставится в пару только со строкой своей базы; база без строки — по записи
// исход решает рендер (объектов 0 — вне опроса, объекты либо отказ рендера —
// красный), без записи — красный; запись при строке — находка; точка без
// таблицы — пара со своей строкой.
func TestFormPairsOnlyWithTheRowOfItsDatabase(t *testing.T) {
	probe := migrationchains.Chain{Service: "notify", Database: "kacho_notifyprobe", Dir: "services/notify/internal/probemigrations"}
	gw := migrationchains.Chain{Service: "notify", Database: "kacho_notify", Dir: "services/notify/internal/migrations"}
	single := migrationchains.Chain{Service: "vpc", Dir: "services/vpc/internal/migrations"}
	recs := map[string]string{"kacho_notify": "N7"}
	chart := formChart{service: "notify", dir: "deploy/helm/notify"}
	renders := func(n int, err error) chartRender {
		return func(string) (int, error) { return n, err }
	}
	none := renders(0, nil)

	if p := pairForm("kacho_notifyprobe", chart, "/", []migrationchains.Chain{probe}, recs, none); len(p.rows) != 1 ||
		p.rows[0] != probe || p.outOfPoll != "" || len(p.findings) != 0 {
		t.Errorf("близнец: форма своей базы — пара со строкой пробы; получено %+v", p)
	}
	if p := pairForm("kacho_notify", chart, "/", []migrationchains.Chain{probe}, recs, none); len(p.rows) != 0 ||
		!strings.HasPrefix(p.outOfPoll, "notify: профиль deploy/helm/notify/values.yaml его не рендерит") ||
		!strings.Contains(p.outOfPoll, "вне опроса") || !strings.Contains(p.outOfPoll, "носителей формы 1") ||
		len(p.findings) != 0 {
		t.Errorf("форма kacho_notify, чарт не рендерит: ожидалась строка «вне опроса» без пары; получено %+v", p)
	}
	if p := pairForm("kacho_notify", chart, "/", []migrationchains.Chain{probe}, recs, renders(7, nil)); len(p.findings) != 1 ||
		p.outOfPoll != "" || !strings.Contains(p.findings[0], "рендерит 7 объектов") {
		t.Errorf("чарт рендерит носителя формы без строки: ожидался красный; получено %+v", p)
	}
	if p := pairForm("kacho_notify", chart, "/", []migrationchains.Chain{probe}, recs, renders(0, errors.New("x"))); len(p.findings) != 1 ||
		p.outOfPoll != "" || !strings.Contains(p.findings[0], "не состоялся") {
		t.Errorf("рендер не состоялся: ожидался красный, а не «не рендерит»; получено %+v", p)
	}
	if p := pairForm("kacho_notify", formChart{service: "notify"}, "/", []migrationchains.Chain{probe}, recs, none); len(p.findings) != 1 ||
		p.outOfPoll != "" {
		t.Errorf("форма вне чарта: «не рендерит» не утверждается — ожидался красный; получено %+v", p)
	}
	if p := pairForm("kacho_notify", chart, "/", []migrationchains.Chain{probe}, nil, none); len(p.rows) != 0 || len(p.findings) != 1 {
		t.Errorf("база без строки и без записи: ожидался красный; получено %+v", p)
	}
	if p := pairForm("kacho_notify", chart, "/", []migrationchains.Chain{probe, gw}, recs, none); len(p.rows) != 1 ||
		p.rows[0] != gw || len(p.findings) != 1 {
		t.Errorf("строка kacho_notify появилась: ожидались пара с ней и находка «запись пережила предмет»; получено %+v", p)
	}
	if p := pairForm("kacho_notify", chart, "/", []migrationchains.Chain{probe, gw}, nil, none); len(p.rows) != 1 || p.rows[0] != gw {
		t.Errorf("подстрока: kacho_notify не должна ставиться с kacho_notifyprobe; получено %+v", p)
	}
	if p := pairForm("", formChart{}, "/", []migrationchains.Chain{single}, nil, none); len(p.rows) != 1 || p.rows[0] != single {
		t.Errorf("точка без таблицы: пара со своей строкой; получено %+v", p)
	}
}

// TestNotifyFormPairsWithItsManifestDatabase — Д84 на дереве: форма чарта
// notify называет kacho_notify (значение values.yaml, точное), в паре со
// строкой пробы её нет, а в паре со строкой kacho_notify (полоса N7) — есть.
func TestNotifyFormPairsWithItsManifestDatabase(t *testing.T) {
	root := repoRoot(t)
	forms, _ := manifestForms(t, root)
	notifyForms := uniqueForms(forms["notify"])
	if len(notifyForms) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: формы вызова точки notify в манифестах нет — пару ставить не на чем")
	}
	var chains []migrationchains.Chain
	for _, c := range treeChains(t, root) {
		if c.Service == "notify" {
			chains = append(chains, c)
		}
	}
	for _, f := range notifyForms {
		db, err := manifestDatabase(root, f)
		if err != nil {
			t.Fatalf("форма %s (%s): dbname не выведен: %v", f, f.origin, err)
		}
		if db != "kacho_notify" {
			t.Errorf("форма %s (%s): dbname %q, ожидалось ровно kacho_notify", f, f.origin, db)
		}
		p := formRows(root, f, chains)
		for _, r := range p.rows {
			if r.Database != db {
				t.Errorf("форма %s с базой %s поставлена в пару со строкой %s", f, db, r.Database)
			}
		}
		for _, fd := range p.findings {
			t.Errorf("форма %s: %s", f, fd)
		}
		if len(p.rows) != 1 || p.outOfPoll != "" {
			t.Errorf("форма %s с базой %s: строк в паре %d, вне опроса %q — ожидалась ровно строка kacho_notify",
				f, db, len(p.rows), p.outOfPoll)
		}
		t.Logf("  форма %s · %s · dbname %s · строк в паре %d", f, f.origin, db, len(p.rows))
	}
}

// TestManifestDatabaseKnowsBothValueForms — разбор значения DSN знает литерал
// и printf чарта; незнакомая запись — ошибка, а не пустое имя.
func TestManifestDatabaseKnowsBothValueForms(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("deploy/helm/x/values.yaml", "db:\n  host: h\n  port: 5432\n  name: kacho_x\n")
	cases := map[string]struct {
		value string
		want  string
	}{
		"literal": {`"host=h dbname=kacho_lit sslmode=require"`, "kacho_lit"},
		"printf":  {`{{ printf "host=%s port=%v dbname=%s sslmode=require" .Values.db.host .Values.db.port .Values.db.name | quote }}`, "kacho_x"},
	}
	for name, c := range cases {
		rel := "deploy/helm/x/templates/" + name + ".yaml"
		write(rel, "          env:\n            - name: KACHO_MIGRATOR_DSN\n              value: "+c.value+"\n")
		got, err := manifestDatabase(root, invocationForm{origin: rel + ":1"})
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v; ожидалось %q", name, got, err, c.want)
		}
	}
	for name, v := range map[string]string{
		"toYaml":       `{{ .Values.db.dsn }}`,
		"not values":   `{{ printf "dbname=%s" $name | quote }}`,
		"missing path": `{{ printf "dbname=%s" .Values.db.absent | quote }}`,
	} {
		rel := "deploy/helm/x/templates/bad.yaml"
		write(rel, "            - name: KACHO_MIGRATOR_DSN\n              value: "+v+"\n")
		if got, err := manifestDatabase(root, invocationForm{origin: rel + ":1"}); err == nil {
			t.Errorf("%s: запись %s разобрана как %q — незнакомая запись обязана быть ошибкой", name, v, got)
		}
	}
}
