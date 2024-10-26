package dynamic

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Client struct {
	discovery *discovery.DiscoveryClient
	dynamic   *dynamic.DynamicClient
	typed     client.Client
}

var serializer = yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

func NewForConfig(cfg *rest.Config) (*Client, error) {
	dsc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{
		discovery: dsc,
		dynamic:   dyn,
	}, nil
}

func (c *Client) List(ctx context.Context, gvr schema.GroupVersionResource, ns string, listOpts v1.ListOptions) (*unstructured.UnstructuredList, error) {
	return c.dynamic.Resource(gvr).Namespace(ns).List(ctx, listOpts)
}

func (c *Client) Apply(ctx context.Context, yaml string) error {
	// Decode YAML manifest into unstructured.Unstructured
	obj := &unstructured.Unstructured{}
	_, _, err := serializer.Decode([]byte(yaml), nil, obj)
	if err != nil {
		return err
	}

	return c.ApplyObj(ctx, obj)
}

func (c *Client) ApplyObj(ctx context.Context, obj *unstructured.Unstructured) error {

	// Prepare a RESTMapper to find GVR
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(c.discovery))

	// Find GVR
	mapping, err := mapper.RESTMapping(obj.GroupVersionKind().GroupKind(), obj.GetAPIVersion())
	if err != nil {
		return err
	}

	// Obtain REST interface for the GVR
	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		// namespaced resources should specify the namespace
		dr = c.dynamic.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		// for cluster-wide resources
		dr = c.dynamic.Resource(mapping.Resource)
	}

	// Marshal object into JSON
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	// Create or Update the object with SSA
	// types.ApplyPatchType indicates SSA.
	// FieldManager specifies the field owner ID.
	_, err = dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "espejote",
	})

	return err
}
