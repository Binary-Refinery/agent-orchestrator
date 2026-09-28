package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestListenerCallback(t *testing.T) {
	for _, outcome := range []string{"success", "denied", "cancelled", "delivery error"} {
		t.Run(outcome, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			redirect := "http://" + listener.Addr().String() + "/callback"
			authorization := "https://provider.example/authorize?" + url.Values{"redirect_uri": {redirect}, "state": {"expected-state"}}.Encode()
			client := &http.Client{Timeout: time.Second}
			request := func(method, path, query, host string, want int) {
				t.Helper()
				req, err := http.NewRequestWithContext(ctx, method, redirect+path+"?"+query, nil)
				if err != nil {
					t.Fatal(err)
				}
				if host != "" {
					req.Host = host
				}
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil || response.StatusCode != want {
					t.Fatalf("callback: status=%d want=%d err=%v", response.StatusCode, want, err)
				}
				if strings.Contains(string(body), "secret-marker") || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Referrer-Policy") != "no-referrer" {
					t.Fatal("unsafe callback response")
				}
			}
			opts := &LoginOptions{CallbackListener: listener, AuthorizationURL: func(_ context.Context, got string) error {
				if got != authorization {
					t.Fatal("authorization URL changed")
				}
				if outcome == "cancelled" {
					cancel()
					return nil
				}
				if outcome == "delivery error" {
					return errors.New("secret-marker")
				}
				for _, test := range []struct {
					method, path, query, host string
					status                    int
				}{
					{http.MethodPost, "", "state=expected-state&code=secret-marker", "", http.StatusMethodNotAllowed},
					{http.MethodGet, "/wrong", "state=expected-state&code=secret-marker", "", http.StatusNotFound},
					{http.MethodGet, "", "state=wrong&code=secret-marker", "", http.StatusBadRequest},
					{http.MethodGet, "", "state=expected-state&code=secret-marker", "external.example", http.StatusBadRequest},
					{http.MethodGet, "", "state=expected-state&code=secret-marker&code=other", "", http.StatusBadRequest},
					{http.MethodGet, "", "state=expected-state&error=secret-marker&code=other", "", http.StatusBadRequest},
					{http.MethodGet, "", "state=expected-state", "", http.StatusBadRequest},
					{http.MethodGet, "", "state=expected-state&code=" + strings.Repeat("x", 17000), "", http.StatusBadRequest},
				} {
					request(test.method, test.path, test.query, test.host, test.status)
				}
				query := "state=expected-state&code=secret-marker"
				if outcome == "denied" {
					query = "state=expected-state&error=secret-marker"
				}
				request(http.MethodGet, "", query, "", http.StatusOK)
				request(http.MethodGet, "", query, "", http.StatusGone)
				return nil
			}}
			code, err := waitForListenerCallback(ctx, opts, authorization, "expected-state")
			if outcome == "success" {
				if err != nil || code != "secret-marker" {
					t.Fatal("valid callback was not delivered")
				}
			} else if err == nil || code != "" || strings.Contains(err.Error(), "secret-marker") {
				t.Fatal("callback failure leaked or returned a credential")
			}
			if outcome == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation = %v", err)
			}
			if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
				_ = conn.Close()
				t.Fatal("listener remains open")
			}
		})
	}
}

func TestListenerCallbackRejectsUnsafeListener(t *testing.T) {
	for _, address := range []string{"0.0.0.0:0", "127.0.0.1:0"} {
		t.Run(address, func(t *testing.T) {
			listener, err := net.Listen("tcp4", address)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			opts := &LoginOptions{CallbackListener: listener, AuthorizationURL: func(context.Context, string) error { called = true; return nil }}
			_, err = waitForListenerCallback(t.Context(), opts, "https://provider.example/authorize?redirect_uri=http://localhost:1/callback", "state")
			if err == nil || called {
				t.Fatal("unsafe listener or mismatched redirect admitted")
			}
			if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
				_ = conn.Close()
				t.Fatal("rejected listener remains open")
			}
		})
	}
}

func TestAuthenticatorListener(t *testing.T) {
	for _, authenticator := range []Authenticator{NewCodexAuthenticator(), NewClaudeAuthenticator()} {
		t.Run(authenticator.Provider(), func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			opts := &LoginOptions{CallbackListener: listener, NoBrowser: true, AuthorizationURL: func(context.Context, string) error { return fmt.Errorf("must reject mismatched redirect") }}
			auth, err := authenticator.Login(t.Context(), nil, opts)
			if auth != nil || err == nil {
				t.Fatal("missing configuration admitted")
			}
			if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
				_ = conn.Close()
				t.Fatal("failed authenticator retained listener")
			}
			listener, err = net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			opts.CallbackListener = listener
			auth, err = authenticator.Login(t.Context(), &config.Config{}, opts)
			if auth != nil || err == nil || !strings.Contains(err.Error(), "does not match redirect") {
				t.Fatalf("redirect validation = %v", err)
			}
			if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
				_ = conn.Close()
				t.Fatal("rejected redirect retained listener")
			}
		})
	}
}

func TestListenerCallbackAsyncResponse(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	redirect := "http://" + listener.Addr().String() + "/callback"
	authorization := "https://provider.example/authorize?" + url.Values{"redirect_uri": {redirect}}.Encode()
	responseDone := make(chan error, 1)
	// A browser's response reader outlives the URL-delivery callback context.
	browserContext := t.Context()
	opts := &LoginOptions{CallbackListener: listener, AuthorizationURL: func(_ context.Context, _ string) error {
		go func() {
			req, _ := http.NewRequestWithContext(browserContext, http.MethodGet, redirect+"?state=expected&code=secret-marker", nil)
			response, err := (&http.Client{Timeout: time.Second}).Do(req)
			if err == nil {
				_, err = io.ReadAll(response.Body)
				_ = response.Body.Close()
				if response.StatusCode != http.StatusOK {
					err = errors.New("callback response rejected")
				}
			}
			responseDone <- err
		}()
		return nil
	}}
	if code, err := waitForListenerCallback(t.Context(), opts, authorization, "expected"); err != nil || code != "secret-marker" {
		t.Fatal("asynchronous callback not delivered")
	}
	if err := <-responseDone; err != nil {
		t.Fatalf("callback response interrupted: %v", err)
	}
}

func TestListenerCallbackCancelledBeforeDelivery(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	authorization := "https://provider.example/authorize?" + url.Values{"redirect_uri": {"http://" + listener.Addr().String() + "/callback"}}.Encode()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opts := &LoginOptions{CallbackListener: listener, AuthorizationURL: func(context.Context, string) error {
		t.Fatal("cancelled login delivered an authorization URL")
		return nil
	}}
	if _, err := waitForListenerCallback(ctx, opts, authorization, "state"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled login = %v", err)
	}
	if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("cancelled login retained listener")
	}
}
