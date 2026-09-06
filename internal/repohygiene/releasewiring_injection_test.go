// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// releasewiring_injection_test.go — доказательство падучести гейта зовущего.
//
// # Зачем
//
// `TestVersionPublishingHasACallerInThePipeline` зелен на дереве, где всё на
// месте, — и ровно так же зелен гейт, у которого разбор выродился: читает сырой
// текст вместо исполняемой части, не различает автоматический триггер, не
// замечает провязки в пустоту. Отличить их можно только внесённым дефектом.
//
// # Одно-фактность и законные близнецы
//
// Инъекция гоняет ТУ ЖЕ функцию `auditReleaseWiring` по синтетическому дереву.
// У каждого дефекта близнец, отличающийся ровно одним фактом. Отдельно стоят
// три близнеца, которых гейт обязан НЕ заметить:
//
//   - имя механизма В КОММЕНТАРИИ объявления — не вызов. Без этой оси гейт мог
//     бы читать сырой текст и зеленеть на объявлении, где вызова нет вовсе;
//   - посторонний скрипт, позванный шагом рядом с производителем, — законен;
//   - ключ `on`, разрешаемый YAML как логическая истина, обязан читаться
//     ТЕКСТОМ ключа. Без этого гейт не увидел бы триггеров вовсе и промолчал бы
//     на объявлении с `push`.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticProducerTree — дерево, где производитель на месте и провязан.
//
// wf — тело объявления; scripts — механизмы, которые в дереве СОЗДАЮТСЯ
// исполняемыми (то, что объявление зовёт, но здесь не названо, окажется
// провязкой в пустоту — и это отдельная ось).
func syntheticProducerTree(t *testing.T, wf string, scripts ...string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755); err != nil {
		t.Fatalf("каталог не создан: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scripts", "release"), 0o755); err != nil {
		t.Fatalf("каталог не создан: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, releaseProducerFile), []byte(wf), 0o644); err != nil {
		t.Fatalf("объявление не записано: %v", err)
	}
	for _, s := range scripts {
		if err := os.WriteFile(filepath.Join(root, s), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
			t.Fatalf("механизм не записан (%s): %v", s, err)
		}
	}
	return root
}

// producerYAML — законное объявление: ручной запуск, три входа, вызов
// производителя. Части параметризованы, чтобы каждая ось меняла ровно ОДНУ.
//
// ДОБАВОЧНЫЙ ТРИГГЕР ПРИСТАВЛЯЕТСЯ ПОСЛЕ БЛОКА ВХОДОВ, а не перед ним, и это не
// вкус. Первая редакция склеивала его с ручным запуском ДО `inputs:`, отчего
// входы оказывались вложены в чужой триггер — то есть ось меняла ДВА факта
// сразу (появился `push` И пропали входы), и по красному нельзя было сказать,
// который из них его дал. Гейт был прав, фикстура — нет.
func producerYAML(extraTriggers, inputs, run string) string {
	return "name: выпуск версии платформы\n" +
		"on:\n" +
		"  workflow_dispatch:\n" +
		"    inputs:\n" + inputs +
		extraTriggers +
		"jobs:\n" +
		"  publish:\n" +
		"    name: производитель версии\n" +
		"    runs-on: ubuntu-latest\n" +
		"    steps:\n" +
		"      - name: предпосылки и публикация\n" +
		"        run: " + run + "\n"
}

const (
	noExtraTriggers = ""
	threeInputs     = "      version: {required: true}\n" +
		"      confirm: {required: true}\n" +
		"      publish: {required: true, type: boolean, default: false}\n"
	callProducer = "scripts/release/publish-tag.sh 'v0.1.0' --confirm 'v0.1.0'"
)

func TestReleaseWiringGateStaysSilentOnALegitimateTree(t *testing.T) {
	root := syntheticProducerTree(t,
		producerYAML(noExtraTriggers, threeInputs, callProducer),
		releaseProducerMechanism)
	a := auditReleaseWiring(root, releaseProducerFile)
	t.Logf("законный близнец: файлов %d, утверждений %d, находок %d", a.filesRead, a.assertions, len(a.findings))
	if len(a.findings) != 0 {
		t.Fatalf("на исправном дереве гейт обязан молчать, а он нашёл: %v", a.findings)
	}
	if a.assertions == 0 {
		t.Fatalf("обход пуст — контроль беспредметен")
	}
}

func TestReleaseWiringGateRedWhenTheProducerDeclarationIsMissing(t *testing.T) {
	root := t.TempDir()
	a := auditReleaseWiring(root, releaseProducerFile)
	if len(a.findings) != 1 || !strings.Contains(a.findings[0], releaseProducerFile) {
		t.Fatalf("без объявления обязана быть ровно одна находка с координатой, получено: %v", a.findings)
	}
	if a.filesRead != 0 {
		t.Fatalf("прочитанных файлов быть не может, прочитано %d", a.filesRead)
	}
	t.Logf("ось «объявления нет»: находка называет %s при нуле прочитанных", releaseProducerFile)
}

// TestReleaseWiringGateRedWhenNoStepCallsTheProducer — ГЛАВНАЯ ОСЬ.
//
// Объявление есть, триггер верный, входы объявлены — и производителя не зовёт
// никто. Ровно это состояние линия выпуска и занимала: механизм в дереве, звал
// его ноль объявлений.
func TestReleaseWiringGateRedWhenNoStepCallsTheProducer(t *testing.T) {
	root := syntheticProducerTree(t,
		producerYAML(noExtraTriggers, threeInputs, "echo 'выпускаю'"),
		releaseProducerMechanism)
	a := auditReleaseWiring(root, releaseProducerFile)
	if len(a.findings) == 0 {
		t.Fatalf("объявление без единого вызова механизма обязано быть находкой")
	}
	if !containsSubstring(a.findings, releaseProducerMechanism) {
		t.Fatalf("находка обязана называть механизм %s, получено: %v", releaseProducerMechanism, a.findings)
	}
	t.Logf("ось «никто не зовёт»: находок %d, механизм назван", len(a.findings))
}

// TestReleaseWiringGateIgnoresTheMechanismNamedOnlyInAComment — законный
// близнец главной оси и одновременно её усиление.
//
// Имя механизма стоит в КОММЕНТАРИИ объявления, а вызова нет. Гейт, читающий
// сырой текст, промолчал бы — то есть зеленел бы ровно на том состоянии, ради
// которого заведён. Комментарий узлом разобранного документа не является, и
// подмены текстом здесь быть не может by construction.
func TestReleaseWiringGateIgnoresTheMechanismNamedOnlyInAComment(t *testing.T) {
	wf := "# Здесь объясняется, зачем нужен scripts/release/publish-tag.sh\n" +
		producerYAML(noExtraTriggers, threeInputs, "echo 'выпускаю'")
	root := syntheticProducerTree(t, wf, releaseProducerMechanism)
	a := auditReleaseWiring(root, releaseProducerFile)
	if !containsSubstring(a.findings, releaseProducerMechanism) {
		t.Fatalf("имя в комментарии вызовом не является — находка обязана остаться, получено: %v", a.findings)
	}
	t.Logf("ось «имя только в комментарии»: гейт по-прежнему краснеет")
}

func TestReleaseWiringGateRedOnAnAutomaticTrigger(t *testing.T) {
	for _, trig := range []string{"  push:\n    branches: [main]\n", "  schedule:\n    - cron: '0 3 * * *'\n"} {
		root := syntheticProducerTree(t,
			producerYAML(trig, threeInputs, callProducer),
			releaseProducerMechanism)
		a := auditReleaseWiring(root, releaseProducerFile)
		if len(a.findings) != 1 {
			t.Fatalf("автоматический триггер обязан дать ровно одну находку, получено: %v", a.findings)
		}
		t.Logf("ось «автоматический триггер»: %s", a.findings[0])
	}
}

func TestReleaseWiringGateRedWhenTheIrreversibleStepNeedsNoConfirmation(t *testing.T) {
	two := "      version: {required: true}\n      publish: {required: true, type: boolean, default: false}\n"
	root := syntheticProducerTree(t,
		producerYAML(noExtraTriggers, two, callProducer),
		releaseProducerMechanism)
	a := auditReleaseWiring(root, releaseProducerFile)
	if len(a.findings) != 1 || !strings.Contains(a.findings[0], "confirm") {
		t.Fatalf("отсутствие подтверждения обязано дать одну находку с именем входа, получено: %v", a.findings)
	}
	t.Logf("ось «подтверждения не требуется»: %s", a.findings[0])
}

// TestReleaseWiringGateRedOnAWiringIntoTheVoid — шаг зовёт механизм, которого в
// дереве нет. Объявление при этом валидно, разбирается и выглядит исправным.
func TestReleaseWiringGateRedOnAWiringIntoTheVoid(t *testing.T) {
	root := syntheticProducerTree(t,
		producerYAML(noExtraTriggers, threeInputs, callProducer))
	a := auditReleaseWiring(root, releaseProducerFile)
	if !containsSubstring(a.findings, "провязка в пустоту") {
		t.Fatalf("вызов несуществующего механизма обязан быть находкой, получено: %v", a.findings)
	}
	t.Logf("ось «провязка в пустоту»: находок %d", len(a.findings))
}

func TestReleaseWiringGateRedOnANonExecutableMechanism(t *testing.T) {
	root := syntheticProducerTree(t,
		producerYAML(noExtraTriggers, threeInputs, callProducer),
		releaseProducerMechanism)
	if err := os.Chmod(filepath.Join(root, releaseProducerMechanism), 0o644); err != nil {
		t.Fatalf("режим не изменён: %v", err)
	}
	a := auditReleaseWiring(root, releaseProducerFile)
	if len(a.findings) != 1 || !strings.Contains(a.findings[0], "не исполняем") {
		t.Fatalf("неисполняемый механизм обязан дать одну находку, получено: %v", a.findings)
	}
	t.Logf("ось «бит исполнения снят»: %s", a.findings[0])
}

// TestReleaseWiringGateIgnoresAnUnrelatedScriptCalledAlongside — законный
// близнец: посторонний механизм линии, позванный рядом с производителем,
// нарушением не является. Без этой оси гейт мог бы требовать «ровно один вызов».
func TestReleaseWiringGateIgnoresAnUnrelatedScriptCalledAlongside(t *testing.T) {
	const other = "scripts/release/probe-published.sh"
	wf := producerYAML(noExtraTriggers, threeInputs, callProducer) +
		"      - name: проба годности\n        run: " + other + " 'v0.1.0'\n"
	root := syntheticProducerTree(t, wf, releaseProducerMechanism, other)
	a := auditReleaseWiring(root, releaseProducerFile)
	if len(a.findings) != 0 {
		t.Fatalf("соседний механизм линии нарушением не является, получено: %v", a.findings)
	}
	t.Logf("близнец «соседний вызов»: механизмов позвано %d, находок 0", len(distinctStrings(a.scripts)))
}

func containsSubstring(hay []string, needle string) bool {
	for _, h := range hay {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}
