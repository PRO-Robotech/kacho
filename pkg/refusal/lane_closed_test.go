// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// Внешний тест-пакет (refusal_test, не refusal): внутри пакета неэкспортируемые
// поля видны, и проба закрытости, написанная там, утверждала бы обратное тому,
// что проверяет, — она бы КОМПИЛИРОВАЛАСЬ. Закрытость есть свойство ГРАНИЦЫ
// пакета, поэтому и проверяется только снаружи.
package refusal_test

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/refusal"
)

// Гейт «компилятор — на пятой полосе».
//
// Что он утверждает: полосу с произвольным токеном НЕЛЬЗЯ собрать за пределами
// pkg/refusal. Это и есть механизм закрытости словаря — не соглашение и не
// проверка в рантайме, а отказ сборки.
//
// Доказан инъекцией в ОБЕ стороны, и обе стороны исполняются здесь:
//   - отрицательная: попытка собрать пятый токен обязана НЕ компилироваться, и
//     сообщение обязано называть причину закрытости, а не постороннюю опечатку;
//   - положительная: законное использование той же формы (значение словаря +
//     конструктор) обязано компилироваться. Без неё гейт зеленел бы и на пакете,
//     который не компилируется вовсе.
//
// Предпосылка гейта проверяется отдельно: нет тулчейна — гейт говорит это вслух,
// а не выдаёт «ноль находок» за «запрет держится».
func TestFifthTokenDoesNotCompileOutsideThePackage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("предпосылка гейта не выполнена: тулчейн go недоступен (%v)", err)
	}
	root := moduleRoot(t)
	probeImport := probeImportPath(t)

	cases := []struct {
		name        string
		body        string
		wantCompile bool
		wantMsg     string
	}{
		{
			name: "инъекция: пятый токен литералом структуры",
			body: `_ = refusal.Lane{token: "FIFTH_TOKEN"}`,
			// Текст ЗАХВАЧЕН у компилятора, а не угадан: гейт, ждущий
			// выдуманного сообщения, зеленел бы на любой посторонней ошибке
			// сборки.
			wantMsg: "unexported field",
		},
		{
			name:    "инъекция: приведение строки к полосе",
			body:    `_ = refusal.Lane("FIFTH_TOKEN")`,
			wantMsg: "cannot convert",
		},
		{
			name:        "законный близнец: значение словаря + конструктор",
			body:        `_ = refusal.ReferredTo.Errf(refusal.Ref{Service: "vpc"}, "x")`,
			wantCompile: true,
		},
	}

	var injections, twins int
	for _, tc := range cases {
		if tc.wantCompile {
			twins++
		} else {
			injections++
		}
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "package main\n\nimport (\n\trefusal \"" + probeImport + "\"\n)\n\nfunc main() {\n\t" + tc.body + "\n}\n"
			probe := filepath.Join(dir, "probe.go")
			require.NoError(t, os.WriteFile(probe, []byte(src), 0o600))

			cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "out"), probe)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()

			if tc.wantCompile {
				require.NoErrorf(t, err, "законная форма обязана компилироваться, вывод:\n%s", out)
				return
			}
			require.Errorf(t, err, "пятый токен собрался — словарь НЕ закрыт; вывод:\n%s", out)
			require.Containsf(t, string(out), tc.wantMsg,
				"сборка упала не по той причине — гейт обязан ловить закрытость, а не опечатку; вывод:\n%s", out)
		})
	}

	// Объём осмотренного: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного» (testing.md §Гейт на класс, п.3).
	t.Logf("проб компиляции осмотрено: %d (инъекций %d, законных близнецов %d); "+
		"путь импорта пакета выведен как %s",
		len(cases), injections, twins, probeImport)
}

// probeImportPath — путь импорта пакета, о котором говорит эта проба. Берётся у
// САМОГО пакета через его тип, а не выписывается литералом: литерал был бы
// координатой ОДНОЙ раскладки, и после переноса каталога синтетическая программа
// перестала бы собираться ВОВСЕ — а гейт закрытости объявил бы это подтверждением
// запрета, потому что сборка упала.
func probeImportPath(t *testing.T) string {
	t.Helper()
	p := reflect.TypeOf(refusal.Lane{}).PkgPath()
	require.NotEmpty(t, p, "путь импорта пакета не выведен — синтетической программе нечего импортировать")
	require.Equal(t, "refusal", path.Base(p),
		"выведен путь не того пакета (%s) — проба собрала бы программу про чужой словарь", p)
	return p
}

// moduleRoot — корень модуля, от которого исполняется проба сборки. Ищется по
// go.mod вверх от рабочего каталога: путь к пакету в дереве не выписывается,
// иначе проба сломалась бы от переноса каталога, и выглядело бы это как «запрет
// снят».
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("предпосылка гейта не выполнена: go.mod не найден вверх от %s", dir)
		}
		dir = parent
	}
}
