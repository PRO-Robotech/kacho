// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
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
	if !strings.Contains(err.Error(), "корневой") {
		t.Errorf("отказ не называет истинной причины (корневой коммит): %v", err)
	}
}
