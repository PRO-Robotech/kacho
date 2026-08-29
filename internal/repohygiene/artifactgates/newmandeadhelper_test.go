// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// deadHelperDriverRel — перепись живёт отдельным скриптом, потому что читает
// РАЗОБРАННЫЙ Python: имя помощника встречается в прозе шапок, в комментариях и
// в отчётах набора, и предикат по подстроке считал бы объяснение вызовом.
const deadHelperDriverRel = "internal/repohygiene/artifactgates/newmandeadhelper_driver.py"

// deadHelperCensus — объём осмотренного, обе половины порознь. Одно суммарное
// число скрыло бы ровно тот случай, ради которого гейт заведён: набор, чьи
// модули кейсов не прочитались, дал бы «ноль находок» так же, как чистый.
type deadHelperCensus struct {
	suites     int
	files      int
	crossFiles int
	injected   int
	dead       int
	// crossOnly — помощники, у которых вызывающего в СВОЁМ наборе нет, а
	// межнаборный есть. Считается отдельно намеренно: полоса межнаборных
	// потребителей ошибается в сторону молчания (см. шапку драйвера), и
	// собственное число делает эту слепоту видимой, а не растворяет её в сумме.
	crossOnly int
}

type deadHelperReport struct {
	Scanned      int            `json:"scanned"`
	CrossScanned int            `json:"cross_scanned"`
	Calls        map[string]int `json:"calls"`
	Cross        map[string]int `json:"cross"`
}

// auditDeadInjectedHelpers — судящая функция гейта.
//
// Выделена, чтобы инъекция гоняла ЕЁ, а не свою копию. На вход — уже собранная
// перепись по наборам: сам разбор Python делает драйвер, и подменить его
// синтетикой в пробе можно, не поднимая дерева.
func auditDeadInjectedHelpers(reports map[string]deadHelperReport) ([]string, deadHelperCensus) {
	cen := deadHelperCensus{suites: len(reports)}

	suites := make([]string, 0, len(reports))
	for s := range reports {
		suites = append(suites, s)
	}
	sort.Strings(suites)

	var findings []string
	for _, s := range suites {
		r := reports[s]
		cen.files += r.Scanned
		cen.crossFiles += r.CrossScanned
		cen.injected += len(r.Calls)

		var dead []string
		for name, n := range r.Calls {
			if n > 0 {
				continue
			}
			// Вызывающий в чужом наборе — тоже вызывающий. Форма, которой
			// распознаватель не знает, уводит предмет не в находку и не в
			// молчание, а в невидимость: снятие такого помощника уронило
			// межнаборную пробу стойкости сериализатора.
			if r.Cross[name] > 0 {
				cen.crossOnly++
				continue
			}
			dead = append(dead, name)
		}
		if len(dead) == 0 {
			continue
		}
		sort.Strings(dead)
		cen.dead += len(dead)
		findings = append(findings, fmt.Sprintf(
			"%s — впрыскивается в модули кейсов, но не вызывается ни одним: %s",
			s, strings.Join(dead, ", ")))
	}
	return findings, cen
}

// Помощник, впрыскиваемый в модули кейсов, имеет в наборе хотя бы одного
// вызывающего.
//
// ПРЕДМЕТ. Таблица впрыска доставляет имя в пространство имён каждого модуля
// кейсов набора. Имя, которое никто не зовёт, доставляется впустую: оно
// выглядит работающим, его правят вслед за живым близнецом (или не правят, и
// тогда копии расходятся), и оно держит в дереве перечень, который никто не
// исполняет.
//
// ПОЧЕМУ ЭТО НЕ «ПРО ЗАПАС». Мёртвый помощник — это обещание кейсов, которых
// нет. Указатель кейсов и отчёт набора перечисляют его как источник проверок, и
// читатель отчёта считает эти проверки исполненными. Так и вышло: три помощника
// проверки имени, меток и описания были перечислены указателем compute, не
// вызывались ни разу и в дереве отсутствуют вовсе.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ. Мёртвая копия не ломает ни сборку, ни генерацию:
// коллекции она не излучает by construction, поэтому её отсутствие ничем не
// наблюдаемо. Набор заводят копированием соседнего — вместе с его помощниками, —
// и половина из них в новом наборе не понадобится никогда.
//
// ЧЕГО ГЕЙТ НЕ СУДИТ. Он не требует, чтобы помощник вызывался в КАЖДОМ наборе:
// живой у одного и мёртвый у другого — обычное дело, и предмет здесь именно
// второй. И он не судит помощники, которые не впрыскиваются: те потребляются
// самим генератором и видны его собственному разбору.
func TestNewmanInjectedHelperHasACallerInItsSuite(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	driver := filepath.Join(root, deadHelperDriverRel)
	if _, err := os.Stat(driver); err != nil {
		t.Fatalf("перепись %s не найдена (%v): гейт без своего разбора судил бы "+
			"пустоту, а «ноль находок» обязано быть отличимо от «ноль прочитанного»",
			deadHelperDriverRel, err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 не найден (%v): генераторы сквозных проб не разбираемы, "+
			"и это «не выполнилось», а не согласие", err)
	}

	var gens []string
	for rel := range tt.files {
		if filepath.Base(rel) == "gen.py" && strings.Contains(rel, "tests/newman/scripts/") {
			gens = append(gens, rel)
		}
	}
	if len(gens) == 0 {
		t.Fatalf("предпосылка гейта не выполняется: генераторов newman в индексе НОЛЬ — " +
			"чинить надо обход, а не молча выходить успехом.")
	}
	sort.Strings(gens)

	// Каталоги скриптов ВСЕХ наборов — полоса межнаборных потребителей. Перечень
	// ВЫВОДИТСЯ из того же обхода, что и сами генераторы: выписанный разошёлся бы
	// с деревом молча, и новый набор остался бы вне полосы.
	var crossDirs []string
	for _, rel := range gens {
		crossDirs = append(crossDirs, filepath.Dir(filepath.Join(root, rel)))
	}

	reports := map[string]deadHelperReport{}
	for _, rel := range gens {
		suiteDir := filepath.Dir(filepath.Dir(filepath.Join(root, rel)))
		args := []string{driver, filepath.Join(root, rel),
			filepath.Join(suiteDir, "cases"), filepath.Join(suiteDir, "scripts"), "--cross"}
		for _, d := range crossDirs {
			if d != filepath.Join(suiteDir, "scripts") {
				args = append(args, d)
			}
		}
		cmd := exec.Command(python, args...) // #nosec G204 -- пути из индекса git
		out, err := cmd.Output()
		if err != nil {
			var stderr string
			if ee, ok := err.(*exec.ExitError); ok {
				stderr = strings.TrimSpace(string(ee.Stderr))
			}
			t.Fatalf("%s: перепись не исполнилась (%v) — сторона набора НЕ ПРОВЕРЕНА,\n"+
				"и это не «ноль находок»\n%s", rel, err, stderr)
		}
		var r deadHelperReport
		if err := json.Unmarshal(out, &r); err != nil {
			t.Fatalf("%s: разбор вывода переписи: %v", rel, err)
		}
		if r.Scanned == 0 {
			t.Fatalf("%s: перепись не прочла ни одного модуля — предпосылка не выполняется", rel)
		}
		reports[rel] = r
	}

	findings, cen := auditDeadInjectedHelpers(reports)

	t.Logf("осмотрено наборов %d, своих модулей %d, межнаборных %d; впрыскиваемых помощников %d, "+
		"из них зовут только из чужого набора %d, без вызывающего вовсе %d",
		cen.suites, cen.files, cen.crossFiles, cen.injected, cen.crossOnly, cen.dead)

	if cen.injected == 0 {
		t.Fatalf("предпосылка гейта не выполняется: впрыскиваемых помощников НОЛЬ.\n" +
			"Либо таблица впрыска переименована, либо разбор смотрит не туда — в обоих\n" +
			"случаях это отказ: гейт, потерявший предмет, вечнозелен.")
	}

	if len(findings) > 0 {
		t.Fatalf("помощник впрыскивается в модули кейсов и не вызывается ни одним.\n"+
			"Мёртвая копия не излучает ничего, поэтому её присутствие ненаблюдаемо, а\n"+
			"указатель кейсов продолжает обещать проверки, которых нет. Исходов два:\n"+
			"позвать её или снять вместе с записью в таблице впрыска и в документации:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
