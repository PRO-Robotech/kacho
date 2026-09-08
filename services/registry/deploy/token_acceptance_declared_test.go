// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// token_acceptance_declared_test.go — КАЖДЫЙ развёртываемый стенд объявляет
// приём токена так, что плоскость данных с этим объявлением поднимается.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Приём identity-JWT перестал выводиться из умолчаний и стал ОБЪЯВЛЕНИЕМ: кого
// принимаем, откуда берём его набор проверочных ключей, кто читает отзыв.
// Незаполненное объявление означало бы «принимаем любого издателя», поэтому
// сервис отказывается стартовать — и это правильно.
//
// Правильный отказ становится поломкой стенда ровно тогда, когда объявление
// завели не во всех цепочках. Так и вышло: ручку объявили три профиля из
// четырёх развёртываемых, а первая фаза подъёма локального стенда идёт
// профилем, оставшимся без неё, — под уходил в перезапуск по кругу, `helm
// --wait` истекал через десять минут, и обе сквозные полосы получали «условие
// не создано» вместо вердикта. Ни одна проба этого не видела: проб, читающих
// это объявление, в дереве было НОЛЬ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЕ, А НЕ РЕНДЕР
//
// Та же причина, что у соседей в deploy/: контракт — то, что профиль
// ОБЪЯВЛЯЕТ. Проверке не нужны ни `helm`, ни скачанные подчарты, поэтому она не
// умеет пропуститься. Отдельно важно, что рендер здесь и не помог бы: `helm
// template` на пустом объявлении выводит синтаксически безупречную пару
// «имя-значение» с пустым значением — дефект виден не в манифесте, а в том, что
// процесс с этим манифестом не поднимается.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЗОВЁТСЯ НАСТОЯЩИЙ ЧИТАТЕЛЬ
//
// Вердикт выносит `config.Config.TokenAcceptance` — ТОТ ЖЕ предикат, который
// исполняет процесс при старте. Второй предикат, сформулированный здесь заново,
// разошёлся бы с первым молча и разошёлся бы там, где расхождение не видно: на
// вырожденном значении. Это уже происходило внутри самого сервиса — два
// правила об одном поле, из которых исполнялось одно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ НЕ ПРОВЕРЯЕТСЯ (названо, чтобы «зелено» не читалось шире, чем есть)
//
// Проверка не утверждает, что объявленные адреса РАЗРЕШАЮТСЯ и отвечают: это
// свойство поднятого кластера, а не дерева.
//
// И она не заменяет собой сборку проверяющего целиком: `jwks.New` держит СВОИ
// требования к тому же объявлению (непустой адресат, объявленный тип токена),
// и их закрепляют пробы у самой сборки. Предмет здесь ровно один — ОБЪЯВЛЕНИЕ
// приёма, то есть та его часть, отсутствие которой уронило стенд.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"gopkg.in/yaml.v3"
)

// stacksTablePath — единственное место в дереве, где объявлен состав стендов.
// Читаем ЕГО, а не выписываем цепочки: рукописная копия уже расходилась с
// деревом, и расхождение не краснело ни у одной из копий.
var stacksTablePath = filepath.Join("..", "..", "..", "deploy", "stacks.txt")

// deployStackChains — имя стенда → упорядоченная цепочка профилей.
func deployStackChains(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(stacksTablePath)
	if err != nil {
		t.Fatalf("таблица стендов %s не читается (%v) — предпосылка проверки исчезла, "+
			"а не дерево стало чистым", stacksTablePath, err)
	}
	out := map[string][]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, chain, ok := strings.Cut(line, ":")
		if !ok || name == "" || chain == "" {
			// Нераспознанная строка — это НЕ «стендов меньше», это «предикат
			// перестал их узнавать».
			t.Fatalf("строка таблицы стендов не разобрана: %q (%s)", line, stacksTablePath)
		}
		out[name] = strings.Split(chain, ",")
	}
	if len(out) == 0 {
		t.Fatalf("в %s нет ни одной строки стенда — проверка не вправе считать, "+
			"что стендов не осталось", stacksTablePath)
	}
	return out
}

// mergeTrees накладывает src на dst так же, как helm накладывает файлы
// значений: карты сливаются по ключам, всё остальное замещается целиком.
func mergeTrees(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if cur, ok := dst[k].(map[string]any); ok {
				dst[k] = mergeTrees(cur, sub)
				continue
			}
			dst[k] = mergeTrees(map[string]any{}, sub)
			continue
		}
		dst[k] = v
	}
	return dst
}

// chartOwnValues — умолчания САМОГО чарта реестра. Профиль накладывается на
// них, а не заменяет их: ручка, которой профиль не касался, приезжает отсюда,
// и вердикт без этого слоя относился бы к посадке, которой не существует.
func chartOwnValues(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("values.yaml")
	if err != nil {
		t.Fatalf("read values.yaml чарта реестра: %v", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("parse values.yaml чарта реестра: %v", err)
	}
	return tree
}

// effectiveRegistryValues — значения подчарта реестра для стенда: умолчания
// чарта, на которые слева направо наложены профили цепочки.
func effectiveRegistryValues(t *testing.T, chain []string) map[string]any {
	t.Helper()
	eff := mergeTrees(map[string]any{}, chartOwnValues(t))
	for _, profile := range chain {
		tree := umbrellaValues(t, profile)
		sub, ok := tree["registry"].(map[string]any)
		if !ok {
			continue
		}
		eff = mergeTrees(eff, sub)
	}
	return eff
}

// scalar — значение YAML как строка. `toStr` рядом возвращает пусто для чисел,
// а порт объявлен числом.
func scalar(v any, ok bool) string {
	if !ok || v == nil {
		return ""
	}
	if s, isStr := v.(string); isStr {
		return s
	}
	return fmt.Sprint(v)
}

// declaredAcceptance собирает из объявленного профилями то и только то, что
// читает страж старта.
func declaredAcceptance(reg map[string]any) config.Config {
	return config.Config{
		AuthMode:            scalar(digOpt(reg, "authMode")),
		TokenIssuers:        scalar(digOpt(reg, "tokenAcceptance", "issuers")),
		TokenIssuerKeySets:  scalar(digOpt(reg, "tokenAcceptance", "issuerKeySets")),
		PlatformTokenIssuer: scalar(digOpt(reg, "tokenAcceptance", "platformIssuer")),
		TokenRevocationURL:  scalar(digOpt(reg, "tokenAcceptance", "revocationUrl")),
	}
}

// TestEveryDeployedStackDeclaresATokenAcceptanceThatBoots — сама проверка.
func TestEveryDeployedStackDeclaresATokenAcceptanceThatBoots(t *testing.T) {
	requireDataplaneIsUnconditional(t)

	chains := deployStackChains(t)
	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)

	examined, withRegistry := 0, 0
	for _, name := range names {
		examined++
		reg := effectiveRegistryValues(t, chains[name])
		if enabled, ok := digOpt(reg, "enabled"); !ok || enabled != true {
			t.Logf("стенд %q: реестр не поднимается — объявлять приём нечему", name)
			continue
		}
		withRegistry++

		cfg := declaredAcceptance(reg)
		if _, err := cfg.TokenAcceptance(); err != nil {
			t.Errorf("стенд %q (цепочка %s): с объявленным приёмом плоскость данных НЕ ПОДНИМЕТСЯ — "+
				"страж старта отвечает:\n    %v\n"+
				"Объяви registry.tokenAcceptance в профиле этой цепочки. Отказ здесь верен: "+
				"незаполненный перечень принимаемых издателей означает «принимаем любого», "+
				"а не «принимаем как раньше»",
				name, strings.Join(chains[name], " → "), err)
		}
	}

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if examined == 0 || withRegistry == 0 {
		t.Fatalf("обход ничего не осмотрел: стендов=%d, из них с реестром=%d — "+
			"предикат перестал узнавать дерево, а не дерево стало чистым", examined, withRegistry)
	}
	t.Logf("осмотрено: стендов=%d, из них поднимают реестр=%d", examined, withRegistry)
}

// requireDataplaneIsUnconditional — проверка СВОЕЙ предпосылки.
//
// Проверка требует объявления от каждого стенда, поднимающего реестр, и это
// верно ровно потому, что чарт выдаёт адрес плоскости данных БЕЗУСЛОВНО:
// непустой адрес и есть то, по чему процесс решает строить проверяющего.
// Появится условие — требование станет шире предмета, и узнать об этом надо
// здесь, а не по красному стенду.
func requireDataplaneIsUnconditional(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read templates/deployment.yaml: %v", err)
	}
	const marker = "- name: KACHO_REGISTRY_DATAPLANE_ADDR"
	lines := strings.Split(string(raw), "\n")
	seen := 0
	for i, ln := range lines {
		if !strings.Contains(ln, marker) {
			continue
		}
		seen++
		// Значение выводится из порта чарта, и порт объявлен непустым: иначе
		// адрес выродился бы в одно двоеточие — непустая строка, на которой
		// процесс строит плоскость данных и не может её слушать.
		if i+1 >= len(lines) || !strings.Contains(lines[i+1], ".Values.service.dataplanePort") {
			t.Fatalf("предпосылка изменилась: адрес плоскости данных больше не выводится из "+
				"service.dataplanePort (строка %d) — пересмотри область этой проверки", i+1)
		}
	}
	if seen != 1 {
		t.Fatalf("предпосылка изменилась: %q встречается %d раз(а) вместо одного — "+
			"адрес плоскости данных стал условным, и требование объявления «у каждого стенда "+
			"с реестром» шире предмета", marker, seen)
	}
	if port := scalar(digOpt(chartOwnValues(t), "service", "dataplanePort")); strings.TrimSpace(port) == "" {
		t.Fatalf("предпосылка изменилась: service.dataplanePort в умолчаниях чарта пуст (%q) — "+
			"адрес плоскости данных вырождается, и предмет проверки надо переопределить", port)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом объявлении той же
// формы. Без положительного контроля отрицание зеленеет на страже, отвергающем
// всё: «объявление негодно» и «читатель сломан» дают одинаково красное.

func TestDeclaredAcceptance_SelfTest(t *testing.T) {
	const (
		ours   = "https://kaname.kacho.local"
		theirs = "https://hydra.example.invalid"
		oursKS = "https://kaname-internal.kacho.svc:9097/.well-known/kaname/jwks.json"
		legKS  = "https://kaname-internal.kacho.svc:9097/.well-known/jwks.json"
		revURL = "https://kaname-internal.kacho.svc:9097/internal/tokens/introspect"
	)
	acceptance := func(mode, issuers, keySets, platform, revocation string) map[string]any {
		return map[string]any{
			"enabled":  true,
			"authMode": mode,
			"tokenAcceptance": map[string]any{
				"issuers":        issuers,
				"issuerKeySets":  keySets,
				"platformIssuer": platform,
				"revocationUrl":  revocation,
			},
		}
	}

	cases := []struct {
		name    string
		reg     map[string]any
		wantErr string // подстрока, которую обязано назвать сообщение; "" = обязан молчать
	}{
		// (а) внесённый дефект — ровно тот, что уронил стенд.
		{
			"объявления нет вовсе",
			map[string]any{"enabled": true, "authMode": "dev"},
			"KACHO_REGISTRY_TOKEN_ISSUERS",
		},
		{
			"перечень вырожден: непустая строка, ноль элементов",
			acceptance("production", ",", ours+"="+oursKS, "", ""),
			"0 elements",
		},
		{
			"издатель принят, записи источника нет",
			acceptance("production", ours+","+theirs, ours+"="+oursKS, ours, revURL),
			"no declared key-set record",
		},
		{
			"наш издатель принят, авторитет отзыва не объявлен",
			acceptance("production", ours, ours+"="+oursKS, ours, ""),
			"KACHO_REGISTRY_TOKEN_REVOCATION_URL",
		},
		{
			"боевая посадка, набор ключей по открытому HTTP",
			acceptance("production", ours, ours+"=http://kaname-internal.kacho.svc:9097/keys", "", ""),
			"https://",
		},

		// (б) законные объявления ТОЙ ЖЕ формы — обязан молчать.
		{
			"боевая посадка: оба издателя, записи, отзыв",
			acceptance("production-strict", ours+","+theirs, ours+"="+oursKS+","+theirs+"="+legKS, ours, revURL),
			"",
		},
		{
			"посадка разработки: открытый HTTP допустим, наш издатель не принимается",
			acceptance("dev", theirs, theirs+"=http://kaname-internal.kacho.svc:9097/keys", "", ""),
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := declaredAcceptance(tc.reg).TokenAcceptance()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("законное объявление отвергнуто: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("внесённый дефект не пойман — проверка приняла объявление, "+
					"с которым процесс не стартует (ждали упоминания %q)", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("находка без координаты: сообщение %q не называет %q", err.Error(), tc.wantErr)
			}
		})
	}
}
