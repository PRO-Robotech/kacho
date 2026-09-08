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
//
// ФОРМ ЧТЕНИЯ ДВЕ, И РАЗБОР ЗНАЕТ ОБЕ. С тех пор как пространство имён личности
// сведено в ОДНО объявление (`pkg/principalwire`), читатель ключа пишет не
// литерал, а `principalwire.MetaTokenAMR`. Распознаватель, знающий только
// литерал, о таком читателе не сказал бы ни красного, ни зелёного — он молчал
// бы, и молчание читалось бы как «читателя нет». Поэтому здесь опознаются обе
// формы: строковый литерал и обращение к константе пакета-объявления, приведённое
// к ПОЛНОМУ ПУТИ ИМПОРТА (псевдоним пишется так же коротко, и разбор по имени
// пакета обходился бы одной буквой в объявлении импорта).
//
// САМО ОБЪЯВЛЕНИЕ ЧИТАТЕЛЕМ НЕ ЯВЛЯЕТСЯ. Пакет `pkg/principalwire` из обхода
// исключён: он называет имена, а не читает значения, и он лежит в фундаменте
// потому, что край импортирует фундамент, а не наоборот. Запрет объявлять имя
// там, где его можно объявить, означал бы, что объявить его негде.
//
// СОСТАВ БЕРЁТСЯ У ИНДЕКСА, А НЕ У ДИСКА. Обход диском подхватывает то, что на
// машине со стендом лежит рядом с деревом и деревом не является — распакованные
// чарты, сборочные каталоги, отчёты прогонов, — и находка из такого каталога
// говорит о чужой копии, а не о нашем коде. Общий корпус (pkg/treecorpus)
// спрашивает индекс git и потому отвечает про отслеживаемое.

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
	"github.com/PRO-Robotech/kacho/pkg/principalwire"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// edgeOnlyKeyNames — ключи, чьё отсутствие за краем утверждается.
//
// Перечень ведётся здесь, а предикат — в пакете: расхождение между ними ловится
// сразу (запись, для которой IsEdgeOnlyKey ложен, роняет пробу соседнего файла),
// а обход дерева получает конкретные имена для сравнения.
func edgeOnlyKeyNames() []string {
	return []string{
		principalmeta.MetaTokenAMR,
		principalmeta.MetaTokenMfaAt,
		principalmeta.MetaTokenBasicCredentialID,
	}
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

	// Имена констант, под которыми те же ключи читаются ПОСЛЕ сведения
	// объявления в одно. Берутся у каталога, а не выписываются: второй перечень
	// имён разошёлся бы с первым молча — ровно тот класс, который этот гейт и
	// сторожит, только этажом выше.
	edgeOnlyIdents := map[string]string{}
	for _, k := range principalwire.Keys() {
		if k.EdgeOnly && k.Ident != "" {
			edgeOnlyIdents[k.Ident] = k.Meta
		}
	}
	require.NotEmpty(t, edgeOnlyIdents, "каталог не назвал ни одной константы ключа края — "+
		"вторая форма чтения осталась бы невидимой, и гейт молчал бы на ней")

	var filesRead, literalsSeen, selectorsSeen int
	var findings []string
	for _, sub := range []string{"services", "pkg"} {
		files, err := treecorpus.UnderWithSuffix(filepath.Join(root, sub), ".go")
		require.NoError(t, err, "состав %s не получен у индекса — обход был бы пустым "+
			"и гейт зелёным ни о чём", sub)
		require.NotEmpty(t, files, "в %s ноль отслеживаемых файлов Go", sub)
		for _, path := range files {
			rel, rerr := filepath.Rel(root, path)
			require.NoError(t, rerr, "путь %s вне дерева", path)
			rel = filepath.ToSlash(rel)
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			// Сгенерённые стабы контрактов руками не правятся и читателем стать
			// не могут; их разбор стоил бы дороже всего остального.
			if strings.HasPrefix(rel, "pkg/api/") {
				continue
			}
			// Пакет-ОБЪЯВЛЕНИЕ: он называет имена, а не читает значения.
			if strings.HasPrefix(rel, "pkg/principalwire/") {
				continue
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				continue // не наш предмет: несобирающийся файл поймает сборка
			}
			filesRead++
			// Псевдонимы пакета-объявления в ЭТОМ файле: обращение опознаётся по
			// полному пути импорта, а не по имени пакета.
			declAliases := map[string]bool{}
			for _, imp := range f.Imports {
				ip, uerr := strconv.Unquote(imp.Path.Value)
				if uerr != nil || ip != principalwire.ImportPath {
					continue
				}
				name := principalwire.ImportPath[strings.LastIndex(principalwire.ImportPath, "/")+1:]
				if imp.Name != nil {
					if imp.Name.Name == "." || imp.Name.Name == "_" {
						continue
					}
					name = imp.Name.Name
				}
				declAliases[name] = true
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BasicLit:
					if node.Kind != token.STRING {
						return true
					}
					literalsSeen++
					v, uerr := strconv.Unquote(node.Value)
					if uerr != nil {
						return true
					}
					for _, k := range keys {
						if strings.EqualFold(strings.TrimSpace(v), k) {
							findings = append(findings,
								rel+":"+strconv.Itoa(fset.Position(node.Pos()).Line)+" — "+k)
						}
					}
				case *ast.SelectorExpr:
					pkg, ok := node.X.(*ast.Ident)
					if !ok || !declAliases[pkg.Name] {
						return true
					}
					selectorsSeen++
					if meta, ok := edgeOnlyIdents[node.Sel.Name]; ok {
						findings = append(findings,
							rel+":"+strconv.Itoa(fset.Position(node.Pos()).Line)+" — "+meta)
					}
				}
				return true
			})
		}
	}

	require.NotZero(t, filesRead, "прочитано ноль файлов Go — «ноль находок» здесь означало бы "+
		"«ноль прочитанного», и гейт был бы зелёным ни о чём")
	require.NotZero(t, literalsSeen, "осмотрено ноль строковых литералов — разбор сломан")
	t.Logf("перепись: файлов Go прочитано %d · строковых литералов осмотрено %d · "+
		"обращений к пакету-объявлению осмотрено %d · ключей края %d (имён констант %d)",
		filesRead, literalsSeen, selectorsSeen, len(keys), len(edgeOnlyIdents))

	for _, f := range findings {
		t.Errorf("%s: ключ объявлен остающимся на краю, а за краем у него появился читатель. "+
			"Мостовой формы у ключа нет, значит производителя за краем тоже нет — читатель получит "+
			"пустоту либо чужое значение. Решите осознанно: завести мостовую форму и пройти по "+
			"требованиям §«Кто вправе ГОВОРИТЬ ЗА пользователя» (сужение круга законных "+
			"отправителей, отказ старта при пустом круге, измерение в самоотчёте) — либо снять "+
			"читателя.", f)
	}
}
