package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type poolMetric struct {
	desc      *prometheus.Desc
	valueType prometheus.ValueType
	value     func(*pgxpool.Stat) float64
}

// PoolCollector exports pgxpool.Stat on every scrape.
type PoolCollector struct {
	pool    *pgxpool.Pool
	metrics []poolMetric
}

// NewPoolCollector returns a collector for pool's connection statistics.
func NewPoolCollector(pool *pgxpool.Pool) *PoolCollector {
	gauge := func(name, help string, value func(*pgxpool.Stat) float64) poolMetric {
		return poolMetric{prometheus.NewDesc(name, help, nil, nil), prometheus.GaugeValue, value}
	}
	counter := func(name, help string, value func(*pgxpool.Stat) float64) poolMetric {
		return poolMetric{prometheus.NewDesc(name, help, nil, nil), prometheus.CounterValue, value}
	}
	return &PoolCollector{pool: pool, metrics: []poolMetric{
		gauge("pgxpool_acquired_conns", "Connections currently acquired from the pool.",
			func(s *pgxpool.Stat) float64 { return float64(s.AcquiredConns()) }),
		gauge("pgxpool_idle_conns", "Idle connections in the pool.",
			func(s *pgxpool.Stat) float64 { return float64(s.IdleConns()) }),
		gauge("pgxpool_constructing_conns", "Connections being opened.",
			func(s *pgxpool.Stat) float64 { return float64(s.ConstructingConns()) }),
		gauge("pgxpool_total_conns", "Total connections in the pool, including constructing ones.",
			func(s *pgxpool.Stat) float64 { return float64(s.TotalConns()) }),
		gauge("pgxpool_max_conns", "Maximum size of the pool.",
			func(s *pgxpool.Stat) float64 { return float64(s.MaxConns()) }),
		counter("pgxpool_acquires_total", "Successful connection acquires.",
			func(s *pgxpool.Stat) float64 { return float64(s.AcquireCount()) }),
		counter("pgxpool_acquire_duration_seconds_total", "Total time spent acquiring connections.",
			func(s *pgxpool.Stat) float64 { return s.AcquireDuration().Seconds() }),
		counter("pgxpool_empty_acquires_total", "Acquires that had to wait because no idle connection was available.",
			func(s *pgxpool.Stat) float64 { return float64(s.EmptyAcquireCount()) }),
		counter("pgxpool_empty_acquire_wait_seconds_total", "Total time acquires spent waiting for a connection when none was idle.",
			func(s *pgxpool.Stat) float64 { return s.EmptyAcquireWaitTime().Seconds() }),
		counter("pgxpool_canceled_acquires_total", "Acquires canceled by their context.",
			func(s *pgxpool.Stat) float64 { return float64(s.CanceledAcquireCount()) }),
		counter("pgxpool_new_conns_total", "Connections opened.",
			func(s *pgxpool.Stat) float64 { return float64(s.NewConnsCount()) }),
		counter("pgxpool_max_lifetime_destroys_total", "Connections closed for exceeding MaxConnLifetime.",
			func(s *pgxpool.Stat) float64 { return float64(s.MaxLifetimeDestroyCount()) }),
		counter("pgxpool_max_idle_destroys_total", "Connections closed for exceeding MaxConnIdleTime.",
			func(s *pgxpool.Stat) float64 { return float64(s.MaxIdleDestroyCount()) }),
	}}
}

// Describe implements prometheus.Collector.
func (c *PoolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range c.metrics {
		ch <- m.desc
	}
}

// Collect implements prometheus.Collector.
func (c *PoolCollector) Collect(ch chan<- prometheus.Metric) {
	stat := c.pool.Stat()
	for _, m := range c.metrics {
		ch <- prometheus.MustNewConstMetric(m.desc, m.valueType, m.value(stat))
	}
}
