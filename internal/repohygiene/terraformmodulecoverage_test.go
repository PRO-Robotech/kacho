// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// terraformmodulecoverage_test.go — гейт на класс: КАЖДЫЙ ресурс, зарегистрированный
// провайдером, описан хотя бы одним модулем `terraform/modules`.
//
// # Предмет
//
// Ресурс, которого нет ни в одном модуле, никем не СОСТАВЛЕН. У него не выражены ни ссылки
// на соседей, ни порядок уничтожения, и его не касается ни одна модульная проба (`tofu test`
// гоняется по модулям — см. `terraform/README.md`). Такой ресурс существует как схема и как
// код, но первым, кто его составит, будет пользователь — на своём ландшафте и без пробы.
// «Домен закрыт провайдером» при этом читается верно, потому что реестр полон: полнота
// реестра и составленность ресурса — РАЗНЫЕ свойства, и второе ничем не держалось.
//
// Соседний гейт (`terraformresourceparity_test.go`) сверяет контракты с реестром провайдера
// и о модулях не утверждает ничего. Предмет здесь — именно РАЗРЫВ между реестром и модулями,
// поэтому перепись печатает обе стороны: сколько ресурсов зарегистрировано и сколько из них
// описано. Число, а не слово «покрыто».
//
// # Чем этот гейт связан со своим соседом
//
// Перечень ресурсов берётся ТЕМ ЖЕ предикатом (`registeredTerraformResources`), что у соседа,
// и намеренно не переписывается: два обходчика одного предмета разошлись бы молча, и разошлись
// бы именно там, где расхождение не видно, — оба возвращают «полно» на полном дереве. Перечень
// файлов модулей берётся из ИНДЕКСА git (`trackedFilesUnder`), а не с диска: на диске рядом
// лежат установленные зависимости, сборки и рабочие каталоги инструментов, и обходчик диска
// судил бы о дереве, которого в репозитории нет.
//
// # Что гейт умеет уронить
//
//  1. зарегистрированный ресурс, которого нет ни в одном модуле и нет в таблице исключений;
//  2. исключение, чей ресурс ПОЯВИЛСЯ в модуле, — запись самоистекает, иначе она молча
//     прикроет следующий пропуск;
//  3. исключение, чьего ресурса нет в реестре, — исключению нечего исключать;
//  4. модуль, ссылающийся на тип `kacho_*`, которого провайдер не регистрирует, — такой план
//     не соберётся у пользователя, а у нас об этом не узнает никто до его первого `apply`.

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// tfModuleExempt — ресурсы, которым НЕ МЕСТО в модуле, и причина по существу.
//
// Здесь не тикет и не долг. Тикет означал бы «мы это ещё напишем» — такая запись живёт до
// бесконечности и превращает гейт в перечень намерений. Запись здесь означает другое:
// составлять этот ресурс модулем НЕЛЬЗЯ или БЕССМЫСЛЕННО по свойствам самого ресурса, и это
// свойство от нашей занятости не зависит.
//
// Запись САМОИСТЕКАЕТ в обе стороны: появился ресурс в модуле — запись становится находкой
// (значит причина была неверна, и надо править причину, а не молчать); исчез ресурс из
// реестра — запись становится находкой (исключению нечего исключать).
var tfModuleExempt = map[string]string{
	"kaname_account": "аккаунт — корень арендной иерархии, ВНУТРИ которого исполняется сам модуль. " +
		"Провайдер уже аутентифицирован в каком-то аккаунте, поэтому вызывающий не может модулем " +
		"завести тот аккаунт, в котором действуют его собственные учётные данные: составлять нечем и " +
		"не с чем. Аккаунт заводится один раз самим владельцем при первом входе (самообслуживание), " +
		"не параметризуется и не повторяется — то есть у него нет ни одного свойства, ради которого " +
		"заводят модуль.",

	"kaname_user_token": "токен — недолговечное удостоверение, а не часть ландшафта, и в модуле " +
		"он даёт две беды сразу. Значение уезжает в состояние КАЖДОГО, кто модуль применит, тогда как " +
		"модуль пишется ради повторного применения многими. И план перестаёт сходиться: срок токена " +
		"истекает сам собой, поэтому следующий `plan` всегда хочет пересоздать ресурс, которого никто " +
		"не менял. Выдача токена — действие, а не описание ландшафта; её место в вызывающем коде.",
}

func TestEveryProviderResourceIsDescribedByAModule(t *testing.T) {
	root := repoRoot(t)

	registered := registeredTerraformResources(t, root)
	if len(registered) == 0 {
		t.Fatal("зарегистрированных ресурсов провайдера не найдено — предикат переписи устарел " +
			"или реестр переехал; ноль здесь означает сломанный обходчик, а не чистое дерево")
	}

	described, modules, files, blocks := moduleDescribedResources(t, root)
	if len(described) == 0 {
		t.Fatal("в модулях не найдено ни одного блока resource — предикат разбора устарел или " +
			"каталог модулей переехал; ноль здесь означает сломанный обходчик, а не пустые модули")
	}

	// (1) зарегистрирован, но не составлен.
	var undescribed []string
	exempted := 0
	for name := range registered {
		if len(described[name]) > 0 {
			continue
		}
		if tfModuleExempt[name] != "" {
			exempted++
			continue
		}
		undescribed = append(undescribed, name)
	}
	sort.Strings(undescribed)
	for _, name := range undescribed {
		t.Errorf("ресурс провайдера %s не описан ни одним модулем terraform/modules: его ссылки, "+
			"порядок уничтожения и модульная проба не выражены нигде — заведите блок в подходящем "+
			"модуле либо внесите ресурс в tfModuleExempt с причиной по существу", name)
	}

	// (2)(3) исключения самоистекают в обе стороны.
	exemptNames := make([]string, 0, len(tfModuleExempt))
	for name := range tfModuleExempt {
		exemptNames = append(exemptNames, name)
	}
	sort.Strings(exemptNames)
	for _, name := range exemptNames {
		if mods := described[name]; len(mods) > 0 {
			t.Errorf("исключение пережило свой предмет: %s объявлен неподходящим для модуля, но "+
				"описан модулем(ями) %s — либо причина исключения неверна и её надо снять, либо "+
				"блок в модуле заведён ошибочно; молча совмещать эти два утверждения нельзя",
				name, strings.Join(mods, ", "))
		}
		if !registered[name] {
			t.Errorf("исключению нечего исключать: %s не зарегистрирован провайдером — запись "+
				"пережила свой ресурс и с этого дня прикрывает пустоту", name)
		}
	}

	// (4) обратная сверка: модуль ссылается на тип, которого провайдер не даёт.
	foreign := map[string]bool{}
	dangling := []string{}
	for name, mods := range described {
		if !strings.HasPrefix(name, "kacho_") {
			foreign[name] = true // ресурс чужого провайдера — не наш предмет, но виден в переписи
			continue
		}
		if !registered[name] {
			dangling = append(dangling, name+" (в модулях: "+strings.Join(mods, ", ")+")")
		}
	}
	sort.Strings(dangling)
	for _, d := range dangling {
		t.Errorf("модуль описывает тип %s, которого провайдер не регистрирует — у пользователя "+
			"такой план не соберётся вовсе; либо ресурс переименован, либо блок опечатан", d)
	}

	t.Logf("перепись: ресурсов провайдера %d, описано модулями %d, исключений %d",
		len(registered), len(registered)-len(undescribed)-exempted, len(tfModuleExempt))
	t.Logf("осмотрено: модулей %d, файлов .tf %d, блоков resource %d, типов чужих провайдеров %d",
		modules, files, blocks, len(foreign))
}

// moduleDescribedResources — типы ресурсов, ОПИСАННЫЕ модулями, и объём осмотренного.
//
// Возвращает тип → модули, которые его описывают (имена нужны в тексте находки: «не описан»
// без указания, где искали, читателю не помогает), число модулей, число прочитанных файлов и
// число найденных блоков. Три счётчика печатаются, чтобы «ноль находок» было отличимо от
// «ноль прочитанного» — гейт, обошедший пустой каталог, обязан выглядеть иначе, чем гейт,
// прочитавший дерево и ничего не нашедший.
func moduleDescribedResources(t *testing.T, root string) (described map[string][]string, modules, files, blocks int) {
	t.Helper()
	described = map[string][]string{}
	seenModule := map[string]bool{}
	for _, rel := range trackedFilesUnder(t, root, "terraform/modules", ".tf") {
		src, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		files++
		mod := moduleNameOf(rel)
		if !seenModule[mod] {
			seenModule[mod] = true
			modules++
		}
		for _, name := range resourceTypesInHCL(string(src)) {
			blocks++
			if !slices.Contains(described[name], mod) {
				described[name] = append(described[name], mod)
			}
		}
	}
	for name := range described {
		sort.Strings(described[name])
	}
	return described, modules, files, blocks
}

// moduleNameOf — имя модуля из пути `terraform/modules/<имя>/<файл>.tf`.
func moduleNameOf(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) >= 3 {
		return parts[2]
	}
	return rel
}

var (
	// Блочный комментарий HCL вырезается ЦЕЛИКОМ: закомментированный блок — не описание
	// ресурса, а проза о нём. Гейт, читающий сырой текст, засчитал бы объяснение вместо дела —
	// ровно тот класс, который мы ловим в коде.
	reHCLBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

	// Заголовок блока обязан стоять В НАЧАЛЕ строки (с точностью до отступа). Этим же якорем
	// отсекаются строчные комментарии обеих форм HCL — `# resource "…"` и `// resource "…"`:
	// в них слову `resource` предшествует знак комментария, значит якорь не сходится. Свойство
	// проверяется отдельной пробой ниже, а не подразумевается.
	reHCLResource = regexp.MustCompile(`(?m)^[ \t]*resource[ \t]+"([a-z0-9_]+)"`)
)

// resourceTypesInHCL — типы ресурсов, ОБЪЯВЛЕННЫЕ текстом HCL (не упомянутые в нём).
func resourceTypesInHCL(src string) []string {
	s := reHCLBlockComment.ReplaceAllString(src, "")
	var out []string
	for _, m := range reHCLResource.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestModuleResourceScanReadsDeclarationsNotProse — предпосылка гейта, проверенная в ОБЕ
// стороны на одном литерале.
//
// Отрицание («закомментированный блок не считается») в одиночку зеленеет и на разборе,
// который не находит вообще ничего, — поэтому рядом стоит положительный контроль: настоящий
// блок в том же тексте обязан быть найден. Литерал написан так, чтобы законный и незаконный
// близнецы отличались ровно одним признаком — знаком комментария.
func TestModuleResourceScanReadsDeclarationsNotProse(t *testing.T) {
	const src = `
resource "kacho_vpc_network" "this" {
  name = "netto"
}

  resource "kacho_vpc_subnet" "indented" {
  name = "subnetto"
}

# resource "kaname_account" "commented_hash" {}
// resource "kaname_role" "commented_slash" {}

/*
resource "kaname_user_token" "commented_block" {}
*/

# ниже в прозе названо слово resource "kacho_storage_volume" — это объяснение, а не блок
`

	got := resourceTypesInHCL(src)
	sort.Strings(got)
	want := []string{"kacho_vpc_network", "kacho_vpc_subnet"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("разбор HCL читает не то, что объявлено: получено %v, ожидалось %v", got, want)
	}
}
