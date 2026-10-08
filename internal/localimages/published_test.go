// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// published_test.go — накладка ОПУБЛИКОВАННЫХ образов одной ревизии (kacho#3102).
//
// Отказ у неё тот же, что у накладки сборки: форму объявления, которой она не
// видит, она не отвергает, а пропускает, — и стенд поднимается на пине чужой
// ревизии. Поэтому судится ИТОГ сложения «цепочка + накладка» по каждой форме,
// и отдельно — что не названная ссылка является находкой, а не тихим пропуском.
package localimages

import (
	"reflect"
	"testing"
)

func TestPublishedOverlayRepointsEveryDeclarationFormAtTheNamedRef(t *testing.T) {
	ext := externalImage(t)
	folded := Merge(map[string]any{}, layer(t, ""+
		"vpc:\n  image: kacho-vpc:dev\n"+
		"kacho-nlb:\n  image:\n    repository: kacho-nlb\n    tag: dev\n"+
		"registry:\n  image:\n    registry: docker.io\n    repository: prorobotech/kacho-registry\n    digest: sha256:aa\n    tag: x\n"+
		"api-gateway:\n  image: kacho-api-gateway:dev\n  imageDigest: sha256:bb\n"+
		"uif:\n  dashboard:\n    image: kacho-ui-future-dashboard:dev\n"+
		"kaname:\n  image:\n    repository: docker.io/prorobotech/"+ext+"\n    tag: 296-x\n"+
		"pg:\n  image:\n    repository: docker.io/bitnamilegacy/postgresql\n    tag: 16\n"))
	refs := map[string]string{
		"kacho-vpc":                 "docker.io/prorobotech/kacho-vpc:1266-1fd076f8",
		"kacho-nlb":                 "docker.io/prorobotech/kacho-nlb:1266-1fd076f8",
		"kacho-registry":            "docker.io/prorobotech/kacho-registry:1266-1fd076f8",
		"kacho-api-gateway":         "docker.io/prorobotech/kacho-api-gateway:1266-1fd076f8",
		"kacho-ui-future-dashboard": "docker.io/prorobotech/kacho-ui-future-dashboard:1266-1fd076f80965649745702f7a835169bf052251ae",
	}
	overlay, census := PublishedOverlay(folded, refs)
	for path, want := range map[string]string{
		"vpc":           refs["kacho-vpc"],
		"kacho-nlb":     refs["kacho-nlb"],
		"registry":      refs["kacho-registry"],
		"api-gateway":   refs["kacho-api-gateway"],
		"uif.dashboard": refs["kacho-ui-future-dashboard"],
		"kaname":        "docker.io/prorobotech/" + ext + ":296-x",
		"pg":            "docker.io/bitnamilegacy/postgresql",
	} {
		got := finalRef(t, folded, overlay, path)
		if path == "pg" {
			// Тег стороннего образа в синтетическом слое — число, и обход его
			// не читает как строку; утверждается только, что накладка его не тронула.
			if _, touched := overlay["pg"]; touched {
				t.Errorf("сторонний образ pg переписан накладкой: %v", overlay["pg"])
			}
			continue
		}
		if got != want {
			t.Errorf("%s: итог сложения %q, ждали %q", path, got, want)
		}
	}
	if census.Rewritten != 5 || census.External != 1 || census.Foreign != 1 || len(census.Unresolved) != 0 {
		t.Errorf("перепись %+v, ждали переписано 5, вынесенных 1, сторонних 1, без ссылки 0", census)
	}
	if got := TreeImages(folded); !reflect.DeepEqual(got, []string{
		"kacho-api-gateway", "kacho-nlb", "kacho-registry", "kacho-ui-future-dashboard", "kacho-vpc",
	}) {
		t.Errorf("TreeImages = %v: вынесенная часть и сторонний образ в перечень не входят", got)
	}
}

// Близнец: та же цепочка, одна ссылка не названа. Стенд из смеси двух ревизий
// хуже отказа — накладка обязана назвать часть, а не пропустить её.
func TestPublishedOverlayNamesTheTreePartItHasNoRefFor(t *testing.T) {
	folded := Merge(map[string]any{}, layer(t, "vpc:\n  image: kacho-vpc:dev\ncompute:\n  image: kacho-compute:dev\n"))
	_, census := PublishedOverlay(folded, map[string]string{"kacho-vpc": "r/kacho-vpc:t"})
	if !reflect.DeepEqual(census.Unresolved, []string{"kacho-compute"}) || census.Rewritten != 1 {
		t.Fatalf("перепись %+v: часть без ссылки обязана попасть в Unresolved", census)
	}
}
