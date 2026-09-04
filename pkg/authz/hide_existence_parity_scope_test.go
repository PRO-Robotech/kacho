// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package authz

// hide_existence_parity_scope_test.go — страж паритета разрешает объявление края
// по ПАКЕТУ, а не по имени файла (задача #1946).
//
// # Предмет
//
// Разрешение по координате файла делает перенос объявления в соседний файл того
// же пакета — законную правку — отказом стража. Отказ при этом не «красный», а
// «не выполнилось», поданное как красное: собственный текст прежнего стража это
// признавал («move the guard with it»).
//
// Цена здесь выше, чем у трёх соседних экземпляров того же класса: предмет
// стража — БАЙТ-ИДЕНТИЧНОСТЬ текста отказа двух мест, а различимый текст есть
// оракул существования. Страж, снятый как «вечно красный», уносит с собой
// единственное, что эту идентичность держит.
//
// # Три отказа, и все три — отказы
//
// пустой обход · объявления нет · объявлений два. Каждый называет объём
// прочитанного: «ноль находок» обязано быть отличимо от «ноль прочитанного», а
// два объявления одного имени в пакете вообще не собрались бы — но обход по
// каталогу их увидел бы, и молчать о них нельзя.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gatewayTableDir — ПАКЕТ, в котором живёт объявление. Координата пакета, а не
// файла: файл внутри пакета переносится свободно, пакет — нет.
const gatewayTableDir = "../../gateway/internal/middleware"

// TestHideExistenceParityResolvesByPackage — объявление находится в ЛЮБОМ
// не-тестовом файле пакета, а не только в историческом.
func TestHideExistenceParityResolvesByPackage(t *testing.T) {
	formats, read, err := parseFormatsInPackage(gatewayTableDir)
	if err != nil {
		t.Fatalf("страж не разрешил объявление в пакете %s: %v", gatewayTableDir, err)
	}
	if len(formats) == 0 {
		t.Fatal("объявление разобралось в ноль записей — страж прошёл бы вакуумно")
	}
	t.Logf("перепись: файлов пакета прочитано %d · записей объявления %d", read, len(formats))
}

// TestHideExistenceParityScope_CanFailAndStaysSilent — способность упасть и
// смолчать, на СИНТЕТИЧЕСКОМ пакете.
func TestHideExistenceParityScope_CanFailAndStaysSilent(t *testing.T) {
	const decl = `package middleware

var hideExistenceNotFoundFormats = map[string]string{
	"vpc_subnet": "Subnet %s not found",
}
`
	cases := []struct {
		name  string
		files map[string]string
		want  string
		why   string
	}{
		{
			name:  "законный близнец: объявление в историческом файле",
			files: map[string]string{"permission_denied_response.go": decl},
			why:   "положительный контроль: без него всякое красное ниже могло бы приходить от самого разбора",
		},
		{
			name: "объявление переехало в соседний файл пакета",
			files: map[string]string{
				"permission_denied_response.go": "package middleware\n\nfunc other() {}\n",
				"hide_existence.go":             decl,
			},
			why: "ровно предмет #1946: перенос внутри пакета — законная правка, и страж обязан " +
				"её пережить, а не отвечать «не выполнилось», поданным как красное",
		},
		{
			name:  "объявления нет вовсе",
			files: map[string]string{"permission_denied_response.go": "package middleware\n\nfunc other() {}\n"},
			want:  "объявление",
			why:   "снятие таблицы края обязано быть находкой: без неё край отвечает нейтральным текстом, различимым от текста владельца",
		},
		{
			name:  "пакета нет",
			files: nil,
			want:  "прочитано",
			why:   "«ноль находок» обязано быть отличимо от «ноль прочитанного»",
		},
		{
			name: "объявлений два",
			files: map[string]string{
				"permission_denied_response.go": decl,
				"hide_existence.go":             decl,
			},
			want: "объявлени",
			why: "два объявления одного имени не собрались бы, но обход по каталогу их видит — " +
				"молчать о них значит выбрать одно наугад",
		},
		{
			name: "объявление в ТЕСТОВОМ файле пакета за объявление не считается",
			files: map[string]string{
				"permission_denied_response.go": "package middleware\n\nfunc other() {}\n",
				"hide_existence_test.go":        decl,
			},
			want: "объявление",
			why: "отрицательный контроль охвата: фикстура пробы — не прод-объявление, и принять её " +
				"за него значит сверять паритет с синтетикой",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "middleware")
			if tc.files != nil {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				for name, body := range tc.files {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			formats, read, err := parseFormatsInPackage(dir)

			if tc.want == "" {
				if err != nil {
					t.Fatalf("страж отказал на законном пакете — первое же ложное срабатывание "+
						"снимает его.\nчто проверялось: %s\nотказ: %v", tc.why, err)
				}
				if len(formats) == 0 || read == 0 {
					t.Fatalf("контроль ничего не доказывает: записей %d · файлов прочитано %d",
						len(formats), read)
				}
				return
			}
			if err == nil {
				t.Fatalf("страж смолчал на инъекции — он НЕ способен упасть по этой оси.\n"+
					"что должно было ловиться: %s", tc.why)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("отказ не о том: ждали %q, получили %v\nчто проверялось: %s", tc.want, err, tc.why)
			}
			if !strings.Contains(err.Error(), "прочитано") {
				t.Errorf("отказ не называет объём прочитанного: %v", err)
			}
		})
	}
}
