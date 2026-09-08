// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Общая оснастка приёмочных проб + пробы САМОГО поддельного края.
//
// # Почему resource.UnitTest, а не resource.Test
//
// terraform-plugin-testing пропускает приёмочные пробы, пока не задана переменная
// окружения TF_ACC. Предмет у этой защиты названный и узкий: «avoid test cases surprising
// a user by creating real resources» — она бережёт от НАСТОЯЩЕЙ инфраструктуры, за которую
// придёт счёт. Здесь настоящей инфраструктуры нет: провайдер разговаривает с httptest-
// сервером в том же процессе, а поддельный край живёт ровно столько, сколько тест.
// Исключение, которому нечего исключать, — находка, а не соглашение.
//
// Цена обратного решения измерима и хуже: пропущенная проба печатает «ok» и от пройденной
// неотличима. Пятнадцать приёмочных проб, тихо пропускаемых на каждой машине, где никто не
// выставил TF_ACC, — это ровно тот ложный зелёный, ради которого их и писали.
//
// resource.UnitTest выставляет TestCase.IsUnitTest, и это поле читается в библиотеке РОВНО
// в одном месте — на самом гейте TF_ACC (testing.go:970, замер на v1.16.0). Никакого
// другого поведения оно не меняет: пробы остаются полноценным циклом Terraform (init →
// plan → apply → повторный plan → refresh → destroy). Имя каждой начинается с
// TestAcceptance — отбор `-run Acceptance` берёт их и не берёт ничего лишнего.
//
// # Что для прогона всё-таки нужно
//
// Двоичный файл terraform. Он ищется в PATH, а если его там нет — скачивается
// terraform-plugin-testing самостоятельно; готовый указывается переменной
// TF_ACC_TERRAFORM_PATH. Переменных окружения СО СТЕНДОМ (KACHO_ENDPOINT, KACHO_TOKEN)
// пробы не требуют и не читают: адрес края и токен приходят блоком provider в самой
// настройке, и адрес — это адрес поддельного края.
//
// Если двоичного файла нет и скачать его неоткуда, библиотека роняет пробу с текстом
// «failed to find or install Terraform CLI». Это ОТКАЗ, а не пропуск, и так и задумано:
// «не выполнилось» не должно выглядеть как «прошло».

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfstate "github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// accProviderFactories — провайдер под пробой. Собирается в том же процессе, поэтому
// проверяется ТОТ ЖЕ код, который уезжает в релиз, а не его двойник.
//
// Здесь же — единственная дверь, через которую проходит КАЖДАЯ проба, исполняющая цикл
// terraform: поэтому предпосылка (исполнитель найден) проверяется здесь, а не восемнадцатью
// вызовами, один из которых однажды забудут. Юнитовые пробы этого пакета сюда не заходят и
// исполнителя не требуют.
//
// # Локальных имён столько же, сколько семейств имён типов
//
// Их ДВА, и оба обязательны. Библиотека строит `required_providers` из ключей
// этой карты, а исполнитель резолвит провайдера по приставке имени типа: `kacho_vpc_network`
// требует локального имени `kacho`, `kaname_role` — `kaname`. Карта с одним ключом делает
// пробы второго семейства неисполнимыми, и отказ приходит не от провайдера, а от
// разрешения зависимостей («required by this configuration but no version is selected») —
// то есть говорит не о том, что сломано.
func accProviderFactories(t *testing.T) map[string]func() (tfprotov6.ProviderServer, error) {
	t.Helper()
	accRequireCLI(t)
	factories := map[string]func() (tfprotov6.ProviderServer, error){}
	for _, localName := range accProviderLocalNames() {
		factories[localName] = providerserver.NewProtocol6WithError(New())
	}
	return factories
}

// accProviderLocalNames — локальные имена, под которыми провайдер приезжает в настройку.
//
// ВЫВОДЯТСЯ из имён типов, а не выписываются: семейство, заведённое завтра, попадёт сюда
// само, а рукописный перечень разошёлся бы с реестром молча — и разошёлся бы именно там,
// где расхождение не видно: на пробе, которая просто перестанет исполняться.
func accProviderLocalNames() []string {
	seen := map[string]bool{}
	var out []string
	p := New().(*kachoProvider)
	for _, ctor := range p.Resources(context.Background()) {
		addLocalNameOf(typeNameOfResource(ctor()), seen, &out)
	}
	for _, ctor := range p.DataSources(context.Background()) {
		addLocalNameOf(typeNameOfDataSource(ctor()), seen, &out)
	}
	sort.Strings(out)
	return out
}

// addLocalNameOf — приставка имени типа как локальное имя провайдера, без повторов.
func addLocalNameOf(typeName string, seen map[string]bool, out *[]string) {
	i := strings.Index(typeName, "_")
	if i <= 0 {
		return
	}
	name := typeName[:i]
	if seen[name] {
		return
	}
	seen[name] = true
	*out = append(*out, name)
}

// accProvider — блок настройки провайдера, нацеленный на поддельный край.
//
// Значения заданы ЯВНО, а не через окружение: провайдер читает окружение запасным путём,
// и проба, полагающаяся на него, зеленела бы или краснела в зависимости от того, что у
// запускающего в оболочке.
// Блок пишется на КАЖДОЕ локальное имя. Имя, объявленное без своего блока, получает
// ненастроенный провайдер, и план отказывает «requires explicit configuration» — отказ,
// который проба списала бы на ресурс.
func accProvider(e *fakeEdge) string {
	var b strings.Builder
	for _, localName := range accProviderLocalNames() {
		fmt.Fprintf(&b, `
provider %q {
  endpoint = %q
  token    = "acceptance-token"
}
`, localName, e.URL())
	}
	return b.String()
}

// ---- пробы самого поддельного края -------------------------------------------------------
//
// Гейт обязан нести проверку СВОЕЙ предпосылки. Пробы ниже утверждают, что каркас
// действительно воспроизводит три свойства края, ради которых он написан. Без них
// приёмочные пробы утверждали бы о провайдере, работающем против упрощённой подделки, и
// «зелено» ничего не значило бы.

// Отсутствие и отказ в доступе неразличимы ПОБАЙТОВО.
//
// Сравниваются два ответа про ОДИН И ТОТ ЖЕ идентификатор: сперва ресурс есть, но скрыт
// от чтения (отказ в доступе), затем его нет вовсе (настоящее отсутствие). Иначе
// сравнивать нечего — ответы про разные идентификаторы и должны различаться, потому что
// называют разные строки.
//
// Первая редакция этой пробы читала скрытый ресурс ДВАЖДЫ и сравнивала отказ с отказом.
// Она была зелёной и оставалась зелёной, когда поддельному краю вернули отдельный текст
// для отказа, — то есть не измеряла ничего. Нашлось это не чтением, а инъекцией дефекта;
// потому здесь и стоит явный порядок «отказ → отсутствие», а не два одинаковых чтения.
func TestAcceptanceFakeEdgeHidesAbsenceIdenticallyToDenial(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	c := mustProviderClient(t, e.URL())
	ctx := t.Context()

	id := accSeedNetwork(t, e, "prj-acc", "живая")
	read := func(what string) *client.Response {
		t.Helper()
		resp, err := c.Do(ctx, http.MethodGet, networksPath+"/"+id, nil, nil)
		if err != nil {
			t.Fatalf("чтение (%s): %v", what, err)
		}
		return resp
	}

	// Положительный контроль: пока сеть видна, чтение отдаёт её саму. Без него проба
	// зеленела бы и на крае, который отвечает «не найдено» вообще на всё.
	if alive := read("живая"); alive.StatusCode != http.StatusOK || !strings.Contains(string(alive.Body), id) {
		t.Fatalf("живая сеть не прочиталась: %d %s", alive.StatusCode, alive.Body)
	}

	e.HideFromRead(id)
	denied := read("отказ в доступе")

	// Тот же идентификатор, но теперь ресурса действительно нет.
	e.Reveal(id)
	e.Forget(id)
	absent := read("настоящее отсутствие")

	if string(denied.Body) != string(absent.Body) {
		t.Fatalf("отказ в доступе отличим от отсутствия:\n отказ:      %s\n отсутствие: %s",
			denied.Body, absent.Body)
	}
	if denied.StatusCode != http.StatusNotFound || absent.StatusCode != http.StatusNotFound {
		t.Fatalf("коды разошлись: отказ %d, отсутствие %d", denied.StatusCode, absent.StatusCode)
	}
}

// Тот же ключ повторной подачи — та же операция и НИ ОДНОГО нового объекта.
func TestAcceptanceFakeEdgeHonoursIdempotencyKey(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	c := mustProviderClient(t, e.URL())
	ctx := t.Context()

	body := []byte(`{"projectId":"prj-acc","name":"повтор"}`)
	first := accPostRaw(t, ctx, c, networksPath, body, "ключ-один")
	again := accPostRaw(t, ctx, c, networksPath, body, "ключ-один")
	if first != again {
		t.Fatalf("тот же ключ дал разные операции: %s и %s", first, again)
	}
	if n := e.CountOf(networksPath); n != 1 {
		t.Fatalf("объектов у края %d, ожидался 1: повтор с тем же ключом создал дубль", n)
	}

	// Зеркальная проба: ДРУГОЙ ключ обязан пойти обычным путём. Без неё «край всегда
	// отвечает первой операцией» выглядело бы соблюдением ключа.
	other := accPostRaw(t, ctx, c, networksPath, body, "ключ-два")
	if other == first {
		t.Fatalf("другой ключ дал ту же операцию %s — ключ не читается вовсе", other)
	}
	if n := e.CountOf(networksPath); n != 2 {
		t.Fatalf("объектов у края %d, ожидалось 2: другой ключ не создал нового", n)
	}

	// И третья сторона: запрос БЕЗ ключа тоже идёт обычным путём.
	bare := accPostRaw(t, ctx, c, networksPath, body, "")
	if bare == first || bare == other {
		t.Fatalf("запрос без ключа повторил чужую операцию %s", bare)
	}
}

// Операция завершается НЕ на первом опросе — и всё-таки завершается.
func TestAcceptanceFakeEdgeOperationIsNotDoneOnFirstPoll(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	c := mustProviderClient(t, e.URL())
	ctx := t.Context()

	opID := accPostRaw(t, ctx, c, networksPath, []byte(`{"projectId":"prj-acc","name":"ожидание"}`), "к")

	first, err := c.Do(ctx, http.MethodGet, "/operations/"+opID, nil, nil)
	if err != nil {
		t.Fatalf("первый опрос: %v", err)
	}
	var op struct {
		Done     bool           `json:"done"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(first.Body, &op); err != nil {
		t.Fatalf("разбор операции: %v", err)
	}
	if op.Done {
		t.Fatal("операция завершена на ПЕРВОМ опросе — провайдер, не умеющий ждать, прошёл бы пробу")
	}
	// Идентификатор в метаданных есть уже сейчас: так и у настоящего края, и именно
	// поэтому читать их вправе только тот, кто уже убедился в отсутствии отказа.
	if op.Metadata["networkId"] == nil {
		t.Fatal("метаданные незавершённой операции не несут идентификатора — свойство края не воспроизведено")
	}

	// Парный положительный: ожидание общим средством провайдера СХОДИТСЯ.
	done, err := c.AwaitOperation(ctx, opID, client.AwaitOptions{})
	if err != nil {
		t.Fatalf("ожидание операции: %v", err)
	}
	if id, ok := done.MetadataString("networkId"); !ok || id == "" {
		t.Fatalf("завершённая операция без идентификатора в метаданных: %+v", done.Metadata)
	}
}

// Незаданные поля приезжают нулями, 64-разрядные целые — строкой, 32-разрядные — числом.
func TestAcceptanceFakeEdgeSpeaksProtojson(t *testing.T) {
	e := newFakeEdge(t, edgeKindTargetGroup())
	c := mustProviderClient(t, e.URL())
	ctx := t.Context()

	id := accSeedTargetGroup(t, e, "prj-acc", "формы")
	resp, err := c.Do(ctx, http.MethodGet, targetGroupsPath+"/"+id, nil, nil)
	if err != nil {
		t.Fatalf("чтение группы целей: %v", err)
	}
	got := string(resp.Body)

	// 64-разрядное целое — СТРОКОЙ. Провайдер разбирает его через numOf; край, отдающий
	// число, оставил бы эту ветку непроверенной.
	if !strings.Contains(got, `"unhealthyThreshold":"2"`) {
		t.Errorf("64-разрядный порог пришёл не строкой: %s", got)
	}
	// 32-разрядное — ЧИСЛОМ. Парный контроль: без него «всё строкой» выглядело бы верным.
	if !strings.Contains(got, `"effectivePort":80`) {
		t.Errorf("32-разрядный порт пришёл не числом: %s", got)
	}
	// Незаданное поле — нулём, а не отсутствием.
	if !strings.Contains(got, `"description":""`) {
		t.Errorf("незаданное описание не пришло нулём: %s", got)
	}
}

// ---- вспомогательное для проб --------------------------------------------------------------

// accPostRaw отправляет готовое тело и возвращает идентификатор операции.
func accPostRaw(t *testing.T, ctx context.Context, c *client.Client, path string, body []byte, key string) string {
	t.Helper()
	hdr := &client.Headers{IdempotencyKey: key}
	resp, err := c.DoRaw(ctx, http.MethodPost, path, body, hdr)
	if err != nil {
		t.Fatalf("отправка %s: %v", path, err)
	}
	if out := client.Classify(resp); out.Kind != client.OutcomeOK {
		t.Fatalf("край отверг %s: %s", path, out.Message)
	}
	var op client.Operation
	if err := json.Unmarshal(resp.Body, &op); err != nil {
		t.Fatalf("разбор ответа %s: %v", path, err)
	}
	if op.ID == "" {
		t.Fatalf("ответ на %s без идентификатора операции: %s", path, resp.Body)
	}
	return op.ID
}

// accCaptureAttr запоминает значение атрибута из состояния — следующему шагу нужно знать
// идентификатор, чтобы заказать краю событие именно по нему.
func accCaptureAttr(addr, attr string, into *string) resource.TestCheckFunc {
	return func(s *tfstate.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("в состоянии нет %s", addr)
		}
		v, ok := rs.Primary.Attributes[attr]
		if !ok || v == "" {
			return fmt.Errorf("у %s нет значения атрибута %s", addr, attr)
		}
		*into = v
		return nil
	}
}

// accCheckAttrLate сверяет атрибут со значением, которое станет известно ПОЗЖЕ.
//
// resource.TestCheckResourceAttr берёт ожидаемое в момент СБОРКИ набора шагов, когда
// идентификатор ещё пуст, и сравнение выходит с пустой строкой. Ошибка тихая: проба
// краснеет по причине, не имеющей отношения к предмету, и первое объяснение («провайдер
// потерял идентификатор») оказывается неверным.
func accCheckAttrLate(addr, attr string, want *string) resource.TestCheckFunc {
	return func(s *tfstate.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("в состоянии нет %s", addr)
		}
		if *want == "" {
			return fmt.Errorf("ожидаемое значение %s.%s не было запомнено предыдущим шагом", addr, attr)
		}
		if got := rs.Primary.Attributes[attr]; got != *want {
			return fmt.Errorf("%s.%s = %q, ожидалось %q", addr, attr, got, *want)
		}
		return nil
	}
}

// accAbsentFromState — ресурса в состоянии больше нет.
//
// Утверждение обратное обычному, и оно осмысленно только рядом с положительным: проба,
// которая ждёт лишь исчезновения, зеленела бы и на провайдере, снимающем из состояния всё
// подряд. Положительный близнец — TestAcceptanceVPCNetwork_SingleNotFoundKeepsTheResource.
func accAbsentFromState(addr string) resource.TestCheckFunc {
	return func(s *tfstate.State) error {
		if _, ok := s.RootModule().Resources[addr]; ok {
			return fmt.Errorf("%s всё ещё в состоянии, хотя край подтвердил его отсутствие", addr)
		}
		return nil
	}
}

// accCheckImportable — реализует ли ресурс импорт.
//
// Утверждается ПАРОЙ: у одного вида импорт обязан быть, у другого его намеренно нет.
// Одностороннее «импорта нет» зеленело бы и на ресурсе, у которого его забыли.
func accCheckImportable(t *testing.T, name string, r fwresource.Resource, want bool) {
	t.Helper()
	_, got := r.(fwresource.ResourceWithImportState)
	if got != want {
		t.Errorf("%s: импорт реализован = %v, ожидалось %v", name, got, want)
	}
}
