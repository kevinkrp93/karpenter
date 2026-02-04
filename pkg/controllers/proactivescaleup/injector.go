/*
Copyright The Kubernetes Authors.

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

package proactivescaleup

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/karpenter/pkg/operator/options"
)

const (
	// FakePodLabel is the label added to all fake pods to identify them
	FakePodLabel = "karpenter.sh/fake-pod"
)

// Injector implements the PodInjector interface to inject fake pods for proactive scale-up
type Injector struct {
	kubeClient client.Client
}

// NewInjector creates a new pod injector for proactive scale-up
func NewInjector(kubeClient client.Client) *Injector {
	return &Injector{
		kubeClient: kubeClient,
	}
}

// InjectPods takes the list of real pending pods and injects fake pods based on
// workload definitions (Deployments, ReplicaSets, StatefulSets, Jobs)
func (i *Injector) InjectPods(ctx context.Context, realPods []*corev1.Pod) ([]*corev1.Pod, error) {
	opts := options.FromContext(ctx)
	if !opts.FeatureGates.ProactiveScaleUp {
		return realPods, nil
	}

	log.FromContext(ctx).V(1).Info("proactive scale-up: injecting fake pods")

	// Get all workload definitions
	deployments, replicaSets, statefulSets, jobs, err := i.getWorkloads(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting workloads, %w", err)
	}

	// Build a set of existing pods by owner reference for quick lookup
	existingPodsByOwner := make(map[string]int)
	allPods := &corev1.PodList{}
	if err := i.kubeClient.List(ctx, allPods); err != nil {
		return nil, fmt.Errorf("listing all pods, %w", err)
	}
	for _, pod := range allPods.Items {
		for _, owner := range pod.OwnerReferences {
			key := fmt.Sprintf("%s/%s/%s", pod.Namespace, owner.Kind, owner.Name)
			existingPodsByOwner[key]++
		}
	}

	fakePods := []*corev1.Pod{}

	// Process Deployments
	for _, dep := range deployments {
		if dep.Spec.Replicas == nil {
			continue
		}
		desired := int(*dep.Spec.Replicas)
		key := fmt.Sprintf("%s/ReplicaSet/%s", dep.Namespace, dep.Name)
		existing := existingPodsByOwner[key]
		gap := desired - existing

		if gap > 0 {
			log.FromContext(ctx).V(1).WithValues("Deployment", klog.KObj(&dep), "desired", desired, "existing", existing, "gap", gap).
				Info("proactive scale-up: injecting fake pods for deployment")
			fakePods = append(fakePods, i.generateFakePods(ctx, &dep.Spec.Template, dep.Namespace, "Deployment", dep.Name, gap, opts.PodInjectionLimit-len(fakePods))...)
		}
	}

	// Process ReplicaSets (that are not owned by Deployments)
	for _, rs := range replicaSets {
		// Skip ReplicaSets owned by Deployments
		if hasOwnerKind(rs.OwnerReferences, "Deployment") {
			continue
		}
		if rs.Spec.Replicas == nil {
			continue
		}
		desired := int(*rs.Spec.Replicas)
		key := fmt.Sprintf("%s/ReplicaSet/%s", rs.Namespace, rs.Name)
		existing := existingPodsByOwner[key]
		gap := desired - existing

		if gap > 0 {
			log.FromContext(ctx).V(1).WithValues("ReplicaSet", klog.KObj(&rs), "desired", desired, "existing", existing, "gap", gap).
				Info("proactive scale-up: injecting fake pods for replicaset")
			fakePods = append(fakePods, i.generateFakePods(ctx, &rs.Spec.Template, rs.Namespace, "ReplicaSet", rs.Name, gap, opts.PodInjectionLimit-len(fakePods))...)
		}
	}

	// Process StatefulSets
	for _, sts := range statefulSets {
		if sts.Spec.Replicas == nil {
			continue
		}
		desired := int(*sts.Spec.Replicas)
		key := fmt.Sprintf("%s/StatefulSet/%s", sts.Namespace, sts.Name)
		existing := existingPodsByOwner[key]
		gap := desired - existing

		if gap > 0 {
			log.FromContext(ctx).V(1).WithValues("StatefulSet", klog.KObj(&sts), "desired", desired, "existing", existing, "gap", gap).
				Info("proactive scale-up: injecting fake pods for statefulset")
			fakePods = append(fakePods, i.generateFakePods(ctx, &sts.Spec.Template, sts.Namespace, "StatefulSet", sts.Name, gap, opts.PodInjectionLimit-len(fakePods))...)
		}
	}

	// Process Jobs
	for _, job := range jobs {
		if job.Spec.Parallelism == nil {
			continue
		}
		desired := int(*job.Spec.Parallelism)
		key := fmt.Sprintf("%s/Job/%s", job.Namespace, job.Name)
		existing := existingPodsByOwner[key]
		gap := desired - existing

		if gap > 0 {
			log.FromContext(ctx).V(1).WithValues("Job", klog.KObj(&job), "desired", desired, "existing", existing, "gap", gap).
				Info("proactive scale-up: injecting fake pods for job")
			fakePods = append(fakePods, i.generateFakePods(ctx, &job.Spec.Template, job.Namespace, "Job", job.Name, gap, opts.PodInjectionLimit-len(fakePods))...)
		}
	}

	log.FromContext(ctx).V(1).WithValues("real-pods", len(realPods), "fake-pods", len(fakePods)).
		Info("proactive scale-up: injected fake pods")

	// Combine real and fake pods
	return append(realPods, fakePods...), nil
}

// getWorkloads retrieves all relevant workload definitions from the cluster
func (i *Injector) getWorkloads(ctx context.Context) ([]appsv1.Deployment, []appsv1.ReplicaSet, []appsv1.StatefulSet, []batchv1.Job, error) {
	deploymentList := &appsv1.DeploymentList{}
	if err := i.kubeClient.List(ctx, deploymentList); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("listing deployments, %w", err)
	}

	replicaSetList := &appsv1.ReplicaSetList{}
	if err := i.kubeClient.List(ctx, replicaSetList); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("listing replicasets, %w", err)
	}

	statefulSetList := &appsv1.StatefulSetList{}
	if err := i.kubeClient.List(ctx, statefulSetList); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("listing statefulsets, %w", err)
	}

	jobList := &batchv1.JobList{}
	if err := i.kubeClient.List(ctx, jobList); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("listing jobs, %w", err)
	}

	return deploymentList.Items, replicaSetList.Items, statefulSetList.Items, jobList.Items, nil
}

// generateFakePods creates fake pod objects based on the workload template
func (i *Injector) generateFakePods(ctx context.Context, template *corev1.PodTemplateSpec, namespace, ownerKind, ownerName string, count, limit int) []*corev1.Pod {
	if count <= 0 || limit <= 0 {
		return nil
	}

	// Respect the injection limit
	if count > limit {
		count = limit
	}

	fakePods := make([]*corev1.Pod, count)
	for idx := 0; idx < count; idx++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("fake-%s-%s-%d", ownerKind, ownerName, idx),
				Namespace: namespace,
				Labels:    make(map[string]string),
			},
			Spec: corev1.PodSpec{
				Containers: make([]corev1.Container, len(template.Spec.Containers)),
			},
		}

		// Copy labels from template
		for k, v := range template.Labels {
			pod.Labels[k] = v
		}
		// Add fake pod label
		pod.Labels[FakePodLabel] = "true"

		// Copy node selector
		if template.Spec.NodeSelector != nil {
			pod.Spec.NodeSelector = make(map[string]string)
			for k, v := range template.Spec.NodeSelector {
				pod.Spec.NodeSelector[k] = v
			}
		}

		// Copy affinity
		if template.Spec.Affinity != nil {
			pod.Spec.Affinity = template.Spec.Affinity.DeepCopy()
		}

		// Copy tolerations
		if len(template.Spec.Tolerations) > 0 {
			pod.Spec.Tolerations = make([]corev1.Toleration, len(template.Spec.Tolerations))
			copy(pod.Spec.Tolerations, template.Spec.Tolerations)
		}

		// Copy topology spread constraints
		if len(template.Spec.TopologySpreadConstraints) > 0 {
			pod.Spec.TopologySpreadConstraints = make([]corev1.TopologySpreadConstraint, len(template.Spec.TopologySpreadConstraints))
			for i, tsc := range template.Spec.TopologySpreadConstraints {
				pod.Spec.TopologySpreadConstraints[i] = *tsc.DeepCopy()
			}
		}

		// Copy container resource requests (most important for scheduling)
		for i, container := range template.Spec.Containers {
			pod.Spec.Containers[i] = corev1.Container{
				Name:  container.Name,
				Image: container.Image,
			}
			if container.Resources.Requests != nil {
				pod.Spec.Containers[i].Resources.Requests = make(corev1.ResourceList)
				for k, v := range container.Resources.Requests {
					pod.Spec.Containers[i].Resources.Requests[k] = v.DeepCopy()
				}
			}
			if container.Resources.Limits != nil {
				pod.Spec.Containers[i].Resources.Limits = make(corev1.ResourceList)
				for k, v := range container.Resources.Limits {
					pod.Spec.Containers[i].Resources.Limits[k] = v.DeepCopy()
				}
			}
		}

		// Copy init container resource requests
		if len(template.Spec.InitContainers) > 0 {
			pod.Spec.InitContainers = make([]corev1.Container, len(template.Spec.InitContainers))
			for i, container := range template.Spec.InitContainers {
				pod.Spec.InitContainers[i] = corev1.Container{
					Name:  container.Name,
					Image: container.Image,
				}
				if container.Resources.Requests != nil {
					pod.Spec.InitContainers[i].Resources.Requests = make(corev1.ResourceList)
					for k, v := range container.Resources.Requests {
						pod.Spec.InitContainers[i].Resources.Requests[k] = v.DeepCopy()
					}
				}
			}
		}

		// Set pod to pending state - this is crucial for Karpenter to consider it
		pod.Status.Phase = corev1.PodPending
		pod.Status.Conditions = []corev1.PodCondition{
			{
				Type:   corev1.PodScheduled,
				Status: corev1.ConditionFalse,
				Reason: "Unschedulable",
			},
		}

		fakePods[idx] = pod
	}

	return fakePods
}

// hasOwnerKind checks if any owner reference has the specified kind
func hasOwnerKind(owners []metav1.OwnerReference, kind string) bool {
	return lo.ContainsBy(owners, func(owner metav1.OwnerReference) bool {
		return owner.Kind == kind
	})
}
