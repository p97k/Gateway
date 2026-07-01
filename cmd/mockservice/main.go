// Command mockservice is a tiny configurable backend used to demonstrate
// gateway routing. The same binary runs as product-service, order-service and
// user-service — distinguished by the SERVICE_NAME and PORT environment
// variables — so docker-compose can stand up three distinct upstreams without
// three separate codebases.
//
// Each instance exposes:
//   - GET /            a JSON greeting that echoes the gateway-injected headers
//     (X-Request-Id, X-User-Id, X-User-Role) so request-context
//     propagation is observable end-to-end.
//   - GET /{anything}  a generic resource response (demonstrates path forwarding)
//   - GET /health      a health probe used by the gateway's checker
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	name := envOr("SERVICE_NAME", "mock-service")
	addr := ":" + envOr("PORT", "8081")

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": name,
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service": name,
			"message": "hello from " + name,
			"method":  r.Method,
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
			// Echo back what the gateway injected so propagation is visible.
			"received_headers": map[string]string{
				"X-Request-Id": r.Header.Get("X-Request-Id"),
				"X-User-Id":    r.Header.Get("X-User-Id"),
				"X-User-Role":  r.Header.Get("X-User-Role"),
				"traceparent":  r.Header.Get("traceparent"),
			},
			"time": time.Now().UTC().Format(time.RFC3339),
		})
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           logRequests(name, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("%s listening on %s", name, addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("%s: %v", name, err)
	}
}

func logRequests(name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[%s] %s %s reqid=%s user=%s", name, r.Method, r.URL.Path,
			r.Header.Get("X-Request-Id"), r.Header.Get("X-User-Id"))
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
