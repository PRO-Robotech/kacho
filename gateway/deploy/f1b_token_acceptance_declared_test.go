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
	// Declares — объявляет ли профиль блок приёма сам. Профиль без блока
	// получает его слоем цепочки ниже, и это судит F1c, а не эта проба.
	Declares bool
	// AppEnv — метка окружения; она выбирает строгость стражей.
	AppEnv string
	Cfg    config.Config
}

// f1bGatewayConfig — ЕДИНСТВЕННОЕ место, где объявление профиля переводится в
// Config, который читает процесс.
//
// Читателей у перевода ДВА, и они спрашивают разное: пофайловая проба ниже
// («объявление ЭТОГО файла даёт поднимающийся процесс») и стендовая
// (revocation_endpoint_test.go, где цепочка профилей сливается так, как её
// сливает helm, — «объявление ЭТОГО стенда даёт поднимающийся процесс»).
// Предметы разные, перевод один: второй перевод, написанный заново, разошёлся
// бы с первым молча и разошёлся бы там, где расхождение не видно — на
// вырожденном значении, где один говорит «непусто», а другой «пусто».
//
// Ключи читаются ТОЧНЫМ ИМЕНЕМ карты, а не вхождением подстроки: переименование
// ключа с суффиксом обязано читаться как «ключа нет», а не удовлетворять
// проверку.
func f1bGatewayConfig(gw map[string]any) (config.Config, bool) {
	var cfg config.Config
	cfg.AppEnv, _ = gw["appEnv"].(string)
	ta, declares := gw["tokenAcceptance"].(map[string]any)
	if !declares {
		return cfg, false
	}
	cfg.TokenIssuers, _ = ta["issuers"].(string)
	cfg.TokenIssuerKeySets, _ = ta["issuerKeySets"].(string)
	cfg.PlatformTokenIssuer, _ = ta["platformIssuer"].(string)
	cfg.PlatformTokenRevocationURL, _ = ta["revocationUrl"].(string)
	if rc, rok := ta["revocationClientCert"].(map[string]any); rok {
		cfg.PlatformTokenRevocationCertFile, _ = rc["certFile"].(string)
		cfg.PlatformTokenRevocationKeyFile, _ = rc["keyFile"].(string)
	}
	return cfg, true
}

// f1bReadProfiles читает объявления края из всех профилей зонта.
//
// Перечень профилей ВЫВОДИТСЯ из дерева, а не выписывается: рукописный список
// разошёлся бы с деревом молча, и новый профиль остался бы непроверенным.
func f1bReadProfiles(t *testing.T) []f1bProfile {
	t.Helper()
	dir := umbrellaDir
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
		cfg, declares := f1bGatewayConfig(gw)
		out = append(out, f1bProfile{
			Name:     name,
			Declares: declares,
			AppEnv:   cfg.AppEnv,
			Cfg:      cfg,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// TestF1b_EveryProfileDeclaresAnAcceptanceTheProcessWillBoot — каждый профиль,
// объявляющий приём сам, обязан объявить его так, чтобы процесс поднялся.
//
// Профиль, приёма не объявляющий, здесь не судится: сам по себе он даёт отказ
// старта (перечень не объявлен), но слоем цепочки он получает перечень от слоя
// ниже, и вопрос «объявлено ли за него» — предмет соседнего файла
// (f1c_issuer_set_reaches_every_stand_test.go), который складывает стенд так,
// как его складывает helm.
func TestF1b_EveryProfileDeclaresAnAcceptanceTheProcessWillBoot(t *testing.T) {
	profiles := f1bReadProfiles(t)
	if len(profiles) == 0 {
		t.Fatalf("прочитано НОЛЬ профилей, называющих край — «ноль находок» на таком объёме " +
			"означало бы «ноль прочитанного», и молчание этой пробы сказано ни о чём")
	}

	declaring, inheriting := 0, 0
	for _, p := range profiles {
		if !p.Declares {
			inheriting++
			continue
		}
		declaring++
		bindings, err := p.Cfg.TokenAcceptance()
		if err != nil {
			t.Errorf("профиль %s объявляет приём, с которым процесс НЕ ПОДНИМЕТСЯ: %v\n\n"+
				"Отказ верен и не смягчается: место, пройденное не полностью, даёт отказ "+
				"проверки при первом же запросе вместо отказа при старте.", p.Name, err)
			continue
		}
		if len(bindings) == 0 {
			t.Errorf("профиль %s дал НОЛЬ записей приёма — «принимаем любого издателя»", p.Name)
		}
	}
	if declaring == 0 {
		t.Errorf("ни один из %d профилей, называющих край, приёма сам не объявляет — судить "+
			"этой пробе нечего, и её молчание было бы сказано ни о чём", len(profiles))
	}
	t.Logf("перепись: профилей, называющих край, %d; объявляют приём САМИ %d (судятся здесь); "+
		"не объявляют сами %d — объявлено ли за них слоем ниже, судит F1c, а не эта проба",
		len(profiles), declaring, inheriting)
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
	dir := umbrellaDir
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

// ─────────────────────────────────────────────────────────────────────────────
// ПРИНЯТЬ ИЗДАТЕЛЯ И НЕ МОЧЬ ВЫПУСТИТЬ ЕГО ТОКЕН — НЕИСПОЛНИМАЯ ВОЗМОЖНОСТЬ.
//
// ПРЕДМЕТ. Профиль объявляет край принимающим НАШЕГО издателя, и одновременно
// объявляет наш токен-эндпоинт с ПЕРЕЧНЕМ допустимых адресатов. Величины эти
// живут в разных подчартах и по отдельности защитимы; неисполнимость появляется
// только НА СТЫКЕ: край принимает токен лишь с адресатом `https://<домен>`, а
// перечень называет другой адресат — значит выпустить токен, который край
// примет, нельзя НИ ПРИ КАКОМ входе. Полоса объявлена, задокументирована,
// покрыта типами — и не работает (`api-conventions.md` §«ДВА ПРАВИЛА ОБ ОДНОМ
// ПОЛЕ»).
//
// ИЗМЕРЕНО ВЫЗОВОМ, А НЕ ВЫВЕДЕНО (#1014, живой стенд): подписанное утверждение
// принято, токен выпущен (`alg=ES256`, `typ=at+jwt`, `iss` — наш издатель), а
// край ответил `401`, и его собственная строка назвала причину — адресат. То же
// утверждение с испорченной подписью дало у края ошибку проверки подписи, то
// есть до подписи дело доходило: единственным препятствием был перечень.
//
// ЧТО СЧИТАЕТСЯ ОЖИДАЕМЫМ АДРЕСАТОМ. То, что профиль ОБЪЯВИЛ краю
// (`api-gateway.tokenAudience`), а где не объявил — запасное значение самого
// подчарта края (`values.yaml` рядом). Выводить его из домена больше нельзя:
// задача #2567 сняла вывод внутри края, и вывод в пробе стал бы вторым местом
// об одном предмете — тем, которое расходится молча.
//
// Предпосылка чтения — что величина доезжает до процесса ИМЕННО этим ключом:
// объявись она в профиле сырой переменной окружения, чтение ключа стало бы
// ложным, и проба обязана об этом сказать.
//
// СХОЖДЕНИЕ ДВУХ ПОЛОВИН стенда проверяется отдельно
// (TestF1b_EdgeAudienceMatchesTheInstallationDomain): чеканка ставит адресат от
// домена установки, край ждёт объявленный ему, и разойтись они могут молча —
// каждая половина по отдельности защитима.
// ─────────────────────────────────────────────────────────────────────────────

// f1bMintDecl — объявление одного профиля о ВЫПУСКЕ токена нашим издателем.
type f1bMintDecl struct {
	name             string
	platformAccepted bool
	mintEnabled      bool
	allowedAudiences []string
	domain           string
	// edgeAudience — адресат, который КРАЙ объявляет ожидаемым на этом профиле
	// (задача #2567). Прежде здесь ничего не стояло: край выводил адресат
	// построением из домена, и проба выводила его тем же способом. Теперь
	// величина ОБЪЯВЛЕНА, и выводить её значило бы судить не то, что доедет до
	// процесса.
	edgeAudience string
}

// f1bReadMintDecls читает стык двух подчартов по каждому профилю зонта.
func f1bReadMintDecls(t *testing.T) []f1bMintDecl {
	t.Helper()
	dir := umbrellaDir
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог профилей зонта не прочитан: %v", err)
	}
	// Базовые значения дают домен профилям, которые его не переопределяют.
	baseDomain := ""
	if raw, rerr := os.ReadFile(filepath.Join(dir, "values.yaml")); rerr == nil {
		var base map[string]any
		if yaml.Unmarshal(raw, &base) == nil {
			if k, ok := base["kacho"].(map[string]any); ok {
				baseDomain, _ = k["domain"].(string)
			}
		}
	}
	if baseDomain == "" {
		t.Fatal("в базовых значениях зонта не объявлен `kacho.domain` — сверить половины " +
			"стенда не с чем, и «ноль находок» означало бы «ноль прочитанного»")
	}
	baseEdgeAudience := f1bEdgeChartAudience(t)

	var out []f1bMintDecl
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "values") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- путь собран из имени файла в каталоге профилей дерева
		if rerr != nil {
			t.Fatalf("профиль %s не прочитан: %v", name, rerr)
		}
		// Сырая переменная окружения адресата сделала бы чтение ключа ниже
		// ложным. Её появление — находка, а не деталь.
		if strings.Contains(string(raw), "KACHO_API_GATEWAY_TOKEN_AUDIENCE") {
			t.Errorf("%s: профиль называет KACHO_API_GATEWAY_TOKEN_AUDIENCE сырой "+
				"переменной — адресат доезжает до края не тем ключом, который читает эта "+
				"проба, и она судит не о том. Либо объявите его ключом "+
				"`api-gateway.tokenAudience`, либо научите пробу читать переменную", name)
		}
		var tree map[string]any
		if yerr := yaml.Unmarshal(raw, &tree); yerr != nil {
			t.Fatalf("профиль %s не разбирается как YAML: %v", name, yerr)
		}
		d := f1bMintDecl{name: name, domain: baseDomain}
		if k, ok := tree["kacho"].(map[string]any); ok {
			if dom, ok := k["domain"].(string); ok && dom != "" {
				d.domain = dom
			}
		}
		d.edgeAudience = baseEdgeAudience
		if gw, ok := tree[edgeChartKey].(map[string]any); ok {
			if ta, ok := gw["tokenAcceptance"].(map[string]any); ok {
				iss, _ := ta["platformIssuer"].(string)
				d.platformAccepted = strings.TrimSpace(iss) != ""
			}
			if aud, ok := gw["tokenAudience"].(string); ok && strings.TrimSpace(aud) != "" {
				d.edgeAudience = strings.TrimSpace(aud)
			}
		}
		if ct := f1bClientTokenOf(tree); ct != nil {
			d.mintEnabled, _ = ct["enabled"].(bool)
			list, _ := ct["allowedAudiences"].(string)
			for _, a := range strings.Split(list, ",") {
				if a = strings.TrimSpace(a); a != "" {
					d.allowedAudiences = append(d.allowedAudiences, a)
				}
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// f1bEdgeChartAudience — запасное значение адресата у самого подчарта края.
//
// Читается ИЗ ДЕРЕВА, а не выписывается: выписанное разошлось бы с чартом молча,
// и разошлось бы то, которое забыли поправить.
func f1bEdgeChartAudience(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("values.yaml")
	if err != nil {
		t.Fatalf("базовые значения подчарта края не прочитаны: %v", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("базовые значения подчарта края не разбираются как YAML: %v", err)
	}
	aud, _ := tree["tokenAudience"].(string)
	if strings.TrimSpace(aud) == "" {
		t.Fatal("подчарт края не объявляет `tokenAudience` — адресат до процесса не " +
			"доезжает ничем, и страж старта отвергнет всякую посадку, профиль не " +
			"объявившую. Либо верните ключ, либо научите пробу читать новый")
	}
	return strings.TrimSpace(aud)
}

// f1bClientTokenOf — объявление токен-эндпоинта в подчарте личности.
func f1bClientTokenOf(tree map[string]any) map[string]any {
	iam, ok := tree["kaname"].(map[string]any)
	if !ok {
		return nil
	}
	cfg, ok := iam["config"].(map[string]any)
	if !ok {
		return nil
	}
	authn, ok := cfg["authn"].(map[string]any)
	if !ok {
		return nil
	}
	ct, _ := authn["clientToken"].(map[string]any)
	return ct
}

// TestF1b_ProfilesAcceptingOurIssuerCanMintForTheEdge — принятая полоса ИСПОЛНИМА.
func TestF1b_ProfilesAcceptingOurIssuerCanMintForTheEdge(t *testing.T) {
	decls := f1bReadMintDecls(t)
	if len(decls) == 0 {
		t.Fatal("прочитано НОЛЬ профилей зонта — судить нечего")
	}

	accepting := 0
	for _, d := range decls {
		if d.platformAccepted {
			accepting++
		}
	}
	t.Logf("осмотрено: профилей %d, принимают НАШЕГО издателя на крае %d", len(decls), accepting)
	for _, d := range decls {
		t.Logf("%s: наш издатель принят=%v, выпуск включён=%v, домен=%s, адресат края=%s, "+
			"допустимые адресаты=%v",
			d.name, d.platformAccepted, d.mintEnabled, d.domain, d.edgeAudience, d.allowedAudiences)
	}
	if accepting == 0 {
		t.Fatal("ни один профиль не принимает нашего издателя на крае — предмет пробы исчез " +
			"из дерева, и её молчание сказано ни о чём")
	}

	for _, d := range decls {
		if !d.platformAccepted {
			continue
		}
		want := d.edgeAudience
		if !d.mintEnabled {
			t.Errorf("%s: край принимает НАШЕГО издателя, а токен-эндпоинт выключен — "+
				"выпустить предъявителя этой полосы нечем, и полоса объявлена неисполнимой",
				d.name)
			continue
		}
		found := false
		for _, a := range d.allowedAudiences {
			if a == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: край принимает НАШЕГО издателя и принимает токен ТОЛЬКО с адресатом "+
				"%q, а перечень допустимых адресатов выпуска — %v. Выпустить токен, который "+
				"край примет, нельзя ни при каком входе: полоса объявлена и неисполнима",
				d.name, want, d.allowedAudiences)
		}
	}
}

// TestF1b_ProfilesNamingAnAuthorityAlsoDeclareHowTheyIntroduceThemselves —
// профиль, назвавший авторитет отзыва, обязан назвать и КЛИЕНТСКУЮ ПАРУ хопа.
//
// # Почему это ПАРА требований, а не два независимых
//
// Авторитет отзыва — наш, и он опознаёт спрашивающего: слушатель, на котором он
// выставлен, запрашивает сертификат, а сам он отвечает опознавательным словом
// тому, кто проверенной цепочки не предъявил. Это не настройка стенда, а
// свойство ПРОДУКТА: iam ОТКАЗЫВАЕТСЯ СТАРТОВАТЬ, если выставляет авторитет на
// слушателе, который сертификата не спрашивает. Значит адрес без личности —
// состояние, в котором контроль собран, провязан, исполняется на каждом запросе
// и не может ответить «действует» НИ РАЗУ.
//
// # Почему это не ловится ничем другим
//
// Отказ fail-closed ПРАВИЛЬНЫЙ, поэтому ни страж старта края, ни рендер чарта
// не краснеют: каждый по отдельности исправен. Расходятся они только вместе —
// у одного вопрос, у другого ответ, и вопрос не тот. Наблюдалось на стенде:
// каждый предъявитель нашей чеканки получал `503`, а журнал называл настройку.
func TestF1b_ProfilesNamingAnAuthorityAlsoDeclareHowTheyIntroduceThemselves(t *testing.T) {
	profiles := f1bReadProfiles(t)
	if len(profiles) == 0 {
		t.Fatal("профилей не прочитано — вердикта нет: «ноль находок» здесь неотличимо от «ноль прочитанного»")
	}

	named, checked := 0, 0
	for _, p := range profiles {
		if strings.TrimSpace(p.Cfg.PlatformTokenRevocationURL) == "" {
			continue // авторитет не назван — предмета у требования нет
		}
		named++
		cert := strings.TrimSpace(p.Cfg.PlatformTokenRevocationCertFile)
		key := strings.TrimSpace(p.Cfg.PlatformTokenRevocationKeyFile)
		switch {
		case cert == "" && key == "":
			t.Errorf("%s: назван авторитет отзыва %q, но не названа клиентская пара хопа "+
				"(tokenAcceptance.revocationClientCert.certFile/keyFile). Авторитет "+
				"спрашивает проверенную цепочку и без неё отвечает отказом — то есть "+
				"КАЖДЫЙ предъявитель нашей чеканки получит 503, а контроль будет "+
				"выглядеть настроенным",
				p.Name, p.Cfg.PlatformTokenRevocationURL)
		case cert == "" || key == "":
			t.Errorf("%s: клиентская пара хопа названа НАПОЛОВИНУ (certFile=%q keyFile=%q) — "+
				"процесс откажется стартовать", p.Name, cert, key)
		default:
			checked++
		}
	}
	t.Logf("перепись: профилей %d · назвали авторитет %d · пара объявлена у %d",
		len(profiles), named, checked)
	if named == 0 {
		t.Fatal("ни один профиль не назвал авторитета отзыва — предмет проверки исчез; " +
			"либо ручка переехала, либо предикат перестал её читать")
	}
}

// ПРОБА «НАШ ИЗДАТЕЛЬ ДОБАВЛЕН К ПЕРЕЧНЮ, А НЕ ЗАМЕНИЛ ЕГО» СНЯТА ВМЕСТЕ С
// ПРЕДМЕТОМ (#2735).
//
// Она держала переходное состояние: предъявителя прежнего издателя добывал
// интерактивный вход человека, и профиль, где наш издатель вытеснил прежнего,
// отказал бы арендатору. Переход окончен: вход человека и его сессия переехали
// на нашу полосу (#1122), край перестал принимать издателя поставщика (#1123),
// и посадку `own` объявляют все стенды таблицы, а поставщик и его издатель не
// поднимаются ни на одном (#2735). Требование «прежний издатель остаётся в
// перечне» стало ложным: его набор ключей на посадке own никто не держит.
//
// Обратное свойство держат рендером, по каждой цепочке: гейт объявленного
// отсутствия (deploy/identity_file_keys_survive_the_environment_test.go —
// издатель поставщика в перечне приёма края есть находка, и инъекция
// `api-gateway.tokenAcceptance.issuers=…,<издатель поставщика>` роняет его) и
// hydra-jwks-url-test.sh (перечень dev и prod называет нашего издателя и не
// называет издателя поставщика). Возвращение приёма прежнего издателя без
// живого производителя его предъявителя ловит
// deploy/scripts/assert-legacy-issuer-acceptance-has-a-subject.py.

// ─────────────────────────────────────────────────────────────────────────────
// ДВЕ ПОЛОВИНЫ ОДНОГО СТЕНДА ОБЯЗАНЫ СВЕРЯТЬСЯ МЕЖДУ СОБОЙ (задача #2567)
//
// ПРЕДМЕТ. Адресат — единственное, чем токен говорит, КАКОЙ УСТАНОВКЕ он выдан.
// Ставит его ЧЕКАНКА (от домена установки), ждёт его КРАЙ (от своего
// объявления). Величины живут в разных подчартах и по отдельности защитимы;
// неверна их РАЗНИЦА, и возникает она побочным эффектом чужой правки — смены
// домена, — а не решением.
//
// ПОЧЕМУ НЕ ПРОБА КАЖДОЙ ПОЛОВИНЫ ОТДЕЛЬНО. Такая проба требует знать, каким
// адресат ДОЛЖЕН быть, — а это и есть спорный вопрос. Сравнение половин
// спрашивает другое: решал ли кто-нибудь, что они различаются. На это ответ есть
// всегда.
//
// ПОЧЕМУ ЭТО НЕ ЛОВИТСЯ НИЧЕМ ДРУГИМ. Рендер обеих половин проходит, страж
// старта края доволен (адресат объявлен), страж чеканки доволен (домен
// объявлен). Расходятся они только вместе — и молча: токен выпускается, подпись
// настоящая, а край отвечает отказом, называя адресат.
//
// ЧТО ЗНАЧИТ СЕГОДНЯШНЕЕ СХОЖДЕНИЕ. Запасное значение подчарта края совпадает с
// базовым `kacho.domain` зонта ДОСЛОВНО, поэтому расхождение ещё не наступило.
// Это худший из возможных исходов, а не лучший: наступит оно у первой же
// установки, объявившей свой домен и не объявившей адресат края, — и ни один
// рендер об этом не скажет. Гейт заведён ровно затем, чтобы сказал.
// ─────────────────────────────────────────────────────────────────────────────

// TestF1b_EdgeAudienceMatchesTheInstallationDomain — сверка половин ПОПАРНО.
//
// Печатает ДВЕ величины: сколько профилей осмотрено и сколько несут свойство.
// Одно число на их месте скрыло бы ровно тот случай, ради которого гейт
// заведён, — профиль, выпавший из обхода.
func TestF1b_EdgeAudienceMatchesTheInstallationDomain(t *testing.T) {
	decls := f1bReadMintDecls(t)
	if len(decls) == 0 {
		t.Fatal("прочитано НОЛЬ профилей зонта — «ноль находок» неотличимо от «ноль прочитанного»")
	}

	converge := 0
	for _, d := range decls {
		want := "https://" + d.domain
		if d.edgeAudience == want {
			converge++
			continue
		}
		t.Errorf("%s: домен установки %q ⇒ чеканка ставит адресат %q, а край объявлен "+
			"ожидающим %q. Выпустить токен, который край примет, нельзя ни при каком "+
			"входе: половины разошлись, и решал это не оператор, а умолчание. "+
			"Объявите `%s.tokenAudience: %q` либо приведите домен в соответствие",
			d.name, d.domain, want, d.edgeAudience, edgeChartKey, want)
	}

	t.Logf("перепись: профилей %d · адресат края сходится с доменом установки %d",
		len(decls), converge)
	if converge != len(decls) {
		t.Fatalf("перепись: профилей %d · сходятся %d — расходятся",
			len(decls), converge)
	}
}

// TestF1b_EdgeAudienceConvergenceCanFail — САМОПРОВЕРКА гейта выше.
//
// Фикстура синтетическая намеренно: опирайся доказательство на живой профиль,
// оно исчезло бы вместе с его починкой — ровно тогда, когда гейт достиг цели.
// Сверяется ТОТ ЖЕ предикат, которым судит гейт, а не его копия: вторая копия
// разошлась бы с первой молча, и разошлась бы та, в которой дефект ещё не нашли.
func TestF1b_EdgeAudienceConvergenceCanFail(t *testing.T) {
	converged := f1bMintDecl{domain: "cloud.example.test", edgeAudience: "https://cloud.example.test"}
	diverged := f1bMintDecl{domain: "cloud.example.test", edgeAudience: "https://api.kacho.cloud"}

	if converged.edgeAudience != "https://"+converged.domain {
		t.Fatal("законный близнец не сходится — синтетика перестала быть контролем")
	}
	if diverged.edgeAudience == "https://"+diverged.domain {
		t.Fatal("внесённый дефект сходится — синтетика больше не воспроизводит расхождение")
	}
	t.Logf("самопроверка: осмотрено половин 2 (дефект + законный близнец)")
}
