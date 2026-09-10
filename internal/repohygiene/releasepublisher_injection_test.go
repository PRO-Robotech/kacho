// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// releasepublisher_injection_test.go — доказательство падучести гейта
// производителя версии.
//
// # Зачем
//
// `TestMonorepoCarriesAVersionPublisher` зелен на дереве, где всё на месте, — и
// ровно так же зелен гейт, у которого перечень предметов опустел или сверка
// выродилась. Отличить их можно только внесённым дефектом.
//
// # Одно-фактность и законный близнец
//
// Инъекция гоняет ТУ ЖЕ функцию `auditReleasePublisher` по синтетическому
// дереву. У каждого дефекта есть близнец, отличающийся ровно одним фактом:
// снят один файл · снят бит исполнения · прогонщик перестал звать одну
// инъекцию. Отдельным утверждением стоит близнец, которого гейт обязан НЕ
// заметить — посторонний скрипт той же формы рядом: без него гейт мог бы
// ловить «сколько файлов в каталоге», а не «есть ли названные».
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticPublisherTree — дерево, где все предметы выпуска на месте.
func syntheticPublisherTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts", "release"), 0o755); err != nil {
		t.Fatalf("каталог не создан: %v", err)
	}
	for _, a := range releaseArtifacts {
		for _, rel := range []string{a.mechanism, a.injection} {
			if err := os.WriteFile(filepath.Join(root, rel), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
				t.Fatalf("файл не записан (%s): %v", rel, err)
			}
		}
	}
	return root
}

// syntheticRunner — текст прогонщика, зовущего обе инъекции.
func syntheticRunner() string {
	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\n")
	for _, a := range releaseArtifacts {
		sb.WriteString(`    run "x" bash "$ROOT/` + a.injection + "\"\n")
	}
	return sb.String()
}

// syntheticCallers — исполняемые строки, зовущие КАЖДЫЙ механизм. Форма
// намеренно повторяет рецепт сборки: строка начинается с табуляции.
func syntheticCallers() []string {
	sources := map[string]string{"Makefile": ""}
	var sb strings.Builder
	for _, a := range releaseArtifacts {
		sb.WriteString("\t" + a.mechanism + " $(VERSION)\n")
	}
	sources["Makefile"] = sb.String()
	return releaseCallerLines(sources)
}

func TestReleasePublisherGateStaysSilentOnALegitimateTree(t *testing.T) {
	t.Parallel()
	a := auditReleasePublisher(syntheticPublisherTree(t), syntheticRunner(), syntheticCallers())
	t.Logf("законный близнец: файлов прочитано %d, утверждений %d, находок %d",
		a.filesRead, a.assertions, len(a.findings))
	if len(a.findings) != 0 {
		t.Fatalf("на исправном дереве гейт обязан молчать, а он нашёл: %v", a.findings)
	}
	if a.assertions == 0 {
		t.Fatalf("обход пуст — контроль беспредметен")
	}
}

// TestReleasePublisherGateIgnoresAnUnrelatedScript — второй законный близнец.
//
// Посторонний скрипт в том же каталоге не является ни механизмом, ни его
// доказательством. Гейт, считающий файлы, на нём бы сработал; гейт, судящий
// названные предметы, — нет.
func TestReleasePublisherGateIgnoresAnUnrelatedScript(t *testing.T) {
	t.Parallel()
	root := syntheticPublisherTree(t)
	if err := os.WriteFile(filepath.Join(root, "scripts", "release", "unrelated.sh"),
		[]byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatalf("файл не записан: %v", err)
	}
	if a := auditReleasePublisher(root, syntheticRunner(), syntheticCallers()); len(a.findings) != 0 {
		t.Fatalf("посторонний скрипт не является предметом гейта, а он нашёл: %v", a.findings)
	}
}

func TestReleasePublisherGateRedOnAMissingMechanism(t *testing.T) {
	t.Parallel()
	root := syntheticPublisherTree(t)
	victim := releaseArtifacts[0].mechanism
	if err := os.Remove(filepath.Join(root, victim)); err != nil {
		t.Fatalf("файл не снят: %v", err)
	}
	a := auditReleasePublisher(root, syntheticRunner(), syntheticCallers())
	if len(a.findings) != 1 || !strings.Contains(a.findings[0], victim) {
		t.Fatalf("снятый механизм обязан дать ровно одну находку с координатой %s, получено: %v", victim, a.findings)
	}
	t.Logf("ось «механизм снят»: гейт краснеет и называет %s", victim)
}

func TestReleasePublisherGateRedOnANonExecutableFile(t *testing.T) {
	t.Parallel()
	root := syntheticPublisherTree(t)
	victim := releaseArtifacts[1].mechanism
	if err := os.Chmod(filepath.Join(root, victim), 0o644); err != nil {
		t.Fatalf("режим не изменён: %v", err)
	}
	a := auditReleasePublisher(root, syntheticRunner(), syntheticCallers())
	if len(a.findings) != 1 || !strings.Contains(a.findings[0], victim) {
		t.Fatalf("неисполняемый файл обязан дать ровно одну находку с координатой %s, получено: %v", victim, a.findings)
	}
	t.Logf("ось «бит исполнения снят»: гейт краснеет и называет %s", victim)
}

// TestReleasePublisherGateRedWhenTheRunnerStopsCallingAnInjection — главная ось.
//
// Файлы на месте, всё выглядит исправным, и доказательство падучести не
// исполняется. Ровно этот класс здесь уже наблюдался: инъекция, которую никто
// не зовёт, ломается в тот же день и молчит об этом.
func TestReleasePublisherGateRedWhenTheRunnerStopsCallingAnInjection(t *testing.T) {
	t.Parallel()
	root := syntheticPublisherTree(t)
	victim := releaseArtifacts[0].injection
	runner := strings.ReplaceAll(syntheticRunner(), victim, "scripts/release/something-else.sh")
	a := auditReleasePublisher(root, runner, syntheticCallers())
	if len(a.findings) != 1 || !strings.Contains(a.findings[0], victim) {
		t.Fatalf("невызываемая инъекция обязана дать ровно одну находку с координатой %s, получено: %v", victim, a.findings)
	}
	t.Logf("ось «прогонщик перестал звать»: гейт краснеет и называет %s", victim)
}

// TestReleasePublisherGateRedWhenAMechanismHasNoCaller — ось «механизм зовётся».
//
// Файлы на месте, доказательство падучести зовётся прогонщиком, и всё выглядит
// исправным — а механизм не исполняет НИКТО. Ровно в этом состоянии был
// производитель поставки: он существовал, был исполняем, имел инъекцию, и
// выкладку при этом доводил человек.
func TestReleasePublisherGateRedWhenAMechanismHasNoCaller(t *testing.T) {
	t.Parallel()
	root := syntheticPublisherTree(t)
	victim := releaseArtifacts[0].mechanism
	var callers []string
	for _, line := range syntheticCallers() {
		if !strings.Contains(line, victim) {
			callers = append(callers, line)
		}
	}
	a := auditReleasePublisher(root, syntheticRunner(), callers)
	if len(a.findings) != 1 || !strings.Contains(a.findings[0], victim) {
		t.Fatalf("механизм без вызывающего обязан дать ровно одну находку с координатой %s, получено: %v",
			victim, a.findings)
	}
	t.Logf("ось «механизм никто не зовёт»: гейт краснеет и называет %s", victim)
}

// TestReleasePublisherGateDoesNotCountACommentAsACaller — ЗАКОННЫЙ БЛИЗНЕЦ оси
// «механизм зовётся», и он несущий.
//
// Отличие от случая выше — РОВНО ОДИН факт: строка вызова осталась на месте, но
// стала комментарием. Путь по-прежнему в тексте, а исполнения нет. Гейт,
// судящий по подстроке в файле, здесь МОЛЧИТ и потому зеленеет на справке
// собственного рецепта, где все девять путей перечислены поимённо.
func TestReleasePublisherGateDoesNotCountACommentAsACaller(t *testing.T) {
	t.Parallel()
	root := syntheticPublisherTree(t)
	victim := releaseArtifacts[0].mechanism

	var sb strings.Builder
	for _, a := range releaseArtifacts {
		if a.mechanism == victim {
			sb.WriteString("##   " + a.mechanism + " — про него написано в справке\n")
			continue
		}
		sb.WriteString("\t" + a.mechanism + " $(VERSION)\n")
	}
	callers := releaseCallerLines(map[string]string{"Makefile": sb.String()})

	a := auditReleasePublisher(root, syntheticRunner(), callers)
	if len(a.findings) != 1 || !strings.Contains(a.findings[0], victim) {
		t.Fatalf("упоминание в комментарии вызовом не является — ожидалась одна находка с %s, получено: %v",
			victim, a.findings)
	}
	t.Logf("законный близнец: путь в комментарии не зачтён вызовом — гейт называет %s", victim)
}

// TestReleasePublisherGateRedOnAnEmptyTree — обход по пустому дереву.
//
// «Ноль находок» и «ноль прочитанного» обязаны быть различимы: пустое дерево
// даёт находку по каждому предмету, а не тишину.
func TestReleasePublisherGateRedOnAnEmptyTree(t *testing.T) {
	t.Parallel()
	a := auditReleasePublisher(t.TempDir(), "", nil)
	if a.filesRead != 0 {
		t.Fatalf("в пустом дереве прочитанных файлов быть не может, прочитано %d", a.filesRead)
	}
	// Находок на предмет ровно ЧЕТЫРЕ: нет механизма · нет его инъекции ·
	// прогонщик инъекцию не зовёт · механизм никто не зовёт. Число выписано, а
	// не «больше нуля»: «больше нуля» зеленело бы и на гейте, схлопнувшем все
	// оси в одну.
	if want := 4 * len(releaseArtifacts); len(a.findings) != want {
		t.Fatalf("пустое дерево обязано дать по четыре находки на предмет (%d), получено %d: %v",
			want, len(a.findings), a.findings)
	}
	if a.callerLines != 0 {
		t.Fatalf("мест вызова в пустом дереве быть не может, прочитано %d", a.callerLines)
	}
	t.Logf("ось «пустое дерево»: находок %d при нуле прочитанных файлов", len(a.findings))
}
