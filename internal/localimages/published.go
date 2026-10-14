// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package localimages

import (
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// ОПУБЛИКОВАННЫЕ ОБРАЗЫ ОДНОЙ РЕВИЗИИ (kacho#3102).
//
// Сестра `Overlay`. Там цепочка переводится на образы, которые сборка этого
// прогона положила в узлы kind; здесь — на образы, которые конвейер дерева
// ОПУБЛИКОВАЛ для названной ревизии. Нужна стенду в своём пространстве имён
// внешнего кластера (`make -C deploy stand-ns-up`): узлов kind у него нет, и
// исполнить он может только то, что лежит в реестре.
//
// Какой ссылкой опубликован образ (реестр, форма тега), здесь НЕ решается:
// ссылку для каждого образа называет вызывающий, а правило формы тега живёт в
// одном месте — `deploy/scripts/gen-managed-image-pins.sh` (`--tag-of`). Пакет
// знает только, ГДЕ в сложенном дереве значений стоит объявление образа, — то
// же, что и у `Overlay`, и тем же обходом.

// TreeImages — имена образов частей ЭТОГО дерева, которые объявляет сложенная
// цепочка, без повторов и по порядку. Части, собранные чужим конвейером
// (служба доступа), и сторонние образы сюда не входят: их пин цепочка несёт
// сама, и переписывать его не из чего.
func TreeImages(folded map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range Declarations(folded) {
		dir, ours := productnaming.ServiceDir(d.Image)
		if !ours || !productnaming.SourcesInThisTree(dir) || seen[d.Image] {
			continue
		}
		seen[d.Image] = true
		out = append(out, d.Image)
	}
	sort.Strings(out)
	return out
}

// PublishedCensus — объём осмотренного накладкой опубликованных образов.
type PublishedCensus struct {
	Declarations int      // объявлений образа осмотрено
	Rewritten    int      // из них переписано на опубликованную ссылку
	External     int      // части, собранные чужим конвейером (пин оставлен)
	Foreign      int      // сторонние образы (не тронуты)
	Unresolved   []string // части этого дерева, для которых ссылка НЕ названа — находка
}

// PublishedOverlay — накладка, ставящая каждому объявлению образа части этого
// дерева ссылку из refs (имя образа → полная ссылка «реестр/репозиторий:тег»).
// Объявление, для которого ссылка не названа, попадает в Unresolved: стенд с
// частью на чужом пине и частью на ревизии исполнял бы смесь двух деревьев.
func PublishedOverlay(folded map[string]any, refs map[string]string) (map[string]any, PublishedCensus) {
	var census PublishedCensus
	decls := Declarations(folded)
	census.Declarations = len(decls)
	overlay := map[string]any{}
	unresolved := map[string]bool{}
	for _, d := range decls {
		dir, ours := productnaming.ServiceDir(d.Image)
		switch {
		case !ours:
			census.Foreign++
			continue
		case !productnaming.SourcesInThisTree(dir):
			census.External++
			continue
		}
		ref := strings.TrimSpace(refs[d.Image])
		if ref == "" {
			unresolved[d.Image] = true
			continue
		}
		census.Rewritten++
		holder := at(overlay, d.Path)
		switch d.Form {
		case Flat:
			holder["image"] = ref
			if d.Digest != "" {
				holder["imageDigest"] = ""
			}
		case Mapped:
			repo, tag := ref, ""
			if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
				repo, tag = ref[:i], ref[i+1:]
			}
			img := map[string]any{"repository": repo, "tag": tag}
			if d.Digest != "" {
				img["digest"] = ""
			}
			if d.Registry != "" {
				img["registry"] = ""
			}
			holder["image"] = img
		}
	}
	for img := range unresolved {
		census.Unresolved = append(census.Unresolved, img)
	}
	sort.Strings(census.Unresolved)
	return overlay, census
}
