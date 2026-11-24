package alert

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/tracium/api/internal/model"
)

const BudgetMetric = "budget"

// CostReader sums a workspace's cost since a UTC day, by workflow or client,
// or under "" for scope workspace.
type CostReader interface {
	Spend(ctx context.Context, workspaceID, scope string, since time.Time) (map[string]float64, error)
}

// BudgetOptions holds the share of a rule's limit at which it warns and goes critical.
type BudgetOptions struct {
	Warning  float64
	Critical float64
}

func DefaultBudgetOptions() BudgetOptions { return BudgetOptions{Warning: 0.8, Critical: 1} }

// Budgets turns a workspace's budget rules into incident signals each evaluation.
type Budgets struct {
	rules RuleStore
	cost  CostReader
	opts  BudgetOptions
}

func NewBudgets(rules RuleStore, cost CostReader, opts BudgetOptions) *Budgets {
	return &Budgets{rules: rules, cost: cost, opts: opts}
}

type spendKey struct{ scope, period string }

// Anomalies reads each enabled rule's period spend once per scope and period.
func (b *Budgets) Anomalies(ctx context.Context, workspaceID string, now time.Time) ([]model.Anomaly, error) {
	rules, err := b.rules.Rules(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	spend := map[spendKey]map[string]float64{}
	for _, r := range rules {
		k := spendKey{r.Scope, r.Period}
		if _, ok := spend[k]; ok || !r.Enabled {
			continue
		}
		m, err := b.cost.Spend(ctx, workspaceID, r.Scope, PeriodStart(r.Period, now))
		if err != nil {
			return nil, fmt.Errorf("budget spend: %w", err)
		}
		spend[k] = m
	}
	return budgetAnomalies(rules, spend, now, b.opts), nil
}

func PeriodStart(period string, now time.Time) time.Time {
	now = now.UTC()
	if period == "month" {
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

var periodNames = map[string]string{"day": "daily", "month": "monthly"}

// budgetAnomalies is one budget signal per rule, subject and period that spent
// at least the warning share of its limit. The signal's bucket is the period
// start, so it fires once and escalates once; when two rules land on the same
// subject and bucket, the one closer to its limit wins.
func budgetAnomalies(rules []Rule, spend map[spendKey]map[string]float64, now time.Time, o BudgetOptions) []model.Anomaly {
	type key struct {
		scope, subject string
		bucket         int64
	}
	best := map[key]model.Anomaly{}
	for _, r := range rules {
		if !r.Enabled || r.ThresholdUSD <= 0 {
			continue
		}
		m := spend[spendKey{r.Scope, r.Period}]
		subjects := []string{r.Subject}
		switch {
		case r.Scope == "workspace":
			subjects = []string{""}
		case r.Subject == AnySubject:
			subjects = subjects[:0]
			for s := range m {
				if s != "" {
					subjects = append(subjects, s)
				}
			}
		}
		bucket := PeriodStart(r.Period, now).UnixMilli()
		for _, s := range subjects {
			cost := m[s]
			ratio := cost / r.ThresholdUSD
			sev := "warning"
			switch {
			case ratio >= o.Critical:
				sev = "critical"
			case ratio < o.Warning:
				continue
			}
			a := model.Anomaly{Metric: BudgetMetric, Scope: r.Scope, BucketMs: bucket, Observed: cost, Expected: r.ThresholdUSD,
				Deviation: cost - r.ThresholdUSD, Score: ratio, Direction: "spike", Severity: sev,
				Summary: fmt.Sprintf("Spent $%.2f of the $%.2f %s budget (%.0f%%)", cost, r.ThresholdUSD, periodNames[r.Period], ratio*100)}
			switch r.Scope {
			case "workflow":
				a.Workflow = s
			case "client":
				a.Client = s
			}
			k := key{r.Scope, s, bucket}
			if prev, ok := best[k]; !ok || a.Score > prev.Score {
				best[k] = a
			}
		}
	}
	out := make([]model.Anomaly, 0, len(best))
	for _, a := range best {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Workflow+out[i].Client < out[j].Workflow+out[j].Client
	})
	return out
}
