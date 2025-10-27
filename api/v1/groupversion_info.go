package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupVersion is group version used to register these objects.
var GroupVersion = schema.GroupVersion{Group: "rebalancer.dev", Version: "v1"}

// SchemeBuilder registers our custom resource objects with a scheme.
var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

// AddToScheme adds all registered resources to the scheme.
var AddToScheme = SchemeBuilder.AddToScheme
