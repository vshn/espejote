package dynamic

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

type Client struct {
	discovery *discovery.DiscoveryClient
	dynamic   *dynamic.DynamicClient
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

func (c *Client) ApplyJsonWithGVK(ctx context.Context, namespace, manifest string) error {
	// Unmarshal manifest into object
	data := []byte(manifest)
	// obj := &unstructured.Unstructured{}
	// if err := json.Unmarshal(data, obj); err != nil {
	// 	return err
	// }
	_, gvk, err := serializer.Decode(data, nil, &unstructured.Unstructured{})
	if err != nil {
		return err
	}

	dr, err := c.Resource(fmt.Sprintf("%s/%s", gvk.Group, gvk.Version), gvk.Kind, namespace)
	if err != nil {
		fmt.Println("EOR", err)
		return err
	}

	_, err = dr.Patch(ctx, "", types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "espejote",
	})

	return err
}

func (c *Client) ApplyJson(ctx context.Context, manifest string) error {
	// Unmarshal manifest into object
	data := []byte(manifest)
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, obj); err != nil {
		return err
	}

	apiVersion := obj.GetAPIVersion()
	kind := obj.GetKind()
	namespace := obj.GetNamespace()

	dr, err := c.Resource(apiVersion, kind, namespace)
	if err != nil {
		return err
	}

	_, err = dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "espejote",
	})
	return err
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

func (c *Client) Resource(apiVersion, kind, namespace string) (dynamic.ResourceInterface, error) {
	gk := schema.FromAPIVersionAndKind(apiVersion, kind).GroupKind()
	v := schema.FromAPIVersionAndKind(apiVersion, kind).Version

	// Prepare a RESTMapper to find GVR
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(c.discovery))

	// Find GVR
	mapping, err := mapper.RESTMapping(gk, v)
	if err != nil {
		return nil, err
	}

	// Obtain REST interface for the GVR
	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		// namespaced resources should specify the namespace
		dr = c.dynamic.Resource(mapping.Resource).Namespace(namespace)
	} else {
		// for cluster-wide resources
		dr = c.dynamic.Resource(mapping.Resource)
	}

	return dr, err
}

func (c *Client) ResourceTest(apiVersion, kind, namespace string) (dynamic.ResourceInterface, error) {
	// Find GVR
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, err
	}
	gvr := gv.WithResource(kind)

	// Obtain REST interface for the GVR
	var dr dynamic.ResourceInterface
	if namespace != "" {
		// namespaced resources should specify the namespace
		dr = c.dynamic.Resource(gvr).Namespace(namespace)
	} else {
		// for cluster-wide resources
		dr = c.dynamic.Resource(gvr)
	}

	return dr, err
}
