// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// resourcenameform_injection_test.go — доказательство, что гейт формы имени
// СПОСОБЕН упасть, и что он молчит на законной форме того же вида.
//
// Инъекция идёт в ОБЕ стороны, и вторая сторона здесь не формальность. Гейт,
// краснеющий на всякой строке, похожей на регулярку, был бы снят первым же
// ложным срабатыванием — а сняли бы его вместе с настоящей защитой. Поэтому
// рядом с подложенным дефектом всегда стоит законный близнец, отличающийся
// ровно проверяемым признаком.
//
// Дерево синтетическое и живёт в t.TempDir(): проверка, которая для
// самопроверки пишет в рабочую копию, портит чужое состояние (правило о
// неприкосновенности чужого состояния) и делает вердикт функцией диска.
package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

// nameFormSynthTree раскладывает минимальное дерево: канон в pkg/validate плюс
// произвольные добавочные файлы.
func nameFormSynthTree(t *testing.T, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"pkg/validate/nameform/nameform.go": "package nameform\n\n" +
			"const Form = `^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`\n",
		"pkg/validate/validate.go": "package validate\n\n" +
			"func defaultNameForID(id string) string { return id }\n\n" +
			"func NameOrDefault(value, id string) string {\n" +
			"\tif value == \"\" {\n\t\treturn defaultNameForID(id)\n\t}\n\treturn value\n}\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("создание %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", rel, err)
		}
	}
	return root
}

// TestInjection_FormCopyIsFound — подложенная КОПИЯ формы находится и называется
// координатой; законный близнец рядом молчит.
func TestInjection_FormCopyIsFound(t *testing.T) {
	// Сторона «должен упасть»: сервис завёл свою копию канона.
	root := nameFormSynthTree(t, map[string]string{
		"services/vpc/internal/domain/types.go": "package domain\n\n" +
			"var nameVPCRe = `^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`\n",
	})
	_, copies, scanned := scanNameFormDecls(t, newSyntheticTree(t, root))
	if len(copies) != 1 {
		t.Fatalf("подложенная копия формы обязана быть найдена: найдено %d (осмотрено %d файлов)",
			len(copies), scanned)
	}
	if copies[0].file != "services/vpc/internal/domain/types.go" {
		t.Fatalf("находка обязана называть координату; получено %q", copies[0].file)
	}

	// Сторона «должен молчать»: регулярка ДРУГОГО референта — идентификатор роли.
	// Она не является именем ресурса, формой имени не судится и находкой быть
	// не должна. Без этой половины гейт ловил бы форму, а не существо.
	clean := nameFormSynthTree(t, map[string]string{
		"services/iam/internal/domain/types.go": "package domain\n\n" +
			"var roleNameSystemRe = `^roles/[a-z]+\\.[a-z]+$`\n\n" +
			"var repoNameRe = `^[a-z0-9]+(?:(?:[._]|__|-+|/)[a-z0-9]+)*$`\n",
	})
	canon, copies, scanned := scanNameFormDecls(t, newSyntheticTree(t, clean))
	if len(copies) != 0 {
		t.Fatalf("регулярки другого референта находками НЕ являются: получено %d (%v)", len(copies), copies)
	}
	if len(canon) != 1 {
		t.Fatalf("канон обязан находиться ровно один раз: получено %d (осмотрено %d)", len(canon), scanned)
	}
}

// TestInjection_SecondDerivationIsFound — второе производство умолчания
// находится; одно объявление молчит.
func TestInjection_SecondDerivationIsFound(t *testing.T) {
	root := nameFormSynthTree(t, map[string]string{
		"services/compute/internal/apps/naming.go": "package apps\n\n" +
			"func defaultNameForID(id string) string { return \"vm-\" + id }\n",
	})
	decls, scanned := scanDerivationDecls(t, newSyntheticTree(t, root))
	if got := len(decls[canonDerivationFunc]); got != 2 {
		t.Fatalf("второе производство умолчания обязано быть найдено: объявлений %d (осмотрено %d)",
			got, scanned)
	}

	// Сторона «должен молчать»: чистое дерево — ровно по одному объявлению.
	clean := nameFormSynthTree(t, nil)
	decls, _ = scanDerivationDecls(t, newSyntheticTree(t, clean))
	if got := len(decls[canonDerivationFunc]); got != 1 {
		t.Fatalf("на чистом дереве производство объявлено один раз, получено %d", got)
	}
	if got := len(decls[canonSubstitutionFunc]); got != 1 {
		t.Fatalf("на чистом дереве подстановка объявлена один раз, получено %d", got)
	}
}

// TestInjection_MissingCanonIsRefusalNotSilence — предпосылка гейта проверяется
// им самим: без канона он судить не берётся и говорит об этом, а не молчит.
// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
func TestInjection_MissingCanonIsRefusalNotSilence(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "pkg", "validate", "validate.go")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte("package validate\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := findCanonPattern(root); err == nil {
		t.Fatal("без константы канона гейт обязан ОТКАЗАТЬСЯ судить, а не выйти успехом: " +
			"молчаливый зелёный здесь означал бы «копий не найдено», хотя искать было не по чему")
	}

	// Сторона «должен молчать»: канон на месте — предпосылка выполнена.
	if _, err := findCanonPattern(nameFormSynthTree(t, nil)); err != nil {
		t.Fatalf("канон на месте, предпосылка обязана быть выполнена: %v", err)
	}
}
