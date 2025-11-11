package testutil_test

import (
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"

	"github.com/vshn/espejote/testutil"
)

func Test_GatherAndCompareInEpsilon(t *testing.T) {
	g := &mockGatherer{
		metrics: `
# HELP test_metric A test metric
# TYPE test_metric gauge
test_metric{label="a"} 102
test_metric{label="b"} 202
		`,
	}
	e := `
# HELP test_metric A test metric
# TYPE test_metric gauge
test_metric{label="a"} 100
test_metric{label="b"} 200
`

	assert.Error(t, testutil.GatherAndCompareInEpsilon(g, strings.NewReader(e), 0.01, "test_metric"), "expected error due to epsilon exceeded")
	assert.NoError(t, testutil.GatherAndCompareInEpsilon(g, strings.NewReader(e), 0.02, "test_metric"), "expected no error within epsilon (exact match)")
	assert.NoError(t, testutil.GatherAndCompareInEpsilon(g, strings.NewReader(e), 0.05, "test_metric"), "expected no error within epsilon")
}

func Test_GatherAndCompareInEpsilon_MissingMetricLabelPair(t *testing.T) {
	g := &mockGatherer{
		metrics: `
# HELP test_metric A test metric
# TYPE test_metric gauge
test_metric{label="a"} 100
		`,
	}
	e := `
# HELP test_metric A test metric
# TYPE test_metric gauge
test_metric{label="a"} 100
test_metric{label="missing-label"} 200
`

	assert.ErrorContains(t, testutil.GatherAndCompareInEpsilon(g, strings.NewReader(e), 0.01, "test_metric"), "missing-label")
}

func Test_GatherAndCompareInEpsilon_MissingMetric(t *testing.T) {
	g := &mockGatherer{
		metrics: `
# HELP test_metric A test metric
# TYPE test_metric gauge
test_metric{label="a"} 100
		`,
	}
	e := `
# HELP test_metric A test metric
# TYPE test_metric gauge
test_metric{label="a"} 100
# HELP other_metric Another test metric
# TYPE other_metric gauge
other_metric{label="a"} 100
`

	assert.ErrorContains(t, testutil.GatherAndCompareInEpsilon(g, strings.NewReader(e), 0.01, "test_metric", "other_metric"), "other_metric")
}

func Test_GatherAndCompareInEpsilon_DifferingType(t *testing.T) {
	g := &mockGatherer{
		metrics: `
# HELP test_metric A test metric
# TYPE test_metric counter
test_metric{label="a"} 100
		`,
	}
	e := `
# HELP test_metric A test metric
# TYPE test_metric gauge
test_metric{label="a"} 100
`

	assert.ErrorContains(t, testutil.GatherAndCompareInEpsilon(g, strings.NewReader(e), 0.01, "test_metric", "other_metric"), "type GAUGE in expected metrics but COUNTER in actual metrics")
}

func Test_GatherAndCompareInEpsilon_ExpectedZero(t *testing.T) {
	g := &mockGatherer{
		metrics: `
# HELP test_metric A test metric
# TYPE test_metric gauge
test_metric{label="a"} 0
		`,
	}
	e := `
# HELP test_metric A test metric
# TYPE test_metric gauge
test_metric{label="a"} 0
`

	assert.ErrorContains(t, testutil.GatherAndCompareInEpsilon(g, strings.NewReader(e), 0.01, "test_metric", "other_metric"), "other than zero")
}

type mockGatherer struct {
	metrics string
}

func (m *mockGatherer) Gather() ([]*dto.MetricFamily, error) {
	tp := expfmt.NewTextParser(model.UTF8Validation)
	metrics, err := tp.TextToMetricFamilies(strings.NewReader(m.metrics))

	if err != nil {
		return nil, err
	}

	metricFamilies := make([]*dto.MetricFamily, 0, len(metrics))
	for _, mf := range metrics {
		metricFamilies = append(metricFamilies, mf)
	}
	return metricFamilies, nil
}
