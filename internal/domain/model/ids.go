package model

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	ErrInvalidUUID = errors.New("invalid uuid")
)

// UserId represents the Supabase Auth user ID (claim sub).
type UserId struct {
	value uuid.UUID
}

func NewUserId(u uuid.UUID) UserId {
	return UserId{value: u}
}

func ParseUserId(s string) (UserId, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return UserId{}, fmt.Errorf("%w: %s", ErrInvalidUUID, s)
	}
	return UserId{value: u}, nil
}

func MustParseUserId(s string) UserId {
	u, err := ParseUserId(s)
	if err != nil {
		panic(err)
	}
	return u
}

func (id UserId) UUID() uuid.UUID {
	return id.value
}

func (id UserId) String() string {
	return id.value.String()
}

// HoldingId represents the unique identifier of a Holding.
type HoldingId struct {
	value uuid.UUID
}

func NewHoldingId() HoldingId {
	return HoldingId{value: uuid.New()}
}

func HoldingIdFromUUID(u uuid.UUID) HoldingId {
	return HoldingId{value: u}
}

func ParseHoldingId(s string) (HoldingId, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return HoldingId{}, fmt.Errorf("%w: %s", ErrInvalidUUID, s)
	}
	return HoldingId{value: u}, nil
}

func (id HoldingId) UUID() uuid.UUID {
	return id.value
}

func (id HoldingId) String() string {
	return id.value.String()
}

// SnapshotId represents the unique identifier of a NetWorthSnapshot.
type SnapshotId struct {
	value uuid.UUID
}

func NewSnapshotId() SnapshotId {
	return SnapshotId{value: uuid.New()}
}

func SnapshotIdFromUUID(u uuid.UUID) SnapshotId {
	return SnapshotId{value: u}
}

func ParseSnapshotId(s string) (SnapshotId, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return SnapshotId{}, fmt.Errorf("%w: %s", ErrInvalidUUID, s)
	}
	return SnapshotId{value: u}, nil
}

func (id SnapshotId) UUID() uuid.UUID {
	return id.value
}

func (id SnapshotId) String() string {
	return id.value.String()
}

// MovementId represents the unique identifier of a Movement.
type MovementId struct {
	value uuid.UUID
}

func NewMovementId() MovementId {
	return MovementId{value: uuid.New()}
}

func MovementIdFromUUID(u uuid.UUID) MovementId {
	return MovementId{value: u}
}

func ParseMovementId(s string) (MovementId, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return MovementId{}, fmt.Errorf("%w: %s", ErrInvalidUUID, s)
	}
	return MovementId{value: u}, nil
}

func (id MovementId) UUID() uuid.UUID {
	return id.value
}

func (id MovementId) String() string {
	return id.value.String()
}
