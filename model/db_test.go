package model_test

import (
	"go-api-server/model"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func init() {
	model.InitDB("file::memory:?cache=shared")
}

func TestInsertAndFetch(t *testing.T) {
	expire := time.Now().Add(24 * time.Hour)
	err := model.InsertURLMapping("https://foo.com", "foo123", "foo123", &expire)
	assert.NoError(t, err)

	url, ex, err := model.GetLongURLWithExpiry("foo123")
	assert.NoError(t, err)
	assert.Equal(t, "https://foo.com", url)
	assert.NotNil(t, ex)
}
