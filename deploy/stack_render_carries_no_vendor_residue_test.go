// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// stack_render_carries_no_vendor_residue_test.go — РЕНДЕР КАЖДОЙ ЦЕПОЧКИ ЗОНТА
// НЕ НЕСЁТ СЛЕДА ПРЕЖНЕГО ПОСТАВЩИКА ЛИЧНОСТИ: ни его объекта, ни его адреса у
// наших нагрузок (kacho#2818; прежде — половина own гейта ключа файла службы
// личности, kacho#2816).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Посадка личности у службы доступа одна — своя (kaname#363), и её объявляют
// все цепочки deploy/stacks.txt. Поставщика на стенде нет: его подчарты
// выключены в базе зонта. Отсутствие обязано быть НАСТОЯЩИМ:
//
//   - в рендере нет ни одного объекта поставщика — имя или образ называет
//     kratos|hydra|ory;
//   - ни одна наша нагрузка не называет адреса или издателя поставщика ни
//     переменной, ни картой настроек (сосед, которого на цепочке нет, либо
//     издатель, чей набор ключей никто не держит).
//
// Судится ПОЛНЫЙ рендер зонта — поэтому тег `helmcharts`: рендер требует
// материализованных зависимостей. Прежде это утверждение было половиной гейта
// ключа файла службы личности; вторая его половина судила НАШУ карту настроек
// поставщика, которую подчарт службы больше не производит (kacho#2818), и
// снята вместе с ней. Эта половина переехала сюда без смены вердикта.
//
// ─────────────────────────────────────────────────────────────────────────────
// КОНТРОЛЬ ПЕРЕПИСИ — СЛЕД ПОСТАВЩИКА, ВНЕСЁННЫЙ В НАСТОЯЩИЙ РЕНДЕР
//
// «Следа ноль» на всех цепочках неотличимо от переписи, которая не видит
// поставщика нигде. Прежде контроль поднимал подчарт поставщика одним фактом
// (`hydra.enabled=true`); подчарт снят физически (kacho#1276), и поднимать
// стало нечего — поэтому контроль переехал на синтетику, а не молча потерял
// силу. Каждая цепочка рендерится НАСТОЯЩИМ helm, и к её документам дописываются
// ровно те формы следа, которые перепись обязана находить: рабочий объект с
// образом поставщика, объект с именем поставщика без образа и карта настроек с
// адресом поставщика. Перепись обязана найти каждую — на КАЖДОЙ цепочке, а не в
// сумме: сумма скрыла бы цепочку, на которой разбор ослеп.
package deploy_test

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// vendorResidueWord — слово поставщика в имени объекта или в ссылке на образ.
// Граница у `ory` — словесная: без неё словом поставщика считались бы
// посторонние имена, содержащие эти три буквы.
var vendorResidueWord = regexp.MustCompile(`(?i)kratos|hydra|oryd|\bory\b`)

// vendorResidueAddress — адрес поставщика в величине: схема и хост/путь,
// называющие kratos|hydra. Путь к файлу адресом не является и печатается
// переписью отдельно.
var vendorResidueAddress = regexp.MustCompile(`(?i)https?://[^\s,"']*(kratos|hydra)`)

// vendorInjectedResidue — след поставщика, дописываемый к настоящему рендеру
// цепочки для контроля переписи: объекты (рабочий с образом поставщика и
// объект с именем поставщика) и адрес поставщика в карте настроек нашей формы.
func vendorInjectedResidue() []renderedDoc {
	return []renderedDoc{
		{"kind": "Deployment", "metadata": map[string]any{"name": "probe-issuer"},
			"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
				"containers": []any{map[string]any{"name": "issuer", "image": "docker.io/oryd/hydra:v26.2.0"}},
			}}}},
		{"kind": "Service", "metadata": map[string]any{"name": "probe-kratos-public"}},
		{"kind": "ConfigMap", "metadata": map[string]any{"name": "probe-config"},
			"data": map[string]any{"config.yaml": "issuer: https://hydra.api.kacho.cloud/\n"}},
	}
}

// vendorInjectedWant — сколько следа перепись обязана найти в
// vendorInjectedResidue: объектов и адресов.
const vendorInjectedObjects, vendorInjectedAddresses = 2, 1

// vendorResidue — след стека поставщика в рендере.
type vendorResidue struct {
	Objects   []string // <Kind>/<имя> [образ…] — объекты поставщика
	Addresses []string // адреса поставщика у наших нагрузок и карт
	NameOnly  []string // имя переменной называет поставщика, величина адресом не является
}

func vendorPodSpecOf(d renderedDoc) map[string]any {
	spec := submap(d, "spec")
	switch str(d, "kind") {
	case "Pod":
		return spec
	case "CronJob":
		return submap(submap(submap(submap(spec, "jobTemplate"), "spec"), "template"), "spec")
	}
	return submap(submap(spec, "template"), "spec")
}

func vendorPodContainers(pod map[string]any) []map[string]any {
	var out []map[string]any
	for _, k := range []string{"initContainers", "containers"} {
		for _, c := range slice(pod, k) {
			if cm, ok := c.(map[string]any); ok {
				out = append(out, cm)
			}
		}
	}
	return out
}

// vendorResidueOf — перепись следа поставщика по одному рендеру.
func vendorResidueOf(docs []renderedDoc) vendorResidue {
	var r vendorResidue
	for _, d := range docs {
		kind, name := str(d, "kind"), str(submap(d, "metadata"), "name")
		pod := vendorPodSpecOf(d)
		var images []string
		for _, c := range vendorPodContainers(pod) {
			if img := str(c, "image"); vendorResidueWord.MatchString(img) {
				images = append(images, img)
			}
		}
		if vendorResidueWord.MatchString(name) || len(images) > 0 {
			obj := kind + "/" + name
			if len(images) > 0 {
				obj += " " + fmt.Sprint(images)
			}
			r.Objects = append(r.Objects, obj)
			continue
		}
		for _, c := range vendorPodContainers(pod) {
			for _, e := range slice(c, "env") {
				em, _ := e.(map[string]any)
				n, v := str(em, "name"), str(em, "value")
				pair := name + "/" + str(c, "name") + " " + n + "=" + v
				switch {
				case vendorResidueAddress.MatchString(v):
					r.Addresses = append(r.Addresses, pair)
				case vendorResidueWord.MatchString(n):
					r.NameOnly = append(r.NameOnly, pair)
				}
			}
		}
		if kind == "ConfigMap" {
			data := submap(d, "data")
			keys := make([]string, 0, len(data))
			for k := range data {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v, _ := data[k].(string)
				for _, line := range strings.Split(v, "\n") {
					t := strings.TrimSpace(line)
					if strings.HasPrefix(t, "#") {
						continue
					}
					if m := vendorResidueAddress.FindString(t); m != "" {
						r.Addresses = append(r.Addresses, name+"/"+k+" "+t)
					}
				}
			}
		}
	}
	sort.Strings(r.Objects)
	sort.Strings(r.Addresses)
	sort.Strings(r.NameOnly)
	return r
}

// judgeVendorAbsence — находки на стеке: отсутствие поставщика обязано быть
// настоящим. Чистая функция — инъекция подаёт ей рендер с внесённым следом.
func judgeVendorAbsence(stack string, r vendorResidue) []string {
	var out []string
	for _, o := range r.Objects {
		out = append(out, fmt.Sprintf("стек %q (посадка службы — своя полоса), а рендер несёт объект "+
			"поставщика %s — нагрузка, которую никто не читает, либо карта, которую никто не монтирует",
			stack, o))
	}
	for _, a := range r.Addresses {
		out = append(out, fmt.Sprintf("стек %q: наша нагрузка называет адрес поставщика: %s — сосед, "+
			"которого на этой цепочке нет, либо издатель, чей набор ключей никто не держит", stack, a))
	}
	return out
}

// TestNoStackRenderCarriesAVendorResidue — сам гейт по каждой цепочке.
func TestNoStackRenderCarriesAVendorResidue(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var docsSum, controlSeen int
	for _, name := range names {
		rendered, err := renderStack(t, stacks[name])
		if err != nil {
			t.Fatalf("стек %q не рендерится (%v) — вердикта НЕТ. Вывод helm:\n%s", name, err, rendered)
		}
		docs := decodeRender(t, rendered)
		if len(docs) == 0 {
			t.Fatalf("стек %q: рендер пуст — «следа ноль» здесь означало бы «не прочитано ничего»", name)
		}
		docsSum += len(docs)
		r := vendorResidueOf(docs)
		t.Logf("осмотрено: стек %-12s · документов %3d · объектов поставщика %d · адресов поставщика у "+
			"наших %d · имён без адреса %d %v", name, len(docs), len(r.Objects), len(r.Addresses),
			len(r.NameOnly), r.NameOnly)
		for _, f := range judgeVendorAbsence(name, r) {
			t.Error(f)
		}

		ctl := vendorResidueOf(append(append([]renderedDoc{}, docs...), vendorInjectedResidue()...))
		gotObj, gotAddr := len(ctl.Objects)-len(r.Objects), len(ctl.Addresses)-len(r.Addresses)
		if gotObj != vendorInjectedObjects || gotAddr != vendorInjectedAddresses {
			t.Errorf("стек %q: в рендер внесено объектов поставщика %d и адресов %d, а перепись "+
				"нашла %d и %d — перепись слепа на этой цепочке, и её «следа ноль» ничего не доказывает",
				name, vendorInjectedObjects, vendorInjectedAddresses, gotObj, gotAddr)
		}
		controlSeen += gotObj + gotAddr
	}
	t.Logf("итого: стеков %d · документов %d · следа поставщика, найденного переписью в "+
		"настоящем рендере с внесённым следом, %d (внесено по %d на стек)",
		len(names), docsSum, controlSeen, vendorInjectedObjects+vendorInjectedAddresses)
	if controlSeen == 0 {
		t.Fatalf("в настоящем рендере с внесённым следом перепись нашла НОЛЬ — перепись " +
			"слепа, и «следа ноль» ничего не доказывает")
	}
}

// TestVendorResidueInjection_ReturnedIssuerRedsAndTwinIsSilent — способность
// упасть НАСТОЯЩИМ входом: край боевого стека, которому в перечень приёма
// возвращён издатель поставщика, — находка с адресом; законный близнец — тот же
// перечень с нашим вторым издателем — молчание.
func TestVendorResidueInjection_ReturnedIssuerRedsAndTwinIsSilent(t *testing.T) {
	stacks := deployStacks(t)
	chain, ok := stacks["prod"]
	if !ok {
		t.Fatal("стека prod в таблице нет — вход инъекции исчез")
	}
	for _, c := range []struct {
		name, issuers string
		want          int
	}{
		{"возвращён издатель поставщика", "https://kaname.kacho.local\\,https://hydra.api.kacho.cloud", 1},
		{"близнец: второй издатель наш", "https://kaname.kacho.local\\,https://kaname.api.kacho.cloud", 0},
	} {
		out, err := renderStack(t, chain, "api-gateway.tokenAcceptance.issuers="+c.issuers)
		if err != nil {
			t.Fatalf("%s: рендер отказал: %v\n%s", c.name, err, out)
		}
		got := 0
		for _, f := range judgeVendorAbsence("prod", vendorResidueOf(decodeRender(t, out))) {
			if strings.Contains(f, "hydra.api.kacho.cloud") {
				got++
			}
		}
		if (got > 0) != (c.want > 0) {
			t.Errorf("%s: находок с издателем поставщика %d, ожидалось %s", c.name, got,
				map[bool]string{true: "хотя бы одна", false: "ни одной"}[c.want > 0])
		}
	}
}
