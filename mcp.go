package mcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"
)

func init() {
	modules.Register("k6/x/mcp", New())
}

const (
	tracerScope       = "github.com/grafana/xk6-mcp"
	connectSpanName   = "Connect"
	listToolsSpanName = "ListTools"
	callToolSpanName  = "CallTool"

	requestDurationMetricName = "mcp_request_duration"
	requestCountMetricName    = "mcp_request_count"
	requestErrorsMetricName   = "mcp_request_errors"

	connectMethodName   = "connect"
	listToolsMethodName = "tools/list"
	callToolMethodName  = "tools/call"
)

type (
	// MCP is the instance of the JS module
	MCP struct {
		vu      modules.VU
		session *mcp.ClientSession
		tracer  trace.Tracer
		metrics *mcpMetrics
	}

	// RootModule is the global module instance that will create module instances for each VU.
	RootModule struct{}

	Module struct {
		*MCP
		exports modules.Exports
	}

	AuthConfig struct {
		BearerToken string
	}

	Config struct {
		BaseURL string
		//Timeout        time.Duration
		Auth           AuthConfig
		TracingEnabled bool
	}

	mcpMetrics struct {
		RequestDuration *metrics.Metric
		RequestCount    *metrics.Metric
		RequestErrors   *metrics.Metric

		TagsAndMeta *metrics.TagsAndMeta
	}
)

var (
	_ modules.Instance = &Module{}
	_ modules.Module   = &RootModule{}
)

// New returns a pointer to a new RootModule instance.
func New() *RootModule {
	return &RootModule{}
}

func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	moduleInstance := &Module{
		MCP: &MCP{
			vu: vu,
		},
	}
	env := vu.InitEnv()

	//moduleInstance.exports.Default = moduleInstance
	moduleInstance.exports.Named = map[string]interface{}{
		"Connect":   moduleInstance.Connect,
		"ListTools": moduleInstance.ListTools,
		"CallTool":  moduleInstance.CallTool,
	}

	// Initialize metrics
	moduleInstance.metrics = &mcpMetrics{
		RequestDuration: env.Registry.MustNewMetric(requestDurationMetricName, metrics.Trend, metrics.Time),
		RequestCount:    env.Registry.MustNewMetric(requestCountMetricName, metrics.Counter),
		RequestErrors:   env.Registry.MustNewMetric(requestErrorsMetricName, metrics.Counter),
		TagsAndMeta: &metrics.TagsAndMeta{
			Tags: env.Registry.RootTagSet(),
		},
	}

	return moduleInstance
}

func (m *Module) Exports() modules.Exports {
	return m.exports
}

func (m *MCP) getTracer() trace.Tracer {
	if m.tracer != nil {
		return m.tracer
	}

	// Check if in a running VU, if not use provider from init environment
	if m.vu.State() == nil {
		m.tracer = m.vu.InitEnv().TracerProvider.Tracer(tracerScope)
	} else {
		m.tracer = m.vu.State().TracerProvider.Tracer(tracerScope)
	}

	return m.tracer
}

func (m *MCP) getContext() context.Context {
	// Since we are holding the connection open across VU iterations
	// we can't use the VU context
	return context.Background()
}

func (m *MCP) getK6Transport() http.RoundTripper {
	// Check if we are in a VU context
	if m.vu.State() != nil {
		return m.vu.State().Transport
	}

	// TODO: Customize this to the environment as much as possible
	return http.DefaultClient.Transport
}

func (m *Module) Connect(cfg Config) error {
	// Check if we are already connected
	if m.session != nil {
		return nil
	}

	baseTransport := otelhttp.NewTransport(m.MCP.getK6Transport())

	httpClient := &http.Client{
		Transport: baseTransport,
	}

	if cfg.Auth.BearerToken != "" {
		ctx := context.Background()

		ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)

		token := oauth2.Token{
			AccessToken: cfg.Auth.BearerToken,
		}
		tokenSource := oauth2.StaticTokenSource(&token)

		httpClient = oauth2.NewClient(ctx, tokenSource)
	}

	mcpTransport := &mcp.StreamableClientTransport{
		Endpoint:   cfg.BaseURL,
		HTTPClient: httpClient,
	}

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "k6", Version: "1.0.0"}, nil)

	var err error
	var session *mcp.ClientSession
	startTime := time.Now()
	ctx, span := m.MCP.getTracer().Start(m.MCP.getContext(), connectSpanName)
	span.SetAttributes(attribute.KeyValue{
		Key:   "rpc.method",
		Value: attribute.StringValue(connectMethodName),
	})

	session, err = mcpClient.Connect(ctx, mcpTransport, nil)
	duration := time.Since(startTime)
	span.End()
	m.pushRequestMetrics(connectMethodName, duration, err)
	if err != nil {
		return err
	}

	m.session = session

	return nil
}

func (m *Module) ListTools(params mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	if m.session == nil {
		return nil, fmt.Errorf("must call `Connect()` before calling `ListTools()`")
	}

	var err error
	var result *mcp.ListToolsResult
	startTime := time.Now()
	ctx, span := m.MCP.getTracer().Start(m.MCP.getContext(), listToolsSpanName)
	span.SetAttributes(attribute.KeyValue{
		Key:   "rpc.method",
		Value: attribute.StringValue(listToolsMethodName),
	})

	result, err = m.session.ListTools(ctx, &params)
	duration := time.Since(startTime)
	span.End()
	m.pushRequestMetrics(listToolsMethodName, duration, err)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (m *Module) CallTool(params mcp.CallToolParams) (*mcp.CallToolResult, error) {
	if m.session == nil {
		return nil, fmt.Errorf("must call `Connect()` before calling `CallTool()`")
	}

	var err error
	var result *mcp.CallToolResult
	startTime := time.Now()
	ctx, span := m.MCP.getTracer().Start(m.MCP.getContext(), callToolSpanName)
	span.SetAttributes(attribute.KeyValue{
		Key:   "rpc.method",
		Value: attribute.StringValue(callToolMethodName),
	})

	result, err = m.session.CallTool(ctx, &params)
	duration := time.Since(startTime)
	span.End()
	m.pushRequestMetrics(callToolMethodName, duration, err)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (m *Module) pushRequestMetrics(method string, duration time.Duration, err error) {
	// Check if we have a VU state to write metrics to
	if m.vu.State() == nil {
		return
	}

	tags := m.vu.State().Tags.GetCurrentValues().Tags.With(
		"method", method,
	)

	metrics.PushIfNotDone(m.MCP.getContext(), m.vu.State().Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: m.metrics.RequestDuration,
			Tags:   tags,
		},
		Time:  time.Now(),
		Value: float64(duration) / float64(time.Millisecond),
	})

	metrics.PushIfNotDone(m.MCP.getContext(), m.vu.State().Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: m.metrics.RequestCount,
			Tags:   tags,
		},
		Time:  time.Now(),
		Value: 1,
	})

	if err != nil {
		metrics.PushIfNotDone(m.MCP.getContext(), m.vu.State().Samples, metrics.Sample{
			TimeSeries: metrics.TimeSeries{
				Metric: m.metrics.RequestErrors,
				Tags:   tags,
			},
			Time:  time.Now(),
			Value: 1,
		})
	}
}
