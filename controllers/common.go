package controllers

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ConfigParser func(b []byte) (client.Client, error)
