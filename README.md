# mix-scheduler-admission-webhook

## Prerequisites

The cluster to test this example must be running Kubernetes 1.16.0 or later

### Initialize environment

kind.config

```yaml
# this config file contains all config fields with comments
# NOTE: this is not a particularly useful config file
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4

networking:
  # WARNING: It is _strongly_ recommended that you keep this the default
  # (127.0.0.1) for security reasons. However it is possible to change this.
  apiServerAddress: "0.0.0.0"
  # By default the API server listens on a random open port.
  # You may choose a specific port but probably don't need to in most cases.
  # Using a random port makes it easier to spin up multiple clusters.
  #apiServerPort: 6443

# patch the generated kubeadm config with some extra settings
kubeadmConfigPatches:
- |
  apiVersion: kubelet.config.k8s.io/v1beta1
  kind: KubeletConfiguration
  evictionHard:
    nodefs.available: "0%"
# patch it further using a JSON 6902 patch
kubeadmConfigPatchesJSON6902:
- group: kubeadm.k8s.io
  version: v1beta2
  kind: ClusterConfiguration
  patch: |
    - op: add
      path: /apiServer/certSANs/-
      value: my-hostname

# 1 control plane node and 11 workers
nodes:
# the control plane node config
- role: control-plane
# the three workers
- role: worker
- role: worker
- role: worker
- role: worker
- role: worker
- role: worker
- role: worker
- role: worker
- role: worker
- role: worker
- role: worker
```

```bash
kind create cluster --name k1 --config ~/config/kind/kind.config
kubectl label nodes spotnode1 node.kubernetes.io/capacity=spot
kubectl label nodes on-demandnode1 node.kubernetes.io/capacity=on-demand
```

check admissionregistration.k8s.io/v1 open API。

```bash
kubectl api-versions | grep admissionregistration.k8s.io/v1
```

In addition, the MutatingAdmissionWebhook should add and list the admission controller kube-apiserver in the admission-control flag.

To build the image, you need GNU make and Go, Docker.

## Deploy Webhook Server

Start a Kubernetes cluster that meets the above prerequisites, and make sure it is active (I. e. through configuration in the default location, or by setting KUBECONFIG environment variables).
Run./deploy.sh. This creates the CA, certificate, and private key for the webhook server and webhook-demo deploy the resources in the newly created namespace in the Kubernetes cluster.

## Verified

1.The pods in the webhook-server namespace should webhook-demo be running:

```bash
$ kubectl -n webhook-demo get pods
NAME                             READY     STATUS    RESTARTS   AGE
webhook-server-6f976f7bf-hssc9   1/1       Running   0          35m
```

2.There should be a Mutating Webhook Configuration name: demo-webhook

```bash
$ kubectl get mutatingwebhookconfigurations
NAME           AGE
demo-webhook   36m
```

3.Deploy a replicas is 1 deployment

```bash
kubectl apply -f examples/onereplicas-test-mix-scheduler.yaml
```

4.Deploy a replicas is 10 deployment

```bash
kubectl apply -f examples/tenreplicas-test-mix-scheduler.yaml
```

5.Deploy a replicas is 10 statefulset

```bash
kubectl apply -f examples/tenreplicas-sts-mix-scheduler.yaml
```
