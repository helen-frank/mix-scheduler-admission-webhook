# mix-scheduler-admission-webhook

## Design Ideas

- Spot Instances are ideal for flexible or fault-tolerant applications
- To ensure the high availability of the service, it is necessary to keep a certain amount of application pods on the on-demand node (non-spot node), and the pods on the spot node should be deployed as scattered as possible to avoid the short-term pressure surge caused by the offline of the single-point spot node, which excessively increases the pressure on other pods and reduces the availability of the service.
- Try to ensure that most pods of the application are deployed on different spot nodes
- Support custom selection of namespaces, whether the application accepts adjusted scheduling, by default, kube-system, mix-scheduler-system is not turned on, other namespaces are turned on, you can set the mix-scheduler-admission-webhook: false to turn off scheduling, the scheduling switch on the instance is better than the scheduling switch of the namespace, and the scheduling switch of the namespace is better than the scheduling switch of the mix-scheduler-admission-webhook.
- On-demand and spot selection weights are adjustable. You can webhook the startup configuration and namespace label configuration (spot/weight: n, on-demand/weight: n). The weight configuration on the instance takes precedence over the weight configuration of the namespace, and the weight configuration of the namespace is better than the weight configuration of the mix-scheduler-admission-webhook.

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
Run ./deploy.sh. This creates the CA, certificate, and private key for the webhook server and mix-scheduler-system deploy the resources in the newly created namespace in the Kubernetes cluster.

## Verified

1.The pods in the webhook-server namespace should mix-scheduler-system be running:

```bash
$ kubectl -n mix-scheduler-system get pods
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

## uninstall
```bash
./delete.sh
```
