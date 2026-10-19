package statementsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"sync"
	"time"
)

type TenantStatus string

const (
	TenantPending   TenantStatus = "pending"
	TenantActive    TenantStatus = "active"
	TenantSuspended TenantStatus = "suspended"
)

var ErrStatementNotAllowed = errors.New("statement export is allowed only for active tenants")

type Tenant struct {
	TenantID    string       `json:"tenant_id"`
	CompanyName string       `json:"company_name"`
	AdminEmail  string       `json:"admin_email"`
	Plan        string       `json:"plan"`
	Status      TenantStatus `json:"status"`
	CreatedAt   time.Time    `json:"created_at"`
}

type OnboardTenantInput struct {
	TenantID    string `json:"tenant_id"`
	CompanyName string `json:"company_name"`
	AdminEmail  string `json:"admin_email"`
	Plan        string `json:"plan"`
}

type ExportStatementInput struct {
	TenantID    string `json:"tenant_id"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
}

type StatementResult struct {
	TenantID     string       `json:"tenant_id"`
	TenantStatus TenantStatus `json:"tenant_status"`
	PeriodStart  string       `json:"period_start"`
	PeriodEnd    string       `json:"period_end"`
	UsagePoints  int          `json:"usage_points"`
	UsageTotal   float64      `json:"usage_total"`
	PDF          string       `json:"pdf"`
	GeneratedAt  time.Time    `json:"generated_at"`
	Source       string       `json:"source"`
}

type usageClient interface {
	pdfGenerate(context.Context, PDFGenerateRequest) (PDFGenerateResponse, error)
	accountUsageTimeseries(context.Context, map[string]string) (UsageTimeseriesResponse, error)
}

type Service struct {
	mu            sync.RWMutex
	tenants       map[string]Tenant
	client        usageClient
	webhookSecret string
}

func NewService(client *InfraiClient, webhookSecret string) *Service {
	return &Service{
		tenants:       make(map[string]Tenant),
		client:        client,
		webhookSecret: webhookSecret,
	}
}

func (s *Service) OnboardTenant(in OnboardTenantInput) Tenant {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := Tenant{
		TenantID:    in.TenantID,
		CompanyName: in.CompanyName,
		AdminEmail:  in.AdminEmail,
		Plan:        in.Plan,
		Status:      TenantPending,
		CreatedAt:   time.Now().UTC(),
	}
	s.tenants[in.TenantID] = tenant
	return tenant
}

func (s *Service) ActivateTenant(tenantID string) (Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant, ok := s.tenants[tenantID]
	if !ok {
		return Tenant{}, errors.New("tenant not found")
	}
	tenant.Status = TenantActive
	s.tenants[tenantID] = tenant
	return tenant, nil
}

func (s *Service) SuspendTenant(tenantID string) (Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant, ok := s.tenants[tenantID]
	if !ok {
		return Tenant{}, errors.New("tenant not found")
	}
	tenant.Status = TenantSuspended
	s.tenants[tenantID] = tenant
	return tenant, nil
}

func (s *Service) ExportStatement(ctx context.Context, in ExportStatementInput) (StatementResult, error) {
	s.mu.RLock()
	tenant, ok := s.tenants[in.TenantID]
	s.mu.RUnlock()
	if !ok {
		return StatementResult{}, errors.New("tenant not found")
	}
	if tenant.Status != TenantActive {
		return StatementResult{}, ErrStatementNotAllowed
	}

	query := map[string]string{
		"tenant_id":    in.TenantID,
		"period_start": in.PeriodStart,
		"period_end":   in.PeriodEnd,
	}
	usage, err := s.client.accountUsageTimeseries(ctx, query)
	if err != nil {
		return StatementResult{}, err
	}

	total := 0.0
	for _, p := range usage.Points {
		total += p.Value
	}

	htmlDoc, err := renderStatementHTML(tenant, in, usage, total)
	if err != nil {
		return StatementResult{}, err
	}

	pdfResp, err := s.client.pdfGenerate(ctx, PDFGenerateRequest{
		HTML:        htmlDoc,
		PageSize:    "A4",
		Orientation: "portrait",
		Store:       true,
	})
	if err != nil {
		return StatementResult{}, err
	}

	return StatementResult{
		TenantID:     tenant.TenantID,
		TenantStatus: tenant.Status,
		PeriodStart:  in.PeriodStart,
		PeriodEnd:    in.PeriodEnd,
		UsagePoints:  len(usage.Points),
		UsageTotal:   total,
		PDF:          pdfResp.PDF,
		GeneratedAt:  time.Now().UTC(),
		Source:       "infrai",
	}, nil
}

func renderStatementHTML(tenant Tenant, in ExportStatementInput, usage UsageTimeseriesResponse, total float64) (string, error) {
	const tpl = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Account statement</title>
  <style>
    body { font-family: Arial, sans-serif; margin: 32px; }
    h1 { font-size: 24px; }
    table { width: 100%; border-collapse: collapse; margin-top: 16px; }
    th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
    .meta { margin-top: 12px; color: #444; }
  </style>
</head>
<body>
  <h1>Account statement</h1>
  <div class="meta">Tenant: {{.Tenant.CompanyName}} ({{.Tenant.TenantID}})</div>
  <div class="meta">Plan: {{.Tenant.Plan}}</div>
  <div class="meta">Period: {{.Input.PeriodStart}} to {{.Input.PeriodEnd}}</div>
  <table>
    <thead><tr><th>Time</th><th>Metered units</th></tr></thead>
    <tbody>
      {{range .Usage.Points}}<tr><td>{{.Time}}</td><td>{{printf "%.2f" .Value}}</td></tr>{{end}}
    </tbody>
  </table>
  <p>Total metered units: {{printf "%.2f" .Total}}</p>
</body>
</html>`

	data := struct {
		Tenant Tenant
		Input  ExportStatementInput
		Usage  UsageTimeseriesResponse
		Total  float64
	}{Tenant: tenant, Input: in, Usage: usage, Total: total}

	parsed, err := template.New("statement").Parse(tpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (r StatementResult) JSON() []byte {
	b, _ := json.Marshal(r)
	return b
}

func (s *Service) HandleAccountEvent(raw []byte) string {
	return fmt.Sprintf("accepted %d bytes", len(raw))
}
