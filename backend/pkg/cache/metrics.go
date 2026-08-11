package cache

import "github.com/prometheus/client_golang/prometheus"

// Metrics is the operator-facing view of the cache (FR-019).
//
// Every label here is drawn from a small closed set — nine families, three
// scopes, four operations. An event id, order id, or filter value must never
// become a label value: Prometheus cardinality is per distinct label
// combination, and a per-event label would grow the series count with the
// catalogue.
type Metrics struct {
	requests    *prometheus.CounterVec // family, result
	invalidatio *prometheus.CounterVec // scope
	invalFails  prometheus.Counter
	errors      *prometheus.CounterVec // op
	distrusted  prometheus.Gauge
}

// NewMetrics registers the collectors. Passing a registry rather than using the
// default one matches how pkg/observability builds its HTTP metrics.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		requests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_requests_total",
				Help: "List-cache lookups by family and outcome (hit, miss, bypass).",
			},
			[]string{"family", "result"},
		),
		invalidatio: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_invalidations_total",
				Help: "Generation bumps by scope, one per committed write that touched cached data.",
			},
			[]string{"scope"},
		),
		invalFails: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cache_invalidation_failures_total",
			Help: "Invalidations that failed after their write committed. Non-zero means entries were provably stale and the cache entered distrust.",
		}),
		errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_errors_total",
				Help: "Cache store errors by operation. These are absorbed — reads fall back to the database — but they are not normal.",
			},
			[]string{"op"},
		),
		distrusted: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cache_distrusted",
			Help: "1 while cached values are being bypassed after a failed invalidation, 0 otherwise.",
		}),
	}
	if reg != nil {
		reg.MustRegister(m.requests, m.invalidatio, m.invalFails, m.errors, m.distrusted)
	}
	return m
}

func (m *Metrics) hit(f Family)    { m.count(f, "hit") }
func (m *Metrics) miss(f Family)   { m.count(f, "miss") }
func (m *Metrics) bypass(f Family) { m.count(f, "bypass") }

func (m *Metrics) count(f Family, result string) {
	if m == nil {
		return
	}
	m.requests.WithLabelValues(string(f), result).Inc()
}

func (m *Metrics) invalidated(s Scope) {
	if m == nil {
		return
	}
	m.invalidatio.WithLabelValues(s.String()).Inc()
}

func (m *Metrics) invalidationFailed() {
	if m == nil {
		return
	}
	m.invalFails.Inc()
}

func (m *Metrics) failed(op string) {
	if m == nil {
		return
	}
	m.errors.WithLabelValues(op).Inc()
}

func (m *Metrics) setDistrusted(v bool) {
	if m == nil {
		return
	}
	if v {
		m.distrusted.Set(1)
		return
	}
	m.distrusted.Set(0)
}
