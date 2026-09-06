// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// readbudget_test.go — ГЕЙТ ДЕРЕВА: мутация, купившая читательский бюджет.
//
// Свойство требуется от ДЕРЕВА, а не от одного каталога. Прежде его держал один
// тест в каталоге vpc, наблюдавший дескрипторы, слинкованные в бинарь vpc; после
// #771 потолок допуска провязан у семи сервисов, а страж оставался один — то есть
// свойство держалось тем, у кого случайно оказался страж.
//
// Дескрипторы берутся ИЗ РЕЕСТРА, а не разбором текста контрактов: реестр — то
// самое, по чему grpc-go диспатчит вызовы. Наполняют его пустые импорты ниже, и
// это РУКОПИСНЫЙ список — поэтому анализатор сверяет увиденное с тем, что
// объявляет дерево, и отказывает на пакете, которого не увидел.
package repohygiene

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	// Пустые импорты наполняют реестр дескрипторов. Список рукописный по
	// построению (импорт — литерал), поэтому его полноту проверяет НЕ он сам, а
	// сверка с деревом внутри анализатора: новый домен без строки здесь роняет
	// гейт с именем пакета, а не проходит молча.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/api"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/quota/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/reference"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/iam/authz/v1"
)

// operationEnvelope — конверт асинхронной мутации.
//
// Имя выписано, и это осознанно: оно проверено обходом дерева, а не взято по
// памяти. В первой редакции стража vpc здесь стояло `kacho.cloud.operation.v1.Operation`
// — лишний `v1`, — и дискриминатор не находил НИ ОДНОЙ мутации, то есть гейт был
// зелёным на всём. Отсюда премиса анализатора: «ни один метод не возвращает
// конверт» — отказ, а не чистота.
const operationEnvelope = "kacho.cloud.operation.Operation"

// exemptPackages — пакеты, для которых дискриминатор НЕСОСТОЯТЕЛЕН, с причиной.
//
// Ровно один, и он не «неудобный случай», а другой предмет: в
// `kacho.cloud.operation` `Operation` — САМ РЕСУРС, а не конверт чужой мутации.
// `OperationService/Get` возвращает его потому, что это чтение операции, которое
// клиент поллит до `done=true`; читательский бюджет он покупает ПО ПРАВУ, и
// покупать обязан — иначе поллинг завершения мутации оплачивался бы бюджетом
// мутации, то есть самый частый служебный вызов платформы душил бы сам себя.
//
// Запись самоистекает: если у пакета не останется метода, дающего находку,
// анализатор пометит её STALE-EXEMPTION.
var exemptPackages = map[string]string{
	"kacho.cloud.operation": "Operation здесь — сам ресурс, а не конверт чужой мутации: " +
		"OperationService/Get есть чтение операции (клиент поллит его до done=true), " +
		"и читательский бюджет ему полагается по праву",
}

func readBudgetOptions(t *testing.T) ReadBudgetOptions {
	t.Helper()
	declared, err := DeclaredProtoPackages(repoRoot(t))
	require.NoError(t, err)
	return ReadBudgetOptions{
		Files:            protoregistry.GlobalFiles,
		OperationMessage: operationEnvelope,
		DeclaredPackages: declared,
		Exempt:           exemptPackages,
		// Классификатор — ТОТ ЖЕ, что исполняется на листенере. Своя копия правила
		// разошлась бы с ним молча, и гейт стерёг бы не то, что работает.
		Classify: func(fullMethod string) CallClass {
			if grpcsrv.ClassifyByKachoConvention(fullMethod) == grpcsrv.ClassRead {
				return ClassRead
			}
			return ClassMutation
		},
	}
}

// TestNoMutationBuysTheReadBudget — ядро гейта, на НАСТОЯЩЕМ дереве.
func TestNoMutationBuysTheReadBudget(t *testing.T) {
	findings, census, err := AuditReadBudgetClassification(readBudgetOptions(t), os.Stdout)
	require.NoError(t, err)
	require.Empty(t, findings,
		"метод(ы) возвращают %s (то есть мутируют по правилу #9), но названы по-читательски "+
			"и потому купят ЧИТАТЕЛЬСКИЙ бюджет — впятеро более щедрый при втрое большей стоимости "+
			"запроса. Переименуйте по конвенции (мутация — не Get*/List*) либо объясните "+
			"классификатору эту форму явно: %v", operationEnvelope, findings)

	// Перепись — отдельное утверждение. Числа не пиннятся: они растут с каждым
	// новым контрактом, и пин превратил бы гейт свойства в гейт числа.
	require.Equal(t, census.PackagesDeclared, census.PackagesSeen,
		"увидено меньше пакетов, чем объявляет дерево")
	t.Logf("%s", census)
}

// TestReadBudgetGate_SeesTheRealTreeFormWhenNotExempt — ЗАКОННЫЙ БЛИЗНЕЦ ИЗ
// ДЕРЕВА, а не синтетика.
//
// Со снятым исключением анализатор обязан НАЙТИ `OperationService/Get` — это
// доказывает, что на настоящих дескрипторах он видит форму «возвращает конверт и
// назван по-читательски», а его молчание выше есть следствие ЗАПИСАННОГО решения,
// а не слепоты. Обратная сторона — сам гейт выше: с исключением он молчит.
func TestReadBudgetGate_SeesTheRealTreeFormWhenNotExempt(t *testing.T) {
	opts := readBudgetOptions(t)
	opts.Exempt = nil

	findings, _, err := AuditReadBudgetClassification(opts, io.Discard)
	require.NoError(t, err)
	require.Len(t, findings, 1,
		"со снятым исключением в дереве обязана найтись ровно одна форма — чтение самой операции")
	require.Equal(t, KindMutationBuysReadBudget, findings[0].Kind)
	require.Equal(t, "/kacho.cloud.operation.OperationService/Get", findings[0].Method)
}
