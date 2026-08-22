// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1b_token_acceptance_declared_test.go — профиль развёртывания края обязан
// объявлять приём токена так, чтобы процесс с ним ПОДНЯЛСЯ.
//
// # Почему зовётся НАСТОЯЩИЙ читатель
//
// Вердикт выносит `config.Config.TokenAcceptance` — ТОТ ЖЕ предикат, который
// исполняет процесс при старте. Второй предикат, сформулированный здесь заново,
// разошёлся бы с первым молча и разошёлся бы там, где расхождение не видно: на
// вырожденном значении, где один говорит «непусто», а другой «пусто».
//
// # Почему объявление читается, а не рендер
//
// Контракт — это то, что профиль ОБЪЯВЛЯЕТ. Чтение объявления не требует ни
// зависимостей чарта, ни helm, поэтому проверка не может пропуститься.
//
// # Чего здесь НЕ проверяется — названо, чтобы «зелено» не читалось шире
//
// Не утверждается, что объявленные адреса разрешаются и отвечают: это свойство
// поднятого кластера, а не дерева. И не заменяется сборка проверяющего:
// `middleware.NewJWTVerifier` держит СВОИ требования к тому же объявлению, и их
// закрепляют пробы у самой сборки.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

const (
	// edgeChartKey — ключ, под которым профиль зонта настраивает КРАЙ.
	edgeChartKey = "api-gateway"
	// dataPlaneChartKey — ключ ПЕРВОЙ конфигурации проверяющего (её перевела Ф1).
	//
	// Имя выписано, а не выведено: чарта в дереве два, и вывести «какой из них
	// первая конфигурация» синтаксического признака нет. Ошибка в имени
	// наказуема сама — сравнивать станет нечего, и проба падает переписью,
	// а не молчит.
	dataPlaneChartKey = "registry"
)

// f1bProfile — объявление ОДНОГО профиля о приёме токена на крае.
type f1bProfile struct {
	// Name — имя файла профиля.
	Name string
	// Declares — объявлен ли перечень издателей вообще. «Не объявлено» и
	// «объявлено пустым» — разные состояния, и различает их процесс.
	Declares bool
	// AppEnv — метка окружения; она выбирает строгость стражей.
	AppEnv string
	Cfg    config.Config
}

// f1bReadProfiles читает объявления края из всех профилей зонта.
//
// Перечень профилей ВЫВОДИТСЯ из дерева, а не выписывается: рукописный список
// разошёлся бы с деревом молча, и новый профиль остался бы непроверенным.
func f1bReadProfiles(t *testing.T) []f1bProfile {
	t.Helper()
	dir := filepath.Join("..", "..", "deploy", "helm", "umbrella")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог профилей зонта не прочитан: %v", err)
	}
	var out []f1bProfile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "values") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- путь собран из имени файла в каталоге профилей дерева, не из ввода
		if rerr != nil {
			t.Fatalf("профиль %s не прочитан: %v", name, rerr)
		}
		var tree map[string]any
		if yerr := yaml.Unmarshal(raw, &tree); yerr != nil {
			t.Fatalf("профиль %s не разбирается как YAML: %v", name, yerr)
		}
		gw, _ := tree[edgeChartKey].(map[string]any)
		if gw == nil {
			continue
		}
		p := f1bProfile{Name: name}
		p.AppEnv, _ = gw["appEnv"].(string)
		p.Cfg.AppEnv = p.AppEnv
		p.Cfg.APIDomain = "api.kacho.test"
		if hydra, ok := gw["hydra"].(map[string]any); ok {
			p.Cfg.HydraIssuer, _ = hydra["issuer"].(string)
			p.Cfg.HydraJWKSURL, _ = hydra["jwksUrl"].(string)
		}
		if ta, ok := gw["tokenAcceptance"].(map[string]any); ok {
			p.Declares = true
			p.Cfg.TokenIssuers, _ = ta["issuers"].(string)
			p.Cfg.TokenIssuerKeySets, _ = ta["issuerKeySets"].(string)
			p.Cfg.PlatformTokenIssuer, _ = ta["platformIssuer"].(string)
			p.Cfg.PlatformTokenRevocationURL, _ = ta["revocationUrl"].(string)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// TestF1b_EveryProfileDeclaresAnAcceptanceTheProcessWillBoot — каждый профиль,
// называющий край, обязан объявить приём так, чтобы процесс поднялся.
func TestF1b_EveryProfileDeclaresAnAcceptanceTheProcessWillBoot(t *testing.T) {
	profiles := f1bReadProfiles(t)
	if len(profiles) == 0 {
		t.Fatalf("прочитано НОЛЬ профилей, называющих край — «ноль находок» на таком объёме " +
			"означало бы «ноль прочитанного», и молчание этой пробы сказано ни о чём")
	}

	declaring, falling := 0, 0
	for _, p := range profiles {
		bindings, err := p.Cfg.TokenAcceptance()
		if err != nil {
			t.Errorf("профиль %s объявляет приём, с которым процесс НЕ ПОДНИМЕТСЯ: %v\n\n"+
				"Отказ верен и не смягчается: место, пройденное не полностью, даёт отказ "+
				"проверки при первом же запросе вместо отказа при старте.", p.Name, err)
			continue
		}
		if len(bindings) == 0 {
			t.Errorf("профиль %s дал НОЛЬ записей приёма — «принимаем любого издателя»", p.Name)
			continue
		}
		if p.Declares {
			declaring++
		} else {
			falling++
		}
	}
	t.Logf("перепись: профилей, называющих край, %d; объявляют перечень издателей %d; "+
		"остаются на прежнем скалярном пине %d", len(profiles), declaring, falling)
}

// TestF1b_DeclaringProfilesAcceptOurIssuerWithARecordAndAnAuthority — профиль,
// объявивший нашего издателя, обязан дать ему И запись источника, И авторитет
// отзыва.
func TestF1b_DeclaringProfilesAcceptOurIssuerWithARecordAndAnAuthority(t *testing.T) {
	profiles := f1bReadProfiles(t)
	withPlatform := 0
	for _, p := range profiles {
		if !p.Declares || strings.TrimSpace(p.Cfg.PlatformTokenIssuer) == "" {
			continue
		}
		withPlatform++
		bindings, err := p.Cfg.TokenAcceptance()
		if err != nil {
			t.Errorf("профиль %s: %v", p.Name, err)
			continue
		}
		var ours *config.TokenIssuerBinding
		for i := range bindings {
			if bindings[i].Issuer == strings.TrimSpace(p.Cfg.PlatformTokenIssuer) {
				ours = &bindings[i]
			}
		}
		if ours == nil {
			t.Errorf("профиль %s объявляет нашего издателя, а записи приёма его не несут", p.Name)
			continue
		}
		if !ours.ReadRevocation {
			t.Errorf("профиль %s: наша полоса не читает отзыв — контроль, действующий только "+
				"на выдаче, отзывом не является", p.Name)
		}
		if ours.TolerateAbsentTokenType {
			t.Errorf("профиль %s: наша полоса терпит отсутствие типа токена — производитель "+
				"типа на ней мы сами", p.Name)
		}
	}
	if withPlatform == 0 {
		t.Logf("профилей, принимающих НАШЕГО издателя, ноль — на этой ревизии переход " +
			"объявлен ни в одном; проба молчит о свойстве, предмета которого нет")
	} else {
		t.Logf("перепись: профилей, принимающих нашего издателя, %d", withPlatform)
	}
}

// TestF1b_BothVerifierConfigurationsInOneProfileNameOnePlatform — обе
// конфигурации проверяющего в ОДНОМ профиле говорят об одной платформе.
//
// Имя не называет «первую» или «вторую» намеренно: опорной здесь нет, тело
// сверяет обе, и порядковое имя читалось бы как утверждение о том, чьё значение
// правильное. Нумерация конфигураций назначена заголовком пакета записи приёма
// на крае; здесь она не нужна вовсе.
//
// Расхождение здесь означало бы два места об одном предмете: край принимал бы
// одного издателя, плоскость данных другого, и обнаружилось бы это не при
// старте, а на живом токене.
func TestF1b_BothVerifierConfigurationsInOneProfileNameOnePlatform(t *testing.T) {
	dir := filepath.Join("..", "..", "deploy", "helm", "umbrella")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог профилей зонта не прочитан: %v", err)
	}
	compared := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "values") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- путь собран из имени файла в каталоге профилей дерева, не из ввода
		if rerr != nil {
			t.Fatalf("профиль %s не прочитан: %v", name, rerr)
		}
		var tree map[string]any
		if yerr := yaml.Unmarshal(raw, &tree); yerr != nil {
			t.Fatalf("профиль %s не разбирается как YAML: %v", name, yerr)
		}
		edge := f1bAcceptanceOf(tree, edgeChartKey)
		data := f1bAcceptanceOf(tree, dataPlaneChartKey)
		if edge == nil || data == nil {
			continue
		}
		compared++
		for _, field := range []string{"platformIssuer", "revocationUrl"} {
			e1, _ := edge[field].(string)
			d1, _ := data[field].(string)
			if strings.TrimSpace(e1) != strings.TrimSpace(d1) {
				t.Errorf("профиль %s: край и плоскость данных объявляют РАЗНОЕ значение %q "+
					"(край %q, плоскость данных %q) — одна платформа, а мест об одном предмете два",
					name, field, e1, d1)
			}
		}
		// Наш издатель и его запись источника обязаны совпадать дословно.
		if !f1bSameIssuerRecord(edge, data, strings.TrimSpace(fmt.Sprint(edge["platformIssuer"]))) {
			t.Errorf("профиль %s: запись источника НАШЕГО издателя различается между "+
				"конфигурациями — ключ одной из них не проверит токен, выпущенный для другой", name)
		}
	}
	t.Logf("перепись: профилей, объявляющих ОБЕ конфигурации проверяющего, %d", compared)
	if compared == 0 {
		t.Fatalf("сравнить было нечего — ни один профиль не объявляет обе конфигурации; " +
			"на таком объёме молчание пробы сказано ни о чём")
	}
}

// f1bAcceptanceOf достаёт объявление приёма у названного чарта.
func f1bAcceptanceOf(tree map[string]any, chart string) map[string]any {
	sub, _ := tree[chart].(map[string]any)
	if sub == nil {
		return nil
	}
	ta, _ := sub["tokenAcceptance"].(map[string]any)
	return ta
}

// f1bSameIssuerRecord сверяет адрес набора, объявленный за одним издателем, в
// двух объявлениях.
func f1bSameIssuerRecord(a, b map[string]any, issuer string) bool {
	if issuer == "" {
		return true
	}
	return f1bKeySetOf(a, issuer) == f1bKeySetOf(b, issuer)
}

func f1bKeySetOf(decl map[string]any, issuer string) string {
	raw, _ := decl["issuerKeySets"].(string)
	for _, pair := range strings.Split(raw, ",") {
		iss, url, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(iss) == issuer {
			return strings.TrimSpace(url)
		}
	}
	return ""
}
