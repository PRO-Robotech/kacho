// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_flow_path_is_served_test.go — адрес, который служба личности выдаёт
// браузеру, обязан быть адресом, который раздача консоли умеет обслужить.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Об одном предмете говорят ДВА места, и они разошлись. Служба личности
// объявляет браузерные адреса потоков (`ui_url:` в чартах), раздача консоли
// объявляет, какие пути она обслуживает (регулярка полосы потоков в
// `ui-future/deploy/templates/configmap-nginx.yaml`). Совпадение между ними не
// проверяло НИЧТО: обе половины покрыты своими пробами, а согласие половин —
// ничем.
//
// Цена расхождения измерена на боевом стенде 2026-08-23. Раздача обслуживает
// потоки в КОРНЕ и проксирует их отдельной службе Ory; путь с лишним сегментом
// в эту регулярку не попадает и уезжает в SPA-заглушку `location /`, которая
// отвечает `200` с `index.html`. То есть отказ выглядит как УСПЕХ: браузер
// получает двухсотый код и пустую оболочку консоли, маршрутизатор которой такого
// пути не знает и уводит на `/dashboard`. Ни `404`, ни ошибки в журнале — только
// «регистрация не открывается».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕЙ АДРЕС ВЕРЕН — РЕШЕНО ЗАМЕРОМ, А НЕ ВКУСОМ
//
// Верно то место, которое ОТВЕЧАЕТ ПОЛЬЗОВАТЕЛЮ. На боевом стенде проверено
// тремя наблюдениями: корневой `/registration` отдаёт `303` в поток Ory; поток
// входа уводит браузер на корневой `/login?flow=…`; путь с лишним сегментом
// отдаёт `200` и оболочку. Значит объявления обязаны выводить КОРЕНЬ, а не
// раздача обязана учиться лишнему сегменту.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЧИТАЕТ ГЕЙТ И ЧЕГО НЕ ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЯ — регулярку раздачи и каждое `ui_url:` под `deploy/helm`, —
// поэтому ему не нужны ни helm, ни кластер, и он не умеет пропуститься. Ни один
// перечень путей здесь не выписан: множество обслуживаемых сегментов ВЫВОДИТСЯ
// из самой регулярки, поэтому расширение раздачи не требует правки гейта, а
// сужение немедленно делает его строже.
//
// Путь, собранный шаблонным выражением (`{{ … }}/registration`), гейт объявляет
// НЕРАЗРЕШИМЫМ и роняет прогон. Это не строгость ради строгости: префикс,
// который вычисляет шаблон, может нести любой сегмент, а «мы не смогли
// установить» обязано быть отличимо от «установили, что верно». Гейт, молча
// пропускающий шаблонную форму, имел бы дыру ровно там, где живёт дефект.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// nginxServingChart — раздача консоли. Единственный источник того, какие пути
// потоков обслуживаются; перечень отсюда ВЫВОДИТСЯ, а не выписывается рядом.
const nginxServingChart = "../ui-future/deploy/templates/configmap-nginx.yaml"

// helmDeclarationsRoot — где ищутся объявления браузерных адресов потоков.
const helmDeclarationsRoot = "helm"

// flowLocationRe — полоса потоков в раздаче: `location ~ ^/(a|b|c)(/|$)`.
// Захватывается сама альтернатива, поэтому множество сегментов есть функция
// дерева, а не константа этого файла.
var flowLocationRe = regexp.MustCompile(`location\s+~\s+\^/\(([a-z0-9|_-]+)\)\(/\|\$\)`)

// uiURLRe — объявление браузерного адреса потока в чарте.
var uiURLRe = regexp.MustCompile(`(?m)^\s*ui_url:\s*(.+?)\s*$`)

// servedFlowSegments — множество первых сегментов пути, которые раздача
// обслуживает как поток личности.
func servedFlowSegments(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(nginxServingChart)
	if err != nil {
		t.Fatalf("раздача консоли %s не читается (%v) — предпосылка гейта исчезла", nginxServingChart, err)
	}
	m := flowLocationRe.FindSubmatch(b)
	if m == nil {
		t.Fatalf("в %s не найдена полоса потоков вида `location ~ ^/(…)(/|$)`. "+
			"Либо раздача перестала обслуживать потоки, либо изменилась её форма — "+
			"в обоих случаях согласие с объявлениями больше не установлено", nginxServingChart)
	}
	out := map[string]bool{}
	for _, seg := range strings.Split(string(m[1]), "|") {
		if seg = strings.TrimSpace(seg); seg != "" {
			out[seg] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("полоса потоков найдена, но не назвала ни одного сегмента — сверять не с чем")
	}
	return out
}

// flowPathOf — путь, который объявление кладёт в адресную строку браузера.
//
// Возвращает (путь, ok). ok=false означает НЕРАЗРЕШИМО: начало пути вычисляет
// шаблон, и какой сегмент окажется первым — отсюда не видно.
func flowPathOf(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	v = strings.Trim(v, `"'`)
	if i := strings.Index(v, "#"); i >= 0 && !strings.Contains(v[:i], "{{") {
		v = strings.TrimSpace(v[:i])
	}
	// Абсолютный адрес: снять схему и власть (в них шаблон законен — он задаёт хост).
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(v, scheme) {
			rest := v[len(scheme):]
			if i := firstSlashOutsideTemplate(rest); i >= 0 {
				v = rest[i:]
			} else {
				return "", false // адрес без пути — первого сегмента не существует
			}
			break
		}
	}
	// Путь обязан быть literal: шаблон в нём делает первый сегмент неизвестным.
	if strings.Contains(v, "{{") || !strings.HasPrefix(v, "/") {
		return "", false
	}
	return v, true
}

// firstSlashOutsideTemplate — индекс первого `/`, не попавшего внутрь `{{ … }}`.
func firstSlashOutsideTemplate(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "{{"):
			depth++
			i++
		case strings.HasPrefix(s[i:], "}}"):
			if depth > 0 {
				depth--
			}
			i++
		case s[i] == '/' && depth == 0:
			return i
		}
	}
	return -1
}

// firstSegment — первый сегмент литерального пути.
func firstSegment(path string) string {
	return strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)[0]
}

// adjudicateFlowURL — вердикт по одному объявлению. Одна функция на гейт и на
// инъекцию: копия разошлась бы с оригиналом молча.
//
// Исходы: "обслуживается" | "не обслуживается" | "неразрешимо".
func adjudicateFlowURL(raw string, served map[string]bool) string {
	path, ok := flowPathOf(raw)
	if !ok {
		return "неразрешимо"
	}
	if served[firstSegment(path)] {
		return "обслуживается"
	}
	return "не обслуживается"
}

// TestIdentityFlowPathIsServedByTheConsole — каждый браузерный адрес потока,
// который объявляет чарт, раздача консоли обслуживает.
func TestIdentityFlowPathIsServedByTheConsole(t *testing.T) {
	served := servedFlowSegments(t)

	var files, decls int
	var findings []string

	err := filepath.WalkDir(helmDeclarationsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" && ext != ".tpl" {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if !strings.Contains(string(b), "ui_url:") {
			return nil
		}
		files++
		for _, m := range uiURLRe.FindAllStringSubmatch(string(b), -1) {
			decls++
			switch verdict := adjudicateFlowURL(m[1], served); verdict {
			case "обслуживается":
			case "не обслуживается":
				p, _ := flowPathOf(m[1])
				findings = append(findings, fmt.Sprintf(
					"%s: объявлен %q → первый сегмент пути %q, а раздача консоли его НЕ обслуживает",
					path, strings.TrimSpace(m[1]), firstSegment(p)))
			case "неразрешимо":
				findings = append(findings, fmt.Sprintf(
					"%s: объявлен %q → начало пути вычисляет шаблон, поэтому первый сегмент отсюда НЕИЗВЕСТЕН. "+
						"Согласие с раздачей не установлено: закрепи префикс потока литералом либо научи гейт его читать",
					path, strings.TrimSpace(m[1])))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s прерван: %v", helmDeclarationsRoot, err)
	}

	segs := make([]string, 0, len(served))
	for s := range served {
		segs = append(segs, s)
	}
	sort.Strings(segs)
	t.Logf("перепись: раздача обслуживает %d сегмент(ов) [%s]; прочитано файлов с объявлениями %d; объявлений %d; находок %d",
		len(segs), strings.Join(segs, " "), files, decls, len(findings))

	if decls == 0 {
		t.Fatalf("под %s не найдено ни одного объявления `ui_url:` — гейту нечего было сверять, "+
			"и «ноль находок» здесь означало бы «ноль прочитанного»", helmDeclarationsRoot)
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("адрес(а), которые служба личности выдаёт браузеру, раздача консоли не обслуживает:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
