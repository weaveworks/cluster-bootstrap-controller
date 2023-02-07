package controllers

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	capiv1alpha2 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha2"
	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSecretSync(t *testing.T) {
	clusterA, clusterASecret, clusterAClient := makeReadyTestCluster(t, "a")
	clusterB, clusterBSecret, clusterBClient := makeReadyTestCluster(t, "b")

	secretA := makeTestSecret(types.NamespacedName{
		Name:      "secret-a",
		Namespace: "default",
	}, map[string][]byte{"value": []byte("a")})

	secretB := makeTestSecret(types.NamespacedName{
		Name:      "secret-b",
		Namespace: "default",
	}, map[string][]byte{"value": []byte("b")})

	secretSyncA := makeSecretSync(
		"secretsync-a",
		secretA.GetNamespace(),
		secretA.GetName(),
		"ns-a",
		map[string]string{"environment": "a"},
	)

	secretSyncB := makeSecretSync(
		"secretsync-b",
		secretB.GetNamespace(),
		secretB.GetName(),
		"ns-b",
		map[string]string{"environment": "b"},
	)

	sc, cl := makeTestClientAndScheme(
		t, clusterA, clusterB,
		clusterASecret, clusterBSecret,
		secretA, secretB,
		secretSyncA, secretSyncB,
	)

	reconciler := NewSecretSyncReconciler(cl, sc)
	reconciler.configParser = func(b []byte) (client.Client, error) {
		clusters := map[string]client.Client{
			"a": clusterAClient,
			"b": clusterBClient,
		}
		return clusters[string(b)], nil
	}

	t.Run("create SecretSync a to sync secret a to leaf cluster a in namespace ns-a", func(t *testing.T) {
		if _, err := reconciler.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{
			Name:      secretSyncA.GetName(),
			Namespace: secretSyncA.GetNamespace(),
		}}); err != nil {
			t.Fatal(err)
		}

		var secret v1.Secret
		if err := clusterAClient.Get(context.TODO(), client.ObjectKey{Name: "secret-a", Namespace: "ns-a"}, &secret); err != nil {
			t.Fatal(err)
		}

		var secretSync capiv1alpha2.SecretSync
		if err := cl.Get(context.TODO(), client.ObjectKeyFromObject(secretSyncA), &secretSync); err != nil {
			t.Fatal(err)
		}

		if _, ok := secretSync.Status.SecretVersions[fmt.Sprintf("%s/%s", clusterA.Name, secretA.Name)]; !ok {
			t.Fatalf("secretsync a status is not updated")
		}

		var secretb v1.Secret
		if err := clusterAClient.Get(context.TODO(), client.ObjectKey{Name: "secret-b", Namespace: "ns-b"}, &secretb); err == nil {
			t.Fatal("secret b found in cluster a")
		}
	})

	t.Run("create SecretSync a to sync secret a to leaf cluster a in namespace ns-a", func(t *testing.T) {
		if _, err := reconciler.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{
			Name:      secretSyncB.GetName(),
			Namespace: secretSyncB.GetNamespace(),
		}}); err != nil {
			t.Fatal(err)
		}

		var secret v1.Secret
		if err := clusterBClient.Get(context.TODO(), client.ObjectKey{Name: "secret-b", Namespace: "ns-b"}, &secret); err != nil {
			t.Fatal(err)
		}

		var secretSync capiv1alpha2.SecretSync
		if err := cl.Get(context.TODO(), client.ObjectKeyFromObject(secretSyncB), &secretSync); err != nil {
			t.Fatal(err)
		}

		if _, ok := secretSync.Status.SecretVersions[fmt.Sprintf("%s/%s", clusterB.Name, secretB.Name)]; !ok {
			t.Fatalf("secretsync a status is not updated")
		}

		var secreta v1.Secret
		if err := clusterBClient.Get(context.TODO(), client.ObjectKey{Name: "secret-a", Namespace: "ns-a"}, &secreta); err == nil {
			t.Fatal("secret a found in cluster b")
		}
	})

	t.Run("update secret a. Secret a on cluster a should be updated too", func(t *testing.T) {
		secretA.Data["value"] = []byte("aaaa")
		if err := cl.Update(context.TODO(), secretA); err != nil {
			t.Fatal(err)
		}

		if _, err := reconciler.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{
			Name:      secretSyncA.GetName(),
			Namespace: secretSyncA.GetNamespace(),
		}}); err != nil {
			t.Fatal(err)
		}

		var secret v1.Secret
		if err := clusterAClient.Get(context.TODO(), client.ObjectKey{Name: "secret-a", Namespace: "ns-a"}, &secret); err != nil {
			t.Fatal(err)
		}
		if bytes.Compare(secret.Data["value"], []byte("aaaa")) != 0 {
			t.Fatal("secret a is not update")
		}
	})
}

func makeSecretSync(name, namespace, secretName, targetNamespace string, selector map[string]string) *capiv1alpha2.SecretSync {
	return &capiv1alpha2.SecretSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: capiv1alpha2.SecretSyncSpec{
			ClusterSelector: metav1.LabelSelector{
				MatchLabels: selector,
			},
			SecretRef: v1.LocalObjectReference{
				Name: secretName,
			},
			TargetNamespace: targetNamespace,
		},
	}
}

func makeReadyTestCluster(t *testing.T, key string) (*gitopsv1alpha1.GitopsCluster, *v1.Secret, client.Client) {
	cluster := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.Name = fmt.Sprintf("cluster-%s", key)
		c.Namespace = corev1.NamespaceDefault
		c.SetLabels(map[string]string{
			"environment": key,
		})
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
	})

	nodeCondition := corev1.NodeCondition{
		Type:               "Ready",
		Status:             "True",
		LastHeartbeatTime:  metav1.Now(),
		LastTransitionTime: metav1.Now(), Reason: "KubeletReady",
		Message: "kubelet is posting ready status"}

	readyNode := makeNode(map[string]string{"node-role.kubernetes.io/master": ""}, nodeCondition)

	secret := makeTestSecret(types.NamespacedName{
		Name:      cluster.GetName() + "-kubeconfig",
		Namespace: cluster.GetNamespace(),
	}, map[string][]byte{"value": []byte(key)})

	_, cl := makeTestClientAndScheme(t, readyNode)

	return cluster, secret, cl
}
