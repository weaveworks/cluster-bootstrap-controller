package controllers

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigParser functions parse a byte slice and return a Kubernetes client.
type ConfigParser func(b []byte) (client.Client, error)
