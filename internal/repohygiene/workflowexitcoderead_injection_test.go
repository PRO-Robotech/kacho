// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт чтения кода возврата СПОСОБЕН упасть — и падает
// на существе, а не на форме записи.
//
// Инъекция идёт по каждой оси в ОБЕ стороны: у каждой красной фикстуры есть
// законный близнец, отличающийся ОДНИМ фактом. Без второй половины гейт ловил бы
// форму, а не предмет, и первый же ложный срабат его отключил бы.
//
// Половина близнецов — про то, чего в дереве сегодня НЕТ ни одного экземпляра:
// `$?` в одинарных кавычках и `$?` в теле heredoc. Перепись это подтверждает —
// расширение распознавателя на эти формы оставило её неизменной (15 чтений до и
// после). Они здесь не ради сегодняшнего дерева, а ради того, чтобы гейт не
// покраснел на подсказке, которую шаг печатает про себя же, — и держатся ровно
// этими пробами, а не обещанием.
package repohygiene

import (
	"strings"
	"testing"
)

// wfWithRun — синтетический конвейер с одним шагом.
func wfWithRun(shell, body string) string {
	var sb strings.Builder
	sb.WriteString("name: синтетика\non:\n  push:\n    branches: [main]\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - name: шаг\n")
	if shell != "" {
		sb.WriteString("        shell: " + shell + "\n")
	}
	sb.WriteString("        run: |\n")
	for _, ln := range strings.Split(body, "\n") {
		sb.WriteString("          " + ln + "\n")
	}
	return sb.String()
}

// unguardedReads — сколько НЕПРИКРЫТЫХ чтений находит гейт в синтетике.
//
// Зовётся ровно тот распознаватель, что судит дерево: проверь инъекция свою
// копию логики, она доказывала бы свойство кода, которого гейт не исполняет.
func unguardedReads(t *testing.T, wf string) (unguarded, total int) {
	t.Helper()
	blocks, err := collectRunBlocks("синтетика.yml", []byte(wf))
	if err != nil {
		t.Fatalf("синтетика не разобрана: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("блоков `run:` %d, ожидался 1 — фикстура собрана неверно, и её "+
			"вердикт ничего не значит", len(blocks))
	}
	errExit, applicable := wfShellHasErrExit(blocks[0].Shell)
	if !applicable || !errExit {
		return 0, 0
	}
	for _, r := range scanRunBodyForExitCodeReads("f", "j", "шаг", blocks[0].Body) {
		total++
		if !r.Guarded {
			unguarded++
		}
	}
	return unguarded, total
}

// TestExitCodeReadGateFindsTheDefectiveForms — красная половина.
func TestExitCodeReadGateFindsTheDefectiveForms(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"после точки с запятой": "./runner.sh; rc=$?\ncase \"$rc\" in 2) exit 2 ;; esac",
		"на следующей строке":   "./runner.sh\nrc=$?\ncase \"$rc\" in 2) exit 2 ;; esac",
		"прямо в условии":       "./runner.sh\nif [ $? -ne 0 ]; then exit 2; fi",
		"после возврата -e":     "set +e\n./probe.sh || probe_rc=$?\nset -e\n./runner.sh; rc=$?",
		"чтение в кавычках":     "./runner.sh; echo \"код $?\"",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			unguarded, total := unguardedReads(t, wfWithRun("", body))
			if total == 0 {
				t.Fatalf("гейт не увидел ни одного чтения — фикстура мимо предмета")
			}
			if unguarded == 0 {
				t.Errorf("чтений %d, непокрытых 0 — гейт пропустил форму, которая под "+
					"`bash -e` не исполняется", total)
			}
		})
	}
}

// TestExitCodeReadGateStaysSilentOnLegalForms — молчаливая половина.
//
// Каждый близнец отличается от красного выше ОДНИМ фактом: прикрытием вызова,
// снятием режима, оболочкой без `-e` либо тем, что `$?` здесь вообще не код.
func TestExitCodeReadGateStaysSilentOnLegalForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, shell, body string
	}{
		{"страховка ||", "", "./runner.sh || rc=$?\ncase \"$rc\" in 2) exit 2 ;; esac"},
		{"страховка && ||", "", "./runner.sh && rc=0 || rc=$?\ncase \"$rc\" in 2) exit 2 ;; esac"},
		{"режим снят set +e", "", "set +e\n./runner.sh; rc=$?\ncase \"$rc\" in 2) exit 2 ;; esac"},
		{"оболочка не bash", "python", "rc = 1  # ./runner.sh; rc=$?"},
		{"шаблон без -e", "bash {0}", "./runner.sh; rc=$?\ncase \"$rc\" in 2) exit 2 ;; esac"},
		{"только в комментарии", "", "# так нельзя: ./runner.sh; rc=$?\n./runner.sh || rc=$?"},
		{"литерал в кавычках", "", "echo 'форма-ловушка: cmd; rc=$?'\n./runner.sh || rc=$?"},
		{"тело heredoc", "", "cat <<'HINT'\nне пиши cmd; rc=$?\nHINT\n./runner.sh || rc=$?"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			unguarded, _ := unguardedReads(t, wfWithRun(c.shell, c.body))
			if unguarded != 0 {
				t.Errorf("законная форма объявлена находкой (%d) — гейт ловит форму "+
					"записи, а не предмет, и первый же ложный срабат его отключит", unguarded)
			}
		})
	}
}

// TestErrExitDetectionCutsBothWays — таблица оболочек: где `-e` действует, а где
// предмета нет вовсе.
//
// Смешать «предмет прикрыт» с «предмета нет» нельзя: первое — свойство кода,
// второе — свойство объявления шага, и перепись печатает их порознь.
func TestErrExitDetectionCutsBothWays(t *testing.T) {
	t.Parallel()
	cases := []struct {
		shell             string
		errExit, applicab bool
	}{
		{"", true, true},            // умолчание раннера — bash -e {0}
		{"bash", true, true},        // синоним bash --noprofile --norc -eo pipefail
		{"sh", true, true},          // синоним sh -e
		{"bash {0}", false, true},   // законное снятие режима на весь шаг
		{"bash -e {0}", true, true}, // режим возвращён явно
		{"bash -eo pipefail {0}", true, true},
		{"python", false, false}, // предмета нет
		{"pwsh", false, false},
	}
	for _, c := range cases {
		e, a := wfShellHasErrExit(c.shell)
		if e != c.errExit || a != c.applicab {
			t.Errorf("оболочка %q: получено (-e=%v, предмет=%v), ожидалось (-e=%v, предмет=%v)",
				c.shell, e, a, c.errExit, c.applicab)
		}
	}
}

// TestExitCodeReadGateSeesCompositeActionSteps — вторая форма объявления шагов.
//
// Составных действий в дереве сегодня НОЛЬ. Именно поэтому форма покрыта здесь,
// а не «когда понадобится»: появись она позже, всё записанное в ней было бы вне
// наблюдения, и перепись не отличила бы это от «нарушений нет».
//
// Инъекция в обе стороны, отличие одним фактом — прикрытием вызова.
func TestExitCodeReadGateSeesCompositeActionSteps(t *testing.T) {
	t.Parallel()
	composite := func(body string) string {
		var sb strings.Builder
		sb.WriteString("name: синтетическое действие\ndescription: d\nruns:\n  using: composite\n  steps:\n    - name: шаг\n      shell: bash\n      run: |\n")
		for _, ln := range strings.Split(body, "\n") {
			sb.WriteString("        " + ln + "\n")
		}
		return sb.String()
	}

	unguarded, total := unguardedReads(t, composite("./runner.sh; rc=$?\ncase \"$rc\" in 2) exit 2 ;; esac"))
	if total == 0 {
		t.Fatalf("шаги составного действия не разобраны вовсе — форма вне наблюдения")
	}
	if unguarded == 0 {
		t.Errorf("чтений %d, непокрытых 0 — в составном действии дефект пропущен", total)
	}

	if u, _ := unguardedReads(t, composite("./runner.sh || rc=$?\ncase \"$rc\" in 2) exit 2 ;; esac")); u != 0 {
		t.Errorf("законная форма в составном действии объявлена находкой (%d)", u)
	}
}

// TestExitCodeReadGateRefusesAnEmptyWalk — гейт не выдаёт пустоту за чистоту.
//
// Конвейер без единого блока `run:` не является «ноль находок»: читать было
// нечего, и вердикта нет.
func TestExitCodeReadGateRefusesAnEmptyWalk(t *testing.T) {
	t.Parallel()
	blocks, err := collectRunBlocks("пусто.yml",
		[]byte("name: пусто\non:\n  push:\n    branches: [main]\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n"))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("блоков `run:` %d, ожидалось 0 — шаг `uses:` телом не обладает", len(blocks))
	}
}
