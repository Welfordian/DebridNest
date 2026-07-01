// rd-proxy forwards Real-Debrid REST requests to a local DebridNest instance,
// injecting Authorization from DEBRIDNEST_API_TOKEN. Alternative to deploy/rd-proxy.conf.
package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

func main() {
	upstream := envOr("DEBRIDNEST_UPSTREAM", "http://debridnest:8080")
	token := os.Getenv("DEBRIDNEST_API_TOKEN")
	listen := envOr("LISTEN", ":8888")

	target, err := url.Parse(strings.TrimRight(upstream, "/"))
	if err != nil {
		log.Fatalf("invalid DEBRIDNEST_UPSTREAM: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = target.Host
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			r.URL.Path = "/healthz"
			proxy.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/rest/1.0/") {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})

	log.Printf("rd-proxy listening on %s -> %s/rest/1.0/", listen, upstream)
	log.Fatal(http.ListenAndServe(listen, mux))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
