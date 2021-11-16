package controllers

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	capiv1alpha1 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha1"
	"github.com/weaveworks/cluster-bootstrap-controller/test"

	"github.com/google/go-cmp/cmp"
)

func Test_renderTemplates(t *testing.T) {
	testCluster := makeTestCluster(
		func(cl *clusterv1.Cluster) {
			cl.ObjectMeta.Annotations = map[string]string{"gitops.solutions/my-test": "just-a-value"}
			cl.ObjectMeta.Labels = map[string]string{"gitops.solutions/my-label": "label-value"}
		},
	)
	renderTests := []struct {
		name string
		job  capiv1alpha1.JobTemplate
		want *batchv1.Job
	}{
		{"no template values", makeTestJobTemplate("testing"), jobFromTemplate(testCluster, makeTestJobTemplate("testing"))},
		{"annotation values", makeTestJobTemplate(`{{ annotation "gitops.solutions/my-test" }}`), jobFromTemplate(testCluster, makeTestJobTemplate("just-a-value"))},
		{"label values", makeTestJobTemplate(`{{ label "gitops.solutions/my-label" }}`), jobFromTemplate(testCluster, makeTestJobTemplate("label-value"))},
		{"data value", makeTestJobTemplate(`{{ .ObjectMeta.Name }}`), jobFromTemplate(testCluster, makeTestJobTemplate(testCluster.ObjectMeta.Name))},
		{"missing element", makeTestJobTemplate(`{{ label "unknown" }}`), jobFromTemplate(testCluster, makeTestJobTemplate(""))},
	}

	for _, tt := range renderTests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := renderTemplates(testCluster, jobFromTemplate(testCluster, tt.job))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, rendered); diff != "" {
				t.Fatalf("failed to render templates:\n%s", diff)
			}
		})
	}
}

func Test_renderTemplates_with_errors(t *testing.T) {
	testCluster := makeTestCluster(
		func(cl *clusterv1.Cluster) {
			cl.ObjectMeta.Annotations = map[string]string{"gitops.solutions/my-test": "!!int"}
		},
	)
	renderTests := []struct {
		name    string
		job     capiv1alpha1.JobTemplate
		wantErr string
	}{
		{"invalid syntax", makeTestJobTemplate("{{ abels}"), "failed to parse template: template: job"},
		{"invalid template", makeTestJobTemplate("{{ annotation }}"), "failed to execute template.*wrong number of args for annotation"},
	}

	for _, tt := range renderTests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := renderTemplates(testCluster, jobFromTemplate(testCluster, tt.job))
			if r != nil {
				t.Error("error case returned a template")
			}
			test.AssertErrorMatch(t, tt.wantErr, err)
		})
	}
}

func makeTestJobTemplate(s string) capiv1alpha1.JobTemplate {
	return capiv1alpha1.JobTemplate{
		GenerateName: "setup-something",
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "test",
					Command: []string{"ls", s},
				},
			},
		},
	}
}
