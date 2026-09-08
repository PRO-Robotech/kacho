// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Переезд типов ресурсов iam под именем Kaname: что происходит с СОСТОЯНИЕМ арендатора.
//
// # Почему это отдельная оснастка, а не ещё один resource.UnitTest
//
// Имя типа — внешне адресуемая координата записи в состоянии оператора (ban #15).
// Проверить, что состояние переезд пережило, можно только имея состояние, записанное под
// ПРЕЖНИМ именем, — а прежнего имени провайдер больше не объявляет. terraform-plugin-testing
// строит состояние исключительно применением конфигурации, поэтому состояния с чужим типом
// у неё не получить ни одним шагом: она умеет только те типы, которые провайдер объявляет
// сейчас.
//
// Поэтому цикл здесь гоняется напрямую: временный каталог, свой файл состояния, свой
// исполнитель. Провайдер при этом ТОТ ЖЕ — он собирается из этого дерева и подставляется
// через dev_overrides, ровно как в джобе конвейера. Подделки нет ни с одной стороны:
// край поддельный (тот же fakeEdge, что у остальных приёмочных проб), а провайдер и
// исполнитель настоящие.
//
// # Почему состояние получается ПЕРЕИМЕНОВАНИЕМ, а не пишется руками
//
// Рукописный файл состояния сверял бы провайдера с моим представлением о его схеме, а не
// с ним самим: добавится вычисляемое поле — рукопись отстанет молча и проба начнёт
// проверять устаревшую форму. Поэтому состояние СОЗДАЁТСЯ применением под новым именем, а
// затем в нём меняется РОВНО ОДНО поле — имя типа. Всё остальное написано самим
// провайдером, то есть в точности то, что лежало бы у оператора до перехода.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---- оснастка прямого цикла --------------------------------------------------------------

// accPluginDir — каталог с собранным провайдером, один на прогон пакета. Пустая строка
// означает «не собирали»; удаляется в TestMain после прогона.
var (
	accPluginDir  string
	accPluginErr  error
	accPluginOnce sync.Once
)

// accTreeRoot — корень дерева продукта относительно каталога пакета.
const accTreeRoot = "../../.."

// accProviderPluginDir собирает провайдер ИЗ ЭТОГО ДЕРЕВА и возвращает каталог, на который
// нацеливается dev_overrides.
//
// Сборка одна на прогон пакета: она стоит секунды, а проб, которым она нужна, несколько.
// Отказ сборки — отказ пробы, а не пропуск: «не выполнилось» не должно выглядеть как
// «прошло».
func accProviderPluginDir(t *testing.T) string {
	t.Helper()
	accPluginOnce.Do(func() {
		dir, err := os.MkdirTemp("", "kacho-provider-plugin-")
		if err != nil {
			accPluginErr = err
			return
		}
		accPluginDir = dir
		// Корень дерева назван относительным путём — так же, как его называет соседняя
		// проба страницы провайдера: у пакета нет другой опоры, а рабочий каталог
		// прогона всегда каталог пакета.
		build := exec.Command("go", "build",
			"-o", filepath.Join(dir, "terraform-provider-kacho"),
			"./terraform/cmd/terraform-provider-kacho")
		build.Dir = accTreeRoot
		out, err := build.CombinedOutput()
		if err != nil {
			accPluginErr = fmt.Errorf("сборка провайдера: %w\n%s", err, out)
		}
	})
	if accPluginErr != nil {
		t.Fatalf("провайдер для прямого цикла не собран: %v", accPluginErr)
	}
	return accPluginDir
}

// accTofuWorkdir — временный каталог с настройкой CLI, нацеленной на собранный провайдер.
//
// `init` не зовётся и не может быть позван: под dev_overrides он честно не находит
// провайдера в реестре и выходит отказом. Глушить этот код возврата значило бы не
// отличать настройку от сбоя, поэтому шаги цикла зовутся напрямую — так же, как это
// делает джоба конвейера с модулями и примерами.
func accTofuWorkdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rc := filepath.Join(dir, "tofu.tfrc")
	// Адрес подмены — ТОТ ЖЕ, что провайдер сверяет у источника переезда
	// (iam_type_names.go). Вписанный здесь вторым литералом, он разошёлся бы с ним молча
	// — и проба продолжала бы проверять переезд по адресу, которого код не признаёт.
	body := fmt.Sprintf(`provider_installation {
  dev_overrides { %q = %q }
  direct {}
}
`, providerSourceAddress, accProviderPluginDir(t))
	if err := os.WriteFile(rc, []byte(body), 0o600); err != nil {
		t.Fatalf("настройка CLI не записана: %v", err)
	}
	return dir
}

// accWriteConfig кладёт настройку в рабочий каталог, затирая прежнюю.
func accWriteConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(body), 0o600); err != nil {
		t.Fatalf("настройка не записана: %v", err)
	}
}

// accTofu исполняет одну команду цикла и возвращает вывод и код возврата.
//
// Код возврата возвращается ЗНАЧЕНИЕМ, а не превращается в отказ пробы: у `plan
// -detailed-exitcode` их три (0 — изменений нет, 2 — есть, 1 — отказ), и каждый из трёх
// в своём сценарии законен. Проба, роняющая себя на любом ненулевом, не смогла бы
// утверждать ни про пустой план, ни про отказ бездействующего оператора.
func accTofu(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(accCLIPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"TF_CLI_CONFIG_FILE="+filepath.Join(dir, "tofu.tfrc"),
		"TF_IN_AUTOMATION=1",
		"CHECKPOINT_DISABLE=1",
		// Наследованная привязка к чужому серверу провайдера увела бы цикл к другому
		// провайдеру, чем собранный: пробы соседних пакетов её выставляют.
		"TF_REATTACH_PROVIDERS=",
	)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("исполнитель цикла не запустился (%s): %v\n%s", accCLIPath, err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// accPlanCounts — то, что план обещает сделать.
type accPlanCounts struct {
	Add    int
	Change int
	Remove int
}

// accPlanSummary читает сводку плана из машинного вывода.
//
// Читается ИМЕННО машинный вывод, а не человеческая строка «Plan: … to add»: её формат
// принадлежит исполнителю и меняется без предупреждения, а поле `changes` объявлено
// контрактом вывода. Отсутствие сводки — отдельное состояние: план, не дошедший до неё,
// не «нулевой», он не построен.
func accPlanSummary(t *testing.T, out string) (accPlanCounts, bool) {
	t.Helper()
	var found bool
	var counts accPlanCounts
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var msg struct {
			Type    string `json:"type"`
			Changes struct {
				Add    int `json:"add"`
				Change int `json:"change"`
				Remove int `json:"remove"`
			} `json:"changes"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Type != "change_summary" {
			continue
		}
		counts = accPlanCounts{Add: msg.Changes.Add, Change: msg.Changes.Change, Remove: msg.Changes.Remove}
		found = true
	}
	return counts, found
}

// accStateTypeAndID — тип и идентификатор единственной записи состояния.
//
// Записей ожидается ровно одна: проба ведёт один ресурс, и вторая запись означала бы, что
// цикл завёл что-то сверх названного, — то есть ровно то пересоздание, которое проба и
// ловит.
func accStateTypeAndID(t *testing.T, dir string) (string, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "terraform.tfstate"))
	if err != nil {
		t.Fatalf("состояние не прочитано: %v", err)
	}
	var st struct {
		Resources []struct {
			Type      string `json:"type"`
			Instances []struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("состояние не разобрано: %v", err)
	}
	if len(st.Resources) != 1 || len(st.Resources[0].Instances) != 1 {
		t.Fatalf("в состоянии ожидалась одна запись с одним экземпляром, найдено записей %d",
			len(st.Resources))
	}
	id, _ := st.Resources[0].Instances[0].Attributes["id"].(string)
	if id == "" {
		t.Fatalf("в состоянии нет идентификатора: %s", raw)
	}
	return st.Resources[0].Type, id
}

// accRewriteStateType меняет в состоянии РОВНО имя типа — и ничего больше.
//
// Так получается состояние, записанное прежней сборкой провайдера: содержимое написано
// им самим, отличается только координата записи.
func accRewriteStateType(t *testing.T, dir, from, to string) {
	t.Helper()
	path := filepath.Join(dir, "terraform.tfstate")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("состояние не прочитано: %v", err)
	}
	var st map[string]any
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("состояние не разобрано: %v", err)
	}
	resources, _ := st["resources"].([]any)
	renamed := 0
	for _, r := range resources {
		row, _ := r.(map[string]any)
		if row == nil {
			continue
		}
		if row["type"] == from {
			row["type"] = to
			renamed++
		}
	}
	if renamed != 1 {
		t.Fatalf("в состоянии переименовано записей %d, ожидалась одна: тип %q не найден", renamed, from)
	}
	// Порядковый номер состояния растёт вместе с содержимым: исполнитель сверяет его с
	// резервной копией и на отставшем номере говорит о рассогласовании.
	if serial, ok := st["serial"].(float64); ok {
		st["serial"] = serial + 1
	}
	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatalf("состояние не собрано: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("состояние не записано: %v", err)
	}
	// Резервная копия остаётся от прежнего применения и несёт ПРЕЖНИЙ тип. Оставленная,
	// она сделала бы вердикт зависящим от того, читает ли её исполнитель.
	_ = os.Remove(path + ".backup")
}

// accMoveWorkdir — общий пролог трёх сценариев переезда.
//
// Возвращает рабочий каталог, где состояние записано под ПРЕЖНИМ именем типа, и
// идентификатор, зафиксированный ДО перехода. Настройку каждый сценарий пишет свою — в
// ней и состоит его единственное отличие от соседа.
func accMoveWorkdir(t *testing.T, e *fakeEdge) (string, string) {
	t.Helper()
	dir := accTofuWorkdir(t)

	// Шаг 1 — состояние, какое было бы у оператора до перехода. Пишется применением, а не
	// рукописью: содержимое обязано быть тем, что пишет сам провайдер.
	accWriteConfig(t, dir, accMoveConfig(e, accMoveTargetType, "", ""))
	out, code := accTofu(t, dir, "apply", "-auto-approve", "-no-color")
	if code != 0 {
		t.Fatalf("положительный контроль не прошёл: ресурс под новым именем не создался "+
			"(код %d)\n%s", code, out)
	}
	gotType, id := accStateTypeAndID(t, dir)
	if gotType != accMoveTargetType {
		t.Fatalf("состояние записано под типом %q, ожидался %q", gotType, accMoveTargetType)
	}

	accRewriteStateType(t, dir, accMoveTargetType, accMoveSourceType)
	return dir, id
}

// Имена типов названы ЗДЕСЬ константами, а не вписаны в каждую настройку: сценариев три, и
// разойтись между собой они не должны — отличие между ними обязано быть одним названным
// фактом, а не опечаткой.
const accMoveTargetType = typeNameIAMAccount

// accMoveSourceType — прежнее имя того же типа, взятое ИЗ СЛОВАРЯ снятых имён.
//
// Вписанное литералом, оно было бы вторым местом об одном предмете: словарь и проба
// разошлись бы молча, и проба продолжала бы проверять переезд с имени, которого никто
// не объявлял снятым.
var accMoveSourceType = retiredResourceTypeNames[accMoveTargetType]

// accMoveConfig — настройка оператора.
//
// `moved` пишется, когда движение объявлено; пустая строка означает «оператор его не
// объявил» — это и есть половинное действие.
func accMoveConfig(e *fakeEdge, resourceType, movedFrom, movedTo string) string {
	localName := resourceType[:strings.Index(resourceType, "_")]
	moved := ""
	if movedFrom != "" {
		moved = fmt.Sprintf("\nmoved {\n  from = %s.t\n  to   = %s.t\n}\n", movedFrom, movedTo)
	}
	return fmt.Sprintf(`terraform {
  required_providers {
    %[1]s = {
      source = "PRO-Robotech/kacho"
    }
  }
}

provider %[1]q {
  endpoint = %[2]q
  token    = "acceptance-token"
}

resource %[3]q "t" {
  name = "переезд"
}
%[4]s`, localName, e.URL(), resourceType, moved)
}

// ---- KAN-W6-01 ---------------------------------------------------------------------------

// Состояние арендатора пережило переход: план не создаёт и не удаляет, идентификатор тот же,
// повторный план пуст.
//
// Три утверждения, и ни одно не лишнее. Нулевой план достижим и ПЕРЕСОЗДАНИЕМ — если
// удаление и создание сойдутся в одном шаге, счётчики скажут «1 и 1», а не «0 и 0»;
// поэтому проверяются оба счётчика. Совпадение идентификатора отделяет смену АДРЕСА записи
// от смены самого ресурса: без него «нулевой план» остаётся утверждением о плане, а не о
// ресурсе арендатора. Пустой повторный план отделяет сошедшийся переезд от колеблющегося.
func TestAcceptanceIAMTypeMove_TenantStateSurvivedTheRename(t *testing.T) {
	accRequireCLI(t)
	e := newFakeEdge(t, edgeKindAccount())
	dir, idBefore := accMoveWorkdir(t, e)

	accWriteConfig(t, dir, accMoveConfig(e, accMoveTargetType, accMoveSourceType, accMoveTargetType))

	out, code := accTofu(t, dir, "plan", "-no-color", "-json", "-detailed-exitcode")
	if code == 1 {
		t.Fatalf("план не построен: переезд объявлен, а исполнитель отказал\n%s", out)
	}
	counts, ok := accPlanSummary(t, out)
	if !ok {
		// Сводки нет ровно тогда, когда изменений нет: это и есть искомый исход,
		// и отдельным состоянием он назван, чтобы «нулевой план» не подменялся
		// «планом, который не построился».
		if code != 0 {
			t.Fatalf("сводки плана нет, а код возврата %d — план не построен\n%s", code, out)
		}
		counts = accPlanCounts{}
	}
	if counts.Add != 0 || counts.Remove != 0 {
		t.Errorf("план после объявленного переезда: к созданию %d, к изменению %d, к удалению %d;"+
			" ожидались нули к созданию и к удалению.\nПереименование без переезда состояния "+
			"означает для арендатора удаление живой инфраструктуры, а не смену адреса записи.\n%s",
			counts.Add, counts.Change, counts.Remove, out)
	}

	applyOut, applyCode := accTofu(t, dir, "apply", "-auto-approve", "-no-color")
	if applyCode != 0 {
		t.Fatalf("переезд не применён (код %d)\n%s", applyCode, applyOut)
	}

	gotType, idAfter := accStateTypeAndID(t, dir)
	if gotType != accMoveTargetType {
		t.Errorf("после перехода запись состояния лежит под типом %q, ожидался %q", gotType, accMoveTargetType)
	}
	if idAfter != idBefore {
		t.Errorf("идентификатор в состоянии сменился: было %q, стало %q.\n"+
			"Переезд обязан менять АДРЕС записи, а не сам ресурс: смена идентификатора "+
			"означает, что прежний ресурс удалён, а на его месте заведён новый.",
			idBefore, idAfter)
	}

	againOut, againCode := accTofu(t, dir, "plan", "-no-color", "-detailed-exitcode")
	if againCode != 0 {
		t.Errorf("повторный план не пуст (код %d): переезд не сошёлся, а колеблется\n%s",
			againCode, againOut)
	}
}

// ---- KAN-W6-02 ---------------------------------------------------------------------------

// Бездействующий оператор получает ОТКАЗ плана, а не тихое пересоздание.
//
// Отличие от сценария выше — один факт: настройка осталась на прежнем имени типа. Громкий
// отказ здесь требуемый исход: бездействие обязано быть безопасным.
func TestAcceptanceIAMTypeMove_UntouchedConfigurationFailsThePlan(t *testing.T) {
	accRequireCLI(t)
	e := newFakeEdge(t, edgeKindAccount())
	dir, _ := accMoveWorkdir(t, e)

	accWriteConfig(t, dir, accMoveConfig(e, accMoveSourceType, "", ""))

	out, code := accTofu(t, dir, "plan", "-no-color", "-detailed-exitcode")
	if code != 1 {
		t.Fatalf("план на непереведённой настройке обязан ОТКАЗАТЬ (код 1), получен код %d.\n"+
			"Молчаливый план здесь означал бы, что прежнее имя типа всё ещё обслуживается.\n%s",
			code, out)
	}
	if !strings.Contains(out, accMoveSourceType) {
		t.Errorf("отказ не называет неизвестный тип %q — читателю не с чего начать\n%s",
			accMoveSourceType, out)
	}
}

// ---- KAN-W6-03 ---------------------------------------------------------------------------

// Половинное действие оператора: тип в настройке переписан, переезд не объявлен.
//
// Отличие от KAN-W6-01 — один факт: объявления переезда нет. Сценарий существует затем,
// чтобы нулевой план KAN-W6-01 нельзя было сдать, не отличив его от исхода БЕЗ переезда:
// без этого контраста «0 к созданию и 0 к удалению» могло бы получаться и само собой.
//
// # Приёмка ждала пересоздания — исполнитель делает ДРУГОЕ, и это замер, а не мнение
//
// Приёмка (KAN-W6-03, вслед за родителем WIRE-6-03) описывает исход как «план показывает
// удаление прежних записей и создание новых». Обе называют этот исход «свойством
// инструмента» и обе оговаривают, что ни одна проба дерева его не прогоняла. Прогон на
// OpenTofu 1.12.5 даёт другое:
//
//	Plan: 1 to add, 0 to change, 0 to destroy.
//	Error: no schema available for kacho_iam_account.t while reading state;
//	       this is a bug in OpenTofu and should be reported
//
// То есть удаление НЕ планируется вовсе: исполнитель не может прочитать запись состояния,
// чей тип провайдер больше не объявляет, и останавливается. Пересоздание было бы верным
// описанием, если бы провайдер продолжал обслуживать прежний тип, — а он его снял.
//
// Для арендатора это ЛУЧШЕ обещанного: половинное действие ничего не разрушает. Но
// описание перехода обязано называть тот исход, в который оператор попадёт на самом деле,
// поэтому проба утверждает измеренное, а не ожидавшееся: план отказывает, называет
// осиротевший адрес и НЕ планирует удаления.
func TestAcceptanceIAMTypeMove_WithoutTheMovedBlockThePlanStops(t *testing.T) {
	accRequireCLI(t)
	e := newFakeEdge(t, edgeKindAccount())
	dir, _ := accMoveWorkdir(t, e)

	accWriteConfig(t, dir, accMoveConfig(e, accMoveTargetType, "", ""))

	out, code := accTofu(t, dir, "plan", "-no-color", "-json", "-detailed-exitcode")
	if code != 1 {
		t.Fatalf("без объявления переезда план обязан ОТКАЗАТЬ (код 1), получен код %d.\n"+
			"Если он строится — значит исход половинного действия сменился, и нулевой план "+
			"KAN-W6-01 больше не отличается от него ничем.\n%s", code, out)
	}

	orphan := accMoveSourceType + ".t"
	if !strings.Contains(out, orphan) {
		t.Errorf("отказ не называет осиротевшую запись %q — оператору не с чего начать\n%s",
			orphan, out)
	}

	// Удаления быть не должно: половинное действие обязано остановиться, а не снести
	// живую запись. Сводка плана до отказа доходит, поэтому утверждать есть о чём.
	counts, ok := accPlanSummary(t, out)
	if !ok {
		t.Fatalf("сводки плана нет — утверждать про отсутствие удаления нечего\n%s", out)
	}
	if counts.Remove != 0 {
		t.Errorf("половинное действие спланировало удаление записей: %d. "+
			"Прежняя запись состояния обязана пережить отказ, а не быть снесённой.\n%s",
			counts.Remove, out)
	}
}
