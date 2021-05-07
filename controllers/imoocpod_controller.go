/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	batchv1alpha1 "github.com/schwarzeni/kubebuilder-imoocpod/api/v1alpha1"
)

// ImoocPodReconciler reconciles a ImoocPod object
type ImoocPodReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=batch.schwarzeni.github.com,resources=imoocpods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch.schwarzeni.github.com,resources=imoocpods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch.schwarzeni.github.com,resources=imoocpods/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ImoocPod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
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

// SetupWithManager sets up the controller with the Manager.
func (r *ImoocPodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1alpha1.ImoocPod{}).
		Complete(r)
}
