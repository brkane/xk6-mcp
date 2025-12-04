package metrics_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	mcp "github.com/grafana/xk6-mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/modulestest"
	k6lib "go.k6.io/k6/lib"
	k6metrics "go.k6.io/k6/metrics"
)

type (
	testCase struct {
		runtime *modulestest.Runtime
		samples chan k6metrics.SampleContainer
	}

	MyToolInput struct {
		Id int `json:"id"`
	}

	MyToolOutput struct {
		Output string `json:"output"`
	}
)

const (
	toolName string = "myTool"
)

// TODO: Refactor this out to common testing library
func setupTest(t *testing.T) *testCase {
	t.Helper()

	registry := k6metrics.NewRegistry()
	samples := make(chan k6metrics.SampleContainer, 1000)
	state := &k6lib.State{
		Samples: samples,
		Tags: k6lib.NewVUStateTags(registry.RootTagSet().WithTagsFromMap(map[string]string{
			"group": k6lib.RootGroupPath,
		})),
		Transport: http.DefaultTransport,
	}

	rt := modulestest.NewRuntime(t)
	vu := rt.VU

	mod, ok := mcp.New().NewModuleInstance(vu).(*mcp.MCPInstance)
	require.True(t, ok)
	require.NoError(t, vu.RuntimeField.Set("mcp", mod.Exports().Named))

	rt.MoveToVUContext(state)

	return &testCase{
		runtime: rt,
		samples: samples,
	}
}

// TODO: Refactor this out to common testing library
func streamableHandler(t *testing.T) (*mcpsdk.StreamableHTTPHandler, error) {
	t.Helper()

	inputSchema, err := jsonschema.For[MyToolInput](nil)
	if err != nil {
		return nil, err
	}
	toolHandler := func(context.Context, *mcpsdk.CallToolRequest, MyToolInput) (*mcpsdk.CallToolResult, any, error) {
		return nil, MyToolOutput{toolName}, nil
	}

	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test", Version: "1.0.0"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: toolName, InputSchema: inputSchema}, toolHandler)
	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return server
	},
		&mcpsdk.StreamableHTTPOptions{
			Stateless: true,
		},
	), nil
}

func TestK6Metrics(t *testing.T) {
	handler, err := streamableHandler(t)
	assert.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer ts.Close()

	tc := setupTest(t)

	_, err = tc.runtime.VU.Runtime().RunString(
		fmt.Sprintf(`const client = mcp.StreamableHTTPClient({
      base_url: "%s"
    });
    const tools = client.callTool({name: "%s", arguments: {id: 1}});`, ts.URL, toolName),
	)

	assert.NoError(t, err)

	var sampleCount int
	sampleContainers := k6metrics.GetBufferedSamples(tc.samples)
	assert.Greater(t, len(sampleContainers), 0)
	for _, sampleContainer := range sampleContainers {
		sampleCount += len(sampleContainer.GetSamples())
	}
	assert.Equal(t, sampleCount, 2)
}

func TestK6ErrorMetrics(t *testing.T) {
	handler, err := streamableHandler(t)
	assert.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer ts.Close()

	tc := setupTest(t)

	tc.runtime.VU.Runtime().RunString(
		fmt.Sprintf(`const client = mcp.StreamableHTTPClient({
      base_url: "%s"
    });
    const tools = client.callTool({name: "%s", arguments: {id: 1}});`, ts.URL, toolName+"bad"),
	)

	var sampleCount int
	sampleContainers := k6metrics.GetBufferedSamples(tc.samples)
	assert.Greater(t, len(sampleContainers), 0)
	for _, sampleContainer := range sampleContainers {
		sampleCount += len(sampleContainer.GetSamples())
	}
	assert.Equal(t, sampleCount, 3)
}
