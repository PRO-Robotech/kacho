// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_delivery_injection_test.go — доказательство того, что
// соседняя проверка СПОСОБНА упасть, и того, что она падает НЕ НА ВСЁМ.
//
// Проверка, читающая объявления, зеленеет на любом дереве, если её предикат
// промахивается мимо предмета; отличить это от исправной работы нельзя ничем,
// кроме подачи ей настоящего дефекта. Поэтому по каждой оси идут ДВЕ подачи:
// дефект — обязана заговорить и НАЗВАТЬ КООРДИНАТУ; законный близнец той же
// формы — обязана смолчать.
//
// ГРАНИЦА СНЯТИЯ НАЗВАНА: снимаются комментарий ЦЕЛОЙ СТРОКОЙ (`#`) и блок
// шаблона (`{{/* */}}`). Хвостовой комментарий после значения не снимается
// намеренно — решётка встречается внутри строк, и снятие по ней резало бы
// объявления. В этих файлах ключи доставки в хвостовых комментариях не стоят;
// появятся — ось придётся расширить, и это сказано здесь, а не подразумевается.
//
// Отдельная ось — СОБСТВЕННОЕ ОБЪЯСНЕНИЕ проверяемого. Об этих ключах в тех же
// файлах написана проза, и она называет их дословно. Ключ, оставленный ТОЛЬКО в
// комментарии, обязан читаться как отсутствующий: иначе проверка зачла бы за
// исполнение рассказ об исполнении.

import (
	"strings"
	"testing"
)

// mutate — подача с ОДНОЙ снятой осью: остальное остаётся целым, поэтому
// покраснеть обязано только проверяемое.
func mutate(base manifestDeliveryDecls, f func(*manifestDeliveryDecls)) manifestDeliveryDecls {
	out := base
	f(&out)
	return out
}

func TestManifestDeliveryAuditFallsOnEachAxisAndStaysSilentOnItsTwin(t *testing.T) {
	base := readManifestDeliveryDecls(t)

	// КОНТРОЛЬ. Целые объявления — молчание. Без него любое отрицание ниже
	// зеленело бы на сломанном дереве.
	if findings, census := auditManifestDelivery(base); len(findings) != 0 {
		t.Fatalf("контроль: на целых объявлениях находок %d, ожидалось 0 (осмотрено байт %d): %v",
			len(findings), census.BytesJudged, findings)
	}

	cases := []struct {
		name    string
		decls   manifestDeliveryDecls
		wantSub string // подстрока, которую находка обязана нести — координата и предмет
	}{
		{
			name: "под не монтирует том — снят ключ источника",
			decls: mutate(base, func(d *manifestDeliveryDecls) {
				d.deployment = strings.ReplaceAll(d.deployment,
					".Values.manifests.configMapName", ".Values.opaSidecar.enabled")
			}),
			wantSub: "templates/deployment.yaml: `.Values.manifests.configMapName` не читается",
		},
		{
			name: "каталог пода стал вторым литералом",
			decls: mutate(base, func(d *manifestDeliveryDecls) {
				d.deployment = strings.ReplaceAll(d.deployment,
					"{{ .Values.manifests.mountPath | quote }}", `"/etc/kaname/manifests"`)
			}),
			wantSub: "templates/deployment.yaml: `.Values.manifests.mountPath` не читается",
		},
		{
			name: "каталог процесса стал вторым литералом",
			decls: mutate(base, func(d *manifestDeliveryDecls) {
				d.configmap = strings.ReplaceAll(d.configmap,
					"{{ .Values.manifests.mountPath | quote }}", `"/etc/kaname/manifests"`)
			}),
			wantSub: "templates/configmap.yaml: `manifests.dir` не выводится",
		},
		{
			name: "секции ручки нет в конфигурации процесса",
			decls: mutate(base, func(d *manifestDeliveryDecls) {
				d.configmap = strings.ReplaceAll(d.configmap, "\n    manifests:\n", "\n")
			}),
			wantSub: "templates/configmap.yaml: секции `manifests:` нет",
		},
		{
			name: "посадка не объявляет каталога",
			decls: mutate(base, func(d *manifestDeliveryDecls) {
				d.values = strings.ReplaceAll(d.values,
					"  mountPath: /etc/kaname/manifests", "  mountPath: \"\"")
			}),
			wantSub: "values.yaml: `manifests.mountPath` не объявлен",
		},
		{
			name: "ключ источника исчез из значений — не пуст, а отсутствует",
			decls: mutate(base, func(d *manifestDeliveryDecls) {
				d.values = strings.ReplaceAll(d.values, "\n  configMapName: \"\"", "")
			}),
			wantSub: "values.yaml: `manifests.configMapName` не объявлен",
		},
		{
			// СОБСТВЕННОЕ ОБЪЯСНЕНИЕ ПРОВЕРЯЕМОГО — ось, без которой проверка
			// зачла бы РАССКАЗ об исполнении за исполнение.
			//
			// Исполнение снято (ключ заменён вторым литералом), а проза о нём
			// оставлена и называет тот же ключ ДОСЛОВНО. Без снятия комментариев
			// эта подача осталась бы ЗЕЛЁНОЙ при сломанном стыке — то есть дала
			// бы ровно тот тихий исход, ради которого ось и заведена.
			name: "ключ остался ТОЛЬКО в комментарии",
			decls: mutate(base, func(d *manifestDeliveryDecls) {
				d.configmap = strings.ReplaceAll(d.configmap,
					"      dir: {{ .Values.manifests.mountPath | quote }}",
					"      # каталог берётся из .Values.manifests.mountPath\n"+
						`      dir: "/etc/kaname/manifests"`)
			}),
			wantSub: "templates/configmap.yaml: `manifests.dir` не выводится",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings, _ := auditManifestDelivery(c.decls)
			if len(findings) == 0 {
				t.Fatalf("инъекция не покраснела — проверка не судит эту ось")
			}
			var hit bool
			for _, f := range findings {
				if strings.Contains(f, c.wantSub) {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("находка есть, но НЕ НАЗЫВАЕТ предмет %q — читатель пойдёт искать не там; получено: %v",
					c.wantSub, findings)
			}
			// Инъекция обязана ронять ТОЛЬКО проверяемое: одна снятая ось — одна
			// находка. Больше означало бы, что подача задела соседний контроль и
			// красное пришло от него.
			if len(findings) != 1 {
				t.Fatalf("одна снятая ось дала находок %d, ожидалась 1 — подача задела соседний контроль: %v",
					len(findings), findings)
			}
		})
	}

	// ЗАКОННЫЕ БЛИЗНЕЦЫ — та же форма, законное содержание: молчание обязательно.
	twins := []struct {
		name  string
		decls manifestDeliveryDecls
	}{
		{
			// Доставка ВЫКЛЮЧЕНА посадкой: имя источника объявлено пустым. Это
			// решение посадки, а не пропуск, и находкой быть не вправе.
			name:  "источник объявлен пустым — доставка выключена осознанно",
			decls: base,
		},
		{
			// Каталог сменён: он свойство посадки, и его величину проверка не судит.
			name: "каталог доставки переименован",
			decls: mutate(base, func(d *manifestDeliveryDecls) {
				d.values = strings.ReplaceAll(d.values,
					"mountPath: /etc/kaname/manifests", "mountPath: /var/lib/kacho/manifests")
			}),
		},
		{
			// Опора объявлена: страж старта это судит, проверка объявлений — нет.
			name: "посадка объявила опору на манифесты",
			decls: mutate(base, func(d *manifestDeliveryDecls) {
				d.values = strings.ReplaceAll(d.values, "  required: false", "  required: true")
			}),
		},
	}
	for _, tw := range twins {
		t.Run("близнец: "+tw.name, func(t *testing.T) {
			if findings, _ := auditManifestDelivery(tw.decls); len(findings) != 0 {
				t.Fatalf("законный близнец покраснел — проверка ловит форму, а не существо: %v", findings)
			}
		})
	}
}
