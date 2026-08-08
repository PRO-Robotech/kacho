// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package protofieldreaders_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/tools/unreadfieldaudit/protofieldreaders"
)

const stubDir = "pkg/api/kacho/cloud/vpc/v1"

var reGetter = regexp.MustCompile(`(?m)^func \(x \*(\w+)\) Get(\w+)\(\)`)

// TestIndexAttributesReadsByReceiverTypeNotByName — контроль в ОБЕ стороны на том
// самом свойстве, ради которого индекс написан.
//
// Утверждается четвёрка, невозможная при атрибуции ПО ИМЕНИ:
//
//	(1) поле F объявлено И у сообщения A, И у сообщения B (геттер есть у обоих) —
//	    без этого «не прочитано у B» истинно тождественно и не значит ничего;
//	(2) F прочитано у A                    — положительная сторона;
//	(3) B индексом ВИДЕН (у него прочитано какое-то другое поле) — иначе
//	    отсутствие в (4) означало бы слепую зону, а не различение;
//	(4) F у B НЕ прочитано                 — отрицательная сторона.
//
// Предикат, ищущий читателя по ИМЕНИ геттера, закрыл бы (4) находкой из (2):
// `GetF(` в дереве есть, и кому он принадлежит — такому предикату неизвестно.
// Поэтому существование такой четвёрки и есть доказательство, что основа
// сменилась.
//
// Утверждение сформулировано как «существует», а не про конкретное поле, чтобы не
// сгнить от законной правки продукта: на момент написания четвёркой была
// `ListSubnetsRequest.Filter` (читается) против `ListUsedAddressesRequest.Filter`
// (не читается) при живом `ListUsedAddressesRequest.SubnetId`.
//
// Если четвёрка когда-нибудь исчезнет, это НЕ повод ослабить тест: сперва проверь,
// не вернулась ли атрибуция по имени. Законная причина ровно одна — в дереве не
// осталось ни одного поля, читаемого у одного сообщения и не читаемого у другого
// с тем же именем; тогда контроль переносится на другой пакет стабов, а не
// снимается.
func TestIndexAttributesReadsByReceiverTypeNotByName(t *testing.T) {
	t.Chdir("../../..")

	declared := declaredGetters(t) // Тип -> множество полей, объявленных стабами.

	ix, err := protofieldreaders.Build("./services/vpc/...")
	if err != nil {
		t.Fatalf("индекс не построился: %v", err)
	}
	if len(ix.Errors) > 0 {
		t.Fatalf("предпосылка не выполнена — %d пакетов не протипизировано, их чтения "+
			"невидимы: %s", len(ix.Errors), strings.Join(ix.Errors, "; "))
	}
	// Положительный контроль: обход состоялся. Без него всё, что ниже, зеленело бы
	// на пустом индексе.
	if n := ix.FileCount(); n == 0 {
		t.Fatal("прочитано 0 файлов — обход не состоялся, тест ничего не утверждает")
	}
	if len(ix.Reads) == 0 {
		t.Fatal("индекс пуст — ни одного чтения, тест ничего не утверждает")
	}

	const stubPkg = "github.com/PRO-Robotech/kacho/" + stubDir
	read := map[string]map[string]bool{} // Тип -> прочитанные поля (только stubPkg).
	for key := range ix.Reads {
		pkg, typ, field, ok := split3(key)
		if !ok || pkg != stubPkg {
			continue
		}
		if read[typ] == nil {
			read[typ] = map[string]bool{}
		}
		read[typ][field] = true
	}
	if len(read) == 0 {
		t.Fatalf("в индексе нет ни одного чтения у типов %s — обход смотрел не туда",
			stubPkg)
	}

	var example string
	for typA, fields := range read {
		for field := range fields {
			for typB, declB := range declared {
				if typB == typA || !declB[field] {
					continue // (1): у B этого поля нет — сравнивать нечего.
				}
				if len(read[typB]) == 0 {
					continue // (3): B вообще не виден индексом — это слепая зона.
				}
				if read[typB][field] {
					continue // (4): поле читается и у B — пара не различающая.
				}
				example = typA + "." + field + " читается; " + typB + "." + field +
					" — нет (при живых чтениях у " + typB + ")"
			}
			if example != "" {
				break
			}
		}
		if example != "" {
			break
		}
	}
	if example == "" {
		t.Fatal("не найдено ни одной пары сообщений, где ОБЪЯВЛЕННОЕ у обоих " +
			"одноимённое поле читается у одного и не читается у другого. Индекс " +
			"перестал РАЗЛИЧАТЬ получателя: либо атрибуция вернулась к имени, либо в " +
			"дереве не осталось предмета различения — проверь первое прежде, чем " +
			"поверить во второе")
	}
	t.Logf("различение по типу получателя подтверждено: %s", example)
}

// declaredGetters — какие поля объявлены стабами у каждого сообщения. Читается из
// сгенерированного кода, а не выводится камелизацией: предикат, ошибающийся в
// имени, ошибается в единственном, ради чего существует.
func declaredGetters(t *testing.T) map[string]map[string]bool {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(stubDir, "*.pb.go"))
	if err != nil {
		t.Fatalf("glob стабов: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("в %s нет ни одного .pb.go — предпосылка теста не выполнена", stubDir)
	}
	out := map[string]map[string]bool{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("чтение %s: %v", p, err)
		}
		for _, m := range reGetter.FindAllStringSubmatch(string(b), -1) {
			if out[m[1]] == nil {
				out[m[1]] = map[string]bool{}
			}
			out[m[1]][m[2]] = true
		}
	}
	return out
}

func split3(key string) (pkg, typ, field string, ok bool) {
	pkg, rest, ok := strings.Cut(key, "|")
	if !ok {
		return "", "", "", false
	}
	typ, field, ok = strings.Cut(rest, "|")
	return pkg, typ, field, ok
}
