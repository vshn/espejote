package controllers

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

const MetricsNamespace = "espejote"

var (
	reconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "reconcile_errors_total",
			Help:      "Total number of errors encountered during reconciliation by error kind and trigger.",
		},
		[]string{"managedresource", "namespace", "trigger", "error_kind"},
	)

	reconciles = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "reconciles_total",
			Help:      "Total number of reconciles by trigger.",
		},
		[]string{"managedresource", "namespace", "trigger"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		reconcileErrors,
		reconciles,
	)
}

var (
	cachedObjectsDesc = prometheus.NewDesc(
		MetricsNamespace+"_cached_objects",
		"Number of objects in the cache.",
		[]string{"managedresource", "namespace", "type", "name"},
		nil,
	)
	cacheSizeBytesDesc = prometheus.NewDesc(
		MetricsNamespace+"_cache_size_bytes",
		"Size of the cache in bytes. Note that this is an approximation. The metric should not be compared across different espejote versions.",
		[]string{"managedresource", "namespace", "type", "name"},
		nil,
	)
)

// CacheSizeCollector collects cache size metrics.
// It loops over all caches and collects the number of cached objects and the size of the cache.
type CacheSizeCollector struct {
	ControllerManager *ManagedResourceControllerManager
}

// Describe implements the prometheus.Collector interface.
func (c *CacheSizeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- cachedObjectsDesc
	ch <- cacheSizeBytesDesc
}

// Collect implements the prometheus.Collector interface.
func (c *CacheSizeCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	for mr, c := range c.shallowCloneCachesWithLock() {
		for cn, cv := range c.contextCaches {
			count, sizeBytes, err := cv.Size(ctx)
			if err != nil {
				fmt.Printf("Metrics: Error getting context cache size %s/%s/%s: %s\n", mr.Name, mr.Namespace, cn, err.Error())
				continue
			}
			ch <- prometheus.MustNewConstMetric(
				cachedObjectsDesc,
				prometheus.GaugeValue,
				float64(count),
				mr.Name,
				mr.Namespace,
				"context",
				cn,
			)
			ch <- prometheus.MustNewConstMetric(
				cacheSizeBytesDesc,
				prometheus.GaugeValue,
				float64(sizeBytes),
				mr.Name,
				mr.Namespace,
				"context",
				cn,
			)
		}
		for tn, tv := range c.triggerCaches {
			count, sizeBytes, err := tv.Size(ctx)
			if err != nil {
				fmt.Printf("Metrics: Error getting trigger cache size %s/%s/%s: %s\n", mr.Name, mr.Namespace, tn, err.Error())
				continue
			}
			ch <- prometheus.MustNewConstMetric(
				cachedObjectsDesc,
				prometheus.GaugeValue,
				float64(count),
				mr.Name,
				mr.Namespace,
				"trigger",
				tn,
			)
			ch <- prometheus.MustNewConstMetric(
				cacheSizeBytesDesc,
				prometheus.GaugeValue,
				float64(sizeBytes),
				mr.Name,
				mr.Namespace,
				"trigger",
				tn,
			)
		}
	}
}

func (c *CacheSizeCollector) shallowCloneCachesWithLock() map[types.NamespacedName]*instanceCache {
	c.ControllerManager.controllersMux.RLock()
	defer c.ControllerManager.controllersMux.RUnlock()

	cloned := make(map[types.NamespacedName]*instanceCache)
	for k, v := range c.ControllerManager.controllers {
		if c := v.reconciler.cache; c != nil {
			cloned[k] = c
		}
	}
	return cloned
}

var (
	espejoteManagedResourceStatusDesc = prometheus.NewDesc(
		MetricsNamespace+"_managedresource_status",
		"Status of the managed resource. Read from the resources .status.status field.",
		[]string{"managedresource", "namespace", "status"},
		nil,
	)

	espejoteManagedResourceStatusReadyDesc = prometheus.NewDesc(
		MetricsNamespace+"_managedresource_status_ready",
		"Ready status of the managed resource. 1 if ready, 0 if any other status. Read from the resources .status.status field.",
		[]string{"managedresource", "namespace"},
		nil,
	)
)

// ManagedResourceStatusCollector collects status metrics for managed resources.
// It loops over all managed resources and collects the infomation in the .status field.
type ManagedResourceStatusCollector struct {
	client.Reader
}

// Describe implements the prometheus.Collector interface.
func (c *ManagedResourceStatusCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- espejoteManagedResourceStatusDesc
	ch <- espejoteManagedResourceStatusReadyDesc
}

// Collect implements the prometheus.Collector interface.
func (c *ManagedResourceStatusCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	var mrs espejotev1alpha1.ManagedResourceList
	if err := c.Reader.List(ctx, &mrs); err != nil {
		ch <- prometheus.NewInvalidMetric(espejoteManagedResourceStatusDesc, err)
		ch <- prometheus.NewInvalidMetric(espejoteManagedResourceStatusReadyDesc, err)
		return
	}
	for _, mr := range mrs.Items {
		ch <- prometheus.MustNewConstMetric(
			espejoteManagedResourceStatusDesc,
			prometheus.GaugeValue,
			1,
			mr.Name,
			mr.Namespace,
			string(mr.Status.Status),
		)

		ready := 0
		if mr.Status.Status == "Ready" {
			ready = 1
		}
		ch <- prometheus.MustNewConstMetric(
			espejoteManagedResourceStatusReadyDesc,
			prometheus.GaugeValue,
			float64(ready),
			mr.Name,
			mr.Namespace,
		)
	}
}
