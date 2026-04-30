package geoip_test

import (
	"testing"

	"github.com/btopcu/argus/internal/geoip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_EmptyPath_NoOp(t *testing.T) {
	l, err := geoip.New("")
	require.NoError(t, err)
	assert.NotNil(t, l)
	result := l.Lookup("8.8.8.8")
	assert.Nil(t, result, "empty path should return nil location without error")
}

func TestLookup_NilOnInvalidIP(t *testing.T) {
	l, err := geoip.New("")
	require.NoError(t, err)
	result := l.Lookup("not-an-ip")
	assert.Nil(t, result)
}

func TestLookup_NilOnEmptyIP(t *testing.T) {
	l, err := geoip.New("")
	require.NoError(t, err)
	result := l.Lookup("")
	assert.Nil(t, result)
}

func TestNew_BadPath_Error(t *testing.T) {
	_, err := geoip.New("/nonexistent/path/GeoLite2-City.mmdb")
	assert.Error(t, err)
}
