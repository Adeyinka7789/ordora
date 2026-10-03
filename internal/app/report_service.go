package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// ReportReader is the persistence contract for reports.
type ReportReader interface {
	KPIs(ctx context.Context, scope tenant.TenantScope, from, to time.Time) (*KPIs, error)
	Trend(ctx context.Context, scope tenant.TenantScope, from, to time.Time, bucketDays int) ([]TrendPoint, error)
	Categories(ctx context.Context, scope tenant.TenantScope, from, to time.Time, limit int) ([]CategoryRow, error)
	Channels(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]ChannelRow, error)
	StageDurations(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]StageDuration, error)
	TopCustomers(ctx context.Context, scope tenant.TenantScope, from, to time.Time, limit int) ([]TopCustomer, error)
	Insights(ctx context.Context, scope tenant.TenantScope) ([]Insight, error)
}

// Re-export the types so handlers can use them without importing postgres.
// KPIs holds the four hero metrics.
type KPIs struct {
	GrossInvoicedMinor int64
	CashSettledMinor   int64
	OutstandingMinor   int64
	BookedOrders       int64
	CompletedOrders    int64
	AvgTicketMinor     int64
	RepeatCustomerPct  int
	TotalCustomers     int64
	RepeatCustomers    int64
	CollectionRatePct  int
	VelocityDays       float64
	PrevGrossMinor     int64
}

type TrendPoint struct {
	BucketStart    time.Time
	InvoicedMinor  int64
	SettledMinor   int64
	MilestoneMinor int64
}

type CategoryRow struct {
	Description    string
	Orders         int64
	TotalMinor     int64
	AvgTicketMinor int64
	PercentOfTotal float64
}

type ChannelRow struct {
	Method     string
	TotalMinor int64
	Count      int64
	Percent    float64
}

type StageDuration struct {
	Stage    string
	AvgHours float64
	Longest  bool
}

type TopCustomer struct {
	ID          uuid.UUID
	Name        string
	Email       string
	Orders      int64
	TotalMinor  int64
	LastOrderAt time.Time
}

type Insight struct {
	Severity   string
	Title      string
	Message    string
	ActionURL  string
	ActionText string
}

// ReportRange is a parsed date range.
type ReportRange struct {
	Key        string
	Label      string
	From       time.Time
	To         time.Time
	BucketDays int
}

// AvailableRanges returns all presets.
func AvailableRanges() []ReportRange {
	now := time.Now()
	endOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(24 * time.Hour)

	// Last 7 days
	last7 := ReportRange{
		Key:        "7d",
		Label:      "Last 7 days",
		From:       endOfToday.AddDate(0, 0, -7),
		To:         endOfToday,
		BucketDays: 1,
	}
	last30 := ReportRange{
		Key:        "30d",
		Label:      "Last 30 days",
		From:       endOfToday.AddDate(0, 0, -30),
		To:         endOfToday,
		BucketDays: 7,
	}
	last90 := ReportRange{
		Key:        "90d",
		Label:      "Last 90 days",
		From:       endOfToday.AddDate(0, 0, -90),
		To:         endOfToday,
		BucketDays: 7,
	}
	// Year to date
	ytd := ReportRange{
		Key:        "ytd",
		Label:      "Year to date",
		From:       time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location()),
		To:         endOfToday,
		BucketDays: 30,
	}
	allTime := ReportRange{
		Key:        "all",
		Label:      "All time",
		From:       time.Date(2020, 1, 1, 0, 0, 0, 0, now.Location()),
		To:         endOfToday,
		BucketDays: 30,
	}
	return []ReportRange{last7, last30, last90, ytd, allTime}
}

// ResolveRange finds a range by key or returns the default (last 30).
func ResolveRange(key string) ReportRange {
	ranges := AvailableRanges()
	for _, r := range ranges {
		if r.Key == key {
			return r
		}
	}
	return ranges[1] // default = 30d
}

// ReportBundle is everything the reports page needs.
type ReportBundle struct {
	Range        ReportRange
	KPIs         *KPIs
	Trend        []TrendPoint
	Categories   []CategoryRow
	Channels     []ChannelRow
	Stages       []StageDuration
	TopCustomers []TopCustomer
	Insights     []Insight
}

// ReportService orchestrates report generation.
type ReportService struct {
	repo ReportReader
}

type ReportServiceDeps struct {
	Repo ReportReader
}

func NewReportService(d ReportServiceDeps) *ReportService {
	return &ReportService{repo: d.Repo}
}

// Load fetches the full report bundle for the given range key.
func (s *ReportService) Load(ctx context.Context, scope tenant.TenantScope, rangeKey string) (*ReportBundle, error) {
	r := ResolveRange(rangeKey)

	b := &ReportBundle{Range: r}

	kpi, err := s.repo.KPIs(ctx, scope, r.From, r.To)
	if err != nil {
		return nil, err
	}
	b.KPIs = kpi

	trend, err := s.repo.Trend(ctx, scope, r.From, r.To, r.BucketDays)
	if err != nil {
		return nil, err
	}
	b.Trend = trend

	cats, err := s.repo.Categories(ctx, scope, r.From, r.To, 10)
	if err != nil {
		return nil, err
	}
	b.Categories = cats

	chans, err := s.repo.Channels(ctx, scope, r.From, r.To)
	if err != nil {
		return nil, err
	}
	b.Channels = chans

	stages, err := s.repo.StageDurations(ctx, scope, r.From, r.To)
	if err != nil {
		return nil, err
	}
	b.Stages = stages

	top, err := s.repo.TopCustomers(ctx, scope, r.From, r.To, 5)
	if err != nil {
		return nil, err
	}
	b.TopCustomers = top

	insights, err := s.repo.Insights(ctx, scope)
	if err != nil {
		return nil, err
	}
	b.Insights = insights

	return b, nil
}

var ErrNoReportsData = errors.New("reports: no data")
