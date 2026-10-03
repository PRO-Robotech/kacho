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
// Строки нет, а форма её базу называет, — исходов два. Запись в
// [notRunUntilRow] — «НЕ ВЫПОЛНИЛОСЬ» с причиной: в зелёные и в красные это не
// входит, и запись истекает сама — появилась строка, и запись стала находкой.
// Записи нет — красный: накат формы отказал бы на стенде.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
)

// notRunUntilRow — базы, которые форма манифеста называет, а строки таблицы
// цепочек у них ещё нет: «база → причина». Запись истекает сама: строка с этой
// базой появилась — запись находка, снимается тем же изменением.
var notRunUntilRow = map[string]string{
	"kacho_notify": "строки kacho_notify в таблице цепочек нет (N7, kacho#2915, Д84)",
}

// formPairing — исход пары одной формы со строками её точки.
type formPairing struct {
	// rows — строки, на которых форма доказывается.
	rows []migrationchains.Chain
	// notRun — «НЕ ВЫПОЛНИЛОСЬ: …» по записи [notRunUntilRow]; пусто — исход есть.
	notRun string
	// findings — красное: пара не выводится либо запись пережила предмет.
	findings []string
}

// pairForm — строки точки, с которыми ставится в пару форма с базой formDB.
//
// Точка без таблицы (у всех строк Database пуст) по имени базы не выбирает:
// форма ставится с её строкой, как было. Точка с таблицей — только строка с
// Database == formDB, сравнение на равенство, не по подстроке.
func pairForm(formDB string, chains []migrationchains.Chain, records map[string]string) formPairing {
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
		p.findings = append(p.findings, fmt.Sprintf("запись notRunUntilRow[%q] пережила предмет: строка "+
			"с этой базой в таблице есть — запись снимается тем же изменением", formDB))
	case len(p.rows) == 0 && recorded:
		p.notRun = "НЕ ВЫПОЛНИЛОСЬ: " + why
	case len(p.rows) == 0:
		p.findings = append(p.findings, fmt.Sprintf("форма называет базу %q, а строки с ней в таблице "+
			"цепочек точки нет — накат этой формы отказал бы на стенде", formDB))
	}
	return p
}

// formRows — пара формы со строками её точки, как её ставит доказательство
// наката в манифестной форме: dbname манифеста выводится только у точки,
// выбирающей цепочку по имени базы; у точки без таблицы форма ставится с её
// строкой. Один источник пары для доказательства и для пробы на дереве.
func formRows(root string, form invocationForm, chains []migrationchains.Chain) formPairing {
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
	return pairForm(db, chains, notRunUntilRow)
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

// TestFormPairsOnlyWithTheRowOfItsDatabase — Д84 на синтетике: форма ставится
// в пару только со строкой своей базы; база без строки — «НЕ ВЫПОЛНИЛОСЬ» по
// записи либо красный без неё; запись при строке — находка; точка без
// таблицы — пара со своей строкой.
func TestFormPairsOnlyWithTheRowOfItsDatabase(t *testing.T) {
	probe := migrationchains.Chain{Service: "notify", Database: "kacho_notifyprobe", Dir: "services/notify/internal/probemigrations"}
	gw := migrationchains.Chain{Service: "notify", Database: "kacho_notify", Dir: "services/notify/internal/migrations"}
	single := migrationchains.Chain{Service: "vpc", Dir: "services/vpc/internal/migrations"}
	recs := map[string]string{"kacho_notify": "N7"}

	if p := pairForm("kacho_notifyprobe", []migrationchains.Chain{probe}, recs); len(p.rows) != 1 ||
		p.rows[0] != probe || p.notRun != "" || len(p.findings) != 0 {
		t.Errorf("близнец: форма своей базы — пара со строкой пробы; получено %+v", p)
	}
	if p := pairForm("kacho_notify", []migrationchains.Chain{probe}, recs); len(p.rows) != 0 ||
		!strings.HasPrefix(p.notRun, "НЕ ВЫПОЛНИЛОСЬ: ") || len(p.findings) != 0 {
		t.Errorf("форма kacho_notify при строке только пробы: ожидалось «НЕ ВЫПОЛНИЛОСЬ» без пары; получено %+v", p)
	}
	if p := pairForm("kacho_notify", []migrationchains.Chain{probe}, nil); len(p.rows) != 0 || len(p.findings) != 1 {
		t.Errorf("база без строки и без записи: ожидался красный; получено %+v", p)
	}
	if p := pairForm("kacho_notify", []migrationchains.Chain{probe, gw}, recs); len(p.rows) != 1 ||
		p.rows[0] != gw || len(p.findings) != 1 {
		t.Errorf("строка kacho_notify появилась: ожидались пара с ней и находка «запись пережила предмет»; получено %+v", p)
	}
	if p := pairForm("kacho_notify", []migrationchains.Chain{probe, gw}, nil); len(p.rows) != 1 || p.rows[0] != gw {
		t.Errorf("подстрока: kacho_notify не должна ставиться с kacho_notifyprobe; получено %+v", p)
	}
	if p := pairForm("", []migrationchains.Chain{single}, nil); len(p.rows) != 1 || p.rows[0] != single {
		t.Errorf("точка без таблицы: пара со своей строкой; получено %+v", p)
	}
}

// TestNotifyFormPairsWithItsManifestDatabase — Д84 на дереве: форма чарта
// notify называет kacho_notify (значение values.yaml, точное), и в паре со
// строкой пробы её нет; исход — «НЕ ВЫПОЛНИЛОСЬ» по записи, пока строки
// kacho_notify в таблице нет.
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
		t.Logf("  форма %s · %s · dbname %s · строк в паре %d · %s", f, f.origin, db, len(p.rows), p.notRun)
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
