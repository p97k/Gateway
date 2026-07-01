package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nbe-group/apigateway/internal/config"
	"github.com/nbe-group/apigateway/internal/proxy"
	"github.com/nbe-group/apigateway/internal/router"
	"github.com/nbe-group/apigateway/internal/service_registry"
	"github.com/nbe-group/apigateway/internal/transport"
)

func init() { gin.SetMode(gin.TestMode) }

// received records what the upstream actually got.
type received struct {
	method  string
	path    string
	query   string
	headers http.Header
}

// newGateway returns a live gateway server (real ResponseWriter, so the reverse
// proxy behaves exactly as in production) wired to the given routes/services.
func newGateway(t *testing.T, routes []config.RouteConfig, services map[string]string, seed func(*gin.Context)) *httptest.Server {
	t.Helper()
	reg, err := service_registry.NewStatic(services)
	require.NoError(t, err)
	p := proxy.New(reg, transport.New(transport.DefaultOptions()), nil, nil)

	matcher := router.NewMatcher(routes)
	r := gin.New()
	chain := []gin.HandlerFunc{matcher.Resolver()}
	if seed != nil {
		chain = append(chain, func(c *gin.Context) { seed(c); c.Next() })
	}
	chain = append(chain, p.Handler())
	r.NoRoute(chain...)

	gw := httptest.NewServer(r)
	t.Cleanup(gw.Close)
	return gw
}

func TestProxy_ForwardsRequestFaithfully(t *testing.T) {
	var got received
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		got = received{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, headers: r.Header}
		w.Header().Set("X-Backend", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	gw := newGateway(t,
		[]config.RouteConfig{{Prefix: "/api/products", Service: "product"}},
		map[string]string{"product": backend.URL},
		func(c *gin.Context) { transport.SetRequestID(c, "rid-1") },
	)

	resp, err := http.Get(gw.URL + "/api/products/42?expand=true")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "yes", resp.Header.Get("X-Backend"))
	assert.JSONEq(t, `{"ok":true}`, string(body))

	assert.Equal(t, http.MethodGet, got.method)
	assert.Equal(t, "/api/products/42", got.path)
	assert.Equal(t, "expand=true", got.query)
	assert.Equal(t, "rid-1", got.headers.Get(transport.HeaderRequestID))
}

func TestProxy_StripPrefix(t *testing.T) {
	var gotPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	gw := newGateway(t,
		[]config.RouteConfig{{Prefix: "/api/products", Service: "product", StripPrefix: true}},
		map[string]string{"product": backend.URL},
		nil,
	)

	resp, err := http.Get(gw.URL + "/api/products/42")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "/42", gotPath, "configured prefix should be stripped before forwarding")
}

func TestProxy_UpstreamDown(t *testing.T) {
	gw := newGateway(t,
		[]config.RouteConfig{{Prefix: "/api/products", Service: "product"}},
		map[string]string{"product": "http://127.0.0.1:1"},
		nil,
	)
	resp, err := http.Get(gw.URL + "/api/products")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Contains(t, string(body), "PRX_001")
}
