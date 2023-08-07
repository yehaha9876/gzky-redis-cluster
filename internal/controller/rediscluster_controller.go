/*
Copyright 2023.

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

package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	redisv1 "gitlab.gzky.com/sean.liu/gzky-redis-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
)

// RedisClusterReconciler reconciles a RedisCluster object
type RedisClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=redis.cache.gzky.com,resources=redisclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=redis.cache.gzky.com,resources=redisclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=redis.cache.gzky.com,resources=redisclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RedisCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *RedisClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var redisCluster redisv1.RedisCluster
	if err := r.Get(ctx, req.NamespacedName, &redisCluster); err != nil {
		log.Error(err, "RedisCluster get error")
	}

	podList, err := r.getAllRedisPod(redisCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Pod list", "podlist lenght", len(podList))

	r.addDeletePods(podList, redisCluster)
	readyFlag, _, err := r.checkAllPodReady(redisCluster)
	if err != nil {
		log.Error(err, "RedisCluster get error")
		log.Info("Wait all pod ready")
	}

	if readyFlag {
		log.Info("All pod ready")
	}

	log.Info("Do return")

	return ctrl.Result{}, nil
}

func (r *RedisClusterReconciler) getAllRedisPod(redisCluster redisv1.RedisCluster) ([]corev1.Pod, error) {
	log := log.FromContext(context.Background())
	podLabels := labels.Set{}

	if redisCluster.Spec.PodTemplate != nil {
		for k, v := range redisCluster.Spec.PodTemplate.Labels {
			log.Info("lables", k, v)
			podLabels[k] = v
		}
	}
	lableSelector := labels.SelectorFromSet(podLabels)

	podList := corev1.PodList{}
	if err := r.List(context.Background(), &podList, client.MatchingLabelsSelector{Selector: lableSelector}, client.InNamespace(redisCluster.Namespace)); err != nil {
		return podList.Items, err
	}

	return podList.Items, nil
}

func (r *RedisClusterReconciler) addDeletePods(podList []corev1.Pod, redisCluster redisv1.RedisCluster) {
	log := log.FromContext(context.Background())
	var vailablePodsNum = 0
	for _, pod := range podList {
		if pod.DeletionTimestamp != nil {
			continue
		}
		vailablePodsNum++
	}

	needPodNum := int(redisCluster.Spec.MasterNum) * int(1+redisCluster.Spec.SlaveNumEach)
	for i := vailablePodsNum; i < needPodNum; i++ {
		newPod := newPodTemplat(redisCluster)
		log.Info("123123")
		if err := r.Create(context.Background(), newPod); err != nil {
			log.Info("Create pod error", "retourn ", err)
		}
	}

	for i := vailablePodsNum; i > needPodNum; i-- {
		delPod := podList[len(podList)-1]
		podList = podList[:len(podList)-1]
		r.Delete(context.Background(), &delPod)
	}
}

func (r *RedisClusterReconciler) checkAllPodReady(redisCluster redisv1.RedisCluster) (bool, []corev1.Pod, error) {
	readyPods := make([]corev1.Pod, 0)
	podList, err := r.getAllRedisPod(redisCluster)
	if err != nil {
		return false, podList, err
	} else {
		for _, pod := range podList {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
					readyPods = append(readyPods)
				}
			}
		}
	}

	allReadyFlag := false
	needPodNum := int(redisCluster.Spec.MasterNum * (1 + redisCluster.Spec.SlaveNumEach))
	if len(readyPods) == needPodNum {
		allReadyFlag = true
	}
	return allReadyFlag, readyPods, nil
}

func newPodTemplat(redisCluster redisv1.RedisCluster) *corev1.Pod {
	podName := fmt.Sprintf("rediscluster-%s-", redisCluster.Name)
	controllerRef := metav1.OwnerReference{
		APIVersion: redisv1.GroupVersion.String(),
		Kind:       redisv1.ResourceKind,
		Name:       redisCluster.Name,
		UID:        redisCluster.UID,
		Controller: boolPtr(true),
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       redisCluster.Namespace,
			GenerateName:    podName,
			Labels:          redisCluster.Spec.PodTemplate.Labels,
			OwnerReferences: []metav1.OwnerReference{controllerRef},
		},
	}

	pod.Spec = redisCluster.Spec.PodTemplate.Spec
	return pod
}

func boolPtr(value bool) *bool {
	return &value
}

// SetupWithManager sets up the controller with the Manager.
func (r *RedisClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&redisv1.RedisCluster{}).
		Complete(r)
}
