// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_readme_names_every_wrapping_secret_test.go — раздел подготовки секретов
// README подчарта службы личности называет КАЖДЫЙ ключ обёртки, который под
// получает из секрета (kacho#3166).
//
// Раздел «must be provisioned BEFORE first-deploy» называл один ключ — обёртки
// подписного ключа, — а ключ обёртки второго фактора обязателен на каждой
// установке, и без него старт отказывает. Цена — лишняя неудачная выкатка.
//
// Перечень выводится из шаблона пода (ссылки `.Values.platform.iam.<x>.
// encKeySecretName`), а имя и ключ секрета — из базовых значений подчарта:
// новый ключ обёртки в шаблоне без строки README краснит пробу сам.
package umbrella_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var wrappingSecretRef = regexp.MustCompile(`\.Values\.platform\.iam\.([A-Za-z]+)\.encKeySecretName`)

// wrappingSecret — координата секрета ключа обёртки: имя объекта и ключ в нём.
type wrappingSecret struct{ Knob, Name, Key string }

// judgeReadmeSecrets — находки: секрет, которого раздел подготовки не называет
// координатой (имя объекта и ключ).
func judgeReadmeSecrets(section string, secrets []wrappingSecret) []string {
	var out []string
	for _, s := range secrets {
		if !strings.Contains(section, "`"+s.Name+"`") || !strings.Contains(section, "`"+s.Name+"/"+s.Key+"`") {
			out = append(out, "раздел подготовки секретов README не называет секрет ключа обёртки "+
				s.Knob+" координатой `"+s.Name+"/"+s.Key+"`")
		}
	}
	return out
}

// provisioningSection — раздел подготовки секретов до первой выкатки.
func provisioningSection(readme string) string {
	start := strings.Index(readme, "## Sealed-secret integration")
	if start < 0 {
		return ""
	}
	rest := readme[start+3:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func TestKanameReadmeNamesEveryWrappingSecret(t *testing.T) {
	tpl, err := os.ReadFile(filepath.Join(iamChartDir, "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("шаблон пода не прочитан: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(iamChartDir, "README.md"))
	if err != nil {
		t.Fatalf("README подчарта не прочитан: %v", err)
	}
	section := provisioningSection(string(readme))
	if section == "" {
		t.Fatal("раздел «Sealed-secret integration» в README подчарта не найден")
	}
	values := filepath.Join(iamChartDir, "values.yaml")
	seen := map[string]bool{}
	var secrets []wrappingSecret
	for _, m := range wrappingSecretRef.FindAllStringSubmatch(string(tpl), -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		secrets = append(secrets, wrappingSecret{
			Knob: m[1],
			Name: readNested(t, values, "platform", "iam", m[1], "encKeySecretName"),
			Key:  readNested(t, values, "platform", "iam", m[1], "encKeySecretKey"),
		})
	}
	t.Logf("перепись: ключей обёртки в шаблоне пода %d", len(secrets))
	if len(secrets) == 0 {
		t.Fatal("в шаблоне пода не найдено ни одной ссылки на секрет ключа обёртки — обход пуст")
	}
	for _, f := range judgeReadmeSecrets(section, secrets) {
		t.Error(f)
	}
}

func TestKanameReadmeSecretsInjection_MissingSecondFactorIsFound(t *testing.T) {
	section := "`kaname-jwks-enc-key` · `kaname-jwks-enc-key/enc_key`"
	got := judgeReadmeSecrets(section, []wrappingSecret{
		{"jwks", "kaname-jwks-enc-key", "enc_key"},
		{"secondFactor", "kaname-second-factor-enc-key", "enc_key"},
	})
	if len(got) != 1 || !strings.Contains(got[0], "secondFactor") {
		t.Fatalf("неназванный ключ второго фактора не найден: %v", got)
	}
}

func TestKanameReadmeSecretsInjection_FullListIsSilent(t *testing.T) {
	section := "`kaname-jwks-enc-key` `kaname-jwks-enc-key/enc_key` " +
		"`kaname-second-factor-enc-key` `kaname-second-factor-enc-key/enc_key`"
	got := judgeReadmeSecrets(section, []wrappingSecret{
		{"jwks", "kaname-jwks-enc-key", "enc_key"},
		{"secondFactor", "kaname-second-factor-enc-key", "enc_key"},
	})
	if len(got) != 0 {
		t.Fatalf("полный перечень назван находкой: %v", got)
	}
}
