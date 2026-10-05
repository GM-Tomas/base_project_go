package dto_test

import (
	"encoding/json"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptional_TellsAbsentFromNullFromAValue(t *testing.T) {
	var body struct {
		Absent dto.Optional[string]  `json:"absent"`
		Null   dto.Optional[string]  `json:"null"`
		Text   dto.Optional[string]  `json:"text"`
		Number dto.Optional[float64] `json:"number"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"null": null, "text": "", "number": 0}`), &body))

	assert.Equal(t, dto.Optional[string]{}, body.Absent)
	assert.Equal(t, dto.Optional[string]{Set: true, Null: true}, body.Null)
	assert.Equal(t, dto.Optional[string]{Set: true}, body.Text)
	assert.Equal(t, dto.Optional[float64]{Set: true}, body.Number)

	assert.Error(t, json.Unmarshal([]byte(`{"number": "ten"}`), &body))
}
