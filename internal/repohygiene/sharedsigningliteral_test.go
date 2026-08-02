// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Общий симметричный подписной ключ не живёт в отслеживаемом дереве.
//
// # Предмет
//
// Симметричная подпись означает, что проверяющий и подписывающий держат ОДНО
// значение. Значение, лежащее в git публичного репозитория, держат все. Поэтому
// «ротация» такого ключа — театр: новое значение попадёт в тот же коммит и
// станет общим ровно так же. Единственный исход, меняющий что-либо, — чтобы
// значения в дереве не было вовсе, а оснастка получала подпись у выдающего
// (iam: MintBootstrapToken → SAKeyService.Issue → OAuth2 client_credentials).
//
// # Почему это гейт, а не разовая уборка
//
// Край уже отказывается стартовать, если общий ключ до него доезжает
// (`gateway/cmd/api-gateway/authz_validation.go`, «refuse to start»), и ни один
// профиль развёртывания его не рендерит. Оба факта — про ПРОДУКТ. Про ОСНАСТКУ
// не было ничего: харнесс продолжал бы чеканить симметричные Bearer'ы, край бы
// их отвергал, и отказ читался бы как дефект продукта. Один литерал, вернувшийся
// копипастой в восьмой файл, воспроизводит это целиком.
//
// # Что здесь считается деревом
//
// `git ls-files` — то же множество, что увидит свежий checkout и CI (авторитет
// объявлен в `trackedtree_test.go`). Обход диска читал бы рабочие копии агентов
// под `.claude/worktrees/`, где чужая ветка вполне может держать литерал, и
// вердикт перестал бы быть свойством коммита.
//
// # Единица счёта
//
// Находка — ФАЙЛ индекса, чей БАЙТОВЫЙ поток содержит литерал. Именно байты, а
// не строки: значение уже лежало в дереве и скомпилированным (.pyc-артефакт
// минтера), где текстовый греп по исходникам его не видит.

// sharedSymmetricSigningLiteral — значение, которое оснастка раздавала как общий
// ключ подписи HS256. Названо здесь ОДИН раз, потому что сам этот файл — то
// единственное место, где ему положено встречаться: гейту нужен предмет запрета,
// иначе он не может ни упасть, ни быть проверенным.
const sharedSymmetricSigningLiteral = "kacho-dev-jwt-secret-2026"

// sharedSigningLiteralCensusFloor — сколько файлов индекса гейт обязан прочитать,
// чтобы его «ноль находок» вообще что-то значил. Дерево на bdafe2c4 несёт свыше
// шести тысяч отслеживаемых файлов; порог намеренно занижен на порядок, чтобы
// он ловил обвал переписи (пустой индекс, обрезанный вывод, чужой каталог), а не
// нормальный рост или усадку дерева.
const sharedSigningLiteralCensusFloor = 2000

// scanForLiteral — предикат, вынесенный отдельно, чтобы инъекция могла подать
// синтетическое дерево. Возвращает отсортированный список путей и число реально
// прочитанных файлов: «ноль находок» обязано отличаться от «ноль прочитанного».
func scanForLiteral(t *testing.T, tt *trackedTree, needle string) (found []string, read int) {
	t.Helper()
	paths := make([]string, 0, tt.count())
	for rel := range tt.files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		b, err := os.ReadFile(filepath.Join(tt.root, filepath.FromSlash(rel)))
		if err != nil {
			// Файл индекса, которого нет на диске (частичный checkout,
			// симлинк наружу) — это не находка и не тишина: он просто не
			// прочитан, поэтому в перепись не идёт.
			continue
		}
		read++
		if strings.Contains(string(b), needle) {
			found = append(found, rel)
		}
	}
	return found, read
}

// sharedSigningLiteralGateFile — ЕДИНСТВЕННОЕ послабление, и оно неизбежно: гейту
// нужен предмет запрета целиком, иначе он не может ни упасть, ни быть проверенным
// инъекцией. Собирать литерал из половинок здесь было бы хуже — опечатка в половинке
// сделала бы гейт вечно-зелёным, и заметить это было бы нечем.
//
// Послабление несёт проверку СВОЕЙ предпосылки (ниже): файл обязан существовать и
// обязан содержать литерал. Переименуют гейт, вынесут константу, снимут запрет — и
// запись останется без предмета, а тест об этом скажет, вместо того чтобы молча
// прикрывать чужой файл.
const sharedSigningLiteralGateFile = "internal/repohygiene/sharedsigningliteral_test.go"

// TestNoSharedSymmetricSigningLiteralInTrackedTree — сам запрет.
func TestNoSharedSymmetricSigningLiteralInTrackedTree(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// Предпосылка послабления: у него обязан быть предмет.
	if !tt.hasFile(sharedSigningLiteralGateFile) {
		t.Fatalf("послабление указывает на %s, которого в индексе нет — "+
			"либо гейт переименован, либо ещё не добавлен; в обоих случаях "+
			"исключение прикрывает не то, что задумано", sharedSigningLiteralGateFile)
	}
	self, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sharedSigningLiteralGateFile)))
	if err != nil {
		t.Fatalf("read %s: %v", sharedSigningLiteralGateFile, err)
	}
	if !strings.Contains(string(self), sharedSymmetricSigningLiteral) {
		t.Fatalf("послаблению больше нечего исключать: %s не содержит литерала. "+
			"Значит либо запрет обессмыслен, либо предмет назван иначе — "+
			"запись, которой нечего исключать, это находка, а не порядок",
			sharedSigningLiteralGateFile)
	}

	found, read := scanForLiteral(t, tt, sharedSymmetricSigningLiteral)
	found = withoutGateFile(found)

	if read < sharedSigningLiteralCensusFloor {
		t.Fatalf("перепись обвалилась: прочитано %d файлов индекса при пороге %d — "+
			"вердикт «ноль находок» на таком объёме означал бы «ноль прочитанного», "+
			"а не «чисто»", read, sharedSigningLiteralCensusFloor)
	}
	t.Logf("прочитано файлов индекса: %d", read)

	if len(found) > 0 {
		t.Fatalf("общий симметричный подписной ключ лежит в отслеживаемом дереве (%d файл(ов)):\n  %s\n\n"+
			"Он опубликован вместе с репозиторием, поэтому смена значения ничего не даёт — "+
			"новое станет общим тем же коммитом. Оснастка, обращающаяся к ПОДНЯТОМУ стенду, "+
			"обязана брать подпись у выдающего (tests/authz-fixtures/prodseed_all.py: "+
			"MintBootstrapToken → SAKeyService.Issue → OAuth2 client_credentials). "+
			"Внутрипроцессные фикстуры под этот запрет не подпадают — они не ходят на стенд "+
			"и держат СВОЁ значение в своём же тесте.",
			len(found), strings.Join(found, "\n  "))
	}
}

// TestSharedSigningLiteralGateInjection — предикат обязан краснеть на внесённом
// дефекте и МОЛЧАТЬ на законной конструкции той же формы.
//
// Отрицание в паре с положительным: без второй половины гейт, отвечающий «нет
// находок» на пустом или нечитаемом дереве, выглядел бы точно так же, как гейт,
// действительно осмотревший дерево.
func TestSharedSigningLiteralGateInjection(t *testing.T) {
	dir := t.TempDir()

	// (а) НАСТОЯЩИЙ дефект: тот самый литерал в оснастке, в форме, в которой он
	// и лежал — значение по умолчанию у переменной окружения.
	mustWrite(t, filepath.Join(dir, "seed.sh"),
		"DEV_SECRET=\"${DEV_SECRET:-"+sharedSymmetricSigningLiteral+"}\"\n")

	// (б) ЗАКОННЫЙ БЛИЗНЕЦ той же формы: имя ручки названо, значения нет —
	// именно так о ней говорят гейт продукта и профиль развёртывания. Гейт,
	// срабатывающий и здесь, был бы снят первым же ложным срабатыванием.
	mustWrite(t, filepath.Join(dir, "values.prod.yaml"),
		"# KACHO_API_GATEWAY_AUTHN_DEV_SECRET must be empty on every deployed stand\n"+
			"authn:\n  devSecret: \"\"\n")

	// (в) ВТОРОЙ законный близнец: асимметричная выдача, ради которой запрет и
	// существует. Слова те же, предмет другой.
	mustWrite(t, filepath.Join(dir, "prodseed.py"),
		"# tokens come from iam SAKeyService.Issue; nothing is signed with a shared secret here\n")

	tt := newSyntheticTree(t, dir)
	found, read := scanForLiteral(t, tt, sharedSymmetricSigningLiteral)

	if read != 3 {
		t.Fatalf("инъекция прочитала %d файлов вместо 3 — проверяется не то дерево", read)
	}
	if len(found) != 1 || found[0] != "seed.sh" {
		t.Fatalf("предикат обязан назвать РОВНО внесённый дефект (seed.sh), получено: %v.\n"+
			"Пусто — гейт не падает на настоящем дефекте; больше одного — падает на "+
			"законной форме и будет отключён первым же ложным срабатыванием", found)
	}
}

// withoutGateFile снимает ровно одну запись — файл самого гейта. Отдельной функцией,
// потому что список исключений из одной записи слишком легко превращается в список из
// восьми: чтобы добавить сюда вторую координату, придётся переписать эту функцию и
// объяснить, почему у второй тоже нет другого места.
func withoutGateFile(found []string) []string {
	out := found[:0]
	for _, rel := range found {
		if rel == sharedSigningLiteralGateFile {
			continue
		}
		out = append(out, rel)
	}
	return out
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
