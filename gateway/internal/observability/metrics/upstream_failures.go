// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/gateway/internal/upstreamfailure"
)

// upstreamFailuresDesc — клетки причины отказа вызова края соседу (kacho#3221).
// Ответ наружу у трёх неисправностей один (503), а чинят их разным: адрес,
// службу, бюджет. Клетка по соседу и причине называет, что именно.
var upstreamFailuresDesc = prometheus.NewDesc(
	"kacho_api_gateway_upstream_call_failures_total",
	"Failed edge calls to a neighbour, by upstream (the backend key: the chart's backends.<key>) and cause: "+
		"unresolved (the neighbour's address did not resolve to any address — fix the address knob or the "+
		"name server, not the service), unavailable (the address resolved but the link did not come up, or the "+
		"service itself answered UNAVAILABLE — the service is down), deadline (the call did not fit its budget: "+
		"callBudgets.identity for the identity service, callBudgets.backend for a domain bridge call, or an "+
		"earlier deadline of the caller). The answer to the caller is the same 503 for the first two; this cell tells them apart.",
	[]string{"upstream", "cause"}, nil)

// Соседи и причины — закрытый словарь МЕТОК этого файла. Это словарь
// поверхности, а не второе объявление: соседей объявляет карта адресов корня
// (`config.Config.BackendAddrs`), причины — пакет учёта; сходимость держат
// пробы `TestUpstreamFailures_UpstreamLabelsAreTheBackendAddressKeys` и
// `TestUpstreamFailures_CauseLabelsAreTheTallyCauses`. Клетка соседа или
// причины, не названной здесь, на поверхность не выйдет, — и проба сходимости
// это увидит.
const (
	upstreamVPC                  = "vpc"
	upstreamVPCInternal          = "vpcInternal"
	upstreamCompute              = "compute"
	upstreamComputeInternal      = "computeInternal"
	upstreamIAM                  = "iam"
	upstreamIAMInternal          = "iamInternal"
	upstreamLoadBalancer         = "loadbalancer"
	upstreamLoadBalancerInternal = "loadbalancerInternal"
	upstreamGeo                  = "geo"
	upstreamGeoInternal          = "geoInternal"
	upstreamRegistry             = "registry"
	upstreamRegistryInternal     = "registryInternal"
	upstreamStorage              = "storage"
	upstreamStorageInternal      = "storageInternal"

	upstreamCauseUnresolved  = "unresolved"
	upstreamCauseUnavailable = "unavailable"
	upstreamCauseDeadline    = "deadline"
)

// UpstreamFailureUpstreamLabels — значения метки `upstream`; для пробы сходимости.
func UpstreamFailureUpstreamLabels() []string {
	return []string{
		upstreamVPC, upstreamVPCInternal, upstreamCompute, upstreamComputeInternal,
		upstreamIAM, upstreamIAMInternal, upstreamLoadBalancer, upstreamLoadBalancerInternal,
		upstreamGeo, upstreamGeoInternal, upstreamRegistry, upstreamRegistryInternal,
		upstreamStorage, upstreamStorageInternal,
	}
}

// UpstreamFailureCauseLabels — значения метки `cause`; для пробы сходимости.
func UpstreamFailureCauseLabels() []string {
	return []string{upstreamCauseUnresolved, upstreamCauseUnavailable, upstreamCauseDeadline}
}

// RegisterUpstreamFailures провязывает читателя клеток учёта отказов соседям.
func (m *Metrics) RegisterUpstreamFailures(read func() []upstreamfailure.Cell) {
	if m == nil || read == nil {
		return
	}
	m.reg.MustRegister(&upstreamFailuresCollector{read: read})
}

type upstreamFailuresCollector struct {
	read func() []upstreamfailure.Cell
}

func (c *upstreamFailuresCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- upstreamFailuresDesc
}

type upstreamCellKey struct{ upstream, cause string }

// Collect — ни одного внешнего вызова: клетки — атомарные счётчики процесса.
// Метки берутся из закрытого словаря этого файла, а не из ключей снимка:
// клетка, по которой отказов не было, стоит нулём, а ключ, пришедший из
// данных, меткой не становится.
func (c *upstreamFailuresCollector) Collect(ch chan<- prometheus.Metric) {
	counts := map[upstreamCellKey]uint64{}
	for _, cell := range c.read() {
		counts[upstreamCellKey{cell.Upstream, string(cell.Cause)}] = cell.Count
	}
	for upstream := range map[string]struct{}{
		upstreamVPC: {}, upstreamVPCInternal: {}, upstreamCompute: {}, upstreamComputeInternal: {},
		upstreamIAM: {}, upstreamIAMInternal: {}, upstreamLoadBalancer: {}, upstreamLoadBalancerInternal: {},
		upstreamGeo: {}, upstreamGeoInternal: {}, upstreamRegistry: {}, upstreamRegistryInternal: {},
		upstreamStorage: {}, upstreamStorageInternal: {},
	} {
		for cause := range map[string]struct{}{
			upstreamCauseUnresolved: {}, upstreamCauseUnavailable: {}, upstreamCauseDeadline: {},
		} {
			ch <- prometheus.MustNewConstMetric(upstreamFailuresDesc, prometheus.CounterValue,
				float64(counts[upstreamCellKey{upstream, cause}]), upstream, cause)
		}
	}
}
