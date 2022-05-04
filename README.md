# cluster-bootstrap-controller

This is a controller that tracks [GitopsCluster] objects.

It provides a CR for a `ClusterBootstrapConfig` which provides a [Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/) template.

When a GitopsCluster is "Ready" a Job is created from the template, the template can access multiple fields.

```yaml
apiVersion: capi.weave.works/v1alpha1
kind: ClusterBootstrapConfig
metadata:
  name: clusterbootstrapconfig-sample
  namespace: default
spec:
  clusterSelector:
    matchLabels:
      demo: "true"
  jobTemplate:
    generateName: 'run-wego-{{ .ObjectMeta.Name }}'
    spec:
      containers:
      - image: alpine:latest
        name: cat-kubeconfig
        volumeMounts:
        - name: kubeconfig
          mountPath: "/etc/wego"
          readOnly: true
        command: ["cat", "/etc/wego/value"]
      restartPolicy: Never
      volumes:
      - name: kubeconfig
        secret:
          secretName: '{{ .ObjectMeta.Name }}-kubeconfig'
```

This is using Go [templating](https://pkg.go.dev/text/template) and the `GitopsCluster` object is provided as the context, this means that expressions like `{{ .ObjectMeta.Name }}` will get the _name_ of the GitopsCluster that has transitioned to "Ready".

## Annotations

Go templating doesn't easily support access to strings that have "/" in them,
which is a common annotation [naming strategy](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/#syntax-and-character-set).

The templating provides a [function](https://pkg.go.dev/text/template#hdr-Functions) called "annotation", this can accept a string annotation which can access an annotation.

e.g.

```yaml
      volumes:
      - name: kubeconfig
        secret:
          secretName: '{{ annotation "example.com/secret-name }}'

```

## Installation

Release files are available https://github.com/weaveworks/cluster-bootstrap-controller/releases

You can install these e.g.

```shell
$ kubectl apply -f https://github.com/weaveworks/cluster-bootstrap-controller/releases/download/v0.0.5/cluster-bootstrap-controller-v0.0.5.yaml
```

