//go:build helmcharts

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notify_probe_manifest_delivery_test.go — манифест стендовой пробы-источника
// notify доставляется РОВНО тем цепочкам, которые поднимают саму пробу (решение
// владельца 2026-10-07: манифест notify-probe — только на стенде, в боевую
// установку не попадает).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Манифест модуля — строка модели прав, а не описание: применитель службы
// доступа заводит по строке `notifications {namespace: notify-probe, readers:
// [notify]}` кортеж reader и запись выдачи. Безусловный обход производителя
// (`services/*/manifest.yaml`) раздаёт найденное ВСЕМ цепочкам — и в боевой
// установке, где пробы нет, появились бы права на ленту несуществующей службы.
// Поэтому манифест пробы лежит ВНЕ безусловного обхода
// (`services/notify/probe/manifest.yaml`), а доставку объявляет профиль стенда
// перечнем `kaname.manifests.conditional` с условием `notifyProbe.enabled` —
// тем же значением, которым чарт поднимает саму пробу.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ — и почему вторая сторона берётся из РЕНДЕРА, а не из профиля
//
// По каждому стенду таблицы stacks.txt сравниваются два независимых исхода:
//
//	доставлено — ключ манифеста пробы в объекте, собранном производителем
//	             (corelib/modulemanifest/producer) для цепочки стенда;
//	поднято    — нагрузка пробы (Deployment kacho-notify-probe) в выводе
//	             `helm template` той же цепочки.
//
// Находка — их расхождение в ЛЮБУЮ сторону:
//
//	доставлено, не поднято — права на ленту службы, которой в установке нет
//	                         (предмет решения владельца);
//	поднято, не доставлено — проба есть, а notify её ленту не читает: кортежа
//	                         reader у службы доступа нет, доставка стоит.
//
// «Поднято» читается из рендера, а не из значения `notifyProbe.enabled`: прочтя
// значение, проверка повторила бы закон производителя и согласилась бы с ним на
// его же ошибке. Рендер — исход чарта, второго закона наложения здесь нет.
//
// Ключ судится независимо от того, ОТКУДА манифест взялся: положи его в
// безусловный обход (`services/notify-probe/manifest.yaml`) — ключ тот же, и
// боевые стенды покраснеют. Это доказывает инъекция в соседнем файле.
//
// Предпосылка — обе стороны непусты: хотя бы один стенд пробу поднимает и хотя
// бы один не поднимает. Иначе «расхождений ноль» неотличимо от «сравнивать было
// не с чем».

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	manifestproducer "github.com/PRO-Robotech/corelib/modulemanifest/producer"
)

const (
	// probeManifestSource — манифест пробы в дереве, вне безусловного обхода.
	probeManifestSource = "services/notify/probe/manifest.yaml"
	// probeManifestKey — ключ доставки манифеста пробы: каталог источника под
	// services/ с `/`, заменённым на `-`. Тот же ключ дала бы и безусловная
	// раскладка `services/notify-probe/`, поэтому судится ключ, а не путь.
	probeManifestKey = "notify-probe.manifest.yaml"
	// probeWorkload — нагрузка пробы в рендере зонта (templates/notify-probe.yaml).
	probeWorkload = "kacho-notify-probe"
)

// probeDeliveryRow — исходы одного стенда.
type probeDeliveryRow struct {
	Stack     string
	Rendered  bool
	Delivered bool
}

// probeDeliveryCensus — объём осмотренного.
type probeDeliveryCensus struct {
	Stacks    int
	Rendered  int
	Delivered int
}

func (c probeDeliveryCensus) Summary() string {
	return fmt.Sprintf("стендов %d · пробу поднимают %d · манифест пробы доставлен %d",
		c.Stacks, c.Rendered, c.Delivered)
}

// auditProbeManifestDelivery — расхождения «доставлено» и «поднято». Чистая
// функция: исходы собирает вызывающий, суд — здесь, и его же зовёт инъекция.
func auditProbeManifestDelivery(rows []probeDeliveryRow) ([]string, probeDeliveryCensus) {
	census := probeDeliveryCensus{Stacks: len(rows)}
	var findings []string
	for _, r := range rows {
		if r.Rendered {
			census.Rendered++
		}
		if r.Delivered {
			census.Delivered++
		}
		switch {
		case r.Delivered && !r.Rendered:
			findings = append(findings, fmt.Sprintf("стенд %s: манифест пробы (%s) доставлен, "+
				"а Deployment %s цепочка не поднимает — применитель службы доступа заведёт кортеж "+
				"reader и запись выдачи на ленту службы, которой в установке нет",
				r.Stack, probeManifestKey, probeWorkload))
		case r.Rendered && !r.Delivered:
			findings = append(findings, fmt.Sprintf("стенд %s: Deployment %s поднят, а манифест "+
				"пробы (%s) не доставлен — кортежа reader на её ленту нет, notify её не читает",
				r.Stack, probeWorkload, probeManifestKey))
		}
	}
	return findings, census
}

// renderChainAbs — `helm template` зонта по цепочке профилей (пути от каталога
// deploy либо абсолютные — приводятся к абсолютным, чтобы рендер и производитель
// читали одни и те же файлы). Отсутствие helm — отказ, а не пропуск: «не
// отрендерено» неотличимо было бы от «пробы нет».
func renderChainAbs(t *testing.T, chain []string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm не в PATH — «поднято» читать неоткуда, вердикт не выносится: %v", err)
	}
	abs := make([]string, 0, len(chain))
	for _, p := range chain {
		a, err := filepath.Abs(p)
		if err != nil {
			t.Fatalf("путь профиля %s не разрешён: %v", p, err)
		}
		abs = append(abs, a)
	}
	out, err := renderChainFiles(umbrellaDir, abs)
	if err != nil {
		t.Fatalf("рендер цепочки %v отказал: %v\n%s", chain, err, out)
	}
	return out
}

// renderedHasProbeWorkload — есть ли в рендере Deployment пробы. Документ, не
// разобранный YAML, — отказ: усечённый разбор молча терял бы объекты.
func renderedHasProbeWorkload(t *testing.T, rendered string) bool {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	docs := 0
	found := false
	for {
		var d map[string]any
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("документ рендера №%d не разобран: %v", docs+1, err)
		}
		docs++
		meta, _ := d["metadata"].(map[string]any)
		if d["kind"] == "Deployment" && meta["name"] == probeWorkload {
			found = true
		}
	}
	if docs == 0 {
		t.Fatal("рендер не дал ни одного документа — «пробы нет» неотличимо от «ничего не прочитано»")
	}
	return found
}

// deliveredProbeManifest — доставлен ли цепочке манифест пробы. Цепочка, не
// объявившая доставку, не доставляет ничего — это законный исход.
func deliveredProbeManifest(t *testing.T, root, stack string, profiles []string) bool {
	t.Helper()
	d, err := manifestproducer.Collect(root, profiles)
	if errors.Is(err, manifestproducer.ErrNotDeclared) {
		return false
	}
	if err != nil {
		t.Fatalf("стенд %s: производитель отказал: %v (%s)", stack, err, d.Census.Summary())
	}
	for _, s := range d.Sources {
		if s.Key() == probeManifestKey {
			return true
		}
	}
	return false
}

// TestStandOnlyProbeManifestReachesExactlyTheChainsThatRaiseTheProbe — в
// цепочках без пробы манифеста пробы нет, в цепочках с пробой — есть.
func TestStandOnlyProbeManifestReachesExactlyTheChainsThatRaiseTheProbe(t *testing.T) {
	if _, err := os.Stat(filepath.Join(repoRoot, probeManifestSource)); err != nil {
		t.Fatalf("манифест пробы %s не найден: %v — доставлять стенду нечего", probeManifestSource, err)
	}
	stacks := deployStacksForRender(t, renderGateOperatorSample)
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]probeDeliveryRow, 0, len(names))
	for _, stack := range names {
		chain := chainPaths(stacks[stack])
		rows = append(rows, probeDeliveryRow{
			Stack:     stack,
			Rendered:  renderedHasProbeWorkload(t, renderChainAbs(t, chain)),
			Delivered: deliveredProbeManifest(t, repoRoot, stack, chain),
		})
	}

	findings, census := auditProbeManifestDelivery(rows)
	t.Logf("осмотрено: %s", census.Summary())
	if census.Rendered == 0 || census.Rendered == census.Stacks {
		t.Fatalf("предпосылка не выполнена: пробу поднимают %d стендов из %d — сравнивать "+
			"доставку не с чем, «расхождений ноль» было бы беспредметным", census.Rendered, census.Stacks)
	}
	for _, f := range findings {
		t.Error(f)
	}
}
