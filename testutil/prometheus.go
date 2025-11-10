package testutil

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// GatherAndCompareInEpsilon gathers metrics from the provided Gatherer and compares
// them against the expected metrics read from expectedReader. The comparison is done
// only for the specified metricNames, and allows for a relative error defined by epsilon.
// If the relative error between expected and actual metric values exceeds epsilon, an error is returned.
// Does not validate the HELP line, does not support histogram or summary metric types, and does not error on extra metrics in gathered metrics.
// Does return an error if the expected value is zero.
func GatherAndCompareInEpsilon(g prometheus.Gatherer, expectedReader io.Reader, epsilon float64, metricNames ...string) error {
	tg := prometheus.ToTransactionalGatherer(g)

	mto, done, err := tg.Gather()
	defer done()
	if err != nil {
		return fmt.Errorf("gathering metrics failed: %w", err)
	}

	tp := expfmt.NewTextParser(model.UTF8Validation)
	expected, err := tp.TextToMetricFamilies(expectedReader)
	if err != nil {
		return fmt.Errorf("converting reader to metric families failed: %w", err)
	}

	actual := slices.DeleteFunc(mto, func(mf *dto.MetricFamily) bool {
		return !slices.Contains(metricNames, mf.GetName())
	})
	maps.DeleteFunc(expected, func(name string, mf *dto.MetricFamily) bool {
		return !slices.Contains(metricNames, name)
	})

	for expectedName, expectedMF := range expected {
		amfs := findMetricFamilyByName(actual, expectedName)
		if len(amfs) == 0 {
			return fmt.Errorf("expected metric family %q not found in actual metrics", expectedName)
		} else if len(amfs) > 1 {
			return fmt.Errorf("expected metric family %q found multiple times in actual metrics", expectedName)
		}
		amf := amfs[0]

		if expectedMF.GetType() != amf.GetType() {
			return fmt.Errorf("metric family %q has type %v in expected metrics but %v in actual metrics", expectedName, expectedMF.GetType(), amf.GetType())
		}

		for _, em := range expectedMF.Metric {
			ams := findMetricByLabelPairs(amf, em.GetLabel())
			if len(ams) == 0 {
				return fmt.Errorf("expected metric with labels %v not found in actual metrics for metric family %q", em.GetLabel(), expectedName)
			} else if len(ams) > 1 {
				return fmt.Errorf("expected metric with labels %v found multiple times in actual metrics for metric family %q", em.GetLabel(), expectedName)
			}
			am := ams[0]

			var ev, av float64
			switch expectedMF.GetType() {
			case dto.MetricType_COUNTER:
				ev = em.GetCounter().GetValue()
				av = am.GetCounter().GetValue()
			case dto.MetricType_GAUGE:
				ev = em.GetGauge().GetValue()
				av = am.GetGauge().GetValue()
			case dto.MetricType_UNTYPED:
				ev = em.GetUntyped().GetValue()
				av = am.GetUntyped().GetValue()
			default:
				return fmt.Errorf("metric family %q has unsupported type %v for comparison", expectedName, expectedMF.GetType())
			}
			relErr, err := calcRelativeError(ev, av)
			if err != nil {
				return fmt.Errorf("calculating relative error for metric family %q with labels %v failed: %w", expectedName, em.GetLabel(), err)
			}
			if relErr > epsilon {
				return fmt.Errorf("metric family %q with labels %v has value %v in expected metrics but %v in actual metrics, which exceeds the allowed relative error of %v (actual relative error: %v)", expectedName, em.GetLabel(), ev, av, epsilon, relErr)
			}
		}
	}

	return nil
}

func calcRelativeError(expected, actual float64) (float64, error) {
	if math.IsNaN(expected) && math.IsNaN(actual) {
		return 0, nil
	}
	if math.IsNaN(expected) {
		return 0, errors.New("expected value must not be NaN")
	}
	if expected == 0 {
		return 0, fmt.Errorf("expected value must have a value other than zero to calculate the relative error")
	}
	if math.IsNaN(actual) {
		return 0, errors.New("actual value must not be NaN")
	}

	return math.Abs(expected-actual) / math.Abs(expected), nil
}

func labelPairsAreEqual(a, b []*dto.LabelPair) bool {
	if len(a) != len(b) {
		return false
	}

	sf := func(a, b *dto.LabelPair) int {
		return 10*strings.Compare(a.GetName(), b.GetName()) + strings.Compare(a.GetValue(), b.GetValue())
	}
	aSorted := slices.Clone(a)
	bSorted := slices.Clone(b)
	slices.SortFunc(aSorted, sf)
	slices.SortFunc(bSorted, sf)
	for i := range aSorted {
		if aSorted[i].GetName() != bSorted[i].GetName() || aSorted[i].GetValue() != bSorted[i].GetValue() {
			return false
		}
	}
	return true
}

func findMetricByLabelPairs(mf *dto.MetricFamily, lp []*dto.LabelPair) []*dto.Metric {
	var result []*dto.Metric
	for _, m := range mf.Metric {
		if labelPairsAreEqual(m.GetLabel(), lp) {
			result = append(result, m)
		}
	}
	return result
}

func findMetricFamilyByName(mfs []*dto.MetricFamily, name string) []*dto.MetricFamily {
	var result []*dto.MetricFamily
	for _, mf := range mfs {
		if mf.GetName() == name {
			result = append(result, mf)
		}
	}
	return result
}
