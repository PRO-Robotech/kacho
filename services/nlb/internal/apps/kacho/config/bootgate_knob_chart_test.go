// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// bootgate_knob_chart_test.go — ключ, которым чарт вооружает загрузочный гейт,
// обязан быть тем самым ключом, который читает загрузчик настроек.
//
// Класс, который проба закрывает, молчалив по построению: чарт доставляет
// настройки ФАЙЛОМ, и ключ, написанный в нём мимо (`require_iam` вместо
// `require-iam`, не под тем родителем), viper просто игнорирует — процесс
// поднимается, гейт остаётся выключенным, а все прочие пробы гейта зелены,
// потому что зовут его напрямую со своим значением.
//
// Поэтому путь ключа берётся ИЗ ШАБЛОНА ЧАРТА, а не из литерала в тесте: копия
// пути в тесте согласуется сама с собой и молчит ровно тогда, когда чарт
// разошёлся с загрузчиком.
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

	"gopkg.in/yaml.v3"
)

const (
	nlbConfigMapTemplate = "../../../../deploy/templates/configmap.yaml"
	nlbSampleConfig      = "../../../../deploy/configmap-sample.yaml"
)

// nlbTemplateAction — инлайновое действие шаблона: заменяем скаляром.
var nlbTemplateAction = regexp.MustCompile(`{{[^{}]*}}`)

// nlbRenderedConfigTree — дерево настроек, которое чарт кладёт в файл. Строки
// управления (`{{- if }}`, `{{- range }}`, `{{- end }}`) выбрасываются целиком,
// инлайновые подстановки становятся скаляром: нас интересует НАЛИЧИЕ ключа, а не
// его значение.
func nlbRenderedConfigTree(t *testing.T, templatePath, blockKey string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("шаблон чарта обязан быть там, где его ищет проба (%s): %v", templatePath, err)
	}

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
		block = append(block, nlbTemplateAction.ReplaceAllString(line, "true"))
	}
	if len(block) == 0 {
		t.Fatalf("в шаблоне не нашлось блока %q — проба читает чарт, которого больше нет; "+
			"чинить надо пробу, а не считать дерево чистым", blockKey)
	}

	var tree map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(block, "\n")), &tree); err != nil {
		t.Fatalf("снятый блок настроек обязан оставаться разбираемым YAML: %v", err)
	}
	return tree
}

// TestChartArmsTheBootGateThroughTheKeyTheLoaderReads — ядро пробы.
func TestChartArmsTheBootGateThroughTheKeyTheLoaderReads(t *testing.T) {
	tree := nlbRenderedConfigTree(t, nlbConfigMapTemplate, "config.yaml: |")

	fga, ok := tree["fga"].(map[string]any)
	if !ok {
		t.Fatal("чарт обязан рендерить секцию `fga` файла настроек: без неё ручка загрузочного " +
			"гейта не доезжает до процесса вовсе, и гейт остаётся выключенным на каждом профиле")
	}
	if _, ok := fga["require-iam"]; !ok {
		t.Fatal("чарт обязан рендерить ключ `fga.require-iam` — именно его читает загрузчик " +
			"(FGAConfig.RequireIAM, mapstructure `require-iam`); ключ с другим именем viper молча " +
			"игнорирует, и гейт остаётся выключенным")
	}

	// Тот же ключ, поданный загрузчику файлом, обязан взвести поле: путь из
	// чарта не просто существует, а читается.
	//
	// Отправная точка — поставляемый образец настроек, а не голый огрызок:
	// загрузчик валидирует конфигурацию целиком и на огрызке отказывает по
	// причинам, к ручке отношения не имеющим. Ослабляется ровно одно измерение —
	// добавляется проверяемый ключ.
	raw, err := os.ReadFile(nlbSampleConfig)
	if err != nil {
		t.Fatalf("образец настроек обязан быть там, где его ищет проба (%s): %v", nlbSampleConfig, err)
	}
	var sample map[string]any
	if err := yaml.Unmarshal(raw, &sample); err != nil {
		t.Fatalf("образец настроек обязан разбираться: %v", err)
	}
	// Ключ ставится СЛИЯНИЕМ в существующую секцию, а не дописыванием второго
	// блока `fga:` в хвост: дубль ключа — ошибка разбора, и проба падала бы,
	// ничего не сказав о предмете.
	sfga, ok := sample["fga"].(map[string]any)
	if !ok {
		sfga = map[string]any{}
	}
	sfga["require-iam"] = true
	sample["fga"] = sfga
	merged, err := yaml.Marshal(sample)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, merged, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.FGA.RequireIAM {
		t.Fatal("ключ, который рендерит чарт, обязан взводить загрузочный гейт; если он этого " +
			"не делает, ручка есть, а гейта нет")
	}
}
