// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// Строка формы в сквозных пробах консоли обязана адресоваться так, чтобы
// объемлющий блок не мог сойти за неё.
//
// Почему это гейт по дереву, а не урок в одном файле: правка локатора закрывает
// экземпляр, а свойство держится только тем, что покраснеет на КОДЕ, КОТОРОГО
// ЕЩЁ НЕТ. Класс уже стоил двух проб, красных на стволе и во всех релизных
// линиях, и обе были красны с момента заведения — то есть занимали слот и не
// проверяли ничего (#636).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const consoleSpecsRel = "ui-future/e2e/specs"

// TestConsoleFormRowLocatorExcludesTheEnclosingBlock — ни одно выражение,
// выбирающее строку формы по тексту, не обходится без отсечения вложенных строк.
//
// Гейт НЕ МОЖЕТ пройти вхолостую: пустой корпус спек и ноль распознанных
// выражений — отказ, а не молчание. Объём осмотренного печатается всегда.
func TestConsoleFormRowLocatorExcludesTheEnclosingBlock(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, consoleSpecsRel)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("не прочитан каталог спек %s: %v — предмета у гейта не осталось", consoleSpecsRel, err)
	}

	specs, examined, guarded := 0, 0, 0
	var found int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}
		rel := filepath.Join(consoleSpecsRel, e.Name())
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("не прочитан %s: %v", rel, err)
		}
		specs++
		findings, n := FindUnguardedFormRowLocators(string(body))
		examined += n
		guarded += n - len(findings)
		for _, f := range findings {
			found++
			t.Error(DescribeFormRowFinding(rel, f))
		}
	}

	t.Logf("осмотрено: файлов .ts в каталоге спек %d; выражений, выбирающих строку формы по тексту, %d; "+
		"из них отсекают вложенные строки %d; находок %d",
		specs, examined, guarded, found)

	if specs == 0 {
		t.Fatalf("в %s не прочитано ни одного файла .ts — «ноль находок» здесь означало бы «ноль прочитанного»",
			consoleSpecsRel)
	}
	if examined == 0 {
		t.Fatalf("в %d файлах не распознано НИ ОДНОГО выражения, выбирающего строку формы по тексту. "+
			"Либо пробы перестали адресовать поля так, либо разбор перестал их видеть — "+
			"второе делает гейт вакуумным, и молчать он не вправе", specs)
	}
}
