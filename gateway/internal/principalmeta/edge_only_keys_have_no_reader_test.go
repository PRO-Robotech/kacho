// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta_test

// edge_only_keys_have_no_reader_test.go — у ключа, объявленного остающимся на
// краю, НЕТ читателя за краем (#1252).
//
// ЗАЧЕМ ЭТО ГЕЙТ, А НЕ ЗАМЕР В ОТЧЁТЕ. Решение «остаются на краю» опирается на
// факт о дереве: доводы условия модели прав не читает ни один сервис. Факт
// верен сегодня и меняется молча — а в тот день, когда сервис начнёт читать
// такой ключ, он окажется входом решения, ПРИЕХАВШИМ БЕЗ КОНТРАКТА переданной
// личности: без мостовой формы у ключа нет производителя за краем, значит
// читатель получал бы либо пустоту (контроль, не отказавший ни разу), либо
// значение, которое туда положил кто-то другой.
//
// Правило `security.md` §«Кто вправе ГОВОРИТЬ ЗА пользователя» связывает всякого,
// кто расширяет контракт переданной личности: сужение круга законных
// отправителей, страж старта, самоотчёт. Этот гейт держит границу, при которой
// расширение контракта НЕ ПРОИСХОДИТ: ключ остаётся внутренним делом края.
// Появится читатель — гейт назовёт его файлом и строкой, и тогда пройти по всем
// требованиям придётся осознанно, а не задним числом.
//
// ЧИТАЕТ УЗЛЫ, А НЕ ТЕКСТ. Имя ключа законно встречается в комментариях соседних
// подсистем (внутренний пол уровня уверенности объясняет, откуда берёт свой), и
// поиск по подстроке краснел бы на чужом объяснении. Гейт судит по строковому
// литералу разобранного дерева.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// edgeOnlyKeyNames — ключи, чьё отсутствие за краем утверждается.
//
// Перечень ведётся здесь, а предикат — в пакете: расхождение между ними ловится
// сразу (запись, для которой IsEdgeOnlyKey ложен, роняет пробу соседнего файла),
// а обход дерева получает конкретные имена для сравнения.
func edgeOnlyKeyNames() []string {
	return []string{principalmeta.MetaTokenAMR, principalmeta.MetaTokenMfaAt}
}

func gateRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller не ответил — корень дерева не найти")
	dir := filepath.Dir(file)
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("go.mod не найден обходом вверх от %s", file)
	return ""
}

func TestEdgeOnlyKeys_HaveNoReaderOutsideTheEdge(t *testing.T) {
	root := gateRepoRoot(t)
	keys := edgeOnlyKeyNames()
	require.NotEmpty(t, keys, "перечень ключей края пуст — гейту нечего осматривать")

	var filesRead, literalsSeen int
	var findings []string
	for _, sub := range []string{"services", "pkg"} {
		base := filepath.Join(root, sub)
		require.DirExists(t, base, "каталог %s отсутствует — обход был бы пустым и гейт зелёным ни о чём", sub)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Сгенерённые стабы контрактов руками не правятся и читателем
				// стать не могут; их разбор стоил бы дороже всего остального.
				if d.Name() == "api" && filepath.Dir(path) == filepath.Join(root, "pkg") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return nil // не наш предмет: несобирающийся файл поймает сборка
			}
			filesRead++
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				literalsSeen++
				v, uerr := strconv.Unquote(lit.Value)
				if uerr != nil {
					return true
				}
				for _, k := range keys {
					if strings.EqualFold(strings.TrimSpace(v), k) {
						rel, _ := filepath.Rel(root, path)
						findings = append(findings,
							rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line)+" — "+k)
					}
				}
				return true
			})
			return nil
		})
		require.NoError(t, err, "обход %s прерван", sub)
	}

	require.NotZero(t, filesRead, "прочитано ноль файлов Go — «ноль находок» здесь означало бы "+
		"«ноль прочитанного», и гейт был бы зелёным ни о чём")
	require.NotZero(t, literalsSeen, "осмотрено ноль строковых литералов — разбор сломан")
	t.Logf("перепись: файлов Go прочитано %d · строковых литералов осмотрено %d · ключей края %d",
		filesRead, literalsSeen, len(keys))

	for _, f := range findings {
		t.Errorf("%s: ключ объявлен остающимся на краю, а за краем у него появился читатель. "+
			"Мостовой формы у ключа нет, значит производителя за краем тоже нет — читатель получит "+
			"пустоту либо чужое значение. Решите осознанно: завести мостовую форму и пройти по "+
			"требованиям §«Кто вправе ГОВОРИТЬ ЗА пользователя» (сужение круга законных "+
			"отправителей, отказ старта при пустом круге, измерение в самоотчёте) — либо снять "+
			"читателя.", f)
	}
}
