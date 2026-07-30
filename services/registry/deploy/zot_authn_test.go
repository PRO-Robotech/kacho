// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// zot_authn_test.go — хранилище слоёв обязано спрашивать предъявителя.
//
// Что тут защищается. Реестр решает права НА КАЖДЫЙ запрос плоскости данных
// (проверка подписи docker-Bearer'а + per-request Check) и только затем
// проксирует в хранилище слоёв. Если само хранилище отвечает любому, кто до него
// дозвонился, то весь этот контроль — один хоп в объезд: перечисление
// репозиториев всех тенантов, выгрузка чужих слоёв, подмена образа под чужим
// тегом и удаление содержимого становятся доступны процессу в сети подов. Ровно
// поэтому «internal = trusted» в этом продукте объявлено запрещённым допущением
// (security.md §1.4), а соседним хранилищам (база, движок прав) боевой профиль
// сегментацию выдаёт.
//
// Почему проба — часть предмета, а не косметика. Неаутентифицированный httpGet
// /v2/, считающий успехом 2xx, СЛУЖИЛ доказательством открытости: при включённой
// аутентификации хранилище отвечает на этот путь отказом, и под с такой пробой
// никогда не стал бы Ready. То есть работающий стенд сам подтверждал дыру.
// Поэтому проба переведена на проверку сокета: она не носит учётных данных (в
// шаблоне пода их и нельзя носить — это не переменная окружения) и больше ничего
// про открытость не утверждает.
package deploy_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// renderPassword — значение, которым рендер-гейты удовлетворяют требование
// учётных данных. Намеренно выглядит чужим (не похоже на настоящий пароль),
// чтобы правдоподобная фикстура не выдавала себя за рабочую настройку.
const renderPassword = "render-guard-not-a-real-password"

// zotConfigJSON вытаскивает config.json хранилища слоёв из рендера.
func zotConfigJSON(t *testing.T, rendered string) map[string]any {
	t.Helper()
	doc := docOf(t, rendered, "statefulset-zot.yaml", "kind: ConfigMap")
	const marker = "config.json: |"
	i := strings.Index(doc, marker)
	if i < 0 {
		t.Fatalf("zot ConfigMap carries no config.json:\n%s", doc)
	}
	var b strings.Builder
	for _, line := range strings.Split(doc[i+len(marker):], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(line, "    ") {
			break
		}
		b.WriteString(trimmed)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(b.String()), &cfg); err != nil {
		t.Fatalf("zot config.json is not valid JSON (%v): %s", err, b.String())
	}
	return cfg
}

// TestZotConfig_DemandsAuthentication — собираемый config.json обязан нести
// http.auth: без него хранилище слоёв обслуживает любого, кто до него дозвонился.
func TestZotConfig_DemandsAuthentication(t *testing.T) {
	cfg := zotConfigJSON(t, helmTemplate(t, "zot.auth.password="+renderPassword))

	httpBlock, ok := cfg["http"].(map[string]any)
	if !ok {
		t.Fatalf("zot config has no http block: %v", cfg)
	}
	auth, ok := httpBlock["auth"].(map[string]any)
	if !ok {
		t.Fatalf("zot config declares NO http.auth — the layer store accepts every request "+
			"without a bearer, and the whole data-plane authorization is one hop away: %v", httpBlock)
	}
	htpasswd, ok := auth["htpasswd"].(map[string]any)
	if !ok {
		t.Fatalf("zot http.auth carries no htpasswd credential source: %v", auth)
	}
	if p, _ := htpasswd["path"].(string); p == "" {
		t.Fatalf("zot http.auth.htpasswd has an empty path: %v", htpasswd)
	}
}

// TestZotStatefulSet_MountsCredentialFileAndRollsOnRotation — файл учётных данных
// обязан быть примонтирован по объявленному пути, а его смена — перекатывать под:
// хранилище читает htpasswd на старте, поэтому правка секрета без переката
// оставляет процесс жить со старым паролем (тот же класс, что инцидент посадки
// storage: хранимое намерение расходится с исполняемым).
func TestZotStatefulSet_MountsCredentialFileAndRollsOnRotation(t *testing.T) {
	out := helmTemplate(t, "zot.auth.password="+renderPassword)
	sts := docOf(t, out, "statefulset-zot.yaml", "kind: StatefulSet")

	cfg := zotConfigJSON(t, out)
	httpBlock, _ := cfg["http"].(map[string]any)
	auth, _ := httpBlock["auth"].(map[string]any)
	htpasswd, _ := auth["htpasswd"].(map[string]any)
	path, _ := htpasswd["path"].(string)
	if path == "" {
		t.Skip("covered by TestZotConfig_DemandsAuthentication")
	}
	dir := path[:strings.LastIndex(path, "/")]
	if !strings.Contains(sts, "mountPath: "+dir) {
		t.Errorf("zot pod does not mount %s — the declared htpasswd path resolves to nothing "+
			"and zot would refuse to start (or, worse, serve open):\n%s", dir, sts)
	}
	if !strings.Contains(sts, "kacho.cloud/zot-auth-checksum:") {
		t.Errorf("zot pod template carries no checksum of the credential file — rotating the " +
			"credential would not roll the pod, and the process would keep the old one")
	}
}

// TestZotProbes_DoNotProveOpenness — пробы не должны утверждать, что
// неаутентифицированный запрос к хранилищу получает успех.
func TestZotProbes_DoNotProveOpenness(t *testing.T) {
	sts := docOf(t, helmTemplate(t, "zot.auth.password="+renderPassword),
		"statefulset-zot.yaml", "kind: StatefulSet")
	if strings.Contains(sts, "path: /v2/") {
		t.Errorf("zot probes still expect 2xx on an UNAUTHENTICATED /v2/ — with authentication on, " +
			"that path answers 401 and the pod never becomes Ready; a green probe there is the " +
			"proof that the store is open")
	}
	if !strings.Contains(sts, "tcpSocket:") {
		t.Errorf("zot has no credential-free liveness/readiness signal (tcpSocket) — a probe " +
			"cannot carry credentials, so the socket check is what remains")
	}
}

// TestZotChart_RefusesToRenderWithoutCredentials — гейт доказан инъекцией в обе
// стороны: без учётных данных рендер ОТКАЗЫВАЕТ и называет ручку (иначе оператор
// не поймёт, что задать), а с ними — молчит (см. соседние тесты).
func TestZotChart_RefusesToRenderWithoutCredentials(t *testing.T) {
	helmTemplateMustFail(t, "zot.auth.password")
}

// TestZotChart_ExistingSecretSatisfiesCredentials — законная форма той же
// конфигурации (учётные данные приходят секретом из вне git) рендерится молча.
// Без этой половины гейт ловил бы форму, а не существо.
func TestZotChart_ExistingSecretSatisfiesCredentials(t *testing.T) {
	out := helmTemplate(t, "zot.auth.existingSecret=kacho-registry-zot-auth")
	sts := docOf(t, out, "statefulset-zot.yaml", "kind: StatefulSet")
	if !strings.Contains(sts, "kacho-registry-zot-auth") {
		t.Errorf("operator-provisioned credential secret is not referenced by the zot pod:\n%s", sts)
	}
	if strings.Contains(out, renderPassword) {
		t.Errorf("chart rendered its own credential Secret although an existing one was named")
	}
}

// TestRegistryDeployment_CarriesZotCredentials — реестр обязан получить те же
// учётные данные: и адаптер плоскости управления, и форвардер плоскости данных
// ходят в хранилище от его имени.
func TestRegistryDeployment_CarriesZotCredentials(t *testing.T) {
	dep := docOf(t, helmTemplate(t, "zot.auth.password="+renderPassword),
		"deployment.yaml", "kind: Deployment")
	for _, env := range []string{"KACHO_REGISTRY_ZOT_USERNAME", "KACHO_REGISTRY_ZOT_PASSWORD"} {
		if !strings.Contains(dep, env) {
			t.Errorf("registry Deployment does not carry %s — the service could not authenticate "+
				"to its own layer store", env)
		}
	}
	if strings.Contains(dep, "value: \""+renderPassword+"\"") {
		t.Errorf("the credential is inlined into the pod spec — it must arrive via secretKeyRef")
	}
}

// TestZotNetworkPolicy_RestrictsToRegistryPods — эшелонирование: политика,
// когда включена, обязана селектить поды хранилища и впускать только реестр.
func TestZotNetworkPolicy_RestrictsToRegistryPods(t *testing.T) {
	out := helmTemplate(t, "zot.auth.password="+renderPassword, "zot.networkPolicy.enabled=true")
	np := docOf(t, out, "networkpolicy-zot.yaml", "kind: NetworkPolicy")
	for _, want := range []string{"app: zot", "app: registry", "port: 5000"} {
		if !strings.Contains(np, want) {
			t.Errorf("zot NetworkPolicy does not declare %q:\n%s", want, np)
		}
	}
	if strings.Contains(helmTemplate(t, "zot.auth.password="+renderPassword), "kind: NetworkPolicy") {
		t.Errorf("zot NetworkPolicy renders while disabled — the knob does nothing")
	}
}

// TestProfiles_EveryRegistryProfileDeclaresZotCredentials — декларативный гейт:
// профиль, включающий реестр, обязан объявить учётные данные хранилища (секретом
// или значением). Читает файлы значений, поэтому пропуститься не может.
func TestProfiles_EveryRegistryProfileDeclaresZotCredentials(t *testing.T) {
	enabled, examined := registryEnabledProfiles(t)
	if len(enabled) == 0 {
		t.Fatalf("no umbrella profile enables the registry (examined %d) — either the chart went "+
			"away or this gate lost its subject", examined)
	}
	for _, p := range enabled {
		tree := umbrellaValues(t, p)
		secret, hasSecret := digOpt(tree, "registry", "zot", "auth", "existingSecret")
		password, hasPassword := digOpt(tree, "registry", "zot", "auth", "password")
		secretSet := hasSecret && strings.TrimSpace(toStr(secret)) != ""
		passwordSet := hasPassword && strings.TrimSpace(toStr(password)) != ""
		if !secretSet && !passwordSet {
			t.Errorf("%s enables the registry but declares no zot credentials "+
				"(registry.zot.auth.existingSecret or .password) — the layer store would accept "+
				"every request from the pod network", p)
		}
	}
	t.Logf("examined %d umbrella profiles, %d enable the registry", examined, len(enabled))
}
