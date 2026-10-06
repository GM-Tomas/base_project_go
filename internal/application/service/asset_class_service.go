package service

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/parallel"
)

var DefaultAssetClasses = []string{
	"Cash",
	"Fixed Income",
	"Index Fund",
	"Equity",
	"Crypto",
}

var DefaultLiquidAssetClasses = []string{
	"Cash",
	"Equity",
	"Crypto",
	"Index Fund",
}

// NewClassDefaults is what classes are like until a user changes them: these classes (DefaultAssetClasses
// if none) and these liquid ones (DefaultLiquidAssetClasses if none). Names the domain wouldn't take are
// left out.
func NewClassDefaults(names, liquid []string) model.ClassDefaults {
	if len(names) == 0 {
		names = DefaultAssetClasses
	}
	if len(liquid) == 0 {
		liquid = DefaultLiquidAssetClasses
	}
	return model.ClassDefaults{Names: assetClasses(names), Liquid: assetClasses(liquid)}
}

func assetClasses(names []string) []model.AssetClass {
	var classes []model.AssetClass
	for _, name := range names {
		if class, err := model.NewAssetClass(name); err == nil && !slices.Contains(classes, class) {
			classes = append(classes, class)
		}
	}
	return classes
}

const classSettingsQuota = "asset_class_settings"

var errClassesLimit = appErrors.NewLimitExceededError(fmt.Sprintf(
	"You can set up to %d asset classes. Remove one to add another.", model.MaxClassSettingsPerUser))

func classNotFound() error {
	return appErrors.NewResourceNotFoundError("Asset class not found")
}

func classExists(class model.AssetClass) error {
	return appErrors.NewConflictError("class-exists", fmt.Sprintf("There's already a class named %q", class.Value()))
}

// AssetClassService lists the user's asset classes and lets them set them up: what a class is like is the
// default until they change it (a settings document per class they changed), and renaming, merging or
// removing one moves its holdings in the same transaction.
type AssetClassService struct {
	tx           outbound.TransactionManager
	holdingRepo  outbound.HoldingRepository
	settingsRepo outbound.AssetClassSettingsRepository
	quotaRepo    outbound.QuotaRepository
	aggregation  outbound.WealthAggregationPort
	defaults     model.ClassDefaults
	clock        Clock
}

func NewAssetClassService(
	tx outbound.TransactionManager,
	holdingRepo outbound.HoldingRepository,
	settingsRepo outbound.AssetClassSettingsRepository,
	quotaRepo outbound.QuotaRepository,
	aggregation outbound.WealthAggregationPort,
	defaults model.ClassDefaults,
	clock Clock,
) *AssetClassService {
	if clock == nil {
		clock = RealClock
	}
	return &AssetClassService{
		tx: tx, holdingRepo: holdingRepo, settingsRepo: settingsRepo, quotaRepo: quotaRepo,
		aggregation: aggregation, defaults: defaults, clock: clock,
	}
}

var _ inbound.AssetClassUseCase = (*AssetClassService)(nil)

// GetAvailableAssetClasses reads the classes in use, the user's settings and what each class is worth, at
// once.
func (s *AssetClassService) GetAvailableAssetClasses(
	ctx context.Context,
	userId model.UserId,
) (inbound.AvailableAssetClasses, error) {
	var (
		inUse    []model.AssetClass
		settings []model.AssetClassSettings
		totals   []outbound.AssetClassAggregate
	)
	err := parallel.Run(ctx,
		func(ctx context.Context) (err error) {
			inUse, err = s.holdingRepo.AssetClassesInUse(ctx, userId)
			return err
		},
		func(ctx context.Context) (err error) {
			settings, err = s.settingsRepo.FindAll(ctx, userId)
			return err
		},
		func(ctx context.Context) (err error) {
			totals, err = s.aggregation.ByAssetClass(ctx, userId)
			return err
		},
	)
	if err != nil {
		return inbound.AvailableAssetClasses{}, err
	}

	classes := model.NewClasses(s.defaults, settings)
	byClass := make(map[model.AssetClass]outbound.AssetClassAggregate, len(totals))
	for _, t := range totals {
		byClass[t.AssetClass] = t
	}
	visible := classes.Visible(inUse)
	available := inbound.AvailableAssetClasses{
		Defaults: labels(s.defaults.Names),
		InUse:    labels(inUse),
		All:      labels(visible),
		Classes:  make([]inbound.AssetClassView, len(visible)),
	}
	for i, class := range visible {
		total, ok := byClass[class]
		if !ok {
			total.Value = model.ZeroMoney
		}
		available.Classes[i] = inbound.AssetClassView{
			Name:              class,
			Color:             classes.Color(class),
			Liquid:            classes.Liquid(class),
			ExpectedReturnPct: classes.ReturnPct(class),
			IsDefault:         classes.IsDefault(class),
			HoldingsCount:     total.Count,
			Value:             total.Value,
		}
	}
	return available, nil
}

func labels(classes []model.AssetClass) []string {
	names := make([]string, len(classes))
	for i, class := range classes {
		names[i] = class.Value()
	}
	return names
}

// CreateAssetClass adds a class the user doesn't have yet (one with that exact name: classes tell case
// apart), so it can be picked before any holding has it. A default class they removed comes back.
func (s *AssetClassService) CreateAssetClass(
	ctx context.Context,
	command inbound.CreateAssetClassCommand,
) (inbound.AssetClassView, error) {
	class, err := model.NewAssetClass(command.Name)
	if err != nil {
		return inbound.AssetClassView{}, err
	}
	color, err := optionalColor(command.Color)
	if err != nil {
		return inbound.AssetClassView{}, err
	}
	pct, err := expectedReturn(command.ExpectedReturnPct)
	if err != nil {
		return inbound.AssetClassView{}, err
	}
	now := s.clock()

	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		classes, visible, err := s.read(ctx, command.UserId)
		if err != nil {
			return err
		}
		if slices.Contains(visible, class) {
			return classExists(class)
		}
		before, had := classes.Settings(class)
		created := model.AssetClassSettings{
			UserId: command.UserId, Name: class, Color: color, Liquid: command.Liquid, ExpectedReturnPct: pct,
			CreatedAt: now, UpdatedAt: now,
		}
		return s.write(ctx, command.UserId, classDoc{before: settingsIf(before, had), after: s.kept(created)})
	})
	if err != nil {
		return inbound.AssetClassView{}, err
	}
	return s.view(ctx, command.UserId, class)
}

// UpdateAssetClass changes what's set for a class the user has, and renames it on all its holdings if a
// new name is sent. Onto a class they have, that's a merge: only with MergeIfExists, and the class merged
// into keeps its settings (the rest of the request is left out). A removed default stays removed.
func (s *AssetClassService) UpdateAssetClass(
	ctx context.Context,
	command inbound.UpdateAssetClassCommand,
) (inbound.AssetClassView, error) {
	class, err := model.ParseAssetClassId(command.Id)
	if err != nil {
		return inbound.AssetClassView{}, classNotFound()
	}
	target := class
	if command.Name != nil {
		if target, err = model.NewAssetClass(*command.Name); err != nil {
			return inbound.AssetClassView{}, err
		}
	}
	color, err := optionalColor(command.Color.Value)
	if err != nil {
		return inbound.AssetClassView{}, err
	}
	pct, err := expectedReturn(command.ExpectedReturnPct.Value)
	if err != nil {
		return inbound.AssetClassView{}, err
	}
	now := s.clock()

	final := target
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		classes, visible, err := s.read(ctx, command.UserId)
		if err != nil {
			return err
		}
		if !slices.Contains(visible, class) {
			return classNotFound()
		}
		current, had := classes.Settings(class)
		if !had {
			current = model.AssetClassSettings{UserId: command.UserId, Name: class, CreatedAt: now}
		}
		updated := current
		if command.Color.Set {
			updated.Color = color
		}
		if command.Liquid.Set {
			updated.Liquid = command.Liquid.Value
		}
		if command.ExpectedReturnPct.Set {
			updated.ExpectedReturnPct = pct
		}

		if target == class {
			if sameClassSettings(current, updated) {
				return nil
			}
			updated.UpdatedAt = now
			return s.write(ctx, command.UserId, classDoc{before: settingsIf(current, had), after: s.kept(updated)})
		}

		gone := classDoc{before: settingsIf(current, had), after: s.removed(command.UserId, class, settingsIf(current, had), now)}
		if slices.Contains(visible, target) {
			if !command.MergeIfExists {
				return classExists(target)
			}
			if _, err := s.holdingRepo.ReassignAssetClass(ctx, command.UserId, class, target); err != nil {
				return err
			}
			return s.write(ctx, command.UserId, gone)
		}

		// A new name: the class, as just set up, goes there (over a removed default's settings, if any).
		if _, err := s.holdingRepo.ReassignAssetClass(ctx, command.UserId, class, target); err != nil {
			return err
		}
		replaced, hadTarget := classes.Settings(target)
		renamed := updated
		renamed.Name, renamed.Hidden, renamed.UpdatedAt = target, false, now
		return s.write(ctx, command.UserId, gone, classDoc{before: settingsIf(replaced, hadTarget), after: s.kept(renamed)})
	})
	if err != nil {
		return inbound.AssetClassView{}, err
	}
	return s.view(ctx, command.UserId, final)
}

// DeleteAssetClass removes a class the user has: its holdings, if any, move to MoveTo in the same
// transaction (without it, a class with holdings stays: 409 class-in-use). A default class stays removed.
func (s *AssetClassService) DeleteAssetClass(ctx context.Context, command inbound.DeleteAssetClassCommand) error {
	class, err := model.ParseAssetClassId(command.Id)
	if err != nil {
		return classNotFound()
	}
	var moveTo *model.AssetClass
	if command.MoveTo != nil {
		to, err := model.NewAssetClass(*command.MoveTo)
		if err != nil {
			return appErrors.NewValidationErrors([]appErrors.ValidationError{{Field: "moveTo", Message: err.Error()}})
		}
		if to == class {
			return appErrors.NewValidationErrors([]appErrors.ValidationError{
				{Field: "moveTo", Message: "moveTo must be another class"}})
		}
		moveTo = &to
	}
	now := s.clock()

	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		inUse, err := s.holdingRepo.AssetClassesInUse(ctx, command.UserId)
		if err != nil {
			return err
		}
		settings, err := s.settingsRepo.FindAll(ctx, command.UserId)
		if err != nil {
			return err
		}
		classes := model.NewClasses(s.defaults, settings)
		if !slices.Contains(classes.Visible(inUse), class) {
			return classNotFound()
		}
		if slices.Contains(inUse, class) {
			if moveTo == nil {
				return appErrors.NewConflictError("class-in-use", fmt.Sprintf(
					"%s still has assets: say which class they move to (moveTo)", class.Value()))
			}
			if _, err := s.holdingRepo.ReassignAssetClass(ctx, command.UserId, class, *moveTo); err != nil {
				return err
			}
		}
		current := settingsIf(classes.Settings(class))
		return s.write(ctx, command.UserId, classDoc{before: current, after: s.removed(command.UserId, class, current, now)})
	})
}

// read is the user's classes as set up and the ones they have, from inside a transaction (one read at a
// time).
func (s *AssetClassService) read(ctx context.Context, userId model.UserId) (model.Classes, []model.AssetClass, error) {
	inUse, err := s.holdingRepo.AssetClassesInUse(ctx, userId)
	if err != nil {
		return model.Classes{}, nil, err
	}
	settings, err := s.settingsRepo.FindAll(ctx, userId)
	if err != nil {
		return model.Classes{}, nil, err
	}
	classes := model.NewClasses(s.defaults, settings)
	return classes, classes.Visible(inUse), nil
}

// view is the class as the list shows it, read after the change (empty, but named, if it's gone already).
func (s *AssetClassService) view(ctx context.Context, userId model.UserId, class model.AssetClass) (inbound.AssetClassView, error) {
	available, err := s.GetAvailableAssetClasses(ctx, userId)
	if err != nil {
		return inbound.AssetClassView{}, err
	}
	for _, v := range available.Classes {
		if v.Name == class {
			return v, nil
		}
	}
	return inbound.AssetClassView{Name: class, Value: model.ZeroMoney}, nil
}

// classDoc is a change to one class's settings document: what it was (nil: none) and what it becomes (nil:
// none).
type classDoc struct {
	before, after *model.AssetClassSettings
}

func settingsIf(s model.AssetClassSettings, ok bool) *model.AssetClassSettings {
	if !ok {
		return nil
	}
	return &s
}

// kept is the document to keep for these settings: a class the user created is kept by its document, a
// default one only needs it for what differs from its defaults.
func (s *AssetClassService) kept(settings model.AssetClassSettings) *model.AssetClassSettings {
	if slices.Contains(s.defaults.Names, settings.Name) && !settings.Customized() {
		return nil
	}
	return &settings
}

// removed is what's left of a class the user removed (or renamed, or merged into another): nothing, or, for
// a default class, a note that it's hidden, so it doesn't come back.
func (s *AssetClassService) removed(userId model.UserId, class model.AssetClass, current *model.AssetClassSettings, now time.Time) *model.AssetClassSettings {
	if !slices.Contains(s.defaults.Names, class) {
		return nil
	}
	hidden := model.AssetClassSettings{UserId: userId, Name: class, Hidden: true, CreatedAt: now, UpdatedAt: now}
	if current != nil {
		hidden.CreatedAt = current.CreatedAt
	}
	return &hidden
}

// write saves these changes, counting the documents they add or remove against the user's cap first.
func (s *AssetClassService) write(ctx context.Context, userId model.UserId, changes ...classDoc) error {
	added := 0
	for _, c := range changes {
		switch {
		case c.before == nil && c.after != nil:
			added++
		case c.before != nil && c.after == nil:
			added--
		}
	}
	if added > 0 {
		ok, err := s.quotaRepo.Reserve(ctx, userId, classSettingsQuota, added, model.MaxClassSettingsPerUser)
		if err != nil {
			return err
		}
		if !ok {
			return errClassesLimit
		}
	}
	for _, c := range changes {
		switch {
		case c.after != nil:
			if err := s.settingsRepo.Save(ctx, *c.after); err != nil {
				return err
			}
		case c.before != nil:
			if _, err := s.settingsRepo.Delete(ctx, userId, c.before.Name); err != nil {
				return err
			}
		}
	}
	if added < 0 {
		return s.quotaRepo.Release(ctx, userId, classSettingsQuota, -added)
	}
	return nil
}

func sameClassSettings(a, b model.AssetClassSettings) bool {
	return a.Hidden == b.Hidden && sameColor(a.Color, b.Color) && sameBool(a.Liquid, b.Liquid) &&
		model.SameReturn(a.ExpectedReturnPct, b.ExpectedReturnPct)
}

func sameColor(a, b *model.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// optionalColor is a color as sent (nil: none), checked.
func optionalColor(sent *string) (*model.Color, error) {
	if sent == nil {
		return nil, nil
	}
	c, err := model.NewColor(*sent)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// withClassReturns is these holdings with their class's default return filled in (see
// model.Holding.EffectiveReturnPct).
func withClassReturns(classes model.Classes, holdings []model.Holding) []model.Holding {
	for i := range holdings {
		holdings[i].ClassReturnPct = classes.ReturnPct(holdings[i].AssetClass)
	}
	return holdings
}
