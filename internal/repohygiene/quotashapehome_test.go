// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/treecorpus"
	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// Пакет общей ФОРМЫ ответа учёта не объявляет НИ ОДНОЙ службы.
//
// ПРЕДМЕТ — `PRO-Robotech/kacho#2362`, решение `Д9` приёмки KAN-QUOTA-1. Служба,
// объявленная в пакете формы, принадлежит модулю квотирования, а не своему
// владельцу: вынося модуль, продукт унёс бы вместе с ним публичную
// арендаторскую поверхность, предмет которой остаётся.
//
// ПОЛОЖИТЕЛЬНЫЙ БЛИЗНЕЦ — пять владельцев платформы, объявляющих `QuotaService`
// каждый в СВОЁМ каталоге. Они обязаны молчать: без них «находок ноль» было бы
// неотличимо от «проверка не различает ничего».
func TestQuotaShapePackageDeclaresNoService(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	protoFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "proto"), ".proto")
	require.NoError(t, err, "перечень контрактов берётся у индекса дерева, а не обходом диска")

	protosSeen := 0
	shapeFilesSeen := 0
	shapeCarriesMessage := false
	declaredElsewhere := 0

	var findings []string
	for _, path := range protoFiles {
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr, "чтение %s", path)
		protosSeen++

		rel := path
		if i := strings.Index(path, "/proto/"); i >= 0 {
			rel = path[i+1:]
		}
		body := string(raw)
		services := repohygiene.ServicesDeclaredIn(body)

		if !repohygiene.InQuotaShapePackage(rel) {
			declaredElsewhere += len(services)
			continue
		}
		shapeFilesSeen++
		if strings.Contains(body, "\nmessage ") || strings.HasPrefix(body, "message ") {
			shapeCarriesMessage = true
		}
		for _, svc := range services {
			findings = append(findings, rel+" — объявляет `service "+svc+"`")
		}
	}
	sort.Strings(findings)

	require.NotZero(t, protosSeen,
		"проверка не прочитала НИ ОДНОГО контракта — «ноль находок» было бы «ноль прочитанного»")

	// ПРЕДПОСЫЛКА проверки, а не её предмет. Отрицательное утверждение молчит
	// одинаково и когда нарушений нет, и когда исчез предмет: развилка
	// `kacho#2190` вправе снять пакет формы целиком, и тогда эту проверку надо
	// снять ВМЕСТЕ с ним, а не оставить вечно зелёной (`testing.md`, гейт на
	// класс, п. 9).
	require.NotZerof(t, shapeFilesSeen,
		"пакета общей формы ответа (%s) в дереве НЕТ — предмет этой проверки исчез. "+
			"Снимите её вместе с предметом либо переведите на признак, который дерево "+
			"производит: молчание проверки без предмета неотличимо от согласия. "+
			"Осмотрено контрактов: %d", "proto/kacho/cloud/quota/", protosSeen)
	require.True(t, shapeCarriesMessage,
		"пакет общей формы не объявляет ни одного сообщения — он перестал быть формой, "+
			"и проверка о том, что он не объявляет служб, потеряла основание")
	require.NotZero(t, declaredElsewhere,
		"вне пакета формы не найдено НИ ОДНОГО объявления службы: разбор перестал ловить "+
			"предмет, и его молчание о пакете формы ничего не доказывает")

	t.Logf("перепись: контрактов осмотрено %d; файлов пакета формы %d; "+
		"объявлений службы вне пакета формы %d; в пакете формы %d",
		protosSeen, shapeFilesSeen, declaredElsewhere, len(findings))

	require.Empty(t, findings,
		"пакет общей формы ответа объявляет ФОРМУ, а не службу: служба, объявленная здесь, "+
			"принадлежит модулю квотирования, а не своему владельцу, и уедет вместе с ним "+
			"(kacho#2362, решение Д9):\n%s", strings.Join(findings, "\n"))
}
