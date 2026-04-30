package trace

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// MustInt64Counter creates an Int64Counter and panics on error.
func MustInt64Counter(m metric.Meter, name string, opts ...metric.Int64CounterOption) metric.Int64Counter {
	c, err := m.Int64Counter(name, opts...)
	if err != nil {
		panic(fmt.Sprintf("failed to create OTel counter %s: %v", name, err))
	}
	return c
}

// MustFloat64Counter creates a Float64Counter and panics on error.
func MustFloat64Counter(m metric.Meter, name string, opts ...metric.Float64CounterOption) metric.Float64Counter {
	c, err := m.Float64Counter(name, opts...)
	if err != nil {
		panic(fmt.Sprintf("failed to create OTel counter %s: %v", name, err))
	}
	return c
}

// MustFloat64Gauge creates a Float64Gauge and panics on error.
func MustFloat64Gauge(m metric.Meter, name string, opts ...metric.Float64GaugeOption) metric.Float64Gauge {
	g, err := m.Float64Gauge(name, opts...)
	if err != nil {
		panic(fmt.Sprintf("failed to create OTel gauge %s: %v", name, err))
	}
	return g
}
