// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// jwks_ca_render_test.go — чарт провязывает якорь доверия хопа за наборами
// ключей под ТЕМ именем, которое читает процесс, и путь в нём ведёт в
// смонтированную связку (kacho#2842).
//
// Судится ОТРЕНДЕРЕННЫЙ манифест, а не текст шаблона: имя переменной сверяется с
// постоянной, которую читает конфигурация края (`config.JWKSCAFileKnob`),
// а значение — с тем, куда рендер действительно монтирует секрет. Переименование
// в одном месте без другого дало бы ручку, которую задают и не читают: хоп пошёл бы
// транспортом по умолчанию и отверг внутренний сертификат, или — хуже — край
// остался бы «настроенным проверять», ничего не проверяя.
//
// # Где живёт ключ (#2734)
//
// Якорь объявляется в `tokenAcceptance.issuerKeySetsCa` — рядом с записью
// `tokenAcceptance.issuerKeySets`, чьи адреса он защищает, и так же, как якорь
// хопа к авторитету отзыва (`tokenAcceptance.revocationCa`). Прежний корень
// ключа носил имя снятого поставщика; он снят тем же изменением у чарта, у всех
// профилей зонта и у проб.
//
// Чем это держится, по сторонам:
//
//	чарт      первая проба ниже рендерит с НОВЫМ ключом и требует смонтированную
//	          связку под читаемым именем;
//	профили   вторая проба судит ОБЪЯВЛЕНИЯ каждой цепочки deploy/stacks.txt, а
//	          не рендер: профиль, оставивший якорь под прежним именем,
//	          рендерится чисто и лишает хоп якоря молча;
//	дерево    возврат прежнего имени ключом — рост числа гейта убывающего
//	          потолка (internal/repohygiene/retiredidentityvendorceiling.go).
package deploy_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// keySetsAnchorSecret — имя секрета связки в осях ниже. Выбрано своё, чтобы
// связку в рендере находить по нему, а не по имени тома.
const keySetsAnchorSecret = "keysets-anchor-probe"

// keySetsAnchorKey — ключ значений якоря в чарте края: узел объявления приёма
// и имя блока в нём. Выписан один раз для обеих проб файла.
var keySetsAnchorKey = []string{"tokenAcceptance", "issuerKeySetsCa"}

type renderedEnv struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type renderedContainer struct {
	Name         string        `yaml:"name"`
	Env          []renderedEnv `yaml:"env"`
	VolumeMounts []struct {
		Name      string `yaml:"name"`
		MountPath string `yaml:"mountPath"`
	} `yaml:"volumeMounts"`
}

type renderedDeployment struct {
	Kind string `yaml:"kind"`
	Spec struct {
		Template struct {
			Spec struct {
				Containers []renderedContainer `yaml:"containers"`
				Volumes    []struct {
					Name   string `yaml:"name"`
					Secret *struct {
						SecretName string `yaml:"secretName"`
						Items      []struct {
							Key  string `yaml:"key"`
							Path string `yaml:"path"`
						} `yaml:"items"`
					} `yaml:"secret"`
				} `yaml:"volumes"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

// renderedDeployments — все объекты Deployment рендера.
func renderedDeployments(t *testing.T, manifest string) []renderedDeployment {
	t.Helper()
	var out []renderedDeployment
	dec := yaml.NewDecoder(bytes.NewBufferString(manifest))
	for {
		var d renderedDeployment
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("рендер чарта не разбирается как YAML: %v", err)
		}
		if d.Kind == "Deployment" {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		t.Fatal("в рендере нет ни одного Deployment — судить провязку не на чем")
	}
	return out
}

// keySetsAnchor — значение ручки якоря у каждого контейнера, который её несёт,
// и путь, по которому рендер кладёт файл связки из секрета keySetsAnchorSecret.
func keySetsAnchor(t *testing.T, manifest string) (values []string, mountedFile string) {
	t.Helper()
	for _, d := range renderedDeployments(t, manifest) {
		spec := d.Spec.Template.Spec
		volume, item := "", ""
		for _, v := range spec.Volumes {
			if v.Secret != nil && v.Secret.SecretName == keySetsAnchorSecret && len(v.Secret.Items) == 1 {
				volume, item = v.Name, v.Secret.Items[0].Path
			}
		}
		for _, c := range spec.Containers {
			for _, e := range c.Env {
				if e.Name == config.JWKSCAFileKnob {
					values = append(values, e.Value)
				}
			}
			for _, m := range c.VolumeMounts {
				if volume != "" && m.Name == volume {
					mountedFile = path.Join(m.MountPath, item)
				}
			}
		}
	}
	return values, mountedFile
}

func TestChart_JWKSAnchorIsRenderedUnderTheReadName(t *testing.T) {
	set := strings.Join(keySetsAnchorKey, ".") + ".secretName=" + keySetsAnchorSecret
	values, mounted := keySetsAnchor(t, helmTemplate(t, set))
	if mounted == "" {
		t.Fatalf("секрет %s не смонтирован ни в один контейнер — связке неоткуда взяться", keySetsAnchorSecret)
	}
	if len(values) != 1 {
		t.Fatalf("переменная %s отрендерена %d раз, ожидалась 1: процесс читает только её, и "+
			"связка, смонтированная без неё, задана и не читается", config.JWKSCAFileKnob, len(values))
	}
	if values[0] != mounted {
		t.Fatalf("%s = %q, а связка смонтирована в %q — хоп искал бы файл не там",
			config.JWKSCAFileKnob, values[0], mounted)
	}
	t.Logf("%s = %s (связка из секрета %s)", config.JWKSCAFileKnob, values[0], keySetsAnchorSecret)
}

// Законный близнец: без объявленного секрета переменной нет — пустая переменная
// не рождается, и хоп идёт транспортом по умолчанию, как объявлено.
func TestChart_JWKSAnchorIsAbsentWithoutASecret(t *testing.T) {
	values, mounted := keySetsAnchor(t, helmTemplate(t))
	if len(values) != 0 || mounted != "" {
		t.Fatalf("без объявленного секрета отрендерено %v (связка %q)", values, mounted)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// СТЕНД: хоп за набором ключей по TLS к внутрикластерному имени несёт якорь.
//
// Лист слушателя с внутрикластерным именем выписан внутренним центром, которого
// в корнях процесса нет. Без якоря хоп отвергает этот лист на первом же
// получении набора, и край отказывает КАЖДОМУ предъявителю — при поде, который
// поднялся и готов, потому что набор тянется лениво, на первом запросе.
// Рендер этого не видит вовсе: профиль, объявивший якорь под ключом, которого
// чарт не читает, рендерится чисто. Поэтому судятся объявления цепочки.

// keySetHopAnchor — объявленный стендом секрет якоря; "" — не объявлен.
// Читается ТОЧНЫМИ именами ключей: ключ с другим именем читается как «якоря
// нет», а не удовлетворяет пробу.
func keySetHopAnchor(gw map[string]any) string {
	node := gw
	for _, k := range keySetsAnchorKey {
		next, ok := node[k].(map[string]any)
		if !ok {
			return ""
		}
		node = next
	}
	s, _ := node["secretName"].(string)
	return strings.TrimSpace(s)
}

// internalTLSKeySets — адреса наборов, которые хоп тянет по TLS с
// внутрикластерного имени Service. Адрес на публичном имени проверяется
// системными корнями и якоря не требует.
func internalTLSKeySets(bindings []config.TokenIssuerBinding) []string {
	var out []string
	for _, b := range bindings {
		u, err := url.Parse(b.KeySetURL)
		if err != nil || !strings.EqualFold(u.Scheme, "https") {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if strings.HasSuffix(host, ".svc") || strings.HasSuffix(host, ".svc.cluster.local") {
			out = append(out, b.KeySetURL)
		}
	}
	return out
}

// keySetHopAnchorVerdict — вердикт по объявлениям края одного стенда: какие
// адреса требуют якоря и находка, если якоря нет. Ошибка — объявление приёма,
// с которым процесс не поднимется; его судит соседняя проба, а здесь судить хоп
// не по чему.
func keySetHopAnchorVerdict(gw map[string]any) (needs []string, finding string, err error) {
	cfg, _ := f1bGatewayConfig(gw)
	bindings, err := cfg.TokenAcceptance()
	if err != nil {
		return nil, "", err
	}
	needs = internalTLSKeySets(bindings)
	if len(needs) > 0 && keySetHopAnchor(gw) == "" {
		finding = fmt.Sprintf("набор ключей тянется по TLS с внутрикластерного имени (%s), а "+
			"якорь хопа не объявлен (%s.secretName). Лист выписан внутренним центром, в "+
			"корнях процесса его нет: хоп отвергнет его на первом получении набора, и край "+
			"откажет каждому предъявителю при готовом поде",
			strings.Join(needs, ", "), strings.Join(keySetsAnchorKey, "."))
	}
	return needs, finding, nil
}

func TestStacks_InClusterKeySetHopCarriesItsAnchor(t *testing.T) {
	stacks := deployableStacks(t)
	read, needing, anchored := 0, 0, 0
	for _, name := range sortedStackNames(stacks) {
		gw, ok := resolveStackGateway(t, stacks[name])
		if !ok {
			continue
		}
		read++
		chain := strings.Join(stacks[name], " + ")
		needs, finding, err := keySetHopAnchorVerdict(gw)
		switch {
		case err != nil:
			t.Errorf("стенд %s (%s): объявление приёма не разбирается (%v) — судить хоп "+
				"за набором ключей не по чему", name, chain, err)
		case len(needs) == 0:
			t.Logf("стенд %-12s набора по TLS с внутрикластерного имени нет — якорь не нужен", name)
		case finding != "":
			needing++
			t.Errorf("стенд %s (%s): %s", name, chain, finding)
		default:
			needing++
			anchored++
			t.Logf("стенд %-12s якорь %s ← %v", name, keySetHopAnchor(gw), needs)
		}
	}
	t.Logf("перепись: стендов прочитано %d · хоп по TLS к внутрикластерному имени %d · "+
		"с объявленным якорем %d", read, needing, anchored)
	if read == 0 {
		t.Fatalf("прочитано НОЛЬ стендов, называющих край (%s) — «ноль находок» на таком "+
			"объёме означает «ноль прочитанного»", stacksTable)
	}
}

// TestStacks_KeySetHopAnchorVerdictFallsOnTheRealStand — способность упасть на
// НАСТОЯЩЕМ входе: объявления края стенда из таблицы, у которого хоп требует
// якоря и якорь объявлен. Каждый случай меняет ровно один факт.
func TestStacks_KeySetHopAnchorVerdictFallsOnTheRealStand(t *testing.T) {
	stacks := deployableStacks(t)
	var live map[string]any
	var liveName string
	for _, name := range sortedStackNames(stacks) {
		gw, ok := resolveStackGateway(t, stacks[name])
		if !ok {
			continue
		}
		if needs, finding, err := keySetHopAnchorVerdict(gw); err == nil && len(needs) > 0 && finding == "" {
			live, liveName = gw, name
			break
		}
	}
	if live == nil {
		t.Fatalf("в таблице стендов нет ни одного, чей хоп требует якоря и несёт его — " +
			"входа для инъекции нет, и способность пробы упасть не доказана")
	}

	withoutAnchor := func() map[string]any {
		gw := mergeInto(map[string]any{}, live)
		ta, _ := gw[keySetsAnchorKey[0]].(map[string]any)
		delete(ta, keySetsAnchorKey[1])
		return gw
	}

	// Дефект: якорь снят, адрес тот же.
	if _, finding, err := keySetHopAnchorVerdict(withoutAnchor()); err != nil || finding == "" {
		t.Fatalf("стенд %s без якоря: находки нет (err=%v) — проба молчит на дефекте, ради "+
			"которого заведена", liveName, err)
	}

	// Законный близнец дефекта: якоря нет, а набор переехал на публичное имя —
	// лист проверяется системными корнями.
	public := withoutAnchor()
	ta := public["tokenAcceptance"].(map[string]any)
	sets, _ := ta["issuerKeySets"].(string)
	moved := strings.ReplaceAll(sets, ".svc:", ".example.org:")
	if moved == sets {
		t.Fatalf("адрес набора стенда %s не несёт внутрикластерного имени вида *.svc:<порт> "+
			"(%q) — близнеца из него не построить", liveName, sets)
	}
	ta["issuerKeySets"] = moved
	if needs, finding, err := keySetHopAnchorVerdict(public); err != nil || len(needs) != 0 || finding != "" {
		t.Fatalf("набор на публичном имени без якоря: needs=%v finding=%q err=%v — проба "+
			"требует якорь там, где его не нужно", needs, finding, err)
	}
	t.Logf("вход — стенд %s: без якоря находка есть; на публичном имени без якоря молчит", liveName)
}
