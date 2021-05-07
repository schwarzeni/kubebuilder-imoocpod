# KubeBuilder Demo


基于 [视频教程：k8s二次开发operator](https://www.bilibili.com/video/BV1zE411j7ky) 学习 kubebuilder 的基础操作

## 环境

- kubebuilder: v3.0.0
- kubernetes api: v1.19.2

---

## 项目初始化

```bash
# 初始化 go mod
go mod init github.com/schwarzeni/kubebuilder-imoocpo

# 初始化项目骨架
kubebuilder init --domain schwarzeni.github.com

# 初始化 CRD，两个确认都是 y
kubebuilder create api --group batch --version v1alpha1 --kind ImoocPod
```

此时 kubebuilder 已经生成了相关的项目骨架和代码文件

```txt
.
├── Dockerfile
├── Makefile
├── PROJECT
├── api
│   └── v1alpha1
│       ├── groupversion_info.go
│       ├── imoocpod_types.go
│       └── zz_generated.deepcopy.go
├── bin
│   └── controller-gen
├── config
│   ├── crd
│   │   ├── kustomization.yaml
│   │   ├── kustomizeconfig.yaml
│   │   └── patches
│   │       ├── cainjection_in_imoocpods.yaml
│   │       └── webhook_in_imoocpods.yaml
│   ├── default
│   │   ├── kustomization.yaml
│   │   ├── manager_auth_proxy_patch.yaml
│   │   └── manager_config_patch.yaml
│   ├── manager
│   │   ├── controller_manager_config.yaml
│   │   ├── kustomization.yaml
│   │   └── manager.yaml
│   ├── prometheus
│   │   └── ...
│   ├── rbac
│   │   └── ...
│   └── samples
│       └── batch_v1alpha1_imoocpod.yaml
├── controllers
│   ├── imoocpod_controller.go
│   └── suite_test.go
├── go.mod
├── go.sum
├── hack
│   └── ...
└── main.go

```

`api/v1alpha1/imoocpod_types.go` 定义了 ImoocPod 的 CRD，执行命令如下命令生成 CRD 定义的 yaml 文件，文件位置在 `config/crd/bases/batch.schwarzeni.github.com_imoocpods.yaml`

```bash
make manifests
```

之后使用 kubectl 将此 CRD 定义 apply 到 k8s 上

```bash
kubectl apply -f config/crd/bases/batch.schwarzeni.github.com_imoocpods.yaml
```

执行如下命令在本地运行 controller

```bash
make run
```

执行如下命令构建 controller 镜像，第一次执行的时候需要下载相关依赖，所以速度比较慢

```bash
ALL_PROXY=http://xxx:xxx make docker-build docker-push IMG=10.211.55.2:10000/imooc-operator:v1
```

注1：这里使用了本地 registry，ip 为 10.211.55.2，由于不是 https 通信，所以需要对容器运行时做相应的设置，这里使用的是 Docker，相关 insecure-registry 的配置见官网文档：[https://docs.docker.com/registry/insecure/](https://docs.docker.com/registry/insecure/)

```bash
docker run -d -p 10000:5000 --restart always --name registry registry:2.7
```

注2：在构建时需要访问某些需要梯子的网络资源，这里使用了 ALL_PROXY 代理，最好也为 Docker 镜像仓库找一个国内的镜像仓库

构建完毕后执行如下命名将 ImoocPod 的 Operator 部署至 k8s 集群

```bash
ALL_PROXY=http://xxx:xxx  make deploy IMG=10.211.55.2:10000/imooc-operator:v1
```

部署结束后，查看集群相关信息。首先，查看集群命名空间，会发现多出了 `kubebuilder-imoocpod-system`

```bash
kubectl get ns
```

查看相关的 deployment，会发现已经成功部署了

```bash
kubectl get deployment -n kubebuilder-imoocpod-system
```

```txt
NAME                                      READY   UP-TO-DATE   AVAILABLE   AGE
kubebuilder-imoocpod-controller-manager   1/1     1            1           115s
```

---

## 使用 CRD 与 Pod 关联

下面对 Controller 的对 CRD 的编排逻辑进行编码，效果为，每启动一个 ImoocPod，都会在相同的命名空间下启动一个 busybox 的 Pod。这里对 `controllers/imoocpod_controller.go` 中的 `ImoocPodReconciler.Reconcile` 进行编写

```go
func (r *ImoocPodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
  logger := r.Log.WithValues("imoocpod", req.NamespacedName)
  logger.Info("start reconcile")

  // your logic here

  // fetch the ImoocPod instance
  instance := &batchv1alpha1.ImoocPod{}
  if err := r.Client.Get(ctx, req.NamespacedName, instance); err != nil {
    if errors.IsNotFound(err) {
      return ctrl.Result{}, nil
    }
    return ctrl.Result{}, err
  }

  // Define a new Pod object
  pod := newPodForCR(instance)

  // Set ImoocPod instance as the owner and controller
  if err := controllerutil.SetControllerReference(instance, pod, r.Scheme); err != nil {
    return ctrl.Result{}, err
  }

  // Check if this Pod already exists
  found := &corev1.Pod{}
  logger.Info("try to retrieve pods " + pod.Name + " " + pod.Namespace)
  err := r.Client.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, found)
  if err != nil && errors.IsNotFound(err) {
    logger.Info("Create a new Pod " + pod.Name)
    if err := r.Client.Create(ctx, pod); err != nil {
      return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
  } else if err != nil {
    return ctrl.Result{}, err
  }

  logger.Info("skip reconcile: Pod already exists")

  return ctrl.Result{}, nil
}

func newPodForCR(cr *batchv1alpha1.ImoocPod) *corev1.Pod {
  labels := map[string]string{"app": cr.Name}
  return &corev1.Pod{
    ObjectMeta: metav1.ObjectMeta{
      Name:      cr.Name + "-pod",
      Namespace: cr.Namespace,
      Labels:    labels,
    },
    Spec: corev1.PodSpec{
      Containers: []corev1.Container{
        {
          Name:    "busybox",
          Image:   "busybox",
          Command: []string{"sleep", "3600"},
        },
      },
    },
  }
}
```

同时，需要为其添加操作 Pod 相关的 RBAC 权限

```go
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get
```

再次将其部署至 k8s 集群中

```bash
make docker-build docker-push IMG=10.211.55.2:10000/imooc-operator:v2
make deploy IMG=10.211.55.2:10000/imooc-operator:v2
```

尝试部署一个 CRD

```bash
kubectl apply -f config/samples/batch_v1alpha1_imoocpod.yaml  -n kubebuilder-imoocpod-system
```

查看相关的 Pod

```bash
kubectl get pods -n kubebuilder-imoocpod-system
```

输出如下：

```txt
NAME                                                      READY   STATUS    RESTARTS   AGE
imoocpod-sample-pod                                       1/1     Running   0          56s
kubebuilder-imoocpod-controller-manager-665768c6b-v4xlk   2/2     Running   0          2m8s
```

清空相关的部署

```bash
kubectl delete -f config/samples/batch_v1alpha1_imoocpod.yaml  -n kubebuilder-imoocpod-system
make undeploy IMG=10.211.55.2:10000/imooc-operator:v2
```

---

## 使用 CRD 与多个 Pod 关联

尝试实现这样的功能：CRD 指定 busybox Pod 的个数，当 CRD 更新时，Pod 的个数也会做相应的更新。

CRD 结构设计如下: `api/v1alpha1/imoocpod_types.go`

```go
// ImoocPodSpec defines the desired state of ImoocPod
type ImoocPodSpec struct {
        // INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
        // Important: Run "make" to regenerate code after modifying this file

        Replicas int `json:"replicas"`
}

// ImoocPodStatus defines the observed state of ImoocPod
type ImoocPodStatus struct {
        // INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
        // Important: Run "make" to regenerate code after modifying this file

        Replicas int      `json:"replicas"`
        PodNames []string `json:"podNames"`
}
```

ImoocPodSpec 表示预期的状态，而 ImoocPodStatus 表示系统的真实状态

执行命令更新 CRD 定义的 yaml 文件 `config/crd/bases/batch.schwarzeni.github.com_imoocpods.yaml`

```bash
make manifests
```

对 `controllers/imoocpod_controller.go` 中的 `ImoocPodReconciler.Reconcile` 进行编写

```go
func (r *ImoocPodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
  logger := r.Log.WithValues("imoocpod", req.NamespacedName)
  logger.Info("start reconcile")

  // fetch the ImoocPod instance
  instance := &batchv1alpha1.ImoocPod{}
  if err := r.Client.Get(ctx, req.NamespacedName, instance); err != nil {
    if errors.IsNotFound(err) {
      return ctrl.Result{}, nil
    }
    return ctrl.Result{}, err
  }

  // 1. 获取 name 对应的所有的 pod 的列表
  lbls := labels.Set{"app": instance.Name}
  existingPods := &corev1.PodList{}
  if err := r.Client.List(ctx, existingPods, &client.ListOptions{
    Namespace: req.Namespace, LabelSelector: labels.SelectorFromSet(lbls)}); err != nil {
    logger.Error(err, "fetching existing pods failed")
    return ctrl.Result{}, err
  }

  // 2. 获取 pod 列表中的 pod name
  var existingPodNames []string
  for _, pod := range existingPods.Items {
    if pod.GetObjectMeta().GetDeletionTimestamp() != nil {
      continue
    }
    if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
      existingPodNames = append(existingPodNames, pod.GetObjectMeta().GetName())
    }
  }

  // 3. 更新当前状态信息
  currStatus := batchv1alpha1.ImoocPodStatus{
    Replicas: len(existingPodNames),
    PodNames: existingPodNames,
  }
  if !reflect.DeepEqual(instance.Status, currStatus) {
    instance.Status = currStatus
    if err := r.Client.Status().Update(ctx, instance); err != nil {
      logger.Error(err, "update pod failed")
      return ctrl.Result{}, err
    }
  }

  // 4. pod.Spec.Replicas > 运行中的 len(pod.replicas)，比期望值小，需要 scale up create
  if instance.Spec.Replicas > len(existingPodNames) {
    logger.Info(fmt.Sprintf("creating pod, current and expected num: %d %d", len(existingPodNames), instance.Spec.Replicas))
    pod := newPodForCR(instance)
    if err := controllerutil.SetControllerReference(instance, pod, r.Scheme); err != nil {
      logger.Error(err, "scale up failed: SetControllerReference")
      return ctrl.Result{}, err
    }
    if err := r.Client.Create(ctx, pod); err != nil {
      logger.Error(err, "scale up failed: create pod")
      return ctrl.Result{}, err
    }
  }

  // 5. pod.Spec.Replicas < 运行中的 len(pod.replicas)，比期望值大，需要 scale down delete
  if instance.Spec.Replicas < len(existingPodNames) {
    logger.Info(fmt.Sprintf("deleting pod, current and expected num: %d %d", len(existingPodNames), instance.Spec.Replicas))
    pod := existingPods.Items[0]
    existingPods.Items = existingPods.Items[1:]
    if err := r.Client.Delete(ctx, &pod); err != nil {
      logger.Error(err, "scale down faled")
      return ctrl.Result{}, err
    }
  }

  return ctrl.Result{Requeue: true}, nil
}

func newPodForCR(cr *batchv1alpha1.ImoocPod) *corev1.Pod {
  labels := map[string]string{"app": cr.Name}
  return &corev1.Pod{
    ObjectMeta: metav1.ObjectMeta{
      GenerateName: cr.Name + "-pod",
      Namespace:    cr.Namespace,
      Labels:       labels,
    },
    Spec: corev1.PodSpec{
      Containers: []corev1.Container{
        {
          Name:    "busybox",
          Image:   "busybox",
          Command: []string{"sleep", "3600"},
        },
      },
    },
  }
}
```

注意，由于要创建多个 Pod，方法 `newPodForCR` 中 Pod 的名字改成了 `GenerateName`

再次将其部署至 k8s 集群中

```bash
make docker-build docker-push IMG=10.211.55.2:10000/imooc-operator:v3
make deploy IMG=10.211.55.2:10000/imooc-operator:v3
```

更新 crd 定义

```bash
kubectl apply -f config/crd/bases/batch.schwarzeni.github.com_imoocpods.yaml
```

部署一个 crd 实例

```yaml
# config/samples/batch_v1alpha1_imoocpod.yaml
apiVersion: batch.schwarzeni.github.com/v1alpha1
kind: ImoocPod
metadata:
  name: imoocpod-sample
spec:
  replicas: 5
```

```bash
kubectl apply -f config/samples/batch_v1alpha1_imoocpod.yaml  -n kubebuilder-imoocpod-system
```

查看 pod 的个数

```txt
> kubectl get pods -n kubebuilder-imoocpod-system
NAME                                                       READY   STATUS    RESTARTS   AGE
imoocpod-sample-pod4bvch                                   1/1     Running   0          33s
imoocpod-sample-podk2nzs                                   1/1     Running   0          33s
imoocpod-sample-podlrcm2                                   1/1     Running   0          33s
imoocpod-sample-podwl4hh                                   1/1     Running   0          33s
imoocpod-sample-podxl8hh                                   1/1     Running   0          33s
kubebuilder-imoocpod-controller-manager-69cc4f6ff5-lllbs   2/2     Running   0          73s
```

将 `config/samples/batch_v1alpha1_imoocpod.yaml` 中 replicas 改为 2，apply 一下，再查看 pod 的个数

```txt
> kubectl get pods -n kubebuilder-imoocpod-system
NAME                                                       READY   STATUS    RESTARTS   AGE
imoocpod-sample-pod4bvch                                   1/1     Running   0          4m11s
imoocpod-sample-podk2nzs                                   1/1     Running   0          4m11s
kubebuilder-imoocpod-controller-manager-69cc4f6ff5-lllbs   2/2     Running   0          4m51s
```
