package metrics

import (
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsMetricsAPIUnavailable(t *testing.T) {
	gvk := schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes"}
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: gvk.Group, Resource: gvk.Resource}, "nodes")
	if !isMetricsAPIUnavailable(notFound) {
		t.Fatalf("expected notFound to be treated as unavailable")
	}

	err := errors.New("the server could not find the requested resource (get nodes.metrics.k8s.io)")
	if !isMetricsAPIUnavailable(err) {
		t.Fatalf("expected string error to be treated as unavailable")
	}

	other := errors.New("boom")
	if isMetricsAPIUnavailable(other) {
		t.Fatalf("expected generic error to not be treated as unavailable")
	}
}
