package mcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
	"golang.org/x/oauth2"
)

func init() {
	modules.Register("k6/x/mcp", New())
}

// MCP is the root module struct
type (
	RootModule struct{}

	// ClientConfig represents the configuration for the MCP client
	ClientConfig struct {
		// Stdio
		Path  string
		Args  []string
		Env   map[string]string
		Debug bool

		// SSE and Streamable HTTP
		BaseURL string
		Auth    AuthConfig
	}

	AuthConfig struct {
		BearerToken string
	}
)

	requestDurationMetricName = "mcp_request_duration"
	requestCountMetricName    = "mcp_request_count"
	requestErrorsMetricName   = "mcp_request_errors"

// New returns a pointer to a new RootModule instance.
func New() *RootModule {
	return &RootModule{}
}

var (
	_ modules.Instance = &MCPInstance{}
	_ modules.Module   = &RootModule{}
)

// NewModuleInstance initializes a new module instance
func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	env := vu.InitEnv()

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

// MCPInstance represents an instance of the MCP module
type MCPInstance struct {
	vu     modules.VU
	logger logrus.FieldLogger
}

type mcpMetrics struct {
	RequestDuration *metrics.Metric
	RequestCount    *metrics.Metric
	RequestErrors   *metrics.Metric

	TagsAndMeta *metrics.TagsAndMeta
}

// Client wraps an MCP client session
type Client struct {
	session *mcp.ClientSession

	k6_state *lib.State
}

// Exports defines the JavaScript-accessible functions
func (m *MCPInstance) Exports() modules.Exports {
	return modules.Exports{
		Named: map[string]interface{}{
			"StdioClient":          m.newStdioClient,
			"SSEClient":            m.newSSEClient,
			"StreamableHTTPClient": m.newStreamableHTTPClient,
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

func (m *MCPInstance) newStdioClient(c sobek.ConstructorCall, rt *sobek.Runtime) *sobek.Object {
	var cfg ClientConfig
	if err := rt.ExportTo(c.Argument(0), &cfg); err != nil {
		common.Throw(rt, fmt.Errorf("invalid config: %w", err))
	}

	cmd := exec.Command(cfg.Path, cfg.Args...)
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	if cfg.Debug {
		cmd.Stderr = os.Stderr
	}

	transport := &mcp.CommandTransport{
		Command: cmd,
	}

	clientObj := m.connect(rt, transport, false)
	var client *Client
	if err := rt.ExportTo(clientObj, &client); err != nil {
		common.Throw(rt, fmt.Errorf("failed to extract Client: %w", err))
	}

	return rt.ToValue(&Client{
		session:  client.session,
		k6_state: m.vu.State(),
	}).ToObject(rt)
}

func (m *MCP) getTracer() trace.Tracer {
	if m.tracer != nil {
		return m.tracer
	}

	transport := &mcp.SSEClientTransport{
		Endpoint:   cfg.BaseURL,
		HTTPClient: m.newk6HTTPClient(cfg),
	}

	clientObj := m.connect(rt, transport, true)
	var client *Client
	if err := rt.ExportTo(clientObj, &client); err != nil {
		common.Throw(rt, fmt.Errorf("failed to extract Client: %w", err))
	}

	return m.tracer
}

func (m *MCPInstance) newStreamableHTTPClient(c sobek.ConstructorCall, rt *sobek.Runtime) *sobek.Object {
	var cfg ClientConfig
	if err := rt.ExportTo(c.Argument(0), &cfg); err != nil {
		common.Throw(rt, fmt.Errorf("invalid config: %w", err))
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   cfg.BaseURL,
		HTTPClient: m.newk6HTTPClient(cfg),
	}

	clientObj := m.connect(rt, transport, false)
	var client *Client
	if err := rt.ExportTo(clientObj, &client); err != nil {
		common.Throw(rt, fmt.Errorf("failed to extract Client: %w", err))
	}

	return rt.ToValue(&Client{
		session:  client.session,
		k6_state: m.vu.State(),
	}).ToObject(rt)
}

func (m *MCPInstance) newk6HTTPClient(cfg ClientConfig) *http.Client {
	var tlsConfig *tls.Config
	if m.vu.State() != nil && m.vu.State().TLSConfig != nil {
		tlsConfig = m.vu.State().TLSConfig.Clone()
		tlsConfig.NextProtos = []string{"http/1.1"}
	}

	transport := http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: tlsConfig,
	}
	if m.vu.State() != nil {
		transport.DisableKeepAlives = m.vu.State().Options.NoConnectionReuse.ValueOrZero() || m.vu.State().Options.NoVUConnectionReuse.ValueOrZero()
		transport.DialContext = m.vu.State().Dialer.DialContext
	}

	httpClient := &http.Client{
		Transport: &transport,
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

	return httpClient
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

	client := mcp.NewClient(&mcp.Implementation{Name: "k6", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		common.Throw(rt, fmt.Errorf("connection error: %w", err))
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
