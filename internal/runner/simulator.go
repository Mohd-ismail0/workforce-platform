package runner

import (
	"context"
	"encoding/json"
	"strings"
	"workforce.local/platform/internal/connectors"
)

type simulatorRunner struct{}

func (simulatorRunner) Manifest() Manifest {
	return Manifest{ID: "simulator", Name: "In-process simulator", Kind: "harness", Version: "1", Simulation: true, Capabilities: []string{"prepare_proposal", "read_records"}, MaxOperations: 3}
}
func (s simulatorRunner) Run(ctx context.Context, req Request) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}
	intent := strings.ToLower(req.Intent)
	integration := "inventory"
	action := "adjust"
	payload := json.RawMessage(`{"delta":1}`)
	if strings.Contains(intent, "document") {
		integration = "documents"
		action = "update"
		payload, _ = json.Marshal(map[string]string{"title": "Prepared by run " + req.RunID})
	}
	if strings.Contains(intent, "mail") {
		integration = "mail"
		action = "send"
		payload, _ = json.Marshal(map[string]any{"recipients": []string{"recipient@example.com"}, "subject": "Prepared by run " + req.RunID, "body": "Prepared by run " + req.RunID, "bcc": []string{}, "attachments": []string{}})
	}
	var target *RecordRef
	for i := range req.Records {
		if req.Records[i].Integration == integration {
			target = &req.Records[i]
			break
		}
	}
	if target == nil {
		return Result{Status: "failed", FailureReason: "no_suitable_record"}, nil
	}
	// Every draft carries a stable business key so the reservation layer can prove the
	// same official operation is never prepared twice for the same run and target.
	op := connectors.Operation{Integration: integration, Action: action, TargetID: target.ID, ExpectedVersion: target.Version, Payload: payload, BusinessKey: "run:" + req.RunID + ":" + target.ID}
	return Result{Status: "succeeded", Summary: "Prepared " + integration + " proposal", Draft: &ProposalDraft{Summary: "Prepared " + integration + " proposal", Operations: []connectors.Operation{op}}}, nil
}
