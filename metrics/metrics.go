package metrics

import (
	"context"
	"time"

	k6metrics "go.k6.io/k6/metrics"
)

type (
	K6Metrics struct {
		samples               chan<- k6metrics.SampleContainer
		tagsAndMeta           k6metrics.TagsAndMeta
		requestDuration       *k6metrics.Metric
		requestCount          *k6metrics.Metric
		requestErrors         *k6metrics.Metric
		requestErrorsDuration *k6metrics.Metric
	}
)

const (
	requestDurationName       = "mcp_request_duration"
	requestCountName          = "mcp_request_count"
	requestErrorsName         = "mcp_request_errors"
	requestErrorsDurationName = "mcp_request_errors_duration"
)

func NewK6Metrics(registry *k6metrics.Registry, samples chan<- k6metrics.SampleContainer, tagsAndMeta k6metrics.TagsAndMeta) *K6Metrics {
	return &K6Metrics{
		samples:               samples,
		tagsAndMeta:           tagsAndMeta,
		requestDuration:       registry.MustNewMetric(requestDurationName, k6metrics.Trend, k6metrics.Time),
		requestCount:          registry.MustNewMetric(requestCountName, k6metrics.Counter),
		requestErrors:         registry.MustNewMetric(requestErrorsName, k6metrics.Counter),
		requestErrorsDuration: registry.MustNewMetric(requestErrorsDurationName, k6metrics.Trend, k6metrics.Time),
	}
}

func (k *K6Metrics) Push(ctx context.Context, method string, duration time.Duration, err error) {
	tags := k.tagsAndMeta.Tags.With(
		"method", method,
	)
	k6metrics.PushIfNotDone(ctx, k.samples, k6metrics.Sample{
		TimeSeries: k6metrics.TimeSeries{
			Metric: k.requestDuration,
			Tags:   tags,
		},
		Time:  time.Now(),
		Value: float64(duration) / float64(time.Millisecond),
	})

	k6metrics.PushIfNotDone(ctx, k.samples, k6metrics.Sample{
		TimeSeries: k6metrics.TimeSeries{
			Metric: k.requestCount,
			Tags:   tags,
		},
		Time:  time.Now(),
		Value: 1,
	})

	if err != nil {
		k6metrics.PushIfNotDone(ctx, k.samples, k6metrics.Sample{
			TimeSeries: k6metrics.TimeSeries{
				Metric: k.requestErrors,
				Tags:   tags,
			},
			Time:  time.Now(),
			Value: 1,
		})

		k6metrics.PushIfNotDone(ctx, k.samples, k6metrics.Sample{
			TimeSeries: k6metrics.TimeSeries{
				Metric: k.requestErrorsDuration,
				Tags:   tags,
			},
			Time:  time.Now(),
			Value: float64(duration) / float64(time.Millisecond),
		})
	}
}
