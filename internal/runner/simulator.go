package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workforce.local/platform/internal/connectors"
)

type simulatorRunner struct{}

func (simulatorRunner) Manifest() Manifest {
	return Manifest{
		ID: "simulator", Name: "In-process simulator", Kind: "harness", Version: "1", Simulation: true,
		Capabilities: []string{"prepare_proposal", "read_records", "request_input"}, MaxOperations: 3,
	}
}

// clarificationFields are the inputs the simulator must have before it can prepare
// the supplier follow-up. Absent them it stops and asks instead of guessing.
const (
	fieldSupplierEmail = "supplier_email"
	fieldQuantity      = "quantity"
)

var clarificationSchema = json.RawMessage(`{"properties":{"supplier_email":{"type":"string"},"quantity":{"type":"integer"}},"required":["supplier_email","quantity"]}`)

// wantsClarification reports whether this intent must ask a human before it can
// proceed. The keywords are deliberately narrow: a mention of the domain (for example
// "supplier") must NOT by itself divert an otherwise well-defined run into asking a
// question, or a runnable task would stall for no reason.
func wantsClarification(intent string) bool {
	for _, kw := range []string{"clarify", "ask", "missing"} {
		if strings.Contains(intent, kw) {
			return true
		}
	}
	return false
}

func (s simulatorRunner) Run(ctx context.Context, req Request) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	intent := strings.ToLower(req.Intent)
	if wantsClarification(intent) {
		return s.clarify(req)
	}

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
		payload, _ = json.Marshal(map[string]any{
			"recipients": []string{"recipient@example.com"},
			"subject":    "Prepared by run " + req.RunID,
			"body":       "Prepared by run " + req.RunID,
			"bcc":        []string{}, "attachments": []string{},
		})
	}

	target := firstRecord(req.Records, integration)
	if target == nil {
		return Result{Status: StatusFailed, FailureReason: "no_suitable_record"}, nil
	}
	return Result{
		Status:  StatusSucceeded,
		Summary: "Prepared " + integration + " proposal",
		Draft: &ProposalDraft{
			Summary:    "Prepared " + integration + " proposal",
			Operations: []connectors.Operation{draftOp(integration, action, *target, req.RunID, payload)},
		},
	}, nil
}

// clarify either asks for the missing details or, once they have been supplied,
// prepares the follow-up using the human's answer. The answer is visibly
// consequential: the recipient and stated quantity come from the response.
func (s simulatorRunner) clarify(req Request) (Result, error) {
	emailRaw, hasEmail := req.Answer(fieldSupplierEmail)
	qtyRaw, hasQty := req.Answer(fieldQuantity)
	if !hasEmail || !hasQty {
		return Result{
			Status: StatusWaiting,
			Gate: &GateRequest{
				Kind:        "missing_information",
				Prompt:      "Which supplier should this follow-up go to, and what quantity should be proposed?",
				InputSchema: clarificationSchema,
			},
		}, nil
	}

	var email string
	if e := json.Unmarshal(emailRaw, &email); e != nil || strings.TrimSpace(email) == "" {
		return Result{Status: StatusFailed, FailureReason: "invalid_input"}, nil
	}
	var qty float64
	if e := json.Unmarshal(qtyRaw, &qty); e != nil || qty != float64(int64(qty)) {
		return Result{Status: StatusFailed, FailureReason: "invalid_input"}, nil
	}

	target := firstRecord(req.Records, "mail")
	if target == nil {
		return Result{Status: StatusFailed, FailureReason: "no_suitable_record"}, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"recipients": []string{email},
		"subject":    "Stock adjustment request prepared by run " + req.RunID,
		"body":       fmt.Sprintf("Requesting a stock adjustment of %d units.", int64(qty)),
		"bcc":        []string{}, "attachments": []string{},
	})
	return Result{
		Status:  StatusSucceeded,
		Summary: "Prepared supplier follow-up using the supplied details",
		Draft: &ProposalDraft{
			Summary:    "Prepared supplier follow-up for " + email,
			Operations: []connectors.Operation{draftOp("mail", "send", *target, req.RunID, payload)},
		},
	}, nil
}

func firstRecord(records []RecordRef, integration string) *RecordRef {
	for i := range records {
		if records[i].Integration == integration {
			return &records[i]
		}
	}
	return nil
}

// draftOp always carries a stable business key so the reservation layer can prove the
// same official operation is never prepared twice for the same run and target.
func draftOp(integration, action string, target RecordRef, runID string, payload json.RawMessage) connectors.Operation {
	return connectors.Operation{
		Integration: integration, Action: action, TargetID: target.ID,
		ExpectedVersion: target.Version, Payload: payload,
		BusinessKey: "run:" + runID + ":" + target.ID,
	}
}
