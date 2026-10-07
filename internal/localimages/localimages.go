// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package localimages — НАКЛАДКА ЗНАЧЕНИЙ, переводящая объявленную цепочку
// стенда на образы СБОРКИ ЭТОГО ДЕРЕВА (kacho#2931).
//
// # Предмет
//
// Цепочка стенда `own` (deploy/stacks.txt) пинит образы продукта на
// опубликованных тегах одной ревизии: так стенд `own` и поднимается на своём
// кластере. Конвейеру, судящему ЗАПРОС, этот пин непригоден by construction:
// он исполнил бы образы чужой ревизии, и провенанс стенда (ревизия каждого
// контейнера против ревизии прогона) краснел бы на каждом запросе — либо, хуже,
// вердикт о посадке относился бы не к тому дереву, которое судится.
//
// Поэтому поверх цепочки накладывается слой, который каждую ссылку на образ
// части ЭТОГО дерева переводит на образ, собранный этим же прогоном
// (`make build-services build-ui` → `<имя>:dev` в узлах kind). Цепочка при этом
// остаётся цепочкой таблицы стендов целиком: посадку, ручки и адреса накладка не
// трогает, меняется только то, ЧЕЙ образ исполняется.
//
// # Почему перечень ссылок ВЫВОДИТСЯ, а не выписывается
//
// Ключи образа у подчартов разные (плоская строка у служб и края, карта у nlb,
// storage и registry, вложенные модули консоли), и выписанная рядом накладка
// была бы вторым объявлением о тех же ключах — она разошлась бы с профилем
// площадки молча, ровно тогда, когда в него добавят образ. Здесь ссылки берутся
// у СЛОЖЕННОГО дерева значений той же цепочки, которую применяет helm.
//
// # Чья часть — решает источник имён
//
// Часть продукта опознаётся `productnaming` (имя образа → каталог части), а
// «собрано здесь» — его же ведомостью вынесенных частей. Вынесенная часть
// (служба доступа) приезжает пином чужого конвейера и остаётся на нём: образа с
// её именем эта сборка не производит. Сторонние образы (базы, почта, реестр
// слоёв) не наши вовсе и не трогаются.
//
// Сверх правила вызывающий называет, что сборка ДЕЙСТВИТЕЛЬНО произвела. Часть
// этого дерева, объявленная цепочкой и не собранная, — находка: накладка её не
// выдумывает, потому что под с таким образом ушёл бы в ImagePullBackOff уже
// ПОСЛЕ «helm upgrade прошёл».
//
// # Граница, сказанная прямо
//
// Обход идёт по картам; ссылка внутри СПИСКА им не видна (тот же предел у
// читателя пинов `product-names --external-pins`). Накладкой её и не заменить —
// helm замещает список целиком. Ссылку, которую объявляет только умолчание
// подчарта, а не значения умбреллы, обход тоже не видит. Обе дыры закрывает
// провенанс поднятого стенда: контейнер части этого дерева, исполняющий чужую
// ревизию, отвечает «расходится», и работа конвейера краснеет.
package localimages

import (
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// Merge — сложение слоёв ТАК ЖЕ, как их складывает helm: карты сливаются по
// ключам, всё остальное замещается целиком. Читатель пинов `product-names`
// зовёт эту функцию (своя копия у него снята тем же изменением, что завело
// пакет). Проверки каталога deploy складывают слои своей `mergeValues` — та же
// форма правила; разойдись они, расхождение было бы молчаливым, и сказано это
// здесь, а не спрятано.
func Merge(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if cur, ok := dst[k].(map[string]any); ok {
				dst[k] = Merge(cur, sub)
				continue
			}
			dst[k] = Merge(map[string]any{}, sub)
			continue
		}
		dst[k] = v
	}
	return dst
}

// Form — как подчарт объявляет образ.
type Form string

const (
	// Flat — `image: "<репо>[:<тег>|@<отпечаток>]"`, рядом необязательный `imageDigest`.
	Flat Form = "flat"
	// Mapped — `image: {registry?, repository, tag, digest?}`.
	Mapped Form = "map"
)

// Declaration — одно объявление образа в сложенном дереве значений.
type Declaration struct {
	Path     []string // путь к карте, несущей ключ `image`
	Form     Form
	Registry string // только у карты: отдельный ключ реестра
	Repo     string // репозиторий без тега и отпечатка
	Tag      string
	Digest   string
	Image    string // последний сегмент репозитория — имя образа
}

// Ref — ссылка так, как её рендерит чарт: отпечаток сильнее тега.
func (d Declaration) Ref() string {
	repo := d.Repo
	if d.Registry != "" {
		repo = d.Registry + "/" + repo
	}
	switch {
	case d.Digest != "":
		return repo + "@" + d.Digest
	case d.Tag != "":
		return repo + ":" + d.Tag
	}
	return repo
}

// Declarations — объявления образа в дереве значений, в байтовом порядке пути.
// Карта с тегом и без репозитория объявлением не считается: чей это образ,
// из неё не установить (её учитывает перепись накладки).
func Declarations(tree map[string]any) []Declaration {
	var out []Declaration
	walk(tree, nil, &out, new(int))
	return out
}

func walk(tree map[string]any, path []string, out *[]Declaration, unknown *int) {
	keys := make([]string, 0, len(tree))
	for k := range tree {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		here := append(append([]string{}, path...), k)
		if k == "image" {
			switch img := tree[k].(type) {
			case string:
				if ref := strings.TrimSpace(img); ref != "" {
					d := splitFlat(ref)
					d.Path = append([]string{}, path...)
					if digest, _ := tree["imageDigest"].(string); strings.TrimSpace(digest) != "" {
						d.Digest = strings.TrimSpace(digest)
					}
					*out = append(*out, d)
				}
				continue
			case map[string]any:
				repo, _ := img["repository"].(string)
				repo = strings.TrimSpace(repo)
				if repo == "" {
					if _, tagged := img["tag"]; tagged {
						*unknown++
					}
					continue
				}
				d := Declaration{Path: append([]string{}, path...), Form: Mapped, Repo: repo}
				d.Registry, _ = img["registry"].(string)
				d.Tag, _ = img["tag"].(string)
				d.Digest, _ = img["digest"].(string)
				d.Registry, d.Tag, d.Digest = strings.TrimSpace(d.Registry), strings.TrimSpace(d.Tag), strings.TrimSpace(d.Digest)
				d.Image = lastSegment(repo)
				*out = append(*out, d)
				continue
			}
		}
		if sub, ok := tree[k].(map[string]any); ok {
			walk(sub, here, out, unknown)
		}
	}
}

// splitFlat — разбор плоской формы. Отпечаток отделяется по `@`; тег — по
// двоеточию ПОСЛЕ последней косой черты: в `host:5000/repo` оно принадлежит хосту.
func splitFlat(ref string) Declaration {
	d := Declaration{Form: Flat}
	if i := strings.Index(ref, "@"); i >= 0 {
		ref, d.Digest = ref[:i], ref[i+1:]
	}
	d.Repo = ref
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		d.Repo, d.Tag = ref[:i], ref[i+1:]
	}
	d.Image = lastSegment(d.Repo)
	return d
}

func lastSegment(repo string) string { return repo[strings.LastIndex(repo, "/")+1:] }

// Census — перепись осмотренного. Печатается вызывающим ВСЕГДА: «ноль
// переписанного» обязано быть отличимо от «ноль прочитанного».
type Census struct {
	Declarations int      // объявлений образа осмотрено
	TreeParts    int      // из них — части этого дерева (переписаны)
	External     int      // части, собранные чужим конвейером (пин оставлен)
	Foreign      int      // сторонние образы (не тронуты)
	UnknownRepo  int      // карт с тегом без репозитория — чья, не установить
	Unbuilt      []string // части этого дерева, объявленные и НЕ собранные — находка
	Unused       []string // собранные образы, которых цепочка не объявляет
}

// Overlay — слой значений, переводящий каждое объявление части этого дерева на
// образ сборки `<имя>:<tag>`. Несобранная часть в слой не попадает и называется
// в `Census.Unbuilt`: решение об исходе — за вызывающим.
func Overlay(folded map[string]any, built []string, tag string) (map[string]any, Census) {
	have := map[string]bool{}
	for _, b := range built {
		if b = strings.TrimSpace(b); b != "" {
			have[b] = true
		}
	}
	var census Census
	decls := []Declaration{}
	walk(folded, nil, &decls, &census.UnknownRepo)
	census.Declarations = len(decls)

	overlay := map[string]any{}
	declared := map[string]bool{}
	unbuilt := map[string]bool{}
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
		declared[d.Image] = true
		if !have[d.Image] {
			unbuilt[d.Image] = true
			continue
		}
		census.TreeParts++
		holder := at(overlay, d.Path)
		switch d.Form {
		case Flat:
			holder["image"] = d.Image + ":" + tag
			if d.Digest != "" {
				holder["imageDigest"] = ""
			}
		case Mapped:
			img := map[string]any{"repository": d.Image, "tag": tag}
			if d.Digest != "" {
				img["digest"] = ""
			}
			if d.Registry != "" {
				img["registry"] = ""
			}
			holder["image"] = img
		}
	}
	for img := range unbuilt {
		census.Unbuilt = append(census.Unbuilt, img)
	}
	for img := range have {
		if !declared[img] {
			census.Unused = append(census.Unused, img)
		}
	}
	sort.Strings(census.Unbuilt)
	sort.Strings(census.Unused)
	return overlay, census
}

// at — карта по пути внутри накладки, заводимая по ходу.
func at(tree map[string]any, path []string) map[string]any {
	cur := tree
	for _, k := range path {
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	return cur
}
