// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// PrefixStorageBackend — id-префикс зарегистрированного бэкенда, hyphen-канон
// (`sb-<crockford-base32>`, ids.NewHyphenID). Единственное место, называющее семью:
// префикс обязан входить в каталог hyphen-префиксов corelib, иначе роутер отвергнет
// well-formed id, который произвёл наш же генератор.
//
// Проверкой ПРИНАДЛЕЖНОСТИ к семье форма id не является и являться не может:
// проверка формата в corelib family-agnostic по контракту. Принадлежность держит
// внешняя связь `disk_type_bindings.backend_id → storage_backends.id` (0015) — то
// есть БД, а не разбор строки.
const PrefixStorageBackend = "sb"

// BackendKind — вид зарегистрированного бэкенда. ЗАКРЫТЫЙ словарь.
//
// Свободная строка означала бы, что композиционный корень выбирает адаптер по
// значению, которого никто не объявлял: неизвестный вид тогда либо молча остаётся
// без адаптера, либо получает чужой. Ровно те же соображения, по которым закрыт
// словарь яруса класса (disk_type.go), только цена ошибки здесь — данные.
type BackendKind string

// Виды бэкендов. Сегодня объявлен один — по числу написанных адаптеров. Второй
// вид заводится вместе со своим адаптером, а не заранее: перечисление, у которого
// нет реализации, обещает выбор, которого нет.
const BackendKindCephRBD BackendKind = "CEPH_RBD"

// Valid сообщает, принадлежит ли вид словарю. Пустой вид НЕ валиден: бэкенд без
// вида нечем обслуживать.
func (k BackendKind) Valid() bool {
	return k == BackendKindCephRBD
}

// BackendStatus — состояние обращения бэкенда. Отвечает на единственный вопрос:
// можно ли завести на нём НОВУЮ ревизию привязки. Уже созданные ресурсы живут при
// любом значении — вывод бэкенда из обращения не отзывает чужие данные.
type BackendStatus string

// Состояния бэкенда.
const (
	// BackendStatusActive — принимает новые привязки.
	BackendStatusActive BackendStatus = "ACTIVE"
	// BackendStatusDraining — новые привязки не заводятся, существующие работают.
	// Состояние вывода из эксплуатации: сначала перестают приходить новые тома,
	// потом мигрируют старые.
	BackendStatusDraining BackendStatus = "DRAINING"
	// BackendStatusDisabled — бэкенд выключен администратором.
	BackendStatusDisabled BackendStatus = "DISABLED"
)

// Valid сообщает, принадлежит ли состояние словарю. Пустое — НЕ валидно: умолчание
// проставляет край, домен не угадывает намерение админа.
func (s BackendStatus) Valid() bool {
	switch s {
	case BackendStatusActive, BackendStatusDraining, BackendStatusDisabled:
		return true
	default:
		return false
	}
}

// maxCredentialsRefLen — потолок длины ссылки на учётные данные. Отсекает не
// секрет (его отсекает форма ниже), а превращение поля-КООРДИНАТЫ в поле-полезную-
// нагрузку: координата в хранилище секретов — это путь, а не документ.
const maxCredentialsRefLen = 256

// credentialsRefRe — форма ссылки на учётные данные: схема из закрытого словаря,
// затем непустая координата из букв, цифр и разделителей пути.
//
// БЕЛЫЙ список формы, а НЕ детектор «похоже на секрет». Чёрный список форм секрета
// неполон by construction — материал, который его обошёл бы, лёг бы в колонку
// навсегда, — поэтому здесь отвергается всё, что не является координатой
// объявленного вида, и материал секрета отвергается как частный случай: пробелы,
// переводы строк, `=`, `+`, кавычки и фигурные скобки в координату не входят.
//
// Схем ровно две, и словарь закрыт намеренно: «любая схема» принимает `data:` и
// `raw:`, которые несут материал в себе, — то есть возвращает ровно то, ради чего
// поле сделано ссылкой.
var credentialsRefRe = regexp.MustCompile(`^(vault|cfg)://[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// errCredentialsRefForm — отказ по форме ссылки на учётные данные.
//
// Сообщение НИКОГДА не воспроизводит отвергнутое значение: иначе поле, заведённое
// ради того, чтобы секрет не попал в БД, само отправило бы его в журнал и в тело
// ответа API — в те же два места, ради которых всё и затевалось. При этом отказ
// обязан оставаться полезным, поэтому называет поле и принимаемую форму.
var errCredentialsRefForm = errors.New(
	"credentials_ref must be a reference of the form vault://<path> or cfg://<path>, not a secret value")

// CredentialsRef — ССЫЛКА на учётные данные бэкенда. Self-validating newtype: сам
// секрет через API не проходит и в БД не ложится.
//
// Почему ссылка, а не значение. Материал, попавший в колонку, переживает ротацию
// (ключ сменили — строка осталась прежней и продолжает выглядеть действующей) и
// уезжает в каждую резервную копию, откуда его уже не отозвать. Две законные точки
// разрешения: `vault://` — координата в хранилище секретов, `cfg://` — координата в
// конфигурации процесса (смонтированный файл, переменная окружения). Обе внешние по
// отношению и к API, и к БД.
type CredentialsRef string

// Validate проверяет форму ссылки.
//
// Оговорка, которую обязан помнить каждый, кто это трогает: проверка утверждает
// ФОРМУ ссылки, а не отсутствие секрета внутри неё. Доказать второе разбором строки
// нельзя — можно лишь не оставлять форме места для материала.
func (r CredentialsRef) Validate() error {
	if r == "" {
		return fmt.Errorf("storage_backend credentials_ref is required")
	}
	if len(r) > maxCredentialsRefLen {
		return errCredentialsRefForm
	}
	// Сегмент `..` — выход за пределы поддерева, за которым закреплены права:
	// резолвер, склеивающий путь, увёл бы чтение в чужую ветку хранилища.
	if strings.Contains(string(r), "..") {
		return errCredentialsRefForm
	}
	if !credentialsRefRe.MatchString(string(r)) {
		return errCredentialsRefForm
	}
	return nil
}

// StorageBackend — зарегистрированный бэкенд плоскости данных. Ресурс ТОЛЬКО
// внутреннего листенера (:9091): и координата, и перечень зон, и вид — сведения о
// физике, которым на публичной поверхности не место.
//
// Класс диска на бэкенд НЕ ссылается: ссылается неизменяемая ревизия привязки
// (disk_type_binding.go). Так смена бэкенда у класса не переадресует данные уже
// созданных томов — она заводит новую ревизию, а прежняя продолжает описывать то,
// что обещали прежним ресурсам.
type StorageBackend struct {
	ID   string
	Name string
	Kind BackendKind
	// Description — свободное пояснение для оператора.
	Description string
	// ZoneIDs — зоны, которые бэкенд обслуживает. Cross-service ссылки на geo.Zone
	// по id, без внешней связи: владелец географии — geo, существование зоны
	// проверяется у него.
	ZoneIDs []string
	// Endpoint — НЕПРОЗРАЧНАЯ координата, разрешаемая конфигурацией процесса.
	// Формой здесь не проверяется намеренно: адреса мониторов и имена пулов — это
	// сведения о физике, и от публичной поверхности их закрывает не разбор строки,
	// а то, что весь ресурс живёт только на внутреннем листенере.
	Endpoint string
	// CredentialsRef — ссылка на учётные данные. Никогда не значение.
	CredentialsRef CredentialsRef
	Status         BackendStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ValidateBackendName — правило имени бэкенда, одно на создание и на правку.
//
// Вынесено из Validate именно затем, чтобы правка могла позвать ЕГО, а не свою
// копию: до этого правка не звала ничего, и создание с правкой расходились в том
// единственном месте, где расхождение стоит дорого — имя уникально по всей
// установке полным индексом (0015).
//
// Имя обязательно, хотя у прочих ресурсов домена пустое имя законно: имя —
// единственный человекочитаемый ключ бэкенда. Пустое здесь не «безымянный
// ресурс», а занятая единственная безымянная строка, после которой второй
// бэкенд без имени не заводится.
func ValidateBackendName(name string) error {
	if name == "" {
		return fmt.Errorf("storage_backend name is required")
	}
	return validateName("storage_backend name", name)
}

// Validate проверяет domain-инварианты бэкенда перед сохранением.
func (b StorageBackend) Validate() error {
	if b.ID == "" {
		return fmt.Errorf("storage_backend id is required")
	}
	// Имя обязательно, хотя у прочих ресурсов домена пустое имя законно: имя —
	// единственный человекочитаемый ключ бэкенда и уникально по всей установке
	// (0015). Пустое имя здесь не «безымянный ресурс», а занятая единственная
	// безымянная строка, после которой второй бэкенд без имени не заводится.
	if err := ValidateBackendName(b.Name); err != nil {
		return err
	}
	if !b.Kind.Valid() {
		return fmt.Errorf("storage_backend kind %q is not a known kind", b.Kind)
	}
	if !b.Status.Valid() {
		return fmt.Errorf("storage_backend status %q is not a known status", b.Status)
	}
	if err := validateBackendZones(b.ZoneIDs); err != nil {
		return err
	}
	if b.Endpoint == "" {
		return fmt.Errorf("storage_backend endpoint is required")
	}
	// Отказ по учётным данным возвращается КАК ЕСТЬ, без обёртки: любая обёртка —
	// это место, где значение однажды допишут в текст.
	return b.CredentialsRef.Validate()
}

// AcceptsNewBindings сообщает, можно ли завести на бэкенде новую ревизию привязки.
// Единственный вопрос, ради которого заведено состояние обращения; существующие
// ревизии и созданные под ними ресурсы продолжают работать при любом значении.
func (b StorageBackend) AcceptsNewBindings() bool { return b.Status == BackendStatusActive }

// validateBackendZones проверяет перечень обслуживаемых зон.
//
// Пустой перечень законен: бэкенд регистрируют раньше, чем расписывают по зонам, и
// привязка к зоне, которую он не обслуживает, отвергается на своём пути. Пустая
// строка и повтор — нет: первая совпала бы с «зона не указана» у ресурса, второй
// делает перечень непригодным для счёта.
func validateBackendZones(zones []string) error {
	seen := make(map[string]struct{}, len(zones))
	for _, z := range zones {
		if z == "" {
			return fmt.Errorf("storage_backend zone_ids must not contain an empty zone id")
		}
		if _, dup := seen[z]; dup {
			return fmt.Errorf("storage_backend zone_ids lists zone %s twice", z)
		}
		seen[z] = struct{}{}
	}
	return nil
}
