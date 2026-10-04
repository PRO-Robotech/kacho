// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// seedaddresspoolparity_test.go — гейт УК3-37 (issue-2918, полоса S1-A8): ключи
// состояния `AddressPool`, которые посев стенда кладёт в строку журнала vpc,
// совпадают с ключами, которые кладёт путь записи сервиса
// (`helpers.AddressPoolDomainPayload`).
//
// # Предмет
//
// Строку журнала `AddressPool CREATED` пишут двое: путь записи сервиса и посев
// стенда (`deploy/scripts/vpc-address-pool-baseline.sql`). Читатель журнала один и
// формы не различает, поэтому у состояния ОДНА форма (замысел З8, CX3E-07 (в)):
// ключ, который есть только у посева (`actor` до под-фазы), — второе представление
// строки; ключ, которого у посева нет, — потерянное поле.
//
// # Откуда берутся обе стороны
//
// Посев — аргументы `jsonb_build_object(…)` в коде файла (комментарии сняты: в них
// законно упоминаются снятые ключи). Путь записи — `AddressPoolDomainPayload` есть
// `DomainToMap(p)` над `*domain.AddressPool`, то есть JSON-снимок структуры: ключи —
// экспортируемые поля под их именем либо именем тега `json`. Пакет гейта не может
// импортировать `services/vpc/internal/…`, поэтому обе предпосылки читаются разбором
// исходника и проверяются: помощник — ровно `DomainToMap` своего аргумента,
// структура — без встраиваний и без собственного `MarshalJSON`. Иная форма — отказ
// с координатой, а не молчание.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	seedPoolSQLRel      = filepath.Join("deploy", "scripts", "vpc-address-pool-baseline.sql")
	seedPoolDomainDir   = filepath.Join("services", "vpc", "internal", "domain")
	seedPoolHelperFile  = filepath.Join("services", "vpc", "internal", "repo", "helpers", "payloads.go")
	seedPoolDomainType  = "AddressPool"
	seedPoolHelperFunc  = "AddressPoolDomainPayload"
	seedPoolHelperInner = "DomainToMap"
)

// TestSeedAddressPoolPayloadKeysMatchTheWriterPath — живой гейт по дереву.
func TestSeedAddressPoolPayloadKeysMatchTheWriterPath(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	sqlBody, err := os.ReadFile(filepath.Join(root, seedPoolSQLRel))
	if err != nil {
		t.Fatalf("посев %s не прочитан: %v", seedPoolSQLRel, err)
	}
	seedKeys, err := seedPayloadKeys(string(sqlBody))
	if err != nil {
		t.Fatalf("%s: %v", seedPoolSQLRel, err)
	}

	helperBody, err := os.ReadFile(filepath.Join(root, seedPoolHelperFile))
	if err != nil {
		t.Fatalf("помощник пути записи %s не прочитан: %v", seedPoolHelperFile, err)
	}
	if err := helperIsDomainSnapshot(string(helperBody)); err != nil {
		t.Fatalf("%s: %v", seedPoolHelperFile, err)
	}

	domainSrc, files, err := readGoDir(filepath.Join(root, seedPoolDomainDir))
	if err != nil {
		t.Fatalf("пакет домена %s не прочитан: %v", seedPoolDomainDir, err)
	}
	domainKeys, err := structJSONKeys(domainSrc, seedPoolDomainType)
	if err != nil {
		t.Fatalf("%s: %v", seedPoolDomainDir, err)
	}

	t.Logf("осмотрено: %s (%d байт, ключей посева %d); %s (%d файлов, ключей domain.%s %d); %s",
		seedPoolSQLRel, len(sqlBody), len(seedKeys), seedPoolDomainDir, files, seedPoolDomainType,
		len(domainKeys), seedPoolHelperFile)

	if msg := keyParityFinding(seedKeys, domainKeys); msg != "" {
		t.Errorf("УК3-37: форма состояния AddressPool у посева стенда расходится с путём записи сервиса:\n%s\n"+
			"Исход: ключи полезной нагрузки посева — ровно ключи helpers.%s (JSON-снимок domain.%s); "+
			"атрибуция строки — колонка initiator, а не ключ состояния.",
			msg, seedPoolHelperFunc, seedPoolDomainType)
	}
}

// keyParityFinding — пусто при совпадении множеств, иначе текст с ключами обеих сторон.
func keyParityFinding(seed, domain []string) string {
	onlySeed := setMinus(seed, domain)
	onlyDomain := setMinus(domain, seed)
	if len(onlySeed) == 0 && len(onlyDomain) == 0 {
		return ""
	}
	var b strings.Builder
	if len(onlySeed) > 0 {
		fmt.Fprintf(&b, "  ключи только у посева (%s): %s\n", seedPoolSQLRel, strings.Join(onlySeed, ", "))
	}
	if len(onlyDomain) > 0 {
		fmt.Fprintf(&b, "  ключи только у пути записи (domain.%s): %s\n", seedPoolDomainType, strings.Join(onlyDomain, ", "))
	}
	return b.String()
}

func setMinus(a, b []string) []string {
	in := map[string]bool{}
	for _, k := range b {
		in[k] = true
	}
	var out []string
	for _, k := range a {
		if !in[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// seedPoolStripSQL снимает (с учётом строковых литералов, в отличие от stripSQLComments) `--` до конца строки и `/* … */` вне строковых литералов.
func seedPoolStripSQL(sql string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if inStr {
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					b.WriteByte(sql[i+1])
					i++
					continue
				}
				inStr = false
			}
			continue
		}
		switch {
		case c == '\'':
			inStr = true
			b.WriteByte(c)
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			if i < len(sql) {
				b.WriteByte('\n')
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 3
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// seedPayloadKeys — ключи единственного вызова `jsonb_build_object` в коде файла.
func seedPayloadKeys(sql string) ([]string, error) {
	code := seedPoolStripSQL(sql)
	lower := strings.ToLower(code)
	const call = "jsonb_build_object("
	first := strings.Index(lower, call)
	if first < 0 {
		return nil, fmt.Errorf("в коде посева нет %s…) — форма полезной нагрузки не распознана, сверять нечего", call)
	}
	if strings.Count(lower, call) != 1 {
		return nil, fmt.Errorf("в коде посева %d вызовов %s…), ожидался ровно один — какой из них состояние, гейт не знает",
			strings.Count(lower, call), call)
	}
	args, err := topLevelArgs(code[first+len(call):])
	if err != nil {
		return nil, err
	}
	if len(args) == 0 || len(args)%2 != 0 {
		return nil, fmt.Errorf("у %s…) %d аргументов — пары «ключ, значение» не складываются", call, len(args))
	}
	var keys []string
	for i := 0; i < len(args); i += 2 {
		k := strings.TrimSpace(args[i])
		if len(k) < 2 || k[0] != '\'' || k[len(k)-1] != '\'' {
			return nil, fmt.Errorf("ключ %d у %s…) — не строковый литерал: %q", i/2+1, call, k)
		}
		keys = append(keys, strings.ReplaceAll(k[1:len(k)-1], "''", "'"))
	}
	return keys, nil
}

// topLevelArgs делит текст после открывающей скобки на аргументы верхнего уровня
// до парной закрывающей.
func topLevelArgs(s string) ([]string, error) {
	depth, inStr, start := 0, false, 0
	var out []string
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inStr = false
			}
			continue
		}
		switch c {
		case '\'':
			inStr = true
		case '(':
			depth++
		case ')':
			if depth == 0 {
				out = append(out, s[start:i])
				return out, nil
			}
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return nil, fmt.Errorf("у jsonb_build_object( нет парной закрывающей скобки")
}

// helperIsDomainSnapshot — предпосылка: `AddressPoolDomainPayload(p *domain.AddressPool)`
// есть ровно `return DomainToMap(p)`.
func helperIsDomainSnapshot(src string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "payloads.go", src, 0)
	if err != nil {
		return err
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != seedPoolHelperFunc {
			continue
		}
		params := fn.Type.Params.List
		if len(params) != 1 || len(params[0].Names) != 1 {
			return fmt.Errorf("%s: ожидался один параметр *domain.%s", seedPoolHelperFunc, seedPoolDomainType)
		}
		star, ok := params[0].Type.(*ast.StarExpr)
		if !ok {
			return fmt.Errorf("%s: параметр не указатель на domain.%s", seedPoolHelperFunc, seedPoolDomainType)
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != seedPoolDomainType {
			return fmt.Errorf("%s: параметр не *domain.%s", seedPoolHelperFunc, seedPoolDomainType)
		}
		if len(fn.Body.List) != 1 {
			return fmt.Errorf("%s: тело не из одного return — ключи снимка из структуры не выводятся", seedPoolHelperFunc)
		}
		ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return fmt.Errorf("%s: тело не `return %s(p)`", seedPoolHelperFunc, seedPoolHelperInner)
		}
		callExpr, ok := ret.Results[0].(*ast.CallExpr)
		if !ok || len(callExpr.Args) != 1 {
			return fmt.Errorf("%s: тело не `return %s(p)`", seedPoolHelperFunc, seedPoolHelperInner)
		}
		id, ok := callExpr.Fun.(*ast.Ident)
		arg, ok2 := callExpr.Args[0].(*ast.Ident)
		if !ok || !ok2 || id.Name != seedPoolHelperInner || arg.Name != params[0].Names[0].Name {
			return fmt.Errorf("%s: тело не `return %s(p)`", seedPoolHelperFunc, seedPoolHelperInner)
		}
		return nil
	}
	return fmt.Errorf("функция %s не найдена", seedPoolHelperFunc)
}

// readGoDir склеивает непробные .go файлы каталога в отдельные исходники.
func readGoDir(dir string) (map[string]string, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	out := map[string]string{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, 0, err
		}
		out[n] = string(b)
	}
	if len(out) == 0 {
		return nil, 0, fmt.Errorf("в каталоге нет ни одного .go файла — обход пуст")
	}
	return out, len(out), nil
}

// structJSONKeys — ключи json.Marshal для структуры typeName по исходникам пакета.
func structJSONKeys(srcs map[string]string, typeName string) ([]string, error) {
	fset := token.NewFileSet()
	var st *ast.StructType
	where := ""
	for name, src := range srcs {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			return nil, err
		}
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if decl.Recv != nil && decl.Name.Name == "MarshalJSON" && seedPoolRecvName(decl.Recv) == typeName {
					return nil, fmt.Errorf("%s: у %s свой MarshalJSON — ключи снимка из полей не выводятся", name, typeName)
				}
			case *ast.GenDecl:
				for _, sp := range decl.Specs {
					ts, ok := sp.(*ast.TypeSpec)
					if !ok || ts.Name.Name != typeName {
						continue
					}
					s, ok := ts.Type.(*ast.StructType)
					if !ok {
						return nil, fmt.Errorf("%s: %s не структура", name, typeName)
					}
					st, where = s, name
				}
			}
		}
	}
	if st == nil {
		return nil, fmt.Errorf("тип %s не найден", typeName)
	}
	var keys []string
	for _, fld := range st.Fields.List {
		if len(fld.Names) == 0 {
			return nil, fmt.Errorf("%s: встроенное поле в %s — ключи снимка из полей не выводятся", where, typeName)
		}
		tag := ""
		if fld.Tag != nil {
			if u, err := strconv.Unquote(fld.Tag.Value); err == nil {
				tag = reflect.StructTag(u).Get("json")
			}
		}
		for _, n := range fld.Names {
			if !n.IsExported() {
				continue
			}
			key := n.Name
			if tag != "" {
				name := strings.Split(tag, ",")[0]
				if name == "-" && !strings.Contains(tag, ",") {
					continue
				}
				if name != "" {
					key = name
				}
			}
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s: у %s ноль экспортируемых полей — сверять не с чем", where, typeName)
	}
	return keys, nil
}

func seedPoolRecvName(r *ast.FieldList) string {
	if r == nil || len(r.List) == 0 {
		return ""
	}
	t := r.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// ---- инъекция в обе стороны ----

const seedPoolDomainTwin = `package domain
type AddressPool struct {
	ID          string
	Name        RcNameVPC
	hidden      int
	ZoneID      string ` + "`json:\"zone\"`" + `
	Skipped     bool   ` + "`json:\"-\"`" + `
}
`

func seedPoolSQLTwin(extra string) string {
	return `-- комментарий законно называет 'actor' и jsonb_build_object('Gone', 1)
WITH ins AS (SELECT 1)
INSERT INTO vpc_outbox (payload)
SELECT jsonb_build_object(
    'ID', i.id,
    /* блочный комментарий 'Old' */
    'Name', coalesce(i.name, ''),
    'zone', to_jsonb(ARRAY['a,b'])` + extra + `)
  FROM ins i;`
}

// TestSeedAddressPoolGateRedOnExtraSeedKey — посев несёт ключ, которого нет у пути
// записи: гейт называет ключ и сторону.
func TestSeedAddressPoolGateRedOnExtraSeedKey(t *testing.T) {
	t.Parallel()
	domainKeys, err := structJSONKeys(map[string]string{"d.go": seedPoolDomainTwin}, "AddressPool")
	if err != nil {
		t.Fatal(err)
	}
	seedKeys, err := seedPayloadKeys(seedPoolSQLTwin(",\n    'actor', 'stand_seed:x'"))
	if err != nil {
		t.Fatal(err)
	}
	msg := keyParityFinding(seedKeys, domainKeys)
	if !strings.Contains(msg, "только у посева") || !strings.Contains(msg, "actor") {
		t.Fatalf("лишний ключ посева 'actor' не назван: %q", msg)
	}
}

// TestSeedAddressPoolGateRedOnMissingSeedKey — у посева нет ключа пути записи.
func TestSeedAddressPoolGateRedOnMissingSeedKey(t *testing.T) {
	t.Parallel()
	domainKeys, _ := structJSONKeys(map[string]string{"d.go": seedPoolDomainTwin}, "AddressPool")
	seedKeys, err := seedPayloadKeys(`SELECT jsonb_build_object('ID', 1, 'Name', 2)`)
	if err != nil {
		t.Fatal(err)
	}
	msg := keyParityFinding(seedKeys, domainKeys)
	if !strings.Contains(msg, "только у пути записи") || !strings.Contains(msg, "zone") {
		t.Fatalf("недостающий ключ 'zone' не назван: %q", msg)
	}
}

// TestSeedAddressPoolGateSilentOnLawfulTwin — законный близнец той же формы:
// ключи совпадают, комментарии называют посторонние ключи, тег `json`
// переименовывает и снимает поле, неэкспортируемое поле не участвует.
func TestSeedAddressPoolGateSilentOnLawfulTwin(t *testing.T) {
	t.Parallel()
	domainKeys, err := structJSONKeys(map[string]string{"d.go": seedPoolDomainTwin}, "AddressPool")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(domainKeys, ","); got != "ID,Name,zone" {
		t.Fatalf("ключи структуры выведены неверно: %s", got)
	}
	seedKeys, err := seedPayloadKeys(seedPoolSQLTwin(""))
	if err != nil {
		t.Fatal(err)
	}
	if msg := keyParityFinding(seedKeys, domainKeys); msg != "" {
		t.Fatalf("гейт нашёл расхождение на законном близнеце: %s", msg)
	}
}

// TestSeedAddressPoolGateRefusesUnknownForms — формы, из которых ключи не выводятся:
// отказ, а не молчание.
func TestSeedAddressPoolGateRefusesUnknownForms(t *testing.T) {
	t.Parallel()
	for name, sql := range map[string]string{
		"нет вызова":     `INSERT INTO vpc_outbox (payload) SELECT kacho_vpc.address_pool_state(i) FROM ins i`,
		"два вызова":     `SELECT jsonb_build_object('a', 1), jsonb_build_object('b', 2)`,
		"ключ выражение": `SELECT jsonb_build_object(k, 1)`,
		"нечётно":        `SELECT jsonb_build_object('a', 1, 'b')`,
	} {
		if _, err := seedPayloadKeys(sql); err == nil {
			t.Errorf("%s: форма принята молча", name)
		}
	}
	for name, src := range map[string]string{
		"встраивание": "package domain\ntype AddressPool struct { Base\n ID string }",
		"MarshalJSON": "package domain\ntype AddressPool struct { ID string }\nfunc (p AddressPool) MarshalJSON() ([]byte, error) { return nil, nil }",
	} {
		if _, err := structJSONKeys(map[string]string{"d.go": src}, "AddressPool"); err == nil {
			t.Errorf("%s: форма структуры принята молча", name)
		}
	}
	if err := helperIsDomainSnapshot("package helpers\nfunc AddressPoolDomainPayload(p *domain.AddressPool) map[string]any { m := DomainToMap(p); m[\"x\"] = 1; return m }"); err == nil {
		t.Errorf("помощник с добавленным ключом принят за чистый снимок")
	}
	if err := helperIsDomainSnapshot("package helpers\nfunc AddressPoolDomainPayload(p *domain.AddressPool) map[string]any { return DomainToMap(p) }"); err != nil {
		t.Errorf("законный помощник отвергнут: %v", err)
	}
}
