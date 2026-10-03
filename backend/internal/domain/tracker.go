package domain

import (
	"fmt"
	"net/url"
	"strings"
)

// TrackerProvider identifies an issue-tracker provider implementation.
type TrackerProvider string

// TrackerProviderGitHub and TrackerProviderGitLab are the supported issue-tracker
// providers.
const (
	TrackerProviderGitHub TrackerProvider = "github"
	TrackerProviderGitLab TrackerProvider = "gitlab"
)

// TrackerID identifies one issue. Native is the provider's own canonical form
// ("owner/repo#123" for GitHub, "group/project#123" for GitLab) and is
// parsed by the adapter.
//
// Host is the GitLab instance host (e.g. "gitlab.example.com"). The zero value
// "" means the default host gitlab.com, so all existing call sites that
// construct TrackerID without setting Host continue to work unchanged.
type TrackerID struct {
	Provider TrackerProvider `json:"provider"`
	Native   string          `json:"native"`
	// Host is the GitLab instance host; "" means gitlab.com.
	Host string `json:"host,omitempty"`
}

// NormalizedIssueState is the cross-provider issue-state vocabulary every
// adapter must implement. The closed list is intentional — adding a value
// here is a port-level decision because every adapter must map it.
type NormalizedIssueState string

// The normalized cross-provider issue states.
const (
	IssueOpen       NormalizedIssueState = "open"
	IssueInProgress NormalizedIssueState = "in_progress"
	IssueInReview   NormalizedIssueState = "review"
	IssueDone       NormalizedIssueState = "done"
	IssueCancelled  NormalizedIssueState = "cancelled"
)

// Issue is the minimum projection every tracker can produce. Provider-specific
// metadata stays inside provider-specific code paths.
type Issue struct {
	ID        TrackerID            `json:"id"`
	Title     string               `json:"title"`
	Body      string               `json:"body"`
	State     NormalizedIssueState `json:"state"`
	URL       string               `json:"url"`
	Labels    []string             `json:"labels,omitempty"`
	Assignees []string             `json:"assignees,omitempty"`
}

// TrackerRepo identifies a repository for cross-issue queries like Tracker.List.
// Native is the provider's canonical owner/project form, e.g. "owner/repo"
// for GitHub or "group/project" for GitLab.
//
// Host is the GitLab instance host (e.g. "gitlab.example.com"). The zero value
// "" means the default host gitlab.com, so all existing call sites that
// construct TrackerRepo without setting Host continue to work unchanged.
type TrackerRepo struct {
	Provider TrackerProvider `json:"provider"`
	Native   string          `json:"native"`
	// Host is the GitLab instance host; "" means gitlab.com.
	Host string `json:"host,omitempty"`
}

// ListStateFilter narrows Tracker.List results by the provider's coarse
// state (open vs closed). It is intentionally NOT the 5-value normalized
// enum — finer filtering (e.g. "only in-review issues") goes through the
// Labels field of ListFilter.
type ListStateFilter string

// Coarse list-state filters for Tracker.List.
const (
	// ListAll is the zero value and returns issues in any state.
	ListAll    ListStateFilter = ""
	ListOpen   ListStateFilter = "open"
	ListClosed ListStateFilter = "closed"
)

// ListFilter is the query the Session Manager passes to Tracker.List.
// Empty / zero values mean "no filter on this dimension".
//
// Limit is an optional total-result cap. Adapters choose their own provider
// page size.
type ListFilter struct {
	State    ListStateFilter `json:"state,omitempty"`
	Labels   []string        `json:"labels,omitempty"`
	Assignee string          `json:"assignee,omitempty"`
	Limit    int             `json:"limit,omitempty"`
}

// TrackerIntakeConfig controls issue-driven worker spawning for a project.
// Enabled requires an explicit assignee eligibility rule so turning intake on
// cannot accidentally drain an entire issue backlog.
type TrackerIntakeConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// Provider defaults to github when Enabled is true. Supported values:
	// "github" and "gitlab".
	Provider TrackerProvider `json:"provider,omitempty" enum:"github,gitlab"`
	// Repo is the provider-native repository key ("owner/repo" for GitHub,
	// "group/project" for GitLab). When empty, the intake loop derives it from
	// the project's repo origin URL.
	Repo string `json:"repo,omitempty"`
	// Assignee narrows eligible issues to one assignee. Provider-specific values
	// such as "*" are passed through unchanged.
	Assignee string `json:"assignee,omitempty"`
	// AutomationV2 holds the opt-in af-ao-automation-v2 policy. Nil means the
	// v2 policy is off; every v2 rule is fail-closed and default-off.
	AutomationV2 *AutomationV2Config `json:"automationV2,omitempty"`
}

// AutomationV2Config is the opt-in af-ao-automation-v2 policy slice attached
// to tracker intake. All fields are default-off/zero-safe: without Enabled the
// v2 rules never admit a task.
type AutomationV2Config struct {
	// Enabled turns the v2 policy on. Default false.
	Enabled bool `json:"enabled,omitempty"`
	// Label is the required GitHub label event that admits an issue to the AO
	// queue. Empty admits nothing while v2 is enabled (fail-closed).
	Label string `json:"label,omitempty"`
	// MaxParallel caps active takes per project. Values <= 0 resolve to
	// DefaultV2MaxParallel at use time.
	MaxParallel int `json:"maxParallel,omitempty"`
	// ReviewTimeoutSeconds marks a needs_review/CI-failing take as stalled
	// after this many seconds. Values <= 0 resolve to DefaultV2ReviewTimeout.
	ReviewTimeoutSeconds int64 `json:"reviewTimeoutSeconds,omitempty"`
}

// DefaultV2MaxParallel is the v2 WIP-gate default: strictly one active take.
const DefaultV2MaxParallel = 1

// DefaultV2ReviewTimeoutSeconds is the v2 stall default for needs_review/CI-failing takes.
const DefaultV2ReviewTimeoutSeconds = 24 * 60 * 60

// ResolvedMaxParallel returns the effective WIP limit (default 1).
func (c *AutomationV2Config) ResolvedMaxParallel() int {
	if c == nil || c.MaxParallel <= 0 {
		return DefaultV2MaxParallel
	}
	return c.MaxParallel
}

// ResolvedReviewTimeoutSeconds returns the effective stall timeout in seconds.
func (c *AutomationV2Config) ResolvedReviewTimeoutSeconds() int64 {
	if c == nil || c.ReviewTimeoutSeconds <= 0 {
		return DefaultV2ReviewTimeoutSeconds
	}
	return c.ReviewTimeoutSeconds
}

// Validate rejects v2 configs that could silently broaden intake.
func (c *AutomationV2Config) Validate() error {
	if c == nil || !c.Enabled {
		return nil
	}
	if err := validateNoWhitespaceField("trackerIntake.automationV2.label", c.Label); err != nil {
		return err
	}
	if c.MaxParallel < 0 {
		return fmt.Errorf("trackerIntake.automationV2.maxParallel: must not be negative")
	}
	if c.ReviewTimeoutSeconds < 0 {
		return fmt.Errorf("trackerIntake.automationV2.reviewTimeoutSeconds: must not be negative")
	}
	return nil
}

// WithDefaults leaves the provider empty when not explicitly set so the
// caller can infer it from the project's repo origin URL at use time (see
// InferTrackerProvider). Disabled intake leaves the zero value untouched so
// empty project configs still store as NULL.
func (c TrackerIntakeConfig) WithDefaults() TrackerIntakeConfig {
	return c
}

// InferTrackerProvider guesses the tracker provider from a repo origin URL.
// GitHub hosts (github.com, *.github.com, *.ghe.io) map to github; any other
// host maps to gitlab (self-managed GitLab uses arbitrary hostnames). The
// empty-string fallback is github for backward compatibility.
func InferTrackerProvider(repoURL string) TrackerProvider {
	raw := strings.TrimSpace(repoURL)
	if raw == "" {
		return TrackerProviderGitHub
	}
	// SSH form git@host:path → normalise to https://host/path
	if strings.HasPrefix(raw, "git@") {
		if _, rest, ok := strings.Cut(raw, "@"); ok {
			if host, path, ok := strings.Cut(rest, ":"); ok {
				raw = "https://" + host + "/" + path
			}
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return TrackerProviderGitHub
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if host == "github.com" || strings.HasSuffix(host, ".github.com") || strings.HasSuffix(host, ".ghe.io") {
		return TrackerProviderGitHub
	}
	return TrackerProviderGitLab
}

// Validate rejects accidental broad intake and unknown providers. An empty
// provider is accepted here and inferred from the repo URL at use time.
func (c TrackerIntakeConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Provider != "" && c.Provider != TrackerProviderGitHub && c.Provider != TrackerProviderGitLab {
		return fmt.Errorf("trackerIntake.provider: unsupported provider %q", c.Provider)
	}
	if err := validateNoWhitespaceField("trackerIntake.repo", c.Repo); err != nil {
		return err
	}
	if err := validateNoWhitespaceField("trackerIntake.assignee", c.Assignee); err != nil {
		return err
	}
	if strings.TrimSpace(c.Assignee) == "" {
		return fmt.Errorf("trackerIntake: assignee is required when enabled")
	}
	if err := c.AutomationV2.Validate(); err != nil {
		return err
	}
	return nil
}
