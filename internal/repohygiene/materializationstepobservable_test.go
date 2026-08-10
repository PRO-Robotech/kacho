// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Здесь держится ПЯТОЕ условие соприкосновения с владельцем прав: «ноль
// срабатываний доказанного входа за прогон» обязано быть НАХОДКОЙ, а не тишиной
// — симметрично правилу про очередь, не доставившую ни одной строки за всю свою
// жизнь (data-integrity.md) и про контроль, ни разу не отказавший
// (security.md §Hardening-инвариант 8).
//
// # Почему одного счётчика мало
//
// Счётчик шагов материализации уже есть, и каждая его клетка заводится нулём —
// чтобы «шаг не исполнялся» было отличимо от «коллектор не провязан». Но у
// этого рассуждения есть предпосылка, которую сам счётчик проверить не может:
// что его ДЕЙСТВИТЕЛЬНО провязали при старте и что у каждого объявленного шага
// ЕСТЬ КТО-ТО, кто его эмитит. Обе половины держатся здесь.
//
// Стоило снять строку провязки в корне композиции — и величина исчезает
// целиком: наружу не выходит ни нуля, ни единицы, а «ноль срабатываний»
// становится неотличим от «нечего показывать». Провязка при этом объявлена
// НЕОБЯЗАТЕЛЬНОЙ и nil-безопасной (так и задумано: use-case не должен падать
// без наблюдаемости), поэтому её пропажу не заметит ни компилятор, ни один
// существующий тест — предикат переписан 2026-08-10 и дал НОЛЬ утверждающих.

// postCommitRecorderCtor / postCommitRecorderSink — имена, которыми провязка
// выражена в дереве. Разъедутся с кодом — проба ниже упадёт на своей же
// предпосылке (перепись найдёт ноль вхождений), а не промолчит.
const (
	postCommitRecorderCtor = "NewRegisterPostCommitRecorder"
	postCommitRecorderSink = "WithMetrics"
	postCommitObserveCall  = "ObserveRegisterPostCommit"
)

// TestMaterializationStepCounterIsWiredAtBoot — счётчик шагов материализации
// провязан в корне композиции.
//
// Провязка ищется как ФАКТ О ДЕРЕВЕ (вызов конструктора, чей результат уходит в
// приёмник), а не по имени файла: корень композиции переезжает, свойство — нет.
func TestMaterializationStepCounterIsWiredAtBoot(t *testing.T) {
	root := repoRoot(t)

	ctorSites, sinkSites := 0, 0
	scanned := 0
	var where []string

	walkOwnerRegisterGoFiles(t, root, []string{"services"}, func(rel string, body []byte) {
		scanned++
		src := string(body)
		if !strings.Contains(src, postCommitRecorderCtor) {
			return
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case postCommitRecorderCtor:
				ctorSites++
				where = append(where, rel+":"+fmtLine(fset, call.Lparen))
			case postCommitRecorderSink:
				// Приёмник засчитывается ТОЛЬКО когда ему передают результат
				// конструктора: `WithMetrics(nil)` или передача чего-то другого
				// — это не провязка счётчика, а её видимость.
				for _, arg := range call.Args {
					if inner, ok := arg.(*ast.CallExpr); ok {
						if isel, ok := inner.Fun.(*ast.SelectorExpr); ok && isel.Sel.Name == postCommitRecorderCtor {
							sinkSites++
						}
					}
				}
			}
			return true
		})
	})

	if scanned == 0 {
		t.Fatal("проба не прочитала ни одного прод-файла — предпосылка обхода сломана")
	}
	if ctorSites == 0 {
		t.Fatalf("в %d прод-файлах НЕТ ни одного вызова %s — либо счётчик шагов материализации "+
			"снят, либо конструктор переименован. В обоих случаях «ноль срабатываний доказанного "+
			"входа» перестаёт быть отличимым от «величины не существует», а именно эту разницу "+
			"счётчик и заводился показывать", scanned, postCommitRecorderCtor)
	}
	if sinkSites == 0 {
		t.Fatalf("счётчик создаётся (%v), но его результат НЕ передаётся в %s — коллектор "+
			"зарегистрирован и мёртв: наружу выходят одни нули, потому что эмитить в него "+
			"некому. Провязка объявлена необязательной и nil-безопасной, поэтому её пропажу "+
			"не поймает ни компилятор, ни существующие тесты", where, postCommitRecorderSink)
	}
	t.Logf("осмотрено прод-файлов %d; конструктор счётчика — %d место(а) %v; передач в приёмник — %d",
		scanned, ctorSites, where, sinkSites)
}

// TestEveryDeclaredMaterializationStepHasAnEmitter — у КАЖДОГО объявленного шага
// есть тот, кто его эмитит.
//
// Шаг без эмиттера — «гейт, у входа которого нет производителя»: его ноль
// СТРУКТУРЕН, а не наблюдение, и читать такой ноль как «путь не исполнялся»
// значит делать вывод о продукте из свойства перечня. Ровно этим и был замер,
// давший ноль срабатываний доказанного входа: чтобы он что-то значил, надо было
// сперва установить, что эмиттер у входа есть.
func TestEveryDeclaredMaterializationStepHasAnEmitter(t *testing.T) {
	root := repoRoot(t)

	declared := declaredPostCommitSteps(t, root)
	if len(declared) == 0 {
		t.Fatal("перечень шагов не прочитан — предпосылка пробы сломана, молчание ничего не доказывает")
	}

	// Константы шагов объявляются один раз и передаются в эмиттер по имени
	// константы, поэтому ищем СТРОКОВЫЙ ЛИТЕРАЛ шага в прод-коде: он есть в
	// объявлении константы, а её употребление ведёт к эмиттеру.
	emitted := map[string]int{}
	observeCalls := 0
	scanned := 0
	walkOwnerRegisterGoFiles(t, root, []string{"services"}, func(_ string, body []byte) {
		scanned++
		src := string(body)
		observeCalls += strings.Count(src, postCommitObserveCall+"(")
		for _, step := range declared {
			emitted[step] += strings.Count(src, `"`+step+`"`)
		}
	})

	if observeCalls == 0 {
		t.Fatalf("в %d прод-файлах нет ни одного вызова %s — эмитить шаги некому, "+
			"и любой их ноль структурен", scanned, postCommitObserveCall)
	}

	var orphans []string
	for _, step := range declared {
		// Одно вхождение — само объявление константы; чтобы у шага был
		// ПОТРЕБИТЕЛЬ, литерал обязан встретиться минимум дважды.
		if emitted[step] < 2 {
			orphans = append(orphans, step)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("шаги без эмиттера в прод-коде (%d из %d): %s\n\n"+
			"их ноль в метрике СТРУКТУРЕН — он говорит о перечне, а не о продукте. "+
			"Либо завести эмиттер, либо снять шаг из закрытого набора: запись, которой "+
			"больше нечего считать, есть находка.",
			len(orphans), len(declared), strings.Join(orphans, ", "))
	}
	t.Logf("осмотрено прод-файлов %d; объявлено шагов %d; вызовов эмиттера %d; вхождений по шагам %v",
		scanned, len(declared), observeCalls, emitted)
}

// declaredPostCommitSteps читает закрытый набор шагов ИЗ ДЕРЕВА, а не
// переписывает его сюда: две копии одного перечня разъезжаются молча, и та, что
// в пробе, разъедется первой.
func declaredPostCommitSteps(t *testing.T, root string) []string {
	t.Helper()
	path := filepath.Join(root, "services", "iam", "internal", "observability", "metrics", "register_postcommit_recorder.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("перечень шагов не прочитан (%s): %v — предпосылка пробы сломана", path, err)
	}
	src := string(body)
	start := strings.Index(src, "RegisterPostCommitSteps = []string{")
	if start < 0 {
		t.Fatalf("в %s не найдено объявление RegisterPostCommitSteps — перечень переехал, "+
			"и проба обязана упасть, а не молчать", path)
	}
	end := strings.Index(src[start:], "}")
	if end < 0 {
		t.Fatalf("объявление RegisterPostCommitSteps не закрыто — разбор невозможен")
	}
	var steps []string
	for _, part := range strings.Split(src[start:start+end], `"`) {
		if strings.Contains(part, "_") && !strings.Contains(part, "{") && !strings.Contains(part, ",") {
			steps = append(steps, part)
		}
	}
	return steps
}

func fmtLine(fset *token.FileSet, pos token.Pos) string {
	p := fset.Position(pos)
	return strings.TrimPrefix(p.String(), p.Filename+":")
}
