// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Предпосылка приёмочных проб: ЧЕМ исполняется цикл terraform и под КАКИМ адресом
// провайдер попадает в сгенерированную настройку. Обе величины проба выясняет сама —
// оператор их не задаёт и не может забыть задать.
//
// # Почему исполнитель ищется здесь, а не оставляется библиотеке
//
// Не найдя исполнителя, terraform-plugin-testing идёт ставить его из сети сама. Замер
// 2026-08-14 на v1.15.0: попытка кончается отказом
//
//	failed to find or install Terraform CLI: unable to verify checksums signature:
//	openpgp: key expired
//
// То есть запасной путь библиотеки сегодня не работает вовсе, а его текст называет ни
// причину, ни средство: читатель уходит искать беду с ключом подписи вместо того, чтобы
// поставить исполнителя. Диагноз ставится по тексту отказа — значит текст обязан называть
// предмет. Плюс скачанный из сети terraform был бы ДРУГИМ инструментом другой версии, чем
// запинённый tofu конвейера, и «локально зелено» перестало бы что-либо говорить о нём.
//
// Порядок поиска — tofu, затем terraform: конвейер ставит OpenTofu, и локальный прогон
// обязан судить тем же инструментом. Заданный снаружи TF_ACC_TERRAFORM_PATH сильнее
// поиска: у оператора остаётся возможность прогнать чем-то третьим осознанно.
//
// # Почему адрес провайдера задаётся явно
//
// Без TF_ACC_PROVIDER_NAMESPACE библиотека ставит провайдера в устаревшее пространство
// имён «-», а OpenTofu его отвергает («legacy provider namespace "-" can be used only with
// hostname registry.opentofu.org») — цикл не начинается ни у одной пробы. Имя
// пространства здесь ОДНО с тем, что объявляют модули и примеры дерева; что они не
// разойдутся, держит TestAcceptanceProviderNamespaceMatchesTheTree ниже.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// accProviderNamespace — пространство имён провайдера в адресе source.
//
// Значение не выдумано: его объявляют required_providers модулей и примеров, и на него же
// нацелен dev_overrides конвейера. Расхождение ловит проба ниже.
const accProviderNamespace = "PRO-Robotech"

// accCLIPath — исполнитель цикла terraform, найденный один раз на пакет.
// Пустая строка означает «не найден» и приводит к отказу ПРИЁМОЧНЫХ проб; юнитовые пробы
// этого пакета исполнителя не требуют и от его отсутствия не страдают.
var accCLIPath string

func TestMain(m *testing.M) {
	if err := os.Setenv("TF_ACC_PROVIDER_NAMESPACE", accProviderNamespace); err != nil {
		panic(err)
	}

	accCLIPath = os.Getenv("TF_ACC_TERRAFORM_PATH")
	if accCLIPath == "" {
		for _, exe := range []string{"tofu", "terraform"} {
			if p, err := exec.LookPath(exe); err == nil {
				accCLIPath = p
				break
			}
		}
		if accCLIPath != "" {
			if err := os.Setenv("TF_ACC_TERRAFORM_PATH", accCLIPath); err != nil {
				panic(err)
			}
		}
	}

	os.Exit(m.Run())
}

// accRequireCLI — отказ с названным предметом вместо чужого отказа про ключ подписи.
//
// Это текст, который читает оператор, поэтому он называет и инструмент, и команду. Пропуск
// здесь был бы хуже отказа: пропущенная проба печатает «ok» и от пройденной неотличима.
func accRequireCLI(t *testing.T) {
	t.Helper()

	// `-short` исключает пробы, которым нужен ВНЕШНИЙ исполнитель, — тем же правилом и
	// по той же причине, по какой он исключает интеграционные пробы с контейнерами.
	// Джоба go и `make test-unit` tofu не ставят и ставить не должны: инструмент нужен
	// одной группе проб, а платили бы за него все.
	//
	// Пропуск здесь НЕ вычитается из вердикта и ничего не маскирует: приёмка исполняется
	// целиком СВОИМ прогоном, где условие для неё создано, — джобой `terraform`
	// конвейера и группой `scripts/ci-local.sh terraform`. Обе гоняют её БЕЗ `-short`,
	// поэтому отсутствие исполнителя там остаётся отказом, а не пропуском.
	if testing.Short() {
		t.Skip("приёмка провайдера исполняет цикл terraform и под -short не гоняется; " +
			"её прогон — `scripts/ci-local.sh terraform` и джоба terraform конвейера")
	}

	if accCLIPath == "" {
		// Версия здесь НЕ называется числом намеренно: пин живёт в одном месте на
		// прогон (scripts/ci-local.sh) и в одном на конвейер, и за их расхождением
		// следит сторож пинов. Число, повторённое здесь, ему невидимо — и устарело бы
		// молча, продолжая звучать как указание.
		t.Fatal("исполнителя цикла terraform нет: в PATH не найдены ни tofu, ни terraform.\n" +
			"Конвейер судит OpenTofu запинённой версии — тем же обязан судить и локальный прогон:\n" +
			"    scripts/ci-local.sh terraform   # ставит tofu той же версии и гоняет всё\n" +
			"Готовый исполнитель указывается переменной TF_ACC_TERRAFORM_PATH.")
	}
}

// accSourceRe — объявление источника провайдера в настройке terraform.
// Захватывается пространство имён: адрес вида "<namespace>/kacho".
var accSourceRe = regexp.MustCompile(`source\s*=\s*"([^"/]+)/kacho"`)

// TestAcceptanceProviderNamespaceMatchesTheTree — константа выше и дерево называют ОДНО
// пространство имён.
//
// Без этой пробы у одного предмета было бы два места: адрес, под которым провайдер
// приезжает в пробу, и адрес, который объявляют модули с примерами. Разошлись бы они молча
// — приёмочные пробы остались бы зелёными, проверяя провайдера под адресом, которого никто
// не пишет.
//
// Ноль осмотренных объявлений — ОТКАЗ, а не успех: обходчик, которому нечего обходить,
// обязан быть отличим от обходчика, у которого всё сошлось. Объём осмотренного печатается.
//
// Единица счёта — ОТСЛЕЖИВАЕМЫЙ git-элемент, а не то, что лежит на диске: под корнем живут
// рабочие копии агентов и распаковки, которых в репозитории нет. Обход диска сделал бы
// вердикт свойством чужого рабочего каталога, а не коммита, — и в обе стороны: красным на
// файле, которого в репозитории нет, и молчанием в свежем клоне.
func TestAcceptanceProviderNamespaceMatchesTheTree(t *testing.T) {
	root := terraformTreeRoot(t)

	files, err := treecorpus.UnderWithSuffix(root, ".tf")
	if err != nil {
		t.Fatalf("перепись настроек terraform: %v", err)
	}

	seen := 0
	for _, path := range files {
		body, err := os.ReadFile(path) // #nosec G304 -- путь пришёл из индекса репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", path, err)
		}
		for _, m := range accSourceRe.FindAllStringSubmatch(string(body), -1) {
			seen++
			if m[1] != accProviderNamespace {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s: source объявляет пространство имён %q, оснастка проб — %q",
					rel, m[1], accProviderNamespace)
			}
		}
	}

	if seen == 0 {
		t.Fatalf("объявлений source провайдера не найдено (осмотрено файлов .tf: %d) — "+
			"предикат поиска устарел или каталог переехал", len(files))
	}
	t.Logf("осмотрено файлов .tf: %d, объявлений source: %d", len(files), seen)
}

// terraformTreeRoot — каталог terraform/ этого дерева.
//
// Пробы исполняются в своём пакете, поэтому путь строится от него, а не от текущего
// каталога вызывающего: `go test ./...` из корня и `go test .` из пакета обязаны
// осматривать одно дерево.
func terraformTreeRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("текущий каталог: %v", err)
	}
	// .../terraform/internal/provider → .../terraform
	root := filepath.Dir(filepath.Dir(wd))
	if filepath.Base(root) != "terraform" {
		t.Fatalf("каталог terraform/ не опознан от %s (получилось %s) — пакет переехал, "+
			"и предикат поиска обязан переехать с ним", wd, root)
	}
	if _, err := os.Stat(filepath.Join(root, "modules")); err != nil {
		t.Fatalf("в %s нет каталога modules: %v", root, err)
	}
	return root
}
