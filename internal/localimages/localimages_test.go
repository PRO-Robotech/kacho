// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// localimages_test.go — доказательство накладки инъекцией.
//
// Отказ у накладки МОЛЧАЛИВЫЙ по устройству: форму объявления, о которой она не
// знает, она не отвергает — она её НЕ ВИДИТ. Стенд тогда поднимается на
// опубликованном образе чужой ревизии, а провенанс в конвейере узнаёт об этом
// уже на поднятом кластере. Поэтому здесь доказывается не то, что накладка
// что-то печатает, а то, что ИТОГ сложения «цепочка + накладка» даёт образ
// сборки на каждой форме — и не даёт его там, где часть собрана не здесь.
package localimages

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// externalImage — имя образа вынесенной части, взятое у ЕДИНСТВЕННОГО владельца.
// Выписать его литералом значило бы завести второе место об одном предмете.
func externalImage(t *testing.T) string {
	t.Helper()
	for dir := range productnaming.ExternallySourcedServices() {
		return productnaming.ChartName(dir)
	}
	t.Fatal("ведомость вынесенных частей пуста — у оси «пин вынесенной части остаётся» нет предмета")
	return ""
}

func layer(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := yaml.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("синтетический слой не разобран: %v\n%s", err, body)
	}
	return out
}

// finalRef — ссылка, которую получит релиз по пути компонента: сложение
// «цепочка + накладка», прочитанное тем же обходом, что и у накладки.
func finalRef(t *testing.T, folded, overlay map[string]any, path string) string {
	t.Helper()
	final := Merge(Merge(map[string]any{}, folded), overlay)
	for _, d := range Declarations(final) {
		if strings.Join(d.Path, ".") == path {
			return d.Ref()
		}
	}
	t.Fatalf("по пути %q объявления образа нет — обход не видит компонент", path)
	return ""
}

// TestOverlayRepointsEveryDeclarationFormAtTheLocalBuild — ФОРМ ОБЪЯВЛЕНИЯ
// НЕСКОЛЬКО, и все законны в этом дереве: плоская строка (службы и край), карта
// (nlb, storage, registry), вложенный модуль консоли, накладка только тегом, чей
// репозиторий приходит слоем ниже, и отпечаток, который у чарта сильнее тега.
func TestOverlayRepointsEveryDeclarationFormAtTheLocalBuild(t *testing.T) {
	cases := []struct {
		name, path, want string
		layers           []string
		built            []string
	}{
		{
			name:   "плоская строка",
			path:   "vpc",
			layers: []string{"vpc:\n  image: docker.io/prorobotech/kacho-vpc:2798-87329e29\n"},
			built:  []string{"kacho-vpc"},
			want:   "kacho-vpc:dev",
		},
		{
			name:   "карта",
			path:   "kacho-nlb",
			layers: []string{"kacho-nlb:\n  image:\n    repository: docker.io/prorobotech/kacho-nlb\n    tag: 2798-87329e29\n"},
			built:  []string{"kacho-nlb"},
			want:   "kacho-nlb:dev",
		},
		{
			name:   "вложенный модуль консоли",
			path:   "uif.dashboard",
			layers: []string{"uif:\n  image: docker.io/prorobotech/kacho-ui-future-host:x\n  dashboard:\n    image: docker.io/prorobotech/kacho-ui-future-dashboard:x\n"},
			built:  []string{"kacho-ui-future-host", "kacho-ui-future-dashboard"},
			want:   "kacho-ui-future-dashboard:dev",
		},
		{
			name: "накладка только тегом",
			path: "storage",
			layers: []string{
				"storage:\n  image:\n    repository: docker.io/prorobotech/kacho-storage\n    tag: main-1\n",
				"storage:\n  image:\n    tag: main-2\n",
			},
			built: []string{"kacho-storage"},
			want:  "kacho-storage:dev",
		},
		{
			name:   "отпечаток карты снят",
			path:   "registry",
			layers: []string{"registry:\n  image:\n    repository: docker.io/prorobotech/kacho-registry\n    tag: main-1\n    digest: sha256:abc\n"},
			built:  []string{"kacho-registry"},
			want:   "kacho-registry:dev",
		},
		{
			name:   "отпечаток плоской строки снят",
			path:   "kacho-geo",
			layers: []string{"kacho-geo:\n  image: docker.io/prorobotech/kacho-geo:main-1\n  imageDigest: sha256:abc\n"},
			built:  []string{"kacho-geo"},
			want:   "kacho-geo:dev",
		},
		{
			name:   "реестр отдельным ключом снят",
			path:   "compute",
			layers: []string{"compute:\n  image:\n    registry: docker.io\n    repository: prorobotech/kacho-compute\n    tag: main-1\n"},
			built:  []string{"kacho-compute"},
			want:   "kacho-compute:dev",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			folded := map[string]any{}
			for _, l := range c.layers {
				folded = Merge(folded, layer(t, l))
			}
			overlay, census := Overlay(folded, c.built, "dev")
			if len(census.Unbuilt) != 0 {
				t.Fatalf("находка на исправном входе: %v", census.Unbuilt)
			}
			if got := finalRef(t, folded, overlay, c.path); got != c.want {
				t.Errorf("итог сложения по %s — %q, ожидался образ сборки %q (накладка %v)",
					c.path, got, c.want, overlay)
			}
		})
	}
}

// TestOverlayLeavesPartsBuiltElsewhereOnTheirPins — ЗАКОННЫЕ БЛИЗНЕЦЫ: та же
// форма объявления, отличающаяся РОВНО принадлежностью образа. Вынесенная часть
// собирается чужим конвейером и приезжает пином; сторонний образ не наш вовсе.
// Без этой оси «переписывает всё» неотличимо от «переписывает нужное».
func TestOverlayLeavesPartsBuiltElsewhereOnTheirPins(t *testing.T) {
	ext := externalImage(t)
	folded := layer(t, ext+":\n  image:\n    repository: docker.io/prorobotech/"+ext+"\n    tag: 366-4af7fd4f\n"+
		"pg-vpc:\n  image: { registry: docker.io, repository: bitnamilegacy/postgresql, tag: '16' }\n"+
		"vpc:\n  image: docker.io/prorobotech/kacho-vpc:main-1\n")
	overlay, census := Overlay(folded, []string{"kacho-vpc", ext}, "dev")

	if _, touched := overlay[ext]; touched {
		t.Errorf("пин вынесенной части %q переписан на образ сборки: %v — её исходников в дереве нет, "+
			"и образа с таким именем сборка не производит", ext, overlay[ext])
	}
	if _, touched := overlay["pg-vpc"]; touched {
		t.Errorf("сторонний образ переписан: %v", overlay["pg-vpc"])
	}
	if got := finalRef(t, folded, overlay, "vpc"); got != "kacho-vpc:dev" {
		t.Errorf("контроль: часть этого дерева не переписана (%q)", got)
	}
	if census.External != 1 || census.Foreign != 1 || census.TreeParts != 1 {
		t.Errorf("перепись разошлась с входом: %+v", census)
	}
}

// TestOverlayNamesATreePartTheBuildDidNotProduce — одна и та же цепочка, вход
// различается РОВНО перечнем собранного. Часть этого дерева, которую сборка не
// произвела, накладка не выдумывает: образа с таким именем в узлах нет, и под
// ушёл бы в ImagePullBackOff уже после «helm upgrade прошёл».
func TestOverlayNamesATreePartTheBuildDidNotProduce(t *testing.T) {
	folded := layer(t, "vpc:\n  image: docker.io/prorobotech/kacho-vpc:main-1\n"+
		"compute:\n  image: docker.io/prorobotech/kacho-compute:main-1\n")

	_, census := Overlay(folded, []string{"kacho-vpc"}, "dev")
	if !reflect.DeepEqual(census.Unbuilt, []string{"kacho-compute"}) {
		t.Errorf("несобранная часть не названа: %v", census.Unbuilt)
	}

	_, census = Overlay(folded, []string{"kacho-vpc", "kacho-compute"}, "dev")
	if len(census.Unbuilt) != 0 {
		t.Errorf("законный близнец: всё собрано, а находка есть — %v", census.Unbuilt)
	}

	_, census = Overlay(folded, []string{"kacho-vpc", "kacho-compute", "kacho-ui-future-host"}, "dev")
	if !reflect.DeepEqual(census.Unused, []string{"kacho-ui-future-host"}) {
		t.Errorf("собранный, но не объявленный цепочкой образ не назван в переписи: %v", census.Unused)
	}
}

// TestFlatReferenceWithADigestKeepsItsName — плоская ссылка с отпечатком: имя
// образа — сегмент до `@`, а не до последнего двоеточия.
func TestFlatReferenceWithADigestKeepsItsName(t *testing.T) {
	d := Declarations(layer(t, "vpc:\n  image: docker.io/prorobotech/kacho-vpc@sha256:abc\n"))
	if len(d) != 1 || d[0].Image != "kacho-vpc" || d[0].Digest != "sha256:abc" {
		t.Errorf("разбор ссылки с отпечатком: %+v", d)
	}
}

// TestMergeFoldsLikeHelm — карты сливаются по ключам, прочее замещается целиком.
func TestMergeFoldsLikeHelm(t *testing.T) {
	got := Merge(layer(t, "a:\n  b: 1\n  c: [1, 2]\n"), layer(t, "a:\n  c: [3]\n  d: 4\n"))
	want := layer(t, "a:\n  b: 1\n  c: [3]\n  d: 4\n")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("сложение %v, ожидалось %v", got, want)
	}
}
