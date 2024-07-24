# mix-scheduler-admission-webhook

## 先决条件

测试此示例的集群必须运行 Kubernetes 1.16.0 或更高版本

### 初始化环境

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

检测 admissionregistration.k8s.io/v1启用 API。

```bash
kubectl api-versions | grep admissionregistration.k8s.io/v1
```

此外，MutatingAdmissionWebhook还应在 的 admission-control 标志中添加和列出准入控制器kube-apiserver。

为了构建镜像，需要GNU make和Go 。

## 部署 Webhook 服务器

启动一个满足上述先决条件的 Kubernetes 集群，并确保它处于活动状态（即通过默认位置的配置，或通过设置KUBECONFIG环境变量）。
运行./deploy.sh。这将为 webhook 服务器创建 CA、证书和私钥，并webhook-demo在 Kubernetes 集群中新创建的命名空间中部署资源。

## 核实

1.webhook-server命名空间中的 pod应该webhook-demo正在运行：

```bash
$ kubectl -n webhook-demo get pods
NAME                             READY     STATUS    RESTARTS   AGE
webhook-server-6f976f7bf-hssc9   1/1       Running   0          35m
```

2.应该存在一个MutatingWebhookConfiguration名称：demo-webhook

```bash
$ kubectl get mutatingwebhookconfigurations
NAME           AGE
demo-webhook   36m
```

3.部署一个单副本deployment

```bash
kubectl apply -f examples/onereplicas-test-mix-scheduler.yaml
```

4.部署一个十副本deployment

```bash
kubectl apply -f examples/tenreplicas-test-mix-scheduler.yaml
```

5.部署一个十副本statefulset

```bash
kubectl apply -f examples/tenreplicas-sts-mix-scheduler.yaml
```
