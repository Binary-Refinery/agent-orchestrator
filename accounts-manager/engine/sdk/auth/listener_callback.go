package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

func waitForListenerCallback(ctx context.Context, opts *LoginOptions, authorization, state string) (string, error) {
	listener := opts.CallbackListener
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !address.IP.IsLoopback() || opts.AuthorizationURL == nil || state == "" {
		return "", errors.New("invalid callback listener options")
	}
	authURL, err := url.Parse(authorization)
	if err != nil || authURL.Scheme != "https" || authURL.Host == "" || authURL.User != nil {
		return "", errors.New("invalid authorization URL")
	}
	redirect, err := url.Parse(authURL.Query().Get("redirect_uri"))
	if err != nil || redirect.Scheme != "http" || redirect.User != nil || redirect.RawQuery != "" || redirect.Fragment != "" || redirect.Path == "" || redirect.Port() != strconv.Itoa(address.Port) {
		return "", errors.New("callback listener does not match redirect")
	}
	host := redirect.Hostname()
	if (host == "localhost" && !address.IP.Equal(net.ParseIP("127.0.0.1")) && !address.IP.Equal(net.IPv6loopback)) || (host != "localhost" && !net.ParseIP(host).Equal(address.IP)) {
		return "", errors.New("callback listener does not match redirect host")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type callback struct {
		code   string
		denied bool
	}
	result := make(chan callback, 1)
	var mu sync.Mutex
	consumed := false
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		MaxHeaderBytes: 32 << 10, ErrorLog: log.New(io.Discard, "", 0),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path != redirect.Path {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if (r.Host != redirect.Host && r.Host != address.String()) || len(r.URL.RawQuery) > 16<<10 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			query, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil || len(query["state"]) != 1 || subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state)) != 1 || len(query["code"]) > 1 || len(query["error"]) > 1 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			code, denial := query.Get("code"), query.Get("error")
			if (code == "") == (denial == "") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if consumed || ctx.Err() != nil {
				w.WriteHeader(http.StatusGone)
				return
			}
			consumed = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "Return to the application to check sign-in status.")
			result <- callback{code: code, denied: denial != ""}
		}),
	}
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	serveDone := make(chan struct{})
	go func() { defer close(serveDone); _ = server.Serve(listener) }()
	if err := opts.AuthorizationURL(ctx, authorization); err != nil {
		return "", errors.New("authorization URL delivery failed")
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-serveDone:
		return "", errors.New("callback listener stopped")
	case received := <-result:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if received.denied {
			return "", errors.New("authorization denied")
		}
		return received.code, nil
	}
}
