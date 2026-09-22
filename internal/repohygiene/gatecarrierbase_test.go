// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// gatecarrierbase_test.go — держатель одного свойства [gateCarrierBase]: КОГДА
// РОДИТЕЛЬ HEAD НЕ РАЗРЕШАЕТСЯ, ОТКАЗ НАЗЫВАЕТ ИСТИННУЮ ПРИЧИНУ.
//
// Причин две, и они лечатся противоположным: у HEAD действительно нет родителя
// (корневой коммит — сравнить не с чем) либо родитель есть у источника и просто
// не довезён мелким клоном (чинится глубиной). Код возврата их не разделяет —
// обе приходят единицей (замер в шапке gitrevcause.go), — и первая редакция
// объявляла второй случай первым: утверждала «у него НЕТ родителя» там, где
// родитель был, и ремонта не называла вовсе.

// gateBaseOriginRepo — источник под пробу: репозиторий с ветвью `main` и
// заданным числом пустых коммитов.
func gateBaseOriginRepo(t *testing.T, commits int) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "origin")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("каталог источника: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := gitenv.Command(dir, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v в источнике: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main", ".")
	for i := 0; i < commits; i++ {
		run("-c", "user.email=probe@example.invalid", "-c", "user.name=probe",
			"commit", "-q", "--allow-empty", "-m", "c")
	}
	return dir
}

// gateBaseClone — клон источника, мелкий или полный, с проверкой предпосылок:
// глубина именно та, что заказана, и ссылка линии в клоне ЕСТЬ — иначе разбор
// пошёл бы другой ветвью, и вердикт был бы не о том.
func gateBaseClone(t *testing.T, origin string, shallow bool) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "clone")
	args := []string{"clone", "-q"}
	if shallow {
		args = append(args, "--depth=1")
	}
	args = append(args, "file://"+origin, dst)
	if out, err := gitenv.Command(filepath.Dir(dst), args...).CombinedOutput(); err != nil {
		t.Fatalf("клон не создан (%v): %s — условие пробы не создано, и её молчание "+
			"ничего не значило бы", err, out)
	}
	if got := gitCloneIsShallow(dst); got != shallow {
		t.Fatalf("клон мелкий=%v, заказан мелкий=%v — предпосылка пробы не выполнена", got, shallow)
	}
	out, err := gitenv.Command(dst, "for-each-ref", "--format=%(refname)",
		"refs/remotes/origin/main").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		t.Fatalf("в клоне нет ссылки линии refs/remotes/origin/main (%v) — разбор "+
			"пошёл бы ветвью живой полосы, и проба судила бы не свой предмет", err)
	}
	if err := gitenv.Command(dst, "merge-base", "--is-ancestor", "HEAD",
		"refs/remotes/origin/main").Run(); err != nil {
		t.Fatalf("HEAD не признан принадлежащим линии (%v) — предпосылка пробы "+
			"не выполнена", err)
	}
	return dst
}

// gateBaseRequiresUnsetKnob — ручка, объявляющая базу точно, перебивает ВЕСЬ
// разбор. Если она выставлена в окружении прогона, проба судила бы первую
// строку функции, а не предмет.
func gateBaseRequiresUnsetKnob(t *testing.T) {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv(gateCarrierBaseEnv)); v != "" {
		t.Fatalf("%s=%s выставлена в окружении прогона — разбор базы не исполняется, "+
			"и молчание пробы ничего не значило бы. Это отказ ПРЕДПОСЫЛКИ, а не "+
			"отсутствие предмета", gateCarrierBaseEnv, v)
	}
}

// TestGateCarrierBaseTellsAnUndeliveredParentFromAMissingOne — МЕЛКИЙ КЛОН НЕ
// ОБЪЯВЛЯЕТСЯ КОРНЕВЫМ КОММИТОМ.
//
// Пара «мелкий против полного» одно-фактна: тот же источник, та же ссылка
// линии, та же голова — меняется ровно глубина клона.
func TestGateCarrierBaseTellsAnUndeliveredParentFromAMissingOne(t *testing.T) {
	t.Parallel()
	gateBaseRequiresUnsetKnob(t)

	origin := gateBaseOriginRepo(t, 2)

	// Законный близнец: полный клон того же источника родителя ЗНАЕТ, и отказа
	// нет вовсе. Без него красное ниже достигалось бы отказом на чём угодно.
	full := gateBaseClone(t, origin, false)
	rev, how, err := gateCarrierBase(full)
	if err != nil {
		t.Fatalf("полный клон не дал базы: %v — близнец не зелёный", err)
	}
	t.Logf("близнец: база %s (%s)", rev, how)
	if rev == "" {
		t.Error("полный клон дал пустую базу при отсутствии отказа — пустой успех")
	}

	// Отрицательный конец: тот же источник, мелкий клон. Родитель существует у
	// источника и просто не довезён.
	shallow := gateBaseClone(t, origin, true)
	_, _, err = gateCarrierBase(shallow)
	if err == nil {
		t.Fatal("мелкий клон дал базу, которой у него нет — fail-open")
	}
	t.Logf("текст отказа мелкому клону: %v", err)
	if !gitRevRemedyNamed(err, gitRevRemedyCloneDepth) {
		t.Errorf("отказ не называет ремонта глубиной клона — читающий пойдёт искать "+
			"несуществующую причину: %v", err)
	}
	if strings.Contains(err.Error(), "нет родителя") {
		t.Errorf("отказ УТВЕРЖДАЕТ отсутствие родителя, которого не проверял: родитель "+
			"есть у источника, он не довезён — это ложь о причине: %v", err)
	}
}

// TestGateCarrierBaseNamesARootCommitAsItIs — ВТОРАЯ ПОЛОВИНА ПАРЫ: когда
// родителя действительно нет, отказ говорит именно это и глубину НЕ поминает.
//
// Без неё правка выше была бы односторонней: «всегда вини глубину» прошло бы
// первую пробу и лгало бы ровно наоборот.
func TestGateCarrierBaseNamesARootCommitAsItIs(t *testing.T) {
	t.Parallel()
	gateBaseRequiresUnsetKnob(t)

	// Полный клон источника, у которого ОДИН коммит: родителя нет по существу.
	full := gateBaseClone(t, gateBaseOriginRepo(t, 1), false)
	_, _, err := gateCarrierBase(full)
	if err == nil {
		t.Fatal("корневой коммит дал базу — сравнивать было не с чем, а отказа нет")
	}
	t.Logf("текст отказа корневому коммиту: %v", err)
	if gitRevRemedyNamed(err, gitRevRemedyCloneDepth) {
		t.Errorf("ПОЛНОМУ клону велено чинить глубину — ремонт назван противоположный "+
			"истинному: %v", err)
	}
	// Причина утверждается КЛАУЗОЙ ОБЩЕГО СЛОВАРЯ, а не подстрокой по прозе.
	// Прежняя редакция искала слово «корневой» — то есть ровно тот поиск по
	// свободному тексту, ради снятия которого словарь заводился; и заведена она
	// была потому, что четвёртой клаузы в словаре не существовало, а ветвь
	// корневого коммита ремонта не называла вовсе.
	if !gitRevRemedyNamed(err, gitRevRemedyDeclaredBase) {
		t.Errorf("отказ не называет своего ремонта %q: %v", gitRevRemedyDeclaredBase, err)
	}
}

// TestGateCarrierNoParentRefusalCarriesTheToolsAnswer — ОТКАЗ НЕСЁТ ОТВЕТ
// ИНСТРУМЕНТА, А НЕ ТОЛЬКО СВОЙ ВЫВОД О НЁМ.
//
// Обе ветви УТВЕРЖДАЮТ причину по отдельному вопросу о глубине, а не по тому,
// что ответил `rev-parse`. Утверждение, выбросившее единственное своё
// опровержение, проверить нечем: при отказе иного рода — битый объект, права —
// код так же уверенно назовёт корневой коммит либо недовезённость. Сегодня этот
// вход недостижим (путь идёт после успешных `for-each-ref` и `merge-base`),
// поэтому ложного зелёного нет; недостижимость же есть свойство МАРШРУТА и
// переживёт его молча.
//
// Пара одно-фактна: те же ref, глубина и вывод — меняется ровно ошибка.
func TestGateCarrierNoParentRefusalCarriesTheToolsAnswer(t *testing.T) {
	t.Parallel()

	const ref = "refs/remotes/origin/main"
	for _, shallow := range []bool{false, true} {
		// Законный близнец: достижимый сегодня вход — код 1 с пустым выводом.
		reachable := gateCarrierNoParentRefusal(ref, shallow, nil, errors.New("exit status 1"))
		if !strings.Contains(reachable.Error(), "exit status 1") {
			t.Errorf("мелкий=%v: отказ не несёт ответа инструмента — проверить его "+
				"утверждение о причине нечем: %v", shallow, reachable)
		}

		// Тот же вход, ОДИН изменённый факт: отказ иного рода. Текст обязан
		// смениться и понести именно его.
		other := gateCarrierNoParentRefusal(ref, shallow, nil,
			errors.New("error: object file .git/objects/4b/825d is empty"))
		if !strings.Contains(other.Error(), "object file") {
			t.Errorf("мелкий=%v: отказ иного рода не доехал до читающего: %v", shallow, other)
		}
		if strings.Contains(other.Error(), "exit status 1") {
			t.Errorf("мелкий=%v: отказ назвал ЧУЖУЮ ошибку: %v", shallow, other)
		}
		if reachable.Error() == other.Error() {
			t.Errorf("мелкий=%v: два разных ответа инструмента дали один текст — "+
				"утверждение о причине неопровержимо by construction: %q",
				shallow, reachable.Error())
		}

		// Вторая половина дизъюнкции: инструмент НЕ отказал, а ревизии не назвал.
		// Названо словом, а не молчанием.
		silent := gateCarrierNoParentRefusal(ref, shallow, []byte("  \n"), nil)
		if !strings.Contains(silent.Error(), "<nil>") {
			t.Errorf("мелкий=%v: пустой ответ без отказа неотличим от отказа: %v", shallow, silent)
		}
	}

	// Ремонты двух ветвей ПРОТИВОПОЛОЖНЫ и не перепутаны — та же проверка, что
	// у клонированной пары выше, но здесь она не зависит от среды.
	root := gateCarrierNoParentRefusal(ref, false, nil, errors.New("exit status 1"))
	shal := gateCarrierNoParentRefusal(ref, true, nil, errors.New("exit status 1"))
	if !gitRevRemedyNamed(root, gitRevRemedyDeclaredBase) || gitRevRemedyNamed(root, gitRevRemedyCloneDepth) {
		t.Errorf("корневому коммиту назван не свой ремонт: %v", root)
	}
	if !gitRevRemedyNamed(shal, gitRevRemedyCloneDepth) || gitRevRemedyNamed(shal, gitRevRemedyDeclaredBase) {
		t.Errorf("мелкому клону назван не свой ремонт: %v", shal)
	}
}
