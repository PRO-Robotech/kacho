// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// client_token_knobs_of_the_pin_test.go — КОПИЯ ЧАРТА СЛУЖБЫ ДОСТУПА ОБЯЗАНА
// ОТДАВАТЬ КАЖДУЮ ВЕЛИЧИНУ ТОКЕН-ЭНДПОИНТА, КОТОРУЮ ЗНАЕТ ПИНЕННАЯ СЛУЖБА
// (kacho#2896, темп поверхности выдачи kaname#315).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Блок `authn.client-token` копии рендерил четыре величины: перечень адресатов,
// адресат по умолчанию, срок токена, потолок тела. Пин службы `2e1d01af171d`
// несёт ещё шесть — темп поверхности выдачи (kaname#315): потолок одновременных
// обменов, темп обменов на клиента, отказы доказательства на источник и их
// окно, темп и потолок точки авторизации. Страж старта службы требует первые
// четыре из шести при включённом эндпоинте и последние две при собранной
// церемонии (`own` и включённый эндпоинт), умолчаний у них нет. Шаблон их не
// отдавал ни при каких значениях профиля, и служба отказывала в старте на
// каждом стенде с включённым эндпоинтом — на всех восьми красных работах
// головы сборки kacho#2903 одним текстом:
//
//	authn.client-token.exchanges-per-client-per-sec must be declared as a
//	positive number … (got 0) — zero means «no pace»; … in-flight-ceiling …;
//	… failed-proofs-per-source …; … failed-proof-window …;
//	… authorize-per-source-per-sec …; … authorize-in-flight-ceiling …
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЕРЕЧЕНЬ ВЫВОДИТСЯ ИЗ ПИНА
//
// Ведущий перечень — теги `mapstructure` структуры `ClientTokenConfig` у
// пиненного модуля (`go.mod` даёт версию, `go env GOMODCACHE` — каталог, тот же
// резолв, что у own_ceilings_and_access_keys_umbrella_test.go). Выписанный здесь
// перечень был бы вторым местом об одном предмете и разошёлся бы с процессом
// ровно на подъёме пина — так и случилось с перечнем соседней проверки
// объявлений (client_token_declaration_test.go), который знал четыре величины.
// Вывод из ЧАРТА был бы тождественно истинным; вывод из ПРОЦЕССА — нет: чарт,
// не выражающий новую величину пина, краснеет до подъёма стенда.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Она не судит величины: какое число темпа у стенда — решение профиля, и его
// объявленность судит client_token_declaration_test.go. Она утверждает, что
// каждой величине процесса ЕСТЬ ЧЕМ доехать до него из чарта.
package deploy_test

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

// clientTokenConfigType — структура настроек токен-эндпоинта у службы.
const clientTokenConfigType = "ClientTokenConfig"

// clientTokenSwitchKey — ключ включения: не величина эндпоинта, а его условие.
const clientTokenSwitchKey = "enabled"

// clientTokenKnob — величина эндпоинта: ключ настройки службы и вид значения.
type clientTokenKnob struct {
	key  string // ключ настройки службы, kebab-case (`in-flight-ceiling`)
	kind string // "int" · "string" · "duration"
}

// valueKey — имя величины в значениях чарта: тот же ключ в lowerCamelCase.
//
// Правило одно для всех десяти ручек блока (`token-ttl` ↔ `tokenTtl`,
// `body-ceiling` ↔ `bodyCeiling`), поэтому выводится, а не выписывается.
func (k clientTokenKnob) valueKey() string {
	parts := strings.Split(k.key, "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// knobKindOf — вид значения по типу поля. Неизвестный тип — отказ разбора, а не
// пропуск: величина, вид которой разбор не знает, ушла бы из-под наблюдения.
func knobKindOf(e ast.Expr) (string, bool) {
	switch t := e.(type) {
	case *ast.Ident:
		switch t.Name {
		case "int", "int32", "int64":
			return "int", true
		case "string":
			return "string", true
		}
	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok && x.Name == "time" && t.Sel.Name == "Duration" {
			return "duration", true
		}
	}
	return "", false
}

// clientTokenKnobsFromFiles — ядро разбора: величины структуры
// `ClientTokenConfig` в наборе исходников пакета. Вынесено отдельно, чтобы
// самопроверка подала синтетический исходник, а не подделывала кэш модулей.
//
// Отказы разбора называются: структуры нет, поле неизвестного вида, ни одной
// величины — во всех трёх случаях «ноль находок» означал бы «ноль прочитанного».
func clientTokenKnobsFromFiles(files map[string][]byte) ([]clientTokenKnob, error) {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	var (
		knobs []clientTokenKnob
		found bool
	)
	for _, name := range names {
		f, err := parser.ParseFile(token.NewFileSet(), name, files[name], parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("исходник %s не разбирается: %w", name, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != clientTokenConfigType {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					return nil, fmt.Errorf("%s в %s объявлен не структурой — форма сменилась", clientTokenConfigType, name)
				}
				found = true
				for _, fld := range st.Fields.List {
					if fld.Tag == nil || len(fld.Names) == 0 {
						continue
					}
					raw, err := strconv.Unquote(fld.Tag.Value)
					if err != nil {
						return nil, fmt.Errorf("тег поля %s не читается: %w", fld.Names[0].Name, err)
					}
					key := strings.Split(reflect.StructTag(raw).Get("mapstructure"), ",")[0]
					if key == "" || key == "-" || key == clientTokenSwitchKey {
						continue
					}
					kind, known := knobKindOf(fld.Type)
					if !known {
						return nil, fmt.Errorf("поле %s (`%s`) неизвестного разбору вида — допиши вид, "+
							"иначе величина уйдёт из-под наблюдения", fld.Names[0].Name, key)
					}
					for range fld.Names {
						knobs = append(knobs, clientTokenKnob{key: key, kind: kind})
					}
				}
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("структура %s не найдена ни в одном из %d исходников — "+
			"перечень выводить неоткуда", clientTokenConfigType, len(files))
	}
	if len(knobs) == 0 {
		return nil, fmt.Errorf("у структуры %s нет ни одной величины с тегом mapstructure — "+
			"форма объявления сменилась", clientTokenConfigType)
	}
	sort.Slice(knobs, func(i, j int) bool { return knobs[i].key < knobs[j].key })
	return knobs, nil
}

// clientTokenKnobsOfPin — величины эндпоинта у пиненного модуля службы.
func clientTokenKnobsOfPin(t *testing.T, moduleDir string) []clientTokenKnob {
	t.Helper()
	dir := filepath.Join(moduleDir, "internal", "apps", "kaname", "config")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("пакет настроек пиненного модуля не прочитан (%s): %v", dir, err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 -- путь собран из пина go.mod, не из ввода
		if err != nil {
			t.Fatalf("исходник пиненного модуля не прочитан: %v", err)
		}
		files[e.Name()] = body
	}
	knobs, err := clientTokenKnobsFromFiles(files)
	if err != nil {
		t.Fatalf("величины токен-эндпоинта пиненного модуля не выведены: %v", err)
	}
	t.Logf("перепись: исходников пакета настроек пина %d · величин токен-эндпоинта %d", len(files), len(knobs))
	return knobs
}

// clientTokenSample — положительное значение величины для рендера.
func clientTokenSample(kind string) string {
	switch kind {
	case "int":
		return "7"
	case "duration":
		return "7m"
	default:
		return "platform.example.invalid"
	}
}

// TestClientToken_RenderEmitsEveryKnobOfThePin — каждая величина пиненного
// эндпоинта доезжает до карты настроек, когда профиль её объявил; при
// выключенном эндпоинте блока нет вовсе.
func TestClientToken_RenderEmitsEveryKnobOfThePin(t *testing.T) {
	moduleDir := kanameModuleDir(t, "..")
	knobs := clientTokenKnobsOfPin(t, moduleDir)

	sets := []string{"config.authn.identityProvider=own", "config.authn.clientToken.enabled=true"}
	for _, k := range knobs {
		sets = append(sets, "config.authn.clientToken."+k.valueKey()+"="+clientTokenSample(k.kind))
	}
	rendered, err := renderIdentitySubchart(t, nil, sets...)
	if err != nil {
		t.Fatalf("рендер подчарта с включённым эндпоинтом не удался: %v\n%s", err, rendered)
	}
	block, ok := configSection(kanameServiceConfig(t, rendered), "authn", "client-token")
	if !ok {
		t.Fatal("включённый эндпоинт: секции `authn.client-token` в настройках нет вовсе")
	}
	emitted := 0
	for _, k := range knobs {
		v, present := block[k.key]
		switch {
		case !present:
			t.Errorf("величину `authn.client-token.%s` пиненной службы рендер НЕ отдаёт при объявленной "+
				"`config.authn.clientToken.%s` — служба откажет в старте с именем ручки на каждом стенде "+
				"с включённым эндпоинтом", k.key, k.valueKey())
		case v == nil:
			t.Errorf("величина `authn.client-token.%s` отрендерилась пустой при объявленной "+
				"`config.authn.clientToken.%s=%s`", k.key, k.valueKey(), clientTokenSample(k.kind))
		default:
			emitted++
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: выключенный эндпоинт блока не несёт — величины без
	// потребителя не требуются.
	off, err := renderIdentitySubchart(t, nil, "config.authn.identityProvider=own", "config.authn.clientToken.enabled=false")
	if err != nil {
		t.Fatalf("рендер подчарта с выключенным эндпоинтом не удался: %v\n%s", err, off)
	}
	if _, present := configSection(kanameServiceConfig(t, off), "authn", "client-token"); present {
		t.Error("выключенный эндпоинт: секция `authn.client-token` рендерится, хотя эндпоинта нет")
	}

	t.Logf("перепись: пин %s · величин пина %d · отдано рендером %d",
		productModulePins(t, "..")[kanameModulePart], len(knobs), emitted)
}

// TestClientTokenKnobReaderSeesEveryFormAndRefusesTheUnknown — самопроверка
// разбора на синтетике: законный исходник даёт величины с видом, три отказа
// разбора называются, а не молчат.
func TestClientTokenKnobReaderSeesEveryFormAndRefusesTheUnknown(t *testing.T) {
	lawful := []byte(`package config

import "time"

type ClientTokenConfig struct {
	Enabled bool          ` + "`mapstructure:\"enabled\"`" + `
	Audiences string      ` + "`mapstructure:\"allowed-audiences\"`" + `
	TTL time.Duration     ` + "`mapstructure:\"token-ttl\"`" + `
	Ceiling int64         ` + "`mapstructure:\"body-ceiling\"`" + `
	Pace int              ` + "`mapstructure:\"exchanges-per-client-per-sec\"`" + `
	internal string
}
`)
	got, err := clientTokenKnobsFromFiles(map[string][]byte{"client_token.go": lawful})
	if err != nil {
		t.Fatalf("законный исходник обязан разбираться: %v", err)
	}
	want := []clientTokenKnob{
		{"allowed-audiences", "string"},
		{"body-ceiling", "int"},
		{"exchanges-per-client-per-sec", "int"},
		{"token-ttl", "duration"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("разбор законного исходника: получено %v, ожидалось %v", got, want)
	}
	if vk := (clientTokenKnob{key: "exchanges-per-client-per-sec"}).valueKey(); vk != "exchangesPerClientPerSec" {
		t.Errorf("имя величины в значениях чарта: получено %q", vk)
	}

	for name, tc := range map[string]struct {
		src    string
		reason string
	}{
		"структуры нет": {
			src:    "package config\n\ntype Other struct{ A int `mapstructure:\"a\"` }\n",
			reason: "не найдена",
		},
		"поле неизвестного вида": {
			src:    "package config\n\ntype ClientTokenConfig struct{ A []string `mapstructure:\"allowed\"` }\n",
			reason: "неизвестного разбору вида",
		},
		"величин ноль": {
			src:    "package config\n\ntype ClientTokenConfig struct{ Enabled bool `mapstructure:\"enabled\"` }\n",
			reason: "нет ни одной величины",
		},
	} {
		_, err := clientTokenKnobsFromFiles(map[string][]byte{"client_token.go": []byte(tc.src)})
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: разбор обязан отказать с причиной %q, получено %v", name, tc.reason, err)
		}
	}
}
