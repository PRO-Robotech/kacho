// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// bootgate_knob_chart_test.go — ключ, которым чарт вооружает загрузочный гейт,
// обязан быть тем самым ключом, который читает загрузчик настроек.
//
// Соседние пробы этого пакета уже утверждают ДВЕ трети цепочки: что умолчание
// ручки false и что переменная окружения `KACHO_VPC_REQUIRE_IAM` доезжает до
// поля. Не утверждала ничего ровно та треть, где ошибка молчалива: чарт
// доставляет настройки ФАЙЛОМ, и ключ, написанный в нём с опечаткой
// (`require-iam` вместо `require`), viper просто игнорирует — процесс
// поднимается, гейт остаётся выключенным, и ни один тест этого не замечает.
//
// Поэтому проба читает ШАБЛОН ЧАРТА и берёт путь ключа оттуда, а не из
// собственного литерала: копия пути в тесте согласуется сама с собой и молчит
// ровно тогда, когда чарт разошёлся с загрузчиком.
//
// Действия шаблона снимаются, а не исполняются: helm здесь не нужен и не должен
// быть нужен — проба, требующая внешнего инструмента, пропускается там, где его
// нет, а пропускающаяся проба на измерении «ключа нет вовсе» бесполезна по
// построению.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
)

const vpcConfigMapTemplate = "../../../../deploy/templates/configmap.yaml"

// templateAction — инлайновое действие шаблона: заменяем скаляром.
var templateAction = regexp.MustCompile(`{{[^{}]*}}`)

// renderedConfigTree — дерево настроек, которое чарт кладёт в файл. Действия
// шаблона снимаются: строки-управление (`{{- if }}`, `{{- range }}`, `{{- end }}`)
// выбрасываются целиком, инлайновые подстановки становятся скаляром. Нас
// интересует НАЛИЧИЕ ключа, а не его значение.
func renderedConfigTree(t *testing.T, templatePath, blockKey string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(templatePath)
	require.NoError(t, err, "шаблон чарта обязан быть там, где его ищет проба")

	var block []string
	var inBlock bool
	var blockIndent int
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if strings.HasPrefix(trimmed, blockKey) {
				inBlock = true
				blockIndent = len(line) - len(strings.TrimLeft(line, " "))
			}
			continue
		}
		if trimmed != "" && len(line)-len(strings.TrimLeft(line, " ")) <= blockIndent {
			break // блок кончился
		}
		if strings.HasPrefix(trimmed, "{{") {
			continue // управляющее действие — выбрасываем строку целиком
		}
		block = append(block, templateAction.ReplaceAllString(line, "true"))
	}
	require.NotEmpty(t, block,
		"в шаблоне не нашлось блока %q — проба читает чарт, которого больше нет; чинить надо пробу, а не считать дерево чистым", blockKey)

	var tree map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(strings.Join(block, "\n")), &tree),
		"снятый блок настроек обязан оставаться разбираемым YAML")
	return tree
}

// TestChartArmsTheBootGateThroughTheKeyTheLoaderReads — ядро пробы.
func TestChartArmsTheBootGateThroughTheKeyTheLoaderReads(t *testing.T) {
	tree := renderedConfigTree(t, vpcConfigMapTemplate, "config.yaml: |")

	iam, ok := tree["iam"].(map[string]any)
	require.True(t, ok,
		"чарт обязан рендерить секцию `iam` файла настроек: без неё ручка загрузочного гейта не доезжает до процесса вовсе, и гейт остаётся выключенным на каждом профиле")
	_, ok = iam["require"]
	require.True(t, ok,
		"чарт обязан рендерить ключ `iam.require` — именно его читает загрузчик (IAMConfig.Require, mapstructure `require`); ключ с другим именем viper молча игнорирует, и гейт остаётся выключенным")

	// Тот же ключ, поданный загрузчику файлом, обязан взвести поле. Это и есть
	// вторая половина утверждения: путь из чарта — не просто существует, а
	// читается.
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("iam:\n  require: true\n"), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.True(t, cfg.IAM.Require,
		"ключ, который рендерит чарт, обязан взводить загрузочный гейт; если он этого не делает, ручка есть, а гейта нет")
}
