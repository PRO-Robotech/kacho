// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// ka1_issuer_agreement_render_test.go — приёмка KA1, сценарий KA1-34 (Р5): на
// каждом стенде издатель, объявленный краю, и издатель, которым служба доступа
// того же стенда штампует `iss`, равны в канонической форме.
//
// # Канон не повторяется здесь
//
// Сравнивает КРАЙ, а не проба: из окружения контейнера края собирается его
// объявление приёма (`config.Config.TokenAcceptance`), из него — записи
// проверяющего (`middleware.IssuerKeySetsFromAcceptance`, тот же код, что у
// процесса), и проверяющий отвечает, читает ли он отзыв нашего авторитета для
// издателя службы (`ReadsRevocationFor`). Второй канон в пробе был бы вторым
// местом нормализации — тем самым классом, который Р5 снимает.
//
// # Значение службы — ПОЛНЫМ ПУТЁМ
//
// ConfigMap `kaname-config` → ключ `config.yaml` → путь `authn.token-signing.issuer`;
// переменная `KANAME_AUTHN__TOKEN_SIGNING__ISSUER` контейнера `kaname` её
// перекрывает (правило загрузчика службы: префикс KANAME, `.` → `__`, `-` → `_`).
// Выбор по имени ключа `issuer` запрещён: блок несёт и другие ключи с этим
// именем (в стенде `fe3455` — `api-server.registry-token.issuer`).
package deploy_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// ─── стенды таблицы — её единственным читателем (`stacks.sh`) ────────────────

// ka1RepoRoot — корень репозитория от каталога пакета (gateway/deploy).
func ka1RepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// ka1Stacks — стенды таблицы и цепочка каждого: `stacks.sh --names` и
// `stacks.sh --args <стенд>` — ровно то, что берут рецепты подъёма.
func ka1Stacks(t *testing.T) map[string][]string {
	t.Helper()
	sh := filepath.Join(ka1RepoRoot(t), "deploy/tests/helm/stacks.sh")
	out, err := exec.Command("bash", sh, "--names").Output()
	if err != nil {
		t.Fatalf("читатель таблицы стендов отказал (%v) — состав стендов взять неоткуда", err)
	}
	stands := map[string][]string{}
	for _, name := range strings.Fields(string(out)) {
		args, aerr := exec.Command("bash", sh, "--args", name).Output()
		if aerr != nil {
			t.Fatalf("цепочка стенда %s не выдана читателем таблицы: %v", name, aerr)
		}
		var chain []string
		fields := strings.Fields(string(args))
		for i := 0; i+1 < len(fields); i += 2 {
			if fields[i] == "-f" {
				chain = append(chain, fields[i+1])
			}
		}
		stands[name] = chain
	}
	if len(stands) == 0 {
		t.Fatal("читатель таблицы не назвал ни одного стенда — таблица пуста")
	}
	return stands
}

// ka1StandRender — исход отрисовки одного стенда.
type ka1StandRender struct {
	err  error
	out  string
	docs []map[string]any
}

// ka1Render — умбрелла, отрисованная цепочкой стенда.
func ka1Render(t *testing.T, chain []string) ka1StandRender {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm не в PATH — отрисовка стендов не исполнилась (это не «зелено»)")
	}
	umbrella := filepath.Join(ka1RepoRoot(t), "deploy/helm/umbrella")
	args := []string{"template", "kacho-umbrella", umbrella, "-n", "kacho"}
	for _, p := range chain {
		args = append(args, "-f", filepath.Join(umbrella, p))
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	r := ka1StandRender{err: err, out: string(out)}
	if err != nil {
		return r
	}
	dec := yaml.NewDecoder(strings.NewReader(string(out)))
	for {
		var d map[string]any
		if dec.Decode(&d) != nil {
			break
		}
		if d != nil {
			r.docs = append(r.docs, d)
		}
	}
	return r
}

func ka1Str(m map[string]any, k string) string         { s, _ := m[k].(string); return s }
func ka1Sub(m map[string]any, k string) map[string]any { s, _ := m[k].(map[string]any); return s }
func ka1Slice(m map[string]any, k string) []any        { s, _ := m[k].([]any); return s }

func ka1LastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// edgeContainerEnv — окружение контейнера api-gateway в Deployment края.
func edgeContainerEnv(docs []map[string]any) (map[string]string, bool) {
	for _, d := range docs {
		if ka1Str(d, "kind") != "Deployment" || ka1Str(ka1Sub(d, "metadata"), "name") != "api-gateway" {
			continue
		}
		env := map[string]string{}
		pod := ka1Sub(ka1Sub(ka1Sub(d, "spec"), "template"), "spec")
		for _, c := range ka1Slice(pod, "containers") {
			cm, _ := c.(map[string]any)
			if ka1Str(cm, "name") != "api-gateway" {
				continue
			}
			for _, e := range ka1Slice(cm, "env") {
				em, _ := e.(map[string]any)
				env[ka1Str(em, "name")] = ka1Str(em, "value")
			}
		}
		return env, true
	}
	return nil, false
}

const kanameIssuerEnv = "KANAME_AUTHN__TOKEN_SIGNING__ISSUER"

// serviceIssuer — издатель службы стенда: переменная окружения либо путь
// `authn.token-signing.issuer` в `config.yaml` карты `kaname-config`.
// raisesService=false — отрисовка службы не поднимает.
func serviceIssuer(docs []map[string]any) (issuer string, raisesService bool, err error) {
	var cfgYAML string
	haveCM := false
	for _, d := range docs {
		meta := ka1Sub(d, "metadata")
		switch {
		case ka1Str(d, "kind") == "ConfigMap" && ka1Str(meta, "name") == "kaname-config":
			cfgYAML, haveCM = ka1Str(ka1Sub(d, "data"), "config.yaml"), true
		case ka1Str(d, "kind") == "Deployment" && ka1Str(meta, "name") == "kaname":
			raisesService = true
			pod := ka1Sub(ka1Sub(ka1Sub(d, "spec"), "template"), "spec")
			for _, c := range ka1Slice(pod, "containers") {
				cm, _ := c.(map[string]any)
				if ka1Str(cm, "name") != "kaname" {
					continue
				}
				for _, e := range ka1Slice(cm, "env") {
					em, _ := e.(map[string]any)
					if ka1Str(em, "name") == kanameIssuerEnv {
						issuer = ka1Str(em, "value")
					}
				}
			}
		}
	}
	if !raisesService || issuer != "" {
		return issuer, raisesService, nil
	}
	if !haveCM {
		return "", true, fmt.Errorf("ConfigMap kaname-config с ключом config.yaml не отрисована")
	}
	var tree map[string]any
	if err := yaml.Unmarshal([]byte(cfgYAML), &tree); err != nil {
		return "", true, fmt.Errorf("config.yaml карты kaname-config не разбирается: %v", err)
	}
	v, _ := ka1Sub(ka1Sub(tree, "authn"), "token-signing")["issuer"].(string)
	if v == "" {
		return "", true, fmt.Errorf("пути authn.token-signing.issuer нет ни в config.yaml карты kaname-config, ни переменной %s", kanameIssuerEnv)
	}
	return v, true, nil
}

// edgeAccepts — читает ли край стенда отзыв НАШЕГО авторитета для токена
// издателя службы: то есть признаёт ли он этот `iss` нашей чеканкой.
func edgeAccepts(env map[string]string, iss string) (bool, error) {
	cfg := config.Config{
		AppEnv:                     "dev",
		TokenIssuers:               env["KACHO_API_GATEWAY_TOKEN_ISSUERS"],
		TokenIssuerKeySets:         env["KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS"],
		PlatformTokenIssuer:        env["KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER"],
		PlatformTokenRevocationURL: env["KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL"],
	}
	acc, err := cfg.TokenAcceptance()
	if err != nil {
		return false, fmt.Errorf("объявление приёма края не принято: %v", err)
	}
	records, _, _ := middleware.IssuerKeySetsFromAcceptance(acc)
	v, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{Issuers: records, ExpectedAudience: "https://api.kacho.test"})
	if err != nil {
		return false, fmt.Errorf("проверяющий края не собирается: %v", err)
	}
	return v.ReadsRevocationFor(iss), nil
}

type issuerCensus struct{ stands, rendered, compared int }

// judgeIssuerAgreement — суждение над множеством «стенд → отрисовка».
func judgeIssuerAgreement(stands map[string]ka1StandRender) ([]string, issuerCensus) {
	var findings []string
	c := issuerCensus{stands: len(stands)}
	names := make([]string, 0, len(stands))
	for n := range stands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		r := stands[name]
		if r.err != nil {
			findings = append(findings, fmt.Sprintf("стенд %s: отрисовка не состоялась (%v):\n%s", name, r.err, ka1LastLines(r.out, 8)))
			continue
		}
		c.rendered++
		env, raisesEdge := edgeContainerEnv(r.docs)
		iss, raisesService, err := serviceIssuer(r.docs)
		if !raisesEdge || !raisesService {
			continue
		}
		if err != nil {
			findings = append(findings, fmt.Sprintf("стенд %s: %v", name, err))
			continue
		}
		c.compared++
		ok, err := edgeAccepts(env, iss)
		switch {
		case err != nil:
			findings = append(findings, fmt.Sprintf("стенд %s: %v", name, err))
		case !ok:
			findings = append(findings, fmt.Sprintf("стенд %s: служба штампует iss=%q (authn.token-signing.issuer), "+
				"а край объявляет TOKEN_ISSUERS=%q, PLATFORM_TOKEN_ISSUER=%q — канонические формы не равны",
				name, iss, env["KACHO_API_GATEWAY_TOKEN_ISSUERS"], env["KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER"]))
		}
	}
	return findings, c
}

// TestKA1_34_EdgeAndServiceAgreeOnTheIssuer — KA1-34.
func TestKA1_34_EdgeAndServiceAgreeOnTheIssuer(t *testing.T) {
	stands := map[string]ka1StandRender{}
	for name, chain := range ka1Stacks(t) {
		stands[name] = ka1Render(t, chain)
	}
	findings, c := judgeIssuerAgreement(stands)
	t.Logf("перепись: стендов %d · отрисовано %d · сравнено %d", c.stands, c.rendered, c.compared)
	if c.rendered < c.stands || c.compared == 0 {
		t.Errorf("предпосылка: отрисовано %d из %d, сравнено %d", c.rendered, c.stands, c.compared)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// rewriteServiceIssuer — та же отрисовка, где путь authn.token-signing.issuer
// карты kaname-config заменён (del=true — удалён).
func rewriteServiceIssuer(t *testing.T, r ka1StandRender, value string, del bool) ka1StandRender {
	t.Helper()
	for _, d := range r.docs {
		if ka1Str(d, "kind") != "ConfigMap" || ka1Str(ka1Sub(d, "metadata"), "name") != "kaname-config" {
			continue
		}
		data := ka1Sub(d, "data")
		var tree map[string]any
		if err := yaml.Unmarshal([]byte(ka1Str(data, "config.yaml")), &tree); err != nil {
			t.Fatal(err)
		}
		ts := ka1Sub(ka1Sub(tree, "authn"), "token-signing")
		if del {
			delete(ts, "issuer")
		} else {
			ts["issuer"] = value
		}
		out, err := yaml.Marshal(tree)
		if err != nil {
			t.Fatal(err)
		}
		data["config.yaml"] = string(out)
	}
	return r
}

// Близнец (а): путь службы заменён на /realm2 — красно, названы стенд и оба
// значения; тот же издатель в другой канонически равной форме (завершающая /) —
// зелено.
func TestKA1_34_Twin_PathChangeIsFoundAndCanonicalFormIsSilent(t *testing.T) {
	table := ka1Stacks(t)
	base := ka1Render(t, table["dev"])
	if base.err != nil {
		t.Fatalf("предпосылка: стенд dev не отрисован: %v", base.err)
	}
	iss, _, err := serviceIssuer(base.docs)
	if err != nil {
		t.Fatal(err)
	}
	moved := rewriteServiceIssuer(t, ka1Render(t, table["dev"]), strings.TrimRight(iss, "/")+"/realm2", false)
	f, _ := judgeIssuerAgreement(map[string]ka1StandRender{"dev": moved})
	if len(f) != 1 || !strings.Contains(f[0], "стенд dev") || !strings.Contains(f[0], "/realm2") {
		t.Fatalf("смена пути не названа со стендом и значениями: %v", f)
	}
	slashed := rewriteServiceIssuer(t, ka1Render(t, table["dev"]), strings.TrimRight(iss, "/")+"/", false)
	if f, _ := judgeIssuerAgreement(map[string]ka1StandRender{"dev": slashed}); len(f) != 0 {
		t.Fatalf("канонически равная форма (завершающая /) обязана быть зелёной: %v", f)
	}
}

// Близнец (б): fe3455 как есть (второй ключ issuer по пути
// api-server.registry-token.issuer несёт другое значение) — зелено; без пути
// authn.token-signing.issuer — красно отказом «пути нет».
func TestKA1_34_Twin_FullPathNotKeyName(t *testing.T) {
	table := ka1Stacks(t)
	asIs := ka1Render(t, table["fe3455"])
	if f, _ := judgeIssuerAgreement(map[string]ka1StandRender{"fe3455": asIs}); len(f) != 0 {
		t.Fatalf("fe3455 как есть обязан быть зелёным: %v", f)
	}
	gone := rewriteServiceIssuer(t, ka1Render(t, table["fe3455"]), "", true)
	f, _ := judgeIssuerAgreement(map[string]ka1StandRender{"fe3455": gone})
	if len(f) != 1 || !strings.Contains(f[0], "стенд fe3455") || !strings.Contains(f[0], "пути authn.token-signing.issuer нет") {
		t.Fatalf("отсутствие пути не названо отказом со стендом: %v", f)
	}
}
