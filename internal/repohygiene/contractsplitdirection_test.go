// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Контракт платформы не зависит от контракта выносимой службы доступа.
//
// # Предмет (задача `PRO-Robotech/kacho#2117`, стадия S2)
//
// Служба доступа выносится отдельным продуктом, и её контракт вправе зависеть
// от платформенного — она его потребитель. Обратное ребро означает противное:
// платформа не собирается без службы, а снять контракт службы нельзя, пока
// платформенный его импортирует. Приёмка называет это ребром между стадиями:
// объявление величин не снять, пока общий контракт учёта берёт из него тип
// области.
//
// # Что именно измерено
//
// На голове линии таких рёбер было **одно** — общий контракт учёта брал у
// службы доступа перечисление области, на которой победила величина. Обратных
// (служба → платформа) двадцать два, и они законны: перепись печатает оба числа,
// чтобы «ноль находок» было отличимо и от «ноль прочитанного», и от «прочитали
// одно дерево из двух».
//
// # Почему гейт, а не разовый предикат
//
// Условие готовности стадии называет `grep`. Он отвечает о дне, когда его
// набрали, и ни о каком другом: следующий контракт платформы, взявший тип у
// службы, вернёт ребро молча — и обнаружится это неразрешимой сборкой
// контрактов у того, кто попытается снять службу.
//
// # Чем держится способность упасть
//
// Инъекцией в обе стороны (`contractsplitdirection_injection_test.go`):
// внесённый импорт службы в платформенный контракт — находка, называющая обе
// координаты; законный импорт платформы в контракт службы молчит; форма
// `import public "…"` читается наравне с обычной.
func TestPlatformContractDoesNotImportTheServiceContract(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)
	protoRoot := filepath.Join(root, "proto")

	platform := readProtoTree(t, protoRoot, "kacho")
	service := readProtoTree(t, protoRoot, "kaname")

	require.NotZero(t, len(platform),
		"в дереве контрактов платформы не прочитано НИ ОДНОГО описания: вердикт "+
			"беспредметен, и «находок нет» здесь означало бы «ничего не прочитано»")
	require.NotZero(t, len(service),
		"в дереве контрактов выносимой службы не прочитано НИ ОДНОГО описания: "+
			"распознаватель ослеп на той стороне, ради которой гейт и заведён")

	findings, cen := repohygiene.AuditContractSplitDirection(platform, service, "kacho", "kaname")
	t.Log(cen.String())

	require.NotZero(t, cen.PlatformImports,
		"в контрактах платформы не прочитано НИ ОДНОГО импорта при %d прочитанных "+
			"описаниях: сменилась форма оператора, и молчание такого гейта неотличимо "+
			"от согласия", cen.PlatformFiles)
	require.NotZero(t, cen.ServiceToPlatform,
		"импортов службы в платформу прочитано ноль: их два десятка, и ноль здесь "+
			"означает слепой распознаватель, а не разорванную зависимость")

	require.Emptyf(t, findings,
		"направление разделения контрактов нарушено:\n  %s\n%s",
		strings.Join(findings, "\n  "), cen.String())
}

// readProtoTree читает описания контрактов одного дерева.
func readProtoTree(t *testing.T, protoRoot, tree string) []repohygiene.ContractFile {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(filepath.Join(protoRoot, tree), ".proto")
	require.NoError(t, err, "обход дерева контрактов %s", tree)

	out := make([]repohygiene.ContractFile, 0, len(files))
	for _, abs := range files {
		rel, rerr := filepath.Rel(protoRoot, abs)
		require.NoError(t, rerr, "путь %s относительно %s", abs, protoRoot)
		src, rerr := os.ReadFile(abs) // #nosec G304 -- путь из индекса git
		require.NoError(t, rerr, "чтение %s", abs)
		out = append(out, repohygiene.ContractFile{Path: filepath.ToSlash(rel), Src: string(src)})
	}
	return out
}
