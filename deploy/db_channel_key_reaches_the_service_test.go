// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// db_channel_key_reaches_the_service_test.go — величина, которой профиль
// объявляет шифрование канала к базе, обязана ДОЕХАТЬ до строки подключения.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Между профилем и строкой, уходящей в пул, стоят ТРИ переименовываемых имени:
//
//	values-ключ чарта      .Values.config.repository.postgres.sslMode
//	  ↓ шаблон config.yaml
//	ключ файла настроек    repository.postgres.ssl-mode
//	  ↓ теги mapstructure
//	поле структуры Config  Repository.Postgres.SSLMode
//	  ↓ сборщик строки
//	sslmode=<mode> в DSN
//
// Разрыв на ЛЮБОМ из трёх переходов возвращает величину к умолчанию `disable`
// (RegisterDefaults), то есть к ОТКРЫТОМУ каналу — и делает это МОЛЧА: випер не
// знает ключа, которого нет в структуре, и отбрасывает его без единого слова.
//
// Три соседние проверки этого не видят, и это измерено, а не предположено:
//
//   - dbtls_declaration_test.go судит ПРОФИЛЬ (объявлен ли `sslMode` в цепочке
//     `-f` каждого боевого стенда) — он зелен, пока профиль объявляет, чем бы
//     это объявление ни кончилось дальше;
//   - service_config_sections_test.go судит ВЕРХНЕУРОВНЕВЫЕ секции — секция
//     `repository` у службы есть, поэтому переименование ЛИСТОВОГО ключа
//     проходит мимо него by construction;
//   - страж посадки службы (`servicecontract`, ось DBSSLMode) судит структуру,
//     СОБРАННУЮ В ПАМЯТИ пробой, — он не знает, каким ключом её наполняет чарт.
//
// Ровно этот класс подозревала задача продукта #2154: «имя ключа значений
// разошлось с тем, что читает служба · путь чтения в конфиге службы после
// переименования». Держателя у него не было.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседей posture_parity_test.go, dbtls_declaration_test.go
// и service_config_sections_test.go: проверке не нужны ни `helm`, ни скачанные
// зависимости чартов, поэтому она НЕ УМЕЕТ пропуститься. Рендер тут и не помог
// бы: значение, приехавшее из умолчания чарта, в манифесте выглядит ровно так
// же, как объявленное, а ключ, который випер отбросил, в манифесте ЕСТЬ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Судятся ТОЛЬКО чарты, рендерящие файл настроек НАШЕЙ части продукта.
//     Часть, чей DSN приезжает одной строкой вместе с `sslmode` внутри (nlb),
//     сюда не попадает: у неё нет отдельного ключа канала, и её судит
//     dbtls_declaration_test.go §InlineDSN. Перепись называет обе величины.
//   - Судится ДОСТИЖИМОСТЬ поля, а не ЗНАЧЕНИЕ: какой режим объявлен на каком
//     стенде — предмет dbtls_declaration_test.go, и второй редакции у него тут
//     быть не должно.
//   - Ось «сборщик строки читает это поле» судится там, где сборщик живёт в
//     пакете настроек службы (его признак — литерал `sslmode=` в теле функции).
//     Где сборщика в этом пакете нет, ось названа в переписи отдельным числом,
//     а не пропущена молча.
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
	"github.com/PRO-Robotech/kacho/internal/servicelayout"
)

// ─────────────────────────────────────────────────────────────────────────────
// Чистые предикаты. Отделены от обхода дерева, чтобы их способность упасть
// показывалась инъекцией синтетики (db_channel_key_reaches_the_service_injection_test.go),
// а не тем, что когда-то падало на дереве.

// configKeyRef — скалярный ключ блока `config.yaml` вместе с его путём.
type configKeyRef struct {
	// path — путь ключа от корня блока: ["repository","postgres","ssl-mode"].
	path []string
	// line — номер строки внутри блока (1-based): координата для читателя.
	line int
	// value — то, что стоит справа от двоеточия (выражение helm либо литерал).
	value string
}

func (r configKeyRef) dotted() string { return strings.Join(r.path, ".") }

var (
	// reCfgKeyLine — скалярный ключ блока настроек. Список (`- name:`) и
	// комментарий сюда не попадают: у первого перед ключом стоит дефис, второй
	// начинается с решётки.
	reCfgKeyLine = regexp.MustCompile(`^([a-z][a-z0-9._-]*):(.*)$`)
	// reDBChannelValue — подстановка ручки режима шифрования канала к базе.
	// Ключ файла настроек здесь НЕ фиксируется: судится то, ЧЕМ значение
	// подставлено, а не то, как названа строка, — иначе переименование ключа
	// вывело бы его из-под наблюдения, то есть ровно то, что проверка ловит.
	reDBChannelValue = regexp.MustCompile(`(?i)\.Values\.[a-z0-9_.]*sslmode`)
)

// configBlockLines — строки блока `config.yaml: |` и его базовый отступ.
//
// Третий возврат — нашёлся ли блок вообще: «ноль ключей» обязано быть отличимо
// от «блока нет», иначе шаблон, переставший рендерить настройки, читался бы как
// шаблон без находок.
func configBlockLines(manifest string) (lines []string, base int, ok bool) {
	all := strings.Split(manifest, "\n")
	start, baseIndent := -1, 0
	for i, ln := range all {
		trimmed := strings.TrimSpace(ln)
		if trimmed != "config.yaml: |" && trimmed != "config.yaml: |-" {
			continue
		}
		start = i + 1
		baseIndent = len(ln) - len(strings.TrimLeft(ln, " ")) + 2
		break
	}
	if start < 0 {
		return nil, 0, false
	}
	for _, ln := range all[start:] {
		if strings.TrimSpace(ln) == "" {
			lines = append(lines, ln)
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		if indent < baseIndent {
			break
		}
		lines = append(lines, ln)
	}
	return lines, baseIndent, true
}

// configKeyRefs — все скалярные ключи блока настроек, каждый со своим путём.
//
// Путь ведётся стеком отступов: ключ без значения открывает уровень, ключ со
// значением — лист. Выражения helm (`{{- if … }}`), комментарии и элементы
// списков уровня не меняют: они не ключи.
func configKeyRefs(lines []string, base int) []configKeyRef {
	type frame struct {
		indent int
		key    string
	}
	var stack []frame
	var out []configKeyRef
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "{{") ||
			strings.HasPrefix(trimmed, "-") {
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		if indent < base {
			continue
		}
		m := reCfgKeyLine.FindStringSubmatch(ln[indent:])
		if m == nil {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		key, value := m[1], strings.TrimSpace(m[2])
		if value == "" {
			stack = append(stack, frame{indent: indent, key: key})
			continue
		}
		path := make([]string, 0, len(stack)+1)
		for _, f := range stack {
			path = append(path, f.key)
		}
		path = append(path, key)
		out = append(out, configKeyRef{path: path, line: i + 1, value: value})
	}
	return out
}

// dbChannelKeys — ключи, чьё значение подставлено из ручки режима шифрования
// канала к базе.
func dbChannelKeys(refs []configKeyRef) []configKeyRef {
	var out []configKeyRef
	for _, r := range refs {
		if reDBChannelValue.MatchString(r.value) {
			out = append(out, r)
		}
	}
	return out
}

// structField — поле структуры: его имя в Go и имя его типа.
type structField struct {
	name     string
	typeName string
}

// tagIndex — теги mapstructure всех структур пакета: тип → тег → поле.
type tagIndex map[string]map[string]structField

// resolveTagPath — резолвит путь ключей по тегам mapstructure от корневого типа.
//
// Возвращает поле, до которого путь дошёл, и ГЛУБИНУ, на которой он оборвался
// (len(path) при полном резолве). Глубина — это координата для читателя: она
// называет ИМЕННО ТОТ сегмент, которого структура не знает, а не «путь не
// найден» целиком.
func resolveTagPath(idx tagIndex, root string, path []string) (fld structField, depth int, ok bool) {
	cur := root
	for i, seg := range path {
		fields, known := idx[cur]
		if !known {
			return structField{}, i, false
		}
		f, found := fields[seg]
		if !found {
			return structField{}, i, false
		}
		if i == len(path)-1 {
			return f, len(path), true
		}
		cur = f.typeName
	}
	return structField{}, 0, false
}

// ─────────────────────────────────────────────────────────────────────────────
// Чтение дерева.

// exprTypeName — имя типа выражения. Чужой пакет и составные типы дают пустую
// строку: спуск по ним всё равно невозможен, и молчаливо продолжать нельзя.
func exprTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprTypeName(t.X)
	}
	return ""
}

// configPackageTags — теги mapstructure всех структур пакета настроек службы.
func configPackageTags(t *testing.T, dir string) (tagIndex, []*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, filepath.Join(repoRoot, dir), func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разобрать пакет настроек %s: %v", dir, err)
	}
	idx := tagIndex{}
	var files []*ast.File
	for _, pkg := range pkgs {
		names := make([]string, 0, len(pkg.Files))
		for n := range pkg.Files {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			f := pkg.Files[n]
			files = append(files, f)
			ast.Inspect(f, func(node ast.Node) bool {
				ts, isType := node.(*ast.TypeSpec)
				if !isType {
					return true
				}
				st, isStruct := ts.Type.(*ast.StructType)
				if !isStruct {
					return true
				}
				fields := idx[ts.Name.Name]
				if fields == nil {
					fields = map[string]structField{}
					idx[ts.Name.Name] = fields
				}
				for _, fld := range st.Fields.List {
					if fld.Tag == nil || len(fld.Names) == 0 {
						continue
					}
					raw, uerr := strconv.Unquote(fld.Tag.Value)
					if uerr != nil {
						continue
					}
					tag := reflect.StructTag(raw).Get("mapstructure")
					if tag == "" || tag == "-" {
						continue
					}
					fields[strings.Split(tag, ",")[0]] = structField{
						name:     fld.Names[0].Name,
						typeName: exprTypeName(fld.Type),
					}
				}
				return false
			})
		}
	}
	return idx, files
}

// dsnComposerReads — читает ли поле сборщик строки подключения этого пакета.
//
// Сборщик опознаётся ЛИТЕРАЛОМ `sslmode=`, который он дописывает к строке, а не
// именем функции: имя переименовывается, а параметр libpq — часть контракта
// драйвера. Второй возврат — нашёлся ли сборщик вообще: «поле не читается»
// обязано быть отличимо от «сборщика в этом пакете нет».
func dsnComposerReads(files []*ast.File, field string) (reads bool, composerFound bool) {
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}
			hasLiteral := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, isLit := n.(*ast.BasicLit)
				if isLit && strings.Contains(lit.Value, "sslmode=") {
					hasLiteral = true
				}
				return true
			})
			if !hasLiteral {
				continue
			}
			composerFound = true
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, isSel := n.(*ast.SelectorExpr)
				if isSel && sel.Sel != nil && sel.Sel.Name == field {
					reads = true
				}
				return true
			})
			if reads {
				return true, true
			}
		}
	}
	return reads, composerFound
}

// configFileMountsInTree — пути файлов настроек НАШИХ частей, объявленные
// шаблонами дерева, вместе с чартом, который их объявляет.
//
// Признак «наша часть» — членство имени в объявленном словаре имён продукта
// (`internal/productnaming`), а НЕ приставка бренда: приставка перестала быть
// признаком в тот день, когда служба доступа назвала себя Kaname, и
// распознаватель по приставке чужое имя не отвергает — он его НЕ ВИДИТ.
func configFileMountsInTree(t *testing.T) map[string]string {
	t.Helper()
	re := regexp.MustCompile(`/etc/([a-z0-9][a-z0-9-]*)/config\.yaml`)
	out := map[string]string{}
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "out", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if n := d.Name(); !strings.HasSuffix(n, ".yaml") && !strings.HasSuffix(n, ".yml") {
			return nil
		}
		rel, rerr := filepath.Rel(repoRoot, path)
		if rerr != nil || !strings.Contains(filepath.ToSlash(rel), "/templates/") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, m := range re.FindAllStringSubmatch(string(body), -1) {
			svcDir, mine := productnaming.ServiceDir(m[1])
			if !mine {
				continue
			}
			chartDir := filepath.ToSlash(filepath.Dir(filepath.Dir(rel)))
			if _, seen := out[svcDir]; !seen {
				out[svcDir] = chartDir
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Проверка дерева.

// TestConfigPopulationCoversEveryPartThatMountsItsSettings — предпосылка обеих
// проверок семейства: часть продукта, монтирующая файл настроек, обязана попасть
// в популяцию.
//
// Отдельным тестом, а не строкой внутри соседа, потому что предмет у него свой:
// сосед судит СОДЕРЖИМОЕ настроек, а этот — что судить есть что. Популяция,
// сузившаяся молча, даёт соседу честный зелёный по пустому месту — ровно то
// «ноль находок против ноль прочитанного», которое корпус требует различать.
func TestConfigPopulationCoversEveryPartThatMountsItsSettings(t *testing.T) {
	inTree := configFileMountsInTree(t)
	if len(inTree) == 0 {
		t.Fatal("в дереве не найдено ни одного файла настроек нашей части — обход прочитал " +
			"ноль, и «ноль находок» здесь означало бы «ноль осмотренного»")
	}
	charts, chartsSeen := serviceConfigCharts(t)
	// ЕДИНИЦА СЧЁТА ЗДЕСЬ — ЧАСТЬ, А НЕ ЧАРТ, и это не педантизм: одну часть
	// продукта описывают ДВА чарта (её собственный и её же подчарт в умбрелле),
	// поэтому счёт чартов дал бы «в популяции 4 из 3» — число, которое читателю
	// нечем истолковать. Оба числа названы, каждое своей единицей.
	covered := map[string]bool{}
	for _, c := range charts {
		covered[c.service] = true
	}

	missing := make([]string, 0, len(inTree))
	for svc := range inTree {
		if !covered[svc] {
			missing = append(missing, svc)
		}
	}
	sort.Strings(missing)
	for _, svc := range missing {
		t.Errorf("часть %q монтирует свой файл настроек (чарт %s), а популяция проверок "+
			"семейства её НЕ ВИДИТ — значит их «ноль находок» о ней не означает ничего.\n"+
			"    Отбор популяции обязан браться из объявленного словаря имён продукта "+
			"(internal/productnaming), а не из литерала приставки: приставка перестала "+
			"быть признаком части в тот день, когда служба доступа назвала себя своим "+
			"именем, и распознаватель по ней чужое имя не отвергает — он его не видит.",
			svc, inTree[svc])
	}

	t.Logf("осмотрено: чартов в дереве %d; частей, монтирующих файл настроек %d; "+
		"из них в популяции %d; вне популяции %d; чартов в популяции %d",
		chartsSeen, len(inTree), len(covered), len(missing), len(charts))
	names := make([]string, 0, len(inTree))
	for svc, chart := range inTree {
		names = append(names, fmt.Sprintf("%s→%s", svc, chart))
	}
	sort.Strings(names)
	t.Logf("  части с файлом настроек: %s", strings.Join(names, " "))
}

// TestDBChannelKeyResolvesToTheFieldTheDSNReads — ключ, которым чарт объявляет
// шифрование канала к базе, обязан доехать до строки подключения.
func TestDBChannelKeyResolvesToTheFieldTheDSNReads(t *testing.T) {
	charts, chartsSeen := serviceConfigCharts(t)
	if chartsSeen == 0 {
		t.Fatal("в дереве не найдено ни одного Chart.yaml — обход прочитал ноль, " +
			"и «ноль находок» здесь означало бы «ноль осмотренного»")
	}
	if len(charts) == 0 {
		t.Fatalf("осмотрено чартов: %d, из них рендерят конфигурацию нашего сервиса: 0 — "+
			"предпосылка проверки исчезла (сменилась форма пути конфигурации?)", chartsSeen)
	}

	keysRead, withChannelKey, composerJudged, findings := 0, 0, 0, 0
	var withoutChannelKey []string
	for _, c := range charts {
		body := readFile(t, c.configMapFile)
		lines, base, ok := configBlockLines(body)
		if !ok {
			t.Errorf("%s: блок `config.yaml: |` не найден — судить нечего, "+
				"а связь до этого шаблона уже прослежена", c.configMapFile)
			continue
		}
		refs := configKeyRefs(lines, base)
		keysRead += len(refs)
		channel := dbChannelKeys(refs)
		if len(channel) == 0 {
			withoutChannelKey = append(withoutChannelKey, c.service)
			continue
		}
		withChannelKey++

		cfgDir := filepath.Join("services", c.service, "internal", "apps",
			servicelayout.UseCaseSegment(c.service), "config")
		idx, files := configPackageTags(t, cfgDir)
		if len(idx) == 0 {
			t.Errorf("%s: в пакете настроек %s не прочитано ни одной структуры с тегами "+
				"mapstructure — сравнивать не с чем", c.chartDir, cfgDir)
			continue
		}

		for _, key := range channel {
			fld, depth, resolved := resolveTagPath(idx, "Config", key.path)
			if !resolved {
				findings++
				t.Errorf("%s (строка блока %d): ключ %q объявляет режим шифрования канала "+
					"к базе, а структура Config службы %s его не знает — путь оборвался на "+
					"сегменте %q (%s).\n"+
					"    Випер отбросит этот ключ МОЛЧА, значение вернётся к умолчанию "+
					"`disable`, и канал к базе станет открытым при зелёном рендере и "+
					"зелёном профиле. Исходов три: (а) служба начинает читать этот ключ; "+
					"(б) шаблон переходит на ключ, который служба читает; (в) ключ "+
					"снимается с контракта чарта. Молча принять и выбросить исходом не "+
					"является.",
					c.configMapFile, key.line, key.dotted(), c.service,
					key.path[min(depth, len(key.path)-1)], cfgDir)
				continue
			}
			reads, composerFound := dsnComposerReads(files, fld.name)
			if !composerFound {
				continue
			}
			composerJudged++
			if !reads {
				findings++
				t.Errorf("%s (строка блока %d): ключ %q доезжает до поля %s, а сборщик "+
					"строки подключения в %s его НЕ ЧИТАЕТ — величина живёт в структуре и "+
					"не попадает в DSN, то есть объявлена и не действует.",
					c.configMapFile, key.line, key.dotted(), fld.name, cfgDir)
			}
		}
	}

	sort.Strings(withoutChannelKey)
	t.Logf("осмотрено: чартов в дереве %d, из них с конфигурацией сервиса %d; "+
		"прочитано ключей настроек %d; из них объявляют канал к базе %d; "+
		"осей «сборщик читает поле» судимо %d; находок %d",
		chartsSeen, len(charts), keysRead, withChannelKey, composerJudged, findings)
	if len(withoutChannelKey) > 0 {
		t.Logf("  отдельного ключа канала не объявляют (их DSN приезжает строкой целиком, "+
			"и его судит dbtls_declaration_test.go §InlineDSN): %s",
			strings.Join(withoutChannelKey, " "))
	}
}
