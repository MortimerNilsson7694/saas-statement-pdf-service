package statementsvc

import (
	"context"
	"errors"
	"testing"
)

type fakeClient struct {
	usage UsageTimeseriesResponse
	pdf   PDFGenerateResponse
}

func (f *fakeClient) pdfGenerate(ctx context.Context, req PDFGenerateRequest) (PDFGenerateResponse, error) {
	return f.pdf, nil
}

func (f *fakeClient) accountUsageTimeseries(ctx context.Context, query map[string]string) (UsageTimeseriesResponse, error) {
	return f.usage, nil
}

func TestExportStatementLifecycleGate(t *testing.T) {
	tests := []struct {
		name    string
		status  TenantStatus
		wantErr error
	}{
		{name: "pending tenant is blocked", status: TenantPending, wantErr: ErrStatementNotAllowed},
		{name: "suspended tenant is blocked", status: TenantSuspended, wantErr: ErrStatementNotAllowed},
		{name: "active tenant is allowed", status: TenantActive, wantErr: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{
				tenants: map[string]Tenant{
					"acme-prod": {
						TenantID:    "acme-prod",
						CompanyName: "Acme Corp",
						AdminEmail:  "ops@acme.test",
						Plan:        "business",
						Status:      tc.status,
					},
				},
				client: &fakeClient{
					usage: UsageTimeseriesResponse{Points: []UsageTimeseriesPoint{{Time: "2026-08-01", Value: 12}, {Time: "2026-08-02", Value: 8}}},
					pdf:   PDFGenerateResponse{PDF: "JVBERi0xLjQ=", JobID: "job_123"},
				},
			}

			result, err := svc.ExportStatement(context.Background(), ExportStatementInput{
				TenantID:    "acme-prod",
				PeriodStart: "2026-08-01",
				PeriodEnd:   "2026-08-31",
			})

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got %v", tc.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.TenantStatus != TenantActive {
				t.Fatalf("expected active status, got %s", result.TenantStatus)
			}
			if result.UsageTotal != 20 {
				t.Fatalf("expected total usage 20, got %v", result.UsageTotal)
			}
			if result.PDF == "" {
				t.Fatal("expected PDF output")
			}
		})
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "topsecret"
	timestamp := "1725148800"
	body := []byte(`{"event":"tenant.activated"}`)
	sig := "78750bd19f75259f143d90a46b84f68ab0ad7ce9986768f17abfe7ad0d8c9f87"
	if !VerifyWebhookSignature(secret, timestamp, body, sig) {
		t.Fatal("expected signature to verify")
	}
}
