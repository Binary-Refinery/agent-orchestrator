package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// CLI DTOs intentionally mirror the HTTP boundary. Raw response JSON preserves
// policy and evidence details without importing daemon/controller packages.
type instructorCommandDTO struct {
	Command              string          `json:"command"`
	SnapshotID           string          `json:"snapshotId,omitempty"`
	WorkIDs              []string        `json:"workIds,omitempty"`
	Approval             string          `json:"approval,omitempty"`
	IdempotencyKey       string          `json:"idempotencyKey,omitempty"`
	New                  json.RawMessage `json:"new,omitempty"`
	PublicationOperation string          `json:"publicationOperation,omitempty"`
}
type instructorSnapshotDTO struct {
	ID    string `json:"id"`
	Works []struct {
		ID        string `json:"id"`
		ProjectID string `json:"projectId"`
		Name      string `json:"name"`
		State     string `json:"state"`
		Phase     string `json:"phase"`
		Next      string `json:"next"`
		Gate      *struct {
			Kind string `json:"kind"`
		} `json:"gate"`
		Actions []string `json:"actions"`
	} `json:"works"`
}

func newInstructorCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "instructor", Short: "Inspect and direct governed work across projects"}
	cmd.AddCommand(newInstructorStatusCommand(ctx), instructorReadCommand(ctx, "capabilities", "Inspect Instructor availability", "instructor/capabilities"), instructorReadCommand(ctx, "inventory", "List real sessions including unassigned legacy work", "instructor/inventory"), instructorReadCommand(ctx, "commands", "List the daemon's command registry", "instructor/commands"), newInstructorDetailsCommand(ctx), newInstructorActionCommand(ctx), newInstructorRegisterCommand(ctx), newInstructorReportCommand(ctx), newInstructorAttachCommand(ctx), newInstructorChecksCommand(ctx), newInstructorPolicyCommand(ctx), newInstructorVerifyCommand(ctx), newInstructorReconcileCommand(ctx), newInstructorExceptionCommand(ctx))
	cmd.AddCommand(newInstructorInventoryReconcileCommand(ctx))
	cmd.AddCommand(newInstructorPlanCommand(ctx), instructorReadCommand(ctx, "plans", "List durable global Instructor dialogues", "instructor/plans"), newInstructorPlanStatusCommand(ctx))
	return cmd
}

func instructorReadCommand(ctx *commandContext, name, short, path string) *cobra.Command {
	cmd := &cobra.Command{Use: name, Short: short, Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().Bool("json", false, "Print JSON (this command always prints structured JSON)")
	return cmd
}

func newInstructorStatusCommand(ctx *commandContext) *cobra.Command {
	var baseID string
	var jsonOutput bool
	cmd := &cobra.Command{Use: "status", Short: "Capture work status and the scope for the next command", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		path := "instructor/status"
		if baseID != "" {
			path += "?baseId=" + url.QueryEscape(baseID)
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
			return err
		}
		if jsonOutput {
			return writeJSON(cmd.OutOrStdout(), response)
		}
		var snapshot instructorSnapshotDTO
		if err := json.Unmarshal(response, &snapshot); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Snapshot: %s\n", snapshot.ID); err != nil {
			return err
		}
		writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(writer, "WORK\tPROJECT\tNAME\tSTATE\tPHASE\tGATE\tNEXT\tACTIONS"); err != nil {
			return err
		}
		for _, work := range snapshot.Works {
			gate := "-"
			if work.Gate != nil {
				gate = work.Gate.Kind
			}
			if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", work.ID, work.ProjectID, work.Name, work.State, work.Phase, gate, work.Next, strings.Join(work.Actions, ",")); err != nil {
				return err
			}
		}
		return writer.Flush()
	}}
	cmd.Flags().StringVar(&baseID, "base", "", "Previous observed snapshot identifier")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print the full daemon snapshot")
	return cmd
}

func newInstructorDetailsCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "details <work-id>", Short: "Read work history, evidence and dispatch outcomes", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), "instructor/works/"+url.PathEscape(args[0]), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().Bool("json", false, "Print JSON (details always prints structured JSON)")
	return cmd
}

func newInstructorActionCommand(ctx *commandContext) *cobra.Command {
	var body instructorCommandDTO
	var newFile string
	cmd := &cobra.Command{Use: "command <name>", Aliases: []string{"run"}, Short: "Execute a registry command against an observed snapshot", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		body.Command = args[0]
		if os.Getenv("AO_SESSION_ID") != "" && strings.HasPrefix(body.Approval, "PUBLISH-GO ") {
			return usageError{errors.New("native sessions cannot authorize publication; a protected operator decision is required")}
		}
		if newFile != "" {
			data, err := os.ReadFile(newFile)
			if err != nil {
				return err
			}
			if !json.Valid(data) {
				return usageError{errors.New("--new-file must contain valid JSON")}
			}
			body.New = data
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "instructor/commands", body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&body.SnapshotID, "snapshot", "", "Exact snapshot identifier returned by instructor status")
	cmd.Flags().StringSliceVar(&body.WorkIDs, "work", nil, "Work identifiers within that snapshot (omit to select its displayed eligible work)")
	cmd.Flags().StringVar(&body.Approval, "approval", "", "Exact approval phrase required by the displayed gate")
	cmd.Flags().StringVar(&body.PublicationOperation, "publication-operation", "", "Bind publication approval to publish or merge (default publish)")
	cmd.Flags().StringVar(&body.IdempotencyKey, "key", "", "Stable caller key for safely retrying the same command")
	cmd.Flags().StringVar(&newFile, "new-file", "", "Registration JSON for a new-work registry command")
	cmd.Flags().Bool("json", false, "Print JSON (command outcomes are always structured)")
	return cmd
}

func newInstructorRegisterCommand(ctx *commandContext) *cobra.Command {
	var inputFile string
	cmd := &cobra.Command{Use: "register", Short: "Register work from explicit project and policy JSON", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		if inputFile == "" {
			return usageError{errors.New("--input is required")}
		}
		body, err := os.ReadFile(inputFile)
		if err != nil {
			return err
		}
		if !json.Valid(body) {
			return usageError{errors.New("--input must contain valid JSON")}
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "instructor/works", json.RawMessage(body), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&inputFile, "input", "", "Registration JSON file with project, goal, source and governance policy")
	return cmd
}

func newInstructorAttachCommand(ctx *commandContext) *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{Use: "attach <work-id>", Short: "Attach an existing session to governed work", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		if sessionID == "" {
			return usageError{errors.New("--session is required")}
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "instructor/works/"+url.PathEscape(args[0])+"/sessions", struct {
			SessionID string `json:"sessionId"`
		}{sessionID}, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&sessionID, "session", "", "Existing AO session identifier")
	return cmd
}

func newInstructorReportCommand(ctx *commandContext) *cobra.Command {
	var inputFile string
	var body struct {
		WorkID     string `json:"workId"`
		SessionID  string `json:"sessionId"`
		Kind       string `json:"kind"`
		Summary    string `json:"summary,omitempty"`
		Next       string `json:"next,omitempty"`
		SHA        string `json:"sha,omitempty"`
		Branch     string `json:"branch,omitempty"`
		Phase      string `json:"phase,omitempty"`
		DispatchID string `json:"dispatchId,omitempty"`
	}
	cmd := &cobra.Command{Use: "report", Short: "Report a work checkpoint or acknowledge an Instructor dispatch", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		var payload any = body
		if inputFile != "" {
			data, err := os.ReadFile(inputFile)
			if err != nil {
				return err
			}
			if !json.Valid(data) {
				return usageError{errors.New("--input must contain valid JSON")}
			}
			payload = json.RawMessage(data)
		} else if body.WorkID == "" || body.SessionID == "" || body.Kind == "" {
			return usageError{errors.New("--work, --session and --kind, or --input are required")}
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "instructor/reports", payload, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&inputFile, "input", "", "Full report JSON, including waits, gates or evidence claims")
	cmd.Flags().StringVar(&body.WorkID, "work", "", "Stable work identifier")
	cmd.Flags().StringVar(&body.SessionID, "session", "", "Reporting AO session identifier")
	cmd.Flags().StringVar(&body.Kind, "kind", "", "Report kind from the work governance contract")
	cmd.Flags().StringVar(&body.Summary, "summary", "", "Short factual progress summary")
	cmd.Flags().StringVar(&body.Next, "next", "", "Concrete next step")
	cmd.Flags().StringVar(&body.SHA, "sha", "", "Current worktree HEAD")
	cmd.Flags().StringVar(&body.Branch, "branch", "", "Current worktree branch")
	cmd.Flags().StringVar(&body.Phase, "phase", "", "Requested governed phase")
	cmd.Flags().StringVar(&body.DispatchID, "dispatch", "", "Dispatch identifier being acknowledged")
	return cmd
}

func newInstructorChecksCommand(ctx *commandContext) *cobra.Command {
	var sessionID string
	var profiles []string
	cmd := &cobra.Command{Use: "checks <work-id>", Short: "Run approved bounded check profiles for attached work", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		if sessionID == "" || len(profiles) == 0 {
			return usageError{errors.New("--session and --profile are required")}
		}
		var response json.RawMessage
		body := struct {
			SessionID string   `json:"sessionId"`
			Profiles  []string `json:"profiles"`
		}{sessionID, profiles}
		if err := ctx.postJSON(cmd.Context(), "instructor/works/"+url.PathEscape(args[0])+"/checks", body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&sessionID, "session", "", "Attached AO session whose worktree owns the checks")
	cmd.Flags().StringSliceVar(&profiles, "profile", nil, "Policy profile names (repeatable or comma-separated)")
	return cmd
}

func newInstructorPolicyCommand(ctx *commandContext) *cobra.Command {
	var inputFile string
	cmd := &cobra.Command{Use: "policy", Short: "Read or validate and save governance policy for future work", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		var response json.RawMessage
		if inputFile == "" {
			if err := ctx.getJSON(cmd.Context(), "instructor/policy", &response); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(inputFile)
			if err != nil {
				return err
			}
			if !json.Valid(data) {
				return usageError{errors.New("--input must contain valid JSON")}
			}
			if err := ctx.putJSON(cmd.Context(), "instructor/policy", json.RawMessage(data), &response); err != nil {
				return err
			}
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&inputFile, "input", "", "Governance policy JSON to validate and persist")
	cmd.Flags().Bool("json", false, "Print JSON (this command always prints structured JSON)")
	return cmd
}

func newInstructorVerifyCommand(ctx *commandContext) *cobra.Command {
	var inputFile string
	cmd := &cobra.Command{Use: "verify <work-id>", Short: "Verify an evidence claim against trusted daemon facts", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		if inputFile == "" {
			return usageError{errors.New("--input evidence JSON file is required")}
		}
		data, err := os.ReadFile(inputFile)
		if err != nil {
			return err
		}
		if !json.Valid(data) {
			return usageError{errors.New("--input must contain valid JSON")}
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "instructor/works/"+url.PathEscape(args[0])+"/evidence/verify", json.RawMessage(data), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&inputFile, "input", "", "Evidence claim JSON to resolve against exact daemon-owned provenance")
	return cmd
}

func newInstructorReconcileCommand(ctx *commandContext) *cobra.Command {
	var body struct {
		SnapshotID string `json:"snapshotId"`
		WorkID     string `json:"workId"`
		DispatchID string `json:"dispatchId"`
		Accepted   bool   `json:"accepted"`
		Note       string `json:"note"`
	}
	cmd := &cobra.Command{Use: "reconcile", Short: "Resolve one uncertain native submission using an explicit observation", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		if body.SnapshotID == "" || body.WorkID == "" || body.DispatchID == "" || strings.TrimSpace(body.Note) == "" || !cmd.Flags().Changed("accepted") {
			return usageError{errors.New("--snapshot, --work, --dispatch, --note and explicit --accepted=true|false are required")}
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "instructor/reconcile", body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&body.SnapshotID, "snapshot", "", "Observed snapshot containing the exact uncertain work")
	cmd.Flags().StringVar(&body.WorkID, "work", "", "Stable work identifier")
	cmd.Flags().StringVar(&body.DispatchID, "dispatch", "", "Exact native submission to reconcile")
	cmd.Flags().BoolVar(&body.Accepted, "accepted", false, "Explicit observation: true accepted, false unaccepted (does not resend)")
	cmd.Flags().StringVar(&body.Note, "note", "", "Observation supporting this reconciliation")
	return cmd
}

func newInstructorExceptionCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "exception", Short: "Preview or approve an expiring exception for one captured work"}
	for _, operation := range []string{"preview", "apply"} {
		var inputFile string
		child := &cobra.Command{Use: operation, Short: operation + " one work-scoped exception from JSON", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
			if inputFile == "" {
				return usageError{errors.New("--input is required")}
			}
			body, err := os.ReadFile(inputFile)
			if err != nil {
				return err
			}
			if !json.Valid(body) {
				return usageError{errors.New("--input must contain valid JSON")}
			}
			path := "instructor/exceptions"
			if operation == "preview" {
				path += "/preview"
			}
			var response json.RawMessage
			if err := ctx.postJSON(cmd.Context(), path, json.RawMessage(body), &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		child.Flags().StringVar(&inputFile, "input", "", "JSON containing snapshot, work, concrete selection or fix budget, reason and expiry; apply also requires the preview's exact approval")
		cmd.AddCommand(child)
	}
	return cmd
}

func newInstructorInventoryReconcileCommand(ctx *commandContext) *cobra.Command {
	var lastSeen string
	cmd := &cobra.Command{Use: "inventory-reconcile <resource-id>", Short: "Acknowledge a no-longer-observed resource without deleting data", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		if lastSeen == "" {
			return usageError{errors.New("--last-seen must equal the displayed observation timestamp")}
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "instructor/inventory/reconcile", struct {
			ResourceID string `json:"resourceId"`
			LastSeenAt string `json:"lastSeenAt"`
		}{args[0], lastSeen}, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&lastSeen, "last-seen", "", "Exact lastSeenAt returned by the cached inventory")
	return cmd
}

func newInstructorPlanCommand(ctx *commandContext) *cobra.Command {
	var inputFile string
	var body struct {
		Request        string `json:"request"`
		SnapshotID     string `json:"snapshotId"`
		IdempotencyKey string `json:"idempotencyKey"`
		TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	}
	cmd := &cobra.Command{Use: "plan", Short: "Start a global native Instructor dialogue that produces advisory proposals", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		var request any = body
		if inputFile != "" {
			data, err := os.ReadFile(inputFile)
			if err != nil {
				return err
			}
			if !json.Valid(data) {
				return usageError{errors.New("--input must contain valid JSON")}
			}
			request = json.RawMessage(data)
		} else if strings.TrimSpace(body.Request) == "" {
			return usageError{errors.New("--request or --input is required")}
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "instructor/plans", request, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&inputFile, "input", "", "Strategic/intake request JSON")
	cmd.Flags().StringVar(&body.Request, "request", "", "Operator request for portfolio strategy or intake clarification")
	cmd.Flags().StringVar(&body.SnapshotID, "snapshot", "", "Frozen snapshot returned by instructor status")
	cmd.Flags().StringVar(&body.IdempotencyKey, "key", "", "Stable caller identity for the same planning request")
	cmd.Flags().IntVar(&body.TimeoutSeconds, "timeout", 0, "Bounded native dialogue timeout in seconds (daemon validates limits)")
	return cmd
}

func newInstructorPlanStatusCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{Use: "plan-status <plan-id>", Short: "Read global Instructor dialogue state and proposals", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), "instructor/plans/"+url.PathEscape(args[0]), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
}
