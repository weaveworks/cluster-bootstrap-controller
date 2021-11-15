package controllers

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/go-logr/logr"
	capiv1alpha1 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha1"
)

// bootstrapCluster applies the jobs from a ClusterBootstrapConfig to a cluster.
func bootstrapClusterWithConfig(ctx context.Context, logger logr.Logger, c client.Client, cl *clusterv1.Cluster, bc *capiv1alpha1.ClusterBootstrapConfig) error {
	job, err := renderTemplates(cl, jobFromTemplate(cl, bc.Spec.Template))
	if err != nil {
		return fmt.Errorf("failed to render job from template: %w", err)
	}
	if err := controllerutil.SetOwnerReference(cl, job, c.Scheme()); err != nil {
		return fmt.Errorf("failed to set owner for job: %w", err)
	}
	logger.Info("creating job", "generate-name", job.ObjectMeta.GenerateName, "namespace", job.ObjectMeta.Namespace)
	if err := c.Create(ctx, job); err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}
	return nil
}

func jobFromTemplate(cl *clusterv1.Cluster, jt capiv1alpha1.JobTemplate) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jt.GenerateName,
			Namespace:    cl.ObjectMeta.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: jt.Spec,
			},
			BackoffLimit: jt.BackoffLimit,
		},
	}
}
