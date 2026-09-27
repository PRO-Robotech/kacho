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
package deploy_test

import (
	"bytes"
	"errors"
	"io"
	"path"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// keySetsAnchorSecret — имя секрета связки в осях ниже. Выбрано своё, чтобы
// связку в рендере находить по нему, а не по имени тома.
const keySetsAnchorSecret = "keysets-anchor-probe"

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
	values, mounted := keySetsAnchor(t, helmTemplate(t, "hydra.jwksCa.secretName="+keySetsAnchorSecret))
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
