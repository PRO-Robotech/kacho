// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"

	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// repoSecurityGroupReader — SecurityGroupReader, выведенный из уже ОБЯЗАТЕЛЬНОГО
// `Repo`. Существует ради одного свойства: у проверки «цель правила лежит в моей
// сети» не должно быть состояния «порт не передан», потому что такое состояние
// значит «разрешено всё» и от «настроена и разрешила» не отличимо — ответ
// одинаковый.
//
// Порт не несёт НИ ОДНОГО факта сверх `Repo`: боевая провязка передаёт
// `cqrsadapter.NewSecurityGroup(kachoRepo)`, то есть адаптер над тем же
// `Repo`, — именно поэтому его и можно было забыть, ничего не сломав на вид.
// Вывод порта из `Repo` делает «забыть» невыразимым.
type repoSecurityGroupReader struct{ repo Repo }

// Get — чтение через свежую Reader-TX (та же форма, что у остальных read-путей
// пакета: открыть → прочитать → закрыть).
func (r repoSecurityGroupReader) Get(ctx context.Context, id string) (*kachorepo.SecurityGroupRecord, error) {
	rd, err := r.repo.Reader(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rd.Close() }()
	return rd.SecurityGroups().Get(ctx, id)
}

// GetMany — резолв набора id в ОДНОЙ Reader-TX и одним запросом: длину набора
// выбирает вызывающий, поэтому резолв в цикле означал бы обращение к БД на
// каждый присланный элемент.
func (r repoSecurityGroupReader) GetMany(ctx context.Context, ids []string) (map[string]*kachorepo.SecurityGroupRecord, error) {
	rd, err := r.repo.Reader(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rd.Close() }()
	return rd.SecurityGroups().GetMany(ctx, ids)
}

// sgTargetReader — порт, которым use-case резолвит цели правил. Результат
// НИКОГДА не nil: `override` — это уточнение источника чтения (composition-root
// передаёт свой адаптер), а его отсутствие означает «читать через уже
// обязательный repo», а НЕ «проверку не делать». Состояния «проверка выключена»
// у пакета больше нет, и вернуть его нельзя незаметно — гейт
// `TestSGTargetReader_NoNilComparisonRemainsInProductionCode` роняет прогон на
// любом сравнении порта с nil.
//
// Параметр назван `override`, а не `reader`/`sgReader`, намеренно: имена портов
// стоят под надзором того гейта, и сравнение здесь — выбор источника, а не
// пропуск проверки. Разные предметы обязаны и называться по-разному.
func sgTargetReader(override SecurityGroupReader, r Repo) SecurityGroupReader {
	if override != nil {
		return override
	}
	return repoSecurityGroupReader{repo: r}
}

// repoCidrGroupReader — читатель именованных наборов, ВЫВЕДЕННЫЙ из уже
// обязательного `Repo`, по той же причине, что и его сосед выше: у проверки
// ссылки правила на набор не должно быть состояния «порт не передан», потому что
// такое состояние значит «разрешено всё» и от «настроена и разрешила» неотличимо.
//
// Он обслуживает СИНХРОННЫЙ путь (быстрый отказ до создания операции). Путь
// записи зовёт ту же проверку с писателем ОТКРЫТОЙ транзакции — только там
// блокировки делают ответ гоночно-стойким; см. validateSGTargetCidrGroup.
type repoCidrGroupReader struct{ repo Repo }

// Get — чтение через свежую Reader-TX (та же форма, что у остальных read-путей
// пакета).
func (r repoCidrGroupReader) Get(ctx context.Context, id string) (*kachorepo.CidrGroupRecord, error) {
	rd, err := r.repo.Reader(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rd.Close() }()
	return rd.CidrGroups().Get(ctx, id)
}
