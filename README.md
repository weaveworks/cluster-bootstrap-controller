# cluster-bootstrap-controller

This is a controller that tracks [CAPI](https://github.com/kubernetes-sigs/cluster-api) [Cluster](https://cluster-api.sigs.k8s.io/developer/architecture/controllers/cluster.html) objects.

It provides a CR for a `ClusterBootstrapConfig` which provides a [Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/) template.

When a CAPI Cluster is "provisioned" a Job is created from the template, the
template can access multiple fields.

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

This is using Go [templating](https://pkg.go.dev/text/template) and the
`Cluster` object is provided as the context, this means that expressions like
`{{ .ObjectMeta.Name }}` will get the _name_ of the Cluster that has
transitioned to "provisioned".

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
