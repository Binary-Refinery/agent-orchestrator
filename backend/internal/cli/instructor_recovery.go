package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"

	"github.com/spf13/cobra"
)

func newInstructorPolicyRebindCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "policy-rebind", Short: "Preview or approve a policy change for existing work"}
	for _, operation := range []string{"preview", "apply"} {
		var inputFile string
		child := &cobra.Command{Use: operation, Short: operation + " a captured work policy change from JSON", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := readInstructorRecoveryInput(inputFile)
			if err != nil {
				return err
			}
			path := "instructor/policy-rebind"
			if operation == "preview" {
				path += "/preview"
			}
			var response json.RawMessage
			if err := ctx.postJSON(cmd.Context(), path, body, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		child.Flags().StringVar(&inputFile, "input", "", "JSON with snapshotId, workId and reason; apply also requires the preview's targetPolicyVersion and exact approval")
		cmd.AddCommand(child)
	}
	return cmd
}

func newInstructorRecoveryCommand(ctx *commandContext) *cobra.Command {
	var inputFile string
	cmd := &cobra.Command{Use: "recovery <work-id>", Short: "Inspect controller recovery, or execute a captured recovery request", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		path := "instructor/works/" + url.PathEscape(args[0]) + "/recovery"
		var response json.RawMessage
		if inputFile == "" {
			if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
				return err
			}
		} else {
			body, err := readInstructorRecoveryInput(inputFile)
			if err != nil {
				return err
			}
			if err := ctx.postJSON(cmd.Context(), path, body, &response); err != nil {
				return err
			}
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().StringVar(&inputFile, "input", "", "Execute JSON with the observed workId, workRevision, policyVersion, sessionId, sessionCreatedAt, providerConversationId and controllerGeneration")
	cmd.Flags().Bool("json", false, "Print JSON (recovery always prints structured JSON)")
	return cmd
}

func readInstructorRecoveryInput(path string) (json.RawMessage, error) {
	if path == "" {
		return nil, usageError{errors.New("--input is required")}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, usageError{errors.New("--input must contain valid JSON")}
	}
	return json.RawMessage(body), nil
}
