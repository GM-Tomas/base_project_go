package service

import (
	"context"
	"fmt"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/parallel"
)

const platformSettingsQuota = "platform_settings"

var errPlatformsLimit = appErrors.NewLimitExceededError(fmt.Sprintf(
	"You can customize up to %d platforms. Reset one to customize another.", model.MaxPlatformSettingsPerUser))

func platformNotFound() error {
	return appErrors.NewResourceNotFoundError("Platform not found")
}

// PlatformService lists the user's platforms (the names their holdings use) as they set them up, and lets
// them change how one looks and what it's called: a rename is made on all its holdings in one transaction.
type PlatformService struct {
	tx           outbound.TransactionManager
	platformRepo outbound.PlatformRepository
	holdingRepo  outbound.HoldingRepository
	settingsRepo outbound.PlatformSettingsRepository
	quotaRepo    outbound.QuotaRepository
	clock        Clock
}

func NewPlatformService(
	tx outbound.TransactionManager,
	platformRepo outbound.PlatformRepository,
	holdingRepo outbound.HoldingRepository,
	settingsRepo outbound.PlatformSettingsRepository,
	quotaRepo outbound.QuotaRepository,
	clock Clock,
) *PlatformService {
	if clock == nil {
		clock = RealClock
	}
	return &PlatformService{
		tx: tx, platformRepo: platformRepo, holdingRepo: holdingRepo, settingsRepo: settingsRepo,
		quotaRepo: quotaRepo, clock: clock,
	}
}

var _ inbound.PlatformUseCase = (*PlatformService)(nil)

// GetAllPlatforms reads the platforms and how the user set them up at once.
func (s *PlatformService) GetAllPlatforms(ctx context.Context, userId model.UserId) ([]model.Platform, error) {
	var (
		platforms []model.Platform
		settings  []model.PlatformSettings
	)
	err := parallel.Run(ctx,
		func(ctx context.Context) (err error) {
			platforms, err = s.platformRepo.FindAll(ctx, userId)
			return err
		},
		func(ctx context.Context) (err error) {
			settings, err = s.settingsRepo.FindAll(ctx, userId)
			return err
		},
	)
	if err != nil {
		return nil, err
	}
	return WithPlatformSettings(platforms, settings), nil
}

// WithPlatformSettings is each platform as the user set it up.
func WithPlatformSettings(platforms []model.Platform, settings []model.PlatformSettings) []model.Platform {
	byKey := make(map[string]model.PlatformSettings, len(settings))
	for _, ps := range settings {
		byKey[ps.Key] = ps
	}
	for i, p := range platforms {
		platforms[i] = p.WithSettings(byKey[p.Key])
	}
	return platforms
}

// UpdatePlatform changes how a platform the user has looks, and its name on all its holdings. A name
// that's the same platform (case aside) respells it; one of another of their platforms merges them, only
// with MergeIfExists (that one keeps its look, the rest of the request is left out); a new one takes the
// platform's look with it.
func (s *PlatformService) UpdatePlatform(ctx context.Context, command inbound.UpdatePlatformCommand) (model.Platform, error) {
	key, err := model.ParsePlatformKey(command.Id)
	if err != nil {
		return model.Platform{}, platformNotFound()
	}
	var name *model.PlatformName
	if command.Name != nil {
		n, err := model.NewPlatformName(*command.Name)
		if err != nil {
			return model.Platform{}, err
		}
		name = &n
	}
	var pType *model.PlatformType
	if v := command.Type.Value; v != nil && *v != "" {
		pt, err := model.NewPlatformType(*v)
		if err != nil {
			return model.Platform{}, err
		}
		pType = &pt
	}
	var avatar *string
	if v := command.AvatarText.Value; v != nil {
		text, err := model.NewAvatarText(*v)
		if err != nil {
			return model.Platform{}, err
		}
		avatar = &text
	}
	color, err := optionalColor(command.Color.Value)
	if err != nil {
		return model.Platform{}, err
	}
	now := s.clock()

	finalKey := key
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		names, err := s.platformRepo.Names(ctx, command.UserId)
		if err != nil {
			return err
		}
		current, ok := names[key]
		if !ok {
			return platformNotFound()
		}
		stored, err := s.settingsRepo.Find(ctx, command.UserId, key)
		if err != nil {
			return err
		}
		updated := model.PlatformSettings{UserId: command.UserId, Key: key}
		if stored != nil {
			updated = *stored
		}
		if command.Type.Set {
			updated.Type = pType
		}
		if command.AvatarText.Set {
			updated.AvatarText = avatar
		}
		if command.Color.Set {
			updated.Color = color
		}
		updated.UpdatedAt = now

		if name == nil || model.PlatformKey(*name) == key {
			if name != nil && *name != current {
				if _, err := s.holdingRepo.ReassignPlatform(ctx, command.UserId, key, *name); err != nil {
					return err
				}
			}
			return s.write(ctx, command.UserId, platformDoc{before: stored, after: keptPlatform(updated)})
		}

		newKey := model.PlatformKey(*name)
		finalKey = newKey
		if existing, taken := names[newKey]; taken {
			if !command.MergeIfExists {
				return appErrors.NewConflictError("platform-exists",
					fmt.Sprintf("There's already a platform named %q", existing.Value()))
			}
			if _, err := s.holdingRepo.ReassignPlatform(ctx, command.UserId, key, existing); err != nil {
				return err
			}
			return s.write(ctx, command.UserId, platformDoc{before: stored})
		}

		// A new name: the platform's look goes with it (over one kept from when a platform had that name).
		if _, err := s.holdingRepo.ReassignPlatform(ctx, command.UserId, key, *name); err != nil {
			return err
		}
		replaced, err := s.settingsRepo.Find(ctx, command.UserId, newKey)
		if err != nil {
			return err
		}
		moved := updated
		moved.Key = newKey
		return s.write(ctx, command.UserId, platformDoc{before: stored}, platformDoc{before: replaced, after: keptPlatform(moved)})
	})
	if err != nil {
		return model.Platform{}, err
	}

	platforms, err := s.GetAllPlatforms(ctx, command.UserId)
	if err != nil {
		return model.Platform{}, err
	}
	for _, p := range platforms {
		if p.Key == finalKey {
			return p, nil
		}
	}
	return model.Platform{}, platformNotFound() // its holdings were removed meanwhile
}

// platformDoc is a change to one platform's settings document: what it was (nil: none) and what it becomes
// (nil: none).
type platformDoc struct {
	before, after *model.PlatformSettings
}

// keptPlatform is the document to keep for these settings: none if they're all the default.
func keptPlatform(settings model.PlatformSettings) *model.PlatformSettings {
	if !settings.Customized() {
		return nil
	}
	return &settings
}

// write saves these changes (leaving alone a document that wouldn't change), counting the documents they
// add or remove against the user's cap first.
func (s *PlatformService) write(ctx context.Context, userId model.UserId, changes ...platformDoc) error {
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
		ok, err := s.quotaRepo.Reserve(ctx, userId, platformSettingsQuota, added, model.MaxPlatformSettingsPerUser)
		if err != nil {
			return err
		}
		if !ok {
			return errPlatformsLimit
		}
	}
	for _, c := range changes {
		switch {
		case c.after != nil && (c.before == nil || !samePlatformSettings(*c.before, *c.after)):
			if err := s.settingsRepo.Save(ctx, *c.after); err != nil {
				return err
			}
		case c.after == nil && c.before != nil:
			if _, err := s.settingsRepo.Delete(ctx, userId, c.before.Key); err != nil {
				return err
			}
		}
	}
	if added < 0 {
		return s.quotaRepo.Release(ctx, userId, platformSettingsQuota, -added)
	}
	return nil
}

func samePlatformSettings(a, b model.PlatformSettings) bool {
	sameType := a.Type == nil && b.Type == nil || a.Type != nil && b.Type != nil && *a.Type == *b.Type
	sameText := a.AvatarText == nil && b.AvatarText == nil || a.AvatarText != nil && b.AvatarText != nil && *a.AvatarText == *b.AvatarText
	return a.Key == b.Key && sameType && sameText && sameColor(a.Color, b.Color)
}
