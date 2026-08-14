// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package blockbackend

import (
	"fmt"
	"regexp"
)

// installPrefixRe — форма префикса установки: строчные буквы и цифры, 2..16
// символов. Узкая намеренно: префикс попадает в имя объекта у чужого хранилища, и
// экзотика там оборачивается отказами, которые нечем объяснить.
var installPrefixRe = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)

// ValidateInstallPrefix проверяет форму префикса установки.
//
// # Почему префикс обязателен
//
// Имя объекта выводится из идентификатора ресурса — значит два развёртывания
// Kachō, нацеленные на ОДИН кластер хранилища, выведут одинаковые имена и
// «усыновят» объекты друг друга: сверщик первого увидит объекты второго как
// собственные и посчитает их утечкой, а удаление в одном облаке снесёт данные в
// другом. Это не идемпотентность, а перекрёстное владение. Префикс установки —
// единственное, что отличает наши объекты от чужих в общем пространстве.
func ValidateInstallPrefix(p string) error {
	if p == "" {
		return fmt.Errorf("install prefix is required: without it two deployments sharing one backend cluster would adopt each other's objects")
	}
	if !installPrefixRe.MatchString(p) {
		return fmt.Errorf("install prefix %q must match %s", p, installPrefixRe.String())
	}
	return nil
}

// ObjectName выводит имя объекта у бэкенда из префикса установки и НЕИЗМЕНЯЕМОГО
// идентификатора ресурса.
//
// Функция чистая и детерминированная — на этом стоит идемпотентность повтора:
// исполнитель операций переисполняет работу не по ошибке, а by construction, и
// повтор обязан попадать в тот же объект, а не создавать второй.
//
// Имя вычислимо арендатором: идентификатор ресурса он видит. Отсюда два следствия,
// которые обязан помнить каждый, кто трогает адаптер: авторизация НИГДЕ не
// опирается на неугадываемость имени, и адаптер имя ВЫВОДИТ, никогда не принимая
// его из запроса.
func ObjectName(installPrefix, resourceID string) string {
	return installPrefix + "-" + resourceID
}

// SnapshotObjectName выводит имя снимка. Снимок адресуется как самостоятельный
// объект, а не как суффикс тома: том может быть удалён раньше снимка, и имя,
// производное от тома, пережило бы свой источник.
func SnapshotObjectName(installPrefix, snapshotID string) string {
	return ObjectName(installPrefix, snapshotID)
}

// NamespaceOfProject выводит единицу изоляции арендатора из идентификатора проекта.
//
// Без неё все арендаторы класса делят одно пространство имён у бэкенда: шумный
// сосед ничем не ограничен, а любая ошибка в правах на стороне хранилища сразу
// межарендная. Шаблон приходит из ревизии привязки — здесь только подстановка.
func NamespaceOfProject(template, projectID string) string {
	if template == "" {
		return projectID
	}
	return replaceProject(template, projectID)
}

// replaceProject подставляет идентификатор проекта в шаблон. Единственная
// подстановка, которую шаблон допускает: расширять её значит заводить язык, за
// которым никто не следит.
func replaceProject(template, projectID string) string {
	const placeholder = "{projectId}"
	out := ""
	for {
		i := indexOf(template, placeholder)
		if i < 0 {
			return out + template
		}
		out += template[:i] + projectID
		template = template[i+len(placeholder):]
	}
}

func indexOf(s, sub string) int {
	if len(sub) == 0 || len(s) < len(sub) {
		return -1
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
