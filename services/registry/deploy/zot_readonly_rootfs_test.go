// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// zot_readonly_rootfs_test.go — корень контейнера хранилища слоёв только на чтение.
//
// Что тут защищается. zot — сторонний образ, который принимает и отдаёт слои
// произвольных тенантов, то есть обрабатывает недоверенный ввод по своей роли.
// Записываемый корень у такого процесса означает, что успешная эксплуатация
// оставляет след: подменённый бинарь, дописанный конфиг, положенный рядом
// исполняемый файл переживают перезапуск процесса, а с ним и попытку «просто
// перекатить под».
//
// Почему проба нужна ОТДЕЛЬНО от IaC-скана. Скан это же свойство проверяет
// (AVD-KSV-0014), но лишь до тех пор, пока чарт вообще ПОПАДАЕТ в его цели, а
// попадает он только потому, что заглушки в корневом trivy.yaml снимают
// намеренный отказ рендера без учётных данных. Пропади этот файл — чарт молча
// уходит из осмотра, и «ноль находок» станет означать «ноль прочитанного».
// Здешняя проба от той конструкции не зависит вовсе: она читает рендер.
//
// Чем это было прежде. Значение стояло выключенным с пояснением «zot пишет
// cache/tmp» — предположение, из-за которого путь чарта держался в исключениях
// IaC-скана (kacho#4). Оно не подтвердилось: весь запись-путь zot лежит внутри
// storage.path (том). Проверено прогоном образа v2.1.18 под read-only корнем —
// старт, push образа 225 МБ, pull обратно, запрос к расширению search.
package deploy_test

import (
	"strings"
	"testing"
)

// zotContainerBlock вырезает описание контейнера zot из StatefulSet — чтобы
// утверждение относилось К НЕМУ, а не к любому совпадению строки в документе
// (в том же файле есть securityContext пода).
func zotContainerBlock(t *testing.T, sts string) string {
	t.Helper()
	const marker = "- name: zot"
	i := strings.Index(sts, marker)
	if i < 0 {
		t.Fatalf("в StatefulSet нет контейнера zot:\n%s", sts)
	}
	rest := sts[i:]
	if j := strings.Index(rest, "\n      volumes:"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// TestZotContainer_RootFilesystemIsReadOnly — умолчание чарта обязано быть
// «корень только на чтение». Читается рендер, а не values: между значением и
// подом стоит шаблон, и проверять надо то, что уедет в кластер.
func TestZotContainer_RootFilesystemIsReadOnly(t *testing.T) {
	sts := docOf(t, helmTemplate(t, "zot.auth.password="+renderPassword),
		"statefulset-zot.yaml", "kind: StatefulSet")
	block := zotContainerBlock(t, sts)
	if !strings.Contains(block, "readOnlyRootFilesystem: true") {
		t.Errorf("контейнер zot едет с записываемым корнем — сторонний обработчик "+
			"недоверенного ввода, у которого правка файловой системы переживает "+
			"перезапуск:\n%s", block)
	}
	for _, want := range []string{
		"allowPrivilegeEscalation: false",
		"runAsNonRoot: true",
		"drop:",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("контейнер zot не объявляет %q:\n%s", want, block)
		}
	}
}

// TestZotContainer_ReadOnlyRootIsReadFromValues — вторая половина инъекции:
// проба обязана читать РЕНДЕР значения, а не находить строку, которая стоит в
// шаблоне неизменно. Перевод ручки в false обязан быть виден пробе — иначе
// утверждение выше зеленело бы при любом значении.
func TestZotContainer_ReadOnlyRootIsReadFromValues(t *testing.T) {
	sts := docOf(t, helmTemplate(t,
		"zot.auth.password="+renderPassword,
		"zot.containerSecurityContext.readOnlyRootFilesystem=false",
	), "statefulset-zot.yaml", "kind: StatefulSet")
	block := zotContainerBlock(t, sts)
	if !strings.Contains(block, "readOnlyRootFilesystem: false") {
		t.Fatalf("перевод ручки в false не виден в рендере — значит проба выше "+
			"утверждает не про значение, а про неизменную строку шаблона:\n%s", block)
	}
}
