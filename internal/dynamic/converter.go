package dynamic

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ObjFromJson takes a string and returns an unstructered object
func ObjFromJson(json string) (*unstructured.UnstructuredList, error) {
	obj := &unstructured.UnstructuredList{}
	_, _, err := serializer.Decode([]byte(json), nil, obj)
	if err != nil {
		return nil, err
	}

	return obj, nil
}
