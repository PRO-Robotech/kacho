// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

// scopeFilteredMethodsExist — есть ли в карте прав хоть один метод, чья
// авторизация возложена на фильтр видимости. Гард S3 по построению молчит, когда
// таких методов нет, поэтому случай обязан спрашивать карту, а не предполагать.
func scopeFilteredMethodsExist() bool { return len(check.ScopeFilteredRPCs()) > 0 }

// Отказ старта проверяется на ПРОЦЕССЕ, а не на функции.
//
// «Гард вернул ошибку» и «сервис не поднялся» — разные утверждения, и расходятся
// они молча: гард может быть написан верно и не быть вызван, вызван и не превратить
// ошибку в отказ, или стоять позже первого соединения — тогда наблюдаемой причиной
// станет недоступная зависимость, а не небезопасная настройка. Поэтому здесь
// собирается реальный бинарь и запускается с реальным окружением, а утверждение
// делается о коде возврата и о том, что процесс СКАЗАЛ, отказываясь.
//
// База — окружение, в котором боевой vpc обязан подниматься; каждый случай ослабляет
// РОВНО одну ручку. Ни одна зависимость (БД, iam, geo) при этом не поднимается: если
// проверка настройки требует живого соседа, чтобы сработать, она не защищает старт.

// buildVPCBinary собирает бинарь один раз на прогон пакета.
func buildVPCBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "kacho-vpc-boot-test")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build kacho-vpc: %v\n%s", err, out)
	}
	return bin
}

// productionEnv — окружение боевой посадки, в котором ВСЕ рёбра защищены и все
// прочие гарды удовлетворены. Возвращается как map, чтобы случай ослаблял ровно
// одну ручку и это было видно в самом случае.
func productionEnv() map[string]string {
	return map[string]string{
		"KACHO_VPC_CONFIG_PATH":                   "", // только defaults + ENV
		"KACHO_VPC_AUTH_MODE":                     "production",
		"KACHO_VPC_REPOSITORY__POSTGRES__URL":     "postgres://vpc@db-that-is-never-dialled:5432/kacho_vpc",
		"KACHO_VPC_DB_SSLMODE":                    "require",
		"KACHO_VPC_AUTHZ__IAM_ENDPOINT":           "kaname-internal:9091",
		"KACHO_VPC_AUTHZ__TRUSTED_FORWARDER_SANS": "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway",
		// Домен доверия — величина установки: без неё дескриптор не принимается,
		// потому что процесс, не назвавший домена, своим не признаёт никого.
		"KACHO_VPC_AUTHZ__TRUST_DOMAIN":                       "kacho.cloud",
		"KACHO_VPC_AUTHZ__LIST_FILTER__ENABLED":               "true",
		"KACHO_VPC_AUTHZ__LIST_FILTER__AUTHORIZE_ENDPOINT":    "kaname:9090",
		"KACHO_VPC_AUTHZ__LIST_FILTER__AUTHORIZE_TLS__ENABLE": "true",
		"KACHO_VPC_PUBLIC_SERVER_MTLS_ENABLE":                 "true",
		"KACHO_VPC_INTERNAL_SERVER_MTLS_ENABLE":               "true",
		"KACHO_VPC_IAM_AUTHZ_MTLS_ENABLE":                     "true",
		"KACHO_VPC_IAM_PROJECT_MTLS_ENABLE":                   "true",
		"KACHO_VPC_IAM_REGISTER_MTLS_ENABLE":                  "true",
		"KACHO_VPC_GEO_MTLS_ENABLE":                           "true",
		// Профиль возможностей исполнителя объявлен ПОЛНОСТЬЮ — иначе окружение
		// перестало бы быть тем, «в котором боевой vpc обязан подниматься», и
		// каждый случай ниже падал бы по чужой причине.
		"KACHO_VPC_DATAPLANE__EXECUTOR__OVERLAPPING_TENANT_ADDRESSES":            "true",
		"KACHO_VPC_DATAPLANE__EXECUTOR__STATE_TRACKING_FAMILIES":                 "v4,v6",
		"KACHO_VPC_DATAPLANE__EXECUTOR__NAMED_SET_REFERENCE_IN_RULE":             "true",
		"KACHO_VPC_DATAPLANE__EXECUTOR__GUARANTEED_PAYLOAD_BYTES":                "1450",
		"KACHO_VPC_DATAPLANE__EXECUTOR__GUARANTEED_BANDWIDTH_PER_INTERFACE_MBPS": "1000",
		"KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_LIMIT_PER_INTERFACE":          "65536",
		// Темп установления соединений и его всплеск — те же величины профиля и по
		// той же причине: боевая посадка обязана объявить каждую, и без них КАЖДЫЙ
		// случай ниже падал бы на них, а не на своём предмете. Числа выше
		// опубликованных потолков (2 000 и 8 000) — фикстура не вправе быть
		// снисходительнее продукта.
		"KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_RATE_LIMIT_PER_INTERFACE_PER_SECOND": "4000",
		"KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_RATE_BURST_PER_INTERFACE":            "16000",
		"KACHO_VPC_DATAPLANE__EXECUTOR__TENANT_SETTABLE_BANDWIDTH_LIMIT":                "false",
		// Перечень служебных диапазонов объявлен — по той же причине, что и профиль
		// выше: без него окружение перестало бы быть тем, «в котором боевой vpc
		// обязан подниматься», и каждый случай падал бы по чужой причине. Значения —
		// то, что зарезервировано на любой посадке (link-local обоих семейств).
		"KACHO_VPC_DATAPLANE__RESERVED_PREFIXES": "169.254.0.0/16,fe80::/10",
		// Величины допуска запросов объявлены по обоим листенерам — по той же
		// причине, что профиль и перечень выше: без них окружение перестало бы
		// быть тем, «в котором боевой vpc обязан подниматься», и каждый случай
		// падал бы по чужой причине. Требование к числам ровно одно — набор
		// объявлен полностью и проходит стража; совпадение с числами чарта здесь
		// не утверждается (годность чарта держит своя проба).
		//
		// Внутренний листенер идёт заведомо выше публичного, и это решение, а не
		// щедрость: ограничитель, задушивший наш собственный поток намерения,
		// воспроизводит заклинивание головы очереди — класс, при котором работа
		// перестаёт доезжать без единого видимого симптома.
		"KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__READ_PER_SEC":       "100",
		"KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__MUTATION_PER_SEC":   "20",
		"KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__BURST_FACTOR":       "5",
		"KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__IN_FLIGHT":          "16",
		"KACHO_VPC_API_SERVER__RATE_LIMIT__INTERNAL__READ_PER_SEC":     "1000",
		"KACHO_VPC_API_SERVER__RATE_LIMIT__INTERNAL__MUTATION_PER_SEC": "500",
		"KACHO_VPC_API_SERVER__RATE_LIMIT__INTERNAL__BURST_FACTOR":     "5",
		"KACHO_VPC_API_SERVER__RATE_LIMIT__INTERNAL__IN_FLIGHT":        "256",
	}
}

// runBoot запускает собранный бинарь с заданным окружением и возвращает
// объединённый вывод и код возврата.
func runBoot(t *testing.T, bin string, env map[string]string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, "serve")
	// Пустое окружение + только объявленные ключи: наследованный KACHO_VPC_*
	// с машины разработчика иначе менял бы посадку под тестом.
	flat := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for k, v := range env {
		flat = append(flat, k+"="+v)
	}
	cmd.Env = flat
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if err != nil {
		if !asExitError(err, &ee) {
			t.Fatalf("run kacho-vpc: %v\n%s", err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// vpc9c-P-01: боевая посадка, где ВСЁ защищено, кроме соединения фильтра видимости
// → процесс НЕ поднимается, и причина названа. До гарда этот же запуск уходил
// дальше и поднимал соединение с insecure-креденшелами.
func TestBootRefusal_ListFilterAuthorizeEdgeInsecure(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	// Ослабляем РОВНО одну ручку: собственный client-cert ребра фильтра.
	env["KACHO_VPC_AUTHZ__LIST_FILTER__AUTHORIZE_TLS__ENABLE"] = "false"
	// И снимаем общий с ребром Check client-cert, которым это ребро тоже
	// покрывается, — иначе ослабление ничего не меняет. Само ребро Check при этом
	// остаётся защищённым проверяемым server-TLS, то есть S4 в прежнем виде доволен.
	env["KACHO_VPC_IAM_AUTHZ_MTLS_ENABLE"] = "false"
	env["KACHO_VPC_AUTHZ__IAM_TLS__ENABLE"] = "true"

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started with the visibility-filter edge unprotected; output:\n%s", out)
	}
	if !strings.Contains(out, "list-filter authorize edge") {
		t.Fatalf("refusal must name the edge it refuses for; output:\n%s", out)
	}
	// Отказ обязан наступать ДО первого соединения: иначе наблюдаемой причиной
	// станет недоступная БД, и настройку никто не увидит.
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}

// vpc9c-P-02: та же посадка, но ребро защищено → процесс проходит гарды и падает
// уже на недоступной БД. Контроль на ту же форму: без него нельзя отличить «гард
// отверг настройку» от «бинарь не стартует вообще».
func TestBootRefusal_SecureListFilterEdgePassesTheGuards(t *testing.T) {
	bin := buildVPCBinary(t)

	out, code := runBoot(t, bin, productionEnv())
	if code == 0 {
		t.Fatalf("process cannot succeed without a database; output:\n%s", out)
	}
	if strings.Contains(out, "config validate") {
		t.Fatalf("fully-protected production posture must pass every boot guard; output:\n%s", out)
	}
	if !strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("expected the run to reach the database dial; output:\n%s", out)
	}
}

// vpc9c-P-03: мягкий проход фильтра при ScopeFiltered RPC — тот же наблюдаемый
// отказ. Случай остаётся осмысленным и когда ScopeFiltered-методов в карте нет:
// тогда гард молчит по построению, и это ровно то, что он обещает.
func TestBootRefusal_ListFilterFailOpen(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	env["KACHO_VPC_AUTHZ__LIST_FILTER__FAIL_OPEN"] = "true"

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process cannot succeed without a database; output:\n%s", out)
	}
	if scopeFilteredMethodsExist() {
		if !strings.Contains(out, "authz.list-filter.fail-open") {
			t.Fatalf("with ScopeFiltered RPCs present the soft-pass must refuse the start; output:\n%s", out)
		}
		return
	}
	if strings.Contains(out, "authz.list-filter.fail-open") {
		t.Fatalf("with no ScopeFiltered RPC the guard must stay silent; output:\n%s", out)
	}
}

// vpc42-P-01: пересечение адресов НЕ объявлено поддержанным → процесс не
// поднимается, называет ручку и останавливается ДО первого соединения.
//
// Ослабляется РОВНО одна ручка. Парный положительный контроль — тот же, что у
// случаев выше (vpc9c-P-02): на полном профиле процесс проходит все гарды и
// доходит до недоступной БД, поэтому «отказал по настройке» отличимо от «бинарь не
// стартует вообще».
func TestBootRefusal_ExecutorProfileOverlapNotDeclared(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	env["KACHO_VPC_DATAPLANE__EXECUTOR__OVERLAPPING_TENANT_ADDRESSES"] = "false"

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started while the executor does not declare tenant-address isolation; output:\n%s", out)
	}
	if !strings.Contains(out, "dataplane.executor.overlapping-tenant-addresses") {
		t.Fatalf("refusal must name the knob the operator has to set; output:\n%s", out)
	}
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}

// vpc42-P-02: отслеживание состояния задано ОДИНОКОЙ ЗАПЯТОЙ — вырожденный вход
// на живом процессе.
//
// Сырая настройка при этом непуста, а семейств в ней ноль. Пока страж и читатель
// спрашивают ОДИН предикат, процесс не поднимается; предикат по длине сырой
// настройки прочитал бы её как заполненную, и посадка с неизвестной статусностью
// поднялась бы молча. Проверяется именно на процессе: подстановка поля в юните не
// прошла бы через разбор строки окружения, где запятая и превращается в две пустые
// записи.
func TestBootRefusal_ExecutorProfileStateTrackingIsALoneComma(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	env["KACHO_VPC_DATAPLANE__EXECUTOR__STATE_TRACKING_FAMILIES"] = ","

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started with an undeclared state-tracking profile; output:\n%s", out)
	}
	if !strings.Contains(out, "dataplane.executor.state-tracking-families") {
		t.Fatalf("refusal must name the knob; output:\n%s", out)
	}
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}

// vpc43-P-01: перечень служебных диапазонов НЕ объявлен → процесс не поднимается,
// называет ручку и останавливается ДО первого соединения.
//
// Это и есть инъекция стража на ЖИВОМ ПРОЦЕССЕ: значение снимается — старт падает
// с называющим текстом; парный положительный контроль тот же, что у случаев выше
// (vpc9c-P-02): на полном окружении процесс проходит все гарды и доходит до
// недоступной БД. Без этой пары «отказал по настройке» было бы неотличимо от
// «бинарь не стартует вообще».
//
// Почему пустое значение — это отказ, а не «нечего резервировать»: перечень
// объявляет ПОСАДКА, и пустой перечень означает «не сужаем». Проверка на пути
// запроса при этом присутствует, исполняется на каждом создании подсети и не
// отвергает ничего — то есть выглядит работающей, ни разу не отказав.
func TestBootRefusal_ReservedPrefixesNotDeclared(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	env["KACHO_VPC_DATAPLANE__RESERVED_PREFIXES"] = ""

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started without a declared reserved-prefix list; output:\n%s", out)
	}
	if !strings.Contains(out, "dataplane.reserved-prefixes") {
		t.Fatalf("refusal must name the knob the operator has to set; output:\n%s", out)
	}
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}

// vpc43-P-02: перечень задан ОДИНОКОЙ ЗАПЯТОЙ — вырожденный вход на живом процессе.
//
// Сырая настройка непуста, а диапазонов в ней ноль. Пока страж и читатель
// спрашивают ОДИН предикат, процесс не поднимается; предикат по длине сырой
// настройки прочитал бы её как заполненную, и посадка без резерва поднялась бы
// молча. Проверяется именно на процессе: подстановка поля в юните не прошла бы
// через разбор строки окружения, где запятая и превращается в две пустые записи.
func TestBootRefusal_ReservedPrefixesIsALoneComma(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	env["KACHO_VPC_DATAPLANE__RESERVED_PREFIXES"] = ","

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started with an undeclared reserved-prefix list; output:\n%s", out)
	}
	if !strings.Contains(out, "dataplane.reserved-prefixes") {
		t.Fatalf("refusal must name the knob; output:\n%s", out)
	}
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}

// vpc43-P-03: негодная запись перечня — негодное ОБЪЯВЛЕНИЕ, а не посадка: отказ
// наступает и там, где посадка не боевая. Подан на живом процессе в dev-режиме
// именно затем, чтобы разделение «посадка против объявления» не осталось
// утверждением одного юнита.
//
// Запись `10.0.0.1/24` выбрана намеренно: она РАЗБИРАЕТСЯ, поэтому молчаливая
// нормализация выглядела бы безобидной — и расширила бы резерв без ведома автора.
func TestBootRefusal_ReservedPrefixUnusableEntryEvenInDev(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	env["KACHO_VPC_AUTH_MODE"] = "dev"
	env["KACHO_VPC_DATAPLANE__RESERVED_PREFIXES"] = "169.254.0.0/16,10.0.0.1/24"

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started with an unusable reserved-prefix entry; output:\n%s", out)
	}
	if !strings.Contains(out, "10.0.0.1/24") {
		t.Fatalf("refusal must quote what the operator wrote; output:\n%s", out)
	}
	if !strings.Contains(out, "10.0.0.0/24") {
		t.Fatalf("refusal must name the form the entry has to be rewritten to; output:\n%s", out)
	}
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}

// vpc42-P-03: неизвестное семейство — негодное ОБЪЯВЛЕНИЕ, а не посадка: отказ
// наступает и там, где посадка не боевая. Случай подан на живом процессе в
// dev-режиме именно затем, чтобы разделение «посадка против объявления» не осталось
// утверждением одного юнита.
func TestBootRefusal_ExecutorProfileUnknownFamilyEvenInDev(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	env["KACHO_VPC_AUTH_MODE"] = "dev"
	env["KACHO_VPC_DATAPLANE__EXECUTOR__STATE_TRACKING_FAMILIES"] = "v4,ipv6"

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started with an unusable family declaration; output:\n%s", out)
	}
	if !strings.Contains(out, "ipv6") {
		t.Fatalf("refusal must quote what the operator wrote; output:\n%s", out)
	}
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}

// vpc49-P-01: величины допуска запросов НЕ объявлены → процесс не поднимается,
// называет ручку и останавливается ДО первого соединения.
//
// Это инъекция стража S7 на ЖИВОМ ПРОЦЕССЕ: значение снимается — старт падает с
// называющим текстом; парный положительный контроль тот же, что у случаев выше
// (vpc9c-P-02): на полном окружении процесс проходит все гарды и доходит до
// недоступной БД. Без этой пары «отказал по настройке» было бы неотличимо от
// «бинарь не стартует вообще».
//
// Почему нулевые величины — это отказ, а не «ограничивать нечего»: ноль означает
// «не ограничиваем». Ограничитель тогда либо не навешивается вовсе, либо
// навешивается пустым — и в обоих случаях выглядит включённым, ни разу не отказав,
// пока один вызывающий занимает базу, обслуживающую всех.
func TestBootRefusal_RequestRateLimitsNotDeclared(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	// Снимаем РОВНО одну ось одного листенера: неполный набор негоден так же, как
	// пустой, и именно это здесь и утверждается.
	env["KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__IN_FLIGHT"] = "0"

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started without a declared request admission limit; output:\n%s", out)
	}
	if !strings.Contains(out, "api-server.rate-limit.public") {
		t.Fatalf("refusal must name the knob the operator has to set; output:\n%s", out)
	}
	if strings.Contains(out, "api-server.rate-limit.internal") {
		t.Fatalf("refusal must not blame the listener that IS declared; output:\n%s", out)
	}
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}

// vpc49-P-02: внутренний листенер судится отдельно от публичного.
//
// Без этого случая страж, смотрящий только на публичный, был бы зелёным на всём
// предыдущем: внутренний листенер зовут наши же модули, и незамеченный там ноль
// означает, что поток намерения ничем не ограничен.
func TestBootRefusal_RequestRateLimitsInternalNotDeclared(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	env["KACHO_VPC_API_SERVER__RATE_LIMIT__INTERNAL__READ_PER_SEC"] = "0"

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started with the internal listener unlimited; output:\n%s", out)
	}
	if !strings.Contains(out, "api-server.rate-limit.internal") {
		t.Fatalf("refusal must name the internal knob; output:\n%s", out)
	}
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}

// vpc49-P-03: самопротиворечивое ОБЪЯВЛЕНИЕ отвергается и там, где посадка не
// боевая.
//
// Всплеск ниже устойчивого темпа — ведро, которое не наполняется до одного
// токена: ограничитель отвергал бы даже законный поток, то есть выглядел бы
// работающим и ломал продукт. Это негодность сама по себе, а не выбор оператора,
// поэтому режим здесь ни при чём — и случай подан на живом процессе в dev именно
// затем, чтобы разделение «посадка против объявления» не осталось утверждением
// одного юнита.
func TestBootRefusal_RequestRateBurstBelowSustainedEvenInDev(t *testing.T) {
	bin := buildVPCBinary(t)

	env := productionEnv()
	env["KACHO_VPC_AUTH_MODE"] = "dev"
	env["KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC__BURST_FACTOR"] = "0.5"

	out, code := runBoot(t, bin, env)
	if code == 0 {
		t.Fatalf("process started with a self-contradicting burst factor; output:\n%s", out)
	}
	if !strings.Contains(out, "api-server.rate-limit.public") {
		t.Fatalf("refusal must name the knob; output:\n%s", out)
	}
	if strings.Contains(out, "db-that-is-never-dialled") {
		t.Fatalf("guard must refuse before any dial (database was contacted); output:\n%s", out)
	}
}
