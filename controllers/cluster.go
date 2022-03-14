package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsControlPlaneReady takes a client connected to a cluster and reports whether or
// not the control-plane for the cluster is "ready".
func IsControlPlaneReady(ctx context.Context, cl client.Client) (bool, error) {
	nodes := &corev1.NodeList{}
	// https://github.com/kubernetes/enhancements/blob/master/keps/sig-cluster-lifecycle/kubeadm/2067-rename-master-label-taint/README.md#design-details
	err := cl.List(ctx, nodes, client.HasLabels([]string{"node-role.kubernetes.io/control-plane"}))
	if err != nil {
		return false, fmt.Errorf("failed to query cluster node list: %w", err)
	}

	readiness := []bool{}
	for _, node := range nodes.Items {
		for _, c := range node.Status.Conditions {
			switch c.Type {
			case corev1.NodeReady:
				readiness = append(readiness, c.Status == corev1.ConditionTrue)
			}
		}
	}

	isReady := func(bools []bool) bool {
		for _, v := range bools {
			if !v {
				return false
			}
		}
		return true
	}

	// If we have no statuses, then we really don't know if we're ready or not.
	return (len(readiness) > 0 && isReady(readiness)), nil
}
