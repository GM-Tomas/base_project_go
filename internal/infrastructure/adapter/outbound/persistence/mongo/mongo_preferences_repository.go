package mongo

import (
	"context"
	"errors"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// preferencesDoc is a user's preferences: one document per user, its _id the user's id. Amounts and
// percentages are decimal text, as everywhere else.
type preferencesDoc struct {
	UserID        string                 `bson:"_id"`
	Estimate      estimatePreferencesDoc `bson:"estimate"`
	AutoSnapshot  string                 `bson:"auto_snapshot,omitempty"`
	DefaultView   string                 `bson:"default_view,omitempty"`
	HistoryPeriod string                 `bson:"history_period,omitempty"`
	UpdatedAt     time.Time              `bson:"updated_at"`
}

type estimatePreferencesDoc struct {
	ContributionUSD       string   `bson:"contribution_usd"`
	Years                 int      `bson:"years"`
	YieldMode             string   `bson:"yield_mode"`
	CustomYieldPCT        string   `bson:"custom_yield_pct"`
	MilestonesUSD         []string `bson:"milestones_usd"`
	InflationPCT          string   `bson:"inflation_pct"`
	ContributionGrowthPCT string   `bson:"contribution_growth_pct"`
}

type MongoPreferencesRepository struct {
	coll  *mongo.Collection
	clock func() time.Time
}

func NewMongoPreferencesRepository(db *MongoDB) *MongoPreferencesRepository {
	return &MongoPreferencesRepository{coll: db.Preferences, clock: func() time.Time { return time.Now().UTC() }}
}

var _ outbound.PreferencesRepository = (*MongoPreferencesRepository)(nil)

func (r *MongoPreferencesRepository) Find(ctx context.Context, userId model.UserId) (*model.Preferences, error) {
	var doc preferencesDoc
	err := r.coll.FindOne(ctx, bson.M{"_id": userId.UUID().String()}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	preferences := readPreferences(doc)
	return &preferences, nil
}

func (r *MongoPreferencesRepository) Save(ctx context.Context, userId model.UserId, preferences model.Preferences) error {
	e := preferences.Estimate
	milestones := make([]string, len(e.Milestones))
	for i, m := range e.Milestones {
		milestones[i] = m.String()
	}
	doc := preferencesDoc{
		UserID: userId.UUID().String(),
		Estimate: estimatePreferencesDoc{
			ContributionUSD:       e.Contribution.String(),
			Years:                 e.Years,
			YieldMode:             string(e.YieldMode),
			CustomYieldPCT:        e.CustomYieldPct.String(),
			MilestonesUSD:         milestones,
			InflationPCT:          e.InflationPct.String(),
			ContributionGrowthPCT: e.ContributionGrowthPct.String(),
		},
		AutoSnapshot:  string(preferences.AutoSnapshot),
		DefaultView:   string(preferences.DefaultView),
		HistoryPeriod: string(preferences.HistoryPeriod),
		UpdatedAt:     r.clock(),
	}
	_, err := r.coll.ReplaceOne(ctx, bson.M{"_id": doc.UserID}, doc, options.Replace().SetUpsert(true))
	return err
}

// readPreferences reads them as stored, each value checked as a PUT checks it: one that isn't fine (only
// writable outside the API), or missing (saved before it existed), is its default, on its own, rather than
// failing the whole read.
func readPreferences(doc preferencesDoc) model.Preferences {
	preferences := model.DefaultPreferences()
	preferences.Estimate = readEstimate(doc.Estimate)
	if mode, err := model.ParseAutoSnapshot(doc.AutoSnapshot); err == nil {
		preferences.AutoSnapshot = mode
	}
	if view, err := model.ParseStartView(doc.DefaultView); err == nil {
		preferences.DefaultView = view
	}
	if period, err := model.ParseHistoryPeriod(doc.HistoryPeriod); err == nil {
		preferences.HistoryPeriod = period
	}
	return preferences
}

func readEstimate(stored estimatePreferencesDoc) model.EstimatePreferences {
	defaults := model.DefaultPreferences().Estimate
	result := defaults
	keep := func(apply func(*model.EstimatePreferences)) {
		candidate := defaults
		apply(&candidate)
		if _, err := candidate.Check(); err == nil {
			apply(&result)
		}
	}
	pct := func(s string) (decimal.Decimal, bool) {
		d, err := decimal.NewFromString(s)
		return d, err == nil
	}

	if m, err := readMoney(stored.ContributionUSD); err == nil {
		keep(func(e *model.EstimatePreferences) { e.Contribution = m })
	}
	keep(func(e *model.EstimatePreferences) { e.Years = stored.Years })
	if mode, err := model.ParseYieldMode(stored.YieldMode); err == nil {
		keep(func(e *model.EstimatePreferences) { e.YieldMode = mode })
	}
	if d, ok := pct(stored.CustomYieldPCT); ok {
		keep(func(e *model.EstimatePreferences) { e.CustomYieldPct = d })
	}
	if d, ok := pct(stored.InflationPCT); ok {
		keep(func(e *model.EstimatePreferences) { e.InflationPct = d })
	}
	if d, ok := pct(stored.ContributionGrowthPCT); ok {
		keep(func(e *model.EstimatePreferences) { e.ContributionGrowthPct = d })
	}
	if stored.MilestonesUSD != nil {
		milestones := make([]model.Money, 0, len(stored.MilestonesUSD))
		for _, s := range stored.MilestonesUSD {
			if m, err := readMoney(s); err == nil {
				milestones = append(milestones, m)
			}
		}
		keep(func(e *model.EstimatePreferences) { e.Milestones = milestones })
	}

	checked, err := result.Check() // each value passed on its own: together too, in order
	if err != nil {
		return defaults
	}
	return checked
}
