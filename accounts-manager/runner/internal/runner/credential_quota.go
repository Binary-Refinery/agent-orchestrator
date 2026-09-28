package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type credentialQuota struct {
	ObservedAt         time.Time          `json:"observedAt"`
	Subscription       *quotaSubscription `json:"subscription,omitempty"`
	Summary            []any              `json:"summary"`
	ServerTimeOffsetMS int64              `json:"serverTimeOffsetMs"`
	Groups             []quotaGroup       `json:"groups"`
}

type quotaSubscription struct {
	Plan string `json:"plan"`
}
type quotaGroup struct {
	DisplayName string        `json:"displayName"`
	Buckets     []quotaBucket `json:"buckets"`
}
type quotaBucket struct {
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
	Description       string  `json:"description"`
}

type credentialQuotaFlight struct {
	done        chan struct{}
	fingerprint string
	until       time.Time
	result      credentialQuota
	err         error
}

func supportsCredentialQuota(auth *coreauth.Auth) bool {
	token, _ := auth.Metadata["access_token"].(string)
	return validVaultProvider(auth.Provider) && auth.Attributes["api_key"] == "" && token != ""
}

func (r *credentialRuntime) quota(ctx context.Context, auth *coreauth.Auth) (credentialQuota, error) {
	if !supportsCredentialQuota(auth) || !r.vault.admitVerified(ctx, auth) {
		return credentialQuota{}, errCredentialFenced
	}
	fingerprint, err := credentialFingerprint(auth)
	if err != nil {
		return credentialQuota{}, err
	}
	r.quotaMu.Lock()
	if r.quotas == nil {
		r.quotas = make(map[string]*credentialQuotaFlight)
	}
	if flight := r.quotas[auth.ID]; flight != nil && flight.fingerprint == fingerprint && time.Now().Before(flight.until) {
		r.quotaMu.Unlock()
		select {
		case <-ctx.Done():
			return credentialQuota{}, ctx.Err()
		case <-flight.done:
		}
		if !r.vault.admitVerified(ctx, auth) {
			return credentialQuota{}, errCredentialFenced
		}
		return flight.result, flight.err
	}
	flight := &credentialQuotaFlight{done: make(chan struct{}), fingerprint: fingerprint, until: time.Now().Add(30 * time.Second)}
	r.quotas[auth.ID] = flight
	r.quotaMu.Unlock()
	flight.result, flight.err = r.fetchQuota(ctx, auth)
	if !r.vault.admitVerified(ctx, auth) {
		flight.result, flight.err = credentialQuota{}, errCredentialFenced
	}
	r.quotaMu.Lock()
	if flight.err != nil {
		flight.until = time.Now().Add(time.Second)
	}
	close(flight.done)
	r.quotaMu.Unlock()
	return flight.result, flight.err
}

func (r *credentialRuntime) fetchQuota(ctx context.Context, auth *coreauth.Auth) (credentialQuota, error) {
	token, _ := auth.Metadata["access_token"].(string)
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	endpoint := "https://api.anthropic.com/api/oauth/usage"
	if auth.Provider == "codex" {
		endpoint = "https://chatgpt.com/backend-api/wham/usage"
		if account, _ := auth.Metadata["account_id"].(string); account != "" {
			if !validIdentityAtom(account) {
				return credentialQuota{}, errCredentialConflict
			}
			headers.Set("ChatGPT-Account-Id", account)
		}
	} else {
		headers.Set("anthropic-beta", "oauth-2025-04-20")
	}
	body, err := r.credentialCheck(ctx, endpoint, headers)
	if err != nil {
		return credentialQuota{}, err
	}
	defer clear(body)
	return parseCredentialQuota(auth.Provider, body, time.Now().UTC())
}

func parseCredentialQuota(provider string, data []byte, observed time.Time) (credentialQuota, error) {
	result := credentialQuota{ObservedAt: observed, Summary: []any{}, Groups: []quotaGroup{{DisplayName: "Account", Buckets: []quotaBucket{}}}}
	add := func(window string, used *float64, reset string) bool {
		if used == nil || *used < 0 || *used > 100 {
			return false
		}
		if reset != "" {
			if _, err := time.Parse(time.RFC3339, reset); err != nil {
				return false
			}
		}
		result.Groups[0].Buckets = append(result.Groups[0].Buckets, quotaBucket{Window: window, RemainingFraction: 1 - *used/100, ResetTime: reset})
		return true
	}
	if provider == "codex" {
		type window struct {
			Used    *float64 `json:"used_percent"`
			Seconds *int64   `json:"limit_window_seconds"`
			Reset   *int64   `json:"reset_at"`
		}
		var raw struct {
			Plan  string `json:"plan_type"`
			Limit *struct {
				Primary   *window `json:"primary_window"`
				Secondary *window `json:"secondary_window"`
			} `json:"rate_limit"`
		}
		if json.Unmarshal(data, &raw) != nil || raw.Limit == nil {
			return credentialQuota{}, errCredentialQuotaResponse
		}
		for index, value := range []*window{raw.Limit.Primary, raw.Limit.Secondary} {
			if value == nil {
				continue
			}
			name := []string{"primary", "secondary"}[index]
			if value.Seconds != nil {
				if *value.Seconds <= 0 {
					return credentialQuota{}, errCredentialQuotaResponse
				}
				name = strconv.FormatInt(*value.Seconds, 10) + "s"
			}
			reset := ""
			if value.Reset != nil {
				if *value.Reset < 0 || *value.Reset > 253402300799 {
					return credentialQuota{}, errCredentialQuotaResponse
				}
				reset = time.Unix(*value.Reset, 0).UTC().Format(time.RFC3339)
			}
			if !add(name, value.Used, reset) {
				return credentialQuota{}, errCredentialQuotaResponse
			}
		}
		switch raw.Plan {
		case "free", "plus", "pro", "team", "business", "enterprise", "edu":
			result.Subscription = &quotaSubscription{Plan: raw.Plan}
		}
	} else {
		type window struct {
			Used  *float64 `json:"utilization"`
			Reset string   `json:"resets_at"`
		}
		var raw struct {
			FiveHour *window `json:"five_hour"`
			SevenDay *window `json:"seven_day"`
			Sonnet   *window `json:"seven_day_sonnet"`
			Opus     *window `json:"seven_day_opus"`
		}
		if json.Unmarshal(data, &raw) != nil {
			return credentialQuota{}, errCredentialQuotaResponse
		}
		for index, value := range []*window{raw.FiveHour, raw.SevenDay, raw.Sonnet, raw.Opus} {
			name := []string{"five_hour", "seven_day", "seven_day_sonnet", "seven_day_opus"}[index]
			if value != nil && !add(name, value.Used, value.Reset) {
				return credentialQuota{}, errCredentialQuotaResponse
			}
		}
	}
	if len(result.Groups[0].Buckets) == 0 {
		return credentialQuota{}, errCredentialQuotaResponse
	}
	return result, nil
}
