package controllers

import (
	"bytes"
	"fmt"
	"text/template"

	batchv1 "k8s.io/api/batch/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/yaml"
)

func lookup(m map[string]string) func(s string) string {
	return func(s string) string {
		return m[s]
	}
}

func makeFuncMap(cl *clusterv1.Cluster) template.FuncMap {
	return template.FuncMap{
		"annotation": lookup(cl.ObjectMeta.GetAnnotations()),
		"label":      lookup(cl.ObjectMeta.GetLabels()),
	}
}

func renderTemplates(cl *clusterv1.Cluster, j *batchv1.Job) (*batchv1.Job, error) {
	raw, err := yaml.Marshal(j)
	if err != nil {
		return nil, fmt.Errorf("failed to parse job as YAML: %w", err)
	}

	tmpl, err := template.New("job").Funcs(makeFuncMap(cl)).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	buf := bytes.Buffer{}
	err = tmpl.Execute(&buf, cl)
	if err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	var updated batchv1.Job
	if err := yaml.Unmarshal(buf.Bytes(), &updated); err != nil {
		return nil, fmt.Errorf("failed to marshal job: %w", err)
	}
	return &updated, nil
}
