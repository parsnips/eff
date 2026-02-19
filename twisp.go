package eff

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TwispContainer wraps a testcontainers container running the Twisp local image.
type TwispContainer struct {
	testcontainers.Container
	GraphQLEndpoint string
	KeepAlive       bool
}

// Cleanup terminates the container unless KeepAlive is set.
// Intended for use with defer.
func (tc *TwispContainer) Cleanup(ctx context.Context, tb testing.TB) {
	if tc.KeepAlive {
		return
	}
	if err := tc.Terminate(ctx); err != nil {
		tb.Logf("terminate container: %v", err)
	}
}

// TwispOption configures StartTwisp.
type TwispOption func(*twispConfig)

type twispConfig struct {
	tb        testing.TB
	keepAlive bool
}

// WithTestLogger forwards container logs to the test output.
func WithTestLogger(tb testing.TB) TwispOption {
	return func(c *twispConfig) { c.tb = tb }
}

// WithKeepAlive prevents the container from being terminated on Cleanup.
func WithKeepAlive() TwispOption {
	return func(c *twispConfig) { c.keepAlive = true }
}

// StartTwisp launches the Twisp local container and waits for the healthcheck.
func StartTwisp(ctx context.Context, opts ...TwispOption) (*TwispContainer, error) {
	var cfg twispConfig
	for _, o := range opts {
		o(&cfg)
	}

	var logConsumers []testcontainers.LogConsumer
	if cfg.tb != nil {
		logConsumers = append(logConsumers, &testLogConsumer{tb: cfg.tb})
	}

	req := testcontainers.ContainerRequest{
		Image:        "public.ecr.aws/twisp/local:latest",
		ExposedPorts: []string{"3000/tcp", "8080/tcp", "8081/tcp"},
		WaitingFor: wait.ForHTTP("/healthcheck").
			WithPort("8080/tcp").
			WithStartupTimeout(120 * time.Second),
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Consumers: logConsumers,
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("starting twisp container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "8080/tcp")
	if err != nil {
		return nil, fmt.Errorf("getting mapped port: %w", err)
	}

	endpoint := fmt.Sprintf("http://%s:%s/financial/v1/graphql", host, port.Port())

	return &TwispContainer{
		Container:       container,
		GraphQLEndpoint: endpoint,
		KeepAlive:       cfg.keepAlive,
	}, nil
}

// NewGraphQLClient creates a genqlient GraphQL client pointing at this container.
// Any provided headers are sent with every request. Transient connection errors
// are retried automatically.
func (tc *TwispContainer) NewGraphQLClient(headers http.Header) graphql.Client {
	httpClient := &http.Client{
		Transport: &retryTransport{
			base: &headerTransport{
				base:    http.DefaultTransport,
				headers: headers,
			},
			maxRetries: 5,
			baseDelay:  200 * time.Millisecond,
		},
	}
	return graphql.NewClient(tc.GraphQLEndpoint, httpClient)
}

type headerTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, vals := range t.headers {
		for _, v := range vals {
			req.Header.Add(key, v)
		}
	}
	return t.base.RoundTrip(req)
}

// retryTransport retries requests on transient connection errors (ECONNREFUSED, ECONNRESET).
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := range t.maxRetries {
		// Clone the request body for retries.
		cloned := req.Clone(req.Context())
		if req.Body != nil && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			cloned.Body = body
		}

		resp, err := t.base.RoundTrip(cloned)
		if err == nil {
			return resp, nil
		}

		if !isTransient(err) {
			return nil, err
		}
		lastErr = err

		delay := t.baseDelay * (1 << attempt)
		select {
		case <-time.After(delay):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	return nil, lastErr
}

func isTransient(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) && netErr.Op == "dial" {
		return true
	}
	return false
}

// testLogConsumer forwards container logs to testing.TB.
type testLogConsumer struct {
	tb testing.TB
}

func (c *testLogConsumer) Accept(l testcontainers.Log) {
	c.tb.Logf("[twisp] %s", strings.TrimRight(string(l.Content), "\n"))
}
