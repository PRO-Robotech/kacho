// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// hideexistenceparity_test.go — два места, отказывающие в чтении чужого объекта,
// обязаны говорить одним голосом.
//
// # Почему страж живёт ЗДЕСЬ, а не в фундаменте (задача #2532, класс 2)
//
// Прежде он лежал в `pkg/authz` тремя файлами и читал каталог края по
// относительному пути `../../gateway/internal/middleware`. Предмет у него —
// ПАРИТЕТ ДВУХ ДЕРЕВЬЕВ, и одно из них принадлежит платформе; фундамент, уехав в
// свой репозиторий, перестаёт видеть край вовсе, и страж отвечал бы «пакет не
// прочитан (прочитано 0 файлов)» — «не выполнилось», поданное как красное.
// Измерено на собранном фундаменте, а не предположено.
//
// Свойство при переезде НЕ ослаблено, а усилено: сторон стало три.
//
//	объявление фундамента   разбирается тем же разбором, что и объявление края
//	объявление края         как прежде
//	ЖИВОЕ значение          `authz.OwnerNotFoundFormat` на каждом разобранном ключе
//
// Третья сторона заведена ровно потому, что переезд её потребовал: прежде наша
// половина бралась значением карты напрямую (страж жил в её пакете), и разбор
// исходника без этой сверки мог бы разойтись с тем, что собралось. Теперь
// расхождение разбора с значением — своя находка.
//
// # Что осталось в фундаменте
//
// `TestHideExistenceMessage_ShapeOfTheFallback` — он о СВОЕЙ функции и ни одного
// чужого дерева не читает. Переезжать ему некуда и незачем.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// Пакеты, чьи объявления сверяются. Координата ПАКЕТА, а не файла: файл внутри
// пакета переносится свободно, пакет — нет (задача #1946).
const (
	gatewayTableDir    = "gateway/internal/middleware"
	foundationTableDir = "pkg/authz"
)

// parseTableInPackage — записи объявления в названном пакете дерева.
func parseTableInPackage(t *testing.T, root, rel string) map[string]string {
	t.Helper()
	formats, read, err := parseFormatsInPackage(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("таблица скрытия существования в пакете %s не разрешена: %v", rel, err)
	}
	t.Logf("перепись: пакет %s — файлов прочитано %d · записей таблицы %d", rel, read, len(formats))
	return formats
}

// TestHideExistenceFormats_MatchTheGateway — те же ключи, те же тексты, и оба
// объявления сходятся с живым значением.
func TestHideExistenceFormats_MatchTheGateway(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	gateway := parseTableInPackage(t, root, gatewayTableDir)
	ours := parseTableInPackage(t, root, foundationTableDir)

	for objectType, gatewayText := range gateway {
		ourText, ok := ours[objectType]
		if !ok {
			t.Errorf("тип %q край отказывает текстом владельца, а перехватчик падает на нейтральный — "+
				"два ответа различимы, и различие есть оракул существования", objectType)
			continue
		}
		if ourText != gatewayText {
			t.Errorf("тип %q: край отвечает %q, перехватчик %q — один из них больше не совпадает "+
				"с промахом владельца", objectType, gatewayText, ourText)
		}
	}
	for objectType, ourText := range ours {
		gatewayText, ok := gateway[objectType]
		if !ok {
			t.Errorf("тип %q известен перехватчику (%q) и неизвестен краю — отказ края по нему "+
				"нейтральный и потому отличим", objectType, ourText)
			continue
		}
		if gatewayText != ourText {
			t.Errorf("тип %q: тексты расходятся (перехватчик %q, край %q)", objectType, ourText, gatewayText)
		}
	}

	// Третья сторона: разобранное объявление фундамента обязано совпадать с тем,
	// что отдаёт СОБРАННЫЙ код. Без неё разбор исходника мог бы разойтись с
	// значением, и обе половины паритета сверялись бы с текстом, который не
	// исполняется.
	live := 0
	for objectType, ourText := range ours {
		form, ok := authz.OwnerNotFoundFormat(objectType)
		if !ok {
			t.Errorf("тип %q разобран из объявления фундамента, но живое значение его не знает — "+
				"разбор разошёлся с собранным кодом", objectType)
			continue
		}
		if form != ourText {
			t.Errorf("тип %q: объявление даёт %q, живое значение %q", objectType, ourText, form)
			continue
		}
		live++
	}
	if live == 0 {
		t.Fatal("ни один разобранный ключ не сверён с живым значением — третья сторона паритета " +
			"не наблюдала ничего")
	}
	t.Logf("перепись: ключей края %d · фундамента %d · сверено с живым значением %d",
		len(gateway), len(ours), live)
}

// TestHideExistenceParityResolvesByPackage — объявление находится в ЛЮБОМ
// не-тестовом файле пакета, а не только в историческом.
func TestHideExistenceParityResolvesByPackage(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, rel := range []string{gatewayTableDir, foundationTableDir} {
		formats, read, err := parseFormatsInPackage(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("страж не разрешил объявление в пакете %s: %v", rel, err)
		}
		if len(formats) == 0 {
			t.Fatalf("объявление пакета %s разобралось в ноль записей — страж прошёл бы вакуумно", rel)
		}
		t.Logf("перепись: пакет %s — файлов прочитано %d · записей объявления %d", rel, read, len(formats))
	}
}

// TestHideExistenceParityParserCanFailAndStaysSilent — способность упасть и
// смолчать, на СИНТЕТИЧЕСКОМ пакете.
func TestHideExistenceParityParserCanFailAndStaysSilent(t *testing.T) {
	t.Parallel()
	const decl = `package middleware

var hideExistenceNotFoundFormats = map[string]string{
	"vpc_subnet": "Subnet %s not found",
}
`
	cases := []struct {
		name  string
		files map[string]string
		want  string
		why   string
	}{
		{
			name:  "законный близнец: объявление в историческом файле",
			files: map[string]string{"permission_denied_response.go": decl},
			why:   "положительный контроль: без него всякое красное ниже могло бы приходить от самого разбора",
		},
		{
			name: "объявление переехало в соседний файл пакета",
			files: map[string]string{
				"permission_denied_response.go": "package middleware\n\nfunc other() {}\n",
				"hide_existence.go":             decl,
			},
			why: "ровно предмет #1946: перенос внутри пакета — законная правка, и страж обязан " +
				"её пережить, а не отвечать «не выполнилось», поданным как красное",
		},
		{
			name:  "объявления нет вовсе",
			files: map[string]string{"permission_denied_response.go": "package middleware\n\nfunc other() {}\n"},
			want:  "объявление",
			why:   "снятие таблицы края обязано быть находкой: без неё край отвечает нейтральным текстом, различимым от текста владельца",
		},
		{
			name:  "пакета нет",
			files: nil,
			want:  "прочитано",
			why:   "«ноль находок» обязано быть отличимо от «ноль прочитанного»",
		},
		{
			name: "объявлений два",
			files: map[string]string{
				"permission_denied_response.go": decl,
				"hide_existence.go":             decl,
			},
			want: "объявлени",
			why: "два объявления одного имени не собрались бы, но обход по каталогу их видит — " +
				"молчать о них значит выбрать одно наугад",
		},
		{
			name: "объявление в ТЕСТОВОМ файле пакета за объявление не считается",
			files: map[string]string{
				"permission_denied_response.go": "package middleware\n\nfunc other() {}\n",
				"hide_existence_test.go":        decl,
			},
			want: "объявление",
			why: "отрицательный контроль охвата: фикстура пробы — не прод-объявление, и принять её " +
				"за него значит сверять паритет с синтетикой",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "middleware")
			if tc.files != nil {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				for name, body := range tc.files {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			formats, read, err := parseFormatsInPackage(dir)

			if tc.want == "" {
				if err != nil {
					t.Fatalf("страж отказал на законном пакете — первое же ложное срабатывание "+
						"снимает его.\nчто проверялось: %s\nотказ: %v", tc.why, err)
				}
				if len(formats) == 0 || read == 0 {
					t.Fatalf("контроль ничего не доказывает: записей %d · файлов прочитано %d",
						len(formats), read)
				}
				return
			}
			if err == nil {
				t.Fatalf("страж смолчал на инъекции — он НЕ способен упасть по этой оси.\n"+
					"что должно было ловиться: %s", tc.why)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("отказ не о том: ждали %q, получили %v\nчто проверялось: %s", tc.want, err, tc.why)
			}
			if !strings.Contains(err.Error(), "прочитано") {
				t.Errorf("отказ не называет объём прочитанного: %v", err)
			}
		})
	}
}

// parseFormatsInPackage — записи объявления `hideExistenceNotFoundFormats` в
// не-тестовых файлах пакета.
//
// Возвращает ОШИБКУ, а не роняет пробу: разбор, обращающийся к `*testing.T`,
// инъекции не поддаётся — падение подставного пакета уронило бы саму пробу
// способности падать.
//
// Отказов три, и все три — отказы: обход пуст · объявления нет · объявлений два.
// Каждый называет объём прочитанного, потому что «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
func parseFormatsInPackage(dir string) (map[string]string, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("пакет %s не прочитан (прочитано 0 файлов): %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	read := 0
	out := map[string]string{}
	var declaredIn []string
	for _, name := range names {
		f, perr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if perr != nil {
			return nil, read, fmt.Errorf("файл %s пакета не разобран (прочитано %d файлов): %w",
				name, read, perr)
		}
		read++
		if got := formatsFromFile(f); len(got) > 0 {
			declaredIn = append(declaredIn, name)
			for k, v := range got {
				out[k] = v
			}
		}
	}

	switch {
	case read == 0:
		return nil, 0, fmt.Errorf("в пакете %s нет ни одного не-тестового файла Go "+
			"(прочитано 0 файлов) — страж судил бы о непрочитанном", dir)
	case len(declaredIn) == 0:
		return nil, read, fmt.Errorf("объявление hideExistenceNotFoundFormats не найдено ни в одном "+
			"не-тестовом файле пакета %s (прочитано %d файлов) — край отвечает нейтральным текстом, "+
			"различимым от текста владельца, и это оракул существования", dir, read)
	case len(declaredIn) > 1:
		return nil, read, fmt.Errorf("объявлений hideExistenceNotFoundFormats в пакете %s больше одного: %s "+
			"(прочитано %d файлов) — какое из них исполняется, решает сборка, и страж выбрал бы наугад",
			dir, strings.Join(declaredIn, ", "), read)
	case len(out) == 0:
		return nil, read, fmt.Errorf("объявление разобралось в ноль записей (прочитано %d файлов) — "+
			"страж прошёл бы вакуумно", read)
	}
	return out, read, nil
}

// formatsFromFile — записи объявления в одном разобранном файле.
func formatsFromFile(f *ast.File) map[string]string {
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "hideExistenceNotFoundFormats" {
			return true
		}
		if len(vs.Values) != 1 {
			return false
		}
		lit, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			return false
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			k, kok := kv.Key.(*ast.BasicLit)
			v, vok := kv.Value.(*ast.BasicLit)
			if !kok || !vok {
				continue
			}
			key, kerr := strconv.Unquote(k.Value)
			val, verr := strconv.Unquote(v.Value)
			if kerr != nil || verr != nil {
				continue
			}
			out[key] = val
		}
		return false
	})
	return out
}
