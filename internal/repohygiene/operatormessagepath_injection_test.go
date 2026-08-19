// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// operatormessagepath_injection_test.go — доказательство, что гейт координат
// СПОСОБЕН упасть и СПОСОБЕН смолчать. Без обоих плеч зелёный гейт неотличим от
// гейта, который ничего не читает.
package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

// operatorMessageTree строит корень, где существуют все объявленные файлы оснастки, и
// кладёт в первый из них заданный текст сообщения оператору.
func operatorMessageTree(t *testing.T, message string) string {
	t.Helper()
	root := t.TempDir()
	for i, rel := range OperatorFacingScripts {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("подготовка: %v", err)
		}
		body := "#!/usr/bin/env bash\n"
		if i == 0 {
			body += message
		} else {
			// Прочие файлы называют координату, которая ЕСТЬ, — иначе корпус
			// оказался бы пуст и упало бы плечо предпосылки, а не предмета.
			body += "# см. scripts/ci-local.sh\n"
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("подготовка: %v", err)
		}
	}
	return root
}

// (а) ВЕРНУТЬ ДЕФЕКТ: координата с корнем дерева, которой нет → находка с именем.
func TestOperatorMessagePathInjection_MissingPathIsFound(t *testing.T) {
	root := operatorMessageTree(t, "  объявите слом там, где он объявляется (proto/breaking-declared.txt);\n")
	c := CollectOperatorMessagePaths(root)

	if len(c.FilesAbsent) != 0 {
		t.Fatalf("подготовка негодна: файлы перечня не созданы: %v", c.FilesAbsent)
	}
	if len(c.Findings) != 1 {
		t.Fatalf("гейт НЕ УПАЛ на возвращённом дефекте: находок %d, ожидалась 1 (путей названо %d)",
			len(c.Findings), c.PathsNamed)
	}
	if got := c.Findings[0].Path; got != "proto/breaking-declared.txt" {
		t.Fatalf("гейт упал, но назвал не ту координату: %q", got)
	}
	if c.Findings[0].File != OperatorFacingScripts[0] {
		t.Fatalf("гейт не назвал ВИНОВНИКА: %q", c.Findings[0].File)
	}
}

// (б) ЗАКОННЫЙ БЛИЗНЕЦ — три формы, на каждой гейт обязан МОЛЧАТЬ.
func TestOperatorMessagePathInjection_LegitimateFormsAreSilent(t *testing.T) {
	cases := []struct {
		name    string
		message string
	}{
		{
			// Существующая координата — прямой положительный контроль: без него
			// молчание гейта означало бы «он не читает», а не «находок нет».
			name:    "существующий путь дерева",
			message: "  локальный прогон зовётся так: scripts/ci-local.sh proto go\n",
		},
		{
			// Относительное имя: из какого каталога его запустят — гейт не знает
			// и судить не вправе. Именно эта форма дала все ложные находки
			// широкого предиката.
			name:    "относительное имя без корня дерева",
			message: "  сгенерируйте коллекции: python3 gen.py && ./run.sh\n",
		},
		{
			// Подстановка оболочки: путь собирается в рантайме, статически он
			// не координата, а шаблон.
			name:    "путь, собранный подстановкой",
			message: "  cat \"$WORK/services/does-not-exist.json\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := operatorMessageTree(t, tc.message)
			c := CollectOperatorMessagePaths(root)
			if len(c.FilesAbsent) != 0 {
				t.Fatalf("подготовка негодна: %v", c.FilesAbsent)
			}
			if len(c.Findings) != 0 {
				t.Fatalf("ЛОЖНАЯ НАХОДКА на законной форме: %+v", c.Findings)
			}
			if c.PathsNamed == 0 {
				t.Fatalf("корпус пуст — молчание не доказывает ничего")
			}
		})
	}
}

// (в) ПРЕДПОСЫЛКА: перечень, указывающий в пустоту, обязан быть ВИДЕН.
func TestOperatorMessagePathInjection_MovedSubjectIsReported(t *testing.T) {
	root := t.TempDir() // ни одного файла перечня
	c := CollectOperatorMessagePaths(root)
	if len(c.FilesAbsent) != len(OperatorFacingScripts) {
		t.Fatalf("переехавший предмет не назван: отсутствующих %d из %d",
			len(c.FilesAbsent), len(OperatorFacingScripts))
	}
	if c.FilesRead != 0 || c.PathsNamed != 0 {
		t.Fatalf("перепись лжёт о пустом дереве: прочитано %d, названо %d", c.FilesRead, c.PathsNamed)
	}
}
