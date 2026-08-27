// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "time"

// Magic-numbers и enum-литералы для domain-слоя (
// запрет inline-status / inline-magic-numbers в use-case-/handler-коде).
// Источник истины для всего, что выглядит как «магическая константа».

// ---- Размерные лимиты ------------------------------------------------------

const (
	// ShortIDLen — длина prefix-сегмента id, используемого в derived-именах
	// (например default-target-group-<8chars>). Зеркалит kacho-vpc.
	ShortIDLen = 8

	// MaxDescriptionLen — UTF-8 rune-count лимит description.
	MaxDescriptionLen = 256

	// MaxLabelPairs — cardinality limit для LbLabels.
	MaxLabelPairs = 64

	// MaxLabelKeyLen / MaxLabelValueLen — границы длины label key/value
	// (в байтах; regex отдельно).
	MaxLabelKeyLen   = 63
	MaxLabelValueLen = 63
)

// ---- Port / weight ---------------------------------------------------------

const (
	// PortMin / PortMax — границы TCP/UDP-порта (LbPort.Validate).
	PortMin LbPort = 1
	PortMax LbPort = 65535

	// MaxTargetWeight — верхняя граница weight таргета.
	// 0 разрешён и означает «drain effectively без remove».
	MaxTargetWeight LbWeight = 1000
)

// ---- HealthCheck defaults / границы ----------------------------------------

const (
	// DefaultHealthInterval / DefaultHealthTimeout —
	DefaultHealthInterval LbDuration = LbDuration(2 * time.Second)
	DefaultHealthTimeout  LbDuration = LbDuration(1 * time.Second)

	// HealthIntervalMin / Max —.
	HealthIntervalMin LbDuration = LbDuration(1 * time.Second)
	HealthIntervalMax LbDuration = LbDuration(600 * time.Second)

	// HealthTimeoutMin / Max — нижняя граница 1ms (positive), верхняя — не
	// больше interval. interval-comparison делается в HealthCheck.Validate.
	HealthTimeoutMin LbDuration = LbDuration(1 * time.Millisecond)

	// HealthThresholdMin / Max — [2..10].
	HealthThresholdMin int32 = 2
	HealthThresholdMax int32 = 10

	// DefaultUnhealthyThreshold / DefaultHealthyThreshold —
	DefaultUnhealthyThreshold int32 = 2
	DefaultHealthyThreshold   int32 = 2
)

// ---- Target group lifecycle -------------------------------------------------

const (
	// DefaultDeregistrationDelay — (300s). NLB-1c (B8): Duration-typed.
	DefaultDeregistrationDelay LbDuration = LbDuration(300 * time.Second)

	// DeregistrationDelayMin / Max — [0s..3600s]. NLB-1c (B8): Duration-typed.
	DeregistrationDelayMin LbDuration = LbDuration(0)
	DeregistrationDelayMax LbDuration = LbDuration(3600 * time.Second)

	// DefaultSlowStart — (0s = выключен). NLB-1c (B8): Duration-typed.
	DefaultSlowStart LbDuration = LbDuration(0)

	// SlowStartMin / Max — [0s..900s]. NLB-1c (B8): Duration-typed.
	SlowStartMin LbDuration = LbDuration(0)
	SlowStartMax LbDuration = LbDuration(900 * time.Second)

	// DefaultTargetWeight — (100).
	DefaultTargetWeight LbWeight = 100
)

// ---- Cardinality лимиты ----------------------------------------------------

const (
	// MaxTargetsPerGroup —; защита от raid'а ресурса DB.
	MaxTargetsPerGroup = 100

	// Предела на число слушателей одного балансировщика здесь БОЛЬШЕ НЕТ, и это
	// не пропуск. Он стоял тут объявлением `MaxListenersPerLB = 50`, которое не
	// читал ни один файл дерева: два вхождения, оба в самом объявлении.
	//
	// Мёртвая константа хуже отсутствующей — она читается как действующий предел.
	// Здесь она вдобавок ПРОТИВОРЕЧИЛА действующему: число слушателей на
	// балансировщик ограничивает учёт квот видом
	// `loadbalancer.networkLoadBalancers.listeners`, и его умолчание — 16, а не 50.
	// Читатель, поверивший объявлению, ошибся бы втрое и не в свою пользу.
	//
	// Величина живёт у владельца величин и меняется администратором; месту в коде
	// сервиса её быть не должно.

	// MaxSecurityGroupsPerLB — cap на security_group_ids. Каждый элемент набора
	// стоит ОДНОГО синхронного peer-Get в vpc (+ FGA-Check там) на request-path,
	// т.е. кардинальность набора напрямую умножает внешние round-trip'ы одного
	// дешёвого Create/Update. Проверяется в LoadBalancer.Validate() — ДО фазы
	// peer-validate (create.go/update.go), поэтому over-limit не стоит ни одного
	// внешнего вызова. Это ЕДИНСТВЕННЫЙ исполнитель предела: прежде ту же величину
	// объявлял и контракт, но не проверял никто, и объявление снято вместе со всем
	// семейством (kacho#1255).
	MaxSecurityGroupsPerLB = 50
)

// ---- Enum-литералы для свободных строковых newtypes -----------------------
// (inline `"TCP"` / `"IPV4"` в use-case-коде запрещён;
// сравниваем через эти именованные константы.)

const (
	ProtoTCP LbProto = "TCP"
	ProtoUDP LbProto = "UDP"

	IPVersionV4 IPVersion = "IPV4"
	IPVersionV6 IPVersion = "IPV6"
)
